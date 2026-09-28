package schemagen

import (
	"encoding/json"
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

var changelogVersionRe = regexp.MustCompile(`(?m)^## v(\d+\.\d+\.\d+)\b`)

// currentSchemaVersion is the release these docs describe: the newest one
// in CHANGELOG.md. A doc example's #:schema URL must be pinned to it, not
// to an older release it was copied from.
func currentSchemaVersion(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	m := changelogVersionRe.FindSubmatch(data)
	if m == nil {
		t.Fatal("CHANGELOG.md: no released version found")
	}
	return string(m[1])
}

// Every skenv file and tool config that a reader can copy from README.md,
// the docs (except the generated command reference and the ADRs) and the
// skenv skill is valid for the current release: for the real parser and
// for the schema it names, with a #:schema URL pinned to that release.
func TestDocExamplesValid(t *testing.T) {
	skenvSchema, cfgSchema := compile(t, schemas.Skenv), compile(t, schemas.Config)
	version := currentSchemaVersion(t)
	wantSkenvURL, wantCfgURL := schemas.URL(schemas.Skenv, version), schemas.URL(schemas.Config, version)

	var skenvCount, cfgCount, skipped, snippets int
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
			names := func(schema string) bool { return directive == schema || strings.HasSuffix(directive, "/"+schema) }
			_, hasUser := top["user"]
			_, hasProject := top["project"]
			_, hasRepo := top["repository"]
			isSkenv := names(schemas.Skenv) || hasUser || hasProject || hasRepo
			isConfig := !isSkenv && (names(schemas.Config) || b.File == filepath.Join("docs", "configuration.md"))

			switch {
			case isSkenv:
				skenvCount++
				if directive != "" && directive != wantSkenvURL {
					t.Errorf("%s: #:schema %s, want %s (the schema of the current release)", where, directive, wantSkenvURL)
				}
				if err := skenvSchema.Validate(instance(t, text, ext)); err != nil {
					t.Errorf("%s: schema rejects:\n%s\n%v", where, text, err)
				}
				if err := parseSkenv(text, ext); err != nil {
					t.Errorf("%s: skenv rejects:\n%s\n%v", where, text, err)
				}
			case isConfig:
				cfgCount++
				if directive != "" && directive != wantCfgURL {
					t.Errorf("%s: #:schema %s, want %s (the schema of the current release)", where, directive, wantCfgURL)
				}
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
