// Package sqlite owns the product database: one connection, explicit
// pragmas, goose migrations and plain parameterized queries.
package sqlite

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// DriverName is the modernc.org/sqlite driver identifier.
const DriverName = "sqlite"

// Open connects to the profile database with the required pragmas applied on
// every connection and caps the pool at a single connection so transactions
// never wait on a second one.
func Open(dbPath string) (*sql.DB, error) {
	dsn := "file:" + filepath.ToSlash(dbPath) +
		"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)"
	db, err := sql.Open(DriverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	var journal string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil {
		db.Close()
		return nil, fmt.Errorf("verify journal mode: %w", err)
	}
	if !strings.EqualFold(journal, "wal") {
		db.Close()
		return nil, fmt.Errorf("sqlite journal_mode is %q, want WAL", journal)
	}
	return db, nil
}
