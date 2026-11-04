// Package store is the SQLite persistence layer: connection setup, the
// migration runner and hand-written parameterized queries. It implements the
// repository interfaces declared by the service packages.
package store

import (
	"database/sql"
	"errors"
	"fmt"

	_ "modernc.org/sqlite" // pure-Go driver, no CGO
)

// readConns is the size of the read pool.
const readConns = 8

// Store opens the same file as two pools:
//   - db is the write pool: exactly ONE connection. Every transaction and every
//     write goes through it, so writers wait in Go (fair queue) and not in the
//     SQLite busy handler (which polls and lets new writers barge in).
//   - r is the read pool, for read-only queries that need no atomicity with a write.
//
// The read pool is opened with query_only, so a write through it fails.
//
// Rule: never use r while holding a write transaction when the read must agree
// with the write; do that read inside the transaction. And never call a Store
// method that uses db from inside a transaction callback: with one connection
// it waits for itself forever.
type Store struct{ db, r *sql.DB }

// Open opens (creating if needed) the SQLite file with the pragmas of
// PLAN.md section 6. _txlock=immediate makes every write transaction take the
// write lock up front (single-writer, no upgrade deadlocks).
// The write pool is opened first, so WAL mode is set before the read pool opens.
func Open(path string) (*Store, error) {
	w, err := sql.Open("sqlite", DSN(path))
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	w.SetMaxIdleConns(1)
	w.SetConnMaxLifetime(0)
	if err := w.Ping(); err != nil { // runs the pragmas (journal_mode=WAL)
		w.Close()
		return nil, fmt.Errorf("open database %s: %w", path, err)
	}
	r, err := sql.Open("sqlite", readDSN(path))
	if err != nil {
		w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(readConns)
	r.SetMaxIdleConns(readConns)
	if err := r.Ping(); err != nil {
		r.Close()
		w.Close()
		return nil, fmt.Errorf("open database %s (read pool): %w", path, err)
	}
	return &Store{db: w, r: r}, nil
}

// Close closes both pools.
func (s *Store) Close() error { return errors.Join(s.r.Close(), s.db.Close()) }

// DB exposes the write pool for tests and the migration runner only.
func (s *Store) DB() *sql.DB { return s.db }
