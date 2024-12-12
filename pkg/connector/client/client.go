package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"io"
	"net/http"
	"net/url"
)

var (
	ErrHostIsNotValid = errors.New("host is not valid")
	ErrApiKeyIsEmpty  = errors.New("api key is empty")
)

var (
	SendGridBaseUrl   = "https://api.sendgrid.com/"
	SendGridEUBaseUrl = "https://api.eu.sendgrid.com/"
	AuthHeaderName    = "Authorization"

	RetrieveAllTeammatesEndpoint = "v3/teammates"
	InviteTeammateEndpoint       = "v3/teammates"
	DeleteTeammateEndpoint       = "v3/teammates"
	SpecificTeammateEndpoint     = "v3/teammates/%s"
	PendingTeammateEndpoint      = "v3/teammates/pending"
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
// TODO: Create an interface for this client.
type SendGridClient struct {
	httpClient *uhttp.BaseHttpClient
	baseUrl    *url.URL
	apiKey     string
	limit      int
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
		limit:      500,
	}, nil
}

func (h *SendGridClient) getUrl(endPoint string) *url.URL {
	return h.baseUrl.JoinPath(endPoint)
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
func (h *SendGridClient) GetSpecificTeammate(ctx context.Context, username string) (*TeammateScope, error) {
	uri := h.getUrl(fmt.Sprintf(SpecificTeammateEndpoint, username))
	var requestResponse TeammateScope

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
func (h *SendGridClient) GetTeammates(ctx context.Context) ([]Teammate, error) {
	limit := h.limit

	var response []Teammate
	offset := 0

	var requestResponse CommonResponse[[]Teammate]

	for {
		uri := h.getUrl(RetrieveAllTeammatesEndpoint)
		query := uri.Query()
		query.Add("limit", fmt.Sprintf("%d", limit))
		query.Add("offset", fmt.Sprintf("%d", offset))

		uri.RawQuery = query.Encode()

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

		response = append(response, requestResponse.Result...)

		if len(requestResponse.Result) == 0 {
			break
		} else {
			offset += len(requestResponse.Result)
		}
	}

	return response, nil
}

// GetPendingTeammates List All Pending Teammates.
// https://www.twilio.com/docs/sendgrid/api-reference/teammates/retrieve-all-pending-teammates
func (h *SendGridClient) GetPendingTeammates(ctx context.Context) ([]PendingUserAccess, error) {
	uri := h.getUrl(PendingTeammateEndpoint)

	var response []PendingUserAccess

	err := h.doRequest(ctx, http.MethodGet, uri, response, nil)
	if err != nil {
		return nil, err
	}

	return response, nil
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
	case http.MethodPost:
		resp, err = h.httpClient.Do(req)
		if resp != nil {
			defer resp.Body.Close()
		}
	}

	if resp != nil && (resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusBadRequest) {
		cErr, err := getError(resp)
		if err != nil {
			return err
		}

		return cErr.Error()
	}

	if err != nil {
		return err
	}

	return nil
}
