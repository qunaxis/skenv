// Command genschemas writes the JSON Schemas of skenv's files, generated
// from the Go types (internal/schemagen).
//
//	go run ./internal/tools/genschemas schemas               # make schemas
//	go run ./internal/tools/genschemas -version 0.4.0 DIR    # "$id" of a release
//
// Run it from the module root: descriptions come from the doc comments in
// the sources.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/qunaxis/skenv/internal/schemagen"
)

func main() {
	version := flag.String("version", "", `skenv release for "$id" (default: the unversioned URL)`)
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: genschemas [-version V] DIR")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*version, flag.Arg(0)); err != nil {
		fmt.Fprintln(os.Stderr, "genschemas:", err)
		os.Exit(1)
	}
}

func run(version, dir string) error {
	files, err := schemagen.Generate(".", version)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
