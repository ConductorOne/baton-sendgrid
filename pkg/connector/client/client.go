package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"io"
	"net/http"
	"net/url"
)

var (
	ErrHostIsNotValid = errors.New("host is not valid")
	ErrApiKeyIsEmpty  = errors.New("api key is empty")
)

var (
	SendGridBaseUrl = "https://api.sendgrid.com/"
	AuthHeaderName  = "Authorization"

	retrieveAllTeammatesEndpoint = "v3/teammates"
)

type CustomErrField struct {
	Message string `json:"message"`
	Field   string `json:"field"`
}

func (c CustomErrField) Error() string {
	return fmt.Sprintf("field: %s, message: %s", c.Field, c.Message)
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

type SendGridClient struct {
	httpClient *uhttp.BaseHttpClient
	baseUrl    *url.URL
	apiKey     string
	limit      int
}

func NewClient(baseUrl, apiKey string) (*SendGridClient, error) {
	parseBaseUrl, err := url.Parse(baseUrl)
	if err != nil {
		return nil, err
	}

	if apiKey == "" {
		return nil, ErrApiKeyIsEmpty
	}

	return &SendGridClient{
		httpClient: &uhttp.BaseHttpClient{},
		baseUrl:    parseBaseUrl,
		apiKey:     apiKey,
		limit:      500,
	}, nil
}

func (h *SendGridClient) getUrl(endPoint string) *url.URL {
	return h.baseUrl.JoinPath(endPoint)
}

// GetTeammates List All Teammates.
// https://www.twilio.com/docs/sendgrid/api-reference/teammates/retrieve-all-teammates
func (h *SendGridClient) GetTeammates(ctx context.Context) ([]SubUserAccess, error) {
	limit := h.limit

	var response []SubUserAccess

	requestResponse := struct {
		SubUserAccess []SubUserAccess `json:"subuser_access"`
		Metadata      struct {
			NextParams PaginationData `json:"next_params"`
		} `json:"metadata"`
	}{}

	afterSubuserId := ""

	for {
		uri := h.getUrl(retrieveAllTeammatesEndpoint)
		query := uri.Query()
		query.Add("limit", fmt.Sprintf("%d", limit))
		if afterSubuserId != "" {
			query.Add("after_subuser_id", afterSubuserId)
		}
		uri.RawQuery = query.Encode()

		err := h.getAPIData(ctx,
			http.MethodGet,
			uri,
			&requestResponse,
		)
		if err != nil {
			return nil, err
		}

		response = append(response, requestResponse.SubUserAccess...)

		if requestResponse.Metadata.NextParams.AfterSubuserId != "" {
			afterSubuserId = requestResponse.Metadata.NextParams.AfterSubuserId
		} else {
			break
		}
	}

	return response, nil
}

func (h *SendGridClient) getAPIData(
	ctx context.Context,
	method string,
	uri *url.URL,
	res any,
) error {
	if err := h.doRequest(ctx, method, uri, &res, nil); err != nil {
		return err
	}

	return nil
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
		uhttp.WithHeader(AuthHeaderName, h.apiKey),
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
