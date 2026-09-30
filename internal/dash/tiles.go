package dash

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The tile view: one bordered tile per agent, where colour and shape carry
// the state and the only prose is a short next-focus line.

const (
	minTileWidth = 30
	maxTileWidth = 44
	tileHeight   = 5 // top border, three content lines, bottom border
	tileGap      = 2
)

// tone is a tile's overall state, from calm to urgent.
type tone int

const (
	toneOff tone = iota
	toneIdle
	toneWorking
	toneForYou
	toneProblem
)

func (st Styles) tone(t tone) lipgloss.Style {
	switch t {
	case toneProblem:
		return st.Fail
	case toneForYou:
		return st.Warn
	case toneWorking:
		return st.OK
	case toneIdle:
		return st.Accent
	default:
		return st.Dim
	}
}

// agentTone picks the border colour: a problem beats something waiting on
// the principal, which beats the agent's run state.
func agentTone(ws *Workspace, a *Agent) tone {
	switch {
	case a.OverdueCount(ws.Scanned) > 0 || a.BlockedCount(ws.Principal) > 0 || a.Health.Level == Fail:
		return toneProblem
	case len(a.ForPrincipal(ws.Principal)) > 0 || a.InboxUnprocessed > 0:
		return toneForYou
	case a.Live != nil && a.Live.Running && a.Live.Status == "working":
		return toneWorking
	case a.Live != nil && a.Live.Running:
		return toneIdle
	default:
		return toneOff
	}
}

// TileColumns is how many tiles fit across width.
func TileColumns(width int) int {
	cols := (width + tileGap) / (minTileWidth + tileGap)
	if cols < 1 {
		cols = 1
	}
	return cols
}

func tileWidth(width, cols int) int {
	w := (width - (cols-1)*tileGap) / cols
	if w > maxTileWidth {
		w = maxTileWidth
	}
	if w < 20 {
		w = 20
	}
	return w
}

// CountHeader is the one-line summary of the whole Space, shared by both
// views: coloured counts, dimmed when zero.
func CountHeader(ws *Workspace, width int, st Styles) string {
	var live, forYou, overdue, blocked, health int
	for _, a := range ws.Agents {
		if a.Live != nil && a.Live.Running {
			live++
		}
		forYou += len(a.ForPrincipal(ws.Principal)) + a.InboxUnprocessed
		overdue += a.OverdueCount(ws.Scanned)
		blocked += a.BlockedCount(ws.Principal)
		if a.Health.Level > OK {
			health++
		}
	}
	count := func(n int, style lipgloss.Style, glyph, label string) string {
		s := fmt.Sprintf("%s %d %s", glyph, n, label)
		if n == 0 {
			return st.Dim.Render(s)
		}
		return style.Render(s)
	}
	parts := []string{st.Title.Render(strings.ToUpper(ws.Name))}
	if ws.Live {
		parts = append(parts, count(live, st.OK, "●", "live"))
	}
	parts = append(parts,
		count(forYou, st.Warn, "✋", "for you"),
		count(overdue, st.Fail, "⚑", "overdue"),
		count(blocked, st.Fail, "■", "P1 blocked"),
		count(health, lipgloss.NewStyle(), "⚠", "health"),
	)
	left := strings.Join(parts, "   ")
	right := st.Dim.Render(ws.Scanned.Format("15:04"))
	pad := width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if pad < 2 {
		return ansi.Truncate(left, width, "…")
	}
	return left + strings.Repeat(" ", pad) + right
}

// Tile renders one agent as tileHeight lines of exactly w cells.
func Tile(ws *Workspace, a *Agent, w int, selected bool, st Styles) []string {
	border := st.tone(agentTone(ws, a))
	h, v := "─", "│"
	tl, tr, bl, br := "╭", "╮", "╰", "╯"
	if selected {
		h, v = "━", "┃"
		tl, tr, bl, br = "┏", "┓", "┗", "┛"
		border = border.Bold(true)
	}
	inner := w - 4 // borders plus one space of padding each side

	// Top edge: ╭─ Name ───── badges ─╮
	name := a.Name
	nameStyle := st.Head
	if selected {
		nameStyle = st.Title
	}
	badges := tileBadges(ws, a, st)
	fill := w - 2 - (3 + ansi.StringWidth(name) + 1) - (ansi.StringWidth(badges) + 2)
	if badges == "" {
		fill += 2
	}
	if fill < 1 {
		badges, fill = "", w-2-(3+ansi.StringWidth(name)+1)
		if fill < 1 {
			name = ansi.Truncate(name, w-6, "…")
			fill = w - 2 - (3 + ansi.StringWidth(name) + 1)
		}
	}
	top := border.Render(tl+h) + " " + nameStyle.Render(name) + " " + border.Render(strings.Repeat(h, max(fill, 0)))
	if badges != "" {
		top += " " + badges + " "
	}
	top += border.Render(h + tr)

	// Line 1: run state and age on the left, activity sparkline on the right.
	state := tileState(ws, a, st)
	spark := sparkline(a.Activity, st)
	gap := inner - ansi.StringWidth(state) - ansi.StringWidth(spark)
	line1 := state
	if gap >= 2 {
		line1 += strings.Repeat(" ", gap) + spark
	}

	// Line 2: the next focus, cut to fit.
	line2 := tileNext(ws, a)

	// Line 3: workload by priority.
	line3 := workloadBar(a, ws.Principal, inner, st)

	row := func(s string) string {
		return border.Render(v) + " " + fit(s, inner) + " " + border.Render(v)
	}
	bottom := border.Render(bl + strings.Repeat(h, w-2) + br)
	return []string{top, row(line1), row(line2), row(line3), bottom}
}

func tileBadges(ws *Workspace, a *Agent, st Styles) string {
	var b []string
	if n := len(a.ForPrincipal(ws.Principal)) + a.InboxUnprocessed; n > 0 {
		b = append(b, st.Warn.Render(fmt.Sprintf("✋%d", n)))
	}
	if n := a.OverdueCount(ws.Scanned); n > 0 {
		b = append(b, st.Fail.Render(fmt.Sprintf("⚑%d", n)))
	}
	if n := a.BlockedCount(ws.Principal); n > 0 {
		b = append(b, st.Fail.Render(fmt.Sprintf("■%d", n)))
	}
	switch a.Health.Level {
	case Fail:
		b = append(b, st.Fail.Render("⚠"))
	case Warn:
		b = append(b, st.Warn.Render("⚠"))
	}
	return strings.Join(b, " ")
}

// tileState is "● working", "◐ idle", or "○ 4d" (time since it last ran).
func tileState(ws *Workspace, a *Agent, st Styles) string {
	if a.Live != nil && a.Live.Running {
		if a.Live.Status == "working" || a.Live.Status == "blocked" {
			return st.OK.Render("● " + a.Live.Status)
		}
		return st.Accent.Render("◐ " + a.Live.State())
	}
	last := a.LastActive
	if s, ok := a.LastSession(); ok && last.IsZero() && !s.Date.IsZero() {
		last = s.Date
	}
	if last.IsZero() {
		return st.Dim.Render("○ never")
	}
	return st.Dim.Render("○ " + humanAge(ws.Scanned.Sub(last)))
}

func tileNext(ws *Workspace, a *Agent) string {
	if a.NextSession != "" {
		return a.NextSession
	}
	if it, ok := a.NextUp(ws.Principal); ok {
		return it.Short()
	}
	if len(a.Open) == 0 {
		return "nothing open"
	}
	return "only parked items"
}

// sparkline draws one cell per day: a dim baseline for idle days, taller
// accent bars for busier ones.
func sparkline(days []int, st Styles) string {
	if len(days) == 0 {
		return ""
	}
	levels := []string{"▃", "▅", "▇"}
	var b strings.Builder
	for _, n := range days {
		if n == 0 {
			b.WriteString(st.Dim.Render("▁"))
			continue
		}
		b.WriteString(st.Accent.Render(levels[min(n, len(levels))-1]))
	}
	return b.String()
}

// workloadBar draws one cell per live, agent-owned open item — █ P1, ▓ P2,
// ░ P3 — scaled down when there are more items than room.
func workloadBar(a *Agent, principal string, width int, st Styles) string {
	counts := map[string]int{}
	total := 0
	for _, it := range a.Open {
		if it.Parked() || it.OnlyOwnedBy(principal) {
			continue
		}
		p := it.Priority
		if p != "P1" && p != "P2" {
			p = "P3"
		}
		counts[p]++
		total++
	}
	if total == 0 {
		return st.Dim.Render("·")
	}
	room := width - 6 // leave space for the count
	scale := 1.0
	if total > room {
		scale = float64(room) / float64(total)
	}
	cells := func(n int) int {
		if n == 0 {
			return 0
		}
		return max(1, int(float64(n)*scale+0.5))
	}
	bar := st.Fail.Render(strings.Repeat("█", cells(counts["P1"]))) +
		st.Warn.Render(strings.Repeat("▓", cells(counts["P2"]))) +
		st.Dim.Render(strings.Repeat("░", cells(counts["P3"])))
	return bar + " " + st.Dim.Render(fmt.Sprintf("%d", total))
}

// TileGrid lays tiles out in rows; it returns the lines and, for scrolling,
// the line index where the selected tile's row starts.
func TileGrid(ws *Workspace, width, cursor int, st Styles) (lines []string, selectedRow int) {
	cols := TileColumns(width)
	w := tileWidth(width, cols)
	for start := 0; start < len(ws.Agents); start += cols {
		end := min(start+cols, len(ws.Agents))
		if cursor >= start && cursor < end {
			selectedRow = len(lines)
		}
		rows := make([]string, tileHeight)
		for i := start; i < end; i++ {
			t := Tile(ws, ws.Agents[i], w, i == cursor, st)
			for r := range rows {
				if i > start {
					rows[r] += strings.Repeat(" ", tileGap)
				}
				rows[r] += t[r]
			}
		}
		lines = append(lines, rows...)
	}
	return lines, selectedRow
}

// TileLegend explains the marks in one dim line.
func TileLegend(st Styles) string {
	return st.Dim.Render("█ P1  ▓ P2  ░ P3 · ▁▃▅▇ sessions per day, last 12 days")
}

// writeTiles prints the tile view once, for --once.
func writeTiles(ws *Workspace, width int, st Styles) []string {
	out := []string{CountHeader(ws, width, st), ""}
	grid, _ := TileGrid(ws, width, -1, st)
	out = append(out, grid...)
	out = append(out, "", TileLegend(st))
	return out
}
