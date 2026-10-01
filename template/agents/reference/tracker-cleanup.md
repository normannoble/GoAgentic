# Tracker Cleanup Procedure

Reference for `agents/CONVENTIONS.md` (the master). Loaded on demand, not at startup.

#### Tracker Cleanup Procedure

When {{PRINCIPAL}} asks you to review or clean up your action tracker, follow these steps:

1. Remove any struck-through or completed items from Open — move to Completed, delete the row from Open
2. Archive completed items older than 30 days to `actions-archive.md`
3. Refresh stale due dates — anything marked "this week" that's >7 days old gets a new date or TBD
4. Cross-check shared items against other agents' trackers for status changes
5. Apply P1/P2/P3 sections if not already present
6. Normalise statuses: every Open status starts with `Not started`, `In progress`, `Waiting on <who>` or `Parked` (master § actions.md), with any detail after ` — `. Blocked / gated / awaiting → `Waiting on <who>`; deferred / on hold / dormant / backlog / watch → `Parked`; Open / To do → `Not started`. Move long status narrative into the Action cell or drop it
7. Check P1 count — if >8, something needs deprioritising
8. Scan for items that may be moot or complete but not marked — present candidates to {{PRINCIPAL}}
9. Present a summary of changes for {{PRINCIPAL}}'s confirmation before saving
