package logger

import (
	"context"
	"testing"
)

func TestNopLogger(t *testing.T) {
	var logger Logger = NopLogger{}
	ctx := context.Background()
	logger.Debug(ctx, "debug")
	logger.Info(ctx, "info")
	logger.Warn(ctx, "warn")
	logger.Error(ctx, "error")
	if logger.With("key", "value") == nil {
		t.Fatal("With returned nil")
	}
}

func TestSlogLogger(t *testing.T) {
	for _, tc := range []struct{ level, format string }{
		{"debug", "text"}, {"warning", "json"}, {"error", "text"}, {"info", "json"}, {"unknown", "unknown"},
	} {
		logger := New(tc.level, tc.format)
		if logger == nil || logger.logger == nil {
			t.Fatalf("New(%q, %q) returned nil", tc.level, tc.format)
		}
		ctx := context.Background()
		logger.Debug(ctx, "debug", "key", "value")
		logger.Info(ctx, "info")
		logger.Warn(ctx, "warn")
		logger.Error(ctx, "error")
		if logger.With("component", "test") == nil {
			t.Fatal("With returned nil")
		}
	}
}
