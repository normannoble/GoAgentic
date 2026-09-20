---
name: harness
description: Show which coding-agent CLI each agent runs under — its peer-session harness (/agents:ask) and its unattended-tick harness — for every active agent in this workspace, and whether that CLI is installed. Read-only: it lists the current configuration and suggests the exact edit to change it, but changes nothing. Use /agents:harness.
disable-model-invocation: true
allowed-tools: Read, Glob, Grep, Bash(date), Bash(ls), Bash(echo:*), Bash(command -v:*)
argument-hint: (none — lists this workspace's harness config)
---

# /agents:harness — Harness configuration

You show which CLI each agent runs under, and you change nothing. Peer panes and unattended ticks come from the same `harness:` key but resolve differently:

- **Peer harness** (`/agents:ask`, peer panes): the agent's own `context.md` `harness:` if set, else the workspace `agents/CONVENTIONS.md` `harness:`, else the default `claude`.
- **Tick harness** (unattended scheduled runs): **always** the workspace `agents/CONVENTIONS.md` `harness:` (else `claude`). A `context.md` override does **not** apply to ticks.
- Interactive sessions work from any harness that has the framework commands installed; this command is about peer panes and ticks.

## Workspace Detection

Glob for both `agents/*/context.md` (single-domain) and `agents/*/*/context.md` (multi-domain). Use whichever matches, or both if mixed. The workspace root is the current working directory. Match directory names case-insensitively. Skip agents whose `context.md` frontmatter has `status: retired`.

## Procedure

1. Read the workspace default: the `harness:` value in `agents/CONVENTIONS.md` frontmatter (strip any trailing comment). If absent, it is `claude`.
2. For each active agent, read its `context.md` frontmatter `harness:` — the peer override — if present.
3. Resolve per agent: **peer harness** = override if set, else the workspace default; **tick harness** = the workspace default. Note the source of the peer harness (`own override` or `workspace`).
4. For each distinct harness that appears, check whether its CLI is on PATH: run `command -v <cli>` for each of `claude`, `codex`, `gemini`, `opencode` that appears. A harness whose CLI is missing means broken peer panes or ticks — mark it.

## Output

Heading: `# Harness configuration — <workspace name>`.

1. One line: `Workspace default: <harness>` — and note if that CLI is not on PATH.
2. A table, one row per active agent:

   | Agent | Peer harness | Source | Tick harness | CLI installed |
   |-------|--------------|--------|--------------|---------------|

   In `CLI installed`, put ✅, or `⚠️ <cli> not on PATH` naming each missing CLI the agent depends on. A peer harness that differs from the tick harness is fine — the Source column shows it is an override. Add a Scope column only if more than one scope exists.

3. **To change one** — list the exact edit as an option only; make no edit:
   - One agent's peer harness: set or remove `harness: <cli>` in `agents/<name>/context.md` frontmatter.
   - The whole workspace (peer default **and** all ticks): set `harness: <cli>` in `agents/CONVENTIONS.md` frontmatter, or run the framework's `harness/install.sh <cli> <workspace> --set-default` from the framework checkout.

   The principal instructs the change from here; this command does not apply it.

Do not change any file. Do not commit.
