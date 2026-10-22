package secrets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// newTestStore returns a store in a directory of its own and the directory
// it was given.
func newTestStore(t *testing.T) (Store, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "credentials")
	store, err := New(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return store, dir
}

func TestPutGetRoundTrip(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)
	ctx := context.Background()
	secret := []byte("sk-live-0123456789abcdef")
	if err := store.Put(ctx, "openai", secret); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := store.Get(ctx, "openai")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !bytes.Equal(got, secret) {
		t.Fatalf("got %q, want %q", got, secret)
	}
	// The bytes belong to the caller: changing them must not change what
	// the store holds.
	got[0] = 'X'
	again, err := store.Get(ctx, "openai")
	if err != nil {
		t.Fatalf("get again: %v", err)
	}
	if !bytes.Equal(again, secret) {
		t.Fatalf("got %q after the caller changed its copy, want %q", again, secret)
	}
}

func TestGetMissingReference(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)
	if _, err := store.Get(context.Background(), "absent"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestPutReplacesSecret(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)
	ctx := context.Background()
	if err := store.Put(ctx, "openai", []byte("first")); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := store.Put(ctx, "openai", []byte("second")); err != nil {
		t.Fatalf("put again: %v", err)
	}
	got, err := store.Get(ctx, "openai")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != "second" {
		t.Fatalf("got %q, want %q", got, "second")
	}
}

func TestDeleteRemovesSecret(t *testing.T) {
	t.Parallel()
	store, dir := newTestStore(t)
	ctx := context.Background()
	if err := store.Put(ctx, "openai", []byte("secret")); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := store.Delete(ctx, "openai"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.Get(ctx, "openai"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v after delete, want ErrNotFound", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read store directory: %v", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), fileSuffix) {
			t.Errorf("the credential file %s is still there", entry.Name())
		}
	}
}

func TestDeleteMissingReference(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)
	if err := store.Delete(context.Background(), "absent"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

// TestUnsafeReferencesAreRejected covers the path traversal defence: a
// reference is a flat name, and anything that could point outside the store
// is refused instead of rewritten.
func TestUnsafeReferencesAreRejected(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, "credentials")
	store, err := New(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	refs := []string{
		"",
		"..",
		"../evil",
		"../../evil",
		"..\\evil",
		"a/b",
		`a\b`,
		`C:\x`,
		"/etc/passwd",
		"a..b",
		strings.Repeat("a", maxRefLength+1),
	}
	ctx := context.Background()
	for _, ref := range refs {
		if err := store.Put(ctx, ref, []byte("secret")); err == nil {
			t.Errorf("put accepted the reference %q", ref)
		}
		if _, err := store.Get(ctx, ref); err == nil {
			t.Errorf("get accepted the reference %q", ref)
		}
		if err := store.Delete(ctx, ref); err == nil {
			t.Errorf("delete accepted the reference %q", ref)
		}
	}
	// Nothing may have been written, neither inside the store nor next to
	// it.
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read root: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() != filepath.Base(dir) {
			t.Errorf("the rejected reference wrote %s outside the store", entry.Name())
		}
	}
	inside, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read store directory: %v", err)
	}
	for _, entry := range inside {
		t.Errorf("the rejected reference wrote %s into the store", entry.Name())
	}
}

func TestReferenceAtTheLengthLimitIsAccepted(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)
	ctx := context.Background()
	ref := strings.Repeat("a", maxRefLength)
	if err := store.Put(ctx, ref, []byte("secret")); err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, err := store.Get(ctx, ref); err != nil {
		t.Fatalf("get: %v", err)
	}
}

// TestReferencesThatSanitizeAlikeStayApart makes sure that the hash suffix
// keeps two references apart even when they end up with the same name.
func TestReferencesThatSanitizeAlikeStayApart(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)
	ctx := context.Background()
	if err := store.Put(ctx, "a:b", []byte("first")); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := store.Put(ctx, "a?b", []byte("second")); err != nil {
		t.Fatalf("put: %v", err)
	}
	for ref, want := range map[string]string{"a:b": "first", "a?b": "second"} {
		got, err := store.Get(ctx, ref)
		if err != nil {
			t.Fatalf("get %q: %v", ref, err)
		}
		if string(got) != want {
			t.Errorf("get %q = %q, want %q", ref, got, want)
		}
	}
}

// TestSecretIsNotStoredInClearText is the point of the package: what reaches
// the disk must not contain the secret.
func TestSecretIsNotStoredInClearText(t *testing.T) {
	t.Parallel()
	store, dir := newTestStore(t)
	secret := []byte("sk-live-do-not-write-me")
	if err := store.Put(context.Background(), "openai", secret); err != nil {
		t.Fatalf("put: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read store directory: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("the store wrote no file")
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		if bytes.Contains(data, secret) {
			t.Errorf("%s holds the secret in clear text", entry.Name())
		}
	}
}

// TestCorruptFileDoesNotLeakItsContent keeps the error of a broken file free
// of the bytes that were in it.
func TestCorruptFileDoesNotLeakItsContent(t *testing.T) {
	t.Parallel()
	store, dir := newTestStore(t)
	ctx := context.Background()
	if err := store.Put(ctx, "openai", []byte("secret")); err != nil {
		t.Fatalf("put: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read store directory: %v", err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), fileSuffix) {
			continue
		}
		garbage := []byte("this is not a credential")
		if err := os.WriteFile(filepath.Join(dir, entry.Name()), garbage, 0o600); err != nil {
			t.Fatalf("corrupt %s: %v", entry.Name(), err)
		}
		_, err := store.Get(ctx, "openai")
		if err == nil {
			t.Fatal("a corrupt credential file was accepted")
		}
		if errors.Is(err, ErrNotFound) {
			t.Fatal("a corrupt credential file was reported as missing")
		}
		if strings.Contains(err.Error(), string(garbage)) {
			t.Errorf("the error repeats the content of the file: %v", err)
		}
	}
}

// TestConcurrentAccess runs writers and a reader over one reference. The
// reader must always see one of the values, never a half written file.
func TestConcurrentAccess(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)
	ctx := context.Background()
	values := [][]byte{[]byte("value-0"), []byte("value-1"), []byte("value-2")}
	if err := store.Put(ctx, "shared", values[0]); err != nil {
		t.Fatalf("put: %v", err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := range values {
		wg.Add(1)
		go func(value []byte) {
			defer wg.Done()
			for round := 0; round < 25; round++ {
				if err := store.Put(ctx, "shared", value); err != nil {
					errs <- fmt.Errorf("put: %w", err)
					return
				}
			}
		}(values[i])
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for round := 0; round < 50; round++ {
			got, err := store.Get(ctx, "shared")
			if err != nil {
				errs <- fmt.Errorf("get: %w", err)
				return
			}
			known := false
			for _, value := range values {
				if bytes.Equal(got, value) {
					known = true
					break
				}
			}
			if !known {
				errs <- fmt.Errorf("get returned %q", got)
				return
			}
		}
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// TestConcurrentReferences gives every goroutine a reference of its own.
func TestConcurrentReferences(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ref := fmt.Sprintf("provider-%d", id)
			secret := []byte(fmt.Sprintf("secret-%d", id))
			for round := 0; round < 20; round++ {
				if err := store.Put(ctx, ref, secret); err != nil {
					errs <- fmt.Errorf("put %q: %w", ref, err)
					return
				}
				got, err := store.Get(ctx, ref)
				if err != nil {
					errs <- fmt.Errorf("get %q: %w", ref, err)
					return
				}
				if !bytes.Equal(got, secret) {
					errs <- fmt.Errorf("get %q = %q, want %q", ref, got, secret)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
