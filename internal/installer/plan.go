package installer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	agentframework "github.com/normannoble/GoAgentic"
)

var unownedAgentsHeading = regexp.MustCompile(`(?m)^## Agents\s*$`)

func diskPath(target, relativePath string) string {
	return filepath.Join(target, filepath.FromSlash(relativePath))
}

func validateRelativePath(relativePath string) error {
	if relativePath == "" || relativePath == "." || strings.ContainsRune(relativePath, '\x00') {
		return installerError(ErrUnsafePath, nil, "Unsafe relative destination path: %s", relativePath)
	}
	clean := path.Clean(relativePath)
	if clean != relativePath || strings.HasPrefix(clean, "/") || clean == ".." || strings.HasPrefix(clean, "../") {
		return installerError(ErrUnsafePath, nil, "Unsafe relative destination path: %s", relativePath)
	}
	return nil
}

func lstat(filePath string) (os.FileInfo, bool, error) {
	info, err := os.Lstat(filePath)
	if err == nil {
		return info, true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	return nil, false, err
}

// IsGitRepository recognizes normal repositories, nested directories, and Git
// worktrees without relying on the shape of the .git entry.
func IsGitRepository(target string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "git", "-C", target, "rev-parse", "--is-inside-work-tree")
	output, err := command.Output()
	return err == nil && strings.TrimSpace(string(output)) == "true"
}

func hashContent(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

func loadManifest(target string) map[string]string {
	manifestPath := diskPath(target, ManifestPath)
	info, exists, err := lstat(manifestPath)
	if err != nil || !exists || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return map[string]string{}
	}
	content, err := os.ReadFile(manifestPath)
	if err != nil || !utf8.Valid(content) {
		return map[string]string{}
	}
	var payload map[string]any
	if json.Unmarshal(content, &payload) != nil {
		return map[string]string{}
	}
	files, ok := payload["files"].(map[string]any)
	if !ok {
		return map[string]string{}
	}
	result := make(map[string]string)
	for key, value := range files {
		if digest, ok := value.(string); ok {
			result[key] = digest
		}
	}
	return result
}

func unsafeDestinationReason(target, relativePath string) (string, error) {
	if err := validateRelativePath(relativePath); err != nil {
		return "destination path is not a safe relative path", nil
	}
	parts := strings.Split(relativePath, "/")
	current := target
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, filepath.FromSlash(part))
		info, exists, err := lstat(current)
		if err != nil {
			return "", err
		}
		if !exists {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Sprintf("parent path is a symbolic link: %s", current), nil
		}
		if !info.IsDir() {
			return fmt.Sprintf("parent path is not a directory: %s", current), nil
		}
	}

	destination := diskPath(target, relativePath)
	info, exists, err := lstat(destination)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", nil
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "destination is a symbolic link", nil
	}
	if !info.Mode().IsRegular() {
		return "destination exists and is not a regular file", nil
	}
	return "", nil
}

type classifyOptions struct {
	Target           string
	RelativePath     string
	Desired          []byte
	Label            string
	Group            string
	PreviousHashes   map[string]string
	PreserveExisting bool
	Managed          bool
	SafeUpdate       bool
}

func classifyFile(options classifyOptions) (*FileChange, error) {
	reason, err := unsafeDestinationReason(options.Target, options.RelativePath)
	if err != nil {
		return nil, err
	}
	if reason != "" {
		return &FileChange{
			RelativePath: options.RelativePath,
			Desired:      append([]byte(nil), options.Desired...),
			Status:       ChangeBlocked,
			Label:        options.Label,
			Group:        options.Group,
			Managed:      options.Managed,
			Reason:       reason,
		}, nil
	}

	destination := diskPath(options.Target, options.RelativePath)
	_, exists, err := lstat(destination)
	if err != nil {
		return nil, err
	}
	if !exists {
		return &FileChange{
			RelativePath: options.RelativePath,
			Desired:      append([]byte(nil), options.Desired...),
			Status:       ChangeAdd,
			Label:        options.Label,
			Group:        options.Group,
			Managed:      options.Managed,
		}, nil
	}

	existing, err := os.ReadFile(destination)
	if err != nil {
		return nil, err
	}
	change := &FileChange{
		RelativePath:    options.RelativePath,
		Desired:         append([]byte(nil), options.Desired...),
		Existing:        append([]byte{}, existing...),
		ExistingPresent: true,
		Label:           options.Label,
		Group:           options.Group,
		Managed:         options.Managed,
	}
	switch {
	case bytes.Equal(existing, options.Desired):
		change.Status = ChangeUnchanged
		change.Reason = "already matches"
	case options.PreserveExisting:
		change.Status = ChangeKeep
		change.Reason = "existing workspace content is preserved"
	case options.SafeUpdate:
		change.Status = ChangeUpdate
		change.Reason = "managed integration will be refreshed"
	case options.PreviousHashes[options.RelativePath] == hashContent(existing):
		change.Status = ChangeUpdate
		change.Reason = "previous managed version is unchanged"
	default:
		change.Status = ChangeConflict
		change.Reason = "existing content differs"
	}
	return change, nil
}

func pathDepth(relativePath string) int {
	if relativePath == "" || relativePath == "." {
		return 0
	}
	return strings.Count(relativePath, "/") + 1
}

func directoryChanges(target string, relativePaths []string) ([]DirectoryChange, error) {
	seen := make(map[string]bool)
	changes := make([]DirectoryChange, 0, len(relativePaths))
	for _, relativePath := range relativePaths {
		if seen[relativePath] {
			continue
		}
		seen[relativePath] = true
		if err := validateRelativePath(relativePath); err != nil {
			return nil, err
		}
		destination := diskPath(target, relativePath)
		info, exists, err := lstat(destination)
		if err != nil {
			return nil, err
		}
		if exists {
			if info.Mode()&os.ModeSymlink != 0 {
				return nil, installerError(
					ErrUnsafePath,
					nil,
					"Refusing symbolic-link directory: %s",
					destination,
				)
			}
			if !info.IsDir() {
				return nil, installerError(
					ErrUnsafePath,
					nil,
					"Expected a directory but found another file type: %s",
					destination,
				)
			}
		}
		changes = append(changes, DirectoryChange{RelativePath: relativePath, Create: !exists})
	}
	sort.Slice(changes, func(i, j int) bool {
		leftDepth := pathDepth(changes[i].RelativePath)
		rightDepth := pathDepth(changes[j].RelativePath)
		if leftDepth != rightDepth {
			return leftDepth < rightDepth
		}
		return changes[i].RelativePath < changes[j].RelativePath
	})
	return changes, nil
}

func claudeContent(existing []byte, existingPresent bool) ([]byte, string, bool, error) {
	if !existingPresent {
		return []byte(ClaudeBlock), "", true, nil
	}
	if !utf8.Valid(existing) {
		return nil, "", false, installerError(
			ErrInstaller,
			nil,
			"CLAUDE.md is not valid UTF-8 and cannot be integrated safely.",
		)
	}
	text := string(existing)
	beginCount := strings.Count(text, ClaudeBegin)
	endCount := strings.Count(text, ClaudeEnd)
	if beginCount != 0 || endCount != 0 {
		if beginCount != 1 || endCount != 1 {
			return nil, "", false, installerError(
				ErrInstaller,
				nil,
				"CLAUDE.md contains malformed Agent Framework managed markers.",
			)
		}
		begin := strings.Index(text, ClaudeBegin)
		endStart := strings.Index(text, ClaudeEnd)
		if endStart < begin {
			return nil, "", false, installerError(
				ErrInstaller,
				nil,
				"CLAUDE.md contains reversed Agent Framework managed markers.",
			)
		}
		end := endStart + len(ClaudeEnd)
		desired := text[:begin] + strings.TrimRight(ClaudeBlock, "\n") + text[end:]
		return []byte(desired), "", true, nil
	}
	if unownedAgentsHeading.MatchString(text) {
		return nil, "CLAUDE.md already has an unowned '## Agents' section; it was preserved.", false, nil
	}
	prefix := strings.TrimRight(text, "\n")
	if prefix == "" {
		return []byte(ClaudeBlock), "", true, nil
	}
	return []byte(prefix + "\n\n" + ClaudeBlock), "", true, nil
}

// BuildPlan performs the complete filesystem preflight without writing to the
// target. Every desired byte and existing snapshot is captured in the result.
func BuildPlan(options InstallOptions) (*InstallPlan, error) {
	if err := requireNamingPool(options.Naming); err != nil {
		return nil, err
	}
	info, err := os.Stat(options.Target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, installerError(
				ErrInvalidInput,
				err,
				"Target directory does not exist: %s",
				options.Target,
			)
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, installerError(
			ErrInvalidInput,
			nil,
			"Target is not a directory: %s",
			options.Target,
		)
	}

	previousHashes := loadManifest(options.Target)
	gitRepository := IsGitRepository(options.Target)
	if !gitRepository && !options.AllowNonGit {
		return nil, installerError(
			ErrInvalidInput,
			nil,
			"Target is not inside a Git repository: %s. Use --allow-non-git to continue.",
			options.Target,
		)
	}

	plan := &InstallPlan{Options: options, IsGitRepository: gitRepository}
	add := func(
		destination string,
		content []byte,
		label string,
		group string,
		preserveExisting bool,
		managed bool,
		safeUpdate bool,
	) error {
		if group == "" {
			group = "file:" + destination
		}
		change, err := classifyFile(classifyOptions{
			Target:           options.Target,
			RelativePath:     destination,
			Desired:          content,
			Label:            label,
			Group:            group,
			PreviousHashes:   previousHashes,
			PreserveExisting: preserveExisting,
			Managed:          managed,
			SafeUpdate:       safeUpdate,
		})
		if err != nil {
			return err
		}
		plan.Files = append(plan.Files, change)
		return nil
	}

	philosophy, err := ReadAsset("PHILOSOPHY.md")
	if err != nil {
		return nil, err
	}
	if err := add("PHILOSOPHY.md", philosophy, "Framework philosophy", "", false, true, false); err != nil {
		return nil, err
	}
	workspaceConventions, err := ReadAsset("template/CONVENTIONS.md")
	if err != nil {
		return nil, err
	}
	if err := add("CONVENTIONS.md", workspaceConventions, "Workspace conventions", "", false, true, false); err != nil {
		return nil, err
	}
	agentConventions, err := RenderAsset("template/agents/CONVENTIONS.md", options)
	if err != nil {
		return nil, err
	}
	if err := add("agents/CONVENTIONS.md", agentConventions, "Agent conventions", "", false, true, false); err != nil {
		return nil, err
	}
	toolsIndex, err := RenderAsset("template/agents/tools/INDEX.md", options)
	if err != nil {
		return nil, err
	}
	if err := add("agents/tools/INDEX.md", toolsIndex, "Shared tools index", "", false, true, false); err != nil {
		return nil, err
	}
	referenceEntries, err := fs.ReadDir(agentframework.Assets, "template/agents/reference")
	if err != nil {
		return nil, err
	}
	for _, entry := range referenceEntries {
		if entry.IsDir() {
			continue
		}
		content, err := RenderAsset("template/agents/reference/"+entry.Name(), options)
		if err != nil {
			return nil, err
		}
		if err := add("agents/reference/"+entry.Name(), content, "Agent conventions reference", "", false, true, false); err != nil {
			return nil, err
		}
	}

	directoryPaths := []string{"agents", "agents/tools", "agents/reference", "agents/skills"}
	if options.InstallSkills {
		manifest, err := ReadAsset(".claude-plugin/plugin.json")
		if err != nil {
			return nil, err
		}
		if err := add(PluginManifestPath, manifest, "agents plugin manifest", "skill:manifest", false, true, false); err != nil {
			return nil, err
		}
		for _, skill := range skills {
			content, err := RenderAsset(fmt.Sprintf("skills/%s/SKILL.md", skill.Name), options)
			if err != nil {
				return nil, err
			}
			group := "skill:" + skill.Name
			if err := add(skill.Canonical, content, "/agents:"+skill.Name+" skill", group, false, true, false); err != nil {
				return nil, err
			}
			if err := add(skill.Mirror, content, "/agents:"+skill.Name+" browsing mirror", group, false, true, false); err != nil {
				return nil, err
			}
		}
	}

	if options.CreateWorkspace {
		directoryPaths = append(directoryPaths, workspaceDirectories...)
		for _, workspaceFile := range workspaceFiles {
			if err := add(
				workspaceFile.Path,
				[]byte(workspaceFile.Content),
				"Workspace index",
				"",
				true,
				true,
				false,
			); err != nil {
				return nil, err
			}
		}
	}

	if options.IntegrateClaude {
		claudePath := diskPath(options.Target, "CLAUDE.md")
		var existing []byte
		var existingPresent bool
		_, exists, err := lstat(claudePath)
		if err != nil {
			return nil, err
		}
		if exists {
			reason, err := unsafeDestinationReason(options.Target, "CLAUDE.md")
			if err != nil {
				return nil, err
			}
			if reason != "" {
				plan.Files = append(plan.Files, &FileChange{
					RelativePath: "CLAUDE.md",
					Status:       ChangeBlocked,
					Label:        "Claude Code integration",
					Group:        "claude",
					Managed:      false,
					Reason:       reason,
				})
			} else {
				existing, err = os.ReadFile(claudePath)
				if err != nil {
					return nil, err
				}
				existingPresent = true
			}
		}
		blockedClaude := false
		for _, change := range plan.Files {
			if change.Group == "claude" {
				blockedClaude = true
				break
			}
		}
		if !blockedClaude {
			desired, note, include, err := claudeContent(existing, existingPresent)
			if err != nil {
				return nil, err
			}
			if note != "" {
				plan.Notes = append(plan.Notes, note)
			} else if include {
				if err := add(
					"CLAUDE.md",
					desired,
					"Claude Code integration",
					"claude",
					false,
					false,
					existingPresent,
				); err != nil {
					return nil, err
				}
			}
		}
	}

	parentSet := make(map[string]bool)
	for _, change := range plan.Files {
		parent := path.Dir(change.RelativePath)
		for parent != "." {
			parentSet[parent] = true
			parent = path.Dir(parent)
		}
	}
	for parent := range parentSet {
		directoryPaths = append(directoryPaths, parent)
	}
	plan.Directories, err = directoryChanges(options.Target, directoryPaths)
	if err != nil {
		return nil, err
	}
	return plan, nil
}

// ApplyConflictPolicy resolves all current conflicts or rejects policies that
// require an interactive decision.
func ApplyConflictPolicy(plan *InstallPlan, policy ConflictPolicy) error {
	conflicts := plan.Conflicts()
	if len(conflicts) == 0 {
		return nil
	}
	if policy == ConflictAsk || policy == ConflictFail {
		paths := make([]string, 0, len(conflicts))
		for _, change := range conflicts {
			paths = append(paths, change.RelativePath)
		}
		return installerError(
			ErrUnresolvedConflict,
			nil,
			"Existing customized files need a decision: %s",
			strings.Join(paths, ", "),
		)
	}
	if policy != ConflictKeep && policy != ConflictOverwrite {
		return installerError(ErrInvalidInput, nil, "Unknown conflict policy: %s", policy)
	}
	for _, change := range conflicts {
		if policy == ConflictKeep {
			change.Status = ChangeKeep
			change.Reason = "kept by conflict policy"
		} else {
			if change.ExistingPresent {
				change.Status = ChangeUpdate
			} else {
				change.Status = ChangeAdd
			}
			change.Reason = "replaced by conflict policy"
		}
	}
	return nil
}

// ResolveConflictGroup applies one explicit keep/overwrite choice atomically
// to all unresolved files in a group.
func ResolveConflictGroup(plan *InstallPlan, group string, overwrite bool) error {
	found := false
	for _, change := range plan.Files {
		if change.Group != group {
			continue
		}
		found = true
		if change.Status != ChangeConflict {
			continue
		}
		if overwrite {
			if change.ExistingPresent {
				change.Status = ChangeUpdate
			} else {
				change.Status = ChangeAdd
			}
			change.Reason = "approved replacement"
		} else {
			change.Status = ChangeKeep
			change.Reason = "existing content kept"
		}
	}
	if !found {
		return installerError(ErrUnresolvedConflict, nil, "Unknown conflict group: %s", group)
	}
	return nil
}

func actualContent(change *FileChange) ([]byte, error) {
	if change.Status == ChangeKeep {
		if !change.ExistingPresent {
			return nil, installerError(
				ErrInstaller,
				nil,
				"Cannot keep missing file: %s",
				change.RelativePath,
			)
		}
		return change.Existing, nil
	}
	return change.Desired, nil
}

func requireResolvedPlan(plan *InstallPlan) error {
	if blocked := plan.Blocked(); len(blocked) > 0 {
		paths := make([]string, 0, len(blocked))
		for _, change := range blocked {
			paths = append(paths, change.RelativePath)
		}
		return installerError(
			ErrUnsafePath,
			nil,
			"Unsafe destination paths: %s",
			strings.Join(paths, ", "),
		)
	}
	if conflicts := plan.Conflicts(); len(conflicts) > 0 {
		paths := make([]string, 0, len(conflicts))
		for _, change := range conflicts {
			paths = append(paths, change.RelativePath)
		}
		return installerError(
			ErrUnresolvedConflict,
			nil,
			"Unresolved conflicts: %s",
			strings.Join(paths, ", "),
		)
	}
	return nil
}

func findChange(plan *InstallPlan, relativePath string) (*FileChange, error) {
	for _, change := range plan.Files {
		if change.RelativePath == relativePath {
			return change, nil
		}
	}
	return nil, installerError(ErrInstaller, nil, "Plan is missing required file: %s", relativePath)
}

func manifestSourceCommit() *string {
	commit, set := os.LookupEnv("AGENT_FRAMEWORK_SOURCE_COMMIT")
	if !set {
		commit = agentframework.SourceCommit
	}
	if commit == "" {
		return nil
	}
	return &commit
}

func appendPythonJSONString(builder *strings.Builder, value string) {
	builder.WriteByte('"')
	for _, character := range value {
		switch character {
		case '"':
			builder.WriteString(`\"`)
		case '\\':
			builder.WriteString(`\\`)
		case '\b':
			builder.WriteString(`\b`)
		case '\t':
			builder.WriteString(`\t`)
		case '\n':
			builder.WriteString(`\n`)
		case '\f':
			builder.WriteString(`\f`)
		case '\r':
			builder.WriteString(`\r`)
		default:
			switch {
			case character >= 0x20 && character <= 0x7e:
				builder.WriteRune(character)
			case character <= 0xffff:
				fmt.Fprintf(builder, `\u%04x`, character)
			default:
				value := character - 0x10000
				high := 0xd800 + (value >> 10)
				low := 0xdc00 + (value & 0x3ff)
				fmt.Fprintf(builder, `\u%04x\u%04x`, high, low)
			}
		}
	}
	builder.WriteByte('"')
}

func manifestBytes(plan *InstallPlan, managedHashes map[string]string) ([]byte, error) {
	// Match json.dumps(..., indent=2, sort_keys=True) byte-for-byte so an
	// installation created by the Python prototype remains a no-op after the
	// Go migration, including for non-ASCII principal names.
	var builder strings.Builder
	builder.WriteString("{\n")
	builder.WriteString("  \"components\": {\n")
	fmt.Fprintf(&builder, "    \"claude\": %t,\n", plan.Options.IntegrateClaude)
	fmt.Fprintf(&builder, "    \"skills\": %t,\n", plan.Options.InstallSkills)
	fmt.Fprintf(&builder, "    \"workspace\": %t\n", plan.Options.CreateWorkspace)
	builder.WriteString("  },\n")

	keys := sortedKeys(managedHashes)
	if len(keys) == 0 {
		builder.WriteString("  \"files\": {},\n")
	} else {
		builder.WriteString("  \"files\": {\n")
		for index, key := range keys {
			builder.WriteString("    ")
			appendPythonJSONString(&builder, key)
			builder.WriteString(": ")
			appendPythonJSONString(&builder, managedHashes[key])
			if index < len(keys)-1 {
				builder.WriteByte(',')
			}
			builder.WriteByte('\n')
		}
		builder.WriteString("  },\n")
	}

	builder.WriteString("  \"framework_version\": ")
	appendPythonJSONString(&builder, agentframework.Version)
	builder.WriteString(",\n  \"naming\": ")
	appendPythonJSONString(&builder, string(plan.Options.Naming))
	builder.WriteString(",\n  \"principal\": ")
	appendPythonJSONString(&builder, plan.Options.Principal)
	builder.WriteString(",\n  \"schema\": 1,\n  \"source_commit\": ")
	if commit := manifestSourceCommit(); commit == nil {
		builder.WriteString("null")
	} else {
		appendPythonJSONString(&builder, *commit)
	}
	builder.WriteString("\n}\n")
	return []byte(builder.String()), nil
}

// FinalizePlan validates all decisions, makes kept canonical skills the source
// of truth for their mirrors, and adds the deterministic managed manifest.
func FinalizePlan(plan *InstallPlan) error {
	if err := requireResolvedPlan(plan); err != nil {
		return err
	}
	previousHashes := loadManifest(plan.Options.Target)
	withoutManifest := plan.Files[:0]
	for _, change := range plan.Files {
		if change.RelativePath != ManifestPath {
			withoutManifest = append(withoutManifest, change)
		}
	}
	plan.Files = withoutManifest

	if plan.Options.InstallSkills {
		for _, skill := range skills {
			canonical, err := findChange(plan, skill.Canonical)
			if err != nil {
				return err
			}
			content, err := actualContent(canonical)
			if err != nil {
				return err
			}
			mirror, err := findChange(plan, skill.Mirror)
			if err != nil {
				return err
			}
			mirror.Desired = append([]byte(nil), content...)
			if canonical.Status == ChangeKeep {
				switch {
				case !mirror.ExistingPresent:
					mirror.Status = ChangeAdd
					mirror.Reason = "missing browsing mirror will copy the kept skill"
				case bytes.Equal(mirror.Existing, content):
					mirror.Status = ChangeUnchanged
					mirror.Reason = "already matches the kept skill"
				default:
					mirror.Status = ChangeKeep
					mirror.Reason = "existing browsing mirror kept with canonical skill"
				}
			}
		}
	}

	managedHashes := make(map[string]string)
	for _, change := range plan.Files {
		if !change.Managed || (change.Status != ChangeAdd && change.Status != ChangeUpdate && change.Status != ChangeUnchanged) {
			continue
		}
		content, err := actualContent(change)
		if err != nil {
			return err
		}
		managedHashes[change.RelativePath] = hashContent(content)
	}
	manifest, err := manifestBytes(plan, managedHashes)
	if err != nil {
		return err
	}
	manifestChange, err := classifyFile(classifyOptions{
		Target:         plan.Options.Target,
		RelativePath:   ManifestPath,
		Desired:        manifest,
		Label:          "Installation manifest",
		Group:          "manifest",
		PreviousHashes: previousHashes,
		Managed:        false,
		SafeUpdate:     true,
	})
	if err != nil {
		return err
	}
	plan.Files = append(plan.Files, manifestChange)

	directoryPaths := make([]string, 0, len(plan.Directories)+len(plan.Files))
	for _, directory := range plan.Directories {
		directoryPaths = append(directoryPaths, directory.RelativePath)
	}
	for _, change := range plan.Files {
		if parent := path.Dir(change.RelativePath); parent != "." {
			directoryPaths = append(directoryPaths, parent)
		}
	}
	plan.Directories, err = directoryChanges(plan.Options.Target, directoryPaths)
	if err != nil {
		return err
	}
	if err := requireResolvedPlan(plan); err != nil {
		return err
	}
	plan.Finalized = true
	return nil
}
