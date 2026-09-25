// Package fileformat picks the format of a file skenv creates: TOML, YAML
// or JSON, chosen with --format. A file that exists keeps its format:
// skenv never converts one, and --format that disagrees with it is an
// error.
package fileformat

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Names are the values of --format; the first is the default. ".yml" is
// read as YAML but never written.
var Names = []string{"toml", "yaml", "json"}

// Default is the format of a new file without --format.
const Default = "toml"

// Valid checks a --format value; "" means not given.
func Valid(format string) error {
	switch format {
	case "", "toml", "yaml", "json":
		return nil
	}
	return fmt.Errorf("--format must be %s, got %q", strings.Join(Names, ", "), format)
}

// Of returns the format of path from its extension: "toml", "yaml" (for
// .yaml and .yml), "json", or "" for anything else.
func Of(path string) string {
	switch filepath.Ext(path) {
	case ".toml":
		return "toml"
	case ".yaml", ".yml":
		return "yaml"
	case ".json":
		return "json"
	}
	return ""
}

// Choose returns the file to write: existing when there is one, otherwise
// dir/<base>.<format> (TOML without format). An existing file in another
// format than a given format is an error that names both; nothing is
// converted.
func Choose(dir, base, existing, format string) (string, error) {
	if err := Valid(format); err != nil {
		return "", err
	}
	if existing != "" {
		if format != "" && Of(existing) != format {
			return "", MismatchError(existing, format)
		}
		return existing, nil
	}
	if format == "" {
		format = Default
	}
	return filepath.Join(dir, base+"."+format), nil
}

// MismatchError explains that --format disagrees with the file that
// exists.
func MismatchError(existing, format string) error {
	return fmt.Errorf("%s exists and is %s; --format %s does not convert it: drop --format to keep %s, "+
		"or convert the file by hand", existing, strings.ToUpper(Of(existing)), format, filepath.Base(existing))
}
