package store

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"time"
)

// embedded holds the migration files compiled into the binary.
//
//go:embed migrations/*.sql
var embedded embed.FS

// EmbeddedMigrations returns the migrations compiled into the binary.
func EmbeddedMigrations() fs.FS {
	sub, err := fs.Sub(embedded, "migrations")
	if err != nil {
		panic(err) // the directory is embedded at build time; cannot fail
	}
	return sub
}

// migrationName is the file name pattern NNNN_name.sql.
var migrationName = regexp.MustCompile(`^(\d{4})_[a-z0-9_]+\.sql$`)

// migration is one embedded SQL file with its SHA-256 checksum.
type migration struct {
	version  int
	name     string
	sql      string
	checksum string
}

// loadMigrations reads and orders the migration files. It fails on a bad name or a gap in the versions.
func loadMigrations(fsys fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var ms []migration
	for _, e := range entries {
		m := migrationName.FindStringSubmatch(e.Name())
		if m == nil {
			return nil, fmt.Errorf("migration file %q does not match NNNN_name.sql", e.Name())
		}
		b, err := fs.ReadFile(fsys, e.Name())
		if err != nil {
			return nil, err
		}
		v, _ := strconv.Atoi(m[1])
		sum := sha256.Sum256(b)
		ms = append(ms, migration{v, e.Name(), string(b), hex.EncodeToString(sum[:])})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].version < ms[j].version })
	for i, m := range ms {
		if m.version != i+1 {
			return nil, fmt.Errorf("migration versions must be contiguous from 0001: found %s at position %d", m.name, i+1)
		}
	}
	return ms, nil
}

// Migrate applies, in order, every migration in fsys that schema_migrations does not
// list, and returns how many it applied. Each one runs in its own transaction, so a
// failure leaves the database at the previous version. It refuses to run when an
// applied file was changed (checksum) or the database is newer than the binary.
// There are no down migrations.
func (s *Store) Migrate(ctx context.Context, fsys fs.FS, now func() time.Time) (int, error) {
	ms, err := loadMigrations(fsys)
	if err != nil {
		return 0, err
	}
	applied, err := s.appliedChecksums(ctx)
	if err != nil {
		return 0, err
	}
	for v, sum := range applied {
		if v > len(ms) {
			return 0, fmt.Errorf("database is at migration %04d but this binary only knows %04d: refusing to start (downgrade not supported)", v, len(ms))
		}
		if ms[v-1].checksum != sum {
			return 0, fmt.Errorf("migration %s was modified after being applied (checksum mismatch): refusing to start", ms[v-1].name)
		}
	}
	n := 0
	for _, m := range ms {
		if _, done := applied[m.version]; done {
			continue
		}
		if err := s.apply(ctx, m, now()); err != nil {
			return n, fmt.Errorf("migration %s failed (rolled back): %w", m.name, err)
		}
		n++
	}
	return n, nil
}

// appliedChecksums returns version to checksum for the applied migrations; empty on a fresh database.
func (s *Store) appliedChecksums(ctx context.Context) (map[int]string, error) {
	out := map[int]string{}
	var name string
	err := s.db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name='schema_migrations'`).Scan(&name)
	if err != nil { // no table yet: fresh database
		return out, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT version, checksum FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var v int
		var sum string
		if err := rows.Scan(&v, &sum); err != nil {
			return nil, err
		}
		out[v] = sum
	}
	return out, rows.Err()
}

// apply runs one migration and records it in one transaction.
func (s *Store) apply(ctx context.Context, m migration, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op after Commit
	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at, checksum) VALUES (?, ?, ?)`,
		m.version, now.Unix(), m.checksum); err != nil {
		return err
	}
	return tx.Commit()
}
