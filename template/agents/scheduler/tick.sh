#!/usr/bin/env bash
# Scheduler tick. Called by launchd, a systemd timer or cron every hour at :07.
#
# Step 1 is a cheap gate (gate.py, no Claude): parse agents/scheduled-tasks.md and
# decide whether any enabled task is due right now. If none is, log one line and exit.
# Step 2 runs the harness headlessly on agents/scheduler/prompt.md only when something is due.
#
# Harness = the coding-agent CLI that runs the tick. Read from `harness:` in the
# frontmatter of agents/CONVENTIONS.md (claude | codex | gemini | opencode; default
# claude). Override with AGENT_HARNESS=<name>. AGENT_HARNESS_CMD="<cmd and flags>"
# replaces the whole command; the prompt is appended as its last argument.
#
# Usage: bash agents/scheduler/tick.sh           # normal tick
#        bash agents/scheduler/tick.sh --dry-run # print what is due, never call the harness
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE="$(cd "$SCRIPT_DIR/../.." && pwd)"
REGISTER="$WORKSPACE/agents/scheduled-tasks.md"
LOGS_DIR="$SCRIPT_DIR/logs"
mkdir -p "$LOGS_DIR"
cd "$WORKSPACE"

DRY_RUN=false
[[ "${1:-}" == "--dry-run" ]] && DRY_RUN=true

# nvm: launchd/systemd/cron start with a bare PATH; npx-based MCP servers need node.
export NVM_DIR="${NVM_DIR:-$HOME/.nvm}"
# shellcheck disable=SC1091
[[ -s "$NVM_DIR/nvm.sh" ]] && . "$NVM_DIR/nvm.sh" >/dev/null 2>&1 || true

if ! command -v python3 >/dev/null; then
  echo "$(date '+%Y-%m-%d %H:%M') gate: python3 missing, cannot evaluate register" >> "$LOGS_DIR/cron.log"
  exit 1
fi

DUE="$(python3 "$SCRIPT_DIR/gate.py" "$REGISTER")" || rc=$?
rc=${rc:-0}
if [[ $rc -eq 2 ]]; then
  echo "$(date '+%Y-%m-%d %H:%M') gate: no register at agents/scheduled-tasks.md" >> "$LOGS_DIR/cron.log"
  exit 0
fi

if [[ -z "$DUE" ]]; then
  echo "$(date '+%Y-%m-%d %H:%M') gate: no tasks due" >> "$LOGS_DIR/cron.log"
  $DRY_RUN && echo "no tasks due"
  exit 0
fi

if $DRY_RUN; then
  echo "due: $DUE"
  exit 0
fi

# Which harness runs the tick
HARNESS="${AGENT_HARNESS:-}"
if [[ -z "$HARNESS" && -f "$WORKSPACE/agents/CONVENTIONS.md" ]]; then
  HARNESS="$(sed -n '1,/^---$/!d;s/^harness:[[:space:]]*//p' "$WORKSPACE/agents/CONVENTIONS.md" | sed 's/[[:space:]]*#.*//' | head -1)"
fi
HARNESS="${HARNESS:-claude}"

# Framework root for non-plugin harnesses: with `extends: plugin` the master
# conventions and reference files live there. Newest Claude plugin cache is the fallback.
if [[ -n "${AGENT_FRAMEWORK_ROOT:-}" ]]; then
  export AGENT_FRAMEWORK_ROOT
else
  newest="$(ls -d "$HOME"/.claude/plugins/cache/*/agents/* 2>/dev/null | sort -V | tail -1 || true)"
  [[ -n "$newest" ]] && export AGENT_FRAMEWORK_ROOT="$newest"
fi

# Find the harness CLI
find_bin() {
  if command -v "$1" >/dev/null 2>&1; then command -v "$1"
  elif [[ -x "$HOME/.claude/bin/$1" ]]; then echo "$HOME/.claude/bin/$1"
  elif [[ -x "/usr/local/bin/$1" ]]; then echo "/usr/local/bin/$1"
  elif [[ -x "$HOME/.local/bin/$1" ]]; then echo "$HOME/.local/bin/$1"
  elif [[ -x "/opt/homebrew/bin/$1" ]]; then echo "/opt/homebrew/bin/$1"
  fi
}

PROMPT="Read agents/scheduler/prompt.md and follow its instructions exactly. The gate script found these tasks due now: $DUE. Verify against the register, then execute only due tasks."

if [[ -n "${AGENT_HARNESS_CMD:-}" ]]; then
  read -r -a CMD <<< "$AGENT_HARNESS_CMD"
  TAIL=()
  HARNESS="custom (${CMD[0]})"
else
  BIN="$(find_bin "$HARNESS")"
  if [[ -z "$BIN" ]]; then
    echo "$(date '+%Y-%m-%d %H:%M') gate: due=[$DUE] but $HARNESS CLI not found" >> "$LOGS_DIR/cron.log"
    exit 1
  fi
  # Unattended run: no one can answer an approval prompt, so each harness gets its
  # own "skip approvals" flag. Same trust level as before, only the CLI differs.
  case "$HARNESS" in
    claude)   CMD=("$BIN" -p); TAIL=(--dangerously-skip-permissions --max-turns 50) ;;
    codex)    CMD=("$BIN" exec -C "$WORKSPACE" --dangerously-bypass-approvals-and-sandbox); TAIL=() ;;
    gemini)   CMD=("$BIN" --approval-mode yolo); TAIL=() ;;
    opencode) CMD=("$BIN" run --auto --dir "$WORKSPACE"); TAIL=() ;;
    *)
      echo "$(date '+%Y-%m-%d %H:%M') gate: due=[$DUE] but harness '$HARNESS' is unknown (claude|codex|gemini|opencode)" >> "$LOGS_DIR/cron.log"
      exit 1 ;;
  esac
fi

echo "$(date '+%Y-%m-%d %H:%M') gate: due=[$DUE] — starting $HARNESS" >> "$LOGS_DIR/cron.log"
# ${TAIL[@]+...} keeps bash 3.2 (macOS /bin/bash) happy with an empty array under set -u
"${CMD[@]}" "$PROMPT" ${TAIL[@]+"${TAIL[@]}"} >> "$LOGS_DIR/cron.log" 2>&1
