#!/bin/sh
# Launcher for the goagentic.dash Herdr plugin. Finds the goagentic CLI and
# runs one of: dash (the dashboard), fleet (every workspace), here <tab>
# [dash|fleet] (either in a taken-over shell tab), sidebar (write $agents
# tokens), or open <entrypoint>.
set -u

find_goagentic() {
	# `goagentic herdr install` records the binary that installed the plugin.
	recorded=""
	if [ -n "${HERDR_PLUGIN_ROOT:-}" ] && [ -f "$HERDR_PLUGIN_ROOT/goagentic-path" ]; then
		recorded=$(head -n 1 "$HERDR_PLUGIN_ROOT/goagentic-path")
	fi
	for candidate in "${GOAGENTIC_BIN:-}" "$recorded" "$(command -v goagentic 2>/dev/null)" \
		"${GOBIN:-}/goagentic" "${GOPATH:-$HOME/go}/bin/goagentic" "$HOME/.local/bin/goagentic"; do
		if [ -n "$candidate" ] && [ -x "$candidate" ]; then
			echo "$candidate"
			return 0
		fi
	done
	return 1
}

herdr="${HERDR_BIN_PATH:-herdr}"
title="GoAgentic Dashboard"
fleet_title="GoAgentic Fleet"

case "${1:-dash}" in
dash | fleet)
	mode="$1"
	args="dash"
	if [ "$mode" = "fleet" ]; then
		title="$fleet_title"
		args="dash --fleet"
	fi
	if ! bin=$(find_goagentic); then
		echo "goagentic CLI not found."
		echo
		echo "Reinstall:  curl -fsSL https://raw.githubusercontent.com/normannoble/GoAgentic/main/install.sh | sh -s -- --dash"
		echo "or set GOAGENTIC_BIN to its path."
		echo
		printf "Press Enter to close. "
		read -r _
		exit 1
	fi
	# The board and the fleet open in a new tab of their own: name the tab
	# after it, move it to the left of the tab row, and drop the pane label,
	# which would only repeat the tab name.
	case "${HERDR_PLUGIN_ENTRYPOINT_ID:-}" in board | fleet) tabbed=1 ;; *) tabbed= ;; esac
	if [ -n "$tabbed" ] && [ -n "${HERDR_PANE_ID:-}" ]; then
		tab=$("$herdr" pane get "$HERDR_PANE_ID" 2>/dev/null | sed -n 's/.*"tab_id":"\([^"]*\)".*/\1/p')
		if [ -n "$tab" ]; then
			"$herdr" tab rename "$tab" "$title" >/dev/null 2>&1
			"$bin" herdr move-tab "$tab" >/dev/null 2>&1
		fi
		"$herdr" pane rename "$HERDR_PANE_ID" "" >/dev/null 2>&1
	fi
	# shellcheck disable=SC2086 # args is one or two plain words
	if ! "$bin" $args; then
		echo
		printf "Press Enter to close. "
		read -r _
	fi
	;;
sidebar)
	bin=$(find_goagentic) || exit 0
	exec "$bin" dash --herdr-sidebar
	;;
here)
	# Run in a shell tab taken over by "open board": name the tab while the
	# dashboard runs, then give the tab its old name back.
	tab="${2:-}"
	args="dash"
	if [ "${3:-dash}" = "fleet" ]; then
		title="$fleet_title"
		args="dash --fleet"
	fi
	old=$("$herdr" tab get "$tab" 2>/dev/null | sed -n 's/.*"label":"\([^"]*\)".*/\1/p')
	"$herdr" tab rename "$tab" "$title" >/dev/null 2>&1
	# shellcheck disable=SC2086 # args is one or two plain words
	bin=$(find_goagentic) && "$bin" $args
	"$herdr" tab rename "$tab" "$old" >/dev/null 2>&1
	;;
open)
	entry="${2:-peek}"
	mode="dash"
	if [ "$entry" = "fleet" ]; then
		title="$fleet_title"
		mode="fleet"
	fi
	if { [ "$entry" = "board" ] || [ "$entry" = "fleet" ]; } && command -v jq >/dev/null 2>&1; then
		ws="${HERDR_WORKSPACE_ID:-}"
		# One such tab per Space: switch to it if it is already open.
		existing=$("$herdr" tab list 2>/dev/null | jq -r --arg ws "$ws" --arg t "$title" \
			'[.result.tabs[] | select(.workspace_id == $ws and .label == $t)][0].tab_id // empty')
		if [ -n "$existing" ]; then
			exec "$herdr" tab focus "$existing"
		fi
		# Take over the current tab when it is nothing but an idle, unnamed
		# shell: one pane, no process but the shell, not an agent's pane.
		pane="${HERDR_PANE_ID:-}"
		if [ -n "$pane" ]; then
			tab=$("$herdr" pane get "$pane" 2>/dev/null | jq -r '.result.pane.tab_id // empty')
			panes=$("$herdr" tab get "$tab" 2>/dev/null | jq -r '.result.tab.pane_count // 0')
			idle=$("$herdr" pane process-info --pane "$pane" 2>/dev/null | jq -r \
				'.result.process_info | ([.foreground_processes[].pid] == [.shell_pid])')
			named=$("$herdr" agent list 2>/dev/null | jq -r --arg p "$pane" \
				'[.result.agents[] | select(.pane_id == $p)] | length')
			if [ -n "$tab" ] && [ "$panes" = "1" ] && [ "$idle" = "true" ] && [ "$named" = "0" ]; then
				exec "$herdr" pane run "$pane" "sh '$HERDR_PLUGIN_ROOT/goagentic-herdr.sh' here $tab $mode"
			fi
		fi
	fi
	# Start the pane in the agent workspace: Herdr names an unnamed Space
	# after its panes' repo, and the plugin's own directory is not that.
	cwd=""
	if bin=$(find_goagentic); then
		cwd=$("$bin" dash --print-root 2>/dev/null)
	fi
	exec "$herdr" plugin pane open --plugin "${HERDR_PLUGIN_ID:-goagentic.dash}" --entrypoint "$entry" \
		${cwd:+--cwd "$cwd"} --focus
	;;
*)
	echo "usage: goagentic-herdr.sh [dash|fleet|here <tab> [dash|fleet]|sidebar|open <entrypoint>]" >&2
	exit 2
	;;
esac
