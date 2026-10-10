package httpx

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestJSON(t *testing.T) {
	res := JSON(http.StatusOK, map[string]string{"status": "ok"})

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if got := res.Headers["Content-Type"]; got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}
	if res.Body != `{"status":"ok"}` {
		t.Fatalf("body = %s", res.Body)
	}
}

func TestJSONUnencodable(t *testing.T) {
	// Channels can't be marshalled to JSON; JSON must fall back to a 500, not panic.
	res := JSON(http.StatusOK, make(chan int))
	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", res.StatusCode)
	}
}

func TestError(t *testing.T) {
	res := Error(http.StatusForbidden, "FORBIDDEN", "not your hospital", "req-1")

	var body ErrorBody
	if err := json.Unmarshal([]byte(res.Body), &body); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 403 || body.Code != "FORBIDDEN" || body.RequestID != "req-1" {
		t.Fatalf("unexpected response: %d %+v", res.StatusCode, body)
	}
}
