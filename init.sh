#!/bin/sh
set -eu

# Compatibility entrypoint. New installations should use:
#   curl -fsSL https://goagentic.sh/install.sh | sh

case $0 in
    *init.sh)
        if [ -f "$0" ]; then
            SCRIPT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
            if [ -f "$SCRIPT_DIR/install.sh" ]; then
                exec "$SCRIPT_DIR/install.sh" "$@"
            fi
        fi
        ;;
esac

if ! command -v curl >/dev/null 2>&1; then
    echo "error: curl is required to load the Agent Framework installer" >&2
    exit 1
fi

INSTALL_URL=${AGENT_FRAMEWORK_INSTALL_URL:-https://goagentic.sh/install.sh}
curl -fsSL "$INSTALL_URL" | sh -s -- "$@"
