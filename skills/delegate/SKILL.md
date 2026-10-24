---
description: "A lead hands one of its members a work order: the member does the work (build, write, fix, commit) in its own Herdr pane or T3 thread under its own autonomy, asks the lead its questions, and hands back for review. Use /agents:delegate <name> \"<work order>\" from inside the lead's session. Requires Herdr (HERDR_ENV=1) or T3 Code (T3CODE_CLI_PATH set) and lead: <caller> in the member's context.md."
allowed-tools: Read, Write, Edit, Glob, Grep, Bash(date), Bash(echo:*), Bash(pwd), Bash(mkdir:*), Bash(herdr agent list:*), Bash(herdr agent start:*), Bash(herdr agent prompt:*), Bash(herdr agent read:*), Bash(herdr agent get:*), Bash(herdr agent wait:*), Bash(herdr pane split:*), Bash(herdr pane rename:*), Bash(herdr pane layout:*), Bash(herdr pane close:*), Bash("$T3CODE_CLI_PATH" thread:*), mcp__t3-code__t3_thread_organize
argument-hint: <name> "<work order>"
---

# /agents:delegate — Delegated Task

You are a **lead**. You hand one of your members a work order for an outcome the principal approved; the member does the work in its own pane, under its own autonomy. The full protocol is `${CLAUDE_PLUGIN_ROOT}/template/agents/reference/task.md` (if the variable is empty: `$AGENT_FRAMEWORK_ROOT/template/agents/reference/task.md`, or the framework root a wrapper skill named; else glob `~/.claude/plugins/cache/*/agents/*/template/agents/reference/task.md`, highest version). Read it once, then follow it. In T3 Code, also read `reference/t3.md` beside it: it maps every Herdr step below onto T3. For a question or a review without work, use `/agents:ask` instead.

## Preconditions (refuse with one line if any fails)

Run **all** of them before writing any file or opening any pane. A refused delegation leaves nothing behind.

1. Run `echo "$HERDR_ENV $HERDR_PANE_ID T3=$T3CODE_CLI_PATH"`. Herdr: `HERDR_ENV` is `1` and `HERDR_PANE_ID` set. T3: `HERDR_ENV` empty and `T3=` set (t3.md § Detecting it). Neither: `Not inside Herdr or T3 — /agents:delegate needs a Herdr pane or a T3 thread.`
2. Workspace root = current working directory; it must contain `agents/`. Caller = the agent active in this session. No agent active → `Only a lead can delegate; start one with /agents:start <name>.`
3. Target: glob `agents/<name>/context.md` or `agents/*/<name>/context.md` (case-insensitive). Missing or `status: retired` → say so. Its frontmatter `lead:` must equal the caller (case-insensitive) → otherwise `<Target> does not report to <Caller> (no lead: <Caller> in its context.md).` Never look outside this root.
4. Run `herdr agent list` now and check `.result.agents[]`: if any entry whose `cwd` is under this root has `name` `<target>`, `<target>-peer` or `<target>-task` (lowercase), refuse: `<Target> is live in pane <id>; one session per agent. Ask the principal to wrap it, then delegate again.` Do not offer to issue it anyway and never close that pane yourself: only the principal ends another session. In T3 the same check is `"$T3CODE_CLI_PATH" thread list --json`: refuse if a `<Target> - …` thread other than yours is live (t3.md: a `<Target> - Peer` or `<Target> - Task` thread with `status: running`, or any other `<Target> - …` thread, a human session, with `settled: false`) (`<Target> is live in thread <title>; …`). Re-run this check when you delegate again.
5. Count work orders in flight: files `agents/*/peer/*.md` (and `agents/*/*/peer/*.md`) with `type: task`, `from: <Caller>`, and status `open`, `working`, `question` or `review`. Limit = `task-limit:` in `agents/CONVENTIONS.md` frontmatter, default 3. At the limit → `<Caller> has <n> tasks in flight (limit <n>); finish or stop one first.`
6. Outcome: the approved outcome this work serves has an outcome file (`agents/<Caller>/outcomes/<slug>.md`, task.md § 2). If the principal approved it in this session and there is no file yet, write it first. If nothing was approved, ask the principal and stop.
7. Size: the work order fits one of the member's sessions with room to wrap (task.md § 4). If it does not, split it and delegate the first part.
8. Harness and model for the target, exactly as `/agents:ask` precondition 4: `harness:` from the target's `context.md`, else `agents/CONVENTIONS.md`, else `claude`; `model:` the same way. In T3, pass `--model <model>` to `thread start` when a model is set; a harness other than `claude` is not yet supported there: refuse with `<Target> runs <harness>; T3 delegation supports claude only for now.` Start command: `claude`/`gemini` → `/agents:start`; `codex` → `$agents-start`; `opencode`/`pi`/`cursor` → `/agents-start`.

## Steps

1. `date` → stamp `YYYY-MM-DD-HHMM`. `mkdir -p agents/<Target>/peer`. Write `agents/<Target>/peer/<stamp>-task-from-<Caller>.md` with the frontmatter from task.md § 4 (`type: task`, `status: open`, `outcome:` (the outcome file path and one line), `lead_pane: $HERDR_PANE_ID`, `rounds: 0`, `timebox:` from `task-timebox:` in `agents/CONVENTIONS.md` frontmatter (default `4h`; longer only if the principal set it in the outcome), and `due:` = now + timebox as `YYYY-MM-DD HH:MM`) and a self-contained `## Work order`: deliverable, acceptance criteria, every path/ticket/branch/ID, out of scope, who else is involved. End with an empty `## Log`.
2. `herdr pane layout --pane "$HERDR_PANE_ID"` → wide → `right`, tall → `down`. `herdr pane split --current --direction <dir> --cwd "$PWD" --no-focus` → read `.result.pane.pane_id`. Write it into `task_pane:`.
3. `herdr pane rename <id> "<Target> - Task"`.
4. `herdr agent start <target>-task --kind <harness> --pane <id> -- <agent args>` (same agent args as `/agents:ask` step 4; leave off ` --` when there are none). On `agent_not_ready`, wait once with `herdr agent get`; still blocked → read the pane, tell the principal, stop.
5. `herdr agent prompt <target>-task "<start> <target> task agents/<Target>/peer/<file>"` — **no `--wait`**: the task can run for hours. Then `herdr agent wait <target>-task --until working --timeout 60000`: a fresh pane starts idle, so a watch armed before it is working wakes at once.
6. Start watching in the background: `herdr agent wait <target>-task --until idle --until done --until blocked` (Claude Code: Bash with `run_in_background`; other harnesses: check back with `herdr agent get <target>-task` when the principal next speaks). Each time it wakes and the work order is still `working` while the pane is working again, re-arm it.
7. Add the work order to the outcome file's Plan. Tell the principal in two lines: what was delegated to whom, and the work order path.

**In T3, steps 2 to 6 are one call instead** (t3.md): `lead_pane: t3` in step 1; then from the workspace root, in the background (Claude Code: Bash with `run_in_background`), run `"$T3CODE_CLI_PATH" thread start . "<start> <target> task agents/<Target>/peer/<file>" --title "<Target> - Task" --json --wait` (plus `--model` if set). With `--wait` the thread id prints only when the call returns, so take it now from `"$T3CODE_CLI_PATH" thread list --json` (the newest thread titled `<Target> - Task`, `status: running`) and write `task_pane: t3:<id>` into the work order. The call returns when the member ends its turn; exit 2 means it waits on an approval in its own thread, which you treat as `blocked`. Every later prompt is `"$T3CODE_CLI_PATH" thread send <id> "Answered in <file>, continue." --wait`, again in the background. Where task.md says close the pane (an accepted hand-back, a stopped task), settle the thread with T3's thread tool (`t3_thread_organize`, `action: settle`, with its thread id; the MCP prefix differs per harness). It moves to the sidebar's Settled list and stays readable; skip silently if the tool is not available. Then step 7.

When the wait returns, run `date`, and follow task.md § 6 (Watching, Answering, Stopping): past `due:` means stop and bring it to the principal; 5 `rounds` (answers plus rejected hand-backs) means stop answering and bring it to the principal.

## Rules

- One work order per call; one task per member at a time.
- Never edit the member's files, worktree or tracker. Direct through the work order.
- Never answer a task pane's permission prompt; tell the principal.
- Do not commit here. The work orders you wrote or answered are staged at your session end (task.md § 6).
