package traefikstats

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type statsMiddleware struct {
	name          string
	next          http.Handler
	cfg           *Config
	client        *http.Client
	queue         chan event
	stop          chan struct{}
	flushInterval time.Duration
}

func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	if config == nil {
		return nil, errors.New("config is required")
	}
	if strings.TrimSpace(config.SidecarURL) == "" {
		return nil, errors.New("sidecarURL is required")
	}
	flushInterval, err := time.ParseDuration(config.FlushInterval)
	if err != nil {
		return nil, fmt.Errorf("invalid flushInterval: %w", err)
	}
	if config.QueueSize <= 0 {
		config.QueueSize = 1024
	}

	m := &statsMiddleware{
		name:          name,
		next:          next,
		cfg:           config,
		client:        &http.Client{Timeout: 5 * time.Second},
		queue:         make(chan event, config.QueueSize),
		stop:          make(chan struct{}),
		flushInterval: flushInterval,
	}
	go m.worker(ctx)
	return m, nil
}

func (m *statsMiddleware) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	if m.isDashboardRequest(req) {
		m.proxyDashboard(rw, req)
		return
	}

	rec := newResponseRecorder(rw)

	cookieState := m.readCookie(req)
	m.maybeSetCookie(rec.Header(), cookieState)
	m.next.ServeHTTP(rec, req)

	status := rec.statusCode()
	contentType := rec.Header().Get("Content-Type")

	if m.isLoggable(status, contentType) {
		m.enqueueEvent(req, contentType, cookieState)
	}

	rec.finalize()
}

func (m *statsMiddleware) Close() error {
	close(m.stop)
	return nil
}

func (m *statsMiddleware) isDashboardRequest(req *http.Request) bool {
	if m.cfg.DashboardPath == "" {
		return false
	}
	if req.URL.Path == m.cfg.DashboardPath {
		return true
	}
	return req.URL.Path == strings.TrimSuffix(m.cfg.DashboardPath, "/")+"/favicon.ico"
}

func (m *statsMiddleware) proxyDashboard(rw http.ResponseWriter, req *http.Request) {
	if m.cfg.DashboardToken != "" {
		auth := req.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || strings.TrimPrefix(auth, "Bearer ") != m.cfg.DashboardToken {
			rw.WriteHeader(http.StatusUnauthorized)
			_, _ = rw.Write([]byte("Unauthorized"))
			return
		}
	}

	target, err := url.Parse(m.cfg.SidecarURL)
	if err != nil {
		rw.WriteHeader(http.StatusBadGateway)
		return
	}
	target.Path = req.URL.Path
	target.RawQuery = req.URL.RawQuery

	outReq, err := http.NewRequestWithContext(req.Context(), http.MethodGet, target.String(), nil)
	if err != nil {
		rw.WriteHeader(http.StatusBadGateway)
		return
	}

	resp, err := m.client.Do(outReq)
	if err != nil {
		rw.WriteHeader(http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for k, vals := range resp.Header {
		for _, v := range vals {
			rw.Header().Add(k, v)
		}
	}
	rw.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(rw, resp.Body)
}

func (m *statsMiddleware) isLoggable(status int, contentType string) bool {
	if status != http.StatusOK {
		return false
	}
	ct := strings.ToLower(contentType)
	return strings.HasPrefix(ct, "text/html") ||
		strings.HasPrefix(ct, "application/atom+xml") ||
		strings.HasPrefix(ct, "application/rss+xml")
}

func (m *statsMiddleware) enqueueEvent(req *http.Request, contentType string, cookieState cookieState) {
	ip := req.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = req.RemoteAddr
	}
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}

	evt := event{
		Timestamp:   time.Now().UTC(),
		Host:        normalizeHost(req.Host),
		Path:        req.URL.Path,
		Query:       req.URL.RawQuery,
		IP:          ip,
		UserAgent:   req.Header.Get("User-Agent"),
		Referrer:    req.Header.Get("Referer"),
		ContentType: contentType,
		SetCookie:   cookieState.setCookie,
		Uniq:        cookieState.uniq,
		SecondVisit: cookieState.secondVisit,
	}

	select {
	case m.queue <- evt:
	default:
		log.Printf("[%s] stats queue full, dropping event", m.name)
	}
}

func (m *statsMiddleware) worker(ctx context.Context) {
	ticker := time.NewTicker(m.flushInterval)
	defer ticker.Stop()

	var buf []event
	for {
		select {
		case <-m.stop:
			return
		case <-ctx.Done():
			return
		case evt := <-m.queue:
			buf = append(buf, evt)
			if len(buf) >= 100 {
				m.flush(buf)
				buf = buf[:0]
			}
		case <-ticker.C:
			if len(buf) > 0 {
				m.flush(buf)
				buf = buf[:0]
			}
		}
	}
}

func (m *statsMiddleware) flush(events []event) {
	body, err := json.Marshal(ingestRequest{Events: events})
	if err != nil {
		log.Printf("[%s] stats marshal failed: %v", m.name, err)
		return
	}

	ingestURL := strings.TrimRight(m.cfg.SidecarURL, "/") + "/ingest"
	req, err := http.NewRequest(http.MethodPost, ingestURL, bytes.NewReader(body))
	if err != nil {
		log.Printf("[%s] stats request failed: %v", m.name, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		log.Printf("[%s] stats send failed: %v", m.name, err)
		return
	}
	_ = resp.Body.Close()
}

type cookieState struct {
	setCookie   string
	uniq        string
	secondVisit bool
	needsSet    bool
	value       string
}

func (m *statsMiddleware) readCookie(req *http.Request) cookieState {
	var state cookieState
	cookie, err := req.Cookie(m.cfg.CookieName)
	if err != nil || cookie == nil || cookie.Value == "" {
		userID := newUUID()
		state.setCookie = userID
		state.needsSet = true
		state.value = "?" + userID
		return state
	}

	if strings.HasPrefix(cookie.Value, "?") {
		userID := strings.TrimPrefix(cookie.Value, "?")
		state.uniq = userID
		state.secondVisit = true
		state.needsSet = true
		state.value = userID
		return state
	}

	state.uniq = cookie.Value
	return state
}

func (m *statsMiddleware) maybeSetCookie(headers http.Header, state cookieState) {
	if !state.needsSet {
		return
	}

	c := &http.Cookie{
		Name:     m.cfg.CookieName,
		Value:    state.value,
		Path:     m.cfg.CookiePath,
		Domain:   m.cfg.CookieDomain,
		MaxAge:   m.cfg.CookieMaxAge,
		Secure:   m.cfg.CookieSecure,
		HttpOnly: m.cfg.CookieHTTPOnly,
	}
	switch strings.ToLower(m.cfg.CookieSameSite) {
	case "strict":
		c.SameSite = http.SameSiteStrictMode
	case "none":
		c.SameSite = http.SameSiteNoneMode
	default:
		c.SameSite = http.SameSiteLaxMode
	}

	headers.Add("Set-Cookie", c.String())
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return uuidFromBytes(b[:])
}

func uuidFromBytes(b []byte) string {
	var buf [36]byte
	hex.Encode(buf[0:8], b[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], b[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], b[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], b[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], b[10:16])
	return string(buf[:])
}

func normalizeHost(host string) string {
	if host == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		return strings.ToLower(h)
	}
	return strings.ToLower(host)
}

type responseRecorder struct {
	inner       http.ResponseWriter
	status      int
	wroteHeader bool
}

func newResponseRecorder(inner http.ResponseWriter) *responseRecorder {
	return &responseRecorder{
		inner:  inner,
		status: http.StatusOK,
	}
}

func (r *responseRecorder) Header() http.Header {
	return r.inner.Header()
}

func (r *responseRecorder) WriteHeader(statusCode int) {
	r.status = statusCode
	r.wroteHeader = true
	r.inner.WriteHeader(statusCode)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(r.status)
	}
	return r.inner.Write(b)
}

func (r *responseRecorder) statusCode() int {
	return r.status
}

func (r *responseRecorder) finalize() {
	if !r.wroteHeader {
		r.inner.WriteHeader(r.status)
		r.wroteHeader = true
	}
}

func (r *responseRecorder) Flush() {
	if flusher, ok := r.inner.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (r *responseRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.inner.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("hijacker not supported")
	}
	return hijacker.Hijack()
}

func (r *responseRecorder) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := r.inner.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}
