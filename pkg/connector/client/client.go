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

	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"github.com/conductorone/baton-sendgrid/pkg/connector/models"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
)

var (
	ErrApiKeyIsEmpty          = errors.New("baton-sendgrid: api key is empty")
	ErrInvalidPaginationToken = errors.New("baton-sendgrid: invalid pagination token")
)

// StatusError wraps an API error with the HTTP status code that produced it,
// so callers can distinguish e.g. "not found" from other failures without
// relying on fragile string matching against the error message.
type StatusError struct {
	StatusCode int
	Err        error
}

func (e *StatusError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("baton-sendgrid: request failed with status %d", e.StatusCode)
	}
	return e.Err.Error()
}

func (e *StatusError) Unwrap() error {
	return e.Err
}

// IsNotFoundErr returns true if err is (or wraps) a StatusError for a 404 response.
func IsNotFoundErr(err error) bool {
	var se *StatusError
	return errors.As(err, &se) && se.StatusCode == http.StatusNotFound
}

// IsForbiddenErr returns true if err is (or wraps) a StatusError for a 403
// response, e.g. the API key lacks permission to act on/for a given subuser.
func IsForbiddenErr(err error) bool {
	var se *StatusError
	return errors.As(err, &se) && se.StatusCode == http.StatusForbidden
}

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
)

type CustomErrField struct {
	Message string `json:"message"`
	Field   string `json:"field"`
}

func (c CustomErrField) Error() string {
	return fmt.Sprintf("field: %s.json, message: %s.json", c.Field, c.Message)
}

type CustomErr struct {
	Errors []CustomErrField `json:"errors"`
}

func (c CustomErr) Error() error {
	errorsResult := make([]error, len(c.Errors))
	for i, err := range c.Errors {
		errorsResult[i] = err
	}

	return errors.Join(errorsResult...)
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

	uhtppClient, err := uhttp.NewBaseHttpClientWithContext(ctx, httpClient)
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
func (h *SendGridClient) DeleteTeammate(ctx context.Context, username string, onBehalfOf string) error {
	uri := h.getUrl(DeleteTeammateEndpoint).JoinPath(username)

	return h.doRequest(ctx, http.MethodDelete, uri, nil, nil, onBehalfOfOpts(onBehalfOf)...)
}

// GetSpecificTeammate Retrieve a specific teammate with scopes.
// onBehalfOf, when non-empty, scopes the lookup to a subuser. Pass "" to look up
// the teammate at parent scope (used e.g. to probe whether a username already
// exists globally, see TeammateExistsAtParentScope).
func (h *SendGridClient) GetSpecificTeammate(ctx context.Context, username string, onBehalfOf string) (*models.TeammateScope, error) {
	uri := h.getUrl(fmt.Sprintf(SpecificTeammateEndpoint, username))
	var requestResponse models.TeammateScope

	err := h.doRequest(
		ctx,
		http.MethodGet,
		uri,
		&requestResponse,
		nil,
		onBehalfOfOpts(onBehalfOf)...,
	)
	if err != nil {
		return nil, err
	}

	return &requestResponse, nil
}

// TeammateExistsAtParentScope reports whether username already exists as a
// parent-scope (global) teammate. Used to dedupe sub-account-local teammates
// discovered via on-behalf-of against the global teammate list synced in
// Phase A, so a global admin is never re-emitted with a different parent.
func (h *SendGridClient) TeammateExistsAtParentScope(ctx context.Context, username string) (bool, error) {
	_, err := h.GetSpecificTeammate(ctx, username, "")
	if err == nil {
		return true, nil
	}
	if IsNotFoundErr(err) {
		return false, nil
	}
	return false, fmt.Errorf("baton-sendgrid: failed to probe parent-scope teammate %s: %w", username, err)
}

// GetTeammates List All Teammates.
// https://www.twilio.com/docs/sendgrid/api-reference/teammates/retrieve-all-teammates
// onBehalfOf, when non-empty, requests the teammates visible to that subuser
// (a mix of global admins and sub-account-local teammates) instead of the
// parent-scope list.
func (h *SendGridClient) GetTeammates(ctx context.Context, pToken *pagination.Token, onBehalfOf string) ([]models.Teammate, string, error) {
	var response models.CommonResponse[[]models.Teammate]

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

	return response.Result, nextTokenPage(offset), nil
}

// onBehalfOf, when non-empty, scopes the lookup to that subuser.
func (h *SendGridClient) GetTeammatesSubAccess(ctx context.Context, username string, pToken *pagination.Token, onBehalfOf string) ([]models.TeammateSubuser, string, error) {
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

	return response.Result, nextTokenPage(offset), nil
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

	return response, nextTokenPage(offset), nil
}

// CreateSubuser Create a Subuser.
// https://www.twilio.com/docs/sendgrid/api-reference/subusers-api/create-subuser
func (h *SendGridClient) CreateSubuser(ctx context.Context, subuser models.SubuserCreate) error {
	uri := h.getUrl(SubusersEndpoint)

	return h.doRequest(ctx, http.MethodPost, uri, nil, subuser)
}

// GetSubuserUsernameByID resolves a subuser's username from its numeric ID.
// SendGrid's Subusers API has no id-based lookup, only by username, so this
// scans paginated GetSubusers results. It is used for single-item
// provisioning lookups (resolving the on-behalf-of subuser for Grants/Delete
// on a sub-account-local teammate), and is also called once per page from
// listSubuserTeammates as part of the SDK-driven List() pagination loop —
// each call re-scans all subuser pages from scratch, so callers that need
// this repeatedly for the same subuser should cache the result.
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
func (h *SendGridClient) SetTeammateScopes(ctx context.Context, username string, scopes []string, isAdmin bool, onBehalfOf string) error {
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

func getError(resp *http.Response) (CustomErr, error) {
	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return CustomErr{}, err
	}

	var cErr CustomErr
	err = json.Unmarshal(bytes, &cErr)
	if err != nil {
		return cErr, err
	}

	return cErr, nil
}

func nextTokenPage(offset int) string {
	return strconv.Itoa(offset + 1)
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

// onBehalfOfOpts builds the optional on-behalf-of header request option.
// Returns nil (no extra options) when onBehalfOf is empty, so the same call
// path works for parent-scope and subuser-scoped requests.
func onBehalfOfOpts(onBehalfOf string) []uhttp.RequestOption {
	if onBehalfOf == "" {
		return nil
	}
	return []uhttp.RequestOption{uhttp.WithHeader(OnBehalfOfHeaderName, onBehalfOf)}
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

	// Build request options
	doOptions := []uhttp.DoOption{}
	// If the response target is not nil, unmarshal the response into it
	if res != nil {
		doOptions = append(doOptions, uhttp.WithResponse(&res))
	}

	resp, err = h.httpClient.Do(req, doOptions...)
	if resp != nil {
		defer resp.Body.Close()
	}

	if resp != nil {
		if resp.StatusCode == http.StatusUnauthorized {
			return &StatusError{StatusCode: resp.StatusCode, Err: errors.New("unauthorized")}
		}

		if resp.StatusCode == http.StatusForbidden {
			return &StatusError{StatusCode: resp.StatusCode, Err: errors.New("forbidden")}
		}

		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusBadRequest {
			cErr, err := getError(resp)
			if err != nil {
				return err
			}
			cErrErr := cErr.Error()
			if cErrErr == nil {
				cErrErr = fmt.Errorf("baton-sendgrid: request failed with status %d and no error details", resp.StatusCode)
			}
			return &StatusError{StatusCode: resp.StatusCode, Err: cErrErr}
		}

		return err
	}

	if err != nil {
		return err
	}

	return nil
}
