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
	exec "$herdr" plugin pane open --plugin "${HERDR_PLUGIN_ID:-goagentic.dash}" --entrypoint "${2:-peek}" --focus
	;;
*)
	echo "usage: goagentic-herdr.sh [dash|sidebar|open <entrypoint>]" >&2
	exit 2
	;;
esac
