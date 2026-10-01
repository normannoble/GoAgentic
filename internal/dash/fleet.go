package dash

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// Fleet is every workspace under one directory (by default ~/agents).
type Fleet struct {
	Root       string
	Workspaces []*Workspace
	Scanned    time.Time
	// Live is false when Herdr could not be queried.
	Live bool
}

// DefaultFleetRoot is ~/agents.
func DefaultFleetRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "agents")
}

// FleetDirs lists the workspaces directly under root: directories holding
// agents/CONVENTIONS.md. Symlinks are skipped, so a workspace kept under an
// old name as a link (HomeLab -> Home) is listed once.
func FleetDirs(root string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, e := range entries {
		if !e.IsDir() || e.Type()&os.ModeSymlink != 0 {
			continue
		}
		dir := filepath.Join(root, e.Name())
		for _, sub := range []string{"agents", "Agents"} {
			if fileExists(filepath.Join(dir, sub, "CONVENTIONS.md")) {
				dirs = append(dirs, dir)
				break
			}
		}
	}
	sort.Slice(dirs, func(i, j int) bool {
		return strings.ToLower(filepath.Base(dirs[i])) < strings.ToLower(filepath.Base(dirs[j]))
	})
	return dirs
}

// ScanFleet scans every workspace under root. Herdr is asked once and the
// answer shared, so a fleet scan costs one `herdr agent list`.
func ScanFleet(root string, opts Options) (*Fleet, error) {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	f := &Fleet{Root: root, Scanned: opts.Now}
	if herdr := opts.Herdr; herdr != nil {
		var once sync.Once
		var panes []LivePane
		var err error
		opts.Herdr = func() ([]LivePane, error) {
			once.Do(func() { panes, err = herdr() })
			return panes, err
		}
	}
	dirs := FleetDirs(root)
	if len(dirs) == 0 {
		return nil, fmt.Errorf("no agent workspaces under %s", root)
	}
	for _, dir := range dirs {
		ws, err := Scan(dir, opts)
		if err != nil {
			continue
		}
		f.Live = f.Live || ws.Live
		f.Workspaces = append(f.Workspaces, ws)
	}
	return f, nil
}

// Counts totals the whole fleet.
func (f *Fleet) Counts() Counts {
	var c Counts
	for _, ws := range f.Workspaces {
		w := ws.Counts()
		c.Agents += w.Agents
		c.Live += w.Live
		c.ForYou += w.ForYou
		c.Overdue += w.Overdue
		c.Blocked += w.Blocked
		c.Health += w.Health
	}
	return c
}

// FleetHeader is the count line for the whole fleet.
func FleetHeader(f *Fleet, width int, st Styles) string {
	return countLine("FLEET", f.Counts(), f.Live, f.Scanned, width, st)
}

// NeedsYou names the agents with something waiting on the principal, most
// first: "Gudrun 5 · Mimir 2".
func NeedsYou(ws *Workspace) string {
	type n struct {
		name  string
		count int
	}
	var list []n
	for _, a := range ws.Agents {
		if c := len(a.ForPrincipal(ws.Principal)) + a.InboxUnprocessed; c > 0 {
			list = append(list, n{a.Name, c})
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].count > list[j].count })
	parts := make([]string, len(list))
	for i, x := range list {
		parts[i] = fmt.Sprintf("%s %d", x.name, x.count)
	}
	return strings.Join(parts, " · ")
}

// workspaceTone is the attention colour for a whole workspace: its most
// urgent agent.
func workspaceTone(ws *Workspace) tone {
	t := toneOff
	for _, a := range ws.Agents {
		if at := agentTone(ws, a); at > t {
			t = at
		}
	}
	return t
}

var fleetCols = [7]int{14, 6, 5, 8, 8, 8, 7} // workspace, agents, live, for you, overdue, blocked, health

// FleetTableHeader and FleetRow render the fleet table; the last column,
// who needs you, takes the rest of the width.
func FleetTableHeader(width int, st Styles) string {
	cells := []string{"WORKSPACE", "AGENTS", "LIVE", "FOR YOU", "OVERDUE", "BLOCKED", "HEALTH"}
	for i := range cells {
		cells[i] = fit(cells[i], fleetCols[i])
	}
	return st.Head.Render(strings.TrimRight("    "+strings.Join(cells, "  ")+"  NEEDS YOU", " "))
}

func FleetRow(ws *Workspace, width int, selected bool, st Styles) string {
	c := ws.Counts()
	num := func(n, w int, style func(string) string) string {
		s := fit(fmt.Sprint(n), w)
		if n == 0 {
			return st.Dim.Render(s)
		}
		return style(s)
	}
	plain := func(s string) string { return s }
	live := "–"
	if ws.Live {
		live = fmt.Sprint(c.Live)
	}
	liveCell := fit(live, fleetCols[2])
	if c.Live > 0 {
		liveCell = st.OK.Render(liveCell)
	} else {
		liveCell = st.Dim.Render(liveCell)
	}
	cells := []string{
		fit(ws.Name, fleetCols[0]),
		fit(fmt.Sprint(c.Agents), fleetCols[1]),
		liveCell,
		num(c.ForYou, fleetCols[3], func(s string) string { return st.Warn.Render(s) }),
		num(c.Overdue, fleetCols[4], func(s string) string { return st.Fail.Render(s) }),
		num(c.Blocked, fleetCols[5], func(s string) string { return st.Fail.Render(s) }),
		num(c.Health, fleetCols[6], plain),
	}
	used := 4
	for _, w := range fleetCols {
		used += w + 2
	}
	needs := ansi.Truncate(NeedsYou(ws), max(width-used, 10), "…")
	marker := "  "
	if selected {
		marker = st.Title.Render("› ")
	}
	mark := " "
	switch workspaceTone(ws) {
	case toneProblem:
		mark = st.Fail.Render("●")
	case toneForYou:
		mark = st.Warn.Render("●")
	case toneWorking:
		mark = st.OK.Render("●")
	}
	return strings.TrimRight(marker+mark+" "+strings.Join(cells, "  ")+"  "+needs, " ")
}

// WriteFleet prints the fleet table once, for --fleet --once.
func WriteFleet(w io.Writer, f *Fleet, width int, st Styles) {
	fmt.Fprintln(w, FleetHeader(f, width, st))
	fmt.Fprintln(w)
	fmt.Fprintln(w, FleetTableHeader(width, st))
	for _, ws := range f.Workspaces {
		fmt.Fprintln(w, FleetRow(ws, width, false, st))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "    "+ColourLegend(st))
}
