package logging

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestLogLineIsJSONWithContext(t *testing.T) {
	var buf bytes.Buffer
	newWithWriter(&buf, "health", "dev").Info("hello", "requestId", "r1")

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("log line is not JSON: %v (%s)", err, buf.String())
	}
	for k, want := range map[string]string{"msg": "hello", "function": "health", "stage": "dev", "requestId": "r1"} {
		if line[k] != want {
			t.Errorf("%s = %v, want %q", k, line[k], want)
		}
	}
}
