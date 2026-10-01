# goagentic.dash — Herdr plugin

Shows what every agent in the current workspace wants to do next, what it did
last, when it last ran, and whether its files are healthy, with no Claude
session running. It is a thin wrapper around `goagentic dash`; everything it
shows comes from the agent files.

## Install

One line, on each machine that runs Herdr (macOS or Linux, no Go needed):

```bash
curl -fsSL https://raw.githubusercontent.com/normannoble/GoAgentic/main/install.sh | sh -s -- --dash
```

It downloads the checksum-verified `goagentic` release into `~/.local/bin`
(override with `GOAGENTIC_INSTALL_DIR`), then runs `goagentic herdr install`,
which:

- checks Herdr is 0.9.0 or newer;
- writes this plugin (embedded in the binary, so the two always match) to
  `~/.local/share/goagentic/herdr-plugin` and registers it, replacing any
  earlier registration;
- binds `prefix+a` to open the dashboard and `prefix+shift+a` to open the
  fleet view, each unless it already has a key (so re-running adds the fleet
  key to an older install); it backs up `config.toml` first, and leaves it
  untouched if a key is taken or Herdr rejects the change;
- reloads Herdr's config.

Run the same line again to update. Options after `--dash`: `--key <key>` and
`--fleet-key <key>` to bind other keys (`--fleet-key ""` skips the fleet key),
`--no-key` to skip both.

If `goagentic` is already installed: `goagentic herdr install`. To check or
remove: `goagentic herdr status`, `goagentic herdr uninstall` (removes the
plugin, its keybinding and its files).

The dashboard reads the agent workspaces on the machine it runs on, so install
it wherever your agents' files live.

## Use

| What | How |
|------|-----|
| Dashboard in its own "GoAgentic Dashboard" tab (switched to if already open in the Space) | `herdr plugin action invoke goagentic.dash.board`, or bind a key (below) |
| Fleet view, every workspace under `~/agents`, in its own "GoAgentic Fleet" tab; `enter` opens a workspace, `esc` comes back | `herdr plugin action invoke goagentic.dash.fleet`, or `prefix+shift+a` |
| Quick look over the current pane, closed with `q` or `esc` | `herdr plugin action invoke goagentic.dash.peek` |
| Refresh sidebar summaries now | `herdr plugin action invoke goagentic.dash.refresh` |

Inside the dashboard: `↑`/`↓` select an agent, `enter` opens it, `→` (or `space`)
shows its full detail, `r` refreshes, `a` includes retired agents, `q` quits.

`enter` never starts a second copy of an agent:

| Agent's state | `enter` |
|---|---|
| running in a pane | focuses that pane |
| pane still named for it, CLI exited (`shell`) | starts it again in that pane |
| no pane | opens a new tab in the current Space, in the workspace, and starts it |

It starts the agent with its `harness:` CLI (`claude` unless `context.md` or the
workspace conventions say otherwise). From the quick-look overlay the dashboard
closes after opening, so you land in the agent; the dashboard tab stays open.

The workspace is the one the focused pane sits in (the nearest directory with
`agents/CONVENTIONS.md`). If the focused pane is outside a workspace, the
dashboard uses the directory most of the Herdr Space's other panes share.

## Sidebar

The installer binds the key for you. To also show each Space's agent summary
("1 live · 3 for you · 2 !") under its name, add to `~/.config/herdr/config.toml`:

```toml
[ui.sidebar.spaces]
rows = [["state_icon", "workspace"], ["$agents"]]
```

The default Space rows are `[["state_icon", "workspace"], ["branch", "git_status"]]`;
add `"$agents"` wherever you like. The token is written on Herdr startup and
whenever an agent pane changes state, exits, or closes.

The summary reads: **live** — agent panes with a coding-agent CLI running;
**for you** — open tracker rows owned only by the principal, plus scheduled runs
nobody has read; **!** — agents with a health finding.
