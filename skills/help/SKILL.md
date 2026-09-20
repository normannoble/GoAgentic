---
description: How the agents plugin works — the commands, the flow from setup to daily use, what an agent is made of, and fixes for common problems. Use /agents:help, or /agents:help <command> for one command.
disable-model-invocation: true
allowed-tools: Read, Glob
argument-hint: [start|list|status|next|doctor|schedule|ask|new|init]
---

# /agents:help — Guide

If `$ARGUMENTS` names a command, read `${CLAUDE_PLUGIN_ROOT}/skills/<command>/SKILL.md` (if the variable is empty, glob `~/.claude/plugins/cache/*/agents/*/skills/<command>/SKILL.md` and take the highest version; in copied mode use `.claude/skills/agents/skills/<command>/SKILL.md`). Print its `description` and `argument-hint`, then explain what it does in five lines or fewer, in plain words. Stop.

Otherwise your whole reply is the guide below, **as is**. Start with its first heading. No sentence before it, nothing after it. Do not read any file.

---

# Agents — quick guide

An **agent** is a named, persistent collaborator that lives in this repo as markdown files. It keeps its own memory, its own action tracker, and a level of autonomy you set. You start it by name and it becomes that agent for the session.

## Commands

| Command | What it does |
|---------|--------------|
| `/agents:init` | Set up this repo as a workspace. Run once. Re-run to backfill missing pieces. |
| `/agents:new` | Design and create an agent. Asks you questions. |
| `/agents:start <name>` | Start an agent. It reads its files and tells you its priorities. |
| `/agents:start <name> <topic>` | Start an agent and work on one thing. |
| `/agents:start <name> close` | End the session. The agent saves memory and updates its tracker. |
| `/agents:list` | List agents. Add `<scope>` to filter, `all` to include retired ones. |
| `/agents:status` | Live board: each agent's role, current focus, and freshness. |
| `/agents:next` | The single next best action across all agents, and why. |
| `/agents:doctor` | Health review. Finds bloat, stale trackers, missing files. Recommends fixes. Changes nothing. |
| `/agents:ask <name> "<request>"` | Ask another agent in this workspace. Opens a pane, runs it as a peer, reads its written reply, closes the pane. Herdr only. |
| `/agents:schedule` | Unattended tasks. `add`, `list`, `remove <id>`, `enable`/`disable <id>`, `status`, `install` (the hourly timer). |
| `/agents:help <command>` | Details for one command. |

## The flow

1. **First time in a repo:** `/agents:init`, then `/agents:new`.
2. **Each working session:** `/agents:start <name>` (or with a topic). Do the work. `/agents:start <name> close` when done.
3. **When you do not know where to go:** `/agents:status` for the whole board, `/agents:next` for the one move.
4. **Once a week or so:** `/agents:doctor`. Run the commands it gives you.
5. **When one agent needs another:** `/agents:ask <name> "<request>"` from inside the asking agent's session. Same workspace only. The reply lands in `agents/<Name>/peer/`.
6. **For work that should happen without you:** `/agents:schedule add`, then `/agents:schedule install` once. A timer checks every hour and runs Claude only when a task is due. Results land in the agent's inbox and surface at its next start.

Always run these from the workspace root (the folder that holds `agents/`).

## What an agent is made of

```
agents/<Name>/
  role.md        what it does and its goals
  soul.md        how it thinks and talks
  autonomy.md    what it may do alone (L1 ask … L5 act and report)
  tools.md       the tools it may use
  actions.md     its action tracker (P1 / P2 / P3, Completed)
  context.md     scope, title, extra files to read at startup
  MEMORY.md      index of its memory
  memory/        standing/ (durable) and sessions/ (one per session)
  peer/          replies to /agents:ask requests
  playbooks/     repeatable procedures with triggers
```

`agents/CONVENTIONS.md` holds the rules for this workspace. It is short and says `extends: plugin`. The full rules ship inside the plugin. The workspace file wins on conflict.

## Common problems

- **"Unknown command" after an update.** Restart Claude Code. Plugins load at start.
- **An agent starts slowly or uses many tokens.** Run `/agents:doctor`. Usually the tracker or standing memory has grown. The doctor names the file.
- **An agent is gone from `/agents:list`.** It is retired. `/agents:list all` shows it. `/agents:start <name>` still works.
- **"the name agents is already taken by an installed plugin".** The repo has an in-repo copy of the plugin (copied mode) and the marketplace plugin is also installed. Keep one. Marketplace: `claude plugin uninstall agents@normannoble`. Or delete `.claude/skills/agents/`.
- **Update the plugin:** `claude plugin marketplace update normannoble && claude plugin update agents@normannoble`, then restart.
- **Use the agents from Codex, Gemini CLI, or OpenCode.** Clone the framework repo and run `bash harness/install.sh <codex|gemini|opencode> <workspace>`. Same agents, same files. Commands are `$agents-start <name>` in Codex, `/agents:start <name>` in Gemini, `/agents-start <name>` in OpenCode.

Source and docs: https://github.com/normannoble/GoAgentic
