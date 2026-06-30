// Package metrics exposes Prometheus collectors and HTTP instrumentation.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	httpRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ar_http_requests_total",
			Help: "Total HTTP requests by method, route, and status.",
		},
		[]string{"method", "route", "status"},
	)

	httpDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ar_http_request_duration_seconds",
			Help:    "HTTP request latency by route.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"route"},
	)

	// SignalingRooms tracks the number of active signaling rooms.
	SignalingRooms = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "ar_signaling_rooms",
		Help: "Number of active signaling rooms.",
	})
)

func init() {
	prometheus.MustRegister(httpRequests, httpDuration, SignalingRooms)
}

// Handler returns the Prometheus scrape handler for /metrics.
func Handler() http.Handler { return promhttp.Handler() }

// Middleware records request count and latency. The route label should be a
// low-cardinality template (the registered pattern), not the raw path.
func Middleware(route string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next(sw, r)
		httpRequests.WithLabelValues(r.Method, route, strconv.Itoa(sw.status)).Inc()
		httpDuration.WithLabelValues(route).Observe(time.Since(start).Seconds())
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wroteHeader {
		s.status = code
		s.wroteHeader = true
	}
	s.ResponseWriter.WriteHeader(code)
}
