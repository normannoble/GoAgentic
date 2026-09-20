---
description: Create a new agent following workspace conventions. Interactive process that defines role, soul, and structure. Use when setting up a new AI collaboration partner for a project or initiative.
disable-model-invocation: true
allowed-tools: Read, Write, Edit, Glob, Grep, Bash(mkdir), Bash(ls), Bash(date), AskUserQuestion
argument-hint: "[scope — e.g., Acme] (optional in single-domain workspaces)"
---

# /agents:new — Agent Builder

Build a new agent following the conventions in `agents/CONVENTIONS.md`.

## Startup

1. Read `agents/CONVENTIONS.md` to understand the template. If its frontmatter has `extends: <path>`, read that master first (if the value is `plugin`, resolve it with `ls "${CLAUDE_PLUGIN_ROOT}/template/agents/CONVENTIONS.md"`, or glob `~/.claude/plugins/cache/*/agents/*/template/agents/CONVENTIONS.md` if the variable is empty); the workspace file wins on conflict
2. Read the reserved names list from CONVENTIONS.md to avoid conflicts
3. **Detect workspace layout:**
   - Glob for `agents/*/context.md` (single-domain: agents are direct children of `agents/`)
   - Glob for `agents/*/*/context.md` (multi-domain: agents are grouped by scope)
   - If only single-domain agents exist (or `agents/` is empty with no scope subdirectories), this is a **single-domain workspace** — skip scope question, use `agents/<name>/` layout
   - If multi-domain agents exist, this is a **multi-domain workspace** — ask for scope, use `agents/<scope>/<name>/` layout
4. If multi-domain and `$ARGUMENTS` contains a scope, note it. Otherwise, ask.

## Process

Work through these phases interactively. Each phase ends with confirmation before moving to the next. Do not rush — this is a design conversation, not a form to fill in.

### Phase 1: Scope & Project

**Multi-domain workspaces only:** If scope wasn't provided in arguments:
- Ask: "Which scope does this agent belong to?" (e.g., Acme, Personal)

**Single-domain workspaces:** Read the workspace root `CLAUDE.md` to infer the scope from the workspace context. No need to ask.

Then:
- Ask: "What project or initiative is this agent scoped to? Give me the short version — what's the goal and why does it need a dedicated agent?"
- Read the project's existing files (CLAUDE.md, INDEX.md) if they exist, to understand context.

### Phase 2: Role Definition

This is the foundation. Get it right before moving on.

Ask the principal to describe:
- **What does this agent do?** What's its primary job?
- **Who does it report to and who decides?** (Usually the principal (named in `agents/CONVENTIONS.md` frontmatter `principal:` if present), but confirm)
- **What outcomes is it accountable for?** What should measurably improve because this agent exists?
- **Who are the key stakeholders?** People the agent needs to track, prepare for, or synthesize input from.
- **What deliverables does it own?** Concrete artifacts it keeps progressing.
- **What's in scope and out of scope?** Where are the boundaries?

After gathering these inputs, draft a complete `role.md` following the structure in CONVENTIONS.md:
- Role Purpose (one paragraph)
- Reporting
- Project Scope
- Outcomes (numbered)
- Key Stakeholders (table)
- Deliverables Owned (numbered list)
- Working Mode (behavioral directives appropriate to this role)
- Scope Boundaries (IN/OUT)
- Primary Objective (one sentence)
- Agent Convention reference

Present the draft. Iterate until approved.

### Phase 3: Autonomy

Based on the approved role, draft an `autonomy.md` with initial authority levels. Use these guidelines for seeding:

- **L5 (Own):** Session housekeeping — update trackers, memory, project logs
- **L4 (Act & Inform):** Administrative actions within the agent's domain — grooming, status updates, minor reorganisation
- **L3 (Intend):** Core deliverable work — drafting, synthesizing, preparing materials the role is accountable for
- **L2 (Recommend):** Decisions that affect direction — priorities, scope changes, new initiatives
- **L1 (Flag):** Strategic decisions, stakeholder relationships, anything out of scope or requiring the principal's authority

Be specific — list concrete action types, not vague categories. The agent will reference this file every session to decide how to behave.

Present the draft. Iterate until approved.

### Phase 4: Soul

Based on the approved role, draft a `soul.md`. Consider:
- What communication style fits this role? A compliance auditor sounds different from a creative strategist.
- What temperament serves the outcomes? An action tracker should be impatient with ambiguity. A research synthesizer should be patient and thorough.
- What should the agent value? Precision? Speed? Thoroughness? Brevity?
- What should it NOT do? Pad output? Speculate? People-please?

Write in third person. Keep it under 15 lines. No overlap with working rules in role.md — soul is how the agent *feels* to interact with, not what it does.

Present the draft. Iterate until approved.

### Phase 5: Name

Based on the approved role and soul, suggest 3 names from the naming pool defined in `agents/CONVENTIONS.md`. For each, give:
- The name (and literal meaning if it translates)
- The historical or mythological figure (2-4 sentences — who they were, what they did, what they're remembered for)
- Why it fits this agent's character (1-2 sentences)

Avoid names already reserved in CONVENTIONS.md. Avoid names that signal the agent's domain — a legal agent named "Ulpian" collapses the neutrality the convention exists to preserve.

Let the principal pick or propose an alternative. Once a name is chosen, the detail written in this phase is the source material for `name.md` in Phase 6 — do not throw it away.

### Phase 6: Create

Once name, role, soul, and autonomy are confirmed, create the full agent structure:

1. **Directory structure:**

   Multi-domain workspace:
   ```
   agents/<scope>/<name>/
   ```

   Single-domain workspace:
   ```
   agents/<name>/
   ```

   Contents (same for both layouts):
   ```
   ├── role.md
   ├── soul.md
   ├── name.md
   ├── autonomy.md
   ├── tools.md
   ├── actions.md
   ├── actions-archive.md
   ├── context.md
   ├── MEMORY.md
   ├── memory/
   │   ├── standing/
   │   └── sessions/
   ├── peer/            # created on demand by /agents:ask; may be omitted here
   └── playbooks/
   ```

2. **name.md** — written from Phase 5 material:
   ```markdown
   # <Name> — Name

   ## The Name

   **<Name>** — <literal meaning if it translates; otherwise note it was a cognomen/epithet>.

   ## The Historical Figure

   <2-4 sentences on who they were, what they did, and what they're remembered for.>

   ## Why It Fits

   <1-2 sentences linking the archetype to this agent's role and soul.>
   ```

3. **actions.md** — empty template:
   ```markdown
   # Actions

   Standing action tracker for <Name>. Updated every session. Source of truth for what's open, who owns it, and what's at risk.

   Last reviewed: <today's date>

   ## Open

   ### P1
   | # | Action | Ticket | Owner | Priority | Due/Target | Status | Since |
   |---|--------|--------|-------|----------|------------|--------|-------|

   ### P2
   | # | Action | Ticket | Owner | Priority | Due/Target | Status | Since |
   |---|--------|--------|-------|----------|------------|--------|-------|

   ### P3
   | # | Action | Ticket | Owner | Priority | Due/Target | Status | Since |
   |---|--------|--------|-------|----------|------------|--------|-------|

   ## Completed

   | # | Action | Ticket | Owner | Completed |
   |---|--------|--------|-------|-----------|
   ```

4. **actions-archive.md** — empty archive template:
   ```markdown
   # Actions — Archive

   Completed actions older than 30 days, moved from `actions.md` to keep context lean. Same format as the Completed table.

   ## Completed (Archived)

   | # | Action | Ticket | Owner | Completed |
   |---|--------|--------|-------|-----------|
   ```

5. **MEMORY.md** — empty template:
   ```markdown
   # <Name> — Memory

   Working memory for <Name>. Organised into standing knowledge (durable across sessions) and session logs (per-engagement records).

   ## Standing

   Durable rules, decisions, and baselines. All read on startup.

   ## Sessions

   Per-session logs. Most recent 2 read on startup.
   ```

6. **context.md** — agent-specific startup context:
   ```markdown
   ---
   scope: <scope or workspace name>
   title: <role title>
   ---

   # Context

   ## Startup Context

   Additional files to read on startup, relative to workspace root.

   - <project-specific paths, e.g., work/projects/foo/INDEX.md>

   ## Project Files

   Files to check for updates during Session End Protocol.

   - <project-specific paths>
   ```

7. **tools.md** — agent tooling configuration. Read `agents/tools/INDEX.md` for the shared tool index and `agents/tools/` for per-tool reference files. Then:
   - Glob for an existing agent's `tools.md` to use as a template (e.g., `agents/*/tools.md` or `agents/*/*/tools.md`). If one exists, read it and substitute the new agent's name and title.
   - If no existing `tools.md` exists in the workspace, create one referencing the shared tool files in `agents/tools/` — include the agent's identity (email alias, display name) and any tool-specific overrides.

   **Important:** After creating the agent, remind the principal to complete the Post-Creation Admin Checklist in `agents/CONVENTIONS.md` (aliases, labels, permissions, etc.).

8. **Update the workspace root instruction file** (single-domain) or the scope's (multi-domain) — `CLAUDE.md`, plus `AGENTS.md` or `GEMINI.md` if the workspace has one with an `## Agents` table — add the agent to the Agents table (name, role title)

9. **Update `agents/CONVENTIONS.md`** — add the name to the `reserved:` frontmatter list (shared/extends mode) or to the **Reserved** line (copied mode)

10. **Create initial baseline** at `<agent-dir>/memory/standing/<date>-baseline.md`:
   - Capture the current state of the project this agent is scoped to
   - Note what exists, what's in progress, what's pending
   - Keep it under ~15 KB (one page): state, rules, IDs and pointers — not raw data dumps. Bulk detail belongs under `work/` or `knowledge/`, with the baseline pointing to it. (This is the `/agents:doctor` standing-file cap.)

Note: Individual per-agent skill files are **not** created. All agents are invoked through the `/agents:start` router (e.g., `/agents:start <name>`).

### Phase 7: Verify

After creation:
- List the created files
- Show the invocation command (`/agents:start <name>`)
- Remind the principal to test with `/agents:start <name>` in a new conversation

## Rules

- Do NOT skip phases or combine them. Each phase is a conversation.
- Do NOT create files until Phase 6. Phases 1-5 are pure discussion.
- If the principal changes their mind about something from an earlier phase, update the affected drafts before proceeding.
- The agent being created must follow ALL conventions in `agents/CONVENTIONS.md`.
