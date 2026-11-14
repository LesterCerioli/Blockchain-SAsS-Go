package monitoring

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

var defaultBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

type series struct {
	labels      map[string]string
	value       float64
	bucketCount []uint64
	sum         float64
	count       uint64
}

type metricFamily struct {
	name    string
	help    string
	typ     string
	buckets []float64
	series  map[string]*series
}

type Registry struct {
	mu       sync.RWMutex
	families map[string]*metricFamily
}

func NewRegistry() *Registry {
	return &Registry{families: make(map[string]*metricFamily)}
}

func (r *Registry) getOrCreate(name, help, typ string, buckets []float64) *metricFamily {
	f, ok := r.families[name]
	if !ok {
		f = &metricFamily{name: name, help: help, typ: typ, series: make(map[string]*series), buckets: buckets}
		r.families[name] = f
	}
	return f
}

func labelKey(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(labels[k])
	}
	return b.String()
}

func cloneLabels(labels map[string]string) map[string]string {
	out := make(map[string]string, len(labels))
	for k, v := range labels {
		out[k] = v
	}
	return out
}

func (r *Registry) IncCounter(name, help string, labels map[string]string) {
	r.AddCounter(name, help, labels, 1)
}

func (r *Registry) AddCounter(name, help string, labels map[string]string, delta float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f := r.getOrCreate(name, help, "counter", nil)
	key := labelKey(labels)
	s, ok := f.series[key]
	if !ok {
		s = &series{labels: cloneLabels(labels)}
		f.series[key] = s
	}
	s.value += delta
}

func (r *Registry) SetGauge(name, help string, labels map[string]string, value float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f := r.getOrCreate(name, help, "gauge", nil)
	key := labelKey(labels)
	s, ok := f.series[key]
	if !ok {
		s = &series{labels: cloneLabels(labels)}
		f.series[key] = s
	}
	s.value = value
}

func (r *Registry) ObserveHistogram(name, help string, labels map[string]string, value float64, buckets []float64) {
	if len(buckets) == 0 {
		buckets = defaultBuckets
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	f := r.getOrCreate(name, help, "histogram", buckets)
	key := labelKey(labels)
	s, ok := f.series[key]
	if !ok {
		s = &series{labels: cloneLabels(labels), bucketCount: make([]uint64, len(f.buckets))}
		f.series[key] = s
	}
	s.sum += value
	s.count++
	for i, b := range f.buckets {
		if value <= b {
			s.bucketCount[i]++
		}
	}
}

func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(r.Render()))
	})
}

func (r *Registry) Render() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.families))
	for name := range r.families {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	for _, name := range names {
		f := r.families[name]
		fmt.Fprintf(&b, "# HELP %s %s\n", f.name, f.help)
		fmt.Fprintf(&b, "# TYPE %s %s\n", f.name, f.typ)

		keys := make([]string, 0, len(f.series))
		for key := range f.series {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for _, key := range keys {
			s := f.series[key]
			switch f.typ {
			case "histogram":
				for i, bucket := range f.buckets {
					fmt.Fprintf(&b, "%s_bucket%s %d\n", f.name, renderLabels(s.labels, "le", fmt.Sprintf("%g", bucket)), s.bucketCount[i])
				}
				fmt.Fprintf(&b, "%s_bucket%s %d\n", f.name, renderLabels(s.labels, "le", "+Inf"), s.count)
				fmt.Fprintf(&b, "%s_sum%s %g\n", f.name, renderLabels(s.labels, "", ""), s.sum)
				fmt.Fprintf(&b, "%s_count%s %d\n", f.name, renderLabels(s.labels, "", ""), s.count)
			default:
				fmt.Fprintf(&b, "%s%s %g\n", f.name, renderLabels(s.labels, "", ""), s.value)
			}
		}
	}
	return b.String()
}

func renderLabels(labels map[string]string, extraKey, extraValue string) string {
	if len(labels) == 0 && extraKey == "" {
		return ""
	}
	keys := make([]string, 0, len(labels)+1)
	for k := range labels {
		keys = append(keys, k)
	}
	if extraKey != "" {
		keys = append(keys, extraKey)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		v := labels[k]
		if k == extraKey {
			v = extraValue
		}
		fmt.Fprintf(&b, "%s=%q", k, v)
	}
	b.WriteByte('}')
	return b.String()
}

type Timer struct {
	start time.Time
}

func StartTimer() *Timer {
	return &Timer{start: time.Now()}
}

func (t *Timer) Seconds() float64 {
	return time.Since(t.start).Seconds()
}
