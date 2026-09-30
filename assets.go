package agentframework

import "embed"

// Assets contains the framework documents, templates, and Claude Code skills
// compiled into every release binary.
//
//go:embed PHILOSOPHY.md template skills .claude-plugin/plugin.json
var Assets embed.FS

// HerdrPlugin holds the goagentic.dash Herdr plugin, which `goagentic herdr
// install` writes out so the plugin always matches this binary.
//
//go:embed integrations/herdr/herdr-plugin.toml integrations/herdr/goagentic-herdr.sh
var HerdrPlugin embed.FS

// Version is the public installer version. Release builds replace it with the
// matching tag through -ldflags, while tests keep the repository default in
// sync with install.sh.
var Version = "0.2.1"

// SourceCommit is populated for release binaries through -ldflags. A caller
// may still override the manifest value with AGENT_FRAMEWORK_SOURCE_COMMIT.
var SourceCommit string
