// Package metrics exposes Prometheus collectors and HTTP instrumentation.
package metrics

import (
	"bufio"
	"errors"
	"net"
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
	return MiddlewareDynamic(func(*http.Request) string { return route }, next)
}

// MiddlewareDynamic is like Middleware, but resolves the route label by
// calling routeFn *after* next has served the request. This is needed when the
// label depends on state set during routing — e.g. an http.ServeMux populates
// r.Pattern (the matched, low-cardinality route template such as
// "POST /v1/users/{id}/disable") only as a side effect of dispatching the
// request, not before. next and routeFn observe the same *http.Request, so
// reading it back afterward is safe.
func MiddlewareDynamic(routeFn func(*http.Request) string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next(sw, r)
		route := routeFn(r)
		httpRequests.WithLabelValues(r.Method, route, strconv.Itoa(sw.status)).Inc()
		// Signaling is a long-lived WebSocket upgrade, not a request/response; its
		// "duration" is the connection lifetime and would blow out the latency
		// histogram's buckets. Still counted above (useful as a connection-open
		// counter), just excluded from the latency distribution.
		if route == signalingRoute {
			return
		}
		httpDuration.WithLabelValues(route).Observe(time.Since(start).Seconds())
	}
}

// signalingRoute is the registered pattern for the signaling WebSocket upgrade,
// excluded from the latency histogram (see MiddlewareDynamic).
const signalingRoute = "GET /v1/signaling"

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

// Hijack lets WebSocket upgrades (e.g. /v1/signaling) take over the connection
// through this instrumentation wrapper.
func (s *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := s.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("underlying ResponseWriter does not support hijacking")
	}
	return hj.Hijack()
}
