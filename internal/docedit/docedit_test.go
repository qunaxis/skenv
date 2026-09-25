package docedit

import (
	"strings"
	"testing"
)

const sha = "0123456789abcdef0123456789abcdef01234567"

func vendorItem(name string) Map {
	return Map{{"name", name}, {"repo", "ext/tools"}, {"path", "tools/" + name}, {"rev", sha}}
}

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

func appendVendor(name string) func(Doc) error {
	return func(d Doc) error { return d.Append([]string{"environment", "vendor"}, vendorItem(name)) }
}

func TestYAMLAppend(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"after the last item, keeping comments and blank lines": {
			in: `# yaml-language-server: $schema=https://example.org/s.json
# my manifest
environment:
  # where skills live
  layout:
    store: ~/.skills

  vendor:
    - name: a   # first
      repo: x/y
      rev: "` + sha + `"

    # trailing comment
host: {}
`,
			want: `# yaml-language-server: $schema=https://example.org/s.json
# my manifest
environment:
  # where skills live
  layout:
    store: ~/.skills

  vendor:
    - name: a   # first
      repo: x/y
      rev: "` + sha + `"
    - name: b
      repo: ext/tools
      path: tools/b
      rev: ` + sha + `

    # trailing comment
host: {}
`,
		},
		"indentless sequence": {
			in:   "environment:\n  vendor:\n  - name: a\n    repo: x/y\n  layout: {}\n",
			want: "environment:\n  vendor:\n  - name: a\n    repo: x/y\n  - name: b\n    repo: ext/tools\n    path: tools/b\n    rev: " + sha + "\n  layout: {}\n",
		},
		"empty flow list": {
			in:   "environment:\n  vendor: []  # none yet\n",
			want: "environment:\n  vendor:  # none yet\n    - name: b\n      repo: ext/tools\n      path: tools/b\n      rev: " + sha + "\n",
		},
		"missing key": {
			in:   "environment:\n  layout:\n    store: x\n# end\n",
			want: "environment:\n  layout:\n    store: x\n  vendor:\n    - name: b\n      repo: ext/tools\n      path: tools/b\n      rev: " + sha + "\n# end\n",
		},
		"missing section": {
			in:   "repo:\n    harness: 0.4.0\n",
			want: "repo:\n    harness: 0.4.0\nenvironment:\n    vendor:\n        - name: b\n          repo: ext/tools\n          path: tools/b\n          rev: " + sha + "\n",
		},
		"empty document": {
			in:   "# only a comment",
			want: "# only a comment\nenvironment:\n  vendor:\n    - name: b\n      repo: ext/tools\n      path: tools/b\n      rev: " + sha + "\n",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := edit(t, ".yaml", c.in, appendVendor("b")); got != c.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, c.want)
			}
		})
	}
}

func TestYAMLQuotesAmbiguousScalars(t *testing.T) {
	got := edit(t, ".yaml", "environment: {}\n", func(d Doc) error {
		return d.Append([]string{"environment", "vendor"}, Map{{"name", "a"}, {"rev", "1234567890123456789012345678901234567890"}, {"path", "yes"}})
	})
	want := "environment:\n  vendor:\n    - name: a\n      rev: \"1234567890123456789012345678901234567890\"\n      path: \"yes\"\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestYAMLFlowListIsRefused(t *testing.T) {
	d, err := Open([]byte("environment:\n  vendor: [{name: a}]\n"), ".yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Append([]string{"environment", "vendor"}, vendorItem("b")); err == nil || !strings.Contains(err.Error(), "flow style") {
		t.Errorf("err = %v", err)
	}
}

func TestYAMLSetString(t *testing.T) {
	in := "repo:\n  harness: 0.3.0   # old\n  visibility: 'private'\nenvironment:\n  vendor:\n    - name: a\n      rev: \"aaaa\" # pinned\n"
	got := edit(t, ".yaml", in,
		func(d Doc) error { return d.SetString([]any{"repo", "harness"}, "0.4.0") },
		func(d Doc) error { return d.SetString([]any{"repo", "visibility"}, "it's") },
		func(d Doc) error { return d.SetString([]any{"environment", "vendor", 0, "rev"}, sha) },
	)
	want := "repo:\n  harness: 0.4.0   # old\n  visibility: 'it''s'\nenvironment:\n  vendor:\n    - name: a\n      rev: \"" + sha + "\" # pinned\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	d, _ := Open([]byte("a:\n  b: |\n    x\n"), ".yaml")
	if err := d.SetString([]any{"a", "b"}, "y"); err == nil {
		t.Error("a block scalar must be refused")
	}
}

func TestYAMLRemove(t *testing.T) {
	in := `environment:
  vendor:
    # first one
    - name: a
      repo: x/y

    - name: b
      repo: x/z
  layout: {}
`
	got := edit(t, ".yaml", in, func(d Doc) error { return d.Remove([]any{"environment", "vendor", 0}) })
	want := `environment:
  vendor:
    # first one

    - name: b
      repo: x/z
  layout: {}
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	got = edit(t, ".yaml", want, func(d Doc) error { return d.Remove([]any{"environment", "vendor", 0}) })
	want = "environment:\n  vendor: []\n    # first one\n\n  layout: {}\n"
	if got != want {
		t.Errorf("last item, got:\n%s\nwant:\n%s", got, want)
	}
}

func TestYAMLPut(t *testing.T) {
	repo := Map{{"harness", "0.4.0"}, {"visibility", "private"}, {"runner", []string{"self-hosted", "linux"}}}
	in := "# yaml-language-server: $schema=https://example.org/s.json\n# my manifest\nenvironment:\n  layout: {}\n"
	got := edit(t, ".yaml", in, func(d Doc) error { return d.Put(nil, "repo", repo, true) })
	want := "# yaml-language-server: $schema=https://example.org/s.json\nrepo:\n  harness: 0.4.0\n  visibility: private\n  runner: [self-hosted, linux]\n# my manifest\nenvironment:\n  layout: {}\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	got = edit(t, ".yaml", "$schema: x\nenvironment: {}\n", func(d Doc) error { return d.Put(nil, "repo", Map{{"harness", "0.4.0"}}, true) })
	if want := "$schema: x\nrepo:\n  harness: 0.4.0\nenvironment: {}\n"; got != want {
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

func TestYAMLCRLF(t *testing.T) {
	got := edit(t, ".yaml", "environment:\r\n  vendor:\r\n    - name: a\r\n", appendVendor("b"))
	if strings.Count(got, "\r\n") != strings.Count(got, "\n") {
		t.Errorf("mixed line endings:\n%q", got)
	}
}

func TestJSON(t *testing.T) {
	in := `{
  "$schema": "https://example.org/s.json?a=1&b=2",
  "environment": {
    "vendor": [],
    "layout": {"ignore": ["tool<x>&*"], "n": 1.50, "ok": true, "none": null}
  }
}`
	got := edit(t, ".json", in,
		appendVendor("b"),
		func(d Doc) error {
			return d.Put(nil, "repo", Map{{"harness", "0.4.0"}, {"runner", []string{"a"}}}, true)
		},
		func(d Doc) error { return d.SetString([]any{"environment", "vendor", 0, "rev"}, "<new>") },
	)
	want := `{
  "$schema": "https://example.org/s.json?a=1&b=2",
  "repo": {
    "harness": "0.4.0",
    "runner": [
      "a"
    ]
  },
  "environment": {
    "vendor": [
      {
        "name": "b",
        "repo": "ext/tools",
        "path": "tools/b",
        "rev": "<new>"
      }
    ],
    "layout": {
      "ignore": [
        "tool<x>&*"
      ],
      "n": 1.50,
      "ok": true,
      "none": null
    }
  }
}
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	got = edit(t, ".json", got, func(d Doc) error { return d.Remove([]any{"environment", "vendor", 0}) })
	if !strings.Contains(got, `"vendor": [],`) {
		t.Errorf("after remove:\n%s", got)
	}
	got = edit(t, ".json", "", appendVendor("b"))
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
		"environment:\n  vendor: &v\n    - name: a\n      rev: x\n",
		"environment:\n  vendor: !!seq\n    - name: a\n      rev: x\n",
		"environment:\n  vendor: !!seq\n  - name: a\n    rev: x\n  - name: b\n",
	} {
		for name, op := range map[string]func(Doc) error{
			"append": appendVendor("b"),
			"remove": func(d Doc) error { return d.Remove([]any{"environment", "vendor", 0}) },
		} {
			d, err := Open([]byte(in), ".yaml")
			if err != nil {
				t.Fatal(err)
			}
			if err := op(d); err == nil || !strings.Contains(err.Error(), "anchor, alias or tag") {
				t.Errorf("%s on %q: %v", name, in, err)
			}
		}
	}
	d, _ := Open([]byte("repo:\n  harness: &h 0.3.0\n"), ".yaml")
	if err := d.SetString([]any{"repo", "harness"}, "0.4.0"); err == nil {
		t.Error("an anchored scalar must be refused")
	}
}

func TestYAMLEdgeCases(t *testing.T) {
	// An empty document still nests new keys.
	got := edit(t, ".yaml", "---\n", func(d Doc) error { return d.Put(nil, "repo", Map{{"harness", "0.4.0"}}, true) })
	if got != "---\nrepo:\n  harness: 0.4.0\n" {
		t.Errorf("empty document:\n%q", got)
	}
	// A byte order mark is kept and does not shift the columns.
	got = edit(t, ".yaml", "\ufeffmanifest: \"a\"\n", func(d Doc) error { return d.SetString([]any{"manifest"}, "b") })
	if got != "\ufeffmanifest: \"b\"\n" {
		t.Errorf("BOM:\n%q", got)
	}
	// A comment after a tab; a # inside a plain value is not a comment.
	got = edit(t, ".yaml", "repo:\n  harness: 0.3.0\t# old\n  x: a#b # c\n",
		func(d Doc) error { return d.SetString([]any{"repo", "harness"}, "0.4.0") },
		func(d Doc) error { return d.SetString([]any{"repo", "x"}, "y") })
	if got != "repo:\n  harness: 0.4.0\t# old\n  x: \"y\" # c\n" && got != "repo:\n  harness: 0.4.0\t# old\n  x: y # c\n" {
		t.Errorf("comments:\n%q", got)
	}
}
