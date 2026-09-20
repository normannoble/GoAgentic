package ui

import (
	"io"
	"strings"

	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"
	"github.com/normannoble/GoAgentic/internal/installer"
	"golang.org/x/term"
)

const (
	defaultPlanTableWidth = 90
	minimumPlanTableWidth = 40
)

func planTable(plan *installer.InstallPlan, noColor bool, width int) string {
	width = max(minimumPlanTableWidth, min(width, defaultPlanTableWidth))
	rows := planTableRows(plan, noColor)
	styles := planTableStyles(noColor)
	columns := planTableColumns(width)
	model := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithHeight(max(2, len(rows)+1)),
		table.WithWidth(width),
		table.WithFocused(false),
		table.WithStyles(styles),
	)

	lines := strings.Split(model.View(), "\n")
	for index := range lines {
		// Bubbles pads every row to the configured width. Removing only the
		// right edge prevents invisible padding from wrapping into blank rows.
		lines[index] = strings.TrimRight(lines[index], " \t")
	}
	return strings.Join(lines, "\n")
}

func planTableOutputWidth(out io.Writer) int {
	writer, ok := out.(interface{ Fd() uintptr })
	if !ok || !term.IsTerminal(int(writer.Fd())) {
		return defaultPlanTableWidth
	}
	width, _, err := term.GetSize(int(writer.Fd()))
	if err != nil {
		return defaultPlanTableWidth
	}
	return max(minimumPlanTableWidth, width-2)
}

func planTableColumns(width int) []table.Column {
	const (
		actionWidth      = 11
		cellSpacingWidth = 6
	)
	remaining := max(2, width-actionWidth-cellSpacingWidth)
	pathWidth := max(1, remaining*58/100)
	detailsWidth := max(1, remaining-pathWidth)
	return []table.Column{
		{Title: "Action", Width: actionWidth},
		{Title: "Path", Width: pathWidth},
		{Title: "Details", Width: detailsWidth},
	}
}

func planTableRows(plan *installer.InstallPlan, noColor bool) []table.Row {
	rows := make([]table.Row, 0, len(plan.Files)+len(plan.Directories))
	for _, change := range plan.Files {
		rows = append(rows, table.Row{
			planActionStyle(change.Status, noColor).Render(statusLabel(change.Status)),
			change.RelativePath,
			change.Label,
		})
	}
	for _, directory := range plan.Directories {
		if directory.Create {
			rows = append(rows, table.Row{
				planActionStyle(installer.ChangeAdd, noColor).Render("Add"),
				directory.RelativePath + "/",
				"Directory",
			})
		}
	}
	return rows
}

func planTableStyles(noColor bool) table.Styles {
	cell := lipgloss.NewStyle().PaddingRight(2)
	header := themedStyle(noColor, dimColor, true).PaddingRight(2)
	if noColor {
		header = cell
	}
	return table.Styles{
		Header:   header,
		Cell:     cell,
		Selected: lipgloss.NewStyle(),
	}
}

func planActionStyle(status installer.ChangeStatus, noColor bool) lipgloss.Style {
	if noColor {
		return lipgloss.NewStyle()
	}
	switch status {
	case installer.ChangeAdd:
		return themedStyle(noColor, answerColor, true)
	case installer.ChangeUpdate:
		return themedStyle(noColor, focusColor, true)
	case installer.ChangeUnchanged, installer.ChangeKeep:
		return themedStyle(noColor, dimColor, false)
	case installer.ChangeConflict, installer.ChangeBlocked:
		return themedStyle(noColor, errorColor, true)
	default:
		return lipgloss.NewStyle()
	}
}
