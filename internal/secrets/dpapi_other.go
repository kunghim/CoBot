//go:build !windows

package secrets

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// masterKeyFile holds the AES key of the store. It lives next to the
// credential files, so the whole directory has to stay private.
const masterKeyFile = "master.key"

// aesCodec seals secrets with AES-GCM under a key kept in the store
// directory.
//
// Without DPAPI the key is a file like any other, so the protection is only
// as good as the permissions of the store directory.
type aesCodec struct {
	dir string

	mu  sync.Mutex
	key []byte
}

// newPlatformCodec returns the codec for platforms without DPAPI. The master
// key is created on first use, not here, so that an unused store leaves no
// key behind.
func newPlatformCodec(dir string) (codec, error) {
	return &aesCodec{dir: dir}, nil
}

// seal encrypts plaintext under the master key.
func (c *aesCodec) seal(plaintext []byte) ([]byte, error) {
	key, err := c.currentKey()
	if err != nil {
		return nil, err
	}
	nonce, ciphertext, err := sealSecret(key, plaintext)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, 1+len(nonce)+len(ciphertext))
	out = append(out, formatVersion)
	out = append(out, nonce...)
	out = append(out, ciphertext...)
	return out, nil
}

// open decrypts a credential file.
func (c *aesCodec) open(sealed []byte) ([]byte, error) {
	if len(sealed) < 1+nonceSize+gcmOverhead {
		return nil, errors.New("credentials: credential file is truncated")
	}
	if sealed[0] != formatVersion {
		return nil, errors.New("credentials: unsupported credential format")
	}
	key, err := c.currentKey()
	if err != nil {
		return nil, err
	}
	nonce := sealed[1 : 1+nonceSize]
	return openSecret(key, nonce, sealed[1+nonceSize:])
}

// currentKey returns the master key, creating it on first use.
func (c *aesCodec) currentKey() ([]byte, error) {
	if err := c.loadKey(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.key, nil
}

// loadKey reads the master key and creates it when it is missing.
//
// CoBot runs one instance per data root, so the file is created once; the
// exclusive create keeps a second process from replacing the key under the
// credentials that were written with it.
func (c *aesCodec) loadKey() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.key != nil {
		return nil
	}
	path := filepath.Join(c.dir, masterKeyFile)
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		key, err := masterKey(data)
		if err != nil {
			return err
		}
		c.key = key
		return nil
	case !errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("credentials: read master key: %w", err)
	}
	key := make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return fmt.Errorf("credentials: generate master key: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("credentials: create master key: %w", err)
		}
		// Another process won the race and wrote the key first.
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("credentials: read master key: %w", err)
		}
		key, err := masterKey(data)
		if err != nil {
			return err
		}
		c.key = key
		return nil
	}
	if _, err := file.Write(key); err != nil {
		file.Close()
		return fmt.Errorf("credentials: write master key: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("credentials: sync master key: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("credentials: close master key: %w", err)
	}
	c.key = key
	return nil
}

// masterKey checks the length of a master key file.
func masterKey(data []byte) ([]byte, error) {
	if len(data) != keySize {
		return nil, fmt.Errorf("credentials: master key file %s is not %d bytes", masterKeyFile, keySize)
	}
	return data, nil
}

// readFile reads a credential file. Outside Windows a plain read does not
// keep other writers out.
func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// renameOver replaces the target with the source. A Unix rename replaces an
// existing file without holding on to it.
func renameOver(from, to string) error {
	return os.Rename(from, to)
}
