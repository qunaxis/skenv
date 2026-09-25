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

func TestAddProject(t *testing.T) {
	for _, c := range []struct {
		name, ext, in string
		mirrors       []string
		want          string
	}{
		{name: "new toml", ext: ".toml", mirrors: []string{".claude/skills"},
			want: projectLead + "[project]\nmirrors = [\".claude/skills\"]\n"},
		{name: "toml with [environment]", ext: ".toml", in: "# mine\n[environment]\n",
			want: "# mine\n[environment]\n\n" + projectLead + "[project]\n"},
		{name: "yaml with repo", ext: ".yaml", in: "# keep me\nrepo:\n  visibility: public\n", mirrors: []string{".claude/skills"},
			want: "# keep me\nrepo:\n  visibility: public\n" + projectLead + "project:\n  mirrors: [.claude/skills]\n"},
		{name: "json", ext: ".json", in: `{"repo": {"visibility": "public"}}`,
			want: "{\n  \"repo\": {\n    \"visibility\": \"public\"\n  },\n  \"project\": {}\n}\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, err := AddProject([]byte(c.in), c.ext, c.mirrors)
			if err != nil {
				t.Fatal(err)
			}
			if string(out) != c.want {
				t.Errorf("got:\n%s\nwant:\n%s", out, c.want)
			}
			p, err := ParseProject(out, c.ext)
			if err != nil || p.Dir != DefaultProjectDir || len(p.Mirrors) != len(c.mirrors) {
				t.Errorf("project = %+v, %v", p, err)
			}
			if _, err := AddProject(out, c.ext, nil); err == nil || !strings.Contains(err.Error(), "has [project] already") {
				t.Errorf("second add: %v", err)
			}
		})
	}
}
