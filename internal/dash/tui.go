package dash

import (
	"context"
	"io"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// refreshEvery is how often the live dashboard re-reads the workspace. A scan
// is a few dozen small file reads plus one `herdr agent list`.
const refreshEvery = 5 * time.Second

// ScanFunc reads the workspace; all includes retired agents.
type ScanFunc func(all bool) (*Workspace, error)

// Run starts the interactive dashboard and blocks until the user quits.
func Run(ctx context.Context, scan ScanFunc, in io.Reader, out io.Writer, noColor, all bool) error {
	m := &model{scan: scan, st: NewStyles(noColor), all: all, width: 120, height: 40,
		quitAfterLaunch: os.Getenv("HERDR_PLUGIN_ENTRYPOINT_ID") == "peek"}
	m.ws, m.err = scan(all)
	p := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(in), tea.WithOutput(out))
	_, err := p.Run()
	if err == tea.ErrProgramKilled && ctx.Err() != nil {
		return nil
	}
	return err
}

type scannedMsg struct {
	ws  *Workspace
	err error
}

type tickMsg struct{}

type launchedMsg struct {
	summary string
	err     error
}

type model struct {
	scan   ScanFunc
	st     Styles
	ws     *Workspace
	err    error
	all    bool
	cursor int
	// full shows the selected agent's detail on its own screen.
	full   bool
	scroll int
	width  int
	height int
	// status is a one-line message shown in the footer, e.g. a launch result.
	status string
	// quitAfterLaunch closes the dashboard once an agent opens: set when it
	// runs as the Herdr quick-look overlay, so you land in the agent.
	quitAfterLaunch bool
}

func (m *model) Init() tea.Cmd { return tick() }

func tick() tea.Cmd {
	return tea.Tick(refreshEvery, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *model) rescan() tea.Cmd {
	scan, all := m.scan, m.all
	return func() tea.Msg {
		ws, err := scan(all)
		return scannedMsg{ws, err}
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		return m, tea.Batch(m.rescan(), tick())
	case scannedMsg:
		m.err = msg.err
		if msg.err == nil {
			m.keepSelection(msg.ws)
		}
	case launchedMsg:
		if msg.err != nil {
			m.status = "✗ " + msg.err.Error()
			return m, nil
		}
		m.status = msg.summary
		if m.quitAfterLaunch {
			return m, tea.Quit
		}
		return m, m.rescan()
	case tea.KeyPressMsg:
		m.status = ""
		return m, m.key(msg.String())
	}
	return m, nil
}

// keepSelection swaps in a new scan, keeping the cursor on the same agent.
func (m *model) keepSelection(ws *Workspace) {
	name := ""
	if a := m.selected(); a != nil {
		name = a.Name
	}
	m.ws = ws
	m.cursor = 0
	for i, a := range ws.Agents {
		if a.Name == name {
			m.cursor = i
		}
	}
}

func (m *model) selected() *Agent {
	if m.ws == nil || m.cursor < 0 || m.cursor >= len(m.ws.Agents) {
		return nil
	}
	return m.ws.Agents[m.cursor]
}

func (m *model) key(k string) tea.Cmd {
	n := 0
	if m.ws != nil {
		n = len(m.ws.Agents)
	}
	switch k {
	case "q", "ctrl+c":
		return tea.Quit
	case "esc":
		if m.full {
			m.full, m.scroll = false, 0
			return nil
		}
		return tea.Quit
	case "up", "k":
		if m.full {
			m.scroll = max(0, m.scroll-1)
		} else if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.full {
			m.scroll++
		} else if m.cursor < n-1 {
			m.cursor++
		}
	case "home", "g":
		m.cursor, m.scroll = 0, 0
	case "end", "G":
		m.cursor = max(0, n-1)
	case "enter", "o":
		return m.launch()
	case "space", "l", "right":
		m.full, m.scroll = !m.full, 0
	case "h", "left":
		m.full, m.scroll = false, 0
	case "r":
		return m.rescan()
	case "a":
		m.all = !m.all
		return m.rescan()
	}
	return nil
}

// launch opens the selected agent in Herdr off the UI goroutine; starting an
// agent waits for its CLI to be ready.
func (m *model) launch() tea.Cmd {
	a := m.selected()
	if a == nil {
		return nil
	}
	m.status = "opening " + a.Name + "…"
	ws := m.ws
	return func() tea.Msg {
		summary, err := Launch(ws, a)
		return launchedMsg{summary, err}
	}
}

func (m *model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "GoAgentic Dashboard"
	return v
}

func (m *model) render() string {
	w, h := max(m.width, 40), max(m.height, 10)
	if m.ws == nil {
		msg := "scanning…"
		if m.err != nil {
			msg = "error: " + m.err.Error()
		}
		return msg
	}
	footer := m.st.Dim.Render("↑↓ select · enter open · → detail · r refresh · a retired · q quit")
	if m.full {
		footer = m.st.Dim.Render("↑↓ scroll · enter open · esc back · q quit")
	}
	if m.status != "" {
		style := m.st.Accent
		if strings.HasPrefix(m.status, "✗") {
			style = m.st.Fail
		}
		footer = style.Render(m.status) + "  " + footer
	}
	if m.err != nil {
		footer = m.st.Fail.Render("refresh failed: "+m.err.Error()) + "  " + footer
	}

	var top []string
	top = append(top, ansi.Truncate(Header(m.ws, m.st), w, "…"))

	if a := m.selected(); m.full && a != nil {
		body := Detail(m.ws, a, w, 0, m.st)
		room := h - len(top) - 2
		m.scroll = min(m.scroll, max(0, len(body)-room))
		body = body[m.scroll:]
		if len(body) > room {
			body = body[:room]
		}
		return compose(top, body, footer, h)
	}

	if waiting := WaitingCompact(m.ws); waiting != "" {
		label := m.st.Warn.Render("Waiting on " + firstName(m.ws.Principal) + ": ")
		top = append(top, ansi.Truncate(label+waiting, w, "…"))
	}
	top = append(top, "")

	var body []string
	if len(m.ws.Agents) == 0 {
		body = append(body, "No agents in this workspace.")
		return compose(top, body, footer, h)
	}
	body = append(body, TableHeader(m.ws, w, m.st))
	for i, a := range m.ws.Agents {
		body = append(body, TableRow(m.ws, a, w, i == m.cursor, m.st))
	}

	// The selected agent's detail fills whatever room the table leaves.
	room := h - len(top) - len(body) - 3
	if a := m.selected(); a != nil && room > 3 {
		body = append(body, m.st.Dim.Render(strings.Repeat("─", w)))
		d := Detail(m.ws, a, w, 12, m.st)
		if len(d) > room {
			d = append(d[:room-1], m.st.Dim.Render("… → for more"))
		}
		body = append(body, d...)
	}
	return compose(top, body, footer, h)
}

// compose stacks header, body and a bottom-pinned footer into h lines.
func compose(top, body []string, footer string, h int) string {
	lines := append(append([]string{}, top...), body...)
	if len(lines) > h-1 {
		lines = lines[:h-1]
	}
	for len(lines) < h-1 {
		lines = append(lines, "")
	}
	return strings.Join(append(lines, footer), "\n")
}
