# Delegated Tasks and Leads

Reference for `agents/CONVENTIONS.md` (the master). Loaded on demand, not at startup. Read it when you are a lead issuing work (`/agents:delegate`), when you run as a task session (`/agents:start <name> task <file>`), or when you set up a lead.

A **peer request** (`reference/peer.md`) asks another agent for a reply: it reads, drafts and answers at L3, and changes nothing. A **delegated task** asks another agent to **do work**: build, write, fix, commit. The agent that issues it is a **lead**. It owns an outcome {{PRINCIPAL}} approved, directs its members to deliver it, answers their questions itself, and brings {{PRINCIPAL}} only what it cannot answer or may not approve.

Everything here is a no-op outside Herdr (`HERDR_ENV` unset), like peer requests.

## 1. Leads and members

- A member names its lead in its `context.md` frontmatter: `lead: <Name>`. No `lead:` key → the agent has no lead, and nothing in this file applies to it.
- A lead directs only its own members: `/agents:delegate` refuses any target whose `lead:` is not the caller. No one-off exceptions.
- The lead's `role.md` lists its members and the outcomes it owns. Members still own their work, files, tracker and memory. The lead owns the outcome and the plan.
- A lead is an agent like any other: {{PRINCIPAL}} talks to it in a normal human session, and it issues work from there.
- A member may still run `/agents:ask` inside a task (a builder asking a reviewer for a review). A member never delegates, and a lead never delegates to another lead's member.

## 2. Outcomes are the boundary

Before a lead issues work, {{PRINCIPAL}} approves the **outcome**: goal, acceptance criteria, scope, and which member does which part. The lead records it in an **outcome file** and every work order names that file in `outcome:`. The approved outcome is the limit of the lead's authority: anything outside it goes back to {{PRINCIPAL}} as a scope change.

**The outcome file holds the state, not the lead's conversation.** `agents/<Lead>/outcomes/<slug>.md`, one per outcome, with a tracker row pointing to it:

```markdown
---
outcome: <one line>
approved: YYYY-MM-DD by {{PRINCIPAL}}
status: active | done | dropped
---

## Definition
<goal, acceptance criteria, scope in/out, which member does which part>

## Plan
<the steps and the work order each became, with its path and status>

## Decisions
<dated: {{PRINCIPAL}}'s answers (quoted), the lead's answers that set direction, settled peer replies>

## Next
<the one next step, so a fresh session can pick up>
```

Update it as things happen (a work order issued, accepted or stopped; a decision; an answer from {{PRINCIPAL}}), not at the end. A lead whose state lives in this file can wrap at any point and pick up in a fresh session. That is the point: a lead's conversation fills fast (every question, answer and review lands in it), so it should wrap between phases the way any session does, and never carry an outcome in its head.

## 3. Authority in a task

The member works under **its own `autonomy.md`**, with one substitution: where its ladder says {{PRINCIPAL}}, read "the lead, within the work order's outcome".

**The lead may:**
- answer questions of fact, product, design and sequencing;
- approve the member's L3 intents and L2 recommendations that sit inside the approved outcome.

**Always {{PRINCIPAL}}, whatever the work order says:**
- every item at the member's L1;
- anything external or irreversible: production changes, sending to people outside the workspace, spend, legal commitments, deleting data;
- any gate in the member's `autonomy.md` changelog or standing memory that names {{PRINCIPAL}} explicitly ("needs {{PRINCIPAL}}'s go", "{{PRINCIPAL}} approves every live change");
- any change to any agent's autonomy.

**No laundering.** The lead cannot approve more than {{PRINCIPAL}} delegated to the lead: its own `autonomy.md` caps what it passes down. If the lead's ladder puts an action at L2, the lead cannot approve that action for a member.

The member decides which bucket a question falls in from its own ladder and marks {{PRINCIPAL}}-only questions as such (§ 5). The lead may move a question *up* to {{PRINCIPAL}}, never down.

## 4. The work order

One file per task, in the **target's** `peer/` folder, next to peer requests: `agents/<Target>/peer/YYYY-MM-DD-HHMM-task-from-<Lead>.md`.

```markdown
---
from: <Lead>
to: <Target>
type: task
date: YYYY-MM-DD HH:MM
status: open
outcome: <the approved outcome, one line, and where it is recorded>
lead_pane: <the lead's HERDR_PANE_ID>
task_pane: <the task pane id>
rounds: 0
timebox: 4h
due: YYYY-MM-DD HH:MM
---

## Work order

<what to deliver; acceptance criteria; every path, ticket, branch and ID the target needs; what is out of scope; who else is involved>

## Log

<the target appends progress, questions and the hand-back; the lead appends answers>
```

`rounds` counts the lead's answers and rejected hand-backs (§ 6). `timebox` is how long the task may run, from `task-timebox` in `agents/CONVENTIONS.md` frontmatter (default `4h`); a longer one only when {{PRINCIPAL}} set it in the outcome. `due` is `date` plus `timebox`, written once so nobody has to work it out again. Waiting on {{PRINCIPAL}} counts: the clock does not pause.

The work order must be self-contained, like a peer request: the target has its own memory, not the lead's conversation.

**Size: one session's work.** A member loads its full context, does the work and wraps, all in one session, and should finish with plenty of room: aim for a task the member can wrap well under half its context window. A bigger outcome becomes several work orders, issued one after another (each builds on the last hand-back) or to different members. When in doubt, split: two small hand-backs cost less than one session that runs out of room before its wrap.

**Statuses.**

| Status | Set by | Meaning |
|--------|--------|---------|
| `open` | lead | Written, task not started |
| `working` | target | The target is on it |
| `question` | target | The target stopped for an answer (see the Log) |
| `review` | target | Delivered; waiting for the lead to accept |
| `done` | lead | Accepted. Nothing more is owed |
| `needs-principal` | lead | Waiting on {{PRINCIPAL}} (a question only {{PRINCIPAL}} can answer, or a stop the lead cannot resolve) |
| `stopped` | lead | Called off by the lead or {{PRINCIPAL}}; the Log says why |

`done` and `stopped` files archive like peer files (`peer/archive/`, never deleted). The work orders are the record of who directed what.

**The Log.** Entries are short and dated:

```markdown
- 10:42 Target — branch export-csv, endpoint done, tests next
- 11:05 Target — Q1: CSV or XLSX for the first cut? (outcome says "export")
- 11:09 Lead — A1: CSV only; XLSX is out of scope (outcome, scope line 2)
- 11:31 Target — Q2 (needs {{PRINCIPAL}}): merge is security-sensitive (my ladder); need {{PRINCIPAL}}'s go
- 11:40 Lead — A2: {{PRINCIPAL}}: yes, merge it (asked 11:35)
- 12:20 Target — Hand-back: PR #214 merged after review APPROVE; acceptance 1–3 met; commit 3f2c1aa
```

## 5. The task session (the target)

Started by the lead as `/agents:start <target> task <file>` in a fresh pane. The router loads the full startup (steps 1–13: including the two most recent sessions, since this is real work) plus the newest file in `memory/tasks/`, skips the priority declaration, the playbook trigger check and the hygiene line, then hands over here.

1. Read the work order. Set `status: working` and log a one-line plan.

   **Every time you start a turn** (the first one, and each time the lead prompts you to continue), run `date` and compare it with `due:`. Past due: stop work, log `Timebox reached at HH:MM: <what is done, what is not>`, set `status: question`, and end your turn. Do not start new work past due, even to finish.
2. Do the work under your own ladder (§ 3). Use your own worktree for code (master § Code Repositories). Keep your own tracker row for the work: add one if none exists, with the work order's path in the Action cell.
   In T3 Code, "end your turn" is literal: ending it is what wakes the lead. Never hold a turn open with a sleep loop, whether waiting for the lead or for a deploy or CI run; log what you are waiting on and end the turn (`reference/t3.md` § Waiting).
3. **When you need an answer:** write `Qn: …` in the Log (`Qn (needs {{PRINCIPAL}}): …` when § 3 says only {{PRINCIPAL}} can give it), set `status: question`, and **end your turn**. The lead answers in the Log and prompts you to continue. Never guess past an approval gate to keep moving.
4. **When you are done:** run the Session End Protocol (`reference/session-end.md`) Steps 1–7 with these differences: the memory goes to `memory/tasks/YYYY-MM-DD-<topic>.md` (not `memory/sessions/`) with `type: task` and a `task: <work order path>` line in its frontmatter, and is not added to MEMORY.md; if `memory/tasks/` then holds more than 10 files, move the oldest to `memory/archive/tasks/`; Step 6 (project files) only for files the work order names; Step 7 stages your own paths, which include this work order (it lives in your `peer/` folder). Then write the hand-back in the Log (what was delivered, each acceptance criterion met or not, commit/PR ids) and set `status: review`. End your turn.
5. If you cannot continue (a blocker no answer fixes, or the work turns out to be outside the outcome), log it as a question; the lead decides whether to stop.
6. **If the work is bigger than one session** (your context is filling, or you can see the rest will not fit), stop at a clean point: commit what works, run step 4 as a **partial hand-back** that lists what is done and what is left, and set `status: review`. The lead accepts the part and issues the rest as a new work order, which starts a fresh session. Never push on until you cannot wrap properly: the wrap is what keeps your memory useful.
7. **Rework.** If the lead sends the hand-back back (`An: not yet: …`), fix it and hand back again; at that second step 4, **update the task log you wrote at the first hand-back** (same file: add what changed and why) rather than writing a second one, and commit again.

A task session never sends anything outside the workspace that its ladder does not already allow, never edits another agent's files, and never delegates.

## 6. The lead's side

### Issuing: `/agents:delegate <name> "<work order>"`

The skill (`skills/delegate/SKILL.md`) checks, in order, and refuses with one line if any fails:
1. Inside Herdr, and the caller is an active agent (a lead), not the principal alone.
2. The target is active, in this workspace root, and its `context.md` has `lead: <caller>`.
3. The target is **not live anywhere**: no `herdr agent list` entry under this root named `<target>`, `<target>-peer` or `<target>-task`. Two sessions on one tracker is the failure this prevents. If the principal has it open, ask them to wrap it first; never issue beside it and never close it yourself.
4. The lead has fewer than `task-limit` tasks in flight (workspace frontmatter, default 3): work orders `from: <caller>` anywhere in the workspace with status `open`, `working`, `question` or `review`.
5. The outcome is approved (§ 2). If the lead cannot point to where {{PRINCIPAL}} approved it, ask {{PRINCIPAL}} first.

All checks run before anything is written: a refused delegation leaves no file and no pane. Then it writes the work order, opens a fresh pane (`<Target> - Task`, in the lead's tab), starts the target as `<target>-task`, prompts `<start> <target> task <file>` **without waiting**, sets `task_pane:`, and returns.

### Watching

In T3 Code, read every Herdr command in this section through the map in `reference/t3.md`: the background `--wait` on `thread start` or `thread send` stands in for `herdr agent wait`, `thread show` for `herdr agent read`, and settling the thread (`t3_thread_organize`, `action: settle`) for `herdr pane close`.

After the start prompt, wait for the pane to reach `working` (`herdr agent wait <target>-task --until working --timeout 60000`): a fresh pane starts idle. Then run `herdr agent wait <target>-task --until idle --until done --until blocked` in the background (Claude Code: Bash with `run_in_background`), so you are woken when the task pane stops. Then read the work order:

- `question` → answer (below).
- `review` → check the hand-back against the acceptance criteria. Accept: set `status: done`, close the pane (`herdr pane close <task_pane>`). Not accepted: log what is missing as an answer (`An: not yet: …`), **increment `rounds`**, set `status: working`, prompt the pane to continue. A rejected hand-back is a round like any answer.
- `working` but the pane is idle → the target ended its turn without a status change; read the pane (`herdr agent read <target>-task --source recent-unwrapped --lines 60`) and prompt it to update the work order.
- **Past `due`**, whatever the status (check with `date` on every wake, and at your startup): stop the task (below) and bring it to {{PRINCIPAL}} with what was done, what was not, and your recommendation (extend, re-scope, or drop). Only {{PRINCIPAL}} extends a timebox: write the new `due:` and log `An: {{PRINCIPAL}}: extended to HH:MM`. You never extend it yourself.
- `blocked` → the task pane hit a permission prompt. Never answer another agent's approval prompt: tell {{PRINCIPAL}} which pane and what it asks.

### Answering

- **You may answer it (§ 3):** check the outcome, the members' files (read only), the wiki or ticket system, or `/agents:ask` another member for a fact. Write `An: …` in the Log, increment `rounds`, set `status: working`, and prompt the pane: `herdr agent prompt <target>-task "Answered in <file>, continue."`
- **{{PRINCIPAL}} already answered it** in your session, in words that name the thing asked ("also add the release note"): quote those words verbatim as `An: {{PRINCIPAL}} (quoted, HH:MM): "…"`. That is relaying, not approving. A general instruction ("get it done") is not an answer to a gate.
- **Only {{PRINCIPAL}} can answer it,** or you are unsure: set `status: needs-principal` and put it to {{PRINCIPAL}} in your session. Batch questions across tasks; give each its options and your recommendation. Write the answer into the Log as `An: {{PRINCIPAL}}: …` and into the outcome file's Decisions, then continue as above.
- **Round limit:** when `rounds` reaches 5 (answers and rejected hand-backs together), stop answering and bring the task to {{PRINCIPAL}}: the work order is probably wrong. Only {{PRINCIPAL}} raises it for a task (`An: {{PRINCIPAL}}: 3 more rounds`).

Never edit the target's files, worktree or tracker yourself. You direct through the work order.

### Stopping

The lead (or {{PRINCIPAL}}) can stop a task: log why, set `status: stopped`, prompt the target to wrap (it runs § 5 step 4 with what it has, and leaves `stopped`), then close the pane.

### At the lead's session end

Bring each active outcome file up to date (Plan statuses, Decisions, Next), then stage the work orders you wrote or answered this session (they sit in your members' `peer/` folders) along with your own paths: `git add agents/<Lead>/ agents/<Target>/peer/<file> …`. The target commits its own copy at its hand-back; whichever commit is later carries the final state.

**Wrap mid-outcome freely.** Members keep working in their panes after you wrap; whatever needs you (a question, a hand-back, an overdue task) waits in its work order until your next session picks it up. Wrap between phases, and whenever your session is getting long, as any session would.

### At the lead's startup

In the peer glance (startup step 17), also list work orders `from: <you>` anywhere in this workspace that are not `done` or `stopped`, with their status: they are the lead's work in flight. Read the outcome file of each `active` outcome (its Next line first): that, not the last session's conversation, is where the outcome stands. Re-arm a background wait for each task pane still live.

## 7. When the principal opens a member directly

If {{PRINCIPAL}} starts a member while `<member>-task` is live, the router warns in one line (the member has a task running in another pane). Two sessions on one tracker fight; the principal decides whether to wait or have the lead stop the task.

## 8. Setting up a lead

1. Create the lead with `/agents:new`. Its role names its members and outcomes; its autonomy ladder carries "issue a work order inside an approved outcome" (suggested L3) and "approve a member's L2 item inside an approved outcome" (suggested L3), plus the § 3 always-the-principal list at L1.
2. Add `lead: <Lead>` to each member's `context.md` frontmatter, and one line to each member's `role.md` reporting section: "Takes work orders from <Lead> for outcomes {{PRINCIPAL}} approved; brings questions to <Lead> first; {{PRINCIPAL}} keeps final authority."
3. Optional: `task-limit: <n>` and `task-timebox: <duration>` in `agents/CONVENTIONS.md` frontmatter.
4. The lead keeps its outcome files in `agents/<Lead>/outcomes/` (created with the first outcome).
