package docedit

import (
	"fmt"
	"path/filepath"
	"strings"
)

// The format of a file skenv creates: TOML, YAML or JSON, chosen with
// --format. A file that exists keeps its format: skenv never converts one,
// and --format that disagrees with it is an error.

// Formats are the values of --format; the first is the default. ".yml" is
// read as YAML but never written.
var Formats = []string{"toml", "yaml", "json"}

// DefaultFormat is the format of a new file without --format.
const DefaultFormat = "toml"

// ValidFormat checks a --format value; "" means not given.
func ValidFormat(format string) error {
	switch format {
	case "", "toml", "yaml", "json":
		return nil
	}
	return fmt.Errorf("--format must be %s, got %q", strings.Join(Formats, ", "), format)
}

// FormatOf returns the format of path from its extension: "toml", "yaml" (for
// .yaml and .yml), "json", or "" for anything else.
func FormatOf(path string) string {
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

// ChooseFormat returns the file to write: existing when there is one, otherwise
// dir/<base>.<format> (TOML without format). An existing file in another
// format than a given format is an error that names both; nothing is
// converted.
func ChooseFormat(dir, base, existing, format string) (string, error) {
	if err := ValidFormat(format); err != nil {
		return "", err
	}
	if existing != "" {
		if format != "" && FormatOf(existing) != format {
			return "", FormatMismatchError(existing, format)
		}
		return existing, nil
	}
	if format == "" {
		format = DefaultFormat
	}
	return filepath.Join(dir, base+"."+format), nil
}

// FormatMismatchError explains that --format disagrees with the file that
// exists.
func FormatMismatchError(existing, format string) error {
	return fmt.Errorf("%s exists and is %s; --format %s does not convert it: drop --format to keep %s, "+
		"or convert the file by hand", existing, strings.ToUpper(FormatOf(existing)), format, filepath.Base(existing))
}
