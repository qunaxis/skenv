package gitx

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestMask(t *testing.T) {
	cases := map[string]string{
		"https://user:secret@github.com/o/r.git":   "https://***@github.com/o/r.git",
		"fatal: https://x-access-token:t@host/r x": "fatal: https://***@host/r x",
		"git@github.com:o/r.git":                   "git@github.com:o/r.git",
		"https://github.com/o/r":                   "https://github.com/o/r",
	}
	for in, want := range cases {
		if got := Mask(in); got != want {
			t.Errorf("Mask(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestErrorIsMasked(t *testing.T) {
	_, err := Git{}.Run(context.Background(), t.TempDir(), "ls-remote", "https://user:secret@127.0.0.1:1/o/r.git")
	var ge *Error
	if !errors.As(err, &ge) {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("credentials leaked: %v", err)
	}
}
