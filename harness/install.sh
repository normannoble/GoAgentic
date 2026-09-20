#!/usr/bin/env bash
# Install the agents framework commands into a non-Claude coding-agent CLI.
#
#   bash harness/install.sh <codex|gemini|opencode> [workspace] [--user] [--set-default]
#
# What it writes (project scope by default, inside <workspace>; --user writes the
# user-level equivalents so every repo gets them):
#
#   all three   .agents/skills/agents-<cmd>/SKILL.md     one thin wrapper per framework skill
#               (the Agent Skills standard folder; Codex, Gemini CLI and OpenCode all read it)
#   gemini      .gemini/commands/agents/<cmd>.toml       so /agents:<cmd> works, same names as Claude
#   opencode    .opencode/commands/agents-<cmd>.md       so /agents-<cmd> works
#   codex       (skills only; Codex invokes them as $agents-<cmd>)
#   instruction file (project scope only, if it has no "## Agents" heading):
#               AGENTS.md (codex, opencode) or GEMINI.md (gemini)
#
# The wrappers do not copy the skills. Each one names this checkout as the
# framework root and tells the model to read the real SKILL.md there. Update the
# checkout and every harness sees the change. Claude Code keeps using its plugin;
# nothing here touches .claude/.
#
# --set-default writes `harness: <name>` into agents/CONVENTIONS.md frontmatter.
# That key picks the CLI for unattended ticks (agents/scheduler/tick.sh) and peer
# panes (/agents:ask). Interactive sessions work from any harness regardless.
set -euo pipefail

usage() { sed -n '2,24p' "$0" | sed 's/^# \{0,1\}//'; exit "${1:-0}"; }

HARNESS="${1:-}"
[[ -z "$HARNESS" || "$HARNESS" == "-h" || "$HARNESS" == "--help" ]] && usage 0
case "$HARNESS" in codex|gemini|opencode) ;; claude) echo "claude uses the plugin: /plugin install agents@normannoble" >&2; exit 1 ;; *) echo "unknown harness: $HARNESS" >&2; usage 1 ;; esac
shift

WORKSPACE="$PWD"
SCOPE=project
SET_DEFAULT=false
for arg in "$@"; do
  case "$arg" in
    --user) SCOPE=user ;;
    --set-default) SET_DEFAULT=true ;;
    -*) echo "unknown option: $arg" >&2; usage 1 ;;
    *) WORKSPACE="$arg" ;;
  esac
done
WORKSPACE="$(cd "$WORKSPACE" && pwd)"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
[[ -f "$ROOT/skills/start/SKILL.md" ]] || { echo "not a framework checkout: $ROOT" >&2; exit 1; }

SKILLS=(help init new start wrap close list status next doctor harness ask schedule)

# The command a user types in this harness. Mirrors the table in template/agents/reference/peer.md.
case "$HARNESS" in
  codex)    START='$agents-start'; PREFIX='$agents-' ;;
  gemini)   START='/agents:start'; PREFIX='/agents:' ;;
  opencode) START='/agents-start'; PREFIX='/agents-' ;;
esac

description_of() {  # first `description:` line of a SKILL.md frontmatter
  sed -n '1,/^---$/!d;s/^description:[[:space:]]*//p' "$ROOT/skills/$1/SKILL.md" | head -1
}

# Body shared by every wrapper. $1 = skill name, $2 = how arguments arrive.
wrapper_body() {
  cat <<BODY
# ${PREFIX}$1 (agents framework)

Framework root: \`$ROOT\`

Read \`$ROOT/skills/$1/SKILL.md\` and follow it exactly, as if you were that command.

- $2
- Wherever it says \`\${CLAUDE_PLUGIN_ROOT}\`, use the framework root above.
- Wherever it names Claude Code tools (Read, Write, Edit, Glob, Grep, Bash, AskUserQuestion), use your own equivalent: read or write the file, search, run the shell command, ask the user.
- Ignore \`allowed-tools\` and \`disable-model-invocation\` in its frontmatter. Respect the Bash allow-list in spirit: run only the commands the skill names.
- Other framework commands it mentions as \`/agents:<cmd>\` are ${3:-$SHARED_CMDS}.
BODY
}
# .agents/skills is read by all three harnesses, so its wrappers must not name one.
SHARED_CMDS='`$agents-<cmd>` in Codex, `/agents:<cmd>` in Gemini CLI, `/agents-<cmd>` in OpenCode'
NATIVE_CMDS="\`${PREFIX}<cmd>\` here"

written=()
write() {  # path, content
  mkdir -p "$(dirname "$1")"
  printf '%s\n' "$2" > "$1"
  written+=("$1")
}

# 1. Agent Skills wrappers (all harnesses)
if [[ $SCOPE == user ]]; then SKILLS_DIR="$HOME/.agents/skills"; else SKILLS_DIR="$WORKSPACE/.agents/skills"; fi
for s in "${SKILLS[@]}"; do
  desc="$(description_of "$s")"
  write "$SKILLS_DIR/agents-$s/SKILL.md" "---
name: agents-$s
description: $desc
---

$(wrapper_body "$s" "The user's arguments are the text after the skill name in their message. Treat that text as \`\$ARGUMENTS\`.")"
done

# 2. Native slash commands where the harness has them
case "$HARNESS" in
  gemini)
    if [[ $SCOPE == user ]]; then CMD_DIR="$HOME/.gemini/commands/agents"; else CMD_DIR="$WORKSPACE/.gemini/commands/agents"; fi
    for s in "${SKILLS[@]}"; do
      desc="$(description_of "$s" | sed 's/\\/\\\\/g;s/"/\\"/g')"
      write "$CMD_DIR/$s.toml" "description = \"$desc\"
prompt = \"\"\"
$(wrapper_body "$s" "\`\$ARGUMENTS\` is: {{args}}" "$NATIVE_CMDS")
\"\"\""
    done ;;
  opencode)
    if [[ $SCOPE == user ]]; then CMD_DIR="$HOME/.config/opencode/commands"; else CMD_DIR="$WORKSPACE/.opencode/commands"; fi
    for s in "${SKILLS[@]}"; do
      write "$CMD_DIR/agents-$s.md" "---
description: $(description_of "$s")
---

$(wrapper_body "$s" "\`\$ARGUMENTS\` is: \$ARGUMENTS" "$NATIVE_CMDS")"
    done ;;
esac

# 3. Instruction file (project scope only)
if [[ $SCOPE == project ]]; then
  case "$HARNESS" in gemini) DOC="$WORKSPACE/GEMINI.md" ;; *) DOC="$WORKSPACE/AGENTS.md" ;; esac
  if [[ ! -f "$DOC" ]] || ! grep -q '^## Agents' "$DOC"; then
    block="## Agents

Persistent AI collaborators with calibrated autonomy. See \`agents/CONVENTIONS.md\` (extends the agents framework master at \`$ROOT/template/agents/CONVENTIONS.md\`).

Start one with \`$START <name>\`. List with \`${PREFIX}list\`. Board: \`${PREFIX}status\`. One move: \`${PREFIX}next\`. Scheduled tasks: \`${PREFIX}schedule\`. Guide: \`${PREFIX}help\`."
    if [[ -f "$DOC" ]]; then printf '\n%s\n' "$block" >> "$DOC"; else printf '%s\n' "$block" > "$DOC"; fi
    written+=("$DOC")
  fi
fi

# 4. Optional: make this harness the default for ticks and peer panes
CONV="$WORKSPACE/agents/CONVENTIONS.md"
if $SET_DEFAULT; then
  [[ -f "$CONV" ]] || { echo "no $CONV; run the init command first" >&2; exit 1; }
  if grep -qE '^harness:' "$CONV"; then
    sed -i.bak -E "s/^harness:.*/harness: $HARNESS/" "$CONV" && rm -f "$CONV.bak"
  else
    # insert before the closing --- of the frontmatter
    awk -v h="harness: $HARNESS" 'NR>1 && !done && /^---$/ {print h; done=1} {print}' "$CONV" > "$CONV.tmp" && mv "$CONV.tmp" "$CONV"
  fi
  written+=("$CONV")
fi

echo "Harness:   $HARNESS ($SCOPE scope)"
echo "Workspace: $WORKSPACE"
echo "Root:      $ROOT"
echo "Written:"
printf '  %s\n' "${written[@]}"
echo
echo "Try: $START <name>   (in $HARNESS, from $WORKSPACE)"
[[ -f "$CONV" ]] || echo "No agents/CONVENTIONS.md yet: run ${PREFIX}init in $HARNESS first, or /agents:init in Claude Code."
$SET_DEFAULT || echo "Ticks and peer panes still use the harness in agents/CONVENTIONS.md (default claude). Add --set-default to switch."
