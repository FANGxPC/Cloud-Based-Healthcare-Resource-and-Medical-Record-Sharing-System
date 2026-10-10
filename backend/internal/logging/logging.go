// Package logging builds the structured JSON logger shared by every Lambda.
//
// Each log line is one JSON object written to stdout. Lambda forwards stdout to CloudWatch Logs,
// where the fields can be searched with CloudWatch Logs Insights, e.g.
//
//	fields @timestamp, msg, requestId | filter level = "ERROR"
//
// Rule: log IDs and counts only, never patient names, dates of birth or document contents (PHI).
package logging

import (
	"io"
	"log/slog"
	"os"
)

// New returns a JSON logger that tags every line with the function name and stage.
func New(function, stage string) *slog.Logger {
	return newWithWriter(os.Stdout, function, stage)
}

// newWithWriter lets tests capture output instead of writing to stdout.
func newWithWriter(w io.Writer, function, stage string) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, nil)).With("function", function, "stage", stage)
}
