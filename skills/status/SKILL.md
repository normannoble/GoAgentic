---
description: Live status board of the whole agent org — role, current focus, and freshness per agent, plus in-flight, blocked, and stale rollups. Use /agents:status or /agents:status <scope>.
disable-model-invocation: true
allowed-tools: Read, Glob, Grep, Bash(echo:*), Bash(herdr agent list:*), Bash(date)
argument-hint: [scope|all]
---

# /agents:status — Org Status Board

Render a **live status board** of the whole agent org by reading each agent's tracker at run time, so it is never stale.

## Workspace Detection

Glob for both `agents/*/context.md` (single-domain) and `agents/*/*/context.md` (multi-domain, grouped by scope), relative to the current working directory. Use whichever matches, or both if mixed. Directory names match case-insensitively.

**Retired agents.** A `context.md` whose frontmatter has `status: retired` is hidden unless `$ARGUMENTS` is `all`.

## Procedure

1. Run `date` to get today's date (used for staleness).
2. Glob as above. For each agent directory, read:
   - `context.md` frontmatter → `title` (the **Role**) and `scope`.
   - `actions.md` → the `Last reviewed:` date, the `Next session:` line if present, and the **open P1 rows** (fall back to P2 if no P1). Use the `Next session:` line as the agent's **current focus / mission** when it exists; otherwise take the Action cell of the top 1–2 open items, and note any whose Status starts `Waiting on` (older trackers: reads blocked / gated / awaiting).
   - `role.md` → the **Primary Objective** line, as a fallback "current focus" only if the agent has no open actions.
3. Render a **GitHub-flavoured markdown table**, one row per agent:

   | Agent | Role | Current focus | Updated |
   |-------|------|---------------|---------|

   **Live column (Herdr only).** Run `echo "$HERDR_ENV"` (you cannot see env vars otherwise). If it prints `1`, run `herdr agent list`, keep only entries whose `cwd` is under the current working directory and whose `name` matches an agent here (lowercase), and add a `Live` column with pane id and state (`w1:p3 idle`), `—` if none. Never show agents from other roots.

   **Table formatting rules (so it always renders correctly):**
   - Keep every cell on a **single line** — summarise; never paste a multi-line action into a cell.
   - **Replace any `|` inside cell text with `·`** — a literal pipe breaks the column. Strip newlines too.
   - Keep "Current focus" to the top 1–2 open items, abbreviated to ~8–12 words.
   - "Updated" = the agent's `Last reviewed:` date; if absent, show `—`.
   - If an agent has no open actions, show `(idle — no open missions)` in Current focus.
   - Add a leading **Scope** column **only if** more than one distinct scope exists across the agents (otherwise omit it).
4. Below the table, add a short cross-org rollup as **bullets** (these are lists, not a grid):
   - **In flight:** active, unblocked missions across the org.
   - **Blocked / gated:** items whose status starts `Waiting on` someone other than the principal (older trackers: blocked / gated / awaiting), each with what it waits on. Items `Waiting on` the principal go under a separate **Waiting on <first name>** line.
   - **Stale (>14 days):** agents whose `Last reviewed` is more than 14 days before today.
5. If a scope filter was given (e.g., `/agents:status Acme`), restrict to that scope.

Heading: `# Agent Org Status — <today's date>` (do **not** hardcode a project name — keep it portable across workspaces).

End with one line: `One move: /agents:next. Start an agent: /agents:start <name>.`
