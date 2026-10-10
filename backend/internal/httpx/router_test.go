package httpx

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/aws/aws-lambda-go/events"

	"healthcloud/internal/auth"
)

func event(routeKey string, claims map[string]string) events.APIGatewayV2HTTPRequest {
	ev := events.APIGatewayV2HTTPRequest{RouteKey: routeKey}
	if claims != nil {
		ev.RequestContext.Authorizer = &events.APIGatewayV2HTTPRequestContextAuthorizerDescription{
			JWT: &events.APIGatewayV2HTTPRequestContextAuthorizerJWTDescription{Claims: claims},
		}
	}
	return ev
}

func testRouter() *Router {
	rt := NewRouter(slog.New(slog.NewJSONHandler(io.Discard, nil)))
	rt.Handle("GET /doctors-only", []auth.Group{auth.Doctor}, func(ctx context.Context, r *Request) Response {
		return JSON(http.StatusOK, map[string]string{"user": r.Principal.UserID})
	})
	rt.Handle("GET /boom", []auth.Group{auth.Doctor}, func(ctx context.Context, r *Request) Response {
		panic("boom")
	})
	return rt
}

func TestRouterStatuses(t *testing.T) {
	doctor := map[string]string{"sub": "d1", "cognito:groups": "[Doctor]"}
	patient := map[string]string{"sub": "p1", "cognito:groups": "[Patient]"}

	cases := []struct {
		name   string
		ev     events.APIGatewayV2HTTPRequest
		status int
	}{
		{"allowed group", event("GET /doctors-only", doctor), 200},
		{"wrong group", event("GET /doctors-only", patient), 403},
		{"no claims", event("GET /doctors-only", nil), 401},
		{"unknown route", event("GET /nope", doctor), 404},
		{"panic becomes 500", event("GET /boom", doctor), 500},
	}
	rt := testRouter()
	for _, c := range cases {
		res, err := rt.Serve(context.Background(), c.ev)
		if err != nil {
			t.Fatalf("%s: unexpected error %v", c.name, err)
		}
		if res.StatusCode != c.status {
			t.Errorf("%s: status = %d, want %d (%s)", c.name, res.StatusCode, c.status, res.Body)
		}
	}
}

func TestListNeverNull(t *testing.T) {
	var none []string
	if res := List(none, ""); res.Body != `{"items":[]}` {
		t.Fatalf("body = %s", res.Body)
	}
}
