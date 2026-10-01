#!/usr/bin/env bash
set -euo pipefail

# Installs the hourly scheduler timer with the native backend for this machine.
# Ships with the `agents` plugin; copy agents/scheduler/ into the workspace first.
#
#   macOS               → launchd  (install-launchd.sh; runs a missed slot on wake)
#   Linux with systemd  → systemd  (install-systemd.sh; Persistent=true runs a missed slot after boot)
#   anything else       → cron     (setup.sh; silently skips slots while the machine is off)
#
# Override the choice with AGENT_SCHEDULER_BACKEND=launchd|systemd|cron.
#
# Usage: bash agents/scheduler/install.sh          # install
#        bash agents/scheduler/install.sh --which  # print the backend, install nothing

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

detect_backend() {
    if [[ -n "${AGENT_SCHEDULER_BACKEND:-}" ]]; then
        echo "$AGENT_SCHEDULER_BACKEND"
        return
    fi
    case "$(uname -s)" in
        Darwin)
            echo launchd ;;
        Linux)
            # A user manager must be reachable, or the timer would never fire.
            if command -v systemctl >/dev/null && systemctl --user show-environment >/dev/null 2>&1; then
                echo systemd
            elif command -v crontab >/dev/null; then
                echo cron
            else
                echo none
            fi ;;
        *)
            if command -v crontab >/dev/null; then echo cron; else echo none; fi ;;
    esac
}

BACKEND="$(detect_backend)"

if [[ "${1:-}" == "--which" ]]; then
    echo "$BACKEND"
    exit 0
fi

case "$BACKEND" in
    launchd) exec bash "$SCRIPT_DIR/install-launchd.sh" ;;
    systemd) exec bash "$SCRIPT_DIR/install-systemd.sh" ;;
    cron)    exec bash "$SCRIPT_DIR/setup.sh" ;;
    none)
        echo "Error: no scheduler backend found on $(uname -s) (need launchd, a systemd user manager, or cron)." >&2
        exit 1 ;;
    *)
        echo "Error: unknown AGENT_SCHEDULER_BACKEND '$BACKEND' (use launchd, systemd or cron)." >&2
        exit 1 ;;
esac
