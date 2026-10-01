---
description: Set up the current repository as an agent workspace — creates agents/CONVENTIONS.md (extends the plugin master), the shared tools index, the scheduler folder and task register, and optional workspace folders. Run once per repo, before /agents:new. Safe to re-run: it only adds what is missing.
disable-model-invocation: true
allowed-tools: Read, Write, Edit, Glob, Bash(ls), Bash(mkdir), Bash(cat), Bash(cp), Bash(chmod), Bash(sed), Bash(git), AskUserQuestion
argument-hint: [--principal <name>] [--naming roman|norse|hellenic] [--workspace | --no-workspace] [--no-scheduler]
---

# /agents:init — Workspace Setup

You set up the current working directory as an agent workspace for the `agents` plugin. You create only what is missing. You never overwrite a file that exists.

## 1. Look before writing

Run `ls -a` and glob for these. Note which already exist:

- `agents/CONVENTIONS.md`
- `agents/tools/INDEX.md`
- `agents/scheduler/` and `agents/scheduled-tasks.md`
- `CONVENTIONS.md`, `PHILOSOPHY.md`, `CLAUDE.md`
- `thinking/`, `work/projects/`, `work/operations/`, `knowledge/`, `outputs/`

If `agents/CONVENTIONS.md` already exists and has `extends:` in its frontmatter, the workspace is set up. Say so, then continue with **backfill only**: skip the questions in step 2 (take `principal` from the frontmatter), and create only the pieces in step 3 that are missing (typically the scheduler folder and register). Do not rewrite existing files.

Resolve the plugin's template folder: `ls "${CLAUDE_PLUGIN_ROOT}/template"`. If the variable is empty, use `$AGENT_FRAMEWORK_ROOT/template` or the framework root a wrapper skill named (another harness); else glob `~/.claude/plugins/cache/*/agents/*/template` and take the highest version. Call this `TEMPLATE`.

Note the harness you are running in: `claude` (Claude Code, the default), `codex`, `gemini`, or `opencode`. It decides two things below: the `harness:` frontmatter value and which instruction file gets the `## Agents` block (`CLAUDE.md`, `AGENTS.md` for codex and opencode, `GEMINI.md` for gemini).

## 2. Ask three things (skip any given as arguments)

Use one AskUserQuestion call with the questions that are still open. If all three were given as arguments, ask nothing.

1. **Principal** — the person who directs the agents. Default: `git config user.name`, else "the principal".
2. **Naming tradition** — one of:
   - **Roman cognomina** (default): historical Roman names that are dignified, neutral, and large enough as a pool to scale. Pool: Cato, Varro, Seneca, Corvus, Regulus, Cassia, Livia, Marius, Titus, Praxis, Lucian, Nerva, Flavia, Sabina, Quintus, Aulus, Gaius, Tertia, Decima, Balbus
   - **Norse saga names**: names from Norse mythology and saga literature — strong, evocative, and drawn from a deep cultural well. Pool: Sigrid, Bjorn, Freya, Leif, Astrid, Gunnar, Ingrid, Ragna, Eirik, Sif, Tyr, Vidar, Brynhild, Ivar, Solveig, Arne, Dagny, Halvard, Rune, Thyra
   - **Hellenic names**: names from ancient Greek history and philosophy — associated with wisdom, governance, and systematic thought. Pool: Solon, Thales, Hypatia, Aspasia, Pericles, Zeno, Lycurgus, Diotima, Arete, Philo, Cleisthenes, Myia, Timaeus, Aristos, Charis, Hector, Melos, Doris, Xanthippe, Archon
3. **Workspace folders** — create the standard layout (`thinking/`, `work/projects/`, `work/operations/`, `knowledge/`, `outputs/`) plus root `CONVENTIONS.md` and `PHILOSOPHY.md`? Default yes. `--workspace` answers yes, `--no-workspace` answers no.

## 3. Create the files

**`agents/CONVENTIONS.md`** (always, if missing):

```markdown
---
extends: plugin
principal: <principal>
naming: <tradition name>
naming-description: <description from the chosen tradition>
naming-examples: <pool>
reserved: []
ticket-column: Ticket
inbound: none
scheduler: none   # launchd | systemd | cron once /agents:schedule install has run
harness: <claude | codex | gemini | opencode — the CLI that runs ticks and peer panes; omit the line for claude>
---

# Agent Conventions — <repo folder name>

This file extends the master that ships with the `agents` plugin. Only differences from the master live here. On conflict, this file wins.

No workspace-specific overrides yet. The master applies in full.
```

**`agents/tools/INDEX.md`** (if missing): copy `TEMPLATE/agents/tools/INDEX.md`, replacing `{{PRINCIPAL}}` with the principal.

**Scheduler** (unless `--no-scheduler`; skip any part that exists):
- `mkdir -p agents/scheduler/logs` and copy `TEMPLATE/agents/scheduler/{prompt.md,policies.md,tick.sh,gate.py,install.sh,install-launchd.sh,install-systemd.sh,setup.sh}` into `agents/scheduler/`. Replace `{{PRINCIPAL}}` in the copies with the principal. `chmod +x` every `.sh` file.
- Copy `TEMPLATE/agents/scheduler/scheduled-tasks.md` to `agents/scheduled-tasks.md`, replacing `{{PRINCIPAL}}` and `YYYY-MM-DD` with today's date. The register ships with one disabled `example-task`.
- Add `agents/scheduler/logs/` to `.gitignore` if not already ignored.
- Do **not** install the launchd/systemd/cron job here. Say: `Timer not installed. When you add a real task, run /agents:schedule install.`

**Workspace layout** (if chosen): `mkdir -p thinking work/projects work/operations knowledge/systems knowledge/people knowledge/processes knowledge/company outputs`. Copy `TEMPLATE/CONVENTIONS.md` to `CONVENTIONS.md` and `TEMPLATE/../PHILOSOPHY.md` to `PHILOSOPHY.md` only if each is missing. Put an empty `.gitkeep` in each new empty folder.

**Instruction file** (`CLAUDE.md` in Claude Code; `AGENTS.md` in Codex and OpenCode; `GEMINI.md` in Gemini CLI): if it exists and has no `## Agents` heading, append this block. If it does not exist, create it with just this block. On a non-Claude harness spell the commands the way that harness does (`$agents-start` in Codex, `/agents-start` in OpenCode).

```markdown
## Agents

Persistent AI collaborators with calibrated autonomy. See `agents/CONVENTIONS.md` (extends the `agents` plugin master).

| Name | Role |
|------|------|
| *(run `/agents:new` to add the first one)* | |

Start one with `/agents:start <name>`. List with `/agents:list`. Board: `/agents:status`. One move: `/agents:next`. Scheduled tasks: `/agents:schedule`.
```

## 4. Confirm

Show a short table of what was created and what was skipped because it existed. Then say:

```
Next: /agents:new        — design and create your first agent
Then: /agents:start <name>
Later: /agents:schedule  — add an unattended task (then `install` to start the timer)
Guide: /agents:help
```

Do not create any agent here. Do not commit.
