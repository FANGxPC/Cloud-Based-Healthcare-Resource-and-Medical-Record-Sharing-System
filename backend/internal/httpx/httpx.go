// Package httpx contains small helpers for building API Gateway HTTP API (payload v2)
// responses, so every Lambda returns JSON and errors in the same shape.
package httpx

import (
	"encoding/json"
	"net/http"

	"github.com/aws/aws-lambda-go/events"
)

// Response is the API Gateway HTTP API (payload format 2.0) response type.
type Response = events.APIGatewayV2HTTPResponse

// ErrorBody is the standard error payload (components/schemas/Error in api/openapi.yaml).
type ErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId,omitempty"`
}

// JSON marshals body and returns it with the given status code.
// If marshalling fails (a programming error), it returns a 500 instead of a broken body.
func JSON(status int, body any) Response {
	b, err := json.Marshal(body)
	if err != nil {
		return Response{
			StatusCode: http.StatusInternalServerError,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       `{"code":"INTERNAL","message":"failed to encode response"}`,
		}
	}
	return Response{
		StatusCode: status,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(b),
	}
}

// Error returns a JSON error in the standard shape, e.g. Error(403, "FORBIDDEN", "...", reqID).
func Error(status int, code, message, requestID string) Response {
	return JSON(status, ErrorBody{Code: code, Message: message, RequestID: requestID})
}
