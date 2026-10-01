#!/usr/bin/env bash
set -euo pipefail

# Idempotent systemd user-timer installer for the agent scheduler (Linux).
# Ships with the `agents` plugin; copy agents/scheduler/ into the workspace first.
# Normally reached through install.sh, which picks the backend for this machine.
#
# Why a systemd timer instead of cron: Persistent=true runs a missed slot as soon as
# the machine is back up, so a laptop that was off or a VM deallocated overnight still
# runs its due tasks. Plain cron just silently skips. This is the Linux counterpart of
# install-launchd.sh.
#
# User timers run only while the user's systemd manager is alive. On a machine with no
# logged-in session that needs lingering (sudo loginctl enable-linger "$USER"). This
# script checks and tells you; it never runs sudo itself.
#
# To remove:
#   systemctl --user disable --now <workspace>-agent-scheduler.timer
#   rm ~/.config/systemd/user/<workspace>-agent-scheduler.{service,timer}

# Resolve workspace root (two levels up from this script) and derive the unit name from its folder name
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE="$(cd "$SCRIPT_DIR/../.." && pwd)"
SLUG="$(basename "$WORKSPACE" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9\n' '-')"
UNIT="${SLUG}-agent-scheduler"
UNIT_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"

if [[ ! -f "$WORKSPACE/agents/scheduled-tasks.md" ]]; then
    echo "Error: Cannot find agents/scheduled-tasks.md in $WORKSPACE"
    exit 1
fi

TICK="$SCRIPT_DIR/tick.sh"
if [[ ! -x "$TICK" ]]; then
    echo "Error: $TICK missing or not executable"
    exit 1
fi

LOGS_DIR="$WORKSPACE/agents/scheduler/logs"
mkdir -p "$LOGS_DIR" "$UNIT_DIR"

echo "Workspace:  $WORKSPACE"
echo "Tick:       $TICK"
echo "Timer:      $UNIT_DIR/$UNIT.timer"

# systemd treats % as a specifier in unit files; escape it in the paths we write.
esc() { printf '%s' "${1//%/%%}"; }

cat > "$UNIT_DIR/$UNIT.service" <<SERVICE_EOF
[Unit]
Description=Agent scheduler tick for $(esc "$WORKSPACE")

[Service]
Type=oneshot
WorkingDirectory=$(esc "$WORKSPACE")
# Login shell, like the launchd job: a bare unit PATH has no node, nvm or ~/.local/bin.
ExecStart=/bin/bash -lc '"$(esc "$TICK")" >> "$(esc "$LOGS_DIR")/systemd.out.log" 2>&1'
SERVICE_EOF

cat > "$UNIT_DIR/$UNIT.timer" <<TIMER_EOF
[Unit]
Description=Hourly agent scheduler tick (:07) for $(esc "$WORKSPACE")

[Timer]
# Fire hourly at :07. Persistent=true runs a missed slot once the machine is back up.
OnCalendar=*-*-* *:07:00
Persistent=true
Unit=$UNIT.service

[Install]
WantedBy=timers.target
TIMER_EOF

systemctl --user daemon-reload
systemctl --user enable --now "$UNIT.timer"

echo ""
echo "Installed and enabled. Scheduler fires at :07 every hour (catches up after boot); Claude runs only when a task is due."

ME="${USER:-$(id -un)}"
if command -v loginctl >/dev/null && loginctl show-user "$ME" -p Linger 2>/dev/null | grep -q '^Linger=no'; then
    echo ""
    echo "Warning: lingering is off for $ME, so the timer stops when you log out and does not start at boot."
    echo "         To keep it running unattended:  sudo loginctl enable-linger $ME"
fi

echo "Verify: systemctl --user list-timers $UNIT.timer"
echo "Logs:   $LOGS_DIR/cron.log"
