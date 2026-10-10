package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/aws/aws-lambda-go/events"
)

func TestHealth(t *testing.T) {
	a := &app{stage: "test", log: slog.New(slog.NewJSONHandler(io.Discard, nil))}

	res, err := a.handle(context.Background(), events.APIGatewayV2HTTPRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}

	var body healthResponse
	if err := json.Unmarshal([]byte(res.Body), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "ok" || body.Stage != "test" {
		t.Fatalf("unexpected body: %+v", body)
	}
}
