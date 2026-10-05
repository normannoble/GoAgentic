package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Wrapper frontmatter must be valid YAML. Some skill descriptions contain
// ": ", which breaks an unquoted value (Pi reported "Nested mappings are not
// allowed"), so every description is written as a double-quoted string.
func TestHarnessWrapperDescriptionsAreQuoted(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	root := repositoryRoot(t)
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, harness := range []string{"pi", "opencode", "codex"} {
		command := exec.Command(bash, filepath.Join(root, "harness", "install.sh"), harness, workspace)
		command.Env = append(os.Environ(), "HOME="+t.TempDir())
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("install %s: %v\n%s", harness, err, output)
		}
	}

	quoted := regexp.MustCompile(`^description: "(?:[^"\\]|\\.)*"$`)
	var files []string
	for _, pattern := range []string{".agents/skills/*/SKILL.md", ".pi/prompts/*.md", ".opencode/commands/*.md"} {
		matches, _ := filepath.Glob(filepath.Join(workspace, pattern))
		files = append(files, matches...)
	}
	if len(files) < 30 {
		t.Fatalf("expected wrappers for three harnesses, found %d files", len(files))
	}
	sawColon := false
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, line := range strings.Split(string(content), "\n") {
			if strings.HasPrefix(line, "description:") {
				found = true
				if !quoted.MatchString(line) {
					t.Errorf("%s: description is not a quoted YAML string: %s", file, line)
				}
				sawColon = sawColon || strings.Contains(line, ": ")
				break
			}
		}
		if !found {
			t.Errorf("%s: no description line", file)
		}
	}
	if !sawColon {
		t.Errorf("no description contains \": \"; the test no longer covers the case it was written for")
	}
}
