// Command health is the unauthenticated liveness endpoint: GET /health.
//
// It proves the whole deployment chain works (API Gateway HTTP API → Go Lambda on arm64 →
// JSON response) without needing Cognito or DynamoDB, so it is the first thing to check after a deploy.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"

	"healthcloud/internal/httpx"
	"healthcloud/internal/logging"
)

// app holds dependencies built once per Lambda container (cold start) and reused across
// invocations. Putting them in a struct also makes the handler easy to unit-test.
type app struct {
	stage string
	log   *slog.Logger
}

type healthResponse struct {
	Status string `json:"status"`
	Stage  string `json:"stage"`
}

func (a *app) handle(ctx context.Context, req events.APIGatewayV2HTTPRequest) (httpx.Response, error) {
	a.log.InfoContext(ctx, "health check", "requestId", req.RequestContext.RequestID)
	return httpx.JSON(http.StatusOK, healthResponse{Status: "ok", Stage: a.stage}), nil
}

func main() {
	stage := os.Getenv("STAGE") // set by template.yaml Globals
	a := &app{stage: stage, log: logging.New("health", stage)}
	lambda.Start(a.handle)
}
