// Package credentials stores the API keys of model providers.
//
// A secret never reaches the disk in clear text, never enters the database
// and never shows up in an error message. The encryption layer is platform
// specific: Windows protects the secret with DPAPI, so only the Windows user
// that wrote it can read it again; other platforms keep an AES-GCM key in the
// store directory.
//
// A reference names one secret. It is a flat name and not a path: a
// reference that would need escaping is rejected, because a reference that
// has to be rewritten is a reference that something else is trying to
// smuggle.
package secrets

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Store persists one secret per reference. Implementations must be safe for
// concurrent use.
type Store interface {
	Put(ctx context.Context, ref string, secret []byte) error
	Get(ctx context.Context, ref string) ([]byte, error)
	Delete(ctx context.Context, ref string) error
}

// ErrNotFound is returned by Get when the reference holds no secret.
var ErrNotFound = errors.New("credential not found")

const (
	// maxRefLength bounds a reference so the file name derived from it
	// stays inside the path limits of every platform CoBot runs on.
	maxRefLength = 128
	// maxSecretSize bounds one credential. Provider API keys are a few
	// hundred bytes; the limit only keeps a caller from filling the disk
	// through this package.
	maxSecretSize = 1 << 20
	// keySize is the AES-256 key length.
	keySize = 32
	// nonceSize is the size of the GCM nonce in a credential file.
	nonceSize = 12
	// gcmOverhead is the size of the GCM authentication tag.
	gcmOverhead = 16
	// formatVersion marks the layout of a credential file.
	formatVersion byte = 1
	// fileSuffix marks the files this package owns in the store directory.
	fileSuffix = ".cred"
)

// codec seals and opens one secret. Implementations must be safe for
// concurrent use.
type codec interface {
	seal(plaintext []byte) ([]byte, error)
	open(sealed []byte) ([]byte, error)
}

// fileStore keeps one encrypted file per reference in the store directory.
type fileStore struct {
	dir   string
	codec codec

	// mu serializes the writes. Replacing a file takes two steps, and
	// Windows refuses to do it twice at the same time.
	mu sync.Mutex
}

// New returns the store for this platform: DPAPI-encrypted on Windows,
// permission-restricted files elsewhere.
func New(dir string) (Store, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("credentials: directory is required")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("credentials: resolve directory: %w", err)
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("credentials: create directory: %w", err)
	}
	platform, err := newPlatformCodec(abs)
	if err != nil {
		return nil, err
	}
	return &fileStore{dir: abs, codec: platform}, nil
}

// Put stores secret under ref, replacing a secret that is already there.
func (s *fileStore) Put(ctx context.Context, ref string, secret []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.pathFor(ref)
	if err != nil {
		return err
	}
	if len(secret) > maxSecretSize {
		return fmt.Errorf("credentials: secret is larger than %d bytes", maxSecretSize)
	}
	sealed, err := s.codec.seal(secret)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeFileAtomic(path, sealed)
}

// Get returns the secret stored under ref. The bytes belong to the caller.
func (s *fileStore) Get(ctx context.Context, ref string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := s.pathFor(ref)
	if err != nil {
		return nil, err
	}
	sealed, err := readFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("credentials: read credential: %w", err)
	}
	plain, err := s.codec.open(sealed)
	if err != nil {
		return nil, err
	}
	// The caller owns what it gets back, so a later write cannot change a
	// secret that another caller is still holding.
	return bytes.Clone(plain), nil
}

// Delete removes the secret stored under ref.
func (s *fileStore) Delete(ctx context.Context, ref string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.pathFor(ref)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return fmt.Errorf("credentials: delete credential: %w", err)
	}
	return nil
}

// pathFor resolves the file that holds the secret of ref.
//
// The file name is built from a fixed alphabet, so it cannot leave the store
// directory; the check is kept as the last line of defence.
func (s *fileStore) pathFor(ref string) (string, error) {
	name, err := credentialFileName(ref)
	if err != nil {
		return "", err
	}
	path := filepath.Join(s.dir, name)
	if filepath.Dir(path) != s.dir {
		return "", errors.New("credentials: credential path escapes the store directory")
	}
	return path, nil
}

// sealSecret encrypts plaintext under key and returns the nonce and the
// ciphertext apart, so each platform can lay out the envelope it needs.
func sealSecret(key, plaintext []byte) (nonce, ciphertext []byte, err error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, fmt.Errorf("credentials: init cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("credentials: init gcm: %w", err)
	}
	nonce = make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("credentials: generate nonce: %w", err)
	}
	return nonce, gcm.Seal(nil, nonce, plaintext, nil), nil
}

// openSecret reverses sealSecret. The error never mentions the plaintext.
func openSecret(key, nonce, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("credentials: init cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("credentials: init gcm: %w", err)
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errors.New("credentials: credential file is corrupt")
	}
	return plain, nil
}
