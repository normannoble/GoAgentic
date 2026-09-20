package installer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	agentframework "github.com/normannoble/GoAgentic"
)

func gitRepo(t *testing.T) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("git", "init", "-q", target)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	return target
}

func testOptions(target string) InstallOptions {
	return InstallOptions{
		Target:          target,
		Principal:       "Fauzaan",
		Naming:          NamingRoman,
		InstallSkills:   true,
		CreateWorkspace: true,
		IntegrateClaude: false,
		AllowNonGit:     false,
	}
}

func readyPlan(t *testing.T, options InstallOptions, policy ConflictPolicy) *InstallPlan {
	t.Helper()
	plan, err := BuildPlan(options)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyConflictPolicy(plan, policy); err != nil {
		t.Fatal(err)
	}
	if err := FinalizePlan(plan); err != nil {
		t.Fatal(err)
	}
	return plan
}

func findPlannedFile(t *testing.T, plan *InstallPlan, relativePath string) *FileChange {
	t.Helper()
	for _, change := range plan.Files {
		if change.RelativePath == relativePath {
			return change
		}
	}
	t.Fatalf("missing planned file %s", relativePath)
	return nil
}

func writeFile(t *testing.T, filePath, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, filePath string) []byte {
	t.Helper()
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func treeEntries(t *testing.T, root string) []string {
	t.Helper()
	var entries []string
	err := filepath.WalkDir(root, func(filePath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, filePath)
		if err != nil {
			return err
		}
		if relative == ".git" || strings.HasPrefix(relative, ".git"+string(filepath.Separator)) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if relative != "." {
			entries = append(entries, filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestValidatePrincipal(t *testing.T) {
	principal, err := ValidatePrincipal("  Fauzaan  ")
	if err != nil || principal != "Fauzaan" {
		t.Fatalf("got %q, %v", principal, err)
	}
	principal, err = ValidatePrincipal(" \n ")
	if err != nil || principal != "the principal" {
		t.Fatalf("blank got %q, %v", principal, err)
	}
	if _, err := ValidatePrincipal("A\nB"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("control character error = %v", err)
	}
	if _, err := ValidatePrincipal(strings.Repeat("é", 121)); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("length error = %v", err)
	}
}

func TestRenderingIsLiteralAndResolvesMarkers(t *testing.T) {
	principal := `Fauzaan & Co | \\ Team`
	options := testOptions(gitRepo(t))
	options.Principal = principal
	options.Naming = NamingHellenic
	rendered, err := RenderAsset("template/agents/CONVENTIONS.md", options)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{principal, "Hellenic names", "Solon, Thales, Hypatia"} {
		if !bytes.Contains(rendered, []byte(expected)) {
			t.Fatalf("rendered content missing %q", expected)
		}
	}
	if bytes.Contains(rendered, []byte("{{")) {
		t.Fatal("rendered content still has a marker")
	}
	source, err := ReadAsset("template/agents/CONVENTIONS.md")
	if err != nil || !bytes.Contains(source, []byte("{{PRINCIPAL}}")) {
		t.Fatal("embedded source was changed")
	}
}

func TestRenderingDoesNotReinterpretReplacementMarkers(t *testing.T) {
	options := testOptions(gitRepo(t))
	options.Principal = "Name {{NAMING_TRADITION}} {{UNKNOWN}}"
	options.Naming = NamingHellenic
	rendered, err := RenderAsset("template/agents/CONVENTIONS.md", options)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(rendered, []byte(options.Principal)) {
		t.Fatal("principal was not inserted literally")
	}
	if bytes.Contains(rendered, []byte("Principal: Name Hellenic names")) {
		t.Fatal("replacement text was interpreted a second time")
	}
}

func TestBuildPlanPerformsNoWrites(t *testing.T) {
	target := gitRepo(t)
	before := treeEntries(t, target)
	plan, err := BuildPlan(testOptions(target))
	if err != nil {
		t.Fatal(err)
	}
	after := treeEntries(t, target)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("build plan wrote to disk: before=%v after=%v", before, after)
	}
	if len(plan.Conflicts()) != 0 {
		t.Fatalf("unexpected conflicts: %v", plan.Conflicts())
	}
	for _, required := range []string{
		"PHILOSOPHY.md",
		"CONVENTIONS.md",
		"agents/CONVENTIONS.md",
		"agents/tools/INDEX.md",
		".claude/skills/agents/.claude-plugin/plugin.json",
		".claude/skills/agents/skills/help/SKILL.md",
		".claude/skills/agents/skills/start/SKILL.md",
		".claude/skills/agents/skills/list/SKILL.md",
		".claude/skills/agents/skills/status/SKILL.md",
		".claude/skills/agents/skills/next/SKILL.md",
		".claude/skills/agents/skills/doctor/SKILL.md",
		".claude/skills/agents/skills/new/SKILL.md",
	} {
		findPlannedFile(t, plan, required)
	}
}

func TestApplyInstallsExpectedFilesAndSkillMirrors(t *testing.T) {
	target := gitRepo(t)
	options := testOptions(target)
	options.Naming = NamingNorse
	result, err := ApplyPlan(readyPlan(t, options, ConflictFail))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Written) != 43 {
		t.Fatalf("written = %d, want 43: %v", len(result.Written), result.Written)
	}
	philosophy, _ := ReadAsset("PHILOSOPHY.md")
	if !bytes.Equal(readFile(t, filepath.Join(target, "PHILOSOPHY.md")), philosophy) {
		t.Fatal("philosophy differs from embedded asset")
	}
	if !bytes.Contains(readFile(t, filepath.Join(target, "agents/CONVENTIONS.md")), []byte("Norse saga names")) {
		t.Fatal("naming profile was not rendered")
	}
	for _, skill := range []string{"help", "start", "wrap", "close", "list", "status", "next", "doctor", "harness", "schedule", "ask", "new"} {
		canonical := readFile(t, filepath.Join(target, ".claude/skills/agents/skills", skill, "SKILL.md"))
		mirror := readFile(t, filepath.Join(target, "agents/skills", skill, "SKILL.md"))
		if !bytes.Equal(canonical, mirror) {
			t.Fatalf("%s skill mirror differs", skill)
		}
	}
	var manifest map[string]any
	if err := json.Unmarshal(readFile(t, filepath.Join(target, ManifestPath)), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest["naming"] != "norse" || manifest["framework_version"] != agentframework.Version {
		t.Fatalf("manifest metadata = %#v", manifest)
	}
}

func TestIdenticalRerunIsTrueNoop(t *testing.T) {
	target := gitRepo(t)
	options := testOptions(target)
	if _, err := ApplyPlan(readyPlan(t, options, ConflictFail)); err != nil {
		t.Fatal(err)
	}
	tracked := filepath.Join(target, "agents/CONVENTIONS.md")
	before, err := os.Stat(tracked)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	second := readyPlan(t, options, ConflictFail)
	result, err := ApplyPlan(second)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(tracked)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Written) != 0 || second.Counts()["unchanged"] != 43 {
		t.Fatalf("rerun wrote %v, counts=%v", result.Written, second.Counts())
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("no-op rerun touched a managed file")
	}
}

func TestConflictPoliciesKeepAndOverwrite(t *testing.T) {
	for _, test := range []struct {
		name       string
		policy     ConflictPolicy
		wantCustom bool
	}{
		{name: "keep", policy: ConflictKeep, wantCustom: true},
		{name: "overwrite", policy: ConflictOverwrite, wantCustom: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			target := gitRepo(t)
			philosophyPath := filepath.Join(target, "PHILOSOPHY.md")
			writeFile(t, philosophyPath, "custom\n")
			options := testOptions(target)
			options.CreateWorkspace = false
			options.InstallSkills = false
			plan, err := BuildPlan(options)
			if err != nil {
				t.Fatal(err)
			}
			if findPlannedFile(t, plan, "PHILOSOPHY.md").Status != ChangeConflict {
				t.Fatal("custom file was not classified as a conflict")
			}
			if err := ApplyConflictPolicy(plan, test.policy); err != nil {
				t.Fatal(err)
			}
			if err := FinalizePlan(plan); err != nil {
				t.Fatal(err)
			}
			if _, err := ApplyPlan(plan); err != nil {
				t.Fatal(err)
			}
			content := readFile(t, philosophyPath)
			if test.wantCustom && string(content) != "custom\n" {
				t.Fatalf("kept content = %q", content)
			}
			if !test.wantCustom && string(content) == "custom\n" {
				t.Fatal("overwrite policy kept custom content")
			}
		})
	}
}

func TestManagedFilesUpdateWithoutFalseConflicts(t *testing.T) {
	target := gitRepo(t)
	roman := testOptions(target)
	if _, err := ApplyPlan(readyPlan(t, roman, ConflictFail)); err != nil {
		t.Fatal(err)
	}
	hellenic := roman
	hellenic.Naming = NamingHellenic
	updated, err := BuildPlan(hellenic)
	if err != nil {
		t.Fatal(err)
	}
	if change := findPlannedFile(t, updated, "agents/CONVENTIONS.md"); change.Status != ChangeUpdate {
		t.Fatalf("managed change status = %s", change.Status)
	}
	if len(updated.Conflicts()) != 0 {
		t.Fatalf("managed update conflicts: %v", updated.Conflicts())
	}
	if err := FinalizePlan(updated); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyPlan(updated); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(readFile(t, filepath.Join(target, "agents/CONVENTIONS.md")), []byte("Hellenic names")) {
		t.Fatal("managed update was not applied")
	}
}

func TestWorkspaceIndexesArePreserved(t *testing.T) {
	target := gitRepo(t)
	customPath := filepath.Join(target, "thinking/INDEX.md")
	writeFile(t, customPath, "my notes\n")
	plan := readyPlan(t, testOptions(target), ConflictFail)
	if change := findPlannedFile(t, plan, "thinking/INDEX.md"); change.Status != ChangeKeep {
		t.Fatalf("workspace status = %s", change.Status)
	}
	if _, err := ApplyPlan(plan); err != nil {
		t.Fatal(err)
	}
	if string(readFile(t, customPath)) != "my notes\n" {
		t.Fatal("workspace index was overwritten")
	}
}

func TestKeptCanonicalSkillControlsMissingMirror(t *testing.T) {
	target := gitRepo(t)
	canonicalPath := filepath.Join(target, ".claude/skills/agents/skills/start/SKILL.md")
	writeFile(t, canonicalPath, "custom agent skill\n")
	options := testOptions(target)
	options.CreateWorkspace = false
	plan := readyPlan(t, options, ConflictKeep)
	if _, err := ApplyPlan(plan); err != nil {
		t.Fatal(err)
	}
	canonical := readFile(t, canonicalPath)
	mirror := readFile(t, filepath.Join(target, "agents/skills/start/SKILL.md"))
	if !bytes.Equal(canonical, mirror) {
		t.Fatal("missing mirror did not copy the kept canonical skill")
	}
	if _, err := os.Stat(filepath.Join(target, ".claude/skills/agents/skills/new/SKILL.md")); err != nil {
		t.Fatal("missing second skill was not added")
	}
}

func TestCustomExistingSkillMirrorIsNotSilentlyOverwritten(t *testing.T) {
	target := gitRepo(t)
	mirrorPath := filepath.Join(target, "agents/skills/start/SKILL.md")
	writeFile(t, mirrorPath, "custom browsing copy\n")
	plan, err := BuildPlan(testOptions(target))
	if err != nil {
		t.Fatal(err)
	}
	if change := findPlannedFile(t, plan, "agents/skills/start/SKILL.md"); change.Status != ChangeConflict {
		t.Fatalf("mirror status = %s", change.Status)
	}
	if err := ApplyConflictPolicy(plan, ConflictKeep); err != nil {
		t.Fatal(err)
	}
	if err := FinalizePlan(plan); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyPlan(plan); err != nil {
		t.Fatal(err)
	}
	if string(readFile(t, mirrorPath)) != "custom browsing copy\n" {
		t.Fatal("custom mirror was overwritten")
	}
}

func TestClaudeManagedBlockIsIdempotentAndPreservesOtherContent(t *testing.T) {
	target := gitRepo(t)
	claudePath := filepath.Join(target, "CLAUDE.md")
	writeFile(t, claudePath, "# Project\n\nKeep this.\n")
	options := testOptions(target)
	options.IntegrateClaude = true
	if _, err := ApplyPlan(readyPlan(t, options, ConflictFail)); err != nil {
		t.Fatal(err)
	}
	first := string(readFile(t, claudePath))
	if strings.Count(first, ClaudeBegin) != 1 || strings.Count(first, ClaudeEnd) != 1 || !strings.Contains(first, "Keep this.") {
		t.Fatalf("unexpected first CLAUDE.md:\n%s", first)
	}
	writeFile(t, claudePath, strings.Replace(first, "Keep this.", "Keep this updated.", 1))
	if _, err := ApplyPlan(readyPlan(t, options, ConflictFail)); err != nil {
		t.Fatal(err)
	}
	second := string(readFile(t, claudePath))
	if strings.Count(second, ClaudeBegin) != 1 || strings.Count(second, ClaudeEnd) != 1 || !strings.Contains(second, "Keep this updated.") {
		t.Fatalf("unexpected second CLAUDE.md:\n%s", second)
	}
}

func TestClaudeMarkerAndUnownedHeadingSafety(t *testing.T) {
	t.Run("reversed markers fail preflight", func(t *testing.T) {
		target := gitRepo(t)
		writeFile(t, filepath.Join(target, "CLAUDE.md"), ClaudeEnd+"\ntext\n"+ClaudeBegin+"\n")
		options := testOptions(target)
		options.IntegrateClaude = true
		_, err := BuildPlan(options)
		if err == nil || !strings.Contains(err.Error(), "reversed") {
			t.Fatalf("error = %v", err)
		}
		if _, err := os.Stat(filepath.Join(target, "PHILOSOPHY.md")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("preflight wrote a file")
		}
	})
	t.Run("unowned section is preserved", func(t *testing.T) {
		target := gitRepo(t)
		claudePath := filepath.Join(target, "CLAUDE.md")
		writeFile(t, claudePath, "# Project\n\n## Agents\n\nCustom.\n")
		options := testOptions(target)
		options.IntegrateClaude = true
		plan, err := BuildPlan(options)
		if err != nil {
			t.Fatal(err)
		}
		if len(plan.Notes) != 1 || !strings.Contains(plan.Notes[0], "unowned") {
			t.Fatalf("notes = %v", plan.Notes)
		}
		for _, change := range plan.Files {
			if change.RelativePath == "CLAUDE.md" {
				t.Fatal("unowned CLAUDE.md was planned for modification")
			}
		}
	})
}

func TestSymlinkAndFileTypeDestinationsAreBlockedBeforeWrites(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics require privileges on Windows")
	}
	t.Run("file symlink", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "target")
		if err := os.Mkdir(target, 0o755); err != nil {
			t.Fatal(err)
		}
		if output, err := exec.Command("git", "init", "-q", target).CombinedOutput(); err != nil {
			t.Fatalf("git init: %v: %s", err, output)
		}
		outside := filepath.Join(root, "outside.md")
		writeFile(t, outside, "outside\n")
		if err := os.Symlink(outside, filepath.Join(target, "PHILOSOPHY.md")); err != nil {
			t.Fatal(err)
		}
		plan, err := BuildPlan(testOptions(target))
		if err != nil {
			t.Fatal(err)
		}
		if len(plan.Blocked()) == 0 {
			t.Fatal("symlink was not blocked")
		}
		if err := FinalizePlan(plan); !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("finalize error = %v", err)
		}
		if string(readFile(t, outside)) != "outside\n" {
			t.Fatal("symlink target was modified")
		}
	})
	t.Run("directory collision", func(t *testing.T) {
		target := gitRepo(t)
		writeFile(t, filepath.Join(target, "agents"), "not a directory\n")
		_, err := BuildPlan(testOptions(target))
		if !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("build error = %v", err)
		}
		if _, err := os.Stat(filepath.Join(target, "PHILOSOPHY.md")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("preflight wrote a file")
		}
	})
}

func TestDerivedSymlinkDestinationsAreBlockedDuringFinalize(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics require privileges on Windows")
	}
	for _, relativePath := range []string{"agents/skills/start/SKILL.md", ManifestPath} {
		t.Run(strings.ReplaceAll(relativePath, "/", "_"), func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, "target")
			if err := os.Mkdir(target, 0o755); err != nil {
				t.Fatal(err)
			}
			if output, err := exec.Command("git", "init", "-q", target).CombinedOutput(); err != nil {
				t.Fatalf("git init: %v: %s", err, output)
			}
			outside := filepath.Join(root, "outside-"+filepath.Base(relativePath))
			writeFile(t, outside, "outside\n")
			destination := diskPath(target, relativePath)
			if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, destination); err != nil {
				t.Fatal(err)
			}
			plan, err := BuildPlan(testOptions(target))
			if err != nil {
				t.Fatal(err)
			}
			_ = ApplyConflictPolicy(plan, ConflictFail)
			err = FinalizePlan(plan)
			if !errors.Is(err, ErrUnsafePath) || !strings.Contains(err.Error(), relativePath) {
				t.Fatalf("finalize error = %v", err)
			}
			if string(readFile(t, outside)) != "outside\n" {
				t.Fatal("symlink target was modified")
			}
		})
	}
}

func TestApplyFailureRollsBackEverything(t *testing.T) {
	target := gitRepo(t)
	plan := readyPlan(t, testOptions(target), ConflictFail)
	realAtomicWrite := atomicWrite
	defer func() { atomicWrite = realAtomicWrite }()
	calls := 0
	atomicWrite = func(destination string, content []byte, mode os.FileMode) error {
		calls++
		if calls == 2 {
			return errors.New("injected failure")
		}
		return realAtomicWrite(destination, content, mode)
	}
	_, err := ApplyPlan(plan)
	if err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("apply error = %v", err)
	}
	for _, relativePath := range []string{"PHILOSOPHY.md", "CONVENTIONS.md", ".agent-framework"} {
		if _, statErr := os.Stat(diskPath(target, relativePath)); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("%s remains after rollback: %v", relativePath, statErr)
		}
	}
}

func TestPanicAfterAtomicReplaceStillRollsBack(t *testing.T) {
	target := gitRepo(t)
	plan := readyPlan(t, testOptions(target), ConflictFail)
	realAtomicWrite := atomicWrite
	defer func() { atomicWrite = realAtomicWrite }()
	panicked := false
	atomicWrite = func(destination string, content []byte, mode os.FileMode) error {
		if err := realAtomicWrite(destination, content, mode); err != nil {
			return err
		}
		if !panicked {
			panicked = true
			panic("interrupt")
		}
		return nil
	}
	_, err := ApplyPlan(plan)
	if err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("apply error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "PHILOSOPHY.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("written file remains after panic rollback")
	}
}

func TestCancellationDuringApplyRollsBackEverything(t *testing.T) {
	t.Run("after directory creation", func(t *testing.T) {
		target := gitRepo(t)
		plan := readyPlan(t, testOptions(target), ConflictFail)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		realMakeDirectory := makeDirectory
		defer func() { makeDirectory = realMakeDirectory }()
		var created string
		makeDirectory = func(destination string, mode os.FileMode) error {
			if err := realMakeDirectory(destination, mode); err != nil {
				return err
			}
			if created == "" {
				created = destination
				cancel()
			}
			return nil
		}

		_, err := ApplyPlanContext(ctx, plan)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("apply error = %v; want context cancellation", err)
		}
		if created == "" {
			t.Fatal("cancellation seam did not create a directory")
		}
		if _, statErr := os.Stat(created); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("created directory remains after cancellation: %v", statErr)
		}
		if _, statErr := os.Stat(filepath.Join(target, "PHILOSOPHY.md")); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("file was written after directory cancellation: %v", statErr)
		}
	})

	t.Run("after atomic replacement", func(t *testing.T) {
		target := gitRepo(t)
		plan := readyPlan(t, testOptions(target), ConflictFail)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		realAtomicWrite := atomicWrite
		defer func() { atomicWrite = realAtomicWrite }()
		writes := 0
		atomicWrite = func(destination string, content []byte, mode os.FileMode) error {
			if err := realAtomicWrite(destination, content, mode); err != nil {
				return err
			}
			writes++
			if writes == 1 {
				cancel()
			}
			return nil
		}

		_, err := ApplyPlanContext(ctx, plan)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("apply error = %v; want context cancellation", err)
		}
		if writes != 1 {
			t.Fatalf("writes = %d; want cancellation after first replacement", writes)
		}
		for _, relativePath := range []string{"PHILOSOPHY.md", "CONVENTIONS.md", ".agent-framework"} {
			if _, statErr := os.Stat(diskPath(target, relativePath)); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("%s remains after cancellation rollback: %v", relativePath, statErr)
			}
		}
	})
}

func TestFailedMkdirDoesNotRollbackConcurrentDirectory(t *testing.T) {
	target := gitRepo(t)
	plan := readyPlan(t, testOptions(target), ConflictFail)
	concurrentDirectory := filepath.Join(target, ".agent-framework")
	realMakeDirectory := makeDirectory
	defer func() { makeDirectory = realMakeDirectory }()
	injected := false
	makeDirectory = func(destination string, mode os.FileMode) error {
		if destination == concurrentDirectory && !injected {
			injected = true
			if err := realMakeDirectory(destination, mode); err != nil {
				return err
			}
		}
		return realMakeDirectory(destination, mode)
	}
	_, err := ApplyPlan(plan)
	if err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("apply error = %v", err)
	}
	info, statErr := os.Stat(concurrentDirectory)
	if statErr != nil || !info.IsDir() {
		t.Fatalf("concurrent directory was removed: %v", statErr)
	}
}

func TestNonemptyCreatedDirectoryReportsIncompleteRollback(t *testing.T) {
	target := gitRepo(t)
	plan := readyPlan(t, testOptions(target), ConflictFail)
	foreignFile := filepath.Join(target, "work/projects/foreign.txt")
	realAtomicWrite := atomicWrite
	defer func() { atomicWrite = realAtomicWrite }()
	calls := 0
	atomicWrite = func(destination string, content []byte, mode os.FileMode) error {
		calls++
		if calls == 1 {
			if err := realAtomicWrite(destination, content, mode); err != nil {
				return err
			}
			return os.WriteFile(foreignFile, []byte("created concurrently\n"), 0o644)
		}
		return errors.New("injected failure")
	}
	_, err := ApplyPlan(plan)
	if err == nil || !strings.Contains(err.Error(), "rollback was incomplete") {
		t.Fatalf("apply error = %v", err)
	}
	if string(readFile(t, foreignFile)) != "created concurrently\n" {
		t.Fatal("foreign file was removed")
	}
	if _, err := os.Stat(filepath.Join(target, "PHILOSOPHY.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("written file remains after rollback")
	}
}

func TestApplyRefusesChangesAfterReviewAndRevalidatesEachFile(t *testing.T) {
	t.Run("appeared before apply", func(t *testing.T) {
		target := gitRepo(t)
		plan := readyPlan(t, testOptions(target), ConflictFail)
		appeared := filepath.Join(target, "PHILOSOPHY.md")
		writeFile(t, appeared, "created after review\n")
		_, err := ApplyPlan(plan)
		if err == nil || !strings.Contains(err.Error(), "appeared after review") {
			t.Fatalf("apply error = %v", err)
		}
		if string(readFile(t, appeared)) != "created after review\n" {
			t.Fatal("appeared file was modified")
		}
	})
	t.Run("appeared mid apply", func(t *testing.T) {
		target := gitRepo(t)
		plan := readyPlan(t, testOptions(target), ConflictFail)
		realAtomicWrite := atomicWrite
		defer func() { atomicWrite = realAtomicWrite }()
		calls := 0
		atomicWrite = func(destination string, content []byte, mode os.FileMode) error {
			if err := realAtomicWrite(destination, content, mode); err != nil {
				return err
			}
			calls++
			if calls == 1 {
				return os.WriteFile(filepath.Join(target, "CONVENTIONS.md"), []byte("appeared mid-apply\n"), 0o644)
			}
			return nil
		}
		_, err := ApplyPlan(plan)
		if err == nil || !strings.Contains(err.Error(), "appeared after review") {
			t.Fatalf("apply error = %v", err)
		}
		if _, err := os.Stat(filepath.Join(target, "PHILOSOPHY.md")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("first write was not rolled back")
		}
		if string(readFile(t, filepath.Join(target, "CONVENTIONS.md"))) != "appeared mid-apply\n" {
			t.Fatal("concurrent file was modified")
		}
	})
}

func TestAtomicOverwritePreservesModeAndRollbackRestoresBytes(t *testing.T) {
	target := gitRepo(t)
	philosophyPath := filepath.Join(target, "PHILOSOPHY.md")
	writeFile(t, philosophyPath, "custom\n")
	if err := os.Chmod(philosophyPath, 0o751); err != nil {
		t.Fatal(err)
	}
	options := testOptions(target)
	options.CreateWorkspace = false
	options.InstallSkills = false
	plan := readyPlan(t, options, ConflictOverwrite)
	realAtomicWrite := atomicWrite
	defer func() { atomicWrite = realAtomicWrite }()
	calls := 0
	atomicWrite = func(destination string, content []byte, mode os.FileMode) error {
		calls++
		if calls == 2 {
			return errors.New("injected failure")
		}
		return realAtomicWrite(destination, content, mode)
	}
	if _, err := ApplyPlan(plan); err == nil {
		t.Fatal("apply unexpectedly succeeded")
	}
	if string(readFile(t, philosophyPath)) != "custom\n" {
		t.Fatal("rollback did not restore original bytes")
	}
	info, err := os.Stat(philosophyPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o751 {
		t.Fatalf("restored mode = %o", info.Mode().Perm())
	}
}

func TestJSONShapesAndManifestSourceCommit(t *testing.T) {
	target := gitRepo(t)
	oldCommit := agentframework.SourceCommit
	agentframework.SourceCommit = "build-commit"
	defer func() { agentframework.SourceCommit = oldCommit }()
	t.Setenv("AGENT_FRAMEWORK_SOURCE_COMMIT", "env-commit")
	options := testOptions(target)
	options.CreateWorkspace = false
	options.InstallSkills = false
	plan := readyPlan(t, options, ConflictFail)
	encodedPlan, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var planPayload map[string]any
	if err := json.Unmarshal(encodedPlan, &planPayload); err != nil {
		t.Fatal(err)
	}
	changes := planPayload["changes"].([]any)
	first := changes[0].(map[string]any)
	if _, ok := first["label"]; !ok {
		t.Fatal("file JSON omitted label")
	}
	if reason, ok := first["reason"]; !ok || reason != "" {
		t.Fatalf("file JSON reason = %#v, present=%v", reason, ok)
	}
	for _, raw := range changes {
		change := raw.(map[string]any)
		if change["kind"] == "directory" {
			if _, ok := change["label"]; ok {
				t.Fatal("directory JSON unexpectedly has label")
			}
		}
	}
	result, err := ApplyPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	encodedResult, err := json.Marshal(result)
	if err != nil || !bytes.Contains(encodedResult, []byte(`"created_directories"`)) {
		t.Fatalf("result JSON = %s, %v", encodedResult, err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(readFile(t, filepath.Join(target, ManifestPath)), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest["source_commit"] != "env-commit" {
		t.Fatalf("source_commit = %#v", manifest["source_commit"])
	}
}

func TestManifestBytesMatchPythonJSONEncodingAndCommitFallback(t *testing.T) {
	oldCommit := agentframework.SourceCommit
	defer func() { agentframework.SourceCommit = oldCommit }()
	oldEnvironmentCommit, environmentCommitWasSet := os.LookupEnv("AGENT_FRAMEWORK_SOURCE_COMMIT")
	defer func() {
		if environmentCommitWasSet {
			_ = os.Setenv("AGENT_FRAMEWORK_SOURCE_COMMIT", oldEnvironmentCommit)
		} else {
			_ = os.Unsetenv("AGENT_FRAMEWORK_SOURCE_COMMIT")
		}
	}()
	if err := os.Unsetenv("AGENT_FRAMEWORK_SOURCE_COMMIT"); err != nil {
		t.Fatal(err)
	}
	agentframework.SourceCommit = "build-commit"
	plan := &InstallPlan{Options: InstallOptions{
		Principal:       "Fauzaan & <Co> é 😀 \\",
		Naming:          NamingHellenic,
		InstallSkills:   true,
		CreateWorkspace: false,
		IntegrateClaude: true,
	}}
	content, err := manifestBytes(plan, map[string]string{
		"z.md": "222",
		"a.md": "111",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n" +
		"  \"components\": {\n" +
		"    \"claude\": true,\n" +
		"    \"skills\": true,\n" +
		"    \"workspace\": false\n" +
		"  },\n" +
		"  \"files\": {\n" +
		"    \"a.md\": \"111\",\n" +
		"    \"z.md\": \"222\"\n" +
		"  },\n" +
		"  \"framework_version\": \"" + agentframework.Version + "\",\n" +
		"  \"naming\": \"hellenic\",\n" +
		"  \"principal\": \"Fauzaan & <Co> \\u00e9 \\ud83d\\ude00 \\\\\",\n" +
		"  \"schema\": 1,\n" +
		"  \"source_commit\": \"build-commit\"\n" +
		"}\n"
	if string(content) != want {
		t.Fatalf("manifest differs\n--- got ---\n%s--- want ---\n%s", content, want)
	}

	if err := os.Setenv("AGENT_FRAMEWORK_SOURCE_COMMIT", ""); err != nil {
		t.Fatal(err)
	}
	content, err = manifestBytes(plan, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(content, []byte("  \"source_commit\": null\n")) {
		t.Fatalf("an explicitly empty environment commit did not serialize as null:\n%s", content)
	}

	if err := os.Unsetenv("AGENT_FRAMEWORK_SOURCE_COMMIT"); err != nil {
		t.Fatal(err)
	}
	agentframework.SourceCommit = ""
	content, err = manifestBytes(plan, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(content, []byte("  \"files\": {},\n")) ||
		!bytes.Contains(content, []byte("  \"source_commit\": null\n")) {
		t.Fatalf("empty manifest fields differ:\n%s", content)
	}
}

func TestGitWorktreeAndNestedDirectoryAreRecognized(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatal(err)
	}
	commands := [][]string{
		{"git", "init", "-q", source},
		{"git", "-C", source, "config", "user.email", "test@example.com"},
		{"git", "-C", source, "config", "user.name", "Test"},
	}
	for _, arguments := range commands {
		if output, err := exec.Command(arguments[0], arguments[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v: %s", arguments, err, output)
		}
	}
	writeFile(t, filepath.Join(source, "README.md"), "test\n")
	for _, arguments := range [][]string{
		{"git", "-C", source, "add", "README.md"},
		{"git", "-C", source, "commit", "-qm", "initial"},
	} {
		if output, err := exec.Command(arguments[0], arguments[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v: %s", arguments, err, output)
		}
	}
	worktree := filepath.Join(root, "worktree")
	if output, err := exec.Command(
		"git", "-C", source, "worktree", "add", "-q", "-b", "test-worktree", worktree,
	).CombinedOutput(); err != nil {
		t.Fatalf("git worktree: %v: %s", err, output)
	}
	nested := filepath.Join(worktree, "nested/path")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if !IsGitRepository(worktree) || !IsGitRepository(nested) {
		t.Fatalf("worktree=%v nested=%v", IsGitRepository(worktree), IsGitRepository(nested))
	}
}

func TestErrorCategoriesDoNotPolluteUserFacingText(t *testing.T) {
	_, err := BuildPlan(InstallOptions{Target: filepath.Join(t.TempDir(), "missing"), Naming: NamingRoman})
	if !errors.Is(err, ErrInstaller) || !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error category = %v", err)
	}
	if strings.HasPrefix(err.Error(), ErrInvalidInput.Error()) {
		t.Fatalf("user-facing error has category prefix: %s", err)
	}
	if !strings.Contains(err.Error(), "Target directory does not exist") {
		t.Fatalf("unexpected error: %s", err)
	}
}

func ExampleInstallPlan_Output() {
	plan := &InstallPlan{
		Options: InstallOptions{Target: "/repo", Principal: "Fauzaan", Naming: NamingRoman},
		Files: []*FileChange{{
			RelativePath: "PHILOSOPHY.md",
			Status:       ChangeAdd,
			Label:        "Framework philosophy",
		}},
	}
	payload, _ := json.Marshal(plan.Output())
	fmt.Println(string(payload))
	// Output: {"schema":1,"target":"/repo","git_repository":false,"options":{"principal":"Fauzaan","naming":"roman","skills":false,"workspace":false,"claude":false},"changes":[{"path":"PHILOSOPHY.md","action":"add","label":"Framework philosophy","reason":""}],"counts":{"add":1,"blocked":0,"conflict":0,"directories":0,"keep":0,"unchanged":0,"update":0},"notes":[],"ready":true}
}
