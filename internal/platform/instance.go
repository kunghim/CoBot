package platform

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ErrAlreadyRunning reports that another live core owns this profile. The
// endpoint lets callers point the user (or desktop shell) at the running
// instance instead of starting a second scheduler.
type ErrAlreadyRunning struct {
	Endpoint string
	PID      int
}

func (e *ErrAlreadyRunning) Error() string {
	return fmt.Sprintf("core already running for this profile (endpoint %s, pid %d)", e.Endpoint, e.PID)
}

// InstanceInfo is the discovery document written to runtime/instance.json.
// It locates a running core but never carries an authorization secret.
type InstanceInfo struct {
	InstanceID   string    `json:"instanceId"`
	ServiceEpoch string    `json:"serviceEpoch"`
	Endpoint     string    `json:"endpoint"`
	Profile      string    `json:"profile"`
	PID          int       `json:"pid"`
	StartedAt    time.Time `json:"startedAt"`
}

// Lock is the environment-level single-instance lock, held until Release.
type Lock struct {
	paths Paths
	file  *os.File
}

// AcquireLock takes the profile lock with create-exclusive semantics. A lock
// file is only removed and retried after the recorded instance proves
// unreachable, so a live core is never silently replaced and two simultaneous
// starts cannot both win.
func AcquireLock(p Paths, probeTimeout time.Duration) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(p.LockFile()), 0o700); err != nil {
		return nil, fmt.Errorf("create runtime directory: %w", err)
	}
	for attempt := 0; attempt < 3; attempt++ {
		f, err := os.OpenFile(p.LockFile(), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			if err := f.Close(); err != nil {
				return nil, fmt.Errorf("close lock file: %w", err)
			}
			return &Lock{paths: p}, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("create lock file: %w", err)
		}
		existing, ok := readLockFile(p.LockFile())
		if ok && probeEndpoint(existing.Endpoint, probeTimeout) {
			return nil, &ErrAlreadyRunning{Endpoint: existing.Endpoint, PID: existing.PID}
		}
		// Stale leftover (crash or corrupt record): only the file is removed,
		// never another process's resources.
		if err := os.Remove(p.LockFile()); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("remove stale lock: %w", err)
		}
	}
	return nil, fmt.Errorf("could not acquire instance lock after retries")
}

// WriteInstance publishes the discovery document after the listener is up.
func (l *Lock) WriteInstance(info InstanceInfo) error {
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("encode instance info: %w", err)
	}
	if err := atomicWrite(l.paths.InstanceFile(), data, 0o600); err != nil {
		return fmt.Errorf("write instance info: %w", err)
	}
	return nil
}

// Release removes every artifact this core owns. It never touches files of
// another instance.
func (l *Lock) Release() {
	_ = os.Remove(l.paths.InstanceFile())
	_ = os.Remove(l.paths.AdminSecretFile())
	_ = os.Remove(l.paths.LockFile())
}

// WriteAdminSecret stores the local runtime credential that lets same-user
// helper processes (desktop shell, duplicate serve) mint page bootstraps.
func WriteAdminSecret(p Paths) (string, error) {
	secret, err := randomSecret()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(p.CredentialsDir(), 0o700); err != nil {
		return "", fmt.Errorf("create credentials directory: %w", err)
	}
	if err := atomicWrite(p.AdminSecretFile(), []byte(secret), 0o600); err != nil {
		return "", fmt.Errorf("write admin secret: %w", err)
	}
	return secret, nil
}

// ReadAdminSecret loads the runtime credential of a running instance.
func ReadAdminSecret(p Paths) (string, error) {
	data, err := os.ReadFile(p.AdminSecretFile())
	if err != nil {
		return "", fmt.Errorf("read admin secret: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// ReadInstance loads the discovery document of a running instance.
func ReadInstance(p Paths) (InstanceInfo, error) {
	data, err := os.ReadFile(p.InstanceFile())
	if err != nil {
		return InstanceInfo{}, fmt.Errorf("read instance info: %w", err)
	}
	var info InstanceInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return InstanceInfo{}, fmt.Errorf("decode instance info: %w", err)
	}
	return info, nil
}

// EndpointAvailable reports whether an instance answers on loopback.
func EndpointAvailable(endpoint string, timeout time.Duration) bool {
	return probeEndpoint(endpoint, timeout)
}

func probeEndpoint(endpoint string, timeout time.Duration) bool {
	if strings.TrimSpace(endpoint) == "" {
		return false
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(endpoint + "/api/v1/status?probe=1")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode < 500
}

func readLockFile(path string) (InstanceInfo, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return InstanceInfo{}, false
	}
	var info InstanceInfo
	if err := json.Unmarshal(data, &info); err != nil {
		// A corrupt lock cannot describe a live instance; treat as stale but
		// only after the recorded endpoint (if any) fails to answer.
		return InstanceInfo{}, false
	}
	return info, true
}

// WriteLockRecord stores the live endpoint inside the lock file so a second
// start can distinguish a live core from a stale leftover.
func (l *Lock) WriteLockRecord(info InstanceInfo) error {
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	return atomicWrite(l.paths.LockFile(), data, 0o600)
}

func atomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func randomSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// FreeLoopbackPort asks the kernel for an unused loopback TCP port.
func FreeLoopbackPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("reserve loopback port: %w", err)
	}
	defer l.Close()
	_, portStr, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(portStr)
}

// IsNotFound reports whether err wraps a missing-file condition.
func IsNotFound(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}
