//go:build windows

package secrets

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The wait for a file that another handle still holds.
const (
	renameAttempts = 20
	renameDelay    = 5 * time.Millisecond
)

// dpapiEntropy scopes a DPAPI blob to CoBot. It is not a secret: the DPAPI
// key of the user is what protects the blob, the entropy only keeps a blob
// written by another program from being opened here.
var dpapiEntropy = []byte("cobot/credentials/v1")

// dpapiCodec seals a secret with DPAPI and AES-GCM.
//
// DPAPI binds the blob to the Windows user that wrote it: another account on
// the same machine cannot open it, and a lost user profile takes the secrets
// with it. CoBot accepts that in exchange for not running a key store of
// its own.
type dpapiCodec struct{}

// newPlatformCodec returns the Windows codec. The store directory holds no
// key material: DPAPI keeps it in the user profile.
func newPlatformCodec(_ string) (codec, error) {
	return dpapiCodec{}, nil
}

// seal encrypts plaintext under a fresh key and wraps the result in a DPAPI
// envelope.
func (dpapiCodec) seal(plaintext []byte) ([]byte, error) {
	// A key per credential keeps one broken file from exposing the others,
	// and the key itself only exists inside the DPAPI envelope.
	key := make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("credentials: generate key: %w", err)
	}
	nonce, ciphertext, err := sealSecret(key, plaintext)
	if err != nil {
		return nil, err
	}
	inner := make([]byte, 0, 1+len(key)+len(nonce)+len(ciphertext))
	inner = append(inner, formatVersion)
	inner = append(inner, key...)
	inner = append(inner, nonce...)
	inner = append(inner, ciphertext...)
	return dpapiProtect(inner)
}

// open unwraps the DPAPI envelope and decrypts the secret inside it.
func (dpapiCodec) open(sealed []byte) ([]byte, error) {
	inner, err := dpapiUnprotect(sealed)
	if err != nil {
		return nil, err
	}
	if len(inner) < 1+keySize+nonceSize+gcmOverhead {
		return nil, errors.New("credentials: credential file is truncated")
	}
	if inner[0] != formatVersion {
		return nil, errors.New("credentials: unsupported credential format")
	}
	key := inner[1 : 1+keySize]
	nonce := inner[1+keySize : 1+keySize+nonceSize]
	return openSecret(key, nonce, inner[1+keySize+nonceSize:])
}

// dpapiProtect encrypts data with the DPAPI key of the current Windows user.
func dpapiProtect(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("credentials: nothing to protect")
	}
	name, err := windows.UTF16PtrFromString("CoBot credential")
	if err != nil {
		return nil, fmt.Errorf("credentials: build data description: %w", err)
	}
	entropy := append([]byte(nil), dpapiEntropy...)
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	ent := windows.DataBlob{Size: uint32(len(entropy)), Data: &entropy[0]}
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, name, &ent, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, fmt.Errorf("credentials: protect data: %w", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return blobBytes(out), nil
}

// dpapiUnprotect decrypts data with the DPAPI key of the current Windows
// user.
func dpapiUnprotect(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("credentials: nothing to unprotect")
	}
	entropy := append([]byte(nil), dpapiEntropy...)
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	ent := windows.DataBlob{Size: uint32(len(entropy)), Data: &entropy[0]}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, &ent, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, fmt.Errorf("credentials: unprotect data: %w", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return blobBytes(out), nil
}

// blobBytes copies the bytes a DPAPI call allocated. The caller frees the
// original with LocalFree.
func blobBytes(blob windows.DataBlob) []byte {
	if blob.Size == 0 || blob.Data == nil {
		return nil
	}
	return bytes.Clone(unsafe.Slice(blob.Data, blob.Size))
}

// readFile reads a credential file.
//
// Go opens files without FILE_SHARE_DELETE, so a plain read would make a
// concurrent overwrite fail with "Access is denied". The handle below allows
// the file to be renamed or deleted while the read is in flight.
func readFile(path string) ([]byte, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("credentials: build path: %w", err)
	}
	handle, err := windows.CreateFile(
		name,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	defer windows.CloseHandle(handle)
	var out bytes.Buffer
	buf := make([]byte, 32*1024)
	for {
		var done uint32
		if err := windows.ReadFile(handle, buf, &done, nil); err != nil {
			return nil, &os.PathError{Op: "read", Path: path, Err: err}
		}
		if done == 0 {
			return out.Bytes(), nil
		}
		out.Write(buf[:done])
	}
}

// renameOver replaces the target with the source.
//
// Windows refuses to replace a file that another handle still holds, so a
// reader or a second writer can turn a write into a failure. Retrying turns
// that refusal into a short wait.
func renameOver(from, to string) error {
	var err error
	for attempt := 0; attempt < renameAttempts; attempt++ {
		if err = os.Rename(from, to); err == nil {
			return nil
		}
		if !errors.Is(err, windows.ERROR_ACCESS_DENIED) && !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			return err
		}
		time.Sleep(renameDelay)
	}
	return err
}
