// Package gitx runs git, the only runtime dependency of skenv.
package gitx

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

var userinfoRe = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^/@\s]+@`)

// Mask hides credentials embedded in URLs ("https://user:token@host" →
// "https://***@host") so they never reach the output (N5).
func Mask(s string) string { return userinfoRe.ReplaceAllString(s, "${1}***@") }

// Git runs git commands with a fixed environment.
type Git struct {
	// Env is appended to the process environment.
	Env []string
}

// Error is a failed git invocation with masked arguments and output.
type Error struct {
	Args   []string
	Dir    string
	Output string
	Err    error
}

func (e *Error) Error() string {
	msg := fmt.Sprintf("git %s", Mask(strings.Join(e.Args, " ")))
	if e.Dir != "" {
		msg += " (in " + e.Dir + ")"
	}
	msg += ": " + e.Err.Error()
	if out := strings.TrimSpace(e.Output); out != "" {
		msg += ": " + Mask(out)
	}
	return msg
}

func (e *Error) Unwrap() error { return e.Err }

// Run executes git in dir and returns trimmed stdout.
func (g Git) Run(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := g.Output(ctx, dir, nil, args...)
	return strings.TrimSpace(string(out)), err
}

// Output executes git in dir with stdin as its input and returns stdout as
// it is: for binary content and batch output.
func (g Git) Output(ctx context.Context, dir string, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// Never block on a credential prompt: autostart runs without a terminal.
	cmd.Env = append(append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C"), g.Env...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		out := stderr.String()
		if out == "" {
			out = stdout.String()
		}
		return nil, &Error{Args: args, Dir: dir, Output: out, Err: err}
	}
	return stdout.Bytes(), nil
}

// OK runs git and reports only whether it succeeded.
func (g Git) OK(ctx context.Context, dir string, args ...string) bool {
	_, err := g.Run(ctx, dir, args...)
	return err == nil
}

// Available reports whether git is on PATH.
func Available() error {
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git not found on PATH: install git, it is the only runtime dependency of skenv")
	}
	return nil
}
