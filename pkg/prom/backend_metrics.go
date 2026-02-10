package prom

import (
	"github.com/prometheus/client_golang/prometheus"
)

type BackendMetrics struct {
	Handshake *prometheus.GaugeVec
	Health    *prometheus.GaugeVec
}

func NewBackendMetrics(namespace, subsystem string) *BackendMetrics {
	return &BackendMetrics{
		Handshake: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "backend_handshake_seconds",
				Help:      "Backend TCP/TLS handshake duration (seconds)",
			},
			[]string{"service", "backend", "datacenter"},
		),

		Health: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "backend_health_status",
				Help:      "Backend health (1=healthy, 0=sick)",
			},
			[]string{"service", "backend", "datacenter"},
		),
	}
}

func (m *BackendMetrics) Gatherer() prometheus.Gatherer {
	reg := prometheus.NewRegistry()
	reg.MustRegister(m.Handshake)
	reg.MustRegister(m.Health)
	return reg
}
