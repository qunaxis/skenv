package schemagen

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"go.yaml.in/yaml/v3"

	"github.com/qunaxis/skenv/internal/mdscan"
	"github.com/qunaxis/skenv/schemas"
)

// docLangs are the fenced-block info strings a reader might copy as a
// configuration file.
var docLangs = []string{"toml", "yaml", "yml", "json"}

// docFiles returns README.md, the docs tree except docs/commands (generated
// by `make docs`, covered by its own tests) and docs/adr (design records,
// not user-facing config), and the skenv skill, in a stable order.
func docFiles(t *testing.T) []string {
	t.Helper()
	excluded := func(dir string) bool {
		return dir == filepath.Join(root, "docs", "commands") || dir == filepath.Join(root, "docs", "adr")
	}
	var files []string
	walk := func(dir string) {
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if excluded(p) {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(p, ".md") {
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	files = append(files, filepath.Join(root, "README.md"))
	walk(filepath.Join(root, "docs"))
	walk(filepath.Join(root, "skills", "skenv"))
	sort.Strings(files)
	return files
}

var (
	tomlSchemaDirectiveRe = regexp.MustCompile(`(?m)^#:schema\s+(\S+)`)
	yamlSchemaDirectiveRe = regexp.MustCompile(`(?m)^#\s*yaml-language-server:\s*\$schema=(\S+)`)
)

// schemaDirective returns the schema URL text declares for editors, in
// whichever form its format uses (see docs/editor-support.md), or "".
func schemaDirective(text, ext string) string {
	switch ext {
	case ".toml":
		if m := tomlSchemaDirectiveRe.FindStringSubmatch(text); m != nil {
			return m[1]
		}
	case ".yaml", ".yml":
		if m := yamlSchemaDirectiveRe.FindStringSubmatch(text); m != nil {
			return m[1]
		}
	case ".json":
		var v struct {
			Schema string `json:"$schema"`
		}
		if json.Unmarshal([]byte(text), &v) == nil {
			return v.Schema
		}
	}
	return ""
}

var directiveVersionRe = regexp.MustCompile(`/v(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?)/(?:` + regexp.QuoteMeta(schemas.Skenv) + `|` + regexp.QuoteMeta(schemas.Config) + `)$`)

// directiveVersion returns the release a #:schema URL pins to, or "" for an
// unversioned URL (schemas.URL's "latest" form, which by construction
// cannot go stale).
func directiveVersion(directiveURL string) string {
	m := directiveVersionRe.FindStringSubmatch(directiveURL)
	if m == nil {
		return ""
	}
	return m[1]
}

// Every skenv file and tool config that a reader can copy from README.md,
// the docs (except the generated command reference and the ADRs) and the
// skenv skill is valid for the current release: for the real parser and
// for the schema it names. Every #:schema URL among them must also pin the
// same release: release.sh runs `go test ./...` before it writes the new
// CHANGELOG.md entry and pushes straight to main (.devloop/process.md:
// nobody but `make release` touches CHANGELOG.md by hand, and a release
// commit touches nothing else), so comparing against "the newest release
// in CHANGELOG.md" would fail on main after every release, until a
// separate follow-up PR bumps the doc pins — pure churn, not a real
// finding. Pinned-but-mutually-inconsistent URLs is the actual bug shape
// (docs/configuration.md and docs/editor-support.md disagreeing), and
// needs no release-time state.
func TestDocExamplesValid(t *testing.T) {
	skenvSchema, cfgSchema := compile(t, schemas.Skenv), compile(t, schemas.Config)

	var skenvCount, cfgCount, skipped, snippets int
	pinnedAt := map[string][]string{} // release -> "file:line" of each #:schema URL naming it
	for _, p := range docFiles(t) {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range mdscan.Blocks(rel, string(data), docLangs...) {
			if b.Skip {
				snippets++
				continue
			}
			ext := "." + b.Lang
			if ext == ".yml" {
				ext = ".yaml"
			}
			where := b.File + ":" + strconv.Itoa(b.Line)
			text := placeholders.Replace(b.Text)

			top, err := decodeTop(text, ext)
			if err != nil {
				t.Errorf("%s: a %s block does not parse: %v\n%s", where, b.Lang, err, text)
				continue
			}

			directive := schemaDirective(text, ext)
			if v := directiveVersion(directive); v != "" {
				pinnedAt[v] = append(pinnedAt[v], where)
			}

			switch classify(top, b.File, directive) {
			case skenvFile:
				skenvCount++
				if err := skenvSchema.Validate(instance(t, text, ext)); err != nil {
					t.Errorf("%s: schema rejects:\n%s\n%v", where, text, err)
				}
				if err := parseSkenv(text, ext); err != nil {
					t.Errorf("%s: skenv rejects:\n%s\n%v", where, text, err)
				}
			case toolConfig:
				cfgCount++
				if err := cfgSchema.Validate(instance(t, text, ext)); err != nil {
					t.Errorf("%s: schema rejects:\n%s\n%v", where, text, err)
				}
				if err := parseConfig(t, text, ext); err != nil {
					t.Errorf("%s: skenv rejects:\n%s\n%v", where, text, err)
				}
			default:
				skipped++
			}
		}
	}
	t.Logf("doc examples: %d skenv file(s), %d tool config(s), %d skipped (not a skenv file or tool config), %d snippet(s) marked incomplete", skenvCount, cfgCount, skipped, snippets)
	if skenvCount < 15 || cfgCount < 2 {
		t.Errorf("only %d skenv file and %d config examples found: the scan may be missing files", skenvCount, cfgCount)
	}
	if len(pinnedAt) > 1 {
		releases := make([]string, 0, len(pinnedAt))
		for v := range pinnedAt {
			releases = append(releases, v)
		}
		sort.Strings(releases)
		var detail strings.Builder
		for _, v := range releases {
			fmt.Fprintf(&detail, "\n  v%s: %s", v, strings.Join(pinnedAt[v], ", "))
		}
		t.Errorf("#:schema URLs pin different releases, so at least one is stale:%s", detail.String())
	}
}

// kind classifies a doc example, per the rules of issue #81.
type kind int

const (
	outOfScope kind = iota
	skenvFile
	toolConfig
)

// classify decides what a candidate block is, from its top-level table, the
// #:schema directive it declares (possibly ""), and the file it is in. It
// does not need top to come from the real parser: schemaDirective and the
// real parsers in TestDocExamplesValid do the actual validation.
func classify(top map[string]any, file, directive string) kind {
	names := func(schema string) bool { return directive == schema || strings.HasSuffix(directive, "/"+schema) }
	_, hasUser := top["user"]
	_, hasProject := top["project"]
	_, hasRepo := top["repository"]
	if names(schemas.Skenv) || hasUser || hasProject || hasRepo {
		return skenvFile
	}
	// A "manifest" key is config's one required-in-practice field (see
	// examples() above), unambiguous enough to classify by even without a
	// directive or the docs/configuration.md location.
	_, hasManifest := top["manifest"]
	if names(schemas.Config) || hasManifest || file == filepath.Join("docs", "configuration.md") {
		return toolConfig
	}
	return outOfScope
}

// TestClassify pins classify's rules against synthetic fixtures, independent
// of whatever the live docs happen to contain today (a scanner regression
// that leaves the current docs tree passing, such as the one that slipped
// past the info-string language split, would otherwise give no red signal).
func TestClassify(t *testing.T) {
	cases := []struct {
		name      string
		top       map[string]any
		file      string
		directive string
		want      kind
	}{
		{"top-level user table", map[string]any{"user": map[string]any{}}, "docs/manifest.md", "", skenvFile},
		{"top-level project table", map[string]any{"project": map[string]any{}}, "docs/project-skills.md", "", skenvFile},
		{"top-level repository table", map[string]any{"repository": map[string]any{}}, "docs/harness.md", "", skenvFile},
		{"#:schema names skenv.schema.json", map[string]any{}, "README.md", "https://qunaxis.github.io/skenv/schemas/v0.6.0/skenv.schema.json", skenvFile},
		{"#:schema names config.schema.json", map[string]any{}, "README.md", "https://qunaxis.github.io/skenv/schemas/v0.6.0/config.schema.json", toolConfig},
		{"manifest key, no directive, outside configuration.md", map[string]any{"manifest": "x"}, "docs/adopting.md", "", toolConfig},
		{"in docs/configuration.md, no magic key or directive", map[string]any{"machine": "x"}, "docs/configuration.md", "", toolConfig},
		{"unrelated top-level keys", map[string]any{"rule": map[string]any{}}, "docs/editor-support.md", "", outOfScope},
		{"pre-0.6 legacy keys are not recognized as a skenv file", map[string]any{"repo": map[string]any{}, "environment": map[string]any{}}, "docs/skenv-file.md", "", outOfScope},
		{"user table wins over docs/configuration.md location", map[string]any{"user": map[string]any{}}, "docs/configuration.md", "", skenvFile},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classify(c.top, c.file, c.directive); got != c.want {
				t.Errorf("classify(%v, %q, %q) = %v, want %v", c.top, c.file, c.directive, got, c.want)
			}
		})
	}
}

// decodeTop decodes text into its top-level table, to classify the example
// and to detect the skenv file's or config's magic keys. It does not need
// to match the real parser's strictness: schemaDirective and the real
// parsers below do that.
func decodeTop(text, ext string) (map[string]any, error) {
	m := map[string]any{}
	var err error
	switch ext {
	case ".toml":
		_, err = toml.Decode(text, &m)
	case ".yaml":
		err = yaml.Unmarshal([]byte(text), &m)
	case ".json":
		err = json.Unmarshal([]byte(text), &m)
	}
	return m, err
}
