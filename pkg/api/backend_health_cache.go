package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
)

type BackendHealthCache struct {
	client      HTTPClient
	services    *ServiceCache
	endpoints   map[string]string
	healthGauge *prometheus.GaugeVec
	logger      log.Logger
	enabled     bool
}

func NewBackendHealthCache(client HTTPClient, services *ServiceCache, endpoints map[string]string, healthGauge *prometheus.GaugeVec, logger log.Logger, enabled bool) *BackendHealthCache {
	c := &BackendHealthCache{
		client:      client,
		services:    services,
		endpoints:   endpoints,
		healthGauge: healthGauge,
		logger:      logger,
		enabled:     enabled,
	}
	if len(endpoints) == 0 || healthGauge == nil {
		c.enabled = false
	}
	if c.logger == nil {
		c.logger = log.NewNopLogger()
	}
	return c
}

func (c *BackendHealthCache) Enabled() bool { return c.enabled }

func (c *BackendHealthCache) Refresh(ctx context.Context) error {
	if !c.enabled {
		return nil
	}

	begin := time.Now()
	c.healthGauge.Reset()

	var firstErr error
	for serviceID, url := range c.endpoints {
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			level.Warn(c.logger).Log("during", "backend health request build", "service_id", serviceID, "err", err)
			continue
		}

		req.Header.Set("Accept", "application/json")

		resp, err := c.client.Do(req)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			level.Warn(c.logger).Log("during", "backend health request", "service_id", serviceID, "err", err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			if firstErr == nil {
				firstErr = fmt.Errorf("backend health status %d", resp.StatusCode)
			}
			level.Warn(c.logger).Log("during", "backend health response", "service_id", serviceID, "status", resp.StatusCode)
			continue
		}

		datacenter := datacenterFromServedBy(resp.Header.Get("X-Served-By"))
		backends, err := decodeBackendHealth(resp.Body)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			level.Warn(c.logger).Log("during", "backend health decode", "service_id", serviceID, "err", err)
			continue
		}

		serviceLabel := serviceID
		if name, _, ok := c.services.Metadata(serviceID); ok && name != "" {
			serviceLabel = name
		}

		for backend, healthy := range backends {
			if backend == "" {
				continue
			}
			if healthy {
				c.healthGauge.WithLabelValues(serviceLabel, backend, datacenter).Set(1)
			} else {
				c.healthGauge.WithLabelValues(serviceLabel, backend, datacenter).Set(0)
			}
		}
	}

	level.Debug(c.logger).Log("refresh_took", time.Since(begin), "endpoints", len(c.endpoints))
	return firstErr
}

func datacenterFromServedBy(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "unknown"
	}

	// Typically: "cache-ewr-kewr1740075, cache-ewr-kewr1740022"
	if i := strings.Index(v, ","); i >= 0 {
		v = v[:i]
	}
	v = strings.TrimSpace(v)

	const prefix = "cache-"
	if !strings.HasPrefix(v, prefix) {
		return "unknown"
	}

	rest := strings.TrimPrefix(v, prefix)
	if j := strings.Index(rest, "-"); j > 0 {
		return strings.ToUpper(rest[:j])
	}

	if len(rest) >= 3 {
		return strings.ToUpper(rest[:3])
	}

	return "unknown"
}

func decodeBackendHealth(r io.Reader) (map[string]bool, error) {
	// Supported shapes:
	// 1) {"backends":[{"name":"origin","health":"healthy"}]}
	// 2) {"backends":[{"name":"origin","healthy":true}]}
	// 3) {"origin":"healthy","origin2":"unhealthy"}
	// 4) {"origin":true,"origin2":false}

	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	// Shape 1/2
	{
		var payload struct {
			Backends []struct {
				Name    string `json:"name"`
				Health  string `json:"health"`
				Status  string `json:"status"`
				Healthy *bool  `json:"healthy"`
			} `json:"backends"`
		}
		if err := json.Unmarshal(b, &payload); err == nil && len(payload.Backends) > 0 {
			out := map[string]bool{}
			for _, be := range payload.Backends {
				name := strings.TrimSpace(be.Name)
				if name == "" {
					continue
				}
				if be.Healthy != nil {
					out[name] = *be.Healthy
					continue
				}
				s := strings.ToLower(strings.TrimSpace(be.Health))
				if s == "" {
					s = strings.ToLower(strings.TrimSpace(be.Status))
				}
				switch s {
				case "healthy":
					out[name] = true
				case "unhealthy", "sick":
					out[name] = false
				default:
					// unknown -> skip
				}
			}
			return out, nil
		}
	}

	// Shape 3
	{
		var m map[string]string
		if err := json.Unmarshal(b, &m); err == nil && len(m) > 0 {
			out := map[string]bool{}
			for k, v := range m {
				name := strings.TrimSpace(k)
				if name == "" {
					continue
				}
				switch strings.ToLower(strings.TrimSpace(v)) {
				case "healthy":
					out[name] = true
				case "unhealthy", "sick":
					out[name] = false
				default:
					// unknown -> skip
				}
			}
			return out, nil
		}
	}

	// Shape 4
	{
		var m map[string]bool
		if err := json.Unmarshal(b, &m); err == nil && len(m) > 0 {
			out := map[string]bool{}
			for k, v := range m {
				name := strings.TrimSpace(k)
				if name == "" {
					continue
				}
				out[name] = v
			}
			return out, nil
		}
	}

	return nil, fmt.Errorf("unsupported backend health JSON shape")
}
