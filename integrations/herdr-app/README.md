# goagentic-tabs — Herdr app mod

The Herdr desktop app numbers new tabs (1, 2, 3 …), and only the app can
rename them. This mod names each agent's tab after the agent, `<Name> - <title>`
(`Varro - Platform Lead`), the way the terminal Herdr's tabs are named.

It reads one terminal label, `goagentic.tab`, which two things set:

- `/agents:start` (startup step 1), when the session runs in an app terminal
  (`HC_TERMINAL_ID` is set): `hc label "$HC_TERMINAL_ID" "goagentic.tab=…"`;
- `goagentic dash`, when its Start opens an agent in a new app tab.

The mod renames a tab once, and only if nobody named it. A tab you rename
yourself is left alone, and so is one you reset to its number afterwards.
It works for terminals on any machine the app is connected to.

## Install

On the Mac (or wherever the app runs):

```bash
cp -R integrations/herdr-app/goagentic-tabs ~/.config/herdr-app/mods/
herdr-app check ~/.config/herdr-app/mods/goagentic-tabs
```

Then pick one of two ways to run it.

**Always on (a personal layout).** Herdr's own layouts take no new blocks, so
copy the built-in one and add the mod as a floating block. It draws nothing:

```bash
herdr-app do layout.clone '{"name":"main","as":"mine"}'
```

In `~/.config/herdr-app/layouts/mine.json`, add to the root object:

```json
"floating": [ { "id": "goagentic-tabs", "content": { "mod": "goagentic-tabs" }, "edge": "bottom", "align": "end" } ]
```

A personal layout no longer receives Herdr's updates to the built-in one.

**On demand (built-in layout).** Run it when you want tabs named; it renames
every unnamed agent tab, shows a toast and closes:

```bash
herdr-app do mod.open '{"name":"goagentic-tabs"}'
```

or bind `mod.open {"name":"goagentic-tabs"}` to a key in `keys.json`.

---

# goagentic-home — Herdr app mod

Every workspace and agent in one view, opened on a key. Each agent is a card:
whether it is running, what it wants to do next, how many items wait on you
(amber), how many P1 items wait on someone else (orange), and anything blocked
(red). Click a running agent to go to its tab. **Start** opens an agent in a new
tab of its workspace's Space, named after it. Type to filter; arrows and Enter
act on the selected card; Esc closes.

It reads `goagentic dash --fleet --json` (CLI v0.3.13 or later) every 20
seconds while open, and starts agents with `goagentic dash --start`.

## Install

```bash
cp -R integrations/herdr-app/goagentic-home ~/.config/herdr-app/mods/
herdr-app check ~/.config/herdr-app/mods/goagentic-home
```

Bind a key in `~/.config/herdr-app/keys.json`, under `global`:

```json
"cmd-alt-j": "mod.open:goagentic-home"
```

If the workspaces live on another machine the app is connected to, say where in
`~/.config/herdr-app/goagentic.json` (every field is optional):

```json
{ "machine": "buildbox", "bin": "/home/you/.local/bin/goagentic", "root": "/home/you/agents" }
```

`machine` is the name `hc machine ls` shows. Give `bin` as a full path there:
commands the app runs on another machine do not get your login shell's PATH.
