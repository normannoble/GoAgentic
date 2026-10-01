package dash

import (
	"bufio"
	"fmt"
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
	// Harness is the agent's own context.md harness: override, if any.
	Harness string

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
	// Unwrapped: the tracker changed after the newest session log and was
	// never committed, so a session ended without a wrap.
	Unwrapped bool

	LastStarted time.Time
	LastActive  time.Time
	Live        *LivePane
	// Activity counts sessions per day, oldest first, ending today.
	Activity []int
	starts   []time.Time

	Health Health
}

// Item is one open row of the action tracker.
type Item struct {
	ID       string `json:"id"`
	Action   string `json:"action"`
	Owner    string `json:"owner,omitempty"`
	Priority string `json:"priority,omitempty"`
	Status   string `json:"status,omitempty"`
	// Due is the date in the Due/Target cell, if it holds one.
	Due *time.Time `json:"due,omitempty"`
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
		Harness: ctx["harness"],
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
		a.InboxUnprocessed = countUnprocessed(string(data))
	}
	return a
}

// countUnprocessed counts inbox entry headings whose state is UNPROCESSED
// ("### 2026-09-02T09:07+08:00 — task — UNPROCESSED"). The file's format
// header mentions the word in prose and in a "<placeholder>" heading; neither
// is an entry.
func countUnprocessed(inbox string) int {
	n := 0
	for _, line := range strings.Split(inbox, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#") || strings.Contains(line, "<") {
			continue
		}
		parts := strings.Split(line, "—")
		if strings.HasPrefix(strings.TrimSpace(parts[len(parts)-1]), "UNPROCESSED") {
			n++
		}
	}
	return n
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
		due := cell(cells, cols, "due/target")
		if due == "" {
			due = cell(cells, cols, "due")
		}
		if d := dateRe.FindString(due); d != "" {
			if t, err := time.Parse("2006-01-02", d); err == nil {
				item.Due = &t
			}
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

// The four standard states a status starts with (master conventions
// § actions.md). Older free-text statuses fall back to keyword matching.
const (
	StateNotStarted = "Not started"
	StateInProgress = "In progress"
	StateWaiting    = "Waiting on"
	StateParked     = "Parked"
)

var (
	stateRe = regexp.MustCompile(`(?i)^(not started|in progress|waiting on|parked)\b`)
	whoRe   = regexp.MustCompile(`^(.*?)(\s+[—–-]\s+|[;(,]|$)`)
)

// State returns the standard state the status starts with, and for
// "Waiting on" who it waits on. ok is false for a free-text status.
func (i Item) State() (state, who string, ok bool) {
	status := strings.TrimSpace(i.Status)
	m := stateRe.FindStringSubmatch(status)
	if m == nil {
		return "", "", false
	}
	for _, st := range []string{StateNotStarted, StateInProgress, StateWaiting, StateParked} {
		if strings.EqualFold(m[1], st) {
			state = st
		}
	}
	if state == StateWaiting {
		rest := strings.TrimSpace(status[len(m[0]):])
		who = strings.TrimSpace(whoRe.FindStringSubmatch(rest)[1])
	}
	return state, who, true
}

// blockedRe matches whole words only, so "Unblocked" is not a block.
var blockedRe = regexp.MustCompile(`\b(block\w*|gated|await\w*|waiting|on hold)\b`)

// Blocked reports whether the status says the item is waiting on something.
func (i Item) Blocked() bool {
	if state, _, ok := i.State(); ok {
		return state == StateWaiting
	}
	return blockedRe.MatchString(strings.ToLower(i.Status))
}

// WaitsOn reports whether the item is waiting on the principal ("Waiting
// on Norman", or in older trackers "awaiting Norman"). Such an item is the
// principal's to move, not a blocker for the agent to chase.
func (i Item) WaitsOn(principal string) bool {
	state, who, ok := i.State()
	if ok && state != StateWaiting {
		return false
	}
	for _, part := range strings.Fields(strings.ToLower(principal)) {
		name := regexp.QuoteMeta(part)
		if ok {
			if regexp.MustCompile(`\b` + name + `\b`).MatchString(strings.ToLower(who)) {
				return true
			}
			continue
		}
		re := `\b(block\w*|gated|await\w*|waiting)\s+((on|for|by)\s+)?` + name + `\b`
		if regexp.MustCompile(re).MatchString(strings.ToLower(i.Status)) {
			return true
		}
	}
	return false
}

// BlockedOnOthers reports whether the item is live and waiting on someone
// other than the principal.
func (i Item) BlockedOnOthers(principal string) bool {
	return i.Blocked() && !i.Parked() && !i.WaitsOn(principal)
}

// notStartedRe matches a free-text status that says work has not begun:
// "Open", "To do", or nothing at all.
var notStartedRe = regexp.MustCompile(`^((not (yet )?started|open|new|to ?do)\b|$)`)

// NotStarted reports whether the item is live but nobody has begun it.
func (i Item) NotStarted() bool {
	if state, _, ok := i.State(); ok {
		return state == StateNotStarted
	}
	return notStartedRe.MatchString(strings.ToLower(strings.TrimSpace(i.Status))) && !i.Parked()
}

// Parked reports whether the item is deliberately not being worked.
func (i Item) Parked() bool {
	if state, _, ok := i.State(); ok {
		return state == StateParked
	}
	s := strings.ToLower(i.Status)
	for _, w := range []string{"parked", "defer", "hold", "held", "dormant", "deprioriti", "done", "closed"} {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// Overdue reports whether the item has a due date before today and is still
// live.
func (i Item) Overdue(now time.Time) bool {
	if i.Due == nil || i.Parked() {
		return false
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return i.Due.Before(today)
}

// Short is the headline cut to its first clause, for tight spaces.
func (i Item) Short() string { return shortText(i.Headline()) }

// shortText keeps the text before the first clause break (" — ", ": ",
// ". ", " (") when that leaves something meaningful.
func shortText(s string) string {
	cut := len(s)
	for _, sep := range []string{" — ", " – ", " - ", ": ", ". ", "; ", " ("} {
		if i := strings.Index(s, sep); i >= 8 && i < cut {
			cut = i
		}
	}
	return strings.TrimRight(s[:cut], ".,;:")
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
		if (it.OnlyOwnedBy(principal) || it.WaitsOn(principal)) && !it.Parked() {
			out = append(out, it)
		}
	}
	return out
}

// PlanStep is one step of the agent's "Next session:" line, with the
// priority of the item it names when that item is open.
type PlanStep struct {
	Priority, Text string
}

var (
	planSplitRe = regexp.MustCompile(`\s*(→|->)\s*`)
	planIDRe    = regexp.MustCompile(`^#(\w+)\s*`)
)

// PlanSteps splits the "Next session:" line on its arrows. A step that
// starts with an open item's number carries that item's priority; the
// agent's own wording is kept.
func (a *Agent) PlanSteps() []PlanStep {
	var out []PlanStep
	for _, part := range planSplitRe.Split(a.NextSession, -1) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		step := PlanStep{Text: part}
		if m := planIDRe.FindStringSubmatch(part); m != nil {
			for _, it := range a.Open {
				if strings.TrimPrefix(it.ID, "#") == m[1] {
					step.Priority = it.Priority
					step.Text = fmt.Sprintf("#%-3s %s", m[1], strings.TrimPrefix(part, m[0]))
					break
				}
			}
		}
		out = append(out, step)
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

// activityDays is how many days the sparkline covers.
const activityDays = 12

// activity counts, per day for the last activityDays days, how many times the
// agent was started (from transcripts), or on days with no transcript data,
// how many session logs it wrote.
func activity(a *Agent, now time.Time) []int {
	day := func(t time.Time) int {
		t = t.In(now.Location())
		d0 := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		d1 := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, now.Location())
		return activityDays - 1 - int(d0.Sub(d1).Hours()/24+0.5)
	}
	starts := make([]int, activityDays)
	for _, t := range a.starts {
		if i := day(t); i >= 0 && i < activityDays {
			starts[i]++
		}
	}
	out := make([]int, activityDays)
	for _, s := range a.Sessions {
		if s.Date.IsZero() {
			continue
		}
		d := time.Date(s.Date.Year(), s.Date.Month(), s.Date.Day(), 12, 0, 0, 0, now.Location())
		if i := day(d); i >= 0 && i < activityDays {
			out[i]++
		}
	}
	for i := range out {
		if starts[i] > 0 {
			out[i] = starts[i]
		}
	}
	return out
}

// BlockedP1s lists P1 items waiting on someone other than the principal.
// Lower priorities are often "awaiting" something as a matter of course;
// flagging them would make every agent look urgent. Items waiting on the
// principal show under "Waiting on" instead.
func (a *Agent) BlockedP1s(principal string) []Item {
	var out []Item
	for _, it := range a.Open {
		if it.Priority == "P1" && it.BlockedOnOthers(principal) {
			out = append(out, it)
		}
	}
	return out
}

// BlockedCount counts the items BlockedP1s returns.
func (a *Agent) BlockedCount(principal string) int { return len(a.BlockedP1s(principal)) }

// OverdueCount counts live items past their due date.
func (a *Agent) OverdueCount(now time.Time) int {
	n := 0
	for _, it := range a.Open {
		if it.Overdue(now) {
			n++
		}
	}
	return n
}
