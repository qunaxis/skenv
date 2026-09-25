// Package schemas holds the JSON Schemas of skenv's files, generated from
// the Go types by `make schemas` (internal/tools/genschemas), and the URLs
// they are published at.
//
// A file skenv writes names its schema in a directive (TOML "#:schema",
// the YAML yaml-language-server comment, JSON "$schema"), so editors
// validate and complete it without any setup. The URL is pinned to a skenv
// release: https://qunaxis.github.io/skenv/schemas/v<X.Y.Z>/<name>.
// Development builds write the unversioned URL of the latest release.
package schemas

import (
	"embed"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/qunaxis/skenv/internal/buildinfo"
	"github.com/qunaxis/skenv/internal/docedit"
)

// Schema file names.
const (
	Skenv  = "skenv.schema.json"  // the skenv file: [repo] and [environment]
	Config = "config.schema.json" // the tool config ~/.config/skenv/config.*
)

// Names lists the schemas in the order `skenv schema` documents them.
var Names = []string{Skenv, Config}

// Base is where the documentation site serves the schemas.
const Base = "https://qunaxis.github.io/skenv/schemas/"

//go:embed *.schema.json
var files embed.FS

// Get returns the embedded schema name, as committed: its "$id" is the
// unversioned URL.
func Get(name string) ([]byte, bool) {
	b, err := files.ReadFile(name)
	return b, err == nil
}

// Stamped returns the embedded schema with "$id" set to its URL for
// version ("" for the latest).
func Stamped(name, version string) ([]byte, bool) {
	b, ok := Get(name)
	if !ok {
		return nil, false
	}
	return SetID(b, name, version), true
}

// SetID replaces the unversioned "$id" of a generated schema with the URL
// for version. The site build does the same with sed for every release.
func SetID(schema []byte, name, version string) []byte {
	from := `"$id": "` + URL(name, "") + `"`
	return []byte(strings.Replace(string(schema), from, `"$id": "`+URL(name, version)+`"`, 1))
}

var releaseRe = regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)

// Release returns version without its "v" when it is a skenv release
// ("v0.4.0", "0.4.0", "0.5.0-rc.1"), and "" for anything else, such as the
// development builds 0.0.0-dev+<sha>.
func Release(version string) string {
	v := strings.TrimPrefix(version, "v")
	if !releaseRe.MatchString(v) || strings.HasPrefix(v, "0.0.0-") {
		return ""
	}
	return v
}

// Running is Release of the running skenv.
func Running() string { return Release(buildinfo.Get().Version) }

// First is the first skenv release that publishes schemas; older versions
// have none on the site.
const First = "0.4.0"

// URL returns the URL of schema name for version: pinned to v<version>
// for a release since First, the latest otherwise.
func URL(name, version string) string {
	if v := Release(version); v != "" && !older(v, First) {
		return Base + "v" + v + "/" + name
	}
	return Base + name
}

// older reports whether release a precedes b by major.minor.patch.
func older(a, b string) bool {
	num := func(v string) [3]int {
		var n [3]int
		core, _, _ := strings.Cut(v, "-")
		for i, p := range strings.SplitN(core, ".", 3) {
			n[i], _ = strconv.Atoi(p)
		}
		return n
	}
	x, y := num(a), num(b)
	for i := range x {
		if x[i] != y[i] {
			return x[i] < y[i]
		}
	}
	return false
}

// ParseURL reports whether url is a skenv schema URL, and which schema and
// version ("" for the latest) it names.
func ParseURL(url string) (name, version string, ok bool) {
	rest, found := strings.CutPrefix(url, Base)
	if !found {
		return "", "", false
	}
	name = rest
	if dir, file, nested := strings.Cut(rest, "/"); nested {
		v, isVersion := strings.CutPrefix(dir, "v")
		if !isVersion || Release(v) != v {
			return "", "", false
		}
		name, version = file, v
	}
	if !slices.Contains(Names, name) {
		return "", "", false
	}
	return name, version, true
}

// Stamp returns data with the directive for schema name at version. A
// directive with a skenv schema URL is moved to that version; a directive
// with any other URL is the user's choice and stays; a missing one is added
// only with add.
func Stamp(data []byte, ext, name, version string, add bool) ([]byte, error) {
	want := URL(name, version)
	cur, ok := docedit.Directive(data, ext)
	if ok {
		if _, _, ours := ParseURL(cur); !ours || cur == want {
			return data, nil
		}
	} else if !add {
		return data, nil
	}
	return docedit.SetDirective(data, ext, want)
}

// Check describes what is wrong with the directive of data for schema name
// at version, "" when nothing is: it is missing, or names another skenv
// schema or version. A URL outside skenv's is the user's choice and passes.
func Check(data []byte, ext, name, version string) string {
	want := URL(name, version)
	cur, ok := docedit.Directive(data, ext)
	switch {
	case !ok:
		return "no schema directive for editors (" + directiveHint(ext) + "); `skenv repo apply` adds it"
	case cur == want:
		return ""
	}
	if _, _, ours := ParseURL(cur); !ours {
		return ""
	}
	return "the schema directive points at " + cur + ", expected " + want + "; `skenv repo apply` updates it"
}

func directiveHint(ext string) string {
	switch ext {
	case ".toml":
		return "a first line #:schema <url>"
	case ".json":
		return `a top-level "$schema" key`
	}
	return "a first line # yaml-language-server: $schema=<url>"
}
