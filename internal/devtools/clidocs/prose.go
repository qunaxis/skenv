package clidocs

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// proseIgnoreDirective opts a single following line, or the fenced block it
// opens, out of TestProseCommandsExist: for a deliberate example of wrong
// usage that must not be "fixed".
const proseIgnoreDirective = "<!-- docs-check: ignore-next -->"

// proseRoots, relative to the repository root, are the files and
// directories TestProseCommandsExist scans: the prose the docs checks own
// (README.md, AGENTS.md, docs/, skills/skenv/, as TestNoRetiredTerms and
// the schemagen doc-example tests scan it) and internal/cli/help/, the
// --help text that docs/commands is generated from. Anything else, such
// as .github/, .devloop/ or notes under the gitignored tmp/, is not
// user-facing prose and must not fail the check. proseExcludedDirs are
// skipped inside them: generated output (docs/commands, which mirrors
// --help) and the ADRs, whose history this check does not own.
var (
	proseRoots        = []string{"README.md", "AGENTS.md", "docs", "skills/skenv", "internal/cli/help"}
	proseExcludedDirs = []string{"docs/commands", "docs/adr"}
)

// proseCandidate is one "skenv ..." invocation found in Markdown, ready to
// resolve against the command tree.
type proseCandidate struct {
	file string
	line int
	text string // "$ " stripped; otherwise exactly as written
}

func proseFindingMessage(c proseCandidate, problem string) string {
	return fmt.Sprintf("%s:%d: %s: %s", c.file, c.line, c.text, problem)
}

// proseFiles lists the Markdown files under root that TestProseCommandsExist
// scans, sorted for a stable report.
func proseFiles(root string) ([]string, error) {
	var files []string
	for _, start := range proseRoots {
		found, err := proseFilesIn(root, start)
		if err != nil {
			return nil, err
		}
		files = append(files, found...)
	}
	sort.Strings(files)
	return files, nil
}

// proseFilesIn lists the Markdown files at start, a file or directory
// relative to root, skipping proseExcludedDirs.
func proseFilesIn(root, start string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(filepath.Join(root, filepath.FromSlash(start)), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if slices.Contains(proseExcludedDirs, rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(p) == ".md" {
			files = append(files, rel)
		}
		return nil
	})
	return files, err
}

// fenceOpenRe matches a fenced code block's opening delimiter and captures
// its info string (the language tag, or "" when there is none).
var fenceOpenRe = regexp.MustCompile("^`{3,}\\s*([A-Za-z0-9_-]*)\\s*$")

// fenceCloseRe matches the closing delimiter of a fenced code block.
var fenceCloseRe = regexp.MustCompile("^`{3,}$")

// inlineCodeRe matches inline code spans (single backticks; skenv prose
// never nests backticks inside an inline span).
var inlineCodeRe = regexp.MustCompile("`([^`]+)`")

// proseRelevantFence is a fenced block's candidates rule: shell examples
// (sh, bash, console) or an untagged block, never any other language.
func proseRelevantFence(info string) bool {
	switch info {
	case "", "sh", "bash", "console":
		return true
	default:
		return false
	}
}

// scanProse extracts skenv invocations from one Markdown file's source:
// lines of a relevant fenced code block, and inline code spans that start
// with "skenv ". It returns the candidates in file order and the number of
// docs-check: ignore-next directives it honoured.
func scanProse(file string, src []byte) (cands []proseCandidate, ignored int) {
	lines := strings.Split(string(src), "\n")
	inFence := false
	fenceRelevant := false
	fenceSkip := false
	ignoreNext := false
	for i, raw := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(raw)
		if trimmed == proseIgnoreDirective {
			ignoreNext = true
			continue
		}
		if inFence {
			if fenceCloseRe.MatchString(trimmed) {
				inFence, fenceSkip, ignoreNext = false, false, false
				continue
			}
			if fenceRelevant && !fenceSkip && !ignoreNext {
				if text, ok := proseInvocation(raw); ok {
					cands = append(cands, proseCandidate{file: file, line: lineNum, text: text})
				}
			}
			if ignoreNext {
				ignored++
			}
			ignoreNext = false
			continue
		}
		if m := fenceOpenRe.FindStringSubmatch(trimmed); m != nil {
			inFence = true
			fenceRelevant = proseRelevantFence(m[1])
			fenceSkip = ignoreNext
			if ignoreNext {
				ignored++
			}
			ignoreNext = false
			continue
		}
		if ignoreNext {
			ignored++
			ignoreNext = false
			continue
		}
		for _, m := range inlineCodeRe.FindAllStringSubmatch(raw, -1) {
			if text, ok := proseInvocation(m[1]); ok {
				cands = append(cands, proseCandidate{file: file, line: lineNum, text: text})
			}
		}
	}
	return cands, ignored
}

// proseInvocation recognises a candidate line or inline span as a skenv
// invocation, stripping a leading shell prompt.
func proseInvocation(s string) (string, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "$ ")
	if s == "skenv" || strings.HasPrefix(s, "skenv ") {
		return s, true
	}
	return "", false
}

// proseResolve resolves a "skenv ..." invocation against root: it consumes
// tokens while they name a subcommand of the current command, stopping at
// a placeholder, an argument or the end of the line, then checks every
// flag-shaped token against the resolved command's flags. It returns the
// problems found, nil when the invocation is fine.
func proseResolve(root *cobra.Command, text string) []string {
	tokens := strings.Fields(text)
	if len(tokens) <= 1 {
		return nil // bare "skenv" (or empty, which is not a candidate)
	}
	tokens = tokens[1:]
	if i := proseCommentIndex(tokens); i >= 0 {
		tokens = tokens[:i]
	}
	cur := root
	for _, tok := range tokens {
		if strings.HasPrefix(tok, "-") {
			continue // flags do not affect subcommand resolution
		}
		if proseIsPlaceholder(tok) || len(cur.Commands()) == 0 {
			break // an argument: cur is the resolved command
		}
		if strings.Contains(tok, "|") {
			sub, problem := proseResolveAlternatives(cur, tok)
			if problem != "" {
				return []string{problem}
			}
			cur = sub
			continue
		}
		sub := proseFindSubcommand(cur, tok)
		if sub == nil {
			return []string{fmt.Sprintf("unknown command %q", tok)}
		}
		cur = sub
	}
	var problems []string
	for _, tok := range tokens {
		if !strings.HasPrefix(tok, "-") {
			continue
		}
		if !proseHasFlag(cur, tok) {
			problems = append(problems, fmt.Sprintf("unknown flag %q", tok))
		}
	}
	return problems
}

// proseCommentIndex is the index of the first shell-comment token ("#..."),
// so a trailing "# explanation" never feeds the resolver.
func proseCommentIndex(tokens []string) int {
	for i, tok := range tokens {
		if strings.HasPrefix(tok, "#") {
			return i
		}
	}
	return -1
}

// proseIsPlaceholder reports whether tok is documentation notation for an
// argument, such as <repo>, [name...] or ..., rather than a real word.
func proseIsPlaceholder(tok string) bool {
	return strings.ContainsAny(tok, "<>…[]")
}

// proseResolveAlternatives resolves a "|"-separated token such as
// "add|update|remove", cobra's usage idiom for enumerating a subcommand's
// alternatives (see docs/commands/skenv_repo_init.md's `--visibility
// private|public`), by resolving each alternative against cur
// independently. It returns the last alternative's command, or a problem
// naming the first alternative that does not exist. A Markdown table cell
// escapes "|" as "\|" (see docs/commands.md); that escaping is undone
// before splitting, so a table-cell token resolves the same as an
// unescaped one.
func proseResolveAlternatives(cur *cobra.Command, tok string) (*cobra.Command, string) {
	unescaped := strings.ReplaceAll(tok, `\|`, "|")
	var next *cobra.Command
	for alt := range strings.SplitSeq(unescaped, "|") {
		sub := proseFindSubcommand(cur, alt)
		if sub == nil {
			return nil, fmt.Sprintf("unknown command %q", alt)
		}
		next = sub
	}
	return next, ""
}

func proseFindSubcommand(cur *cobra.Command, name string) *cobra.Command {
	for _, s := range cur.Commands() {
		if s.Name() == name || s.HasAlias(name) {
			return s
		}
	}
	return nil
}

// proseHasFlag reports whether tok (a token starting with "-", "--flag" or
// "--flag=value" already split) names a local, inherited or persistent
// flag of cur, or is -h/--help, which every command accepts.
func proseHasFlag(cur *cobra.Command, tok string) bool {
	name, _, _ := strings.Cut(tok, "=")
	if name == "-h" || name == "--help" {
		return true
	}
	switch {
	case strings.HasPrefix(name, "--"):
		n := strings.TrimPrefix(name, "--")
		return cur.LocalFlags().Lookup(n) != nil || cur.InheritedFlags().Lookup(n) != nil
	case strings.HasPrefix(name, "-") && len(name) == 2:
		n := strings.TrimPrefix(name, "-")
		return cur.LocalFlags().ShorthandLookup(n) != nil || cur.InheritedFlags().ShorthandLookup(n) != nil
	default:
		return false
	}
}
