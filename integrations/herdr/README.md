# goagentic.dash — Herdr plugin

Shows what every agent in the current workspace wants to do next, what it did
last, when it last ran, and whether its files are healthy, with no Claude
session running. It is a thin wrapper around `goagentic dash`; everything it
shows comes from the agent files.

## Install

```bash
go install github.com/normannoble/GoAgentic/cmd/goagentic@latest   # or: go install ./cmd/goagentic from a checkout
herdr plugin link ~/code/GoAgentic/integrations/herdr
```

The launcher finds `goagentic` on `PATH`, in `$GOBIN`, `~/go/bin`, or
`~/.local/bin`; set `GOAGENTIC_BIN` to override.

## Use

| What | How |
|------|-----|
| Quick look over the current pane | `herdr plugin action invoke goagentic.dash.peek`, or bind a key (below). `q` or `esc` closes it. |
| Dashboard pane beside your agents | `herdr plugin action invoke goagentic.dash.board` |
| Refresh sidebar summaries now | `herdr plugin action invoke goagentic.dash.refresh` |

Inside the dashboard: `↑`/`↓` select an agent, `enter` shows its full detail, `r`
refreshes, `a` includes retired agents, `q` quits.

The workspace is the one the focused pane sits in (the nearest directory with
`agents/CONVENTIONS.md`). If the focused pane is outside a workspace, the
dashboard uses the directory most of the Herdr Space's other panes share.

## Keybinding and sidebar

Add to `~/.config/herdr/config.toml`:

```toml
[[keys.command]]
key = "prefix+a"
type = "plugin_action"
command = "goagentic.dash.peek"
description = "agents dashboard"

# Show each Space's agent summary ("1 live · 3 for you · 2 !") under its name.
[ui.sidebar.spaces]
rows = [["state_icon", "workspace"], ["$agents"]]
```

The default Space rows are `[["state_icon", "workspace"], ["branch", "git_status"]]`;
add `"$agents"` wherever you like. The token is written on Herdr startup and
whenever an agent pane changes state, exits, or closes.

The summary reads: **live** — agent panes with a coding-agent CLI running;
**for you** — open tracker rows owned only by the principal, plus scheduled runs
nobody has read; **!** — agents with a health finding.
