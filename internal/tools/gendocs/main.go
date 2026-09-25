// Command gendocs writes the command reference from the cobra command tree.
//
//	go run ./internal/tools/gendocs docs/commands            # Markdown
//	go run ./internal/tools/gendocs -man [-version V] [-date RFC3339] man
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/qunaxis/skenv/internal/clidocs"
)

func main() {
	man := flag.Bool("man", false, "write section-1 man pages instead of Markdown")
	version := flag.String("version", "", "skenv version for the man page footer")
	date := flag.String("date", "", "man page date, RFC 3339 (default: SOURCE_DATE_EPOCH or today)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: gendocs [-man [-version V] [-date RFC3339]] DIR")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*man, *version, *date, flag.Arg(0)); err != nil {
		fmt.Fprintln(os.Stderr, "gendocs:", err)
		os.Exit(1)
	}
}

func run(man bool, version, date, dir string) error {
	if !man {
		// The reference directory also holds the recorded example output.
		return clidocs.Generate(dir, dir)
	}
	var d time.Time
	if date != "" {
		var err error
		if d, err = time.Parse(time.RFC3339, date); err != nil {
			return fmt.Errorf("-date: %w", err)
		}
	}
	return clidocs.GenerateMan(dir, version, d)
}
