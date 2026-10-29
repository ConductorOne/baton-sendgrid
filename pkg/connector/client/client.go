package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"github.com/conductorone/baton-sendgrid/pkg/connector/models"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
)

// Username and OnBehalfOf are distinct string types so adjacent
// same-purpose parameters (a teammate's username vs. the subuser to act
// on behalf of) can't be silently swapped at a call site — the compiler
// rejects passing one where the other is expected.
type Username string
type OnBehalfOf string

var (
	ErrApiKeyIsEmpty          = errors.New("baton-sendgrid: api key is empty")
	ErrInvalidPaginationToken = errors.New("baton-sendgrid: invalid pagination token")
)

var (
	SendGridBaseUrl      = "https://api.sendgrid.com/"
	SendGridEUBaseUrl    = "https://api.eu.sendgrid.com/"
	AuthHeaderName       = "Authorization"
	OnBehalfOfHeaderName = "on-behalf-of"

	RetrieveAllTeammatesEndpoint     = "v3/teammates"
	InviteTeammateEndpoint           = "v3/teammates"
	DeleteTeammateEndpoint           = "v3/teammates"
	SpecificTeammateEndpoint         = "v3/teammates/%s"
	PendingTeammateEndpoint          = "v3/teammates/pending"
	TeammateSubuserAccessEndpoint    = "v3/teammates/%s/subuser_access"
	TeammateUpdatePermissionEndpoint = "/v3/teammates/%s"

	SubusersEndpoint              = "v3/subusers"
	SpecificSubusersEndpoint      = "v3/subusers/%s"
	SubusersWebsiteAccessEndpoint = "v3/subusers/%s/website_access"

	ScopesEndpoint = "v3/scopes"
)

type CustomErrField struct {
	Message string `json:"message"`
	Field   string `json:"field"`
}

func (c CustomErrField) Error() string {
	return fmt.Sprintf("field: %s, message: %s", c.Field, c.Message)
}

// CustomErr binds both vendor error envelope shapes observed on the SendGrid
// API: the fielded {"errors":[...]} array and the flat {"message": "..."} —
// so neither variant's detail is silently dropped.
type CustomErr struct {
	Message string           `json:"message"` // flat envelope: {"message": "..."}
	Errors  []CustomErrField `json:"errors"`
}

// Error renders the vendor detail as one line: the flat message first (when
// present), then each fielded entry. A CustomErr with neither set renders ""
// — the doRequest caller only joins non-empty strings.
func (c CustomErr) Error() string {
	msgs := make([]string, 0, len(c.Errors)+1)
	if c.Message != "" {
		msgs = append(msgs, c.Message)
	}
	for _, e := range c.Errors {
		msgs = append(msgs, e.Error())
	}
	return strings.Join(msgs, "; ")
}

// SendGridClient is a client for the SendGrid API.
type SendGridClient struct {
	httpClient *uhttp.BaseHttpClient
	baseUrl    *url.URL
	apiKey     string
	pageLimit  int
}

func NewClient(ctx context.Context, baseUrl, apiKey string) (*SendGridClient, error) {
	parseBaseUrl, err := url.Parse(baseUrl)
	if err != nil {
		return nil, err
	}

	if apiKey == "" {
		return nil, ErrApiKeyIsEmpty
	}

	httpClient, err := uhttp.NewClient(ctx, uhttp.WithLogger(true, ctxzap.Extract(ctx)))
	if err != nil {
		return nil, err
	}

	uhtppClient, err := uhttp.NewBaseHttpClientWithContext(ctx, httpClient, uhttp.WithCacheKeyHeaders(OnBehalfOfHeaderName))
	if err != nil {
		return nil, err
	}

	return &SendGridClient{
		httpClient: uhtppClient,
		baseUrl:    parseBaseUrl,
		apiKey:     apiKey,
		pageLimit:  500,
	}, nil
}

// InviteTeammate Invite a teammate.
// https://www.twilio.com/docs/sendgrid/api-reference/teammates/invite-teammate
func (h *SendGridClient) InviteTeammate(ctx context.Context, email string, scopes []string, isAdmin bool) (*models.TeammateInvitation, error) {
	uri := h.getUrl(InviteTeammateEndpoint)
	var response models.TeammateInvitation

	bodyPost := struct {
		Email   string   `json:"email"`
		Scopes  []string `json:"scopes"`
		IsAdmin bool     `json:"is_admin"`
	}{
		Email:   email,
		Scopes:  scopes,
		IsAdmin: isAdmin,
	}

	err := h.doRequest(ctx, http.MethodPost, uri, &response, bodyPost)
	if err != nil {
		return nil, err
	}

	return &response, nil
}

// https://www.twilio.com/docs/sendgrid/api-reference/teammates/delete-teammate
// onBehalfOf, when non-empty, scopes the delete to a sub-account-local teammate
// (i.e. one that only exists inside that subuser). Pass "" for parent-scope teammates.
func (h *SendGridClient) DeleteTeammate(ctx context.Context, username Username, onBehalfOf OnBehalfOf) error {
	uri := h.getUrl(DeleteTeammateEndpoint).JoinPath(string(username))

	return h.doRequest(ctx, http.MethodDelete, uri, nil, nil, onBehalfOfOpts(onBehalfOf)...)
}

// GetSpecificTeammate Retrieve a specific teammate with scopes.
// onBehalfOf, when non-empty, scopes the lookup to a subuser. Pass "" to look up
// the teammate at parent scope. The response is http-cached, so this is for
// read-only callers (sync); reads whose result feeds a write must use
// GetSpecificTeammateNoCache.
func (h *SendGridClient) GetSpecificTeammate(ctx context.Context, username Username, onBehalfOf OnBehalfOf) (*models.TeammateScope, error) {
	return h.getSpecificTeammate(ctx, username, onBehalfOf)
}

// GetSpecificTeammateNoCache is GetSpecificTeammate with the http cache
// bypassed, for the read half of the read-modify-write in scope Grant/Revoke.
// SetTeammateScopes replaces a teammate's entire scope list, so that read must
// see the live list: uhttp caches GETs for an hour, never invalidates them on a
// write, and only clears caches at end-of-sync — so a cached read would let
// back-to-back provisioning tasks on the same teammate each build their new
// scope list from a pre-write snapshot, silently dropping whatever the previous
// task granted.
func (h *SendGridClient) GetSpecificTeammateNoCache(ctx context.Context, username Username, onBehalfOf OnBehalfOf) (*models.TeammateScope, error) {
	return h.getSpecificTeammate(ctx, username, onBehalfOf, uhttp.WithNoCache())
}

func (h *SendGridClient) getSpecificTeammate(
	ctx context.Context,
	username Username,
	onBehalfOf OnBehalfOf,
	extraOpts ...uhttp.RequestOption,
) (*models.TeammateScope, error) {
	uri := h.getUrl(fmt.Sprintf(SpecificTeammateEndpoint, username))
	var requestResponse models.TeammateScope

	err := h.doRequest(
		ctx,
		http.MethodGet,
		uri,
		&requestResponse,
		nil,
		append(onBehalfOfOpts(onBehalfOf), extraOpts...)...,
	)
	if err != nil {
		return nil, err
	}

	return &requestResponse, nil
}

// GetTeammates List All Teammates.
// https://www.twilio.com/docs/sendgrid/api-reference/teammates/retrieve-all-teammates
// onBehalfOf, when non-empty, requests the teammates visible to that subuser
// (a mix of global admins and sub-account-local teammates) instead of the
// parent-scope list.
func (h *SendGridClient) GetTeammates(ctx context.Context, pToken *pagination.Token, onBehalfOf OnBehalfOf) ([]*models.Teammate, string, error) {
	var response models.CommonResponse[[]*models.Teammate]

	offset, err := getTokenValue(pToken)
	if err != nil {
		return nil, "", err
	}

	uri := h.getUrl(RetrieveAllTeammatesEndpoint)
	query := uri.Query()
	query.Add("limit", fmt.Sprintf("%d", h.pageLimit))
	query.Add("offset", fmt.Sprintf("%d", offset))

	uri.RawQuery = query.Encode()

	err = h.doRequest(
		ctx,
		http.MethodGet,
		uri,
		&response,
		nil,
		onBehalfOfOpts(onBehalfOf)...,
	)
	if err != nil {
		return nil, "", err
	}

	return response.Result, nextTokenPage(offset, len(response.Result)), nil
}

// onBehalfOf, when non-empty, scopes the lookup to that subuser.
func (h *SendGridClient) GetTeammatesSubAccess(ctx context.Context, username Username, pToken *pagination.Token, onBehalfOf OnBehalfOf) ([]*models.TeammateSubuser, string, error) {
	var response models.TeammateSubuserResponse

	uri := h.getUrl(fmt.Sprintf(TeammateSubuserAccessEndpoint, username))
	query := uri.Query()
	query.Add("limit", fmt.Sprintf("%d", h.pageLimit))

	if pToken.Token != "" {
		id, err := strconv.Atoi(pToken.Token)
		if err != nil {
			return nil, "", err
		}

		query.Add("after_subuser_id", fmt.Sprintf("%d", id))
	}

	uri.RawQuery = query.Encode()

	err := h.doRequest(
		ctx,
		http.MethodGet,
		uri,
		&response,
		nil,
		onBehalfOfOpts(onBehalfOf)...,
	)
	if err != nil {
		return nil, "", err
	}

	nextToken := ""

	if response.Metadata.NextParams.AfterSubuserId != 0 {
		nextToken = strconv.Itoa(response.Metadata.NextParams.AfterSubuserId)
	}

	return response.SubuserAccess, nextToken, nil
}

// GetPendingTeammates List All Pending Teammates.
// https://www.twilio.com/docs/sendgrid/api-reference/teammates/retrieve-all-pending-teammates
func (h *SendGridClient) GetPendingTeammates(ctx context.Context, pToken *pagination.Token) ([]*models.TeammateInvitation, string, error) {
	var response models.CommonResponse[[]*models.TeammateInvitation]

	offset, err := getTokenValue(pToken)
	if err != nil {
		return nil, "", err
	}

	uri := h.getUrl(PendingTeammateEndpoint)
	query := uri.Query()
	query.Add("limit", fmt.Sprintf("%d", h.pageLimit))
	query.Add("offset", fmt.Sprintf("%d", offset))
	uri.RawQuery = query.Encode()

	err = h.doRequest(ctx, http.MethodGet, uri, &response, nil)
	if err != nil {
		return nil, "", err
	}

	return response.Result, nextTokenPage(offset, len(response.Result)), nil
}

// GetScopes returns the permission scopes assigned to the authenticating API
// key. The vendor shape is a single page {"scopes": [string]} with no
// pagination token (verified against the Twilio SendGrid docs), so no token
// is returned.
// https://www.twilio.com/docs/sendgrid/api-reference/api-key-permissions/retrieve-a-list-of-scopes-for-which-this-user-has-access
func (h *SendGridClient) GetScopes(ctx context.Context) ([]string, error) {
	var res models.ScopesResponse
	err := h.doRequest(ctx, http.MethodGet, h.getUrl(ScopesEndpoint), &res, nil)
	if err != nil {
		return nil, fmt.Errorf("baton-sendgrid: failed to get scopes: %w", err)
	}
	return res.Scopes, nil
}

// DeletePendingTeammate Delete a pending teammate invitation.
// https://www.twilio.com/docs/sendgrid/api-reference/teammates/delete-pending-teammate
func (h *SendGridClient) DeletePendingTeammate(ctx context.Context, token string) error {
	uri := h.getUrl(PendingTeammateEndpoint).JoinPath(token)

	return h.doRequest(ctx, http.MethodDelete, uri, nil, nil)
}

// GetSubusers List All Subusers.
// https://www.twilio.com/docs/sendgrid/api-reference/subusers-api/list-all-subusers
func (h *SendGridClient) GetSubusers(ctx context.Context, pToken *pagination.Token) ([]models.Subuser, string, error) {
	response := make([]models.Subuser, 0)

	offset, err := getTokenValue(pToken)
	if err != nil {
		return nil, "", err
	}

	uri := h.getUrl(SubusersEndpoint)
	query := uri.Query()
	query.Add("limit", fmt.Sprintf("%d", h.pageLimit))
	query.Add("offset", fmt.Sprintf("%d", offset))
	uri.RawQuery = query.Encode()

	err = h.doRequest(ctx, http.MethodGet, uri, &response, nil)
	if err != nil {
		return nil, "", err
	}

	return response, nextTokenPage(offset, len(response)), nil
}

// CreateSubuser Create a Subuser.
// https://www.twilio.com/docs/sendgrid/api-reference/subusers-api/create-subuser
func (h *SendGridClient) CreateSubuser(ctx context.Context, subuser models.SubuserCreate) error {
	uri := h.getUrl(SubusersEndpoint)

	return h.doRequest(ctx, http.MethodPost, uri, nil, subuser)
}

// GetSubuserUsernameByID resolves a subuser's username from its numeric ID.
// SendGrid's Subusers API has no id-based lookup, only by username, so this
// scans paginated GetSubusers results. Callers that need this repeatedly for
// the same subuser (e.g. across List() pages) should cache the result
// themselves — see teammateBuilder's use of the session store.
func (h *SendGridClient) GetSubuserUsernameByID(ctx context.Context, subuserID string) (string, error) {
	token := &pagination.Token{}
	for {
		subusers, next, err := h.GetSubusers(ctx, token)
		if err != nil {
			return "", fmt.Errorf("baton-sendgrid: failed to list subusers while resolving subuser id %s: %w", subuserID, err)
		}

		for _, su := range subusers {
			if strconv.Itoa(su.Id) == subuserID {
				return su.Username, nil
			}
		}

		if next == "" || len(subusers) == 0 {
			break
		}
		token = &pagination.Token{Token: next}
	}

	return "", fmt.Errorf("baton-sendgrid: subuser with id %s not found", subuserID)
}

// DeleteSubuser Delete a Subuser.
// https://www.twilio.com/docs/sendgrid/api-reference/subusers-api/delete-a-subuser
func (h *SendGridClient) DeleteSubuser(ctx context.Context, username string) error {
	uri := h.getUrl(fmt.Sprintf(SpecificSubusersEndpoint, username))

	return h.doRequest(ctx, http.MethodDelete, uri, nil, nil)
}

// SetSubuserDisabled SetSubuserAccess Set Subuser Access.
// https://www.twilio.com/docs/sendgrid/api-reference/subusers-api/enabledisable-website-access-to-a-subuser
func (h *SendGridClient) SetSubuserDisabled(ctx context.Context, username string, disabled bool) error {
	uri := h.getUrl(fmt.Sprintf(SubusersWebsiteAccessEndpoint, username))

	body := struct {
		Disabled bool `json:"disabled"`
	}{
		Disabled: disabled,
	}

	return h.doRequest(ctx, http.MethodPatch, uri, nil, body)
}

// SetTeammateScopes
// https://www.twilio.com/docs/sendgrid/api-reference/teammates/update-teammates-permissions
// onBehalfOf, when non-empty, scopes the update to a sub-account-local teammate.
func (h *SendGridClient) SetTeammateScopes(ctx context.Context, username Username, scopes []string, isAdmin bool, onBehalfOf OnBehalfOf) error {
	uri := h.getUrl(fmt.Sprintf(TeammateUpdatePermissionEndpoint, username))

	body := struct {
		Scopes  []string `json:"scopes"`
		IsAdmin bool     `json:"is_admin"`
	}{
		Scopes:  scopes,
		IsAdmin: isAdmin,
	}

	return h.doRequest(ctx, http.MethodPatch, uri, nil, body, onBehalfOfOpts(onBehalfOf)...)
}

// Helpers

func (h *SendGridClient) getUrl(endPoint string) *url.URL {
	return h.baseUrl.JoinPath(endPoint)
}

func nextTokenPage(offset, count int) string {
	return strconv.Itoa(offset + count)
}

func getTokenValue(pToken *pagination.Token) (int, error) {
	token := pToken.Token
	if token == "" {
		token = "0"
	}

	value, err := strconv.Atoi(token)
	if err != nil {
		return 0, ErrInvalidPaginationToken
	}

	return value, nil
}

// onBehalfOfOpts scopes a request to a subuser when onBehalfOf is set. The
// on-behalf-of header is folded into the cache key via WithCacheKeyHeaders on
// the client, so parent- and subuser-scoped responses stay distinct in the cache.
func onBehalfOfOpts(onBehalfOf OnBehalfOf) []uhttp.RequestOption {
	if onBehalfOf == "" {
		return nil
	}
	return []uhttp.RequestOption{uhttp.WithHeader(OnBehalfOfHeaderName, string(onBehalfOf))}
}

func (h *SendGridClient) doRequest(
	ctx context.Context,
	method string,
	urlAddress *url.URL,
	res interface{},
	body interface{},
	extraOpts ...uhttp.RequestOption,
) error {
	var (
		resp *http.Response
		err  error
	)

	reqOpts := []uhttp.RequestOption{
		uhttp.WithHeader(AuthHeaderName, fmt.Sprintf("Bearer %s", h.apiKey)),
		uhttp.WithJSONBody(body),
	}
	reqOpts = append(reqOpts, extraOpts...)

	req, err := h.httpClient.NewRequest(
		ctx,
		method,
		urlAddress,
		reqOpts...,
	)
	if err != nil {
		return err
	}

	resp, err = h.httpClient.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}

	if resp == nil {
		return err
	}

	// Read the body exactly once. uhttp.BaseHttpClient.Do already drains the
	// body and re-buffers it into a re-readable NopCloser before returning,
	// so a single ReadAll here is safe on every status (probed:
	// baton-sdk v0.42.7 pkg/uhttp/wrapper.go:747-844). No DoOption is passed
	// to Do: WithResponse would decode the body into res on EVERY status,
	// including error statuses, and its decode failure on a non-2xx is
	// stripped by withoutDecodeErrors — both of which AC5 forbids.
	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return errors.Join(err, fmt.Errorf("baton-sendgrid: reading response body: %w", readErr))
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if res != nil && len(respBody) == 0 {
			// Behavior restoration, not a new failure mode: the replaced
			// uhttp.WithJSONResponse path (baton-sdk v0.42.7
			// pkg/uhttp/wrapper.go:228-243) json.Unmarshals the body whenever
			// the decode target is non-nil, so an EMPTY body errored loudly
			// ("unexpected end of JSON input"). Skipping decode would zero
			// out GetSpecificTeammate/GetTeammates/GetScopes and let C1
			// bucket real grants as deleted.
			return errors.Join(err, errors.New("baton-sendgrid: empty response body where JSON was expected"))
		}
		// Nil-target contract restored: callers that pass res == nil (DeleteTeammate,
		// DeletePendingTeammate, CreateSubuser, DeleteSubuser, SetSubuserDisabled,
		// SetTeammateScopes) must skip decoding entirely, exactly as the replaced
		// uhttp.WithJSONResponse path did (baton-sdk v0.42.7 pkg/uhttp/wrapper.go
		// WithResponse is only supplied when a target exists). Unmarshaling into a
		// nil target fails on any body — InvalidUnmarshalError on non-empty,
		// "unexpected end of JSON input" on empty — which broke every 2xx for
		// those fire-and-forget calls.
		if res != nil {
			if unmarshalErr := json.Unmarshal(respBody, res); unmarshalErr != nil {
				// A 2xx with an undecodable body is a wrapped error, never a
				// silent empty result.
				return errors.Join(err, fmt.Errorf("baton-sendgrid: decoding response into %T: %w", res, unmarshalErr))
			}
		}
		if err != nil {
			return err
		}
		return nil
	}

	// Pinned truth (probed against baton-sdk v0.42.7, pkg/uhttp/wrapper.go
	// GrpcCodeFromHTTPStatus, lines 502-529): 429, 502, 503 and 504 map to
	// codes.Unavailable (lines 508-509), and every other 5xx maps to
	// codes.Unavailable via the 500-599 range (lines 522-523). The uhttp
	// error carried in err already carries the right gRPC code for this HTTP
	// status — JOIN the vendor body's detail onto it, never replace it, so
	// callers can keep checking status.Code(err).
	var cErr CustomErr
	// An unparseable error body is tolerated: we fail open on DETAIL, never
	// on the error itself.
	_ = json.Unmarshal(respBody, &cErr)
	errOut := err
	if s := cErr.Error(); s != "" {
		errOut = errors.Join(err, fmt.Errorf("baton-sendgrid: %s", s))
	}
	return errOut
}
