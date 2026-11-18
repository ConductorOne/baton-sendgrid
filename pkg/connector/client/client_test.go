package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/conductorone/baton-sendgrid/pkg/connector/models"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// errorSurfaceHandler serves one fixed response per path, so each test case
// gets a distinct URL (uhttp caches 200 GETs per URL).
type errorSurfaceHandler struct {
	status int
	body   string
}

func (h *errorSurfaceHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(h.status)
	if h.body != "" {
		_, _ = w.Write([]byte(h.body))
	}
}

// newTestClient points a real SendGridClient (uhttp BaseHttpClient included)
// at an httptest server so every response shape below exercises the exact
// production doRequest path.
func newTestClient(t *testing.T, status int, body string) (*SendGridClient, string) {
	t.Helper()

	srv := httptest.NewServer(&errorSurfaceHandler{status: status, body: body})
	t.Cleanup(srv.Close)

	ctx := ctxzap.ToContext(context.Background(), zap.NewNop())
	c, err := NewClient(ctx, srv.URL, "test-api-key")
	require.NoError(t, err)

	return c, srv.URL
}

func TestDoRequest_FlatEnvelope4xx_CarriesVendorMessageAndTargetStaysZero(t *testing.T) {
	// AC5/AC6: an error-status body is never unmarshaled into the success
	// target, and the flat {"message": ...} envelope's reason reaches the
	// caller.
	c, base := newTestClient(t, http.StatusUnauthorized, `{"message":"authorization required"}`)

	var res models.ScopesResponse
	err := c.doRequest(context.Background(), http.MethodGet, c.getUrl(base+"/v3/scopes"), &res, nil)

	require.Error(t, err)
	require.Contains(t, err.Error(), "authorization required")
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.Zero(t, res.Scopes, "error-status body must not populate the success target")
}

func TestDoRequest_FieldedEnvelope4xx_NoJsonLiteral(t *testing.T) {
	// AC4: fielded error detail renders as "field: x, message: ..." with no
	// literal ".json" anywhere in the text.
	c, base := newTestClient(t, http.StatusForbidden, `{"errors":[{"field":"x","message":"authorization required"}]}`)

	var res models.ScopesResponse
	err := c.doRequest(context.Background(), http.MethodGet, c.getUrl(base+"/v3/scopes"), &res, nil)

	require.Error(t, err)
	require.Contains(t, err.Error(), "field: x, message: authorization required")
	require.NotContains(t, err.Error(), ".json")
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestDoRequest_EmptyBody502_StatusErrorSurvives(t *testing.T) {
	// The join falls back cleanly when the error body carries no parseable
	// envelope: the uhttp status error is returned, no panic.
	c, base := newTestClient(t, http.StatusBadGateway, "")

	var res models.ScopesResponse
	err := c.doRequest(context.Background(), http.MethodGet, c.getUrl(base+"/v3/scopes"), &res, nil)

	require.Error(t, err)
	require.Contains(t, err.Error(), "502")
	require.Equal(t, codes.Unavailable, status.Code(err))
}

func TestDoRequest_2xxUndecodableBody_WrappedErrorNotSilentEmpty(t *testing.T) {
	// S1/I5: a 2xx with an undecodable body returns a wrapped error, never a
	// silent empty result.
	c, base := newTestClient(t, http.StatusOK, `not-json`)

	var res models.ScopesResponse
	err := c.doRequest(context.Background(), http.MethodGet, c.getUrl(base+"/v3/scopes"), &res, nil)

	require.Error(t, err)
	require.Contains(t, err.Error(), "baton-sendgrid: decoding response into")
}

// TestDoRequest_2xxEmptyBodyWithDecodeTarget_WrappedErrorNotSilentEmpty
// restores the old WithJSONResponse behavior (baton-sdk v0.42.7
// pkg/uhttp/wrapper.go:228-243): with a non-nil decode target an EMPTY 2xx
// body reached json.Unmarshal and errored loudly. Silently skipping decode
// would zero GetSpecificTeammate/GetTeammates/GetScopes and let C1 bucket
// real grants as deleted.
func TestDoRequest_2xxEmptyBodyWithDecodeTarget_WrappedErrorNotSilentEmpty(t *testing.T) {
	c, base := newTestClient(t, http.StatusOK, "")

	var res models.ScopesResponse
	err := c.doRequest(context.Background(), http.MethodGet, c.getUrl(base+"/v3/scopes"), &res, nil)

	require.Error(t, err)
	require.Contains(t, err.Error(), "baton-sendgrid: empty response body where JSON was expected")
	require.Equal(t, models.ScopesResponse{}, res, "zero-valued target on the empty-body error")
}

// TestDoRequest_NilDecodeTarget_SkipsDecode is the regression for judge
// comment 4410197492: the decode used to run unconditionally, so every
// fire-and-forget 2xx call that passes res == nil (DeleteTeammate,
// DeletePendingTeammate, CreateSubuser, DeleteSubuser, SetSubuserDisabled,
// SetTeammateScopes) failed — InvalidUnmarshalError on a non-empty body,
// "unexpected end of JSON input" on an empty one. Contract: nil target skips
// decode entirely, on any 2xx body.
func TestDoRequest_NilDecodeTarget_SkipsDecode(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{name: "200 non-empty body", status: http.StatusOK, body: `{"foo":"bar"}`},
		{name: "204 empty body", status: http.StatusNoContent, body: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, base := newTestClient(t, tc.status, tc.body)

			err := c.doRequest(context.Background(), http.MethodGet, c.getUrl(base+"/v3/scopes"), nil, nil)

			require.NoError(t, err)
		})
	}
}

// TestDoRequest_RateLimitAndServerErrors_JoinVendorDetailPreservingGrpcCode is
// the regression for the previously-dropped 429/5xx statuses: the old switch
// discarded vendor detail for them. Probed pinned truth (baton-sdk v0.42.7
// pkg/uhttp/wrapper.go GrpcCodeFromHTTPStatus): 429 and every 5xx map to
// codes.Unavailable.
func TestDoRequest_RateLimitAndServerErrors_JoinVendorDetailPreservingGrpcCode(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		wantCode codes.Code
	}{
		{name: "429", status: http.StatusTooManyRequests, wantCode: codes.Unavailable},
		{name: "500", status: http.StatusInternalServerError, wantCode: codes.Unavailable},
		{name: "503", status: http.StatusServiceUnavailable, wantCode: codes.Unavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, base := newTestClient(t, tc.status, `{"message":"vendor says slow down"}`)

			var res models.ScopesResponse
			err := c.doRequest(context.Background(), http.MethodGet, c.getUrl(base+"/v3/scopes"), &res, nil)

			require.Error(t, err)
			require.Contains(t, err.Error(), "baton-sendgrid: vendor says slow down")
			require.Equal(t, tc.wantCode, status.Code(err))
			require.Zero(t, res.Scopes)
		})
	}
}

func TestCustomErr_ErrorRendering(t *testing.T) {
	require.Equal(t, "", CustomErr{}.Error())
	require.Equal(t, "flat only", CustomErr{Message: "flat only"}.Error())
	require.Equal(t,
		"field: a, message: one; field: b, message: two",
		CustomErr{Errors: []CustomErrField{{Field: "a", Message: "one"}, {Field: "b", Message: "two"}}}.Error())
	require.True(t, strings.Contains(CustomErr{Message: "m", Errors: []CustomErrField{{Field: "f", Message: "g"}}}.Error(), "; "))
}
