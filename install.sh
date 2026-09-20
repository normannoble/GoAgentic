#!/bin/sh
set -eu

# Public bootstrap for:
#   curl -fsSL https://agent-framework.sh/install.sh | sh
#
# Release binaries are built from the GitHub repository for each supported
# operating system and architecture. AGENT_FRAMEWORK_BINARY is a local-only
# escape hatch for testing an unpublished binary.

VERSION=${AGENT_FRAMEWORK_VERSION:-0.1.0}
REPOSITORY=${AGENT_FRAMEWORK_REPOSITORY:-normannoble/GoAgentic}
RELEASE_BASE=${AGENT_FRAMEWORK_RELEASE_BASE:-"https://github.com/${REPOSITORY}/releases/download/v${VERSION}"}

TEMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/goagentic.XXXXXX")
trap 'rm -rf "$TEMP_DIR"' 0
trap 'exit 129' 1
trap 'exit 130' 2
trap 'exit 143' 15

sha256_file() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$1" | awk '{print $1}'
    else
        echo "error: sha256sum or shasum is required to verify Agent Framework" >&2
        exit 1
    fi
}

BINARY="$TEMP_DIR/goagentic"

if [ -n "${AGENT_FRAMEWORK_BINARY:-}" ]; then
    if [ ! -f "$AGENT_FRAMEWORK_BINARY" ]; then
        echo "error: local Agent Framework binary does not exist: $AGENT_FRAMEWORK_BINARY" >&2
        exit 1
    fi
    cp "$AGENT_FRAMEWORK_BINARY" "$BINARY"
else
    if ! command -v curl >/dev/null 2>&1; then
        echo "error: curl is required to download Agent Framework" >&2
        exit 1
    fi

    case $(uname -s) in
        Darwin) OS=darwin ;;
        Linux) OS=linux ;;
        *)
            echo "error: unsupported operating system: $(uname -s)" >&2
            exit 1
            ;;
    esac

    case $(uname -m) in
        arm64 | aarch64) ARCH=arm64 ;;
        x86_64 | amd64) ARCH=amd64 ;;
        *)
            echo "error: unsupported architecture: $(uname -m)" >&2
            exit 1
            ;;
    esac

    ASSET="goagentic_${VERSION}_${OS}_${ARCH}"
    CHECKSUM="$TEMP_DIR/$ASSET.sha256"
    curl -LsSf "$RELEASE_BASE/$ASSET" -o "$BINARY"
    curl -LsSf "$RELEASE_BASE/$ASSET.sha256" -o "$CHECKSUM"

    EXPECTED=$(awk 'NF { print $1; exit }' "$CHECKSUM")
    ACTUAL=$(sha256_file "$BINARY")
    if [ -z "$EXPECTED" ] || [ "$ACTUAL" != "$EXPECTED" ]; then
        echo "error: Agent Framework binary checksum verification failed" >&2
        exit 1
    fi
fi

chmod 0755 "$BINARY"

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
        # In the canonical curl | sh flow, stdin contains this script while
        # stdout is the shell's existing read/write terminal descriptor.
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

run_cli "$@"
