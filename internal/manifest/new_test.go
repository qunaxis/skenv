package manifest

import (
	"strings"
	"testing"
)

func TestAddUser(t *testing.T) {
	own := &Checkout{ID: "skills", Repo: "me/skills", CheckoutDir: "."}
	for _, c := range []struct {
		name, ext, in string
		own           *Checkout
		want          string
	}{
		{name: "new toml", ext: ".toml", own: own,
			want: userLead + "[user]\n\n" + checkoutComment + "[user.checkouts.skills]\nrepo         = \"me/skills\"\ncheckout_dir = \".\"\n" + checkoutSelection + "\n" + dependencyExample},
		{name: "new toml without a remote", ext: ".toml",
			want: userLead + "[user]\n\n" + checkoutComment + checkoutExample + "\n" + dependencyExample},
		{name: "toml with [repository]", ext: ".toml", in: "# keep me\n[repository]\ntemplate_version = \"0.4.0\" # and me\nvisibility = \"private\"", own: own,
			want: "# keep me\n[repository]\ntemplate_version = \"0.4.0\" # and me\nvisibility = \"private\"\n\n" + userLead + "[user]\n\n" + checkoutComment +
				"[user.checkouts.skills]\nrepo         = \"me/skills\"\ncheckout_dir = \".\"\n" + checkoutSelection + "\n" + dependencyExample},
		{name: "new yaml", ext: ".yaml", own: own,
			want: userLead + userYAMLHint + "user:\n  checkouts:\n    skills:\n      repo: me/skills\n      checkout_dir: .\n"},
		{name: "new yaml without a remote", ext: ".yaml",
			want: userLead + userYAMLHint + "user: {}\n"},
		{name: "yaml with repository", ext: ".yml", in: "# keep me\nrepository:\n  template_version: 0.4.0 # and me\n  visibility: private\n",
			want: "# keep me\nrepository:\n  template_version: 0.4.0 # and me\n  visibility: private\n" + userLead + userYAMLHint + "user: {}\n"},
		{name: "new json", ext: ".json", own: own,
			want: "{\n  \"user\": {\n    \"checkouts\": {\n      \"skills\": {\n        \"repo\": \"me/skills\",\n        \"checkout_dir\": \".\"\n      }\n    }\n  }\n}\n"},
		{name: "json with repository", ext: ".json", in: `{"$schema": "x", "repository": {"template_version": "0.4.0", "visibility": "public"}}`,
			want: "{\n  \"$schema\": \"x\",\n  \"repository\": {\n    \"template_version\": \"0.4.0\",\n    \"visibility\": \"public\"\n  },\n  \"user\": {}\n}\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, err := AddUser([]byte(c.in), c.ext, c.own)
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
			if (c.own != nil) != (len(m.Checkouts) == 1) || len(m.Dependencies) != 0 {
				t.Errorf("manifest = %+v", m)
			}
			if got := m.Checkouts["skills"]; c.own != nil && (got.Repo != own.Repo || got.CheckoutDir != own.CheckoutDir) {
				t.Errorf("checkout = %+v", got)
			}
			// A second call refuses: the section exists.
			if _, err := AddUser(out, c.ext, nil); err == nil || !strings.Contains(err.Error(), "has [user] already") {
				t.Errorf("second add: %v", err)
			}
		})
	}
}

// A flow-style YAML document is not edited: docedit says how to fix it.
func TestAddUserFlowYAML(t *testing.T) {
	if _, err := AddUser([]byte("{repository: {template_version: 0.4.0, visibility: private}}\n"), ".yaml", nil); err == nil || !strings.Contains(err.Error(), "flow style") {
		t.Errorf("err = %v", err)
	}
}

// Added lines take the line endings of the file.
func TestAddUserCRLF(t *testing.T) {
	for ext, in := range map[string]string{
		".toml": "# mine\r\n[repository]\r\ntemplate_version = \"0.4.0\"\r\nvisibility = \"private\"\r\n",
		".yaml": "# mine\r\nrepository:\r\n  template_version: 0.4.0\r\n  visibility: private\r\n",
	} {
		out, err := AddUser([]byte(in), ext, &Checkout{ID: "skills", Repo: "me/skills", CheckoutDir: "."})
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
		{name: "toml with [user]", ext: ".toml", in: "# mine\n[user]\n",
			want: "# mine\n[user]\n\n" + projectLead + "[project]\n"},
		{name: "yaml with repository", ext: ".yaml", in: "# keep me\nrepository:\n  visibility: public\n", mirrors: []string{".claude/skills"},
			want: "# keep me\nrepository:\n  visibility: public\n" + projectLead + "project:\n  mirrors: [.claude/skills]\n"},
		{name: "json", ext: ".json", in: `{"repository": {"visibility": "public"}}`,
			want: "{\n  \"repository\": {\n    \"visibility\": \"public\"\n  },\n  \"project\": {}\n}\n"},
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
