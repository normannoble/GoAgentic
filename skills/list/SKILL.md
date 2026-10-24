---
description: List the agents in this workspace with their scope and role. Use /agents:list, /agents:list <scope> to filter, or /agents:list all to include retired agents.
disable-model-invocation: true
allowed-tools: Read, Glob, Grep, Bash(echo:*), Bash(herdr agent list:*), Bash("$T3CODE_CLI_PATH" thread list:*)
argument-hint: [scope|all]
---

# /agents:list — Agent Directory

You list the agents in this workspace. You do not activate one.

## Workspace Detection

Glob for both `agents/*/context.md` (single-domain) and `agents/*/*/context.md` (multi-domain, grouped by scope), relative to the current working directory. Use whichever matches, or both if mixed. Directory names match case-insensitively.

**Retired agents.** A `context.md` whose frontmatter has `status: retired` is hidden unless `$ARGUMENTS` is `all`.

## Procedure

1. Glob as above.
2. Read each `context.md` frontmatter to extract `scope`, `title`, and the agent name (from the directory name).
3. Present a table:

```
| Name | Scope | Role |
|------|-------|------|
| Sigrid | — | Senior Product Manager |
```

**Live column (Herdr or T3).** Run `echo "$HERDR_ENV"` (you cannot see env vars otherwise). If it prints `1`, run `herdr agent list` and keep only entries whose `cwd` is under the current working directory and whose `name` matches an agent here (lowercase). Add a `Live` column: the pane id (`w1:p3`) plus state (`idle`, `working`, `blocked`), `w1:p3 · w1:p5` if more than one, `—` if none. Ignore every other entry; never show agents from other roots. In T3 Code (`HERDR_ENV` empty, `T3CODE_CLI_PATH` set; run `echo "T3=$T3CODE_CLI_PATH"`), take the column from `"$T3CODE_CLI_PATH" thread list --json` instead: `<Name> - …` threads that are live (t3.md: a `<Name> - Peer` or `<Name> - Task` thread with `status: running`, or any other `<Name> - …` thread, a human session, with `settled: false`), shown as the title (`Sigrid - Task`).

If a scope filter was given (e.g., `/agents:list Acme`), only show agents in that scope. `/agents:list all` includes retired agents, with a Status column (`active` / `retired since <date>`).

4. End with one line: `Start one with /agents:start <name>. Board: /agents:status. One move: /agents:next.`
