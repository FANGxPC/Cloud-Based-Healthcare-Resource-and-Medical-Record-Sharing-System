// Package testutil builds fake API Gateway events for handler tests.
package testutil

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/aws/aws-lambda-go/events"

	"healthcloud/internal/httpx"
)

// Users used across tests.
var (
	Patient1 = map[string]string{"sub": "pat-1", "cognito:groups": "[Patient]"}
	Patient2 = map[string]string{"sub": "pat-2", "cognito:groups": "[Patient]"}
	DoctorH1 = map[string]string{"sub": "doc-1", "cognito:groups": "[Doctor]", "custom:hospital_id": "H001"}
	AdminH1  = map[string]string{"sub": "adm-1", "cognito:groups": "[HospitalAdmin]", "custom:hospital_id": "H001"}
	AdminH2  = map[string]string{"sub": "adm-2", "cognito:groups": "[HospitalAdmin]", "custom:hospital_id": "H002"}
)

// Req describes a request to send to a handler.
type Req struct {
	Route  string            // e.g. "GET /patients/{patientId}"
	Claims map[string]string // nil means unauthenticated
	Path   map[string]string
	Query  map[string]string
	Body   string
}

// Event converts a Req into the payload API Gateway would send to Lambda.
func (r Req) Event() events.APIGatewayV2HTTPRequest {
	ev := events.APIGatewayV2HTTPRequest{
		RouteKey:              r.Route,
		PathParameters:        r.Path,
		QueryStringParameters: r.Query,
		Body:                  r.Body,
	}
	ev.RequestContext.RequestID = "test-req"
	if r.Claims != nil {
		ev.RequestContext.Authorizer = &events.APIGatewayV2HTTPRequestContextAuthorizerDescription{
			JWT: &events.APIGatewayV2HTTPRequestContextAuthorizerJWTDescription{Claims: r.Claims},
		}
	}
	return ev
}

// Logger discards output.
func Logger() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

// Decode unmarshals a response body into T or fails the test.
func Decode[T any](t *testing.T, res httpx.Response) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(res.Body), &v); err != nil {
		t.Fatalf("decode %q: %v", res.Body, err)
	}
	return v
}
