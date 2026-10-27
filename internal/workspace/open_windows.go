package workspace

import "os"

func openAt(root *os.Root, path string) (*os.File, error) {
	return root.Open(path)
}
