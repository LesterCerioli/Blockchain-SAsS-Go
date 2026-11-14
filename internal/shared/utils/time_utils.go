package utils

import (
	"strconv"
	"strings"
	"time"
)

func ParseDurationOrDefault(value string, fallback time.Duration) time.Duration {
	if value == "" {
		return fallback
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return d
}

func FormatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func BoolFromEnv(value string, fallback bool) bool {
	if value == "" {
		return fallback
	}
	b, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return fallback
	}
	return b
}

func SplitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
