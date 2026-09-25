// Package cliexample reads the examples of a skenv command (cobra's
// Example field) and their recorded output. The example tests in
// internal/cli record the output of each runnable example as a golden file
// under docs/commands/examples; the reference generator (internal/clidocs)
// embeds it under the example. The skenv binary does not link this package.
package cliexample

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Example is one invocation in a command's Example field. Lines starting
// with "# " describe the invocation below them.
type Example struct {
	Comment string // the "# " lines above the invocation, joined by spaces
	Line    string // the invocation as shown, "skenv doctor"
}

// Parse splits an Example field into its invocations.
func Parse(field string) []Example {
	var out []Example
	var comment []string
	for _, line := range strings.Split(field, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
		case strings.HasPrefix(line, "#"):
			comment = append(comment, strings.TrimSpace(strings.TrimPrefix(line, "#")))
		default:
			out = append(out, Example{Comment: strings.Join(comment, " "), Line: line})
			comment = nil
		}
	}
	return out
}

// Args returns the arguments after "skenv" of a plain invocation. A line
// with shell syntax (a redirection, a pipe, a variable, quotes) is not
// run by the example tests and has no recorded output.
func (e Example) Args() ([]string, bool) {
	if strings.ContainsAny(e.Line, `<>|&;$"'()*`+"`") {
		return nil, false
	}
	f := strings.Fields(e.Line)
	if len(f) == 0 || f[0] != "skenv" {
		return nil, false
	}
	return f[1:], true
}

// Dir is the directory of the recorded output of a command's examples,
// relative to the reference directory (docs/commands): examples/skenv_vendor_add.
func Dir(commandPath string) string {
	return filepath.Join("examples", strings.ReplaceAll(commandPath, " ", "_"))
}

// File is the recorded output of the n-th example (1-based) of a command,
// relative to the reference directory.
func File(commandPath string, n int) string {
	return filepath.Join(Dir(commandPath), strconv.Itoa(n)+".txt")
}

// Output is what an example printed: stdout and stderr in the order they
// were written, as a terminal shows them, and the exit code.
type Output struct {
	Code int
	Text string
}

// Format is the golden file: an "exit <code>" line, then the output.
func (o Output) Format() string {
	return fmt.Sprintf("exit %d\n%s", o.Code, o.Text)
}

// Read reads a golden file; ok is false when it does not exist.
func Read(path string) (o Output, ok bool, err error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Output{}, false, nil
	}
	if err != nil {
		return Output{}, false, err
	}
	head, text, _ := strings.Cut(string(b), "\n")
	code, found := strings.CutPrefix(head, "exit ")
	if !found {
		return Output{}, false, fmt.Errorf("%s: first line is not \"exit <code>\"", path)
	}
	if o.Code, err = strconv.Atoi(code); err != nil {
		return Output{}, false, fmt.Errorf("%s: %w", path, err)
	}
	o.Text = text
	return o, true, nil
}
