package monitoring

import (
	"context"
	"sort"
	"time"
)

type Check struct {
	Name    string
	Timeout time.Duration
	Fn      func(ctx context.Context) error
}

type CheckResult struct {
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
	Latency string `json:"latency"`
}

type HealthReport struct {
	Status    string                 `json:"status"`
	Version   string                 `json:"version"`
	Uptime    string                 `json:"uptime"`
	Timestamp time.Time              `json:"timestamp"`
	Checks    map[string]CheckResult `json:"checks,omitempty"`
}

type HealthChecker struct {
	version   string
	startedAt time.Time
	checks    []Check
}

func NewHealthChecker(version string) *HealthChecker {
	return &HealthChecker{version: version, startedAt: time.Now().UTC()}
}

func (h *HealthChecker) AddCheck(check Check) {
	if check.Timeout <= 0 {
		check.Timeout = 2 * time.Second
	}
	h.checks = append(h.checks, check)
}

func (h *HealthChecker) Liveness() HealthReport {
	return HealthReport{
		Status:    "ok",
		Version:   h.version,
		Uptime:    time.Since(h.startedAt).Round(time.Second).String(),
		Timestamp: time.Now().UTC(),
	}
}

func (h *HealthChecker) Readiness(ctx context.Context) HealthReport {
	report := h.Liveness()
	report.Checks = make(map[string]CheckResult, len(h.checks))
	overall := "ok"

	checks := make([]Check, len(h.checks))
	copy(checks, h.checks)
	sort.Slice(checks, func(i, j int) bool { return checks[i].Name < checks[j].Name })

	for _, check := range checks {
		cctx, cancel := context.WithTimeout(ctx, check.Timeout)
		start := time.Now()
		err := check.Fn(cctx)
		cancel()
		result := CheckResult{Status: "ok", Latency: time.Since(start).Round(time.Millisecond).String()}
		if err != nil {
			result.Status = "error"
			result.Error = err.Error()
			overall = "degraded"
		}
		report.Checks[check.Name] = result
	}
	report.Status = overall
	return report
}
