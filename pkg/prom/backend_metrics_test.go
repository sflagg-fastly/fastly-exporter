package prom

import "testing"

func TestBackendMetricsExposeFamilies(t *testing.T) {
	m := NewBackendMetrics("fastly", "rt")

	m.Handshake.WithLabelValues("unknown", "unknown", "aggregate").Set(0)
	m.Health.WithLabelValues("unknown", "unknown", "aggregate").Set(0)

	g := m.Gatherer()
	mfs, err := g.Gather()
	if err != nil {
		t.Fatal(err)
	}

	seen := map[string]bool{}
	for _, mf := range mfs {
		seen[mf.GetName()] = true
	}

	if !seen["fastly_rt_backend_handshake_seconds"] {
		t.Fatalf("missing fastly_rt_backend_handshake_seconds")
	}
	if !seen["fastly_rt_backend_health_status"] {
		t.Fatalf("missing fastly_rt_backend_health_status")
	}
}
