package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/aws/aws-lambda-go/events"

	"healthcloud/internal/auth"
)

// Request is what a handler receives: the raw API Gateway request plus the authenticated caller.
type Request struct {
	events.APIGatewayV2HTTPRequest
	Principal auth.Principal
}

// RequestID is API Gateway's ID for this request. It is echoed in error bodies for debugging.
func (r *Request) RequestID() string { return r.RequestContext.RequestID }

// Path returns a path parameter, e.g. r.Path("hospitalId").
func (r *Request) Path(name string) string { return r.PathParameters[name] }

// Query returns a query-string parameter ("" if absent).
func (r *Request) Query(name string) string { return r.QueryStringParameters[name] }

// Limit parses the optional ?limit= page size (1..100, default 25).
func (r *Request) Limit() (int, error) {
	s := r.Query("limit")
	if s == "" {
		return 25, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 100 {
		return 0, errors.New("limit must be an integer between 1 and 100")
	}
	return n, nil
}

// DecodeJSON unmarshals the request body into v. Unknown fields are rejected so typos are caught.
func (r *Request) DecodeJSON(v any) error {
	dec := json.NewDecoder(stringsReader(r.Body))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// HandlerFunc handles one route.
type HandlerFunc func(ctx context.Context, r *Request) Response

type route struct {
	handler HandlerFunc
	allowed []auth.Group
}

// Router dispatches on API Gateway's RouteKey (e.g. "GET /patients/{patientId}"), which is
// exactly the route declared in template.yaml, so there is no URL parsing to get wrong.
type Router struct {
	routes map[string]route
	log    *slog.Logger
}

func NewRouter(log *slog.Logger) *Router {
	return &Router{routes: map[string]route{}, log: log}
}

// Handle registers a route. allowed lists the Cognito groups that may call it (the first,
// coarse check). Finer rules such as "own hospital only" are checked inside the handler.
func (rt *Router) Handle(routeKey string, allowed []auth.Group, h HandlerFunc) {
	rt.routes[routeKey] = route{handler: h, allowed: allowed}
}

// Serve is the Lambda entry point. It authenticates, checks groups, runs the handler and writes
// one structured access-log line per request.
func (rt *Router) Serve(ctx context.Context, ev events.APIGatewayV2HTTPRequest) (Response, error) {
	start := time.Now()
	reqID := ev.RequestContext.RequestID

	res, userID := rt.dispatch(ctx, ev)

	rt.log.InfoContext(ctx, "request",
		"requestId", reqID,
		"route", ev.RouteKey,
		"status", res.StatusCode,
		"userId", userID,
		"latencyMs", time.Since(start).Milliseconds(),
	)
	return res, nil
}

func (rt *Router) dispatch(ctx context.Context, ev events.APIGatewayV2HTTPRequest) (res Response, userID string) {
	reqID := ev.RequestContext.RequestID

	// A panic in one handler must not kill the container without a response or a log line.
	defer func() {
		if p := recover(); p != nil {
			rt.log.ErrorContext(ctx, "panic", "requestId", reqID, "panic", p)
			res = Error(http.StatusInternalServerError, "INTERNAL", "internal error", reqID)
		}
	}()

	r, ok := rt.routes[ev.RouteKey]
	if !ok {
		return Error(http.StatusNotFound, "NOT_FOUND", "no such route", reqID), ""
	}

	p, err := auth.FromRequest(ev)
	if err != nil {
		return Error(http.StatusUnauthorized, "UNAUTHORIZED", "missing identity", reqID), ""
	}
	if !slices.ContainsFunc(r.allowed, p.Is) {
		return Forbidden(reqID, "your role cannot call this endpoint"), p.UserID
	}

	return r.handler(ctx, &Request{APIGatewayV2HTTPRequest: ev, Principal: p}), p.UserID
}
