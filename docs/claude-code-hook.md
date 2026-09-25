# Claude Code hook

`.claude/settings.json` (a managed file since harness 0.3.0, see
[harness](harness.md)) runs `skenv lint --hook` after every `Edit`, `Write`
or `MultiEdit` (PostToolUse). The command first checks that the installed
skenv knows `--hook`, so machines with an older skenv or none at all are not
interrupted.

The hook reads the event from stdin, lints the skill that contains the
edited file and, when there are problems, prints them to stderr with exit
code 2, which Claude Code hands to the agent; edits outside skills, and
machines without skenv, are ignored.

> [!NOTE]
> The hook runs `skenv` from the `PATH` Claude Code sees. When skenv is
> missing there, or too old to know `--hook`, the hook exits 0 silently:
> edits go through unchecked, and the problems surface only in pre-commit
> and CI. Keep skenv on that `PATH` to get feedback while the agent edits.

JSON has no comments, so the "managed by skenv" header is the `$comment`
key; personal settings belong in `.claude/settings.local.json`.

## Verified behaviour

Checked by hand with Claude Code 2.1.282 in a temporary repository
(`skenv repo init --visibility private`, `skenv new demo --dir .`), headless:

```sh
claude -p "In skills/demo/SKILL.md change 'name: demo' to 'name: Demo_Skill' with the Edit tool, then quote any hook feedback" \
  --permission-mode acceptEdits --output-format stream-json --verbose
```

The agent received, right after the edit:

```text
PostToolUse:Edit hook blocking error from command: "command -v skenv … skenv lint --hook":
skenv lint: …/skills/demo has 2 problems after Edit; fix them:
…/skills/demo/SKILL.md: L2: name "Demo_Skill" must equal the directory name "demo"
…/skills/demo/SKILL.md: L2: name "Demo_Skill" may contain only lowercase letters, digits and "-"
```

and quoted it back in its answer. In an interactive session the same message
appears under the Edit tool call and the agent fixes the frontmatter.
