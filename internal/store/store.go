// Package store owns SQLite setup and the only durable write path.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type Store struct {
	ReadDB  *sql.DB
	WriteDB *sql.DB
	writeMu sync.Mutex
}

func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("database path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	uriPath := strings.ReplaceAll(abs, "\\", "/")
	writeDSN := "file:" + uriPath + "?_txlock=immediate"
	writeDB, err := sql.Open("sqlite", writeDSN)
	if err != nil {
		return nil, fmt.Errorf("open write database: %w", err)
	}
	writeDB.SetMaxOpenConns(1)
	writeDB.SetMaxIdleConns(1)
	s := &Store{WriteDB: writeDB}
	if err := s.configureAndMigrate(ctx); err != nil {
		writeDB.Close()
		return nil, err
	}

	// mode=ro and _query_only=on are deliberately visible in the DSN. The
	// _pragma form is the driver-supported enforcement of query_only.
	readDSN := "file:" + uriPath + "?mode=ro&_query_only=on&_pragma=query_only(1)"
	readDB, err := sql.Open("sqlite", readDSN)
	if err != nil {
		writeDB.Close()
		return nil, fmt.Errorf("open read database: %w", err)
	}
	readDB.SetMaxOpenConns(5)
	readDB.SetMaxIdleConns(5)
	if err := readDB.PingContext(ctx); err != nil {
		readDB.Close()
		writeDB.Close()
		return nil, fmt.Errorf("ping read database: %w", err)
	}
	s.ReadDB = readDB
	return s, nil
}

func (s *Store) configureAndMigrate(ctx context.Context) error {
	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA mmap_size = 134217728",
		"PRAGMA cache_size = -32000",
		"PRAGMA wal_autocheckpoint = 0",
	}
	for _, q := range pragmas {
		if _, err := s.WriteDB.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("%s: %w", q, err)
		}
	}
	return s.migrate(ctx)
}

func (s *Store) migrate(ctx context.Context) error {
	entries, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(entries)
	for _, name := range entries {
		version := filepath.Base(name)
		var seen int
		if err := s.WriteDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version).Scan(&seen); err != nil && !strings.Contains(err.Error(), "no such table") {
			return err
		}
		if seen > 0 {
			continue
		}
		body, err := migrationFiles.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read %s: %w", version, err)
		}
		tx, err := s.WriteDB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, string(body)); err == nil {
			_, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations(version) VALUES(?)", version)
		}
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("apply %s: %w", version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", version, err)
		}
	}
	return nil
}

// Write serializes every transaction in addition to the single SQLite writer.
func (s *Store) Write(ctx context.Context, fn func(*sql.Tx) error) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.WriteDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// LoadBalances is used at startup to rebuild the process-local hot cache.
func (s *Store) LoadBalances(ctx context.Context) (map[string]int64, error) {
	rows, err := s.ReadDB.QueryContext(ctx, "SELECT account_id, balance FROM accounts")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	balances := make(map[string]int64)
	for rows.Next() {
		var id string
		var balance int64
		if err := rows.Scan(&id, &balance); err != nil {
			return nil, err
		}
		balances[id] = balance
	}
	return balances, rows.Err()
}

func (s *Store) Close() error {
	var first error
	if s.ReadDB != nil {
		first = s.ReadDB.Close()
	}
	if err := s.WriteDB.Close(); err != nil && first == nil {
		first = err
	}
	return first
}
