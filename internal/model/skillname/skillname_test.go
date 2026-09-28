package skillname

import (
	"strings"
	"testing"
)

func TestCheck(t *testing.T) {
	for _, name := range []string{"a", "foo-bar", "a1-2b", strings.Repeat("a", MaxLen)} {
		if err := Check(name); err != nil {
			t.Errorf("Check(%q) = %v, want nil", name, err)
		}
	}
	for _, name := range []string{"", "foo.bar", "foo_bar", "-foo", "foo-", "foo--bar", "Foo", "foo bar", "фу",
		strings.Repeat("a", MaxLen+1)} {
		if err := Check(name); err == nil {
			t.Errorf("Check(%q) = nil, want an error", name)
		}
	}
}
