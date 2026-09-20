---
name: wrap
description: End the current agent session and save its state — review the work, update the action tracker, write the session memory, update MEMORY.md and project files, then commit and push. Run it from inside an active agent session; no name needed. Same as /agents:start <name> close. To also clear the conversation afterwards, use /agents:close.
disable-model-invocation: true
allowed-tools: Read, Write, Edit, Glob, Grep, Bash(date), Bash(git add:*), Bash(git commit:*), Bash(git push:*), Bash(git status), Bash(git log:*), Bash(echo:*), Bash(ls)
argument-hint: (none — wraps the session you are in)
---

# /agents:wrap — End the session

`/agents:wrap` ends the agent session you are currently in and saves its state. It is the standalone form of `/agents:start <name> close` — run it from inside an active session, with no name. (`/agents:close` does the same and then clears the context.)

## Procedure

1. **Identify the active agent.** You have been operating as an agent in this conversation (started earlier with `/agents:start <name>`). That agent is the one to wrap; its directory is `agents/<name>/` or `agents/<scope>/<name>/`.
   - If no agent has been activated in this conversation, do **not** guess or pick one. Say: "No agent is active in this session — nothing to wrap. To close a specific agent, run `/agents:start <name> close`." Then stop.
2. **Run the Session End Protocol — all eight steps, in order.** Read `${CLAUDE_PLUGIN_ROOT}/template/agents/reference/session-end.md` (if the variable is empty: `$AGENT_FRAMEWORK_ROOT/template/agents/reference/session-end.md`, or the framework root a wrapper skill named; else glob `~/.claude/plugins/cache/*/agents/*/template/agents/reference/session-end.md` and take the highest version; in copied mode `.claude/skills/agents/template/agents/reference/session-end.md`) and follow it exactly: review the session, update the action tracker, review autonomy, write the session memory, update MEMORY.md, update project files, commit and push, confirm. Do not skip steps or improvise the order.

That is the whole command. It takes no arguments.
