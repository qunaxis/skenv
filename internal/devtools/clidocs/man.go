package clidocs

import (
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra/doc"

	"github.com/qunaxis/skenv/internal/cli"
)

// GenerateMan writes one section-1 man page per command into dir
// (skenv.1, skenv-vendor-add.1, ...) and removes pages of commands that
// no longer exist. version goes into the page footer ("skenv 0.4.0");
// a zero date means SOURCE_DATE_EPOCH or, without it, today, so a
// release passes the commit date to keep archives reproducible.
func GenerateMan(dir, version string, date time.Time) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	old, err := filepath.Glob(filepath.Join(dir, "*.1"))
	if err != nil {
		return err
	}
	for _, f := range old {
		if err := os.Remove(f); err != nil {
			return err
		}
	}
	h := &doc.GenManHeader{Section: "1", Source: "skenv", Manual: "skenv manual"}
	if version != "" {
		h.Source = "skenv " + version
	}
	if !date.IsZero() {
		h.Date = &date
	}
	return doc.GenManTree(cli.Command(), h, dir)
}
