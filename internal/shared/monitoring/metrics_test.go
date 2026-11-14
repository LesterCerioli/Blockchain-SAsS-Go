package monitoring

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRegistryMetrics(t *testing.T) {
	registry := NewRegistry()
	registry.IncCounter("requests_total", "Total requests", map[string]string{"method": "GET", "path": "/"})
	registry.AddCounter("requests_total", "Total requests", map[string]string{"method": "GET", "path": "/"}, 2)
	registry.SetGauge("payments_open", "Open payments", nil, 4)
	registry.ObserveHistogram("latency_seconds", "Latency", map[string]string{"route": "/payments"}, 0.02, nil)
	registry.ObserveHistogram("latency_seconds", "Latency", map[string]string{"route": "/payments"}, 0.2, []float64{0.1, 0.5})

	rendered := registry.Render()
	for _, want := range []string{
		"# HELP requests_total Total requests",
		"# TYPE requests_total counter",
		`requests_total{method="GET",path="/"} 3`,
		"payments_open 4",
		"# TYPE latency_seconds histogram",
		"latency_seconds_bucket",
		"latency_seconds_sum",
		"latency_seconds_count",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("render missing %q:\n%s", want, rendered)
		}
	}

	recorder := httptest.NewRecorder()
	registry.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	if !strings.Contains(recorder.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("content type = %q", recorder.Header().Get("Content-Type"))
	}
}

func TestLabelsAndTimer(t *testing.T) {
	if got := labelKey(nil); got != "" {
		t.Fatalf("empty key = %q", got)
	}
	if got := labelKey(map[string]string{"b": "2", "a": "1"}); got != "a=1,b=2" {
		t.Fatalf("key = %q", got)
	}
	if got := renderLabels(nil, "", ""); got != "" {
		t.Fatalf("empty labels = %q", got)
	}
	if got := renderLabels(map[string]string{"a": "1"}, "le", "0.5"); got != `{a="1",le="0.5"}` {
		t.Fatalf("labels = %q", got)
	}

	timer := StartTimer()
	time.Sleep(time.Millisecond)
	if timer.Seconds() <= 0 {
		t.Fatal("expected positive duration")
	}
}
