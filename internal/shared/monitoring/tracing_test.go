package monitoring

import (
	"context"
	"testing"
)

func TestTraceLifecycle(t *testing.T) {
	trace := NewTrace()
	if len(trace.TraceID) != 32 || len(trace.SpanID) != 16 || !trace.Sampled {
		t.Fatalf("trace = %+v", trace)
	}

	ctx := context.Background()
	if _, ok := TraceFromContext(ctx); ok {
		t.Fatal("expected no trace")
	}
	if TraceID(ctx) != "" || Traceparent(ctx) != "" {
		t.Fatal("expected empty trace values")
	}
	if _, ok := TraceFromContext(nil); ok {
		t.Fatal("nil context must not contain trace")
	}

	ctx = WithTrace(ctx, trace)
	found, ok := TraceFromContext(ctx)
	if !ok || found.TraceID != trace.TraceID {
		t.Fatalf("trace = %+v %v", found, ok)
	}
	if TraceID(ctx) != trace.TraceID {
		t.Fatalf("trace id = %q", TraceID(ctx))
	}
	want := "00-" + trace.TraceID + "-" + trace.SpanID + "-01"
	if Traceparent(ctx) != want {
		t.Fatalf("traceparent = %q, want %q", Traceparent(ctx), want)
	}
}

func TestParseTraceparent(t *testing.T) {
	valid := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	trace := ParseTraceparent(valid)
	if trace.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" || !trace.Sampled {
		t.Fatalf("trace = %+v", trace)
	}

	for _, invalid := range []string{
		"", "invalid", "00-abc-00f067aa0ba902b7-01",
		"00-4bf92f3577b34da6a3ce929d0e0e4736-xyz-01",
		"00-zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz-00f067aa0ba902b7-01",
	} {
		generated := ParseTraceparent(invalid)
		if len(generated.TraceID) != 32 || len(generated.SpanID) != 16 {
			t.Fatalf("invalid %q produced %+v", invalid, generated)
		}
	}
}

func TestStartSpan(t *testing.T) {
	ctx, span := StartSpan(context.Background(), "test-span")
	if span.Name != "test-span" || span.Trace.TraceID == "" {
		t.Fatalf("span = %+v", span)
	}
	if TraceID(ctx) != span.Trace.TraceID {
		t.Fatal("span trace missing from context")
	}
	if span.End() < 0 {
		t.Fatal("negative duration")
	}

	existing := NewTrace()
	ctx = WithTrace(context.Background(), existing)
	_, span = StartSpan(ctx, "child")
	if span.Trace.TraceID != existing.TraceID {
		t.Fatalf("trace = %+v, want %+v", span.Trace, existing)
	}
}
