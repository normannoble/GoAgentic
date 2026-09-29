#!/bin/sh
# Launcher for the goagentic.dash Herdr plugin. Finds the goagentic CLI and
# runs one of: dash (the dashboard), sidebar (write $agents tokens), or
# open <entrypoint> (open a plugin pane).
set -u

find_goagentic() {
	for candidate in "${GOAGENTIC_BIN:-}" "$(command -v goagentic 2>/dev/null)" \
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

case "${1:-dash}" in
dash)
	if ! bin=$(find_goagentic); then
		echo "goagentic CLI not found."
		echo
		echo "Install it:  go install github.com/normannoble/GoAgentic/cmd/goagentic@latest"
		echo "or set GOAGENTIC_BIN to its path."
		echo
		printf "Press Enter to close. "
		read -r _
		exit 1
	fi
	# The board opens in a tab of its own: name the tab after it and drop the
	# pane label, which would only repeat the tab name.
	if [ "${HERDR_PLUGIN_ENTRYPOINT_ID:-}" = "board" ] && [ -n "${HERDR_PANE_ID:-}" ]; then
		tab=$("$herdr" pane get "$HERDR_PANE_ID" 2>/dev/null | sed -n 's/.*"tab_id":"\([^"]*\)".*/\1/p')
		[ -n "$tab" ] && "$herdr" tab rename "$tab" "$title" >/dev/null 2>&1
		"$herdr" pane rename "$HERDR_PANE_ID" "" >/dev/null 2>&1
	fi
	if ! "$bin" dash; then
		echo
		printf "Press Enter to close. "
		read -r _
	fi
	;;
sidebar)
	bin=$(find_goagentic) || exit 0
	exec "$bin" dash --herdr-sidebar
	;;
open)
	# One dashboard tab per Space: switch to it if it is already open.
	if [ "${2:-peek}" = "board" ] && [ -n "${HERDR_WORKSPACE_ID:-}" ] && command -v jq >/dev/null 2>&1; then
		existing=$("$herdr" tab list 2>/dev/null | jq -r --arg ws "$HERDR_WORKSPACE_ID" --arg t "$title" \
			'[.result.tabs[] | select(.workspace_id == $ws and .label == $t)][0].tab_id // empty')
		if [ -n "$existing" ]; then
			exec "$herdr" tab focus "$existing"
		fi
	fi
	exec "$herdr" plugin pane open --plugin "${HERDR_PLUGIN_ID:-goagentic.dash}" --entrypoint "${2:-peek}" --focus
	;;
*)
	echo "usage: goagentic-herdr.sh [dash|sidebar|open <entrypoint>]" >&2
	exit 2
	;;
esac
