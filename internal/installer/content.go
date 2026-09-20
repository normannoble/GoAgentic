package installer

import (
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	agentframework "github.com/normannoble/GoAgentic"
)

// NamingProfiles is the complete set of supported naming traditions.
var NamingProfiles = map[NamingPool]NamingProfile{
	NamingRoman: {
		Key:       NamingRoman,
		Title:     "Roman cognomina",
		Tradition: "Roman cognomina",
		Description: "historical Roman names that are dignified, neutral, and large enough " +
			"as a pool to scale",
		Examples: "Cato, Varro, Seneca, Corvus, Regulus, Cassia, Livia, Marius, Titus, " +
			"Praxis, Lucian, Nerva, Flavia, Sabina, Quintus, Aulus, Gaius, Tertia, " +
			"Decima, Balbus",
	},
	NamingNorse: {
		Key:       NamingNorse,
		Title:     "Norse sagas",
		Tradition: "Norse saga names",
		Description: "names from Norse mythology and saga literature — strong, evocative, " +
			"and drawn from a deep cultural well",
		Examples: "Sigrid, Bjorn, Freya, Leif, Astrid, Gunnar, Ingrid, Ragna, Eirik, Sif, " +
			"Tyr, Vidar, Brynhild, Ivar, Solveig, Arne, Dagny, Halvard, Rune, Thyra",
	},
	NamingHellenic: {
		Key:       NamingHellenic,
		Title:     "Hellenic sages",
		Tradition: "Hellenic names",
		Description: "names from ancient Greek history and philosophy — associated with " +
			"wisdom, governance, and systematic thought",
		Examples: "Solon, Thales, Hypatia, Aspasia, Pericles, Zeno, Lycurgus, Diotima, " +
			"Arete, Philo, Cleisthenes, Myia, Timaeus, Aristos, Charis, Hector, " +
			"Melos, Doris, Xanthippe, Archon",
	},
}

var workspaceDirectories = []string{
	"thinking",
	"work",
	"work/projects",
	"work/operations",
	"knowledge",
	"knowledge/systems",
	"knowledge/people",
	"knowledge/processes",
	"knowledge/company",
	"outputs",
}

type workspaceFile struct {
	Path    string
	Content string
}

var workspaceFiles = []workspaceFile{
	{
		Path: "thinking/INDEX.md",
		Content: `---
title: Thinking
type: index
scope: area
---

# Thinking

Unstructured capture — ideas, conversations, notes, and backlog items. Low friction, minimal structure.
`,
	},
	{
		Path: "work/INDEX.md",
		Content: `---
title: Work
type: index
scope: area
---

# Work

Structured work — initiatives and ongoing responsibilities.

## Lanes

- [[work/projects/INDEX|Projects]] — Finite initiatives with clear deliverables and timelines
- [[work/operations/INDEX|Operations]] — Ongoing responsibilities (no fixed end date)
`,
	},
	{
		Path: "knowledge/INDEX.md",
		Content: `---
title: Knowledge
type: index
scope: area
---

# Knowledge

Distilled reference material. Items here are maintained over time — they represent settled understanding, not in-progress thinking.

## Categories

- **systems/** — Platform architecture, integrations, and technical concepts
- **people/** — Context about people you work with
- **processes/** — Operational processes, standards, and principles
- **company/** — Org structure, strategy, and business context
`,
	},
	{
		Path: "outputs/INDEX.md",
		Content: `---
title: Outputs
type: index
scope: workspace
---

# Outputs

Artifacts produced for specific audiences — decks, reports, emails, briefings.
`,
	},
	{
		Path: "knowledge/MOC.md",
		Content: `---
title: Knowledge Map
type: moc
scope: workspace
---

# Knowledge Map

Conceptual map of knowledge, organized by domain. Each category has its own folder for browsing.
`,
	},
}

const (
	ClaudeBegin = "<!-- agent-framework:agents:start -->"
	ClaudeEnd   = "<!-- agent-framework:agents:end -->"
	ClaudeBlock = ClaudeBegin + `
## Agents

Persistent AI collaborators with calibrated autonomy. See ` + "`PHILOSOPHY.md`" + ` for principles, ` + "`agents/CONVENTIONS.md`" + ` for mechanics.

| Name | Role |
|------|------|
| *(use ` + "`/agents:new`" + ` to add your first agent)* | |

Invoke with ` + "`/agents:start <name>`" + `. List with ` + "`/agents:list`" + `; board ` + "`/agents:status`" + `; next move ` + "`/agents:next`" + `; health check ` + "`/agents:doctor`" + `; scheduled tasks ` + "`/agents:schedule`" + `; peer request ` + "`/agents:ask`" + `; guide ` + "`/agents:help`" + `.
` + ClaudeEnd + "\n"

	ManifestPath = ".agent-framework/install.json"
)

type skillPaths struct {
	Name      string
	Canonical string
	Mirror    string
}

// PluginDir is the in-repo plugin folder. Any folder under .claude/skills/ that
// carries .claude-plugin/plugin.json is loaded by Claude Code as a project
// plugin named "agents@skills-dir", so its skills are namespaced (/agents:help, /agents:start,
// /agents:wrap, /agents:close, /agents:list, /agents:status, /agents:next, /agents:doctor, /agents:harness,
// /agents:schedule, /agents:ask, /agents:new) — the same commands
// the marketplace plugin provides.
const PluginDir = ".claude/skills/agents"

// PluginManifestPath is where the copied plugin manifest lands.
const PluginManifestPath = PluginDir + "/.claude-plugin/plugin.json"

var skills = []skillPaths{
	{Name: "help", Canonical: PluginDir + "/skills/help/SKILL.md", Mirror: "agents/skills/help/SKILL.md"},
	{Name: "start", Canonical: PluginDir + "/skills/start/SKILL.md", Mirror: "agents/skills/start/SKILL.md"},
	{Name: "wrap", Canonical: PluginDir + "/skills/wrap/SKILL.md", Mirror: "agents/skills/wrap/SKILL.md"},
	{Name: "close", Canonical: PluginDir + "/skills/close/SKILL.md", Mirror: "agents/skills/close/SKILL.md"},
	{Name: "list", Canonical: PluginDir + "/skills/list/SKILL.md", Mirror: "agents/skills/list/SKILL.md"},
	{Name: "status", Canonical: PluginDir + "/skills/status/SKILL.md", Mirror: "agents/skills/status/SKILL.md"},
	{Name: "next", Canonical: PluginDir + "/skills/next/SKILL.md", Mirror: "agents/skills/next/SKILL.md"},
	{Name: "doctor", Canonical: PluginDir + "/skills/doctor/SKILL.md", Mirror: "agents/skills/doctor/SKILL.md"},
	{Name: "harness", Canonical: PluginDir + "/skills/harness/SKILL.md", Mirror: "agents/skills/harness/SKILL.md"},
	{Name: "schedule", Canonical: PluginDir + "/skills/schedule/SKILL.md", Mirror: "agents/skills/schedule/SKILL.md"},
	{Name: "ask", Canonical: PluginDir + "/skills/ask/SKILL.md", Mirror: "agents/skills/ask/SKILL.md"},
	{Name: "new", Canonical: PluginDir + "/skills/new/SKILL.md", Mirror: "agents/skills/new/SKILL.md"},
}

var markerPattern = regexp.MustCompile(`\{\{[A-Z_]+\}\}`)

// ValidatePrincipal applies the same whitespace, length, and control-character
// rules used by the Python installer.
func ValidatePrincipal(value string) (string, error) {
	principal := strings.TrimSpace(value)
	if principal == "" {
		return "the principal", nil
	}
	if utf8.RuneCountInString(principal) > 120 {
		return "", installerError(
			ErrInvalidInput,
			nil,
			"Principal name must be 120 characters or fewer.",
		)
	}
	for _, character := range principal {
		if character < 32 || character == 127 {
			return "", installerError(
				ErrInvalidInput,
				nil,
				"Principal name cannot contain newlines or control characters.",
			)
		}
	}
	return principal, nil
}

// ReadAsset reads a canonical framework resource from the root embedded FS.
func ReadAsset(relativePath string) ([]byte, error) {
	if !fs.ValidPath(relativePath) || relativePath == "." {
		return nil, installerError(
			ErrInstaller,
			nil,
			"Invalid installer asset path: %s",
			relativePath,
		)
	}
	content, err := fs.ReadFile(agentframework.Assets, relativePath)
	if err != nil {
		return nil, installerError(
			ErrInstaller,
			err,
			"Unable to read installer asset: %s",
			relativePath,
		)
	}
	return content, nil
}

// RenderAsset substitutes markers in one pass. Replacement text is never
// scanned again, so principal text containing marker-like strings is literal.
func RenderAsset(relativePath string, options InstallOptions) ([]byte, error) {
	content, err := ReadAsset(relativePath)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(content) {
		return nil, installerError(
			ErrInstaller,
			nil,
			"Installer asset is not valid UTF-8: %s",
			relativePath,
		)
	}
	profile, ok := NamingProfiles[options.Naming]
	if !ok {
		return nil, installerError(
			ErrInvalidInput,
			nil,
			"Unknown naming pool: %s",
			options.Naming,
		)
	}
	replacements := map[string]string{
		"{{PRINCIPAL}}":          options.Principal,
		"{{NAMING_TRADITION}}":   profile.Tradition,
		"{{NAMING_DESCRIPTION}}": profile.Description,
		"{{NAMING_EXAMPLES}}":    profile.Examples,
	}
	original := string(content)
	unknownSet := make(map[string]bool)
	for _, marker := range markerPattern.FindAllString(original, -1) {
		if _, known := replacements[marker]; !known {
			unknownSet[marker] = true
		}
	}
	if len(unknownSet) > 0 {
		unknown := make([]string, 0, len(unknownSet))
		for marker := range unknownSet {
			unknown = append(unknown, marker)
		}
		sort.Strings(unknown)
		return nil, installerError(
			ErrInstaller,
			nil,
			"Unresolved template markers in %s: %s",
			relativePath,
			strings.Join(unknown, ", "),
		)
	}
	rendered := markerPattern.ReplaceAllStringFunc(original, func(marker string) string {
		return replacements[marker]
	})
	return []byte(rendered), nil
}

func requireNamingPool(pool NamingPool) error {
	if _, ok := NamingProfiles[pool]; !ok {
		return installerError(ErrInvalidInput, nil, "Unknown naming pool: %s", pool)
	}
	return nil
}

func assetf(format string, args ...any) ([]byte, error) {
	return ReadAsset(fmt.Sprintf(format, args...))
}
