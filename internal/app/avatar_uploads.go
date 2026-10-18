package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// Local pictures arrive already cropped and scaled by the interface; the
// core only accepts that exact shape, so it needs no image processing.
const (
	avatarUploadMaxBytes = 512 << 10
	avatarUploadSide     = 256
	// avatarUploadGrace keeps a just-saved picture that is not referenced
	// yet (the choice is saved right after the upload) from being cleaned.
	avatarUploadGrace = 10 * time.Minute
)

// SaveAvatarUpload stores a local picture for use as an avatar and returns
// its file name. The picture stays in this profile's data root.
func (a *App) SaveAvatarUpload(ctx context.Context, principal auth.Principal, data []byte) (string, error) {
	if principal.Kind != auth.KindUser {
		return "", domain.Forbidden("error.forbidden.user_only", nil)
	}
	if len(data) == 0 || len(data) > avatarUploadMaxBytes {
		return "", domain.InvalidRequest("error.avatar.upload_size", map[string]any{"maxBytes": avatarUploadMaxBytes})
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width != avatarUploadSide || config.Height != avatarUploadSide {
		return "", domain.InvalidRequest("error.avatar.upload_format", map[string]any{"side": avatarUploadSide})
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		return "", domain.InvalidRequest("error.avatar.upload_format", map[string]any{"side": avatarUploadSide})
	}
	sum := sha256.Sum256(data)
	name := hex.EncodeToString(sum[:]) + ".png"
	dir := a.Paths.AvatarUploadsDir()
	target := filepath.Join(dir, name)
	if _, err := os.Stat(target); err == nil {
		// The same picture again: refresh its age so cleanup spares it.
		now := time.Now()
		_ = os.Chtimes(target, now, now)
		return name, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return "", err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return name, nil
}

// AvatarUploadPath returns where a stored picture is, for serving it.
func (a *App) AvatarUploadPath(principal auth.Principal, name string) (string, error) {
	if principal.Kind != auth.KindUser {
		return "", domain.Forbidden("error.forbidden.user_only", nil)
	}
	if !domain.UploadFileName.MatchString(name) {
		return "", domain.NotFound("error.not_found", nil)
	}
	path := filepath.Join(a.Paths.AvatarUploadsDir(), name)
	if _, err := os.Stat(path); err != nil {
		return "", domain.NotFound("error.not_found", nil)
	}
	return path, nil
}

// requireUploadExists rejects a reference to a picture that is not stored.
func (a *App) requireUploadExists(ref *domain.AvatarRef) error {
	if ref == nil || ref.Kind != domain.AvatarKindUpload {
		return nil
	}
	if _, err := os.Stat(filepath.Join(a.Paths.AvatarUploadsDir(), ref.File)); err != nil {
		return domain.InvalidRequest("error.avatar.upload_missing", nil)
	}
	return nil
}

// CleanAvatarUploads removes stored pictures no member or role uses any
// more. It runs once as the core starts; recent files are spared.
func (a *App) CleanAvatarUploads(ctx context.Context) {
	dir := a.Paths.AvatarUploadsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	used, err := a.usedUploads(ctx)
	if err != nil {
		if a.Logger != nil {
			a.Logger.Warn("avatar pictures not cleaned", "error", err)
		}
		return
	}
	cutoff := time.Now().Add(-avatarUploadGrace)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || used[name] {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		// Unused pictures and leftovers of interrupted saves both go.
		if domain.UploadFileName.MatchString(name) || strings.HasPrefix(name, ".upload-") {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

func (a *App) usedUploads(ctx context.Context) (map[string]bool, error) {
	used := map[string]bool{}
	note := func(stored string) {
		if ref := domain.DecodeAvatar(stored); ref != nil && ref.Kind == domain.AvatarKindUpload {
			used[ref.File] = true
		}
	}
	// Deleted members keep their history; their pictures stay too.
	rows, err := a.DB.QueryContext(ctx, `SELECT avatar FROM bots WHERE avatar != ''`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var stored string
		if err := rows.Scan(&stored); err != nil {
			rows.Close()
			return nil, err
		}
		note(stored)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	roles, err := sqlite.RoleAvatars(ctx, a.DB)
	if err != nil {
		return nil, err
	}
	for _, ref := range roles {
		if ref.Kind == domain.AvatarKindUpload {
			used[ref.File] = true
		}
	}
	return used, nil
}
