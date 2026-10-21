package secrets

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// credentialFileName maps a reference to the file that holds its secret.
//
// A reference that is not safe to put in a file name is rejected instead of
// escaped: rewriting it would silently turn one reference into another. The
// hash suffix keeps two references that sanitize to the same name apart.
func credentialFileName(ref string) (string, error) {
	if ref == "" {
		return "", errors.New("credentials: reference is empty")
	}
	if utf8.RuneCountInString(ref) > maxRefLength {
		return "", fmt.Errorf("credentials: reference is longer than %d characters", maxRefLength)
	}
	if strings.Contains(ref, "..") {
		return "", errors.New("credentials: reference must not contain ..")
	}
	if strings.ContainsAny(ref, `/\`) {
		return "", errors.New("credentials: reference must not contain a path separator")
	}
	sum := sha256.Sum256([]byte(ref))
	return sanitizeRef(ref) + "-" + hex.EncodeToString(sum[:8]) + fileSuffix, nil
}

// sanitizeRef keeps only the characters that are safe in a file name on
// every platform CoBot runs on.
func sanitizeRef(ref string) string {
	var out strings.Builder
	out.Grow(len(ref))
	for _, r := range ref {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			out.WriteRune(r)
		default:
			out.WriteByte('_')
		}
	}
	return out.String()
}

// writeFileAtomic replaces the target with data in one step.
//
// A secret that is written in place can be read half written, and a crash
// in the middle leaves a truncated credential behind. Writing a temporary
// file and renaming it over the target avoids both.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".cred-*")
	if err != nil {
		return fmt.Errorf("credentials: create temporary file: %w", err)
	}
	name := tmp.Name()
	// Removes the temporary file on every path that does not rename it.
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("credentials: write temporary file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("credentials: sync temporary file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("credentials: close temporary file: %w", err)
	}
	if err := renameOver(name, path); err != nil {
		return fmt.Errorf("credentials: replace credential: %w", err)
	}
	return nil
}
