package traefikstats

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCookieSecondVisit(t *testing.T) {
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer sidecar.Close()

	cfg := CreateConfig()
	cfg.SidecarURL = sidecar.URL
	cfg.FlushInterval = "10ms"

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("ok"))
	})

	handler, err := New(context.Background(), next, cfg, "test")
	if err != nil {
		t.Fatalf("new middleware failed: %v", err)
	}
	m := handler.(*statsMiddleware)
	defer m.Close()

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.AddCookie(&http.Cookie{Name: cfg.CookieName, Value: "?test-uuid"})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	setCookie := rr.Header().Get("Set-Cookie")
	if !strings.Contains(setCookie, "stats_id=test-uuid") {
		t.Fatalf("expected updated cookie, got %q", setCookie)
	}
}

func TestIngestEventPosted(t *testing.T) {
	events := make(chan ingestRequest, 1)
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ingest" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var req ingestRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			events <- req
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer sidecar.Close()

	cfg := CreateConfig()
	cfg.SidecarURL = sidecar.URL
	cfg.FlushInterval = "5ms"

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})

	handler, err := New(context.Background(), next, cfg, "test")
	if err != nil {
		t.Fatalf("new middleware failed: %v", err)
	}
	m := handler.(*statsMiddleware)
	defer m.Close()

	req := httptest.NewRequest(http.MethodGet, "http://example.com/hello", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	select {
	case ingested := <-events:
		if len(ingested.Events) == 0 {
			t.Fatalf("expected events, got none")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("expected ingest call")
	}
}
