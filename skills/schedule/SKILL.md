---
description: Manage the workspace's unattended scheduled tasks — list, add, remove, enable, disable a task in agents/scheduled-tasks.md; check the timer and last runs with status; install the hourly launchd/systemd/cron timer. Use /agents:schedule, /agents:schedule add, /agents:schedule status.
disable-model-invocation: true
allowed-tools: Read, Write, Edit, Glob, Grep, Bash(date), Bash(ls), Bash(tail), Bash(launchctl list), Bash(systemctl --user list-timers *), Bash(crontab -l), Bash(bash agents/scheduler/tick.sh --dry-run), Bash(bash agents/scheduler/install.sh), Bash(bash agents/scheduler/install.sh --which), Bash(uname), AskUserQuestion
argument-hint: [list | add | remove <id> | enable <id> | disable <id> | status | install]
---

# /agents:schedule — Scheduled Tasks

You manage the task register at `agents/scheduled-tasks.md` and the timer that runs it. You never run a task yourself and never change anything but the register and the timer.

## Setup check

Glob for `agents/scheduled-tasks.md` and `agents/scheduler/tick.sh`. If either is missing, say `Scheduler not set up. Run /agents:init (it backfills the scheduler folder).` and stop.

Read the register. Each task is a `### <id>` block with these fields, one per line: `Schedule` (cron in backticks + human note), `Agent`, `Execution` (`direct` | `agent`), `Autonomy ceiling` (L3–L5), `Task`, `Enabled`, `Last run`, `Created`, and optional `Note`/`Updated` lines.

Route on the first word of `$ARGUMENTS` (empty = `list`).

## list

Table, one row per task: `| Id | Agent | Schedule | Mode | Ceiling | Enabled | Last run |`. Below it one line: `Timer: <installed as <label> | not installed>` (by `bash agents/scheduler/install.sh --which`: launchd → `launchctl list | grep agent-scheduler`; systemd → `systemctl --user list-timers *agent-scheduler*`; cron → `crontab -l | grep agent-scheduler`). End with `Add: /agents:schedule add · Status: /agents:schedule status`.

## add

Ask, in one AskUserQuestion call, for what is not obvious from `$ARGUMENTS`:
1. **Agent** — must be an existing active agent (glob `agents/*/context.md`).
2. **When** — plain words ("every Friday 10:00", "1st of the month 09:00"). Convert to a 5-field cron. Confirm the conversion back in words.
3. **Mode** — `direct` (mechanical, no judgment, reads only named files) or `agent` (loads the agent's trimmed tick context).
4. **What** — the task text. Push for precision: every file to read, every channel or record ID, and what must NOT be touched. Ticks cannot ask questions.

Then set the ceiling: `direct` → L4; `agent` → L3 unless the principal asks for L4. Never L2/L1 (the scheduler skips those).

Read `agents/scheduler/policies.md` and check the task against it (no email sends, no writes to shared records without an explicit ID, no messages outside the named channels). Say if something in the task text conflicts and get a fix before writing.

Append the block to `## Tasks` with `Enabled: true`, `Last run: never`, `Created: <today>`. Id is a lowercase-hyphen slug. Remove the shipped `example-task` block if it is still there and still disabled.

If the agent has no `memory/scheduled/inbox.md`, create it with a one-line header (`# Scheduled-run inbox — <Agent>`) so ticks have somewhere to write.

Finish: show the block, then `Timer: <installed | not installed — run /agents:schedule install>`. Say that the register change is uncommitted and the next interactive session with the agent commits it.

## remove <id> / enable <id> / disable <id>

Find the block. `remove` deletes it (show it first, one confirmation). `enable`/`disable` flip the `Enabled` field. Say what changed and that it is uncommitted.

## status

Show:
- Timer: label and whether loaded (`launchctl list` / `systemctl --user list-timers` / `crontab -l`, by `install.sh --which`).
- `bash agents/scheduler/tick.sh --dry-run` → what is due right now.
- Last 5 gate lines from `agents/scheduler/logs/cron.log` (`tail`).
- The newest `## ` entry in the newest `agents/scheduler/logs/YYYY-MM.md`.
- For each agent with `memory/scheduled/inbox.md`: count of `UNPROCESSED` entries. If any, say `Drain: /agents:start <Agent>`.

## install

Run `bash agents/scheduler/install.sh`. It picks the backend for this machine: launchd on macOS, a systemd user timer on Linux with systemd, cron otherwise (`AGENT_SCHEDULER_BACKEND` overrides). Show the script's output, including any lingering warning. Then set `scheduler:` to the backend `bash agents/scheduler/install.sh --which` prints (`launchd`, `systemd` or `cron`) in the frontmatter of `agents/CONVENTIONS.md`. Say: `Timer fires at :07 every hour. Claude runs only when a task is due. Check: /agents:schedule status`.

## Rules

- Do not commit.
- Do not edit `agents/scheduler/prompt.md` or `policies.md`; those come from the plugin.
- Do not run a task by hand. To test one, tell the principal to lower nothing and use `--dry-run` plus wait for the timer, or run `bash agents/scheduler/tick.sh` themselves.
