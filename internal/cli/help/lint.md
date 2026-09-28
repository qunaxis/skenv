Check skills (directories with SKILL.md) under each path (default "."):
L1 frontmatter, L2 name, L3 Agent Skills limits, L4 relative links,
L5 file size and secret-like files, L6 shebangs. --publish adds P1: a license,
metadata.source not book/internal/third-party-copy, no stop-list phrase
($SKENV_DENYLIST or ~/.config/skenv/denylist.txt) and gitleaks over the whole
history. --hook is the Claude Code PostToolUse hook: it reads the event on stdin.
Exit code 0: clean, 1: problems, 2: error (--hook: 2 with findings,
so Claude Code shows them to the agent).
