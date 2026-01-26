package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/khaled/banan-stats/banan-stats/internal/analyzer"
	"github.com/khaled/banan-stats/banan-stats/internal/dashboard"
	"github.com/khaled/banan-stats/banan-stats/internal/store"
)

type ingestEvent struct {
	Timestamp   time.Time `json:"timestamp"`
	Host        string    `json:"host"`
	Path        string    `json:"path"`
	Query       string    `json:"query"`
	IP          string    `json:"ip"`
	UserAgent   string    `json:"userAgent"`
	Referrer    string    `json:"referrer"`
	ContentType string    `json:"contentType"`
	SetCookie   string    `json:"setCookie"`
	Uniq        string    `json:"uniq"`
	SecondVisit bool      `json:"secondVisit"`
}

type ingestRequest struct {
	Events []ingestEvent `json:"events"`
}

func main() {
	var (
		listen = flag.String("listen", ":7070", "listen address")
		dbPath = flag.String("db-path", "clj_simple_stats.duckdb", "DuckDB file path")
	)
	flag.Parse()

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("store open failed: %v", err)
	}
	defer st.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/ingest", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req ingestRequest
		dec := json.NewDecoder(r.Body)
		if err := dec.Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		lines := make([]analyzer.Line, 0, len(req.Events))
		for _, evt := range req.Events {
			ts := evt.Timestamp.UTC()
			line := analyzer.Line{
				Date:        ts.Format("2006-01-02"),
				Time:        ts.Format("15:04:05"),
				Host:        evt.Host,
				Path:        evt.Path,
				Query:       evt.Query,
				IP:          evt.IP,
				UserAgent:   evt.UserAgent,
				Referrer:    evt.Referrer,
				Type:        contentTypeToType(evt.ContentType),
				SetCookie:   evt.SetCookie,
				Uniq:        evt.Uniq,
				SecondVisit: evt.SecondVisit,
			}
			lines = append(lines, line)
		}
		if err := st.Insert(r.Context(), lines); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})

	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		dashboard.Render(r.Context(), st.DB(), w, r)
	})
	mux.HandleFunc("/stats/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	server := &http.Server{
		Addr:              *listen,
		Handler:           mux,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("stats sidecar listening on %s", *listen)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server failed: %v", err)
	}
}

func contentTypeToType(contentType string) string {
	ct := strings.ToLower(contentType)
	switch {
	case strings.HasPrefix(ct, "application/atom+xml"):
		return "feed"
	case strings.HasPrefix(ct, "application/rss+xml"):
		return "feed"
	default:
		return ""
	}
}
