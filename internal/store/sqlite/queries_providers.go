package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
)

// providerColumns lists the model_providers columns in scan order.
const providerColumns = `id, name, kind, auth_mode, credential_ref, endpoints_json, models_json, default_model, retries_json, preset_id, revision, created_at, updated_at`

// ListProviders reads every model provider, oldest first.
func ListProviders(ctx context.Context, q Queryer) ([]domain.Provider, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+providerColumns+` FROM model_providers ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list providers: %w", err)
	}
	defer rows.Close()
	out := []domain.Provider{}
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("walk providers: %w", err)
	}
	return out, nil
}

// GetProvider reads one model provider.
func GetProvider(ctx context.Context, q Queryer, id string) (domain.Provider, error) {
	p, err := getProvider(ctx, q, id)
	if errors.Is(err, ErrNotFound) {
		return domain.Provider{}, domain.NotFound("error.provider.not_found", map[string]any{"id": id})
	}
	return p, err
}

// getProvider reads one model provider and keeps the store's own not-found
// error, so callers inside this package can tell "absent" from "broken".
func getProvider(ctx context.Context, q Queryer, id string) (domain.Provider, error) {
	row := q.QueryRowContext(ctx, `SELECT `+providerColumns+` FROM model_providers WHERE id = ?`, id)
	return scanProvider(row)
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanProvider(s scanner) (domain.Provider, error) {
	var (
		p             domain.Provider
		endpointsJSON string
		modelsJSON    string
		retriesJSON   string
		created       string
		updated       string
	)
	err := s.Scan(&p.ID, &p.Name, &p.Kind, &p.AuthMode, &p.CredentialRef, &endpointsJSON,
		&modelsJSON, &p.DefaultModel, &retriesJSON, &p.PresetID, &p.Revision, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Provider{}, ErrNotFound
	}
	if err != nil {
		return domain.Provider{}, fmt.Errorf("scan provider: %w", err)
	}
	// A malformed blob must not take the whole list down: the row is still
	// usable for everything that does not depend on the broken field.
	if err := json.Unmarshal([]byte(endpointsJSON), &p.Endpoints); err != nil {
		p.Endpoints = nil
	}
	if err := json.Unmarshal([]byte(modelsJSON), &p.Models); err != nil {
		p.Models = nil
	}
	if err := json.Unmarshal([]byte(retriesJSON), &p.Retries); err != nil {
		p.Retries = domain.ProviderRetries{}
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	p.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return p, nil
}

// UpsertProvider creates or updates a model provider. expectedRevision 0
// skips the concurrency check; a non-zero value must match the stored
// revision or the write is refused.
func UpsertProvider(ctx context.Context, db *sql.DB, p domain.Provider, expectedRevision int64) (domain.Provider, error) {
	if err := p.Validate(); err != nil {
		return domain.Provider{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Provider{}, fmt.Errorf("begin provider tx: %w", err)
	}
	defer tx.Rollback()

	saved, err := UpsertProviderTx(ctx, tx, p, expectedRevision)
	if err != nil {
		return domain.Provider{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Provider{}, err
	}
	return saved, nil
}

// UpsertProviderTx is the in-transaction form of UpsertProvider.
func UpsertProviderTx(ctx context.Context, tx *sql.Tx, p domain.Provider, expectedRevision int64) (domain.Provider, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	endpointsJSON, err := json.Marshal(p.Endpoints)
	if err != nil {
		return domain.Provider{}, fmt.Errorf("encode provider endpoints: %w", err)
	}
	modelsJSON, err := json.Marshal(p.Models)
	if err != nil {
		return domain.Provider{}, fmt.Errorf("encode provider models: %w", err)
	}
	retriesJSON, err := json.Marshal(p.Retries)
	if err != nil {
		return domain.Provider{}, fmt.Errorf("encode provider retries: %w", err)
	}

	current, err := getProvider(ctx, tx, p.ID)
	switch {
	case errors.Is(err, ErrNotFound):
		_, err = tx.ExecContext(ctx, `
			INSERT INTO model_providers (`+providerColumns+`)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
			p.ID, p.Name, p.Kind, p.AuthMode, p.CredentialRef, string(endpointsJSON),
			string(modelsJSON), p.DefaultModel, string(retriesJSON), p.PresetID, now, now)
		if err != nil {
			return domain.Provider{}, fmt.Errorf("insert provider: %w", err)
		}
	case err != nil:
		return domain.Provider{}, err
	default:
		if expectedRevision > 0 && current.Revision != expectedRevision {
			return domain.Provider{}, domain.RevisionConflict("error.provider.revision_conflict", map[string]any{
				"expected": expectedRevision, "actual": current.Revision,
			})
		}
		_, err = tx.ExecContext(ctx, `
			UPDATE model_providers SET
				name = ?, kind = ?, auth_mode = ?, credential_ref = ?,
				endpoints_json = ?, models_json = ?, default_model = ?,
				retries_json = ?, preset_id = ?, revision = revision + 1, updated_at = ?
			WHERE id = ?`,
			p.Name, p.Kind, p.AuthMode, p.CredentialRef, string(endpointsJSON),
			string(modelsJSON), p.DefaultModel, string(retriesJSON), p.PresetID, now, p.ID)
		if err != nil {
			return domain.Provider{}, fmt.Errorf("update provider: %w", err)
		}
	}
	return getProvider(ctx, tx, p.ID)
}

// DeleteProvider removes a model provider.
func DeleteProvider(ctx context.Context, db *sql.DB, id string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin provider delete tx: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM model_providers WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete provider: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete provider rows: %w", err)
	}
	if affected == 0 {
		return domain.NotFound("error.provider.not_found", map[string]any{"id": id})
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}
