---
description: The single next best action across the agent org — which agent to engage, on what, and why. Dependency-aware companion to /agents:status. Use /agents:next or /agents:next <scope>.
disable-model-invocation: true
allowed-tools: Read, Glob, Grep, Bash(date)
argument-hint: [scope|all]
---

# /agents:next — Next Best Action

Recommend the **single next best action** across the whole org — which agent to engage, on what, and *why* — so the principal never has to guess where to go next. `/agents:status` shows the *whole board*; **next** picks the *one move*.

## Workspace Detection

Glob for both `agents/*/context.md` (single-domain) and `agents/*/*/context.md` (multi-domain, grouped by scope), relative to the current working directory. Use whichever matches, or both if mixed. Directory names match case-insensitively.

**Retired agents.** A `context.md` whose frontmatter has `status: retired` is hidden unless `$ARGUMENTS` is `all`.

## Procedure

1. Run `date` (for staleness + recency context).
2. Glob as above. For each agent read:
   - `context.md` frontmatter → `title`, `scope`.
   - `actions.md` → `Last reviewed`, and **every open item** with its **Action text, Owner, and Status**.
3. **Classify** each open item:
   - **Actionable now** — Status starts `Not started` or `In progress` (older trackers: is *not* blocked / gated / awaiting / holding), and the Owner is the agent or the principal (not "waiting on another agent or an external event").
   - **Queued** — Status starts `Waiting on` or `Parked` (older trackers: reads blocked / gated / awaiting / holding), or the Action text says it waits on another item, an external event, or incoming evidence. Queued items are **not** candidates for "next."
4. **Score the actionable items by leverage**, reading the Action text for dependency cues — phrases like *unblocks, gates, blocks #N, head of chain, feeds, →, critical path, tracer bullet, next best action, then*. An item scores higher when it (a) sits on the **stated critical path** / is named the next step, and/or (b) **unblocks the most downstream work** (other agents' items depend on it).
5. Output a **decisive, single recommendation** — not a list:
   - **▶ Next:** *Go to **\<Agent>** — **\<action>** — because **\<why: critical path · unblocks X & Y · clears a blocker>**.*
   - **Then:** the 1–2 actions that come right after it (the chain), one line each.
   - **Correctly waiting (not your hands yet):** the top queued items + what each waits on — so the principal knows what's *deliberately* parked and doesn't chase it.
   - If two actions are genuinely co-equal, say so and give the tiebreak rather than hedging.
6. If a scope filter is given (e.g., `/agents:next Acme`), restrict to that scope.

Heading: `# Next Best Action — <today's date>` (no hardcoded project name — keep it portable).

End with one line: `Go: /agents:start <Agent> <action>`.

**Judgment note:** this ranks from tracker text. For the nuanced calls (e.g. "queue this decision behind incoming evidence"), activating the **orchestrator** agent gives richer dependency-aware reasoning than the tracker text alone encodes.
