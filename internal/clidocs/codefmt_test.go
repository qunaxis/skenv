package clidocs

import "testing"

func TestCodeFormat(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"flags", "into --path (default x) or -h; --dir=x.", "into `--path` (default x) or `-h`; `--dir=x`."},
		{"flag in parentheses", "(--hook: 2 with findings,", "(`--hook`: 2 with findings,"},
		{"env vars", "the SKENV_<KEY> variable and $SKENV_MANIFEST.", "the `SKENV_<KEY>` variable and `$SKENV_MANIFEST`."},
		{"paths", "Log: ~/.local/state/skenv/autostart.log.", "Log: `~/.local/state/skenv/autostart.log`."},
		{"placeholder path", "(default ./<repo> in", "(default `./<repo>` in"},
		{"directories", "Create skills/<name>/ with references/ in", "Create `skills/<name>/` with `references/` in"},
		{"absolute path", "under /etc/skenv or /", "under `/etc/skenv` or /"},
		{"file names", "SKILL.md, config.toml, lefthook.yml, *.md and .gitignore", "`SKILL.md`, `config.toml`, `lefthook.yml`, `*.md` and `.gitignore`"},
		{"dotted keys", "an older repo.harness", "an older `repo.harness`"},
		{"sections", "the [environment] section; Refuses if [repo] exists.", "the `[environment]` section; Refuses if `[repo]` exists."},
		{"quoted command", `"skenv init" records it.`, "`skenv init` records it."},
		{"quoted key", `its one key is "manifest", which`, "its one key is `manifest`, which"},
		{"quoted dot", `(default "."):`, "(default `.`):"},
		{"quoted prose stays", `a "real home" here`, `a "real home" here`},
		{"prose stays", "and/or L1-L6, P1, e.g. HEAD, i.e. 0.4.0 - a skill.", "and/or L1-L6, P1, e.g. HEAD, i.e. 0.4.0 - a skill."},
		{"code span kept", "then `skenv sync --quiet` or --quiet", "then `skenv sync --quiet` or `--quiet`"},
		{"code block kept", "Run:\n\n\tsource <(skenv completion bash) --x\n    ~/y", "Run:\n\n\tsource <(skenv completion bash) --x\n    ~/y"},
		{"bare placeholder", "skenv init <owner/repo> clones", "skenv init `<owner/repo>` clones"},
		{"idempotent", "`--path` and `~/x`", "`--path` and `~/x`"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := codeFormat(c.in); got != c.want {
				t.Errorf("codeFormat(%q)\n got %q\nwant %q", c.in, got, c.want)
			}
		})
	}
}
