package manifest

import (
	"strings"
	"testing"
)

func TestAddEnvironment(t *testing.T) {
	own := &Own{Repo: "me/skills", Path: "~/src/skills"}
	for _, c := range []struct {
		name, ext, in string
		own           *Own
		want          string
	}{
		{name: "new toml", ext: ".toml", own: own,
			want: envLead + "[environment]\n\n" + ownComment + "[[environment.own]]\nrepo = \"me/skills\"\npath = \"~/src/skills\"\n" + ownSelection + "\n" + vendorExample},
		{name: "new toml without a remote", ext: ".toml",
			want: envLead + "[environment]\n\n" + ownComment + ownExample + "\n" + vendorExample},
		{name: "toml with [repo]", ext: ".toml", in: "# keep me\n[repo]\nharness = \"0.4.0\" # and me\nvisibility = \"private\"", own: own,
			want: "# keep me\n[repo]\nharness = \"0.4.0\" # and me\nvisibility = \"private\"\n\n" + envLead + "[environment]\n\n" + ownComment +
				"[[environment.own]]\nrepo = \"me/skills\"\npath = \"~/src/skills\"\n" + ownSelection + "\n" + vendorExample},
		{name: "new yaml", ext: ".yaml", own: own,
			want: envLead + envYAMLHint + "environment:\n  own:\n    - repo: me/skills\n      path: ~/src/skills\n"},
		{name: "new yaml without a remote", ext: ".yaml",
			want: envLead + envYAMLHint + "environment: {}\n"},
		{name: "yaml with repo", ext: ".yml", in: "# keep me\nrepo:\n  harness: 0.4.0 # and me\n  visibility: private\n",
			want: "# keep me\nrepo:\n  harness: 0.4.0 # and me\n  visibility: private\n" + envLead + envYAMLHint + "environment: {}\n"},
		{name: "new json", ext: ".json", own: own,
			want: "{\n  \"environment\": {\n    \"own\": [\n      {\n        \"repo\": \"me/skills\",\n        \"path\": \"~/src/skills\"\n      }\n    ]\n  }\n}\n"},
		{name: "json with repo", ext: ".json", in: `{"$schema": "x", "repo": {"harness": "0.4.0", "visibility": "public"}}`,
			want: "{\n  \"$schema\": \"x\",\n  \"repo\": {\n    \"harness\": \"0.4.0\",\n    \"visibility\": \"public\"\n  },\n  \"environment\": {}\n}\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, err := AddEnvironment([]byte(c.in), c.ext, c.own)
			if err != nil {
				t.Fatal(err)
			}
			if c.want != "" && string(out) != c.want {
				t.Errorf("got:\n%s\nwant:\n%s", out, c.want)
			}
			m, err := Parse(out, c.ext)
			if err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			if (c.own != nil) != (len(m.Own) == 1) || len(m.Vendor) != 0 {
				t.Errorf("manifest = %+v", m)
			}
			if c.own != nil && (m.Own[0].Repo != own.Repo || m.Own[0].Path != own.Path) {
				t.Errorf("own = %+v", m.Own[0])
			}
			// A second call refuses: the section exists.
			if _, err := AddEnvironment(out, c.ext, nil); err == nil || !strings.Contains(err.Error(), "has [environment] already") {
				t.Errorf("second add: %v", err)
			}
		})
	}
}

// A flow-style YAML document is not edited: docedit says how to fix it.
func TestAddEnvironmentFlowYAML(t *testing.T) {
	if _, err := AddEnvironment([]byte("{repo: {harness: 0.4.0, visibility: private}}\n"), ".yaml", nil); err == nil || !strings.Contains(err.Error(), "flow style") {
		t.Errorf("err = %v", err)
	}
}

func TestGitHubRepo(t *testing.T) {
	for remote, want := range map[string]string{
		"https://github.com/me/skills.git":     "me/skills",
		"https://github.com/me/skills":         "me/skills",
		"https://user@github.com/me/skills/":   "me/skills",
		"ssh://git@github.com/me/skills.git":   "me/skills",
		"git@github.com:me/skills.git":         "me/skills",
		"git@GitHub.com:me/sk.ills":            "me/sk.ills",
		"https://gitlab.com/me/skills.git":     "",
		"git@gitlab.com:me/skills.git":         "",
		"file:///tmp/remotes/me/skills.git":    "",
		"/tmp/remotes/me/skills.git":           "",
		"https://github.com/me/skills/sub.git": "",
		"":                                     "",
	} {
		got, ok := GitHubRepo(remote)
		if got != want || ok != (want != "") {
			t.Errorf("GitHubRepo(%q) = %q, %v; want %q", remote, got, ok, want)
		}
	}
}

// Added lines take the line endings of the file.
func TestAddEnvironmentCRLF(t *testing.T) {
	for ext, in := range map[string]string{
		".toml": "# mine\r\n[repo]\r\nharness = \"0.4.0\"\r\nvisibility = \"private\"\r\n",
		".yaml": "# mine\r\nrepo:\r\n  harness: 0.4.0\r\n  visibility: private\r\n",
	} {
		out, err := AddEnvironment([]byte(in), ext, &Own{Repo: "me/skills", Path: "~/src/skills"})
		if err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(string(out), "\n"); n != strings.Count(string(out), "\r\n") {
			t.Errorf("%s: mixed line endings:\n%q", ext, out)
		}
	}
}
