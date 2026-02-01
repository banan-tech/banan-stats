package traefikstats

import (
	"bufio"
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
	cfg.FlushInterval = "1h"

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
	events := make(chan string, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		if r.Body != nil {
			defer r.Body.Close()
		}
		scanner := bufio.NewScanner(r.Body)
		for scanner.Scan() {
			var evt event
			if err := json.Unmarshal(scanner.Bytes(), &evt); err != nil {
				continue
			}
			if evt.Path == "" {
				continue
			}
			select {
			case events <- evt.Path:
			default:
			}
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	cfg := CreateConfig()
	cfg.SidecarURL = server.URL
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
	case path := <-events:
		if path != "/hello" {
			t.Fatalf("expected path /hello, got %q", path)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("expected ingest call")
	}
}
