package contextkey

import (
	"context"
	"testing"
)

func TestTraceIDStandardContextRoundTrip(t *testing.T) {
	const expected = "trace-context-123"
	ctx := WithTraceID(context.Background(), expected)
	if got, ok := TraceIDFromContext(ctx); !ok || got != expected {
		t.Fatalf("TraceIDFromContext() = %q, %v; want %q, true", got, ok, expected)
	}
}

func TestTraceIDFromNilContext(t *testing.T) {
	if got, ok := TraceIDFromContext(nil); ok || got != "" {
		t.Fatalf("TraceIDFromContext(nil) = %q, %v; want empty, false", got, ok)
	}
}
