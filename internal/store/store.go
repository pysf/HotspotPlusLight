// Package store is the SQLite persistence layer: connection setup, the
// migration runner and hand-written parameterized queries. It implements the
// repository interfaces declared by the service packages.
package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite" // pure-Go driver, no CGO
)

type Store struct{ db *sql.DB }

// Open opens (creating if needed) the SQLite file with the pragmas of
// PLAN.md section 6. _txlock=immediate makes every write transaction take the
// write lock up front (single-writer, no upgrade deadlocks).
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", DSN(path))
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("open database %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// DB exposes the handle for tests and the migration runner only.
func (s *Store) DB() *sql.DB { return s.db }
