package platform

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func testPaths(t *testing.T) Paths {
	t.Helper()
	root := filepath.Join(t.TempDir(), "profile")
	p, err := Resolve(EnvDevelopment, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestResolveUsesEnvironmentDefaults(t *testing.T) {
	dev, err := Resolve(EnvDevelopment, "")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dev.Root) != ".cobot-dev" {
		t.Fatalf("development root = %q, want ~/.cobot-dev", dev.Root)
	}
	prod, err := Resolve(EnvProduction, "")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(prod.Root) != ".cobot" {
		t.Fatalf("production root = %q, want ~/.cobot", prod.Root)
	}
	if dev.Root == prod.Root {
		t.Fatal("development and production must not share a root")
	}
}

func TestAcquireLockSecondStartSeesRunningInstance(t *testing.T) {
	p := testPaths(t)
	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer live.Close()

	first, err := AcquireLock(p, time.Second)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	info := InstanceInfo{Endpoint: live.URL, PID: 4242, InstanceID: "i1"}
	if err := first.WriteInstance(info); err != nil {
		t.Fatal(err)
	}
	if err := first.WriteLockRecord(info); err != nil {
		t.Fatal(err)
	}

	_, err = AcquireLock(p, time.Second)
	var already *ErrAlreadyRunning
	if err == nil {
		t.Fatal("second acquire must fail while the instance answers")
	}
	if !errors.As(err, &already) {
		t.Fatalf("want ErrAlreadyRunning, got %v", err)
	}
	if already.Endpoint != live.URL {
		t.Fatalf("endpoint = %q, want %q", already.Endpoint, live.URL)
	}

	first.Release()
	if _, err := os.Stat(p.LockFile()); !os.IsNotExist(err) {
		t.Fatal("release must remove the lock file")
	}
	if _, err := os.Stat(p.InstanceFile()); !os.IsNotExist(err) {
		t.Fatal("release must remove instance.json")
	}
}

func TestAcquireLockTakesOverStaleLeftover(t *testing.T) {
	p := testPaths(t)
	// Simulate a crash: lock + instance record exist but nothing listens.
	stale := InstanceInfo{Endpoint: "http://127.0.0.1:1", PID: 999999}
	data, _ := json.Marshal(stale)
	if err := os.WriteFile(p.LockFile(), data, 0o600); err != nil {
		t.Fatal(err)
	}

	second, err := AcquireLock(p, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("stale takeover must succeed, got %v", err)
	}
	second.Release()
}

func TestInstanceLockIsExclusiveBeforeDiscoveryAndReleaseIsOwned(t *testing.T) {
	p := testPaths(t)
	first, err := AcquireLock(p, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(first.Release)
	second, err := AcquireLock(p, time.Millisecond)
	if err == nil {
		second.Release()
		t.Fatal("stole lock before discovery was published")
	}
	var busy *ErrAlreadyRunning
	if !errors.As(err, &busy) {
		t.Fatal(err)
	}
	first.Release()
	next, err := AcquireLock(p, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Release()
	info := InstanceInfo{PID: os.Getpid(), ServiceEpoch: "next", Endpoint: "http://127.0.0.1:1"}
	if err := next.WriteInstance(info); err != nil {
		t.Fatal(err)
	}
	first.Release()
	got, err := ReadInstance(p)
	if err != nil || got.ServiceEpoch != "next" {
		t.Fatalf("previous owner removed new discovery: %+v %v", got, err)
	}
}

func TestAdminSecretRoundTrip(t *testing.T) {
	p := testPaths(t)
	secret, err := WriteAdminSecret(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(secret) < 32 {
		t.Fatalf("secret too short: %d", len(secret))
	}
	got, err := ReadAdminSecret(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != secret {
		t.Fatal("round trip mismatch")
	}
}

func TestConcurrentInstanceLockAcquisitionHasOneOwner(t *testing.T) {
	p := testPaths(t)
	start := make(chan struct{})
	results := make(chan *Lock, 12)
	var wait sync.WaitGroup
	for range 12 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			l, err := AcquireLock(p, time.Second)
			if err == nil {
				results <- l
			}
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	var locks []*Lock
	for l := range results {
		locks = append(locks, l)
	}
	defer func() {
		for _, l := range locks {
			l.Release()
		}
	}()
	if len(locks) != 1 {
		t.Fatalf("concurrent owners=%d", len(locks))
	}
}
