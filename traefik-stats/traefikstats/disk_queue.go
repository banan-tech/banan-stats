package traefikstats

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	_ "modernc.org/sqlite"
)

type queuedEvent struct {
	ID   int64
	Event event
}

type diskQueue struct {
	db        *sql.DB
	notify    chan struct{}
	maxEvents int
	mu        sync.Mutex
	cond      *sync.Cond
	count     int
}

func newDiskQueue(path string, maxEvents int) (*diskQueue, error) {
	if path == "" {
		return nil, fmt.Errorf("buffer path is empty")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite buffer: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	for _, stmt := range []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA synchronous=NORMAL;",
		"PRAGMA busy_timeout=5000;",
		`CREATE TABLE IF NOT EXISTS events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			payload TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);`,
		"CREATE INDEX IF NOT EXISTS idx_events_created_at ON events(created_at);",
	} {
		if _, err := db.Exec(stmt); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("init sqlite buffer: %w", err)
		}
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(1) FROM events").Scan(&count); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("count sqlite buffer: %w", err)
	}

	q := &diskQueue{
		db:        db,
		notify:    make(chan struct{}, 1),
		maxEvents: maxEvents,
		count:     count,
	}
	q.cond = sync.NewCond(&q.mu)
	return q, nil
}

func (q *diskQueue) Close() error {
	if q == nil || q.db == nil {
		return nil
	}
	return q.db.Close()
}

func (q *diskQueue) Enqueue(evt event) error {
	payload, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("encode event: %w", err)
	}

	q.mu.Lock()
	for q.maxEvents > 0 && q.count >= q.maxEvents {
		q.cond.Wait()
	}
	q.count++
	q.mu.Unlock()

	if _, err := q.db.Exec("INSERT INTO events(payload) VALUES (?)", string(payload)); err != nil {
		q.mu.Lock()
		q.count--
		q.cond.Signal()
		q.mu.Unlock()
		return fmt.Errorf("insert event: %w", err)
	}

	select {
	case q.notify <- struct{}{}:
	default:
	}
	return nil
}

func (q *diskQueue) FetchBatch(limit int) ([]queuedEvent, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := q.db.Query("SELECT id, payload FROM events ORDER BY id LIMIT ?", limit)
	if err != nil {
		return nil, fmt.Errorf("select batch: %w", err)
	}
	defer rows.Close()

	var out []queuedEvent
	for rows.Next() {
		var id int64
		var payload string
		if err := rows.Scan(&id, &payload); err != nil {
			return nil, fmt.Errorf("scan batch: %w", err)
		}
		var evt event
		if err := json.Unmarshal([]byte(payload), &evt); err != nil {
			log.Printf("stats buffer: invalid payload id=%d: %v", id, err)
			if _, delErr := q.db.Exec("DELETE FROM events WHERE id = ?", id); delErr != nil {
				log.Printf("stats buffer: failed to delete bad payload id=%d: %v", id, delErr)
			}
			continue
		}
		out = append(out, queuedEvent{ID: id, Event: evt})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate batch: %w", err)
	}
	return out, nil
}

func (q *diskQueue) DeleteUpTo(lastID int64) error {
	if lastID <= 0 {
		return nil
	}
	res, err := q.db.Exec("DELETE FROM events WHERE id <= ?", lastID)
	if err != nil {
		return fmt.Errorf("delete batch: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil
	}
	if affected > 0 {
		q.mu.Lock()
		q.count -= int(affected)
		if q.count < 0 {
			q.count = 0
		}
		q.cond.Broadcast()
		q.mu.Unlock()
	}
	return nil
}
