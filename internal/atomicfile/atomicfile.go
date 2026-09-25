// Package atomicfile replaces files atomically (N2).
package atomicfile

import (
	"io/fs"
	"os"
	"path/filepath"
)

// Write replaces file with data via a temporary file in the same directory
// and a rename, so readers never see a partial file.
func Write(file string, data []byte, mode fs.FileMode) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(file), "."+filepath.Base(file)+".tmp-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Chmod(mode); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), file)
}
