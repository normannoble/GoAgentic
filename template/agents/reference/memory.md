# Memory Entry Types, Consolidation and Workspace Memory

Reference for `agents/CONVENTIONS.md` (the master). Loaded on demand, not at startup.

### Memory Entry Types

Use these in the `type` frontmatter field:
- `session` — session summary (default) → `memory/sessions/`
- `decision` — key decision with rationale and context → `memory/standing/`
- `baseline` — project state snapshot → `memory/standing/`
- `stakeholder-feedback` — stakeholder positions, alignment/divergence → `memory/standing/`

### Baseline Consolidation

When the agent has accumulated more than 5 standing entries, more than 10 session entries, or any single standing file over ~15 KB, consolidate:
- Create a new baseline entry in `memory/standing/` that captures **every** durable rule, decision, path, ID, and threshold from the existing standing entries (losing a rule is the failure mode). **Keep the baseline itself under ~15 KB (≈ one page)** — it holds rules, decisions, IDs and thresholds, not raw data. If source detail (logs, inventories, scan output, long transcripts) would push it over, that detail moves to `work/` or `knowledge/` and the baseline keeps a one-line pointer to it. A standing file over 15 KB is what `/agents:doctor` fails on.
- Move the superseded standing entries to `memory/archive/standing/`
- Keep the 10 most recent session entries; move the rest to `memory/archive/sessions/`
- Write `memory/archive/INDEX.md` listing every archived file with its one-line summary
- Rewrite MEMORY.md so Standing lists the baseline (plus anything genuinely new since) and Sessions lists the kept 10, one line each, ≤40 words

Wiki-links resolve by filename, so moving files does not break `[[...]]` references.

**Why this matters:** everything in `memory/standing/` and all of MEMORY.md is read at every startup. A 40-entry standing memory costs ~60k tokens before the agent says hello. Large data files (exports, scans, dumps) never belong in `memory/standing/` — put them under `work/` or `knowledge/` and leave a one-page summary that points to them.

### Workspace Memory File Format

Workspace-level memories (shared operational knowledge, distinct from agent memories) use this frontmatter:

```yaml
---
title: <Description>
type: memory
category: operational | learning | personal
scope: workspace | project
tags: [<1-3 tags>]
created: YYYY-MM-DD
updated: YYYY-MM-DD
---
```

**Naming:** lowercase hyphenated slugs. No date prefix — updated in place.

When you discover operational knowledge worth persisting — a tool config, a workaround, a convention the user corrects you on — write it to your own `memory/standing/`.
