package traefikstats

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCookieSecondVisit(t *testing.T) {
	cfg := CreateConfig()
	cfg.SidecarURL = "http://example.com"
	cfg.FlushInterval = "1h"
	cfg.BufferPath = filepath.Join(t.TempDir(), "buffer.sqlite")

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("ok"))
	})

	handler, err := New(context.Background(), next, cfg, "test")
	if err != nil {
		t.Fatalf("new middleware failed: %v", err)
	}
	m := handler.(*statsMiddleware)
	m.streamClient.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return newResponse(http.StatusAccepted), nil
	})
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

	cfg := CreateConfig()
	cfg.SidecarURL = "http://example.com"
	cfg.FlushInterval = "5ms"
	cfg.BufferPath = filepath.Join(t.TempDir(), "buffer.sqlite")

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})

	handler, err := New(context.Background(), next, cfg, "test")
	if err != nil {
		t.Fatalf("new middleware failed: %v", err)
	}
	m := handler.(*statsMiddleware)
	m.streamClient.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
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
		return newResponse(http.StatusAccepted), nil
	})
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newResponse(status int) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("")),
	}
}
