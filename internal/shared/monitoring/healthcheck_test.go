package monitoring

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLivenessAndReadiness(t *testing.T) {
	checker := NewHealthChecker("1.2.3")
	liveness := checker.Liveness()
	if liveness.Status != "ok" || liveness.Version != "1.2.3" || liveness.Timestamp.IsZero() {
		t.Fatalf("liveness = %+v", liveness)
	}

	checker.AddCheck(Check{Name: "ok", Fn: func(context.Context) error { return nil }})
	checker.AddCheck(Check{Name: "bad", Fn: func(context.Context) error { return errors.New("down") }})
	checker.AddCheck(Check{Name: "timeout", Timeout: 5 * time.Millisecond, Fn: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}})

	report := checker.Readiness(context.Background())
	if report.Status != "degraded" {
		t.Fatalf("status = %s", report.Status)
	}
	if len(report.Checks) != 3 {
		t.Fatalf("checks = %+v", report.Checks)
	}
	if report.Checks["ok"].Status != "ok" {
		t.Fatalf("ok check = %+v", report.Checks["ok"])
	}
	if report.Checks["bad"].Status != "error" || report.Checks["bad"].Error != "down" {
		t.Fatalf("bad check = %+v", report.Checks["bad"])
	}
	if report.Checks["timeout"].Status != "error" {
		t.Fatalf("timeout check = %+v", report.Checks["timeout"])
	}
}
