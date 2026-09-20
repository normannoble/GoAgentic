---
name: close
description: End the current agent session, save its state, and then clear the conversation for a clean start. It is /agents:wrap followed by /clear. Run it from inside an active agent session; no name needed. Use /agents:wrap instead if you want to keep the conversation open after saving.
disable-model-invocation: true
allowed-tools: Read, Write, Edit, Glob, Grep, Bash(date), Bash(git add:*), Bash(git commit:*), Bash(git push:*), Bash(git status), Bash(git log:*), Bash(echo:*), Bash(ls)
argument-hint: (none — closes the session you are in)
---

# /agents:close — End the session and clear context

`/agents:close` is `/agents:wrap` followed by `/clear`: it saves the session's state, then clears the conversation so the next session starts fresh. Because the Session End Protocol persists everything to disk (tracker, memory, commit) and `/clear` keeps the conversation resumable, nothing is lost by clearing.

## Procedure

1. **Wrap the session first.** Do everything `/agents:wrap` does: read `${CLAUDE_PLUGIN_ROOT}/skills/wrap/SKILL.md` (if the variable is empty, glob `~/.claude/plugins/cache/*/agents/*/skills/wrap/SKILL.md` and take the highest version; in copied mode `.claude/skills/agents/skills/wrap/SKILL.md`) and follow it exactly — identify the active agent and run the full eight-step Session End Protocol.
   - If `/agents:wrap` finds no active agent, stop as it says. Do **not** clear the context.
2. **Confirm the wrap fully succeeded** before clearing: the action tracker and session memory are written and the commit (Session End Protocol step 7) reported success. If any step failed, stop and report it — do **not** clear the context, so the unsaved work stays visible.
3. **Clear the context.** As your final action, say one short line (e.g. "Session wrapped and committed. Clearing context.") and then run the built-in `/clear` command to reset the conversation for the next session.

It takes no arguments.
