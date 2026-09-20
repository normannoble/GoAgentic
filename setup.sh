#!/bin/sh
set -eu

# Local source-checkout launcher. The public install.sh bootstrap downloads a
# pinned, checksummed Go binary and does not require a local Go installation.

SCRIPT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)

if [ ! -f "$SCRIPT_DIR/go.mod" ]; then
    echo "error: setup.sh must be run from an Agent Framework source checkout" >&2
    exit 1
fi

if ! command -v go >/dev/null 2>&1; then
    echo "error: Go is required to run setup.sh from a source checkout" >&2
    exit 1
fi

TEMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/goagentic.XXXXXX")
trap 'rm -rf "$TEMP_DIR"' 0
trap 'exit 129' 1
trap 'exit 130' 2
trap 'exit 143' 15

BINARY="$TEMP_DIR/goagentic"
(
    cd "$SCRIPT_DIR"
    GO111MODULE=on CGO_ENABLED=0 go build -trimpath -o "$BINARY" ./cmd/goagentic
)

flag_value_is_true() {
    case $1 in
        1 | t | T | true | TRUE | True) return 0 ;;
        *) return 1 ;;
    esac
}

non_interactive_requested() {
    json=false
    yes=false
    dry_run=false
    for argument in "$@"; do
        case $argument in
            --) break ;;
            --json) json=true ;;
            --json=*)
                if flag_value_is_true "${argument#*=}"; then json=true; else json=false; fi
                ;;
            --yes | -y) yes=true ;;
            --yes=* | -y=*)
                if flag_value_is_true "${argument#*=}"; then yes=true; else yes=false; fi
                ;;
            --dry-run) dry_run=true ;;
            --dry-run=*)
                if flag_value_is_true "${argument#*=}"; then dry_run=true; else dry_run=false; fi
                ;;
        esac
    done
    [ "$json" = true ] || [ "$yes" = true ] || [ "$dry_run" = true ]
}

run_cli() {
    if non_interactive_requested "$@"; then
        # Preserve the caller's descriptors so redirected JSON and dry-run
        # output remains machine-readable and capturable.
        "$BINARY" init "$@"
    elif [ -t 0 ] && [ -t 1 ]; then
        "$BINARY" init "$@"
    elif [ -t 1 ]; then
        "$BINARY" init "$@" <&1
    elif [ -t 2 ] && [ -c /dev/tty ]; then
        # Huh needs both its input and output streams to be terminals. Keep
        # diagnostics on stderr while routing the interactive form to the
        # controlling terminal when normal stdout is redirected.
        "$BINARY" init "$@" </dev/tty >/dev/tty
    else
        "$BINARY" init "$@"
    fi
}

# The binary executes outside the build subshell, preserving the directory from
# which the user launched setup.sh as the default installation target.
run_cli "$@"
