package dash

import (
	"context"
	"io"

	tea "charm.land/bubbletea/v2"
)

// FleetScanFunc reads every workspace; all includes retired agents.
type FleetScanFunc func(all bool) (*Fleet, error)

// RunFleet starts the interactive fleet view. Enter opens a workspace's own
// dashboard in place; esc comes back to the fleet.
func RunFleet(ctx context.Context, scan FleetScanFunc, wsScan func(root string) ScanFunc, in io.Reader, out io.Writer, noColor, all bool) error {
	m := &fleetModel{scan: scan, wsScan: wsScan, st: NewStyles(noColor), all: all, width: 120, height: 40}
	m.f, m.err = scan(all)
	go shellPath() // warm the harness menu's CLI lookup
	p := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(in), tea.WithOutput(out))
	_, err := p.Run()
	if err == tea.ErrProgramKilled && ctx.Err() != nil {
		return nil
	}
	return err
}

type fleetScannedMsg struct {
	f   *Fleet
	err error
}

type fleetModel struct {
	scan   FleetScanFunc
	wsScan func(root string) ScanFunc
	st     Styles
	f      *Fleet
	err    error
	all    bool
	cursor int
	width  int
	height int
	// inner is the open workspace's dashboard, nil on the fleet table.
	inner *model
}

func (m *fleetModel) Init() tea.Cmd { return tick() }

func (m *fleetModel) rescan() tea.Cmd {
	scan, all := m.scan, m.all
	return func() tea.Msg {
		f, err := scan(all)
		return fleetScannedMsg{f, err}
	}
}

func (m *fleetModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.inner != nil {
			m.inner.width, m.inner.height = msg.Width, msg.Height
		}
		return m, nil
	case fleetScannedMsg:
		m.err = msg.err
		if msg.err == nil {
			m.keepSelection(msg.f)
		}
		return m, nil
	case tickMsg:
		if m.inner != nil {
			_, cmd := m.inner.Update(msg)
			return m, cmd
		}
		return m, tea.Batch(m.rescan(), tick())
	case tea.KeyPressMsg:
		return m, m.key(msg.String())
	}
	if m.inner != nil {
		_, cmd := m.inner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *fleetModel) keepSelection(f *Fleet) {
	name := ""
	if ws := m.selected(); ws != nil {
		name = ws.Name
	}
	m.f, m.cursor = f, 0
	for i, ws := range f.Workspaces {
		if ws.Name == name {
			m.cursor = i
		}
	}
}

func (m *fleetModel) selected() *Workspace {
	if m.f == nil || m.cursor < 0 || m.cursor >= len(m.f.Workspaces) {
		return nil
	}
	return m.f.Workspaces[m.cursor]
}

func (m *fleetModel) key(k string) tea.Cmd {
	if k == "ctrl+c" {
		return tea.Quit
	}
	if m.inner != nil {
		if !m.inner.full && (k == "esc" || k == "q") {
			m.inner = nil
			return m.rescan()
		}
		m.inner.status = ""
		return m.inner.key(k)
	}
	n := 0
	if m.f != nil {
		n = len(m.f.Workspaces)
	}
	switch k {
	case "q", "esc":
		return tea.Quit
	case "up", "k":
		m.cursor = max(0, m.cursor-1)
	case "down", "j":
		m.cursor = min(max(n-1, 0), m.cursor+1)
	case "home", "g":
		m.cursor = 0
	case "end", "G":
		m.cursor = max(0, n-1)
	case "enter", "o", "right", "l":
		ws := m.selected()
		if ws == nil {
			return nil
		}
		m.inner = &model{scan: m.wsScan(ws.Root), st: m.st, ws: ws, all: m.all,
			width: m.width, height: m.height, inFleet: true}
		return m.inner.rescan()
	case "r":
		return m.rescan()
	case "a":
		m.all = !m.all
		return m.rescan()
	}
	return nil
}

func (m *fleetModel) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "GoAgentic Fleet"
	return v
}

func (m *fleetModel) render() string {
	if m.inner != nil {
		return m.inner.render()
	}
	w, h := max(m.width, 40), max(m.height, 10)
	if m.f == nil {
		msg := "scanning…"
		if m.err != nil {
			msg = "error: " + m.err.Error()
		}
		return msg
	}
	footer := m.st.Dim.Render("↑↓ select · enter open workspace · r refresh · q quit")
	if m.err != nil {
		footer = m.st.Fail.Render("refresh failed: "+m.err.Error()) + "  " + footer
	}
	top := []string{FleetHeader(m.f, w, m.st), ""}
	body := []string{FleetTableHeader(w, m.st)}
	for i, ws := range m.f.Workspaces {
		body = append(body, FleetRow(ws, w, i == m.cursor, m.st))
	}
	body = append(body, "", "    "+ColourLegend(m.st))
	room := h - len(top) - 2
	if len(body) > room {
		body = body[:room]
	}
	return compose(top, body, footer, h)
}
