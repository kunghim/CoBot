package latex

import (
	"errors"
	"io/fs"
	"path/filepath"
)

var ErrOutputLimit = errors.New("LaTeX output exceeds preview limits")

func checkOutput(directory string) error {
	var size int64
	count := 0
	return filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		count++
		if count > 256 || entry.Type()&fs.ModeSymlink != 0 {
			return ErrOutputLimit
		}
		if !entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return ErrOutputLimit
			}
			size += info.Size()
			if size > 64<<20 {
				return ErrOutputLimit
			}
		}
		return nil
	})
}
