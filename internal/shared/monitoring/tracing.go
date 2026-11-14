package monitoring

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

type traceKey struct{}
type spanKey struct{}

type Trace struct {
	TraceID string
	SpanID  string
	Sampled bool
}

func NewTrace() Trace {
	return Trace{TraceID: randomHex(16), SpanID: randomHex(8), Sampled: true}
}

func ParseTraceparent(value string) Trace {
	parts := strings.Split(value, "-")
	if len(parts) != 4 || len(parts[1]) != 32 || len(parts[2]) != 16 {
		return NewTrace()
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return NewTrace()
	}
	if _, err := hex.DecodeString(parts[2]); err != nil {
		return NewTrace()
	}
	sampled := strings.HasSuffix(parts[3], "01")
	return Trace{TraceID: parts[1], SpanID: randomHex(8), Sampled: sampled}
}

func WithTrace(ctx context.Context, trace Trace) context.Context {
	return context.WithValue(ctx, traceKey{}, trace)
}

func TraceFromContext(ctx context.Context) (Trace, bool) {
	if ctx == nil {
		return Trace{}, false
	}
	t, ok := ctx.Value(traceKey{}).(Trace)
	return t, ok
}

func TraceID(ctx context.Context) string {
	t, ok := TraceFromContext(ctx)
	if !ok {
		return ""
	}
	return t.TraceID
}

func Traceparent(ctx context.Context) string {
	t, ok := TraceFromContext(ctx)
	if !ok {
		return ""
	}
	flags := "00"
	if t.Sampled {
		flags = "01"
	}
	return fmt.Sprintf("00-%s-%s-%s", t.TraceID, t.SpanID, flags)
}

type Span struct {
	Name      string
	Trace     Trace
	StartedAt time.Time
}

func StartSpan(ctx context.Context, name string) (context.Context, *Span) {
	trace, ok := TraceFromContext(ctx)
	if !ok {
		trace = NewTrace()
		ctx = WithTrace(ctx, trace)
		ctx = context.WithValue(ctx, spanKey{}, trace.SpanID)
	}
	span := &Span{Name: name, Trace: trace, StartedAt: time.Now()}
	return ctx, span
}

func (s *Span) End() time.Duration {
	return time.Since(s.StartedAt)
}

func randomHex(bytes int) string {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return strings.Repeat("0", bytes*2)
	}
	return hex.EncodeToString(buf)
}
