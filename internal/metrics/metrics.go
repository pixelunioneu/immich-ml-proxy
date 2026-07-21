// Package metrics defines the Prometheus metrics exposed by the proxy at
// /metrics.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// RequestsTotal counts proxied requests by backend and routing reason.
	RequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "immich_ml_proxy_requests_total",
		Help: "Total requests proxied, labeled by chosen backend and routing reason.",
	}, []string{"backend", "reason"})

	// RouteFallbackTotal counts requests where routing fell back to the
	// default backend due to ambiguity (non-/predict path, empty/malformed
	// body) rather than a confident routing decision.
	RouteFallbackTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "immich_ml_proxy_route_fallback_total",
		Help: "Total requests that fell back to the default backend due to routing ambiguity, labeled by reason.",
	}, []string{"reason"})

	// UpstreamErrorsTotal counts failed attempts to reach a backend.
	UpstreamErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "immich_ml_proxy_upstream_errors_total",
		Help: "Total requests that failed to reach the chosen backend.",
	}, []string{"backend"})

	// RequestDuration observes end-to-end proxy latency by backend.
	RequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "immich_ml_proxy_request_duration_seconds",
		Help:    "Time to proxy a request to its backend and stream the response back.",
		Buckets: prometheus.ExponentialBuckets(0.01, 2, 14), // 10ms .. ~82s
	}, []string{"backend"})
)
