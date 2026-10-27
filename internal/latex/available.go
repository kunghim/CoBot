package latex

import (
	"os"
	"path/filepath"
	"runtime"
)

// Available checks provisioned local resources without executing or installing
// anything. Missing packages used by a particular document remain compile errors.
func Available(config Config) bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	for index, name := range []string{config.Compiler, config.Cache, config.Temporary} {
		if !filepath.IsAbs(name) {
			return false
		}
		info, err := os.Stat(name)
		if err != nil {
			return false
		}
		if index == 0 {
			if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
				return false
			}
		} else if !info.IsDir() {
			return false
		}
	}
	_, err := os.Stat("/usr/bin/sandbox-exec")
	return err == nil
}

// Provisioned enables an explicitly supplied offline runtime. Empty paths keep
// preview unavailable; no runtime or resource discovery downloads are attempted.
func Provisioned(compiler, cache, temporary string) (*Config, error) {
	if compiler == "" && cache == "" {
		return nil, nil
	}
	if runtime.GOOS != "darwin" || !filepath.IsAbs(compiler) || !filepath.IsAbs(cache) || !filepath.IsAbs(temporary) {
		return nil, ErrUnavailable
	}
	binary, err := os.Stat(compiler)
	if err != nil || !binary.Mode().IsRegular() || binary.Mode().Perm()&0111 == 0 {
		return nil, ErrUnavailable
	}
	resources, err := os.Stat(cache)
	if err != nil || !resources.IsDir() {
		return nil, ErrUnavailable
	}
	if err := os.MkdirAll(temporary, 0700); err != nil {
		return nil, err
	}
	config := &Config{Compiler: compiler, Cache: cache, Temporary: temporary}
	if !Available(*config) {
		return nil, ErrUnavailable
	}
	return config, nil
}
