package observability

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Counters
	HTTPRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total HTTP requests",
	}, []string{"method", "path", "status"})

	WSConnectionsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ws_connections_total",
		Help: "Total WebSocket connections established",
	})

	MatchStartedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "match_started_total",
		Help: "Total matches started",
	})

	MatchEndedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "match_ended_total",
		Help: "Total matches ended",
	}, []string{"result"}) // "normal", "no_contest"

	MatchPanicTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "match_panic_total",
		Help: "Total match panics recovered",
	})

	// Gauges
	ActiveConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "active_ws_connections",
		Help: "Current active WebSocket connections",
	})

	ActiveMatches = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "active_matches",
		Help: "Current active matches",
	})

	ActiveRooms = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "active_rooms",
		Help: "Current active rooms",
	})

	// Histograms
	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request duration in seconds",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})
)

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) StatusCode() int {
	return rw.statusCode
}

// MetricsMiddleware records HTTP request counts and durations.
func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := newResponseWriter(w)
		next.ServeHTTP(wrapped, r)
		duration := time.Since(start).Seconds()

		HTTPRequestsTotal.WithLabelValues(r.Method, r.URL.Path, strconv.Itoa(wrapped.StatusCode())).Inc()
		HTTPRequestDuration.WithLabelValues(r.Method, r.URL.Path).Observe(duration)
	})
}
