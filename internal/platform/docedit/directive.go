package docedit

import (
	"fmt"
	"regexp"
	"strings"
)

// SchemaKey is the top-level key of a JSON file that names its schema.
const SchemaKey = "$schema"

var (
	// #:schema <url>, understood by Taplo (Even Better TOML and others).
	tomlDirectiveRe = regexp.MustCompile(`^#:schema\s+(\S+)\s*$`)
	// # yaml-language-server: $schema=<url>
	yamlDirectiveRe = regexp.MustCompile(`^\s*#\s*yaml-language-server:\s*\$schema=(\S+)\s*$`)
)

// Directive returns the schema URL a file names for editors: the
// "#:schema" line at the top of TOML, the yaml-language-server comment of
// YAML, the top-level "$schema" of JSON.
func Directive(data []byte, ext string) (string, bool) {
	switch ext {
	case ".toml":
		if i, url := tomlDirective(lines(data)); i >= 0 {
			return url, true
		}
	case ".yaml", ".yml":
		if i, url := yamlDirective(lines(data)); i >= 0 {
			return url, true
		}
	case ".json":
		d, err := openJSON(data)
		if err != nil {
			return "", false
		}
		if _, v := d.root.get(SchemaKey); v != nil && v.isStr {
			return v.str, true
		}
	}
	return "", false
}

// SetDirective returns data with its schema directive set to url: an
// existing directive is replaced in place, a missing one is added as the
// first line (TOML, YAML) or the first key (JSON).
func SetDirective(data []byte, ext, url string) ([]byte, error) {
	if strings.ContainsAny(url, " \t\r\n") {
		return nil, fmt.Errorf("docedit: schema URL %q contains white space", url)
	}
	nl := "\n"
	if strings.Contains(string(data), "\r\n") {
		nl = "\r\n"
	}
	switch ext {
	case ".toml", ".yaml", ".yml":
		ls := lines(data)
		var i int
		var line string
		if ext == ".toml" {
			i, _ = tomlDirective(ls)
			line = "#:schema " + url
		} else {
			i, _ = yamlDirective(ls)
			line = "# yaml-language-server: $schema=" + url
		}
		if i >= 0 {
			ending := ls[i][len(strings.TrimRight(ls[i], "\r\n")):]
			ls[i] = line + ending
		} else {
			ls = append([]string{line + nl}, ls...)
		}
		return []byte(strings.Join(ls, "")), nil
	case ".json":
		d, err := openJSON(data)
		if err != nil {
			return nil, err
		}
		if _, v := d.root.get(SchemaKey); v != nil {
			if err := d.SetString([]any{SchemaKey}, url); err != nil {
				return nil, err
			}
		} else if err := d.Put(nil, SchemaKey, url, true); err != nil {
			return nil, err
		}
		return d.Bytes(), nil
	}
	return nil, fmt.Errorf("docedit: unsupported format %q", ext)
}

func lines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	ls := strings.SplitAfter(string(data), "\n")
	if ls[len(ls)-1] == "" {
		ls = ls[:len(ls)-1]
	}
	return ls
}

// tomlDirective finds "#:schema" among the comments and blank lines at the
// top of the file, where Taplo reads it.
func tomlDirective(ls []string) (int, string) {
	for i, l := range ls {
		t := strings.TrimRight(l, "\r\n")
		if m := tomlDirectiveRe.FindStringSubmatch(t); m != nil {
			return i, m[1]
		}
		if !isBlankOrComment(t) {
			break
		}
	}
	return -1, ""
}

// yamlDirective finds the yaml-language-server comment on a line of its
// own.
func yamlDirective(ls []string) (int, string) {
	for i, l := range ls {
		if m := yamlDirectiveRe.FindStringSubmatch(strings.TrimRight(l, "\r\n")); m != nil {
			return i, m[1]
		}
	}
	return -1, ""
}
