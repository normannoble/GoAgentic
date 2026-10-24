---
description: Health review of the agent workspace — startup cost per agent, tracker hygiene, memory bloat, staleness, missing files, conventions drift. Reports findings by severity and recommends the exact next commands. Read-only. Use /agents:doctor or /agents:doctor <name>.
disable-model-invocation: true
allowed-tools: Read, Glob, Grep, Bash(date), Bash(wc), Bash(ls), Bash(git status), Bash(git log), Bash(echo:*), Bash(herdr agent list:*), Bash("$T3CODE_CLI_PATH" thread list:*)
argument-hint: [name|scope|all]
---

# /agents:doctor — Workspace Health Review

You audit the agent workspace and say what to fix first. You **never change a file**. The owning agent does the fixing; you hand the principal the exact command to start it.

Thresholds below mirror the master conventions (`agents/CONVENTIONS.md` § Tracker Hygiene, § Memory, § Baseline Consolidation). Do not read the master for this; the numbers are here.

## Workspace Detection

Glob for both `agents/*/context.md` (single-domain) and `agents/*/*/context.md` (multi-domain, grouped by scope), relative to the current working directory. Use whichever matches, or both if mixed. Directory names match case-insensitively.

`$ARGUMENTS`: empty = every active agent. `<name>` = one agent (deep report). `<scope>` = that scope. `all` = include retired agents.

**Retired agents** (`status: retired` in `context.md` frontmatter) are skipped unless `all`. Only check that their files still exist.

## Checks

Run `date` first. For every agent in scope, run `wc -c` on the files below in one call (glob the paths, then one `wc -c` per agent). Read files only where a check needs content; use Grep where a pattern is enough.

### A. Startup cost (what the agent reads before it says hello)

Sum the bytes of: workspace `agents/CONVENTIONS.md` + the master it extends (count once per report; the master is `${CLAUDE_PLUGIN_ROOT}/template/agents/CONVENTIONS.md`, or `$AGENT_FRAMEWORK_ROOT/template/agents/CONVENTIONS.md` / the root a wrapper skill named on another harness, or glob `~/.claude/plugins/cache/*/agents/*/template/agents/CONVENTIONS.md` highest version, or `.claude/skills/agents/../..` in copied mode — if unresolvable, use 27,000), `soul.md`, `name.md`, `role.md`, `autonomy.md`, `agents/tools/INDEX.md`, `tools.md`, `actions.md`, `MEMORY.md`, every file in `memory/standing/`, the 2 newest files in `memory/sessions/`, `context.md`, and every path listed under its `## Startup Context`. Estimate tokens as bytes ÷ 4.

| Tokens | Grade |
|--------|-------|
| ≤ 25k | ✅ |
| 25k–50k | ⚠️ |
| > 50k | ❌ |

Always name the **single biggest file** and its share. That is the fix.

### B. Tracker (`actions.md`)

- ❌ missing file.
- ❌ file > 40 KB; ⚠️ > 20 KB.
- ❌ the `Last reviewed:` line is longer than 600 characters, or contains "Prior review" more than once. Session narrative has been stuffed into the header; it belongs in `memory/sessions/` (one file per session) with the header cut to one line.
- ⚠️ no `Last reviewed:` line; ⚠️ its date is > 14 days old; ❌ > 45 days old.
- ⚠️ Open section has no `### P1` / `### P2` / `### P3` sub-sections.
- ⚠️ more than 8 open P1 rows (count table rows under `### P1` before the next heading).
- ⚠️ Open contains struck-through rows: the Action cell starts with `~~` (a done item left in Open). Strikethrough inside a live item's text (`~~algorithm HTML~~ (received) + SQL source`) is fine.
- ⚠️ Completed table has rows dated more than 30 days ago and `actions-archive.md` exists (they should be archived); ⚠️ `actions-archive.md` missing.
- ⚠️ Open rows whose Status does not start with `Not started`, `In progress`, `Waiting on`, `Blocked` or `Parked` (master § actions.md; case does not matter): give the count and the item numbers, e.g. `3 of 30 statuses free-text (#4, #9, #12)`. A free-text status hides who the item waits on, so `goagentic dash` cannot list it under the right person. Fix: move those rows onto the five states at the next wrap (`reference/session-end.md` § Step 2).
- ℹ️ Open rows whose Action cell does not start with a bold label (`**…**`), or whose leading bold phrase is longer than 60 characters (master § actions.md): give the count, e.g. `12 of 30 items without a label`. No fix needed now; rows get one when next touched at a wrap (`reference/session-end.md` § Step 2) or at a tracker cleanup.
- ℹ️ Open Action cells longer than 500 characters (master § actions.md: the cell says what is true now; history goes to the outcome file, plan doc or `items/`): give the count and the longest, e.g. `6 of 30 cells over 500 chars (longest #12, 2,400)`. Fix at the next wrap or tracker cleanup (step 8).
- ⚠️ an `items/…` pointer in `actions.md` whose file does not exist in the agent folder.
- ⚠️ any row whose Status cell starts `Waiting on` (or says blocked / gated / awaiting) for more than 30 days (compare the row's date if it has one; otherwise skip).

### C. Memory

- ❌ more than 5 files in `memory/standing/` or more than 10 in `memory/sessions/` (consolidation is overdue: master § Baseline Consolidation).
- ❌ any single file in `memory/standing/` > 15 KB (consolidate: situational rules and exact detail move to the agent's `reference/` with pointers; data dumps move to `work/` or `knowledge/` with a one-page summary).
- ⚠️ `MEMORY.md` > 6 KB.
- ⚠️ `MEMORY.md` links (`[[name]]` or `memory/...` paths) that match no file in `memory/**` (grep the names, glob for them).
- If `reference/` exists (agent-level, not the framework's): ⚠️ a file in it has no line under `## Reference` in `MEMORY.md` containing `Read when`; ⚠️ a `reference §N` / `<file> §N` pointer in `memory/standing/` whose file has no heading starting `## N.` (grep the pointers, then the headings). Reference files are not counted in startup cost and have no size limit.
- ⚠️ newest file in `memory/sessions/` older than the tracker's `Last reviewed` date by more than 7 days (sessions ran without a memory entry).
- ⚠️ files in `memory/sessions/` with `type: task` in their frontmatter: task logs belong in `memory/tasks/` (agents v1.20.0). Name the count and the fix: `git mv` them to `memory/tasks/`, keep the 10 newest there, archive the rest to `memory/archive/tasks/`, drop their MEMORY.md lines. Leave them out of the 10-session count above.
- ⚠️ more than 10 files in `memory/tasks/` (the task wrap archives older ones).

### D. Files and wiring

- ❌ any of these missing: `role.md`, `soul.md`, `name.md`, `autonomy.md`, `tools.md`, `actions.md`, `context.md`, `MEMORY.md`, `memory/standing/`, `memory/sessions/`.
- ❌ a path under `## Startup Context` in `context.md` that does not exist. Skip placeholder bullets such as `(none yet …)`. ⚠️ a bullet there that is not a workspace path at all (a URL, a wiki page name, prose): the agent cannot load it at startup; move it to the body of `context.md`.
- ⚠️ the agent's directory name is not spelled exactly like its name in `name.md` (the `# <Name> — Name` heading), case included: e.g. `agents/chrys/` for **Chrys**. Lookups still work (they ignore case), but the folder should match the name. Fix: the owner runs `git mv agents/<old> agents/<Name>` (on macOS through a temporary name: case-only renames need two steps) and updates live paths that use the old spelling; old session memories can stay.
- ⚠️ a path under `## Startup Context` points into the agent's `reference/` (reference files are read on demand, never at startup).
- ⚠️ `memory/scheduled/inbox.md` exists and has `UNPROCESSED` entries (count them). Count only entry headings (`### <timestamp> — <task> — UNPROCESSED` with a real date); the file's format header mentions the word too, and that is not an entry.
- ⚠️ `peer/` has more than 10 files (fix: tracker cleanup archives them; `peer/archive/` is not counted), or any file with `status: open` older than 7 days (an exchange that failed or a handover nobody picked up; if the file already holds a reply, only its status is stale). Name the `needs-principal` files: they wait on the principal.
- ⚠️ `lead:` in `context.md` names an agent that is missing or retired, or the agent itself: its lead cannot delegate to it (fix: correct the key or remove it).
- ⚠️ a work order (`peer/*.md` with `type: task`) in `working`, `question` or `review` for more than 3 days, or in `needs-principal` (name it: it waits on the principal). A `working` order whose `task_pane` is not in `herdr agent list` (Herdr only) lost its pane: the lead re-prompts or stops it (`reference/task.md` § 6).
- ⚠️ a playbook (`playbooks/*.md`) has no `## Trigger` section. ℹ️ `playbooks/` is empty (normal for a new agent; mention once, no fix needed).
- ℹ️ active agent whose `Last reviewed` is > 60 days old: suggest retiring it (`status: retired` in `context.md`).

### E. Workspace level (once per report)

- ❌ `agents/CONVENTIONS.md` missing or without `extends:` in frontmatter.
- ⚠️ `reserved:` list in that frontmatter does not match the agent directories (names missing from the list, or listed names with no directory). Compare case-insensitively: `Ned` in the list matches `agents/ned/`.
- ⚠️ `agents/tools/INDEX.md` missing.
- ⚠️ the instruction file's `## Agents` table (`CLAUDE.md`; also `AGENTS.md` or `GEMINI.md` if present) lists an agent that is retired or missing, or omits an active one.
- ⚠️ `harness:` in `agents/CONVENTIONS.md` frontmatter, or in any agent's `context.md` frontmatter, is set to a CLI that is not on PATH (`claude`, `codex`, `gemini`, `opencode`, `pi`, or `cursor`, whose CLI is `cursor-agent`), or to a name that is none of these: ticks and peer panes would fail.
- ℹ️ `git status --short agents/` shows uncommitted changes (list the count only).
- ⚠️ two or more active agents' `tools.md` (or `context.md`) name the same code repository path outside the workspace and it is not a `<repo>.wt/<agent>` worktree path: they share one checkout. Fix: each owner moves to its own worktree (master conventions § Code Repositories).
- ❌ (Herdr only: run `echo "$HERDR_ENV"`; if `1`) `herdr agent list` shows the same agent name live in two or more panes whose `cwd` is under this root — two sessions share one tracker. Name the panes. The same holds when `<name>` and `<name>-task` are both live (a human session beside a delegated task). Ignore other roots. In T3 Code (`T3CODE_CLI_PATH` set): the same holds when `"$T3CODE_CLI_PATH" thread list --json` shows two or more `<Name> - …` threads that are live (t3.md: a `<Name> - Peer` or `<Name> - Task` thread with `status: running`, or any other `<Name> - …` thread, a human session, with `settled: false`).
- ⚠️ a lead has more work orders in flight (`open`, `working`, `question`, `review`) than `task-limit` (default 3).
- ⚠️ a work order whose `outcome:` names an outcome file that does not exist, or a lead's `outcomes/*.md` with `status: active` and no work order in flight and no `## Next` line (stalled or unfinished bookkeeping).
- ⚠️ a work order in flight whose `due:` has passed, or whose `rounds` is 5 or more: its lead should have stopped it and asked the principal (`reference/task.md` § 6).

## Output

Heading: `# Agent Workspace Health — <today's date>` (no hardcoded project name).

1. **Scorecard** — one row per agent, single-line cells, `|` replaced with `·`:

   | Agent | Startup | Tracker | Memory | Files | Top issue |
   |-------|---------|---------|--------|-------|-----------|

   Startup shows the token estimate and grade, e.g. `44k ⚠️`. The other columns show the worst grade in that group. Top issue is ≤ 10 words. Add a Scope column only if more than one scope exists.

2. **Findings** — ❌ first, then ⚠️, then ℹ️. One line each: `❌ Balbus — actions.md is 93 KB; the Last reviewed line holds ~80 KB of nested session narrative.` Group by agent. Skip checks that passed; do not pad.

3. **Do this next** — at most 3 items, highest leverage first. Each names the agent, the fix in one sentence, and the exact command to start it, e.g.:
   - `/agents:start Balbus tracker cleanup — cut the Last reviewed line to one sentence and move the history into session memories`
   - `/agents:start Cato consolidate memory`
   - `/agents:new` is never a doctor recommendation; `/agents:init` only if E fails.

4. If everything passes: say so in one line and stop.

**Deep report** (`/agents:doctor <name>`): same layout for one agent, plus a table of every startup file with bytes and share, largest first.

Do not fix anything. Do not commit.
