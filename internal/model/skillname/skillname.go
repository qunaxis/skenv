// Package skillname holds the one rule for skill names, shared by the
// manifest, `skenv lint`, `skenv new` and the JSON Schema of the skenv file.
//
// The rule follows the Agent Skills specification (agentskills.io): 1 to
// MaxLen characters, lowercase ASCII letters, digits and hyphens, no leading
// or trailing hyphen and no consecutive hyphens.
package skillname

import (
	"errors"
	"fmt"
	"regexp"
)

// Pattern is the regular expression of a skill name. It is written in the
// subset that Go, ECMA-262 and Rust agree on, because the JSON Schema
// carries it to editors.
const Pattern = `^[a-z0-9]+(-[a-z0-9]+)*$`

// MaxLen is the longest allowed name, in characters.
const MaxLen = 64

var re = regexp.MustCompile(Pattern)

// Rule describes the rule in words, for error messages.
const Rule = "lowercase letters, digits and single hyphens, with no hyphen at the start or end"

// Matches reports whether name matches Pattern, without the length limit.
func Matches(name string) bool { return re.MatchString(name) }

// Check reports whether name is a valid skill name.
func Check(name string) error {
	switch {
	case name == "":
		return errors.New("name is required")
	case len(name) > MaxLen:
		return fmt.Errorf("name %q is %d characters, the limit is %d", name, len(name), MaxLen)
	case !re.MatchString(name):
		return fmt.Errorf("name %q must be %s (at most %d characters)", name, Rule, MaxLen)
	}
	return nil
}
