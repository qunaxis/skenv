package cli

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra/doc"
)

var docsOut = flag.String("docs-out", "", "write the command reference into this directory")

func TestGenDocs(t *testing.T) {
	if *docsOut == "" {
		t.Skip("no -docs-out")
	}
	root := NewRoot(nil)
	if err := os.MkdirAll(*docsOut, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := doc.GenMarkdownTree(root, *docsOut); err != nil {
		t.Fatal(err)
	}
	_ = filepath.Join
}
