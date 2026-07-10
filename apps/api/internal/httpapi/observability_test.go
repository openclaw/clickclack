package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestOperationalEndpointsExposeSafeCorrelatedMetadata(t *testing.T) {
	st := newEmptyHTTPStore(t)
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{
		DisableDevAuth: true,
		MetricsEnabled: true,
		Environment:    "fakeco",
		Version:        "test-version",
		Commit:         "abc123",
	}).Handler())
	t.Cleanup(server.Close)

	req, err := http.NewRequest(http.MethodGet, server.URL+"/healthz?prompt=do-not-export", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(correlationIDHeader, "fakeco-observe-1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var health map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || health["status"] != "ok" || resp.Header.Get(correlationIDHeader) != "fakeco-observe-1" {
		t.Fatalf("unexpected health response: status=%s body=%#v correlation=%q", resp.Status, health, resp.Header.Get(correlationIDHeader))
	}
	dynamicResp, err := http.Get(server.URL + "/api/channels/chn_private/messages?prompt=private-content")
	if err != nil {
		t.Fatal(err)
	}
	dynamicResp.Body.Close()
	if dynamicResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("dynamic route status = %s", dynamicResp.Status)
	}
	otherReq, err := http.NewRequest("FAKECO-CUSTOM-METHOD", server.URL+"/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}
	otherResp, err := http.DefaultClient.Do(otherReq)
	if err != nil {
		t.Fatal(err)
	}
	otherResp.Body.Close()

	readyResp, err := http.Get(server.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	readyBody, err := io.ReadAll(readyResp.Body)
	readyResp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if readyResp.StatusCode != http.StatusOK || !strings.Contains(string(readyBody), `"status":"ready"`) {
		t.Fatalf("unexpected readiness response: %s %s", readyResp.Status, readyBody)
	}

	metricsResp, err := http.Get(server.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	metrics, err := io.ReadAll(metricsResp.Body)
	metricsResp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	text := string(metrics)
	for _, expected := range []string{
		`clickclack_build_info{environment="fakeco",version="test-version",commit="abc123"} 1`,
		"clickclack_ready 1",
		`route="/healthz"`,
		`route="/readyz"`,
		`route="/api/channels/{channel_id}/messages"`,
		`method="OTHER"`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("metrics missing %q:\n%s", expected, text)
		}
	}
	if strings.Contains(text, "do-not-export") || strings.Contains(text, "private-content") || strings.Contains(text, "chn_private") || strings.Contains(text, "prompt") {
		t.Fatalf("metrics exported query content:\n%s", text)
	}

	badReq, err := http.NewRequest(http.MethodGet, server.URL+"/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}
	badReq.Header.Set(correlationIDHeader, "unsafe correlation")
	badResp, err := http.DefaultClient.Do(badReq)
	if err != nil {
		t.Fatal(err)
	}
	badResp.Body.Close()
	generated := badResp.Header.Get(correlationIDHeader)
	if generated == "unsafe correlation" || !validCorrelationID(generated) {
		t.Fatalf("expected a generated safe correlation ID, got %q", generated)
	}
}

func TestMetricsDisabledAndReadinessFailure(t *testing.T) {
	base := newEmptyHTTPStore(t)
	server := httptest.NewServer(New(failingPingStore{Store: base}, realtime.NewHub(), Options{DisableDevAuth: true}).Handler())
	t.Cleanup(server.Close)

	readyResp, err := http.Get(server.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	readyResp.Body.Close()
	if readyResp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("readiness status = %s", readyResp.Status)
	}
	metricsResp, err := http.Get(server.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	metricsResp.Body.Close()
	if metricsResp.StatusCode != http.StatusNotFound {
		t.Fatalf("disabled metrics status = %s", metricsResp.Status)
	}
}

func TestRequestLoggerIncludesOnlySafeCorrelationMetadata(t *testing.T) {
	var logs bytes.Buffer
	formatter := &pathOnlyLogFormatter{Logger: log.New(&logs, "", 0)}
	router := chi.NewRouter()
	router.Use(correlationIDMiddleware)
	router.Use(middleware.RequestLogger(formatter))
	router.Get("/api/channels/{channel_id}/messages", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "https://example.test/api/channels/chn_private/messages?token=secret", nil)
	req.Header.Set(correlationIDHeader, "fakeco-log-1")
	router.ServeHTTP(httptest.NewRecorder(), req)
	got := logs.String()
	if !strings.Contains(got, `correlation_id="fakeco-log-1"`) || !strings.Contains(got, `route="/api/channels/{channel_id}/messages"`) {
		t.Fatalf("missing safe request metadata: %q", got)
	}
	for _, forbidden := range []string{"secret", "token" + "=", "?", "chn_private"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("logger leaked %q in request-specific content: %q", forbidden, got)
		}
	}
}

type failingPingStore struct {
	store.Store
}

func (failingPingStore) Ping(context.Context) error { return errors.New("database unavailable") }
