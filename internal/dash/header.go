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
	toneWaiting
	toneBlocked
)

func (st Styles) tone(t tone) lipgloss.Style {
	switch t {
	case toneBlocked:
		return st.Fail
	case toneWaiting:
		return st.Wait
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

// agentTone picks the attention colour: a blocked item (red) beats a P1
// waiting on someone else (orange), which beats something waiting on the
// principal (amber), which beats the agent's run state. Overdue items and
// health have their own counts and column, so red keeps one meaning.
func agentTone(ws *Workspace, a *Agent) tone {
	switch {
	case a.BlockedCount() > 0:
		return toneBlocked
	case a.WaitingCount(ws.Principal) > 0:
		return toneWaiting
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
	Agents  int `json:"agents"`
	Live    int `json:"live"`
	ForYou  int `json:"for_you"`
	Overdue int `json:"overdue"`
	Waiting int `json:"waiting"`
	Blocked int `json:"blocked"`
	Health  int `json:"health"`
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
		c.Waiting += a.WaitingCount(ws.Principal)
		c.Blocked += a.BlockedCount()
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
		count(c.Waiting, st.Wait, "◆", "P1 waiting on others"),
		count(c.Blocked, st.Fail, "■", "blocked"),
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
