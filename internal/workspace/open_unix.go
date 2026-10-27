//go:build !windows

package workspace

import (
	"os"
	"syscall"
)

func openAt(root *os.Root, path string) (*os.File, error) {
	// Opening a FIFO must not hold an HTTP worker waiting for a writer.
	return root.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
