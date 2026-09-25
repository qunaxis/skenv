package cli

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
)

// kongMarkdown renders one command node as Markdown (spike: kong has no
// built-in doc generator, so this walks kong's model).
func kongMarkdown(n *kong.Node) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n\n%s\n\n```\nskenv %s\n```\n\n", n.FullPath(), n.Help, n.Summary())
	if n.Detail != "" {
		fmt.Fprintf(&b, "%s\n\n", n.Detail)
	}
	if len(n.Positional) > 0 {
		b.WriteString("| Argument | Description |\n|---|---|\n")
		for _, p := range n.Positional {
			fmt.Fprintf(&b, "| `%s` | %s |\n", p.Summary(), p.Help)
		}
		b.WriteString("\n")
	}
	b.WriteString("| Flag | Default | Description |\n|---|---|---|\n")
	for _, f := range n.Flags {
		if f.Hidden {
			continue
		}
		name := "--" + f.Name
		if !f.IsBool() {
			name += "=" + f.FormatPlaceHolder()
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s |\n", name, f.Default, strings.ReplaceAll(f.Help, "|", `\|`))
	}
	return b.String()
}

func TestSpikeKongMarkdown(t *testing.T) {
	var c kongCLI
	p := kong.Must(&c, kong.Name("skenv"))
	for _, n := range p.Model.Leaves(false) {
		if n.FullPath() == "skenv vendor add" {
			if os.Getenv("SPIKE_DOC") != "" {
				fmt.Print(kongMarkdown(n))
			}
			return
		}
	}
	t.Fatal("vendor add not found")
}
