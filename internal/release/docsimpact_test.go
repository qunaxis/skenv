package release

import (
	"errors"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestDocsImpact verifies that check-docs-impact.sh correctly validates
// PR changes to user-facing code against documentation requirements.
func TestDocsImpact(t *testing.T) {
	need(t, "bash")
	script := filepath.Join(root(t), "scripts", "check-docs-impact.sh")

	cases := []struct {
		name     string
		changes  map[string]bool // path -> is created
		prBody   string
		labels   string // comma-separated
		wantFail bool
	}{
		{
			name:     "no user-facing changes",
			changes:  map[string]bool{"internal/foo/foo_test.go": true},
			wantFail: false,
		},
		{
			name: "user-facing without docs",
			changes: map[string]bool{
				"internal/cli/cli.go": true,
			},
			wantFail: true,
		},
		{
			name: "user-facing with docs update",
			changes: map[string]bool{
				"internal/cli/cli.go": true,
				"docs/readme.md":      true,
			},
			wantFail: false,
		},
		{
			name: "user-facing with Docs: none reason (em dash)",
			changes: map[string]bool{
				"internal/cli/cli.go": true,
			},
			prBody:   "Docs: none — refactoring only",
			wantFail: false,
		},
		{
			name: "user-facing with Docs: none reason (hyphen)",
			changes: map[string]bool{
				"internal/cli/cli.go": true,
			},
			prBody:   "Docs: none - refactoring only",
			wantFail: false,
		},
		{
			name: "Docs: none without enough reason",
			changes: map[string]bool{
				"internal/cli/cli.go": true,
			},
			prBody:   "Docs: none — xx",
			wantFail: true,
		},
		{
			name: "user-facing with docs:none label",
			changes: map[string]bool{
				"internal/cli/cli.go": true,
			},
			labels:   "docs:none",
			wantFail: false,
		},
		{
			name: "multiple user-facing paths",
			changes: map[string]bool{
				"internal/cli/cli.go":     true,
				"internal/skenvfile/x.go": true,
			},
			wantFail: true,
		},
		{
			name: "skenv file change without docs",
			changes: map[string]bool{
				"internal/skenvfile/legacy.go": true,
			},
			wantFail: true,
		},
		{
			name: "manifest change without docs",
			changes: map[string]bool{
				"internal/manifest/x.go": true,
			},
			wantFail: true,
		},
		{
			name: "config change without docs",
			changes: map[string]bool{
				"internal/config/x.go": true,
			},
			wantFail: true,
		},
		{
			name: "schema change without docs",
			changes: map[string]bool{
				"schemas/x.json": true,
			},
			wantFail: true,
		},
		{
			name: "harness template change without docs",
			changes: map[string]bool{
				"internal/harness/templates/x": true,
			},
			wantFail: true,
		},
		{
			name: "AGENTS.md change is docs update",
			changes: map[string]bool{
				"internal/cli/cli.go": true,
				"AGENTS.md":           true,
			},
			wantFail: false,
		},
		{
			name: "skills/skenv change is docs update",
			changes: map[string]bool{
				"internal/cli/cli.go":   true,
				"skills/skenv/manifest": true,
			},
			wantFail: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRepo(t)
			r.commit("feat: initial")

			// Create changed files
			for path := range c.changes {
				// Create parent directories and file
				r.run("sh", "-c", "mkdir -p $(dirname '"+path+"') && echo content > '"+path+"'")
				r.run("git", "add", path)
			}
			r.run("git", "commit", "--quiet", "-m", "test changes")

			// Run the script
			cmd := exec.Command("bash", script)
			cmd.Dir = r.dir
			env := append(append([]string{}, r.env...),
				"BASE=HEAD~1",
				"HEAD=HEAD",
				"PR_BODY="+c.prBody,
				"PR_LABELS="+c.labels,
			)
			cmd.Env = env
			out, err := cmd.CombinedOutput()
			outStr := string(out)

			if c.wantFail {
				if err == nil {
					t.Errorf("script should have failed:\n%s", outStr)
				}
			} else {
				if err != nil {
					var ee *exec.ExitError
					if errors.As(err, &ee) {
						t.Errorf("script should have passed (exit %d):\n%s", ee.ExitCode(), outStr)
					} else {
						t.Errorf("script should have passed: %v\n%s", err, outStr)
					}
				}
			}
		})
	}
}
