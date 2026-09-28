package clidocs

import (
	"regexp"
	"strings"
)

// The help text is plain text for the terminal and the man pages. In the
// Markdown reference, codeFormat wraps its technical tokens in backticks
// (flags, environment variables, paths, file names, [sections], quoted
// commands and keys). A word with a <placeholder> is code too: raw, GitHub
// drops <placeholder> as an unknown HTML tag.

var (
	quoted       = regexp.MustCompile(`"([^"\n]+)"`)
	word         = regexp.MustCompile(`\S+`)
	quotedCode   = regexp.MustCompile(`^(skenv( [a-z][a-z0-9-]*)*|[a-z][a-z0-9_-]*|\.)$`)
	leadingPunct = regexp.MustCompile(`^[("']+`)
	trailPunct   = regexp.MustCompile(`[.,;:!?)"']+$`)
	codeWord     = []*regexp.Regexp{
		regexp.MustCompile(`^--?[A-Za-z][A-Za-z0-9-]*(=\S*)?$`),                                      // --rev, -h, --dir=x
		regexp.MustCompile(`^\$[A-Za-z_][A-Za-z0-9_]*$`),                                             // $SKENV_DENYLIST
		regexp.MustCompile(`^[A-Z][A-Z0-9]*_[A-Z0-9_<>]+$`),                                          // SKENV_MANIFEST, SKENV_<KEY>
		regexp.MustCompile(`<[A-Za-z][^<>\s]*>`),                                                     // ./<repo>, skills/<name>/
		regexp.MustCompile(`^(~/|\.\.?/|/[\w.~-])`),                                                  // ~/..., ./x, /etc/x
		regexp.MustCompile(`^[\w.-]+(/[\w.-]+)*/$`),                                                  // references/
		regexp.MustCompile(`^(\*|[\w-]+[\w.-]*)\.(toml|ya?ml|json|md|txt|log|fish|sh|bash|zsh|go)$`), // SKILL.md, *.md
		regexp.MustCompile(`^\.[A-Za-z][\w.-]*$`),                                                    // .gitignore
		regexp.MustCompile(`^[a-z][a-z0-9_-]+(\.[a-z][a-z0-9_-]+)+$`),                                // repo.harness, not e.g.
		regexp.MustCompile(`^\[[A-Za-z][\w.-]*\]$`),                                                  // [repo]
	}
)

// codeFormat formats help text as Markdown. Lines indented by a tab or four
// spaces are code blocks and text in backticks is a code span already;
// both stay as they are.
func codeFormat(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "    ") {
			continue
		}
		parts := strings.Split(line, "`")
		for j := 0; j < len(parts); j += 2 {
			parts[j] = formatProse(parts[j])
		}
		lines[i] = strings.Join(parts, "`")
	}
	return strings.Join(lines, "\n")
}

// formatProse formats text outside code spans: quoted commands and keys
// first, then single words.
func formatProse(s string) string {
	var b strings.Builder
	last := 0
	for _, m := range quoted.FindAllStringSubmatchIndex(s, -1) {
		inner := s[m[2]:m[3]]
		if !quotedCode.MatchString(inner) {
			continue
		}
		b.WriteString(formatWords(s[last:m[0]]))
		b.WriteString("`" + inner + "`")
		last = m[1]
	}
	b.WriteString(formatWords(s[last:]))
	return b.String()
}

func formatWords(s string) string {
	return word.ReplaceAllStringFunc(s, func(w string) string {
		lead := leadingPunct.FindString(w)
		core := w[len(lead):]
		trail := trailPunct.FindString(core)
		core = core[:len(core)-len(trail)]
		for _, re := range codeWord {
			if core != "" && re.MatchString(core) {
				return lead + "`" + core + "`" + trail
			}
		}
		return w
	})
}
