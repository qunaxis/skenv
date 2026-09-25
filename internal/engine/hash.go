package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// treeHash is the content hash of the skill directory dir, as git would
// store it: every regular file (path, executable bit, content) and symlink
// (path, target), in path order, without the .skenv marker at the top and
// without .git. Empty directories and other file modes do not count, so a
// fresh clone of a project gives the same hash as the copy skenv made.
func treeHash(dir string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		switch {
		case rel == ".":
			return nil
		case d.Name() == ".git":
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		case rel == markerName:
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			fmt.Fprintf(h, "link %q %q\n", rel, filepath.ToSlash(link))
		case info.Mode().IsRegular():
			sum, err := fileSum(p)
			if err != nil {
				return err
			}
			kind := "file"
			if info.Mode().Perm()&0o100 != 0 {
				kind = "exec"
			}
			fmt.Fprintf(h, "%s %q %s\n", kind, rel, sum)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func fileSum(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
