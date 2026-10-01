package dash

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The count header and the tone that colours an agent's attention dot.

// tone is an agent's overall state, from calm to urgent.
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

// agentTone picks the attention colour: a problem beats something waiting on
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

// Counts are the totals the count header shows.
type Counts struct {
	Agents, Live, ForYou, Overdue, Blocked, Health int
}

// Counts totals the workspace for the count header and the fleet view.
func (ws *Workspace) Counts() Counts {
	c := Counts{Agents: len(ws.Agents)}
	for _, a := range ws.Agents {
		if a.Live != nil && a.Live.Running {
			c.Live++
		}
		c.ForYou += len(a.ForPrincipal(ws.Principal)) + a.InboxUnprocessed
		c.Overdue += a.OverdueCount(ws.Scanned)
		c.Blocked += a.BlockedCount(ws.Principal)
		if a.Health.Level > OK {
			c.Health++
		}
	}
	return c
}

// CountHeader is the one-line summary of the whole Space:
// coloured counts, dimmed when zero.
func CountHeader(ws *Workspace, width int, st Styles) string {
	return countLine(strings.ToUpper(ws.Name), ws.Counts(), ws.Live, ws.Scanned, width, st)
}

func countLine(title string, c Counts, live bool, at time.Time, width int, st Styles) string {
	count := func(n int, style lipgloss.Style, glyph, label string) string {
		s := fmt.Sprintf("%s %d %s", glyph, n, label)
		if n == 0 {
			return st.Dim.Render(s)
		}
		return style.Render(s)
	}
	parts := []string{st.Title.Render(title)}
	if live {
		parts = append(parts, count(c.Live, st.OK, "●", "live"))
	}
	parts = append(parts,
		count(c.ForYou, st.Warn, "✋", "for you"),
		count(c.Overdue, st.Fail, "⚑", "overdue"),
		count(c.Blocked, st.Fail, "■", "P1 blocked"),
		count(c.Health, lipgloss.NewStyle(), "⚠", "health"),
	)
	left := strings.Join(parts, "   ")
	right := st.Dim.Render(at.Format("15:04"))
	pad := width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if pad < 2 {
		return ansi.Truncate(left, width, "…")
	}
	return left + strings.Repeat(" ", pad) + right
}
