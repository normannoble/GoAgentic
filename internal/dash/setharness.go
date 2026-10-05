package dash

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SetHarness makes harness the agent's default by editing the harness: line
// in the frontmatter of its context.md, the one file and the one line the
// dashboard ever writes. When the harness is what the agent would get from
// the workspace anyway, the agent's own line is removed instead. The agent's
// own model: line is removed too when the harness changes, since a model
// name for one CLI means nothing to another. It returns a one-line summary.
func SetHarness(ws *Workspace, a *Agent, harness string) (string, error) {
	if !knownHarness(harness) {
		return "", fmt.Errorf("unknown harness %q", harness)
	}
	path := filepath.Join(a.Dir, "context.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", fmt.Errorf("%s has no frontmatter", path)
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return "", fmt.Errorf("%s: frontmatter is not closed", path)
	}

	inherited, _ := HarnessSource(ws, &Agent{})
	keep := harness != inherited // the agent needs its own line
	changing := harness != harnessOf(ws, a)

	var front []string
	removedModel := ""
	for _, line := range lines[1:end] {
		key, value, _ := strings.Cut(line, ":")
		switch {
		case strings.HasPrefix(line, " "):
		case strings.TrimSpace(key) == "harness":
			continue
		case strings.TrimSpace(key) == "model" && changing:
			removedModel = strings.Trim(strings.TrimSpace(value), `"'`)
			continue
		}
		front = append(front, line)
	}
	if keep {
		front = append(front, "harness: "+harness)
	}
	out := append(append(append([]string{lines[0]}, front...), lines[end]), lines[end+1:]...)
	if err := os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644); err != nil {
		return "", err
	}

	summary := a.Name + " → " + harness
	if !keep {
		summary += " (workspace default)"
	}
	if removedModel != "" {
		summary += "; its model " + removedModel + " was removed"
	}
	return summary, nil
}
