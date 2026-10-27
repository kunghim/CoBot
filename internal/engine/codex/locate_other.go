//go:build !darwin && !windows

package codex

import "errors"

func locatePlatformBinary() (string, error) {
	return "", errors.New("automatic ChatGPT desktop discovery is not supported on this platform; specify its bundled engine path")
}
