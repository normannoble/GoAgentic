#!/usr/bin/env bash
set -euo pipefail

# Idempotent cron installer for the agent scheduler: the fallback when neither launchd nor a
# systemd user manager is available. Ships with the `agents` plugin; copy agents/scheduler/
# into the workspace first. Prefer install.sh, which picks launchd on macOS and a systemd
# timer on Linux: cron silently skips slots while the machine is asleep or off.
# Adds an hourly cron entry that fires a Claude session to check
# the task register and execute due tasks.
#
# Usage: bash agents/scheduler/setup.sh

# Resolve workspace root (two levels up from this script)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE="$(cd "$SCRIPT_DIR/../.." && pwd)"
SLUG="$(basename "$WORKSPACE" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9\n' '-')"
MARKER="${SLUG}-agent-scheduler"

# Verify we're in the right place
if [[ ! -f "$WORKSPACE/agents/scheduled-tasks.md" ]]; then
    echo "Error: Cannot find agents/scheduled-tasks.md in $WORKSPACE"
    echo "Copy scheduled-tasks.md from the plugin template to agents/ first."
    exit 1
fi

TICK="$SCRIPT_DIR/tick.sh"
[[ -x "$TICK" ]] || { echo "Error: $TICK missing or not executable"; exit 1; }

echo "Workspace: $WORKSPACE"
echo "Tick:      $TICK"

# Check if cron entry already exists
if crontab -l 2>/dev/null | grep -q "$MARKER"; then
    echo "Already installed. Scheduler cron entry exists."
    echo ""
    echo "Current entry:"
    crontab -l | grep "$MARKER"
    exit 0
fi

# Build the cron entry
LOGS_DIR="$WORKSPACE/agents/scheduler/logs"
mkdir -p "$LOGS_DIR"

CRON_ENTRY="7 * * * * $TICK >> $LOGS_DIR/launchd.out.log 2>&1 # $MARKER"

# Append to crontab
(crontab -l 2>/dev/null || true; echo "$CRON_ENTRY") | crontab -

echo "Installed. Scheduler will run at :07 every hour; Claude runs only when a task is due."
echo ""
echo "Entry:"
crontab -l | grep "$MARKER"
echo ""
echo "To remove: crontab -e and delete the line with '$MARKER'"
echo "Logs: $LOGS_DIR/cron.log"
