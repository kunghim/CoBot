package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations exposes the embedded schema files for tooling and tests.
func Migrations() fs.FS { return migrationsFS }

// MaxSupportedVersion is the highest migration this build understands. A
// database ahead of it must be refused instead of silently downgraded.
const MaxSupportedVersion = 32

// Migrator wraps the goose provider bound to the product connection.
type Migrator struct {
	provider *goose.Provider
	db       *sql.DB
}

// NewMigrator binds the embedded migrations to an open database. goose only
// globs the filesystem root, so the migrations subdirectory is the view we
// hand it.
func NewMigrator(db *sql.DB) (*Migrator, error) {
	migrations, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("open embedded migrations: %w", err)
	}
	return NewMigratorWithFS(db, migrations)
}

// NewMigratorWithFS is the test seam: it accepts any migration filesystem so
// failure paths can be exercised without touching the embedded sources.
func NewMigratorWithFS(db *sql.DB, migrations fs.FS) (*Migrator, error) {
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, migrations)
	if err != nil {
		return nil, fmt.Errorf("create goose provider: %w", err)
	}
	return &Migrator{provider: provider, db: db}, nil
}

// CurrentVersion returns the highest applied migration version (0 when new).
func (m *Migrator) CurrentVersion(ctx context.Context) (int64, error) {
	var exists int
	err := m.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='goose_db_version'`).Scan(&exists)
	if err != nil {
		return 0, fmt.Errorf("check goose version table: %w", err)
	}
	if exists == 0 {
		return 0, nil
	}
	var version sql.NullInt64
	if err := m.db.QueryRowContext(ctx, `SELECT MAX(version_id) FROM goose_db_version`).Scan(&version); err != nil {
		return 0, fmt.Errorf("read goose version: %w", err)
	}
	if !version.Valid {
		return 0, nil
	}
	return version.Int64, nil
}

// Guard refuses databases written by a newer program before anything else
// touches business tables.
func (m *Migrator) Guard(ctx context.Context) error {
	version, err := m.CurrentVersion(ctx)
	if err != nil {
		return err
	}
	if version > MaxSupportedVersion {
		return fmt.Errorf("database schema version %d is newer than supported %d; refusing to open this profile with this build", version, MaxSupportedVersion)
	}
	return nil
}

// MigrateToLatest applies pending migrations. When the database already has
// business tables, a consistent snapshot is saved into backupsDir first and
// only the most recent keep backups remain. A brand-new database then gets
// today's first-run settings (see applyFirstRunDefaults).
func (m *Migrator) MigrateToLatest(ctx context.Context, backupsDir string) error {
	if err := m.Guard(ctx); err != nil {
		return err
	}
	version, err := m.CurrentVersion(ctx)
	if err != nil {
		return err
	}
	fresh := false
	backupPath := ""
	// Version 23 replaced tasks with requirements and their cards; the old
	// ones cannot be carried over and are dropped (see that migration).
	// Count them first, so the teams that had some are told.
	var dropped map[string]droppedTasks
	if version > 0 && version < 23 {
		if dropped, err = countLegacyTasks(ctx, m.db); err != nil {
			return err
		}
	}
	if version < targetVersion(m.provider) {
		// Any pre-existing business table (old library seed or earlier
		// version) earns a consistent snapshot before schema changes.
		var tables int
		if err := m.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'goose_%'`).Scan(&tables); err != nil {
			return fmt.Errorf("inspect existing tables: %w", err)
		}
		if tables > 0 {
			if backupPath, err = backup(ctx, m.db, backupsDir); err != nil {
				return fmt.Errorf("backup before migration: %w", err)
			}
		}
		fresh = version == 0 && tables == 0
	}
	if _, err := m.provider.Up(ctx); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	if fresh {
		if err := applyFirstRunDefaults(ctx, m.db); err != nil {
			return err
		}
	}
	if len(dropped) > 0 {
		// Only an upgrade that ran version 23 dropped them.
		if now, err := m.CurrentVersion(ctx); err != nil {
			return err
		} else if now >= 23 {
			if err := tellDroppedTasks(ctx, m.db, dropped, backupPath); err != nil {
				return err
			}
		}
	}
	pruneBackups(backupsDir, 3)
	return nil
}

// applyFirstRunDefaults sets the settings a new profile starts with where
// they differ from the column defaults. Migrations stay append-only and an
// upgraded profile keeps what it has, so a default that changed after its
// column was added (the theme) is applied here, to new databases only.
func applyFirstRunDefaults(ctx context.Context, db *sql.DB) error {
	defaults := domain.DefaultSettings()
	// A migrator built from an older migration set (tests) may stop before
	// the column exists.
	var hasTheme int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('settings') WHERE name = 'theme'`).Scan(&hasTheme); err != nil {
		return fmt.Errorf("inspect settings: %w", err)
	}
	if hasTheme == 0 {
		return nil
	}
	if _, err := db.ExecContext(ctx, `UPDATE settings SET theme = ? WHERE id = 1`, defaults.Theme); err != nil {
		return fmt.Errorf("apply first-run settings: %w", err)
	}
	return nil
}

// targetVersion is the highest version known to the migration source list.
func targetVersion(p *goose.Provider) int64 {
	var target int64
	for _, src := range p.ListSources() {
		if src.Version > target {
			target = src.Version
		}
	}
	return target
}

// backup writes a consistent snapshot with VACUUM INTO, which includes WAL
// content; copying the main file alone would lose recent commits.
func backup(ctx context.Context, db *sql.DB, backupsDir string) (string, error) {
	if err := os.MkdirAll(backupsDir, 0o700); err != nil {
		return "", fmt.Errorf("create backups dir: %w", err)
	}
	version, err := func() (int64, error) {
		var v sql.NullInt64
		err := db.QueryRowContext(ctx, `SELECT MAX(version_id) FROM goose_db_version`).Scan(&v)
		if err != nil {
			return 0, err
		}
		if !v.Valid {
			return 0, nil
		}
		return v.Int64, nil
	}()
	if err != nil {
		return "", fmt.Errorf("read version for backup name: %w", err)
	}
	name := fmt.Sprintf("cobot-v%d-%s.sqlite", version, time.Now().UTC().Format("20060102T150405.000Z"))
	target := filepath.Join(backupsDir, name)
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, target); err != nil {
		return "", fmt.Errorf("vacuum into backup: %w", err)
	}
	return target, nil
}

func pruneBackups(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "cobot-v") || !strings.HasSuffix(e.Name(), ".sqlite") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names) // timestamp format sorts chronologically
	for len(names) > keep {
		_ = os.Remove(filepath.Join(dir, names[0]))
		names = names[1:]
	}
}
