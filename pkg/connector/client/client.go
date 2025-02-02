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

var (
	SendGridBaseUrl   = "https://api.sendgrid.com/"
	SendGridEUBaseUrl = "https://api.eu.sendgrid.com/"
	AuthHeaderName    = "Authorization"

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
func (h *SendGridClient) InviteTeammate(ctx context.Context, email string, scopes []string, isAdmin bool) error {
	uri := h.getUrl(InviteTeammateEndpoint)

	bodyPost := struct {
		Email   string   `json:"email"`
		Scopes  []string `json:"scopes"`
		IsAdmin bool     `json:"is_admin"`
	}{
		Email:   email,
		Scopes:  scopes,
		IsAdmin: isAdmin,
	}

	return h.doRequest(ctx, http.MethodPost, uri, nil, bodyPost)
}

// DeleteTeammate Delete a teammate.
// https://www.twilio.com/docs/sendgrid/api-reference/teammates/delete-teammate
func (h *SendGridClient) DeleteTeammate(ctx context.Context, username string) error {
	uri := h.getUrl(DeleteTeammateEndpoint).JoinPath(username)

	return h.doRequest(ctx, http.MethodDelete, uri, nil, nil)
}

// GetSpecificTeammate Retrieve a specific teammate with scopes.
func (h *SendGridClient) GetSpecificTeammate(ctx context.Context, username string) (*models.TeammateScope, error) {
	uri := h.getUrl(fmt.Sprintf(SpecificTeammateEndpoint, username))
	var requestResponse models.TeammateScope

	err := h.doRequest(
		ctx,
		http.MethodGet,
		uri,
		&requestResponse,
		nil,
	)
	if err != nil {
		return nil, err
	}

	return &requestResponse, nil
}

// GetTeammates List All Teammates.
// https://www.twilio.com/docs/sendgrid/api-reference/teammates/retrieve-all-teammates
func (h *SendGridClient) GetTeammates(ctx context.Context, pToken *pagination.Token) ([]models.Teammate, string, error) {
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
	)
	if err != nil {
		return nil, "", err
	}

	return response.Result, nextTokenPage(offset), nil
}

func (h *SendGridClient) GetTeammatesSubAccess(ctx context.Context, username string, pToken *pagination.Token) ([]models.TeammateSubuser, string, error) {
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
func (h *SendGridClient) GetPendingTeammates(ctx context.Context, pToken *pagination.Token) ([]models.PendingUserAccess, string, error) {
	var response []models.PendingUserAccess

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

	return response, "", nil
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
func (h *SendGridClient) SetTeammateScopes(ctx context.Context, username string, scopes []string, isAdmin bool) error {
	uri := h.getUrl(fmt.Sprintf(TeammateUpdatePermissionEndpoint, username))

	body := struct {
		Scopes  []string `json:"scopes"`
		IsAdmin bool     `json:"is_admin"`
	}{
		Scopes:  scopes,
		IsAdmin: isAdmin,
	}

	return h.doRequest(ctx, http.MethodPatch, uri, nil, body)
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

func (h *SendGridClient) doRequest(
	ctx context.Context,
	method string,
	urlAddress *url.URL,
	res interface{},
	body interface{},
) error {
	var (
		resp *http.Response
		err  error
	)

	req, err := h.httpClient.NewRequest(
		ctx,
		method,
		urlAddress,
		uhttp.WithHeader(AuthHeaderName, fmt.Sprintf("Bearer %s", h.apiKey)),
		uhttp.WithJSONBody(body),
	)
	if err != nil {
		return err
	}

	switch method {
	case http.MethodGet:
		resp, err = h.httpClient.Do(req, uhttp.WithResponse(&res))
		if resp != nil {
			defer resp.Body.Close()
		}
	case http.MethodPost, http.MethodPatch, http.MethodDelete:
		resp, err = h.httpClient.Do(req)
		if resp != nil {
			defer resp.Body.Close()
		}
	}

	if resp != nil {
		if resp.StatusCode == http.StatusUnauthorized {
			return errors.New("unauthorized")
		}

		if resp.StatusCode == http.StatusForbidden {
			return errors.New("forbidden")
		}

		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusBadRequest {
			cErr, err := getError(resp)
			if err != nil {
				return err
			}
			return cErr.Error()
		}

		return err
	}

	if err != nil {
		return err
	}

	return nil
}
