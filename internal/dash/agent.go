package dash

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Agent is everything the dashboard knows about one agent.
type Agent struct {
	Name  string
	Dir   string
	Title string
	Scope string

	Retired bool

	// LastReviewed is the date on the tracker's "Last reviewed:" line.
	LastReviewed    time.Time
	LastReviewedLen int
	// NextSession is the agent's own "Next session:" line, written at wrap.
	NextSession string
	Open        []Item

	Sessions    []Session
	Standing    []FileInfo
	ActionsSize int64
	Missing     []string

	PeerOpen         int
	InboxUnprocessed int
	Uncommitted      int

	LastStarted time.Time
	LastActive  time.Time
	Live        *LivePane

	Health Health
}

// Item is one open row of the action tracker.
type Item struct {
	ID       string `json:"id"`
	Action   string `json:"action"`
	Owner    string `json:"owner,omitempty"`
	Priority string `json:"priority,omitempty"`
	Status   string `json:"status,omitempty"`
	// Section is the ### heading the row sits under.
	Section string `json:"section,omitempty"`
}

// Session is one file in memory/sessions/.
type Session struct {
	File    string    `json:"file"`
	Date    time.Time `json:"date"`
	Title   string    `json:"title"`
	Summary string    `json:"summary,omitempty"`
}

// FileInfo is a file name and size.
type FileInfo struct {
	Name string
	Size int64
}

// coreFiles must exist in every agent directory.
var coreFiles = []string{"soul.md", "role.md", "autonomy.md", "actions.md", "MEMORY.md"}

func loadAgent(dir, principal string, now time.Time) *Agent {
	ctx := readFrontmatter(filepath.Join(dir, "context.md"))
	a := &Agent{
		Name:    filepath.Base(dir),
		Dir:     dir,
		Title:   ctx["title"],
		Scope:   ctx["scope"],
		Retired: strings.EqualFold(ctx["status"], "retired"),
	}
	for _, f := range coreFiles {
		if !fileExists(filepath.Join(dir, f)) {
			a.Missing = append(a.Missing, f)
		}
	}

	actionsPath := filepath.Join(dir, "actions.md")
	if info, err := os.Stat(actionsPath); err == nil {
		a.ActionsSize = info.Size()
	}
	if data, err := os.ReadFile(actionsPath); err == nil {
		t := parseActions(string(data))
		a.LastReviewed, a.LastReviewedLen = t.lastReviewed, t.lastReviewedLen
		a.NextSession = t.nextSession
		a.Open = t.open
	}

	a.Sessions = loadSessions(filepath.Join(dir, "memory", "sessions"))
	a.Standing = listFiles(filepath.Join(dir, "memory", "standing"), "*")

	for _, p := range listFiles(filepath.Join(dir, "peer"), "*.md") {
		switch strings.ToLower(readFrontmatter(filepath.Join(dir, "peer", p.Name))["status"]) {
		case "open", "answered", "needs-principal":
			a.PeerOpen++
		}
	}
	if data, err := os.ReadFile(filepath.Join(dir, "memory", "scheduled", "inbox.md")); err == nil {
		a.InboxUnprocessed = strings.Count(string(data), "UNPROCESSED")
	}
	return a
}

type tracker struct {
	lastReviewed    time.Time
	lastReviewedLen int
	nextSession     string
	open            []Item
}

var (
	dateRe        = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)
	priorityRe    = regexp.MustCompile(`^P([1-9])\b`)
	separatorRe   = regexp.MustCompile(`^\|[\s:|-]+\|?\s*$`)
	nextSessionRe = regexp.MustCompile(`(?i)^\**next session:?\**:?\s*(.+)$`)
)

// parseActions reads the tracker format from the conventions: a
// "Last reviewed:" line, an optional "Next session:" line, and markdown tables
// under "## Open" (optionally grouped by "### P1" style headings). Columns are
// found by header name, so trackers that renamed the ticket column still parse.
func parseActions(text string) tracker {
	var t tracker
	inOpen := false
	section := ""
	var cols map[string]int
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "Last reviewed:"):
			t.lastReviewedLen = len(line)
			if d := dateRe.FindString(line); d != "" {
				t.lastReviewed, _ = time.Parse("2006-01-02", d)
			}
			continue
		case nextSessionRe.MatchString(line) && !strings.HasPrefix(line, "|"):
			if next := cleanMarkdown(nextSessionRe.FindStringSubmatch(line)[1]); !placeholder(next) {
				t.nextSession = next
			}
			continue
		case strings.HasPrefix(line, "## "):
			inOpen = strings.HasPrefix(strings.ToLower(strings.TrimSpace(line[3:])), "open")
			section, cols = "", nil
			continue
		case strings.HasPrefix(line, "### "):
			section, cols = strings.TrimSpace(line[4:]), nil
			continue
		}
		if !inOpen || !strings.HasPrefix(line, "|") {
			if line != "" && !strings.HasPrefix(line, "|") {
				cols = nil
			}
			continue
		}
		if separatorRe.MatchString(line) {
			continue
		}
		cells := splitRow(line)
		if cols == nil {
			cols = headerColumns(cells)
			if _, ok := cols["action"]; !ok {
				cols = nil
			}
			continue
		}
		item := Item{
			ID:       cell(cells, cols, "#"),
			Action:   cell(cells, cols, "action"),
			Owner:    cleanMarkdown(cell(cells, cols, "owner")),
			Priority: strings.ToUpper(cleanMarkdown(cell(cells, cols, "priority"))),
			Status:   cleanMarkdown(cell(cells, cols, "status")),
			Section:  section,
		}
		if placeholder(item.Action) || strings.HasPrefix(item.Action, "~~") {
			continue
		}
		if !priorityRe.MatchString(item.Priority) {
			item.Priority = ""
			if m := priorityRe.FindString(section); m != "" {
				item.Priority = m
			}
		} else {
			item.Priority = priorityRe.FindString(item.Priority)
		}
		t.open = append(t.open, item)
	}
	return t
}

// placeholder reports an empty-table filler row such as "*(none)*" or "—".
func placeholder(action string) bool {
	a := strings.Trim(cleanMarkdown(action), "*_() ")
	return a == "" || a == "—" || a == "-" || strings.EqualFold(a, "none")
}

func splitRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	// Escaped pipes are text, not column breaks.
	line = strings.ReplaceAll(line, `\|`, "\x00")
	parts := strings.Split(line, "|")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(strings.ReplaceAll(p, "\x00", "|"))
	}
	return parts
}

func headerColumns(cells []string) map[string]int {
	cols := map[string]int{}
	for i, c := range cells {
		key := strings.ToLower(cleanMarkdown(c))
		if key == "id" {
			key = "#"
		}
		if _, dup := cols[key]; !dup {
			cols[key] = i
		}
	}
	return cols
}

func cell(cells []string, cols map[string]int, name string) string {
	i, ok := cols[name]
	if !ok || i >= len(cells) {
		return ""
	}
	return cells[i]
}

var (
	linkRe     = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	wikiRe     = regexp.MustCompile(`\[\[([^\]]+)\]\]`)
	leadBoldRe = regexp.MustCompile(`^\*\*(.+?)\*\*`)
	brRe       = regexp.MustCompile(`(?i)<br\s*/?>`)
)

// cleanMarkdown turns a table cell into plain one-line text.
func cleanMarkdown(s string) string {
	s = brRe.ReplaceAllString(s, " ")
	s = wikiRe.ReplaceAllString(s, "$1")
	s = linkRe.ReplaceAllString(s, "$1")
	s = strings.NewReplacer("**", "", "__", "", "`", "", "~~", "").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

// Headline is the short form of an action: its leading bold phrase when the
// tracker uses the "**Summary.** detail" style, else the whole cell.
func (i Item) Headline() string {
	if m := leadBoldRe.FindStringSubmatch(strings.TrimSpace(i.Action)); m != nil {
		return strings.TrimRight(cleanMarkdown(m[1]), ".:")
	}
	return cleanMarkdown(i.Action)
}

// Blocked reports whether the status says the item is waiting on something.
func (i Item) Blocked() bool {
	s := strings.ToLower(i.Status)
	for _, w := range []string{"block", "gated", "await", "waiting", "on hold"} {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// Parked reports whether the item is deliberately not being worked.
func (i Item) Parked() bool {
	s := strings.ToLower(i.Status)
	for _, w := range []string{"parked", "defer", "hold", "held", "dormant", "deprioriti", "done", "closed"} {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// OwnedBy reports whether name appears among the item's owners.
func (i Item) OwnedBy(name string) bool {
	return name != "" && containsWord(i.Owner, name)
}

// OnlyOwnedBy reports whether name is the item's sole owner.
func (i Item) OnlyOwnedBy(name string) bool {
	if !i.OwnedBy(name) {
		return false
	}
	rest := strings.ToLower(i.Owner)
	for _, part := range strings.Fields(strings.ToLower(name)) {
		rest = strings.ReplaceAll(rest, part, "")
	}
	return strings.Trim(rest, " /,&+()-") == ""
}

func containsWord(haystack, name string) bool {
	h := strings.ToLower(haystack)
	for _, part := range strings.Fields(strings.ToLower(name)) {
		if strings.Contains(h, part) {
			return true
		}
	}
	return false
}

func priorityRank(p string) int {
	if len(p) == 2 && p[0] == 'P' {
		return int(p[1] - '0')
	}
	return 9
}

// NextUp is the tracker's best candidate for the agent's next piece of work:
// the highest-priority open row the agent (not only the principal) owns that
// is not parked. The agent's own "Next session:" line, when present, is shown
// in preference by the renderers.
func (a *Agent) NextUp(principal string) (Item, bool) {
	var best Item
	found := false
	for _, it := range a.Open {
		if it.Parked() || it.OnlyOwnedBy(principal) {
			continue
		}
		if !found || priorityRank(it.Priority) < priorityRank(best.Priority) {
			best, found = it, true
		}
	}
	return best, found
}

// ForPrincipal lists open rows the principal alone owns: work that waits on
// them, not work they share with the agent.
func (a *Agent) ForPrincipal(principal string) []Item {
	var out []Item
	for _, it := range a.Open {
		if it.OnlyOwnedBy(principal) && !it.Parked() {
			out = append(out, it)
		}
	}
	return out
}

// Counts returns the number of open items per priority.
func (a *Agent) Counts() map[string]int {
	out := map[string]int{}
	for _, it := range a.Open {
		out[it.Priority]++
	}
	return out
}

// LastSession returns the newest session log, if any.
func (a *Agent) LastSession() (Session, bool) {
	if len(a.Sessions) == 0 {
		return Session{}, false
	}
	return a.Sessions[0], true
}

// loadSessions returns session logs newest first, by frontmatter date, then
// file name, then modification time.
func loadSessions(dir string) []Session {
	files := listFiles(dir, "*.md")
	type keyed struct {
		s     Session
		mtime time.Time
	}
	var all []keyed
	for _, f := range files {
		path := filepath.Join(dir, f.Name)
		s := Session{File: f.Name}
		fm := readFrontmatter(path)
		d := dateRe.FindString(fm["date"])
		if d == "" {
			d = dateRe.FindString(f.Name)
		}
		s.Date, _ = time.Parse("2006-01-02", d)
		s.Title, s.Summary = sessionHeadline(path)
		if s.Title == "" {
			s.Title = strings.TrimSuffix(f.Name, ".md")
		}
		var mtime time.Time
		if info, err := os.Stat(path); err == nil {
			mtime = info.ModTime()
		}
		all = append(all, keyed{s, mtime})
	}
	sort.SliceStable(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if !a.s.Date.Equal(b.s.Date) {
			return a.s.Date.After(b.s.Date)
		}
		if a.s.File != b.s.File {
			return a.s.File > b.s.File
		}
		return a.mtime.After(b.mtime)
	})
	out := make([]Session, len(all))
	for i, k := range all {
		out[i] = k.s
	}
	return out
}

// sessionHeadline returns the first "# " heading and the first prose line
// after it.
func sessionHeadline(path string) (title, summary string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	inFront := false
	for n := 0; sc.Scan() && n < 200; n++ {
		line := strings.TrimSpace(sc.Text())
		if n == 0 && line == "---" {
			inFront = true
			continue
		}
		if inFront {
			if line == "---" {
				inFront = false
			}
			continue
		}
		if title == "" {
			if strings.HasPrefix(line, "# ") {
				title = cleanMarkdown(line[2:])
			}
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "|") {
			if summary != "" {
				break
			}
			continue
		}
		summary = strings.TrimSpace(summary + " " + cleanMarkdown(strings.TrimLeft(line, "-* ")))
	}
	return title, summary
}

func listFiles(dir, pattern string) []FileInfo {
	matches, _ := filepath.Glob(filepath.Join(dir, pattern))
	var out []FileInfo
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil || info.IsDir() || strings.HasPrefix(info.Name(), ".") {
			continue
		}
		out = append(out, FileInfo{Name: info.Name(), Size: info.Size()})
	}
	return out
}
