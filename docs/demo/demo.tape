# Demo of the main flow for the README. Regenerate with `make demo`
# (needs vhs: https://github.com/charmbracelet/vhs). Runs from the
# repository root in a throwaway $HOME built by docs/demo/setup.sh, whose
# .demorc also prints an empty line after each command's output.
#
# Pacing: the pause after a command grows with its output, about 1.5-2 s
# for a line or two and 3.5-5 s for a manifest, a listing or a table, so
# the viewer can read it before the next command is typed.

Output docs/demo/demo.gif

Require git
Require go

Set Shell bash
Set FontSize 17
Set Width 1400
Set Height 680
Set Padding 20
Set Theme "Catppuccin Mocha"
Set Framerate 20
Set TypingSpeed 22ms

Hide
Type "docs/demo/setup.sh >/dev/null && source /tmp/skenv-demo/.demorc && clear"
Enter
Wait@90s /\$$/
Show

# 1. Bootstrap a machine from the skills repository.
Type "# One skenv.toml in your skills repository. Bootstrap a machine:"
Enter
Sleep 500ms
Type "skenv init example-org/agent-skills"
Sleep 300ms
Enter
Wait /\$$/
Sleep 4s
Type "cat agent-skills/skenv.toml"
Sleep 300ms
Enter
Wait /\$$/
Sleep 5s

Hide
Type "clear"
Enter
Show

# 2. Every agent sees the same skills.
Type "# Claude Code, pi and Codex (the store ~/.agents/skills) get the same skills"
Enter
Sleep 500ms
Type "ls ~/.claude/skills ~/.pi/agent/skills ~/.agents/skills"
Sleep 300ms
Enter
Wait /\$$/
Sleep 4500ms
Type "readlink ~/.claude/skills/pdf-tools"
Sleep 300ms
Enter
Wait /\$$/
Sleep 2s
Type "skenv doctor"
Sleep 300ms
Enter
Wait /\$$/
Sleep 3s

Hide
Type "clear"
Enter
Show

# 3. Drift: doctor reports it, sync repairs it.
Type "# Something drifts: doctor catches it, sync repairs it"
Enter
Sleep 500ms
Type "rm ~/.claude/skills/pdf-tools"
Sleep 300ms
Enter
Wait /\$$/
Sleep 1500ms
Type 'skenv doctor; echo "exit code $?"'
Sleep 300ms
Enter
Wait /\$$/
Sleep 4500ms
Type "skenv sync"
Sleep 300ms
Enter
Wait /\$$/
Sleep 2500ms
Type "skenv doctor"
Sleep 300ms
Enter
Wait /\$$/
Sleep 3s

Hide
Type "clear"
Enter
Show

# 4. Pin a third-party skill.
Type "# Pin a third-party skill to its current commit"
Enter
Sleep 500ms
Type "skenv vendor add example-vendor/commit-helper"
Sleep 300ms
Enter
Wait /\$$/
Sleep 4500ms
Type "tail -5 agent-skills/skenv.toml"
Sleep 300ms
Enter
Wait /\$$/
Sleep 5s
