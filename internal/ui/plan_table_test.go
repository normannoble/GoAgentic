package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/normannoble/GoAgentic/internal/installer"
)

func TestPlanTableRendersEveryChangeWithoutSelectionDecoration(t *testing.T) {
	plan := sampleTablePlan()
	view := ansi.Strip(planTable(plan, true, defaultPlanTableWidth))

	for _, text := range []string{
		"Action",
		"Path",
		"Details",
		"Add",
		"PHILOSOPHY.md",
		"Update",
		"CLAUDE.md",
		"No change",
		"CONVENTIONS.md",
		"thinking/",
		"Directory",
	} {
		if !strings.Contains(view, text) {
			t.Fatalf("plan table does not contain %q:\n%s", text, view)
		}
	}
	if strings.Contains(view, "+ Add") {
		t.Fatalf("Add action has an unwanted prefix:\n%s", view)
	}
	if strings.Contains(view, "outputs/") {
		t.Fatalf("table includes a directory that will not be created:\n%s", view)
	}
}

func TestPlanTableNeverUsesRowBackgrounds(t *testing.T) {
	for _, noColor := range []bool{false, true} {
		styles := planTableStyles(noColor)
		for name, style := range map[string]lipgloss.Style{
			"header":   styles.Header,
			"cell":     styles.Cell,
			"selected": styles.Selected,
		} {
			if _, ok := style.GetBackground().(lipgloss.NoColor); !ok {
				t.Fatalf("noColor=%v %s background = %#v; want lipgloss.NoColor", noColor, name, style.GetBackground())
			}
		}

		view := planTable(sampleTablePlan(), noColor, defaultPlanTableWidth)
		if noColor && strings.Contains(view, "\x1b") {
			t.Fatalf("no-color table contains ANSI escapes: %q", view)
		}
		for _, sequence := range []string{"\x1b[48", "\x1b[7m", "\x1b[27m"} {
			if strings.Contains(view, sequence) {
				t.Fatalf("noColor=%v rendered table contains selection sequence %q: %q", noColor, sequence, view)
			}
		}
	}
}

func TestPlanTableFitsWithoutWrappingIntoBlankRows(t *testing.T) {
	const width = 72
	view := planTable(sampleTablePlan(), true, width)
	for lineNumber, line := range strings.Split(view, "\n") {
		if strings.HasSuffix(line, " ") {
			t.Fatalf("line %d has trailing padding: %q", lineNumber+1, line)
		}
		if got := ansi.StringWidth(line); got > width {
			t.Fatalf("line %d width = %d; want at most %d: %q", lineNumber+1, got, width, line)
		}
	}
}

func sampleTablePlan() *installer.InstallPlan {
	return &installer.InstallPlan{
		Files: []*installer.FileChange{
			{RelativePath: "PHILOSOPHY.md", Status: installer.ChangeAdd, Label: "Project philosophy"},
			{RelativePath: "CLAUDE.md", Status: installer.ChangeUpdate, Label: "Claude Code integration"},
			{RelativePath: "CONVENTIONS.md", Status: installer.ChangeUnchanged, Label: "Project conventions"},
		},
		Directories: []installer.DirectoryChange{
			{RelativePath: "thinking", Create: true},
			{RelativePath: "outputs", Create: false},
		},
	}
}
