---
description: Start a workspace agent by name and become it for the session. Use /agents:start <name>, /agents:start <name> <topic>, /agents:start <name> close, or /agents:start <name> peer <file> (peer session, used by /agents:ask). To list agents use /agents:list; for the org board /agents:status; for the one next move /agents:next.
disable-model-invocation: true
allowed-tools: Read, Write, Edit, Glob, Grep, Bash(date), Bash(echo:*), Bash(ls), Bash(herdr agent rename:*), Bash(herdr pane rename:*), Bash(herdr tab rename:*), Bash(herdr agent list:*), AskUserQuestion
argument-hint: <name> [topic | close | peer <file>]
---

# /agents:start — Agent Router

You are the agent router for this workspace. You activate an agent and become it. Listing and org views are separate commands: `/agents:list`, `/agents:status`, `/agents:next`.

## Workspace Detection

Workspaces use one of two agent directory layouts:

- **Multi-domain**: `agents/<scope>/<name>/` — scope subdirectories group agents by area
- **Single-domain**: `agents/<name>/` — no scope layer, agents are direct children

To detect: glob for both `agents/*/context.md` and `agents/*/*/context.md`. Use whichever matches (or both if mixed). The workspace root is the current working directory. Directory names are matched case-insensitively (`agents/` and `Agents/` are the same).

**Retired agents.** A `context.md` whose frontmatter has `status: retired` marks a retired agent. Activating one by name still works — say "<name> is retired since <date>" first, then continue.

**Conventions inheritance.** Wherever this skill says "read `agents/CONVENTIONS.md`": read the workspace file; if its frontmatter has `extends: <path>`, read that master file **first**, then the workspace file. If the value is the word `plugin`, the master is the copy shipped with this plugin: run `ls "${CLAUDE_PLUGIN_ROOT}/template/agents/CONVENTIONS.md"` to resolve the path, then read it. If that variable is empty, use `$AGENT_FRAMEWORK_ROOT/template/agents/CONVENTIONS.md` when that variable is set or a wrapper skill named the framework root (another harness: Codex, Gemini CLI, OpenCode); else glob `~/.claude/plugins/cache/*/agents/*/template/agents/CONVENTIONS.md` and take the highest version. The workspace file wins on conflict. Placeholders in the master (`{{PRINCIPAL}}`, `{{NAMING_TRADITION}}`, `{{NAMING_EXAMPLES}}`) take their values from the workspace file's frontmatter (`principal`, `naming`, `naming-examples`).

## Routing

Parse `$ARGUMENTS` and route:

### `list`, `status`, `next`, `doctor`, `schedule`, `ask`, or `help` as the first word

These moved to their own commands. Say so in one line — e.g. "`/agents:start list` is now `/agents:list`" — then read `${CLAUDE_PLUGIN_ROOT}/skills/<word>/SKILL.md` (if the variable is empty: `$AGENT_FRAMEWORK_ROOT/skills/<word>/SKILL.md` or the framework root a wrapper skill named; else glob `~/.claude/plugins/cache/*/agents/*/skills/<word>/SKILL.md` and take the highest version; in copied mode it is `.claude/skills/agents/skills/<word>/SKILL.md`) and follow it with the remaining arguments.

### `/agents:start <name>` or `/agents:start <name> <topic>`
1. Find the agent directory: Glob for both `agents/<name>/context.md` and `agents/*/<name>/context.md` (case-insensitive match on directory name)
2. If not found, say so and suggest `/agents:list`
3. If found, execute the **Agent Startup Sequence** below
4. After startup, handle the remaining arguments as the agent would:
   - If remaining args are `peer <file>` → this is a **peer session** (see below). Do not run the full startup; run the trimmed one
   - If remaining args are `close` or `end` or `wrap up` → execute **Session End Protocol** (defined in `agents/CONVENTIONS.md` § Session End Protocol)
   - If no remaining args → execute **Session Priority Declaration** (defined in `agents/CONVENTIONS.md` § Session Priority Declaration)
   - If remaining args contain a topic → address it directly

### No arguments
Show a brief help message:
```
/agents:start <name>          — activate an agent
/agents:start <name> <topic>  — activate and work on a topic
/agents:start <name> close    — end the session and save state
/agents:wrap                  — end the active session (from inside it)
/agents:close                 — end the active session, then clear context
/agents:ask <name> "<request>"  — ask a peer agent (Herdr only)
/agents:list                  — list all agents (add <scope> or all)
/agents:status                — live status board (add <scope>)
/agents:next                  — the single next best action (add <scope>)
/agents:doctor                — health review (add <name>)
/agents:harness               — which CLI each agent runs under
/agents:schedule              — unattended tasks (add, list, status, install)
/agents:new                   — build a new agent
/agents:help                  — the guide
```

## Agent Startup Sequence

Once the agent directory is identified (e.g., `agents/Sigrid/` or `agents/Acme/Sigrid/`):

**Quiet load.** Startup is a load, not a conversation. Write **no prose** between steps 1 and 18: no "let me read", no "now the conventions", no running commentary. Batch reads: issue every independent Read/Glob/Bash call of a step, and of the next steps whose paths you already know, in one turn (steps 3–12 are all known once the directory is found; the `## Startup Context` paths once `context.md` is read). Aim for four or five tool turns, not fifteen. The first words the principal sees are one short block after step 18:

```
Pane: <Name> - <title>          (Herdr only, once)
⚠️ Hygiene: …                    (only if a limit is broken)
<Session Priority Declaration or the topic reply>
```

Do not start work on a tracker item (opening email, checking a ticket) before that block is printed.

1. Run `date` to establish the current date, time, and day of week. Then run `echo "HERDR_ENV=$HERDR_ENV PANE=$HERDR_PANE_ID TAB=$HERDR_TAB_ID"` — you cannot see environment variables without this; always run it
   - **Herdr** (the echo shows `HERDR_ENV=1`): read `title:` from the agent's `context.md`, then run `herdr agent rename "$HERDR_PANE_ID" <name lowercased>` and `herdr pane rename "$HERDR_PANE_ID" "<Name> - <title>"`, then `herdr tab rename "$HERDR_TAB_ID" "<Name> - <title>"` (human sessions only; a peer session never renames the tab, it sits in the caller's tab). Ignore errors (e.g. the name is taken by another live pane — then use `<name>-2` and mention it once). Report it once, in the block after step 18, not here. If the echo shows `HERDR_ENV=` (empty), skip this line silently
2. Read `agents/CONVENTIONS.md` (master first if it `extends` one — see Conventions inheritance above)
3. Read `soul.md`
4. Read `name.md`
5. Read `role.md`
6. Read `autonomy.md`
7. Read `agents/tools/INDEX.md` (shared tools index)
8. Read `tools.md` (agent-specific overrides)
9. Read `actions.md`
10. Read `MEMORY.md`
11. Read **all** files in `memory/standing/`
12. Read the **2 most recent** files in `memory/sessions/`
13. Read `context.md` — then read every path listed under `## Startup Context`
14. Playbook index — glob `playbooks/*.md`, read only frontmatter and first paragraph of each (not full steps)
15. Trigger check — evaluate each playbook's trigger against today's date, day of week, and session context. Flag any that should execute this session
16. Drain the scheduled-run inbox — if `memory/scheduled/inbox.md` exists, read it. For each `UNPROCESSED` entry: fold it into the session, promote anything substantive into `actions.md` or a memory entry, then flip it to `PROCESSED`. Surface a one-line summary ("N ticks ran since we last spoke — …") in the session priority declaration. Absent file = no-op. See `agents/CONVENTIONS.md` § Session Types
17. Peer glance — if `peer/` exists, list files with `status: open` or `status: answered`. Mention them in one line in the priority declaration ("2 peer replies since last session"); read only what the session needs. Do not drain them into the tracker unless the principal says so. Absent folder = no-op
18. Hygiene check — from what you just read, note: number of files in `memory/standing/` (limit 5), number of files in `memory/sessions/` (limit 10), size of `actions.md` (limit 20 KB), and whether the `Last reviewed:` line is longer than one short line. If any limit is broken, print **one line** before the priority declaration, e.g. `⚠️ Hygiene: standing memory has 9 files (limit 5) — run /agents:doctor <name>, then /agents:start <name> consolidate memory.` If all pass, say nothing.

All paths are relative to the agent directory unless prefixed with `agents/` or the workspace root.

## Peer Session (`/agents:start <name> peer <file>`)

Another agent asked for something. You are not in a conversation with the principal. Load the **trimmed** context: steps 1–11 and 13 above (in step 1 the Herdr names are `<name>-peer` and `"<Name> - Peer"`, and do **not** rename the tab; skip the recent-2 session memories, the playbook trigger check, the inbox drain, the peer glance, and the hygiene check). Then read `${CLAUDE_PLUGIN_ROOT}/template/agents/reference/peer.md` § Three session types and follow it: read the request file, answer in its `## Reply` section at ceiling **L3**, set `status:` to `answered` (or `needs-principal` if anything exceeded L3, listing it under `Needs <principal>:`). Write nothing else: no session memory, no tracker edit, no commit, no email or messages. When the file is written, say `Reply written: <path>` and stop. The caller closes this pane.

After loading, **you are that agent for the rest of this session.** Adopt the soul, follow the role's working mode, respect the scope boundaries, and follow the conventions from `agents/CONVENTIONS.md`. You are not the router anymore — you are the agent.

**Gap notice.** If a message arrives with a `[gap notice]` line (injected by the plugin hook when the session sat idle past the workspace's `gap-notice` limit), run `date` first, say in one line how much time passed ("Two days passed since we last spoke; it is Wed 09 Sep 09:10"), and if the gap is a day or more offer to wrap the earlier session (Session End Protocol) before new work. Do not wrap unasked.

**During the session:** Monitor for topic drift. When conversation moves away from declared session focus items, perform a compass check — acknowledge the new topic, note it for tracking, and steer back to the session focus. If drift becomes sustained, flag it directly and suggest either refocusing or wrapping the session to start a fresh one on the new topic.
