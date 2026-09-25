# Demo of the main flow for the README. Regenerate with `make demo`
# (needs vhs: https://github.com/charmbracelet/vhs). Runs from the
# repository root in a throwaway $HOME built by docs/demo/setup.sh.

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

Type "# One manifest in your skills repository. Bootstrap a machine:"
Enter
Sleep 400ms
Type "skenv init example-org/agent-skills"
Sleep 300ms
Enter
Wait /\$$/
Sleep 2500ms
Type "cat agent-skills/env.toml"
Sleep 300ms
Enter
Sleep 4s

Hide
Type "clear"
Enter
Show

Type "# Claude Code, pi and Codex (the store ~/.agents/skills) get the same skills"
Enter
Sleep 400ms
Type "ls ~/.claude/skills ~/.pi/agent/skills ~/.agents/skills"
Sleep 300ms
Enter
Sleep 2500ms
Type "readlink ~/.claude/skills/pdf-tools"
Sleep 300ms
Enter
Sleep 1500ms
Type "skenv doctor"
Sleep 300ms
Enter
Wait /\$$/
Sleep 2s

Hide
Type "clear"
Enter
Show

Type "# Something drifts: doctor catches it, sync repairs it"
Enter
Sleep 400ms
Type "rm ~/.claude/skills/pdf-tools"
Sleep 300ms
Enter
Type 'skenv doctor; echo "exit code $?"'
Sleep 300ms
Enter
Wait /\$$/
Sleep 3500ms
Type "skenv sync"
Sleep 300ms
Enter
Wait /\$$/
Sleep 1500ms
Type "skenv doctor"
Sleep 300ms
Enter
Wait /\$$/
Sleep 2500ms

Hide
Type "clear"
Enter
Show

Type "# Pin a third-party skill to its current commit"
Enter
Sleep 400ms
Type "skenv vendor add example-vendor/commit-helper"
Sleep 300ms
Enter
Wait /\$$/
Sleep 2500ms
Type "tail -5 agent-skills/env.toml"
Sleep 300ms
Enter
Sleep 4s
