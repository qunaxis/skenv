Install and load the autostart job, which runs `skenv sync --quiet` at
login and every hour.

- Reads: the path of this skenv binary and of git.
- Changes: ~/Library/LaunchAgents/com.qunaxis.skenv.plist (macOS) or
  skenv.service and skenv.timer in ~/.config/systemd/user (Linux), loaded
  with launchctl or systemctl --user; an existing job is replaced.
- Network: none itself; each hourly sync pulls and fetches.
- Preview: none; `skenv sync --dry-run` shows what the job would change.
- Next: "skenv autostart status"; the log is ~/.local/state/skenv/autostart.log.
