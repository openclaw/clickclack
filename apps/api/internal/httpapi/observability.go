package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/openclaw/clickclack/apps/api/internal/requestmeta"
)

const correlationIDHeader = "X-Correlation-ID"

func correlationIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		correlationID := strings.TrimSpace(r.Header.Get(correlationIDHeader))
		if !validCorrelationID(correlationID) {
			correlationID = newCorrelationID()
		}
		w.Header().Set(correlationIDHeader, correlationID)
		ctx := requestmeta.WithCorrelationID(r.Context(), correlationID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func validCorrelationID(value string) bool {
	return requestmeta.ValidCorrelationID(value)
}

func newCorrelationID() string {
	var body [16]byte
	if _, err := rand.Read(body[:]); err == nil {
		return "corr_" + hex.EncodeToString(body[:])
	}
	return fmt.Sprintf("corr_%d", time.Now().UnixNano())
}

func correlationIDFromContext(ctx context.Context) string {
	return requestmeta.CorrelationID(ctx)
}

type buildMetadata struct {
	Environment string
	Version     string
	Commit      string
}

type metricKey struct {
	Method      string
	Route       string
	StatusClass string
}

type metricValue struct {
	Count           uint64
	DurationSeconds float64
}

type metricsRegistry struct {
	mu                sync.Mutex
	values            map[metricKey]metricValue
	githubOAuthEvents map[string]uint64
	ready             bool
	readyMu           sync.RWMutex
}

func newMetricsRegistry() *metricsRegistry {
	return &metricsRegistry{
		values:            make(map[metricKey]metricValue),
		githubOAuthEvents: make(map[string]uint64),
	}
}

func (m *metricsRegistry) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "unmatched"
		}
		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}
		key := metricKey{Method: metricMethod(r.Method), Route: route, StatusClass: strconv.Itoa(status/100) + "xx"}
		m.mu.Lock()
		value := m.values[key]
		value.Count++
		value.DurationSeconds += time.Since(started).Seconds()
		m.values[key] = value
		m.mu.Unlock()
	})
}

func (m *metricsRegistry) setReady(ready bool) {
	m.readyMu.Lock()
	m.ready = ready
	m.readyMu.Unlock()
}

func (m *metricsRegistry) readiness() bool {
	m.readyMu.RLock()
	defer m.readyMu.RUnlock()
	return m.ready
}

func (s *Server) recordGitHubOAuthEvent(event string) {
	if s.metrics == nil {
		return
	}
	s.metrics.mu.Lock()
	s.metrics.githubOAuthEvents[event]++
	s.metrics.mu.Unlock()
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		if s.metrics != nil {
			s.metrics.setReady(false)
		}
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	if s.metrics != nil {
		s.metrics.setReady(true)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) metricsHandler(w http.ResponseWriter, r *http.Request) {
	if s.metrics == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = fmt.Fprint(w, s.metrics.render(s.build))
}

func (m *metricsRegistry) render(build buildMetadata) string {
	m.mu.Lock()
	keys := make([]metricKey, 0, len(m.values))
	values := make(map[metricKey]metricValue, len(m.values))
	for key, value := range m.values {
		keys = append(keys, key)
		values[key] = value
	}
	oauthEvents := make([]string, 0, len(m.githubOAuthEvents))
	oauthEventValues := make(map[string]uint64, len(m.githubOAuthEvents))
	for event, value := range m.githubOAuthEvents {
		oauthEvents = append(oauthEvents, event)
		oauthEventValues[event] = value
	}
	m.mu.Unlock()
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Method != keys[j].Method {
			return keys[i].Method < keys[j].Method
		}
		if keys[i].Route != keys[j].Route {
			return keys[i].Route < keys[j].Route
		}
		return keys[i].StatusClass < keys[j].StatusClass
	})
	sort.Strings(oauthEvents)
	var out strings.Builder
	out.WriteString("# HELP clickclack_build_info ClickClack build metadata.\n")
	out.WriteString("# TYPE clickclack_build_info gauge\n")
	fmt.Fprintf(&out, "clickclack_build_info{environment=\"%s\",version=\"%s\",commit=\"%s\"} 1\n", metricLabel(build.Environment), metricLabel(build.Version), metricLabel(build.Commit))
	out.WriteString("# HELP clickclack_ready Whether the database readiness probe last succeeded.\n")
	out.WriteString("# TYPE clickclack_ready gauge\n")
	if m.readiness() {
		out.WriteString("clickclack_ready 1\n")
	} else {
		out.WriteString("clickclack_ready 0\n")
	}
	out.WriteString("# HELP clickclack_http_requests_total HTTP requests by method, route pattern, and status class.\n")
	out.WriteString("# TYPE clickclack_http_requests_total counter\n")
	out.WriteString("# HELP clickclack_http_request_duration_seconds HTTP request duration by method, route pattern, and status class.\n")
	out.WriteString("# TYPE clickclack_http_request_duration_seconds summary\n")
	for _, key := range keys {
		value := values[key]
		labels := fmt.Sprintf("method=\"%s\",route=\"%s\",status_class=\"%s\"", metricLabel(key.Method), metricLabel(key.Route), metricLabel(key.StatusClass))
		fmt.Fprintf(&out, "clickclack_http_requests_total{%s} %d\n", labels, value.Count)
		fmt.Fprintf(&out, "clickclack_http_request_duration_seconds_sum{%s} %g\n", labels, value.DurationSeconds)
		fmt.Fprintf(&out, "clickclack_http_request_duration_seconds_count{%s} %d\n", labels, value.Count)
	}
	out.WriteString("# HELP clickclack_github_oauth_events_total GitHub OAuth lifecycle events by bounded event category.\n")
	out.WriteString("# TYPE clickclack_github_oauth_events_total counter\n")
	for _, event := range oauthEvents {
		fmt.Fprintf(&out, "clickclack_github_oauth_events_total{event=\"%s\"} %d\n", metricLabel(event), oauthEventValues[event])
	}
	return out.String()
}

func metricLabel(value string) string {
	value = strings.Map(func(r rune) rune {
		if (r < 0x20 && r != '\n') || r == 0x7f {
			return '_'
		}
		return r
	}, value)
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\n", "\\n")
	return strings.ReplaceAll(value, "\"", "\\\"")
}

func metricMethod(method string) string {
	switch strings.ToUpper(method) {
	case http.MethodConnect, http.MethodDelete, http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPatch, http.MethodPost, http.MethodPut, http.MethodTrace:
		return strings.ToUpper(method)
	default:
		return "OTHER"
	}
}
