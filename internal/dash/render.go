package dash

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Styles colors the dashboard. The zero value (NoColor) prints plain text.
type Styles struct {
	Title, Dim, Head, OK, Warn, Fail, Accent lipgloss.Style
}

// NewStyles returns the dashboard palette, or plain styles when noColor.
func NewStyles(noColor bool) Styles {
	if noColor {
		return Styles{}
	}
	c := func(hex string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(hex)) }
	return Styles{
		Title:  c("#7c6df2").Bold(true),
		Dim:    c("#777777"),
		Head:   lipgloss.NewStyle().Bold(true),
		OK:     c("#65d1a7"),
		Warn:   c("#e5c07b"),
		Fail:   c("#ff5f5f").Bold(true),
		Accent: c("#61afef"),
	}
}

// level colours health. Only a failure is a problem worth red; a warning is
// routine housekeeping and stays plain, and healthy is dimmed so it recedes.
// Amber is kept for one meaning only: something waits on the principal.
func (s Styles) level(l Level) lipgloss.Style {
	switch l {
	case Fail:
		return s.Fail
	case Warn:
		return lipgloss.NewStyle()
	default:
		return s.Dim
	}
}

// Summary is the one-line rollup used for the Herdr sidebar token:
// "1 live · 3 for you · 2 !".
func (ws *Workspace) Summary() string {
	var live, forYou, warn int
	for _, a := range ws.Agents {
		if a.Live != nil && a.Live.Running {
			live++
		}
		forYou += len(a.ForPrincipal(ws.Principal)) + a.InboxUnprocessed
		if a.Health.Level > OK {
			warn++
		}
	}
	var parts []string
	if ws.Live {
		parts = append(parts, fmt.Sprintf("%d live", live))
	}
	if forYou > 0 {
		parts = append(parts, fmt.Sprintf("%d for you", forYou))
	}
	if warn > 0 {
		parts = append(parts, fmt.Sprintf("%d !", warn))
	}
	return strings.Join(parts, " · ")
}

// WaitingLines lists what is waiting on the principal across the workspace.
func WaitingLines(ws *Workspace) []string {
	var out []string
	for _, a := range ws.Agents {
		for _, it := range a.ForPrincipal(ws.Principal) {
			out = append(out, fmt.Sprintf("%s #%s %s", a.Name, strings.TrimPrefix(it.ID, "#"), it.Headline()))
		}
		if a.InboxUnprocessed > 0 {
			out = append(out, fmt.Sprintf("%s: %d scheduled run(s) not yet read", a.Name, a.InboxUnprocessed))
		}
	}
	return out
}

// WaitingCompact is the one-line form for the live view: "Paullus 2 · Mimir 2 runs".
func WaitingCompact(ws *Workspace) string {
	var parts []string
	for _, a := range ws.Agents {
		if n := len(a.ForPrincipal(ws.Principal)); n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", a.Name, n))
		}
		if a.InboxUnprocessed > 0 {
			parts = append(parts, fmt.Sprintf("%s %d unread run(s)", a.Name, a.InboxUnprocessed))
		}
	}
	return strings.Join(parts, " · ")
}

// Row is one agent's table cells, unstyled.
type Row struct {
	Agent, Live, Started, Health, Session, Next string
	Level                                       Level
	NextBlocked                                 bool
}

// MakeRow flattens an agent into table cells.
func MakeRow(ws *Workspace, a *Agent) Row {
	r := Row{Agent: a.Name, Live: "—", Started: "—", Level: a.Health.Level}
	if a.Retired {
		r.Agent += " (retired)"
	}
	if a.Live != nil {
		r.Live = a.Live.State()
	} else if ws.Live {
		r.Live = "off"
	}
	if !a.LastStarted.IsZero() {
		r.Started = humanAge(ws.Scanned.Sub(a.LastStarted))
	}
	r.Health = a.Health.Level.Symbol()
	if n := len(a.Health.Findings); n > 0 {
		r.Health += fmt.Sprintf(" %d", n)
	}
	if s, ok := a.LastSession(); ok {
		date := "     "
		if !s.Date.IsZero() {
			date = s.Date.Format("01-02")
		}
		r.Session = date + " " + s.Title
	}
	switch {
	case a.NextSession != "":
		r.Next = "» " + a.NextSession
	default:
		if it, ok := a.NextUp(ws.Principal); ok {
			r.Next = strings.TrimSpace(fmt.Sprintf("%s #%s %s", it.Priority, strings.TrimPrefix(it.ID, "#"), it.Headline()))
			r.NextBlocked = it.BlockedOnOthers(ws.Principal)
		} else if len(a.Open) == 0 {
			r.Next = "(no open actions)"
		} else {
			r.Next = "(only parked or principal-owned items)"
		}
	}
	return r
}

// columns returns widths for agent, live, started, health, session, next.
func columns(ws *Workspace, width int) [6]int {
	agent := len("AGENT")
	for _, a := range ws.Agents {
		if n := ansi.StringWidth(MakeRow(ws, a).Agent); n > agent {
			agent = n
		}
	}
	w := [6]int{agent, 7, 7, 6, 0, 0}
	rest := width - (w[0] + w[1] + w[2] + w[3]) - 5*2 - 4 // cursor and attention mark
	if rest < 30 {
		rest = 30
	}
	w[4] = rest * 2 / 5
	if w[4] > 40 {
		w[4] = 40
	}
	w[5] = rest - w[4]
	return w
}

func fit(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	if pad := w - ansi.StringWidth(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

// TableHeader and TableRow render the agent table at a given width.
func TableHeader(ws *Workspace, width int, st Styles) string {
	w := columns(ws, width)
	cells := []string{"AGENT", "LIVE", "STARTED", "HEALTH", "LAST SESSION", "NEXT UP"}
	for i := range cells {
		cells[i] = fit(cells[i], w[i])
	}
	return st.Head.Render(strings.TrimRight("    "+strings.Join(cells, "  "), " "))
}

// TableRow renders one agent row; selected rows are highlighted.
func TableRow(ws *Workspace, a *Agent, width int, selected bool, st Styles) string {
	w := columns(ws, width)
	r := MakeRow(ws, a)
	live := fit(r.Live, w[1])
	switch r.Live {
	case "working", "blocked":
		live = st.OK.Render(live)
	case "idle", "running":
		live = st.Accent.Render(live)
	default:
		live = st.Dim.Render(live)
	}
	next := fit(r.Next, w[5])
	if r.NextBlocked {
		// The text stays plain; a tag right after it says it is blocked, red
		// only for a P1 (red means a problem), dim otherwise.
		tag, style := " ■ blocked", st.Dim
		if strings.HasPrefix(r.Next, "P1 ") {
			style = st.Fail
		}
		text := ansi.Truncate(r.Next, max(w[5]-len([]rune(tag)), 10), "…")
		next = text + style.Render(tag)
		if pad := w[5] - ansi.StringWidth(next); pad > 0 {
			next += strings.Repeat(" ", pad)
		}
	}
	cells := []string{
		fit(r.Agent, w[0]),
		live,
		st.Dim.Render(fit(r.Started, w[2])),
		st.level(r.Level).Render(fit(r.Health, w[3])),
		fit(r.Session, w[4]),
		next,
	}
	marker := "  "
	if selected {
		marker = st.Title.Render("› ")
	}
	return strings.TrimRight(marker+attentionMark(ws, a, st)+" "+strings.Join(cells, "  "), " ")
}

// attentionMark is a coloured dot before the agent's name, the same colour
// logic as the tile borders: red for a problem, amber when something waits on
// the principal, green while it works. Nothing to act on means no mark, so
// the eye goes to the rows that matter.
func attentionMark(ws *Workspace, a *Agent, st Styles) string {
	switch agentTone(ws, a) {
	case toneProblem:
		return st.Fail.Render("●")
	case toneForYou:
		return st.Warn.Render("●")
	case toneWorking:
		return st.OK.Render("●")
	default:
		return " "
	}
}

// ColourLegend explains the attention dots in one line.
func ColourLegend(st Styles) string {
	return st.Fail.Render("●") + st.Dim.Render(" problem   ") +
		st.Warn.Render("●") + st.Dim.Render(" waiting on you   ") +
		st.OK.Render("●") + st.Dim.Render(" working   ") +
		st.Dim.Render("no dot: nothing to act on")
}

// Detail renders the full picture for one agent as lines, listing at most
// maxItems open actions (0 lists them all).
func Detail(ws *Workspace, a *Agent, width, maxItems int, st Styles) []string {
	var out []string
	add := func(s string) { out = append(out, ansi.Truncate(s, width, "…")) }
	title := st.Title.Render(a.Name)
	if a.Title != "" {
		title += st.Dim.Render(" — " + a.Title)
	}
	add(title)

	var facts []string
	if a.Live != nil {
		facts = append(facts, fmt.Sprintf("pane %s %s", a.Live.PaneID, a.Live.State()))
	}
	if !a.LastStarted.IsZero() {
		facts = append(facts, "started "+a.LastStarted.Local().Format("Mon 02 Jan 15:04"))
	}
	if !a.LastActive.IsZero() {
		if age := ws.Scanned.Sub(a.LastActive); age < time.Minute {
			facts = append(facts, "active just now")
		} else {
			facts = append(facts, "last active "+humanAge(age)+" ago")
		}
	}
	if !a.LastReviewed.IsZero() {
		facts = append(facts, "tracker reviewed "+a.LastReviewed.Format("2006-01-02"))
	}
	if len(facts) > 0 {
		add(st.Dim.Render(strings.Join(facts, " · ")))
	}

	if a.NextSession != "" {
		add("")
		add(st.Head.Render("Next session (agent's own plan)"))
		add("  " + a.NextSession)
	}

	if mine := a.ForPrincipal(ws.Principal); len(mine) > 0 || a.InboxUnprocessed > 0 {
		add("")
		add(st.Warn.Render("Waiting on " + firstName(ws.Principal)))
		for _, it := range mine {
			add(fmt.Sprintf("  #%-3s %s", strings.TrimPrefix(it.ID, "#"), it.Headline()))
		}
		if a.InboxUnprocessed > 0 {
			add(fmt.Sprintf("  %d scheduled run(s) not yet read — start the agent to drain its inbox", a.InboxUnprocessed))
		}
	}

	blocked := a.BlockedP1s(ws.Principal)
	if len(blocked) > 0 {
		add("")
		add(st.Fail.Render("Blocked items"))
		for _, it := range blocked {
			add(itemLine(it, st))
		}
	}

	if len(a.Open) > 0 {
		add("")
		c := a.Counts()
		add(st.Head.Render(fmt.Sprintf("Open actions — P1 %d · P2 %d · P3 %d", c["P1"], c["P2"], c["P3"])))
		// Blocked P1s are listed above, so they are left out here; the
		// counts in the heading still include them. Within each priority,
		// live items come before parked ones.
		shown, listed := 0, len(blocked)
		var ordered []Item
		for _, p := range []string{"P1", "P2", "P3", ""} {
			for _, parked := range []bool{false, true} {
				for _, it := range a.Open {
					if it.Priority == p && it.Parked() == parked {
						ordered = append(ordered, it)
					}
				}
			}
		}
		for _, it := range ordered {
			if maxItems > 0 && shown >= maxItems {
				break
			}
			if it.Priority == "P1" && it.BlockedOnOthers(ws.Principal) {
				continue
			}
			shown++
			line := fmt.Sprintf("  %-2s ", it.Priority) + strings.TrimPrefix(itemLine(it, st), "  ")
			if it.BlockedOnOthers(ws.Principal) {
				line += st.Dim.Render("  ■ blocked")
			} else if it.Parked() {
				line = st.Dim.Render(line)
			}
			add(line)
		}
		if more := len(a.Open) - listed - shown; more > 0 {
			add(st.Dim.Render(fmt.Sprintf("  … %d more", more)))
		}
	}

	if s, ok := a.LastSession(); ok {
		add("")
		date := ""
		if !s.Date.IsZero() {
			date = s.Date.Format("Mon 02 Jan") + " — "
		}
		add(st.Head.Render("Last session: ") + date + s.Title)
		for _, l := range wrap(s.Summary, width-2, 3) {
			add(st.Dim.Render("  " + l))
		}
	}

	add("")
	if len(a.Health.Findings) == 0 {
		add(st.OK.Render("Health ✓") + st.Dim.Render(fmt.Sprintf("  standing %d/%d · sessions %d/%d · actions %d KB",
			len(a.Standing), maxStanding, len(a.Sessions), maxSessions, a.ActionsSize/1024)))
	} else {
		add(st.level(a.Health.Level).Render("Health " + a.Health.Level.Symbol()))
		for _, f := range a.Health.Findings {
			add("  " + f)
		}
	}
	return out
}

// wrap breaks text into at most max lines of width w.
func wrap(text string, w, max int) []string {
	if w < 20 {
		w = 20
	}
	var lines []string
	cur := ""
	for _, word := range strings.Fields(text) {
		if cur != "" && ansi.StringWidth(cur)+1+ansi.StringWidth(word) > w {
			lines = append(lines, cur)
			cur = ""
			if len(lines) == max {
				lines[max-1] = ansi.Truncate(lines[max-1]+" "+word, w, "…")
				return lines
			}
		}
		if cur != "" {
			cur += " "
		}
		cur += word
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// View names the dashboard layouts.
const (
	ViewTiles = "tiles"
	ViewTable = "table"
)

// WriteSnapshot prints the whole dashboard once, for --once.
func WriteSnapshot(w io.Writer, ws *Workspace, width int, view string, detail bool, st Styles) {
	if view == ViewTiles && len(ws.Agents) > 0 {
		for _, l := range writeTiles(ws, width, st) {
			fmt.Fprintln(w, strings.TrimRight(l, " "))
		}
		if detail {
			for _, a := range ws.Agents {
				fmt.Fprintln(w)
				for _, l := range Detail(ws, a, width, 0, st) {
					fmt.Fprintln(w, l)
				}
			}
		}
		return
	}
	fmt.Fprintln(w, CountHeader(ws, width, st))
	if waiting := WaitingLines(ws); len(waiting) > 0 {
		fmt.Fprintln(w, st.Warn.Render("Waiting on "+firstName(ws.Principal)+":"))
		for _, l := range waiting {
			fmt.Fprintln(w, ansi.Truncate("  "+l, width, "…"))
		}
	}
	fmt.Fprintln(w)
	if len(ws.Agents) == 0 {
		fmt.Fprintln(w, "No agents in this workspace.")
		return
	}
	fmt.Fprintln(w, TableHeader(ws, width, st))
	for _, a := range ws.Agents {
		fmt.Fprintln(w, TableRow(ws, a, width, false, st))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "    "+ColourLegend(st))
	if detail {
		for _, a := range ws.Agents {
			fmt.Fprintln(w)
			for _, l := range Detail(ws, a, width, 0, st) {
				fmt.Fprintln(w, l)
			}
		}
	}
}

// itemLine is one action in the detail view: id, headline, then owner and
// status dimmed.
func itemLine(it Item, st Styles) string {
	line := fmt.Sprintf("  #%-3s %s", strings.TrimPrefix(it.ID, "#"), it.Headline())
	if it.Owner != "" {
		line += st.Dim.Render("  [" + it.Owner + "]")
	}
	if it.Status != "" {
		line += st.Dim.Render("  " + it.Status)
	}
	return line
}

func firstName(principal string) string {
	if f := strings.Fields(principal); len(f) > 0 {
		return f[0]
	}
	return "you"
}

// JSON is the machine-readable snapshot for scripts and the Herdr plugin.
type JSON struct {
	Workspace string      `json:"workspace"`
	Root      string      `json:"root"`
	Principal string      `json:"principal"`
	Summary   string      `json:"summary"`
	Scanned   time.Time   `json:"scanned"`
	Agents    []JSONAgent `json:"agents"`
}

// JSONAgent is one agent in the JSON snapshot.
type JSONAgent struct {
	Name         string         `json:"name"`
	Title        string         `json:"title,omitempty"`
	Retired      bool           `json:"retired,omitempty"`
	Live         string         `json:"live,omitempty"`
	Pane         string         `json:"pane,omitempty"`
	LastStarted  *time.Time     `json:"last_started,omitempty"`
	LastActive   *time.Time     `json:"last_active,omitempty"`
	LastSession  *Session       `json:"last_session,omitempty"`
	NextSession  string         `json:"next_session,omitempty"`
	NextUp       *Item          `json:"next_up,omitempty"`
	ForPrincipal []Item         `json:"for_principal,omitempty"`
	OpenCounts   map[string]int `json:"open_counts"`
	Health       string         `json:"health"`
	Findings     []string       `json:"findings,omitempty"`
}

// WriteJSON prints the snapshot as JSON.
func WriteJSON(w io.Writer, ws *Workspace) error {
	out := JSON{Workspace: ws.Name, Root: ws.Root, Principal: ws.Principal, Summary: ws.Summary(), Scanned: ws.Scanned}
	for _, a := range ws.Agents {
		j := JSONAgent{
			Name: a.Name, Title: a.Title, Retired: a.Retired,
			NextSession:  a.NextSession,
			ForPrincipal: a.ForPrincipal(ws.Principal),
			OpenCounts:   a.Counts(),
			Health:       [...]string{"ok", "warn", "fail"}[a.Health.Level],
			Findings:     a.Health.Findings,
		}
		if a.Live != nil {
			j.Live, j.Pane = a.Live.State(), a.Live.PaneID
		}
		if !a.LastStarted.IsZero() {
			t := a.LastStarted
			j.LastStarted = &t
		}
		if !a.LastActive.IsZero() {
			t := a.LastActive
			j.LastActive = &t
		}
		if s, ok := a.LastSession(); ok {
			j.LastSession = &s
		}
		if it, ok := a.NextUp(ws.Principal); ok {
			j.NextUp = &it
		}
		out.Agents = append(out.Agents, j)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
