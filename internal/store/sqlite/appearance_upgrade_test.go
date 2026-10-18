package sqlite

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/testutil"
)

func TestReviewAndAppearanceUpgrade(t *testing.T) {
	for _, version := range []int{0, 19, 20} {
		t.Run(fmt.Sprintf("from_v%d", version), func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			db, err := Open(filepath.Join(dir, "profile.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if version > 0 {
				files, err := fs.Glob(migrationsFS, "migrations/*.sql")
				if err != nil {
					t.Fatal(err)
				}
				oldSource := map[string]string{}
				for _, path := range files {
					name := strings.TrimPrefix(path, "migrations/")
					if name < fmt.Sprintf("%05d", version+1) {
						raw, err := fs.ReadFile(migrationsFS, path)
						if err != nil {
							t.Fatal(err)
						}
						oldSource[name] = string(raw)
					}
				}
				old, err := NewMigratorWithFS(db, testutil.NewMapFS(oldSource))
				if err != nil {
					t.Fatal(err)
				}
				if err := old.MigrateToLatest(ctx, filepath.Join(dir, "backups")); err != nil {
					t.Fatal(err)
				}
				if _, err := db.ExecContext(ctx, `UPDATE settings SET language = 'en', default_model = 'kept-model', revision = 7 WHERE id = 1`); err != nil {
					t.Fatal(err)
				}
				if version == 20 {
					if _, err := db.ExecContext(ctx, `UPDATE settings SET review_reject_limit = 5 WHERE id = 1`); err != nil {
						t.Fatal(err)
					}
				}
			}
			current, err := NewMigrator(db)
			if err != nil {
				t.Fatal(err)
			}
			if err := current.MigrateToLatest(ctx, filepath.Join(dir, "backups")); err != nil {
				t.Fatal(err)
			}
			actual, err := current.CurrentVersion(ctx)
			if err != nil || actual != MaxSupportedVersion {
				t.Fatalf("version = %d, error = %v; want %d", actual, err, MaxSupportedVersion)
			}
			saved, err := GetSettings(ctx, db)
			if err != nil {
				t.Fatal(err)
			}
			limit := 3
			if version == 20 {
				limit = 5
			}
			if saved.ColorMode != "system" || saved.Theme != "graphite" || saved.ReviewRejectLimit != limit {
				t.Fatalf("combined settings not preserved: %+v", saved)
			}
			if version > 0 && (saved.Language != "en" || saved.DefaultModel != "kept-model" || saved.Revision != 7) {
				t.Fatalf("existing settings changed: %+v", saved)
			}
			dark, harbor := "dark", "harbor"
			saved, err = UpdateSettings(ctx, db, domain.SettingsPatch{ColorMode: &dark, Theme: &harbor}, saved.Revision)
			if err != nil {
				t.Fatal(err)
			}
			if saved.ReviewRejectLimit != limit {
				t.Fatal("appearance update overwrote review settings")
			}
			newLimit := 9
			saved, err = UpdateSettings(ctx, db, domain.SettingsPatch{ReviewRejectLimit: &newLimit}, saved.Revision)
			if err != nil {
				t.Fatal(err)
			}
			if saved.ColorMode != dark || saved.Theme != harbor || saved.ReviewRejectLimit != newLimit {
				t.Fatalf("review update overwrote appearance: %+v", saved)
			}
			rows, err := db.QueryContext(ctx, `SELECT review_type, reviewer_id, reject_count, requirement_id FROM tasks LIMIT 0`)
			if err != nil {
				t.Fatal(err)
			}
			rows.Close()
			if version > 0 {
				backups, err := filepath.Glob(filepath.Join(dir, "backups", "*.sqlite"))
				if err != nil || len(backups) == 0 {
					t.Fatalf("upgrade backup missing: %v", err)
				}
			}
		})
	}
}
