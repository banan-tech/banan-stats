package store

import (
	"context"
	"database/sql"

	_ "github.com/duckdb/duckdb-go/v2"

	"github.com/khaled/banan-stats/banan-stats/internal/analyzer"
)

type Store struct {
	db *sql.DB
}

func Open(dbPath string) (*Store, error) {
	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := initSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func initSchema(db *sql.DB) error {
	stmts := []string{
		"CREATE TYPE IF NOT EXISTS agent_type_t AS ENUM ('feed', 'bot', 'browser')",
		"CREATE TYPE IF NOT EXISTS agent_os_t AS ENUM ('Android', 'Windows', 'iOS', 'macOS', 'Linux')",
		`CREATE TABLE IF NOT EXISTS stats (
			date       DATE,
			time       TIME,
			host       VARCHAR,
			path       VARCHAR,
			query      VARCHAR,
			ip         VARCHAR,
			user_agent VARCHAR,
			referrer   VARCHAR,
			type       agent_type_t,
			agent      VARCHAR,
			os         agent_os_t,
			ref_domain VARCHAR,
			mult       INTEGER,
			set_cookie UUID,
			uniq       UUID
		)`,
		"ALTER TABLE stats ADD COLUMN IF NOT EXISTS host VARCHAR",
		"CREATE INDEX IF NOT EXISTS idx_stats_host_date ON stats(host, date)",
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Insert(ctx context.Context, lines []analyzer.Line) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	insertSQL := `INSERT INTO stats
		(date, time, host, path, query, ip, user_agent, referrer, type, agent, os, ref_domain, mult, set_cookie, uniq)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	stmt, err := tx.PrepareContext(ctx, insertSQL)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()

	updateSQL := "UPDATE stats SET uniq = ? WHERE set_cookie = ?"
	updStmt, err := tx.PrepareContext(ctx, updateSQL)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer updStmt.Close()

	for _, line := range lines {
		analyzer.Analyze(&line)
		_, err = stmt.ExecContext(
			ctx,
			nullString(line.Date),
			nullString(line.Time),
			nullString(line.Host),
			nullString(line.Path),
			nullString(line.Query),
			nullString(line.IP),
			nullString(line.UserAgent),
			nullString(line.Referrer),
			nullString(line.Type),
			nullString(line.Agent),
			nullString(line.OS),
			nullString(line.RefDomain),
			line.Mult,
			nullUUID(line.SetCookie),
			nullUUID(line.Uniq),
		)
		if err != nil {
			_ = tx.Rollback()
			return err
		}

		if line.SecondVisit && line.Uniq != "" {
			if _, err := updStmt.ExecContext(ctx, line.Uniq, line.Uniq); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
	}

	return tx.Commit()
}

func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func nullUUID(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
