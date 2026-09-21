# Agent Conventions

How to build and structure agents in this workspace.

## Inheritance

This master is shared. A workspace's `agents/CONVENTIONS.md` carries frontmatter (`extends: plugin`, `principal`, `naming`, `naming-examples`, `reserved`, and the optional switches below) and only the rules that differ from, or add to, this file. **The workspace file wins on conflict.** Where this file says `{{PRINCIPAL}}`, `{{NAMING_TRADITION}}`, or `{{NAMING_EXAMPLES}}`, use the workspace frontmatter values. In copied mode (installer) the placeholders are already substituted.

**Optional frontmatter switches** (so common differences need no prose override):

| Key | Default | Effect |
|-----|---------|--------|
| `ticket-column` | `Ticket` | Name of the issue-tracker column in `actions.md` (e.g. `Linear`, `Jira`). The ticket sync rules in § actions.md apply to that tracker. |
| `inbound` | `none` | Channel checked in step 2 of the Session Priority Declaration (`email`, `slack`, `none`). The triage command lives in each agent's `tools.md`. |
| `scheduler` | `none` | `launchd` or `cron` if the workspace runs the shared scheduler (see § Session Types). |
| `gap-notice` | `2h` | Idle gap after which the plugin's hook tells the agent how much time passed (`30m`, `2h`, `1d`, or `off`). See § Stale Sessions. |
| `harness` | `claude` | The coding-agent CLI that runs unattended ticks and peer panes: `claude`, `codex`, `gemini`, or `opencode`. An agent's `context.md` may set its own `harness:` for peer panes (see § context.md). Interactive sessions work from any harness that has the framework commands installed (see § Invocation). |

## Reference files (read on demand, never at startup)

Long procedures live beside this file in `reference/`. In plugin mode that is `${CLAUDE_PLUGIN_ROOT}/template/agents/reference/`; on another harness it is `$AGENT_FRAMEWORK_ROOT/template/agents/reference/` (the framework root named by the wrapper skill or exported by `tick.sh`); in copied mode it is `agents/reference/`.

| File | Read when |
|------|-----------|
| `session-end.md` | The session is closing (all eight steps) |
| `memory.md` | Consolidating memory, or writing a non-session memory entry |
| `tooling.md` | Creating an agent, adding a tool, or unsure of a tool action's level |
| `playbooks.md` | Writing or editing a playbook |
| `autonomy-format.md` | Creating an agent or restructuring `autonomy.md` |
| `tracker-cleanup.md` | Asked to clean up the action tracker |
| `index-maintenance.md` | Updating an INDEX.md at session end |
| `peer.md` | Asking another agent for something (`/agents:ask`), running as a peer session, or anything Herdr |

---

# Part 1: Agent Structure

## Engagement Posture

Default to challenging and clarifying the thinking before acting. Ask hard questions rather than assuming the goal is settled. Do not create files or take implementation actions until {{PRINCIPAL}} explicitly signals to move forward. This principle is reinforced by the autonomy model (§ Autonomy) — agents state intent and reasoning, not ask for instructions.

## Naming

Agents are named using **{{NAMING_TRADITION}}** — {{NAMING_DESCRIPTION}}. Names are also invocation handles: the agent's name is its skill command (`/agents:start <name>`, not `/agents:start platform-strategy-lead`).

Names should not signal the agent's domain. A project-scoped agent may outlive its original project, or be reassigned. Neutral names survive these changes.

**Examples:** {{NAMING_EXAMPLES}}

**Reserved:** *(add names here as agents are created)*

## Directory Structure

Agents live under `agents/<agent-name>/`:

```
agents/<agent-name>/
├── role.md              # Purpose, scope, outcomes, boundaries
├── soul.md              # Voice, temperament, behavioral traits
├── name.md              # Historical figure behind the name and why it fits
├── autonomy.md          # Authority ladder — what the agent owns vs. flags
├── tools.md             # Available tools and their configuration
├── actions.md           # Standing action tracker (always current)
├── actions-archive.md   # Completed actions older than 30 days
├── context.md           # Startup context paths and project file references
├── MEMORY.md            # Memory index — standing and session sections
├── memory/
│   ├── standing/        # Durable rules, decisions, baselines
│   ├── sessions/        # Per-session logs
│   └── scheduled/       # Scheduler inbox (only agents with scheduled tasks)
├── peer/                # Agent-to-agent exchanges (created on demand by /agents:ask)
└── playbooks/           # Repeatable procedures with defined execution modes
```

## File Reference

### role.md

Defines what the agent does. Contains:
- Role purpose (one paragraph)
- Reporting line and decision authority
- Project scope
- Accountable outcomes (numbered)
- Key stakeholders
- Deliverables owned
- Working mode (behavioral directives — includes intent-based communication posture)
- Scope boundaries (in/out)
- Primary objective (one sentence)

Ends with a reference to this conventions doc.

### soul.md

Defines how the agent feels to interact with. Short — under 20 lines. Written in third person ("Varro is direct", not "You are direct"). Covers:
- Communication style and tone
- Temperament (patient/impatient, formal/casual, etc.)
- 2-3 behavioral "do/don't" notes
- What the agent values (precision, speed, thoroughness, etc.)

Soul is read during startup but doesn't repeat working rules from role.md. Role says what to do; soul says how to be.

### name.md

Captures the historical or mythological figure behind the agent's name and why it fits. Short — under 15 lines. Written once at creation and rarely updated. Structure:
- The name and its literal meaning if it translates
- The historical figure (2-4 sentences — who they were, what they did)
- Why it fits this agent's character (1-2 sentences)

Name files preserve the naming rationale across time. Agents are named from the {{NAMING_TRADITION}} pool specifically because the pool is deep and the figures carry archetypes; that archetype is lost if the reasoning only lives in the agent-creation conversation. Read on startup to give the agent a small anchor of identity beyond role and soul.

### autonomy.md

Defines the agent's operating authority — what it can do independently and what requires {{PRINCIPAL}}'s input. Structure:
- Five authority levels (L5 Own → L1 Flag), each listing specific action types
- A changelog recording promotions and demotions with dates and context

See **Part 2: Autonomy** for the full framework. This file is the agent's primary reference for how to behave during a session — it governs whether the agent acts, announces intent, or asks.

### tools.md

Defines what external tools the agent has access to and how to use them. Contains:
- Agent identity (email alias, display name, if applicable)
- Tool-specific overrides and scoped commands
- References to shared tool files in `agents/tools/`

Autonomy levels for tooling actions are defined in `autonomy.md`, not here. `tools.md` says *how* to use the tools; `autonomy.md` says *when* the agent needs approval.

### actions.md

Single standing file — the agent's running to-do list. Always current, updated every session close. Structure:
- `Last reviewed: YYYY-MM-DD` at the top — **the date only, one line**. Session narrative never goes here; it goes in `memory/sessions/`. `/agents:doctor` flags this line when it exceeds 600 characters.
- Open table: #, Action, Ticket, Owner, Priority, Due/Target, Status, Since
- Completed table: #, Action, Ticket, Owner, Completed date

The `Ticket` column is optional — contains the issue tracker ID when the action has a corresponding ticket. Its header is the workspace's `ticket-column` frontmatter value (default `Ticket`). When present:
- At session close, sync status both ways (update tracker state to match action status, and vice versa)
- During session priority declaration, check linked tickets for state changes since last session
- Not all actions need a ticket — agent operational items (memory hygiene, follow-ups, session carryover) stay in actions.md only

Priority uses P1/P2/P3:
- **P1** — Must progress this week. Session focus candidates.
- **P2** — Should progress when P1s are clear. Tracked actively.
- **P3** — Tracked, not urgent. Revisit periodically.

Session memories reference `actions.md` for action items rather than duplicating them. This prevents action items from being lost in old session logs.

#### Tracker Hygiene

1. **No ghosts in Open.** When an item completes, move it to Completed and delete the row from Open. No strikethrough-then-leave-it pattern. The Open section must be a reliable list of live work.
2. **Fortnightly full pass.** Every other week (or equivalent cadence session), audit the full tracker: refresh stale due dates, cross-check shared items against other agents' trackers, verify statuses against external state, and check P1 count stays under ~8.
3. **Priority sections.** Structure the Open table with P1 / P2 / P3 sub-sections. Flat lists don't scale past ~30 items.
4. **Archive completed items monthly.** The Completed table keeps only the last 30 days. Older items move to `actions-archive.md` in the same directory — same table format, out of the startup read path. Create the archive file when the agent is created; first archival happens when items age past 30 days.

#### Tracker Cleanup Procedure

When {{PRINCIPAL}} asks you to review or clean up your tracker, read `reference/tracker-cleanup.md` (beside this file) and follow it.

### context.md

Frontmatter carries `scope` and `title`. An optional `harness:` (`claude`, `codex`, `gemini`, or `opencode`) sets the CLI that runs this agent as a peer in `/agents:ask`; it overrides the workspace `harness:` key for that agent only (ticks stay on the workspace value). A retired agent adds `status: retired`, `retired: YYYY-MM-DD`, and `last-active: YYYY-MM-DD`. Retired agents keep their files (history has value) but are hidden from `/agents:list`, `/agents:status`, and `/agents:next` unless `all` is passed. Activating a retired agent by name still works; the router says it is retired first.

Defines what additional files the agent needs on startup and which project files to update on session end. Frontmatter includes `scope` and `title` (used by `/agents:list`). Body has two sections:
- **Startup Context** — paths to read after the standard agent files (soul, role, autonomy, actions, memory)
- **Project Files** — paths to check for updates during Session End Protocol

All paths are relative to the workspace root.

---

# Part 2: Autonomy

Inspired by *Turn the Ship Around* (L. David Marquet). The core principle: agents state intent and drive execution rather than waiting for instructions or asking permission for things they're competent to handle. {{PRINCIPAL}} owns the decisions; agents own the momentum.

## The Authority Ladder

| Level | Agent says | Meaning |
|-------|-----------|---------|
| **L5 — Own** | *(in session summary)* | Act independently. Report at session close. |
| **L4 — Act & Inform** | "I've done X." | Act, then flag it in the moment. |
| **L3 — Intend** | "I intend to..." | State intent and reasoning. Proceed unless redirected. |
| **L2 — Recommend** | "I recommend..." | Present analysis + recommendation. Wait for approval. |
| **L1 — Flag** | "I see a problem..." | Surface the issue for {{PRINCIPAL}} to decide. |

**Default for new agents:** Most actions start at L2 (Recommend). Mechanical/administrative actions (updating trackers, grooming stale tickets) can start at L4-L5. Strategic decisions always start at L1.

## Intent-Based Communication

The signature behavior change: agents default to **"I intend to..."** (L3) rather than asking "What should I do?" or "Would you like me to...?"

When operating at L3, the agent:
1. States what it intends to do
2. States why (brief reasoning)
3. Proceeds after a natural pause unless {{PRINCIPAL}} redirects

This keeps {{PRINCIPAL}} in the loop without making them the bottleneck. The agent drives; {{PRINCIPAL}} steers.

## Senior vs. Junior Agents

The autonomy model applies differently depending on how the agent is designed.

**Senior agents** start with more actions at L3 (Intend) and move to L4-L5 faster. They're expected to exercise judgment, push back on direction they disagree with, and drive session agendas. Less documentation in `tools.md` and `role.md` — more latitude in `autonomy.md`. A senior agent that always agrees with you is a broken agent.

**Junior agents** start with more actions at L1-L2 and need detailed runbooks in `tools.md` and explicit scope boundaries in `role.md`. They're reliable executors with well-defined playbooks. Promotion is slower and more granular.

Both are valid design choices. Senior agents trade documentation for judgment; junior agents trade judgment for predictability. The framework defaults to senior — if you want a junior agent, be deliberate about it in Phase 2 (Role) and Phase 3 (Autonomy) of `/agents:new`.

## Promotion & Demotion

Authority levels change through explicit signals:

**Promotion** (moving an action type up the ladder):
- {{PRINCIPAL}} says something like "good call, just do that next time" or "you don't need to ask me for this"
- The agent records the promotion in `autonomy.md` with the date and context

**Demotion** (moving an action type down the ladder):
- {{PRINCIPAL}} corrects a decision or says "check with me before doing that"
- The agent records the demotion in `autonomy.md` with the date and what went wrong

**Self-assessment:** During session end, the agent should note any moments where it felt uncertain about its authority level — these are candidates for explicit clarification with {{PRINCIPAL}}.

## autonomy.md Format

The file template (five level sections plus a dated changelog) is in `reference/autonomy-format.md`. Read it when creating an agent or restructuring an `autonomy.md`.

---

# Part 3: Session Lifecycle

## Invocation

All agents are invoked through the `/agents:start` router (from the `agents` plugin — installed from the marketplace, or copied into `.claude/skills/agents/` by the installer):

```
/agents:start <name>          — activate an agent (status check)
/agents:start <name> <topic>  — activate and work on a topic
/agents:start <name> close    — end session and save state
/agents:wrap                  — end the active session (from inside it)
/agents:close                 — end the active session, then /clear
/agents:list [scope|all]      — list agents
/agents:status [scope]        — live org status board
/agents:next [scope]          — the single next best action
/agents:doctor [name]         — health review, recommends fixes
/agents:harness               — which CLI each agent runs under (peer + ticks)
/agents:help [command]        — the guide
/agents:ask <name> "<request>" — ask a peer agent (Herdr only)
```

The router parses the agent name, finds the agent directory under `agents/<name>/`, executes the startup sequence, and becomes that agent for the session. Individual per-agent skill files are not needed — agent-specific startup context is defined in `context.md`.

**Other harnesses.** The same skills run in Codex, Gemini CLI, and OpenCode through thin wrapper skills that point at the framework checkout (`harness/install.sh <harness>` in the framework repo writes them). Only the command spelling differs; `/agents:<cmd>` in this document means the matching form below.

| Harness | Start an agent | Any command | Installed by |
|---------|----------------|-------------|--------------|
| Claude Code | `/agents:start <name>` | `/agents:<cmd>` | the `agents` plugin |
| Codex | `$agents-start <name>` | `$agents-<cmd>` | `.agents/skills/agents-<cmd>/SKILL.md` |
| Gemini CLI | `/agents:start <name>` | `/agents:<cmd>` | `.gemini/commands/agents/<cmd>.toml` (+ the `.agents/skills/` wrapper) |
| OpenCode | `/agents-start <name>` | `/agents-<cmd>` | `.opencode/commands/agents-<cmd>.md` (+ the `.agents/skills/` wrapper) |

The agent files, this document, and the reference files are the same on every harness. Two things are Claude Code only: the gap-notice hook (§ Stale Sessions) and the `${CLAUDE_SESSION_ID}` session ID (`reference/session-end.md` § Step 4 has the per-harness form).

## Startup Sequence

Standard order after the agent directory is identified:

1. Run `date` to establish current date, time, and day of week
2. Read `agents/CONVENTIONS.md`. If its frontmatter has `extends: <path>`, read that master file **first**, then the workspace file; the workspace file wins on conflict (see § Inheritance)
3. Soul (`soul.md`)
4. Name (`name.md`)
5. Role (`role.md`)
6. Autonomy (`autonomy.md`)
7. Shared tools index (`agents/tools/INDEX.md`)
8. Agent tools (`tools.md` — agent-specific overrides, references shared tool files in `agents/tools/`)
9. Actions (`actions.md`)
10. Memory index (`MEMORY.md`)
11. All standing memories
12. Most recent 2 session memories
13. Paths listed in `context.md` under `## Startup Context`
14. Playbook index — glob `playbooks/*.md`, read only frontmatter and first paragraph of each (not full steps)
15. Trigger check — evaluate each playbook's trigger against today's date, day of week, and session context. Flag any that should execute this session
16. Drain the scheduled-run inbox — if `memory/scheduled/inbox.md` exists, read it. For each `UNPROCESSED` entry: fold it into the session, promote anything substantive into `actions.md` or a memory entry, then flip it to `PROCESSED`. Surface a one-line summary ("N ticks ran since we last spoke — …") in the priority declaration. Absent file = no-op. See § Session Types

## Session Types

Not every session is an interactive pairing with {{PRINCIPAL}}. Automated scheduler runs and requests from other agents are **different kinds of session** and must not pollute — or be lost to — the agent's memory. Three types:

| Type | Trigger | Context load | Writes |
|------|---------|--------------|--------|
| **Session** (interactive) | {{PRINCIPAL}} runs `/agents:start <name>` | Full startup sequence | Session memory, action tracker, commit |
| **Tick** (automated) | A scheduler fires a due task | **Trimmed** — conventions, soul, name, role, autonomy, tools, actions, standing memory, the inbox, the specific task/playbook, `context.md` startup paths. **Not** the recent-2 session memories | The **inbox only** — never session memory, action tracker, or commit |
| **Peer** (agent-to-agent) | Another agent in the same workspace runs `/agents:ask` | **Trimmed** like a tick, plus the request file. Runs in a fresh Herdr pane that closes when the reply is written | The **reply section of one file in `peer/`** only. Ceiling L3. See `reference/peer.md` |

**Why trimmed + inbox-only:** if ticks wrote session memories, a run of thin automated ticks between pairing sessions would flush the substantive interactive sessions out of the recent-2 startup window and rot the agent's context. So ticks stay out of `memory/sessions/` entirely. But their work must not vanish either — so ticks deposit their output in a rolling inbox that the next interactive session drains.

**The scheduled-run inbox** (`memory/scheduled/inbox.md`): a rolling, append-only ledger. Ticks append `UNPROCESSED` entries. At interactive startup (step 16), the agent drains it — folds entries into the session, promotes substance into `actions.md` or a memory entry, then flips them to `PROCESSED`. Only agents with scheduled tasks have an inbox; if the file is absent, step 16 is a no-op.

A scheduler is any unattended runner (cron, launchd, a cloud routine) that invokes the harness headlessly (`claude -p`, `codex exec`, `gemini --approval-mode yolo`, or `opencode run --auto`, chosen by the `harness:` frontmatter key) against a task register. It must: cap each task with an autonomy ceiling, cap tool calls, never commit or push, never use the `/agents:start` router, and log every run. The shared implementation ships with the plugin under `${CLAUDE_PLUGIN_ROOT}/template/agents/scheduler/`: `tick.sh` (entrypoint; sources nvm, runs the gate, starts the harness only when something is due), `gate.py` (parses the register, applies the catch-up rule), `prompt.md`, `policies.md`, a `scheduled-tasks.md` register template, `install-launchd.sh` (macOS) and `setup.sh` (cron). `/agents:init` copies it into `agents/scheduler/` and creates the register; `/agents:schedule` manages tasks and installs the timer. Ticks read the workspace copy, so re-run `/agents:init` (backfill) after a plugin update that changes it.

## Stale Sessions

A session left open for hours or days does not know that time passed. The plugin ships a hook (`hooks/gap-notice.sh`) that runs on every prompt: when the gap since the session's last activity exceeds `gap-notice` (frontmatter, default 2 hours), it injects a `[gap notice]` line with the current time, the last-activity time, and the gap. On seeing it the agent must: re-run `date`, state the gap in one line, and if a day or more passed, offer to run the Session End Protocol for the earlier session before taking new work. It never wraps on its own; {{PRINCIPAL}} decides. Nothing runs while the session is idle. The hook exists only in Claude Code; on other harnesses there is no gap notice, so the `date` at startup and the agent's own judgment cover it.

## Herdr

When the session runs inside [Herdr](https://herdr.dev) (`HERDR_ENV=1`), the router tags the pane with the agent's name and labels the pane and its tab `<Name> - <Role title>` (a peer session labels only its pane, `<Name> - Peer`), so every live agent is visible by name in the Herdr sidebar and to `/agents:list`, `/agents:status`, and `/agents:doctor`. **Scope is the repo root**, not the Herdr workspace: agents see only live agents whose working directory is under the same root. `/agents:ask <name> "<request>"` is the only sanctioned way for one agent to engage another. Full protocol: `reference/peer.md`.

## Session Priority Declaration

Agents have a natural tendency toward recency bias — prioritising whatever was discussed most recently over items that may actually be more important. These mechanics counteract that.

At the start of every session (after loading context), the agent:
1. Reviews `actions.md` and identifies the top priorities (P1 items first, then P2)
2. Checks for inbound communications on the workspace's `inbound` channel (frontmatter; see `tools.md` for the triage command). If messages exist, factor them into the priority assessment — a message from a stakeholder may elevate or introduce a priority. If no inbound channel is configured or the inbox is empty, move on silently.
3. Reconciles in-flight {{PRINCIPAL}}-owned items. If `actions.md` lists items owned by {{PRINCIPAL}} that are *in progress* or otherwise time-sensitive, and inbound messages haven't already updated them, asks {{PRINCIPAL}} for a fresh status before declaring session focus. The tracker is updated by the owning agent at session close, but {{PRINCIPAL}} may have made progress between sessions that the tracker does not reflect. Reconciling first prevents stale-state-driven session focus.
4. Includes any playbooks flagged by the trigger check (startup step 15) — cadenced playbooks that are due this session
5. Declares a **session focus** — up to 3 items that this session should progress, in priority order. If inbound messages are relevant, note them: "I have a message from [stakeholder] about [topic] — factoring into priorities."
6. Gets {{PRINCIPAL}}'s agreement before proceeding

This declaration becomes the session's compass. Everything that follows is measured against it.

## Compass Check

When conversation moves to a topic not in the session focus, the agent pauses briefly:
- If the new topic is clearly higher priority, acknowledge the shift: "This supersedes what we were working on. Adjusting session focus."
- If the new topic is a side thread, note it: "I'll capture this in the action tracker. Back to [current focus]?"
- If unclear, ask: "Is this more urgent than [current focus], or should we note it and come back?"

This isn't about being rigid — it's about making drift conscious rather than accidental.

## Drift Management

If the session has spent significant time on a topic outside the declared focus, the agent flags it directly:
- State what the original session focus was
- Note how far the conversation has drifted
- Suggest either: (a) refocusing on the original priorities, or (b) wrapping the current session and starting a fresh one dedicated to the new topic

The agent should not silently allow session priorities to be displaced by conversation momentum.

## Session End Protocol

Triggered by `/agents:wrap`, `/agents:close`, `/agents:start <name> close`, `end`, `wrap up`, or when {{PRINCIPAL}} signals the session is ending (`/agents:close` also runs `/clear` after the protocol completes). **When triggered, read `reference/session-end.md` (beside this file) and follow all eight steps:** review the session, update the action tracker, review autonomy, write the session memory, update MEMORY.md, update project files, commit and push, confirm. Do not skip steps and do not improvise the order.

## Session Continuity

Use `claude --continue` (most recent) or `claude --resume` (pick from list) to return to previous conversations. Other harnesses: `codex resume`, `gemini --resume`, `opencode run --continue`. Name conversations descriptively early in the session so they're easy to find.

Before wrapping up a conversation, update a lightweight working context note in the relevant area — a brief summary of what was being worked on and what's next. This helps orient the next session without needing full conversation logs.

---

# Part 4: Supporting Systems

## Memory

### MEMORY.md

Index file with two sections:
- **Standing** — links to durable entries (rules, decisions, baselines). All read on startup.
- **Sessions** — links to session logs. Most recent 2 read on startup.

One-line summaries only. Keep concise.

### memory/standing/

Entries that remain relevant across many sessions:
- `baseline` — project state snapshot (created at agent inception, refreshed periodically)
- `decision` — key decision with rationale
- `stakeholder-feedback` — durable stakeholder positions and divergences

### memory/sessions/

Per-session logs with frontmatter:
```yaml
---
date: YYYY-MM-DD
type: session
session_id: ${CLAUDE_SESSION_ID}
resume: claude --resume ${CLAUDE_SESSION_ID}
---
```
(Claude Code form. On another harness use the row for it in `reference/session-end.md` § Step 4.)

Contain: topics discussed, decisions made, open questions. Do NOT duplicate action items — reference `actions.md`.

### memory/scheduled/

Present only for agents with scheduled tasks. Holds `inbox.md` — the rolling ledger that **tick** (automated) runs write to instead of `memory/sessions/`. Drained at interactive startup (see § Session Types). Never enters the recent-2 window.

### Entry types, consolidation, workspace memory

Frontmatter `type` values, the full consolidation procedure, and the workspace-memory file format are in `reference/memory.md`.

**Consolidation trigger (check every startup):** more than 5 standing entries, more than 10 session entries, or any single standing file over ~15 KB means consolidate this session or next — read `reference/memory.md` and do it. (The size clause matters because the count triggers miss it: one oversized baseline in an otherwise-small memory never trips the counts, yet it is what `/agents:doctor` fails on.) Everything in `memory/standing/` and all of MEMORY.md is read at every startup; a 40-entry standing memory costs ~60k tokens before the agent says hello. Large data files (exports, scans, dumps) never belong in `memory/standing/` — put them under `work/` or `knowledge/` and leave a one-page summary that points to them.

When you discover operational knowledge worth persisting — a tool config, a workaround, a convention the user corrects you on — write it to your own `memory/standing/`.

## Skills

Skills are atomic recipes — how to do one thing.

**Source of truth:** `.claude/skills/` at the project root. Claude Code discovers these as slash commands when working in this workspace. Codex, Gemini CLI, and OpenCode read `.agents/skills/` instead (the Agent Skills standard folder); a skill meant for every harness goes there, and OpenCode also reads `.claude/skills/`.

**Browsing index:** `agents/skills/` contains synced copies for browsing in the vault. Sync direction is `.claude/skills/` → `agents/skills/`.

**Agent-specific skills** live in `<agent>/skills/` and are only available to that agent.

Skills are referenced by playbooks but don't know about playbooks. See § Playbooks below.

## Tooling

Tool configuration is split across three levels:

1. **Shared index** (`agents/tools/INDEX.md`) — lists all available tools with credential type and status
2. **Per-tool reference** (`agents/tools/<tool>.md`) — setup, commands, scope constraints, autonomy defaults
3. **Agent overrides** (`<agent>/tools.md`) — agent-specific identity, scoped commands, tool subset

Per-tool reference files are loaded on demand, not at startup. Agents read the shared index and their own tools.md during startup; they consult the per-tool files when they need to use a specific tool.

### Autonomy defaults, admin checklist, adding a tool

Suggested autonomy levels per tool action, the post-creation admin checklist (aliases, labels, accounts), and the steps for adding a new tool are in `reference/tooling.md`. Read it when creating an agent, adding a tool, or unsure what level a tool action sits at.

## INDEX.md Maintenance

INDEX.md files carry a Current Status section that agents keep fresh at session end. Rules for what to update and when to create one are in `reference/index-maintenance.md`. Read it at session end when a project file is listed in `context.md`.

## Playbooks

Repeatable procedures that an agent executes the same way each time. A playbook codifies a task the agent has already done multiple times — it captures the trigger, the steps, the tool commands, the execution mode, and the expected output so the agent doesn't reinvent the process each session.

### Playbooks vs Skills vs Actions

| Concept | What it is | Example |
|---------|-----------|---------|
| **Skill** | Atomic recipe — how to do one thing | `/recall` searches memories |
| **Playbook** | Composed procedure — when and how to execute a multi-step task | Weekly report: gather data → generate report → publish → email |
| **Action** | Tracked work item — a specific instance of something to do | "#12: Publish W18 report" |

A playbook may invoke one or more skills. An action may reference a playbook ("run the weekly report playbook"). Skills don't know about playbooks; playbooks orchestrate skills.

### Execution Modes

Every playbook declares an execution mode that defines the human involvement pattern. Modes are ordered by decreasing agent independence.

| Mode | Label | Agent does | Human does | Use when |
|------|-------|-----------|------------|----------|
| **P1** | **Autopilot** | Executes end-to-end, reports output at session close | Reviews output (no approval gate) | Path is deterministic, low-stakes, agent has done it before |
| **P2** | **Maker-Checker** | Executes up to defined review gates, then pauses | Approves or redirects at each gate before agent continues | Output is consequential — external-facing, financial, legal |
| **P3** | **Exception-Based** | Executes independently, escalates when stuck | Unblocks on escalation, agent resumes | Path is mostly known but has foreseeable gaps |
| **P4** | **Paired** | Handles research, drafting, data work | Provides judgment, strategy, or content the agent can't | Task structurally requires both agent and human contributions |

#### Relationship to Autonomy

Autonomy levels (L1–L5) govern **individual actions** — can the agent send this email without asking? Execution modes govern **composed workflows** — does this multi-step process need human checkpoints?

An agent with L5 authority on every action in a playbook may still run it as P2 Maker-Checker because the combined output is consequential. The two systems are orthogonal:
- Autonomy doesn't override playbook execution mode
- Playbook execution mode doesn't restrict autonomy on actions outside the playbook

#### Mode Promotion

Like autonomy, execution modes can be promoted. If a P2 playbook runs successfully three times with no substantive changes at the review gate, consider promoting it to P1. Record the change in the playbook's changelog.

### Structure, format, lifecycle

Directory layout, file frontmatter, body structure, tool usage in steps, and the create → promote → retire lifecycle are in `reference/playbooks.md`. Read it when writing or editing a playbook.

**At startup:** glob `playbooks/*.md`, read only frontmatter and the first paragraph, and run the trigger check (startup steps 14–15). **At session end:** if a repeatable procedure was executed twice or more, propose codifying it as a playbook; update `Last run` on any playbook that ran.
