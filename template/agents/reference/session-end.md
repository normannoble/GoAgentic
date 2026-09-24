# Session End Protocol

Reference for `agents/CONVENTIONS.md` (the master). Loaded on demand, not at startup.

## Session End Protocol

Triggered by `/agents:start <name> close`, `/agents:start <name> end`, `/agents:start <name> wrap up`, or when {{PRINCIPAL}} signals the session is ending.

### Step 1: Review the session

Scan the full conversation and identify:
- Topics discussed
- Decisions made (with rationale)
- Action items created or completed (who, what, by when)
- Open questions or unresolved items
- Communications sent or received
- Documents created, updated, or shared
- Any changes to project state
- **Session focus reconciliation:** Which declared focus items were addressed? Which were not, and why? Did any side topics emerge that need P1/P2 actions?

### Step 2: Update action tracker

Update `actions.md`:
- Add new action items to Open
- Move completed items to Completed with date
- Update statuses and flag items at risk or overdue

### Step 3: Review autonomy

Check if {{PRINCIPAL}} gave any explicit signals about authority levels during the session:
- "Good call, just do that next time" or similar → **promotion**
- "Check with me before doing that" or similar → **demotion**
- Agent felt uncertain about authority level → note for clarification next session

If any changes occurred, update `autonomy.md` (both the level sections and the changelog).

### Step 4: Create session memory

Write to `memory/sessions/YYYY-MM-DD-<topic>.md`:
```yaml
---
date: YYYY-MM-DD
type: session
session_id: ${CLAUDE_SESSION_ID}
resume: claude --resume ${CLAUDE_SESSION_ID}
herdr_pane: ${HERDR_PANE_ID}   # omit the line outside Herdr
---
```
Include topics discussed, decisions made, and open questions. Do NOT duplicate action items — reference `actions.md`.

Never invent a `session_id`. In Claude Code, if `${CLAUDE_SESSION_ID}` is empty, the ID is the filename (without `.jsonl`) of the active conversation file. Look it up from the workspace root:
```bash
find ~/.claude/projects/-$(pwd | tr '/' '-' | cut -c2-) -name "*.jsonl" -mmin -60 -not -path "*/subagents/*" | head -1 | xargs basename | sed 's/.jsonl//'
```

**Other harnesses.** The `session_id` and `resume` lines depend on the CLI you are running in. Use the row that matches; if the harness shows no session ID, write `session_id: none`.

| Harness | `session_id` | `resume` |
|---------|--------------|----------|
| Claude Code | `${CLAUDE_SESSION_ID}` or the lookup above | `claude --resume <id>` |
| Codex | `none` unless shown | `codex resume` (picker) |
| Gemini CLI | `none` unless shown | `gemini --resume` (pick from `gemini --list-sessions`) |
| OpenCode | `none` unless shown | `opencode run --continue` |

If the session produced durable rules or decisions, write a separate entry to `memory/standing/` and add it to the Standing section in MEMORY.md.

### Step 5: Update memory index

Add a one-line summary with link to `MEMORY.md` under the appropriate section.

### Step 6: Update project files

Read `context.md` for the list of project files. Update relevant project logs and status files if progress was made.

### Step 7: Commit and push

Stage **only what this session changed**: your own agent directory plus any project files you updated in Step 6.

```bash
git add agents/<Name>/ <project files from Step 6>   # multi-domain: agents/<scope>/<Name>/
git commit -m "<descriptive message>"
git push || { git pull --rebase --autostash && git push; }
```

Never `git add -A`, `git add .`, or `git commit -a`. Other agents in this workspace may be mid-session with uncommitted work; a blanket stage commits it under your name, half-finished. If `git status` shows changes outside your paths, leave them for their owner.

### Step 8: Confirm

Show {{PRINCIPAL}} a brief summary of what was saved and the current action item status.
