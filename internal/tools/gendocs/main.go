// Command gendocs writes the command reference: go run ./internal/tools/gendocs docs/commands
package main

import (
	"fmt"
	"os"

	"github.com/qunaxis/skenv/internal/clidocs"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: gendocs DIR")
		os.Exit(2)
	}
	if err := clidocs.Generate(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "gendocs:", err)
		os.Exit(1)
	}
}
