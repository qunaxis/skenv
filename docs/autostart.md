# Enable automatic sync

Autostart runs `skenv sync --quiet` at login and every hour, so a machine
picks up what other machines pushed without you running anything. It is
optional. Turn it on only after a manual `skenv sync` works on this machine
and `skenv doctor` exits 0 (see [Connect another machine](another-machine.md)):
the job runs unattended, and a sync that needs a decision (a conflict, a
dirty working copy) only leaves a warning in its log.

```sh
skenv sync --dry-run     # nothing unexpected
skenv doctor             # exit 0
skenv autostart enable
skenv autostart status   # exit 0: installed and loaded
```

- **macOS**: a LaunchAgent `~/Library/LaunchAgents/com.qunaxis.skenv.plist`,
  loaded with `launchctl`.
- **Linux**: `skenv.service` and `skenv.timer` in `~/.config/systemd/user`,
  enabled with `systemctl --user`.

The job runs the skenv binary you ran `enable` with, by its path: install
skenv where it stays (see [Install](install.md)) and run `enable` again
after moving it. An existing job is replaced.

The log is `~/.local/state/skenv/autostart.log`. With `--quiet` it holds
only warnings and errors; check it, or run `skenv doctor`, when a machine
seems out of date. A change reaches the job only after it is committed and
pushed on the machine where you made it.

```sh
skenv autostart disable  # unload and remove the job
```

Reference: [skenv autostart](commands/skenv_autostart.md).
