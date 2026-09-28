package docedit

import (
	"strings"
	"testing"
)

func edit(t *testing.T, ext, in string, ops ...func(Doc) error) string {
	t.Helper()
	d, err := Open([]byte(in), ext)
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range ops {
		if err := op(d); err != nil {
			t.Fatal(err)
		}
	}
	return string(d.Bytes())
}

func TestYAMLSetString(t *testing.T) {
	in := "repo:\n  template_version: 0.3.0   # old\n  visibility: 'private'\n"
	got := edit(t, ".yaml", in,
		func(d Doc) error { return d.SetString([]string{"repo", "template_version"}, "0.4.0") },
		func(d Doc) error { return d.SetString([]string{"repo", "visibility"}, "it's") },
	)
	want := "repo:\n  template_version: 0.4.0   # old\n  visibility: 'it''s'\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	d, _ := Open([]byte("a:\n  b: |\n    x\n"), ".yaml")
	if err := d.SetString([]string{"a", "b"}, "y"); err == nil {
		t.Error("a block scalar must be refused")
	}
}

func TestYAMLPut(t *testing.T) {
	repo := Map{{"template_version", "0.4.0"}, {"visibility", "private"}, {"runner", []string{"self-hosted", "linux"}}}
	in := "# yaml-language-server: $schema=https://example.org/s.json\n# my manifest\nenvironment:\n  layout: {}\n"
	got := edit(t, ".yaml", in, func(d Doc) error { return d.Put(nil, "repo", repo, true) })
	want := "# yaml-language-server: $schema=https://example.org/s.json\nrepo:\n  template_version: 0.4.0\n  visibility: private\n  runner: [self-hosted, linux]\n# my manifest\nenvironment:\n  layout: {}\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	got = edit(t, ".yaml", "$schema: x\nenvironment: {}\n", func(d Doc) error { return d.Put(nil, "repo", Map{{"template_version", "0.4.0"}}, true) })
	if want := "$schema: x\nrepo:\n  template_version: 0.4.0\nenvironment: {}\n"; got != want {
		t.Errorf("after $schema, got:\n%s\nwant:\n%s", got, want)
	}
	got = edit(t, ".yaml", "environment:\n  layout: {}\n", func(d Doc) error { return d.Put([]string{"environment", "layout"}, "store", "~/s", false) })
	if want := "environment:\n  layout:\n    store: ~/s\n"; got != want {
		t.Errorf("into {}, got:\n%s\nwant:\n%s", got, want)
	}
	d, _ := Open([]byte("repo: {}\n"), ".yaml")
	if err := d.Put(nil, "repo", "x", false); err == nil {
		t.Error("an existing key must be refused")
	}
}

// A scalar that would read back as a different type (a long digit string, a
// reserved word) is quoted so it round-trips as a string.
func TestYAMLPutQuotesAmbiguousScalars(t *testing.T) {
	got := edit(t, ".yaml", "environment: {}\n", func(d Doc) error {
		return d.Put([]string{"environment"}, "vendor", Map{{"name", "a"}, {"rev", "1234567890123456789012345678901234567890"}, {"path", "yes"}}, false)
	})
	want := "environment:\n  vendor:\n    name: a\n    rev: \"1234567890123456789012345678901234567890\"\n    path: \"yes\"\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestYAMLCRLF(t *testing.T) {
	got := edit(t, ".yaml", "environment:\r\n  vendor: a\r\n", func(d Doc) error {
		return d.Put([]string{"environment"}, "layout", "b", false)
	})
	if strings.Count(got, "\r\n") != strings.Count(got, "\n") {
		t.Errorf("mixed line endings:\n%q", got)
	}
}

// #55: a JSON file indented with tabs or four spaces keeps that indentation
// after an edit; only the changed value's line differs.
func TestJSONKeepsIndent(t *testing.T) {
	cases := map[string]string{
		"tabs":        "{\n\t\"a\": {\n\t\t\"b\": \"x\",\n\t\t\"c\": \"keep\"\n\t}\n}\n",
		"four spaces": "{\n    \"a\": {\n        \"b\": \"x\",\n        \"c\": \"keep\"\n    }\n}\n",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			got := edit(t, ".json", in, func(d Doc) error {
				return d.SetString([]string{"a", "b"}, "y")
			})
			want := strings.Replace(in, `"x"`, `"y"`, 1)
			if got != want {
				t.Errorf("got:\n%q\nwant:\n%q", got, want)
			}
		})
	}
}

func TestJSON(t *testing.T) {
	in := `{
  "$schema": "https://example.org/s.json?a=1&b=2",
  "environment": {
    "layout": {"ignore": ["tool<x>&*"], "n": 1.50, "ok": true, "none": null, "store": "old"}
  }
}`
	got := edit(t, ".json", in,
		func(d Doc) error {
			return d.Put(nil, "repo", Map{{"template_version", "0.4.0"}, {"runner", []string{"a"}}}, true)
		},
		func(d Doc) error { return d.SetString([]string{"environment", "layout", "store"}, "<new>") },
	)
	want := `{
  "$schema": "https://example.org/s.json?a=1&b=2",
  "repo": {
    "template_version": "0.4.0",
    "runner": [
      "a"
    ]
  },
  "environment": {
    "layout": {
      "ignore": [
        "tool<x>&*"
      ],
      "n": 1.50,
      "ok": true,
      "none": null,
      "store": "<new>"
    }
  }
}
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	got = edit(t, ".json", got, func(d Doc) error { return d.Remove([]string{"environment", "layout", "store"}) })
	if strings.Contains(got, `"store"`) {
		t.Errorf("after remove:\n%s", got)
	}
	got = edit(t, ".json", "", func(d Doc) error { return d.Put(nil, "environment", Map{{"vendor", []string{"b"}}}, false) })
	if !strings.HasPrefix(got, "{\n  \"environment\": {\n    \"vendor\": [\n") {
		t.Errorf("empty file:\n%s", got)
	}
}

func TestDirective(t *testing.T) {
	const url = "https://example.org/v1/s.json"
	cases := []struct{ ext, in, want string }{
		{".toml", "# note\n[repo]\n", "#:schema " + url + "\n# note\n[repo]\n"},
		{".toml", "#:schema old\n# note\n[repo]\n", "#:schema " + url + "\n# note\n[repo]\n"},
		{".toml", "# note\n\n#:schema old\r\n[repo]\n", "# note\n\n#:schema " + url + "\r\n[repo]\n"},
		// Below the first key Taplo ignores it: a new one goes on top.
		{".toml", "a = 1\n#:schema old\n", "#:schema " + url + "\na = 1\n#:schema old\n"},
		{".yaml", "a: 1\n", "# yaml-language-server: $schema=" + url + "\na: 1\n"},
		{".yaml", "# c\n# yaml-language-server: $schema=old\na: 1\n", "# c\n# yaml-language-server: $schema=" + url + "\na: 1\n"},
		{".json", `{"a": 1}`, "{\n  \"$schema\": \"" + url + "\",\n  \"a\": 1\n}\n"},
		{".json", `{"a": 1, "$schema": "old"}`, "{\n  \"a\": 1,\n  \"$schema\": \"" + url + "\"\n}\n"},
	}
	for _, c := range cases {
		out, err := SetDirective([]byte(c.in), c.ext, url)
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != c.want {
			t.Errorf("SetDirective(%s %q):\ngot  %q\nwant %q", c.ext, c.in, out, c.want)
		}
		if got, ok := Directive(out, c.ext); !ok || got != url {
			t.Errorf("Directive(%q) = %q, %v", out, got, ok)
		}
	}
	if _, ok := Directive([]byte("a = 1\n#:schema x\n"), ".toml"); ok {
		t.Error("a #:schema line below a key is not a directive")
	}
}

// Anchors, aliases and tags move the positions the parser reports; such
// nodes are refused instead of being edited at the wrong place.
func TestYAMLRefusesAnchorsAndTags(t *testing.T) {
	for _, in := range []string{
		"environment: &e\n  vendor:\n    - name: a\n",
		"environment: !!map\n  vendor:\n    - name: a\n",
	} {
		d, err := Open([]byte(in), ".yaml")
		if err != nil {
			t.Fatal(err)
		}
		if err := d.Remove([]string{"environment", "vendor"}); err == nil || !strings.Contains(err.Error(), "anchor, alias or tag") {
			t.Errorf("remove on %q: %v", in, err)
		}
	}
	d, _ := Open([]byte("repo:\n  template_version: &h 0.3.0\n"), ".yaml")
	if err := d.SetString([]string{"repo", "template_version"}, "0.4.0"); err == nil {
		t.Error("an anchored scalar must be refused")
	}
}

func TestYAMLEdgeCases(t *testing.T) {
	// An empty document still nests new keys.
	got := edit(t, ".yaml", "---\n", func(d Doc) error { return d.Put(nil, "repo", Map{{"template_version", "0.4.0"}}, true) })
	if got != "---\nrepo:\n  template_version: 0.4.0\n" {
		t.Errorf("empty document:\n%q", got)
	}
	// A byte order mark is kept and does not shift the columns.
	got = edit(t, ".yaml", "\ufeffmanifest: \"a\"\n", func(d Doc) error { return d.SetString([]string{"manifest"}, "b") })
	if got != "\ufeffmanifest: \"b\"\n" {
		t.Errorf("BOM:\n%q", got)
	}
	// A comment after a tab; a # inside a plain value is not a comment.
	got = edit(t, ".yaml", "repo:\n  template_version: 0.3.0\t# old\n  x: a#b # c\n",
		func(d Doc) error { return d.SetString([]string{"repo", "template_version"}, "0.4.0") },
		func(d Doc) error { return d.SetString([]string{"repo", "x"}, "y") })
	if got != "repo:\n  template_version: 0.4.0\t# old\n  x: \"y\" # c\n" && got != "repo:\n  template_version: 0.4.0\t# old\n  x: y # c\n" {
		t.Errorf("comments:\n%q", got)
	}
}

// Remove with a key deletes a mapping entry; the last one leaves {}.
func TestRemoveKey(t *testing.T) {
	for ext, c := range map[string]struct{ in, one, none string }{
		".yaml": {
			in:   "# top\nuser:\n  deps:\n    a:\n      repo: x/a # keep\n    b:\n      repo: x/b\n  other: 1\n",
			one:  "# top\nuser:\n  deps:\n    b:\n      repo: x/b\n  other: 1\n",
			none: "# top\nuser:\n  deps: {}\n  other: 1\n",
		},
		".json": {
			in:   `{"user": {"deps": {"a": {"repo": "x/a"}, "b": {"repo": "x/b"}}, "other": "1"}}`,
			one:  "{\n  \"user\": {\n    \"deps\": {\n      \"b\": {\n        \"repo\": \"x/b\"\n      }\n    },\n    \"other\": \"1\"\n  }\n}\n",
			none: "{\n  \"user\": {\n    \"deps\": {},\n    \"other\": \"1\"\n  }\n}\n",
		},
	} {
		d, err := Open([]byte(c.in), ext)
		if err != nil {
			t.Fatal(err)
		}
		if err := d.Remove([]string{"user", "deps", "a"}); err != nil {
			t.Fatalf("%s: %v", ext, err)
		}
		if got := string(d.Bytes()); got != c.one {
			t.Errorf("%s:\n%s\nwant:\n%s", ext, got, c.one)
		}
		d, _ = Open(d.Bytes(), ext)
		if err := d.Remove([]string{"user", "deps", "b"}); err != nil {
			t.Fatalf("%s: %v", ext, err)
		}
		if got := string(d.Bytes()); got != c.none {
			t.Errorf("%s:\n%s\nwant:\n%s", ext, got, c.none)
		}
		if err := d.Remove([]string{"user", "deps", "zzz"}); err == nil {
			t.Errorf("%s: a missing key must fail", ext)
		}
	}
}

// Remove on a key whose value is an indentless sequence (items dashed at the
// key's own column, not indented under it) takes the whole list with it.
func TestRemoveKeyIndentlessSequence(t *testing.T) {
	in := "environment:\n  vendor:\n  - name: a\n  layout: {}\n"
	got := edit(t, ".yaml", in, func(d Doc) error { return d.Remove([]string{"environment", "vendor"}) })
	want := "environment:\n  layout: {}\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}
