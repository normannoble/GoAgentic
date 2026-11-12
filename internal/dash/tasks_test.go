package dash

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func strp(s string) *string { return &s }

// diffLines returns the indexes of the lines that differ; both texts must
// have the same number of lines.
func diffLines(t *testing.T, a, b string) []int {
	t.Helper()
	la, lb := strings.Split(a, "\n"), strings.Split(b, "\n")
	if len(la) != len(lb) {
		t.Fatalf("line count changed: %d → %d", len(la), len(lb))
	}
	var out []int
	for i := range la {
		if la[i] != lb[i] {
			out = append(out, i)
		}
	}
	return out
}

const backlogTracker = `# Actions

Last reviewed: 2026-11-09
Next session: #12 ship → #2 tidy

## Open

### Active
| # | Action | Ticket | Owner | Priority | Due/Target | Status | Since |
|---|--------|--------|-------|----------|------------|--------|-------|
| 12 | **Ship it** — the thing, with ` + "`a\\|b`" + ` escaped | | Friday | P1 | Thu 12-11 | In progress — next: mockup | 2026-11-08 |
| 13 | **Pipe in code** — ` + "`x | y`" + ` unescaped | | Friday | P2 | | Not started | 2026-11-08 |

### Backlog (slack only)
| # | Action | Ticket | Owner | Priority | Due/Target | Status | Since |
|---|--------|--------|-------|----------|------------|--------|-------|
| 2 | **Repo cleanup** — list, verdict, one commit | | Friday | P2 | TBD | Not started | 2026-09-25 |
| 7 | **Ask Irfan** — something | | Irfan | P3 | | Waiting on Irfan — Do it? Recommend: yes. If no answer by 10-11: I park it | 2026-10-01 |

## Completed

| # | Action | Ticket | Owner | Completed |
|---|--------|--------|-------|-----------|
| 1 | Done thing | | Friday | 2026-09-01 |
| 2 | Duplicate id in Completed | | Friday | 2026-09-02 |
`

func TestEditTrackerRowChangesOnlyTheTargetCells(t *testing.T) {
	out, err := EditTrackerRow(backlogTracker, RowEdit{ID: "2", Status: strp("In progress — listing"), Priority: strp("P1")})
	if err != nil {
		t.Fatal(err)
	}
	changed := diffLines(t, backlogTracker, out)
	if len(changed) != 1 {
		t.Fatalf("changed lines %v, want one", changed)
	}
	got := strings.Split(out, "\n")[changed[0]]
	want := "| 2 | **Repo cleanup** — list, verdict, one commit | | Friday | P1 | TBD | In progress — listing | 2026-09-25 |"
	if got != want {
		t.Errorf("row =\n%s\nwant\n%s", got, want)
	}
	// The Completed row with the same id is untouched: only Open is searched.
	if !strings.Contains(out, "| 2 | Duplicate id in Completed | | Friday | 2026-09-02 |") {
		t.Error("the Completed table changed")
	}
}

func TestEditTrackerRowEscapedPipesAndEmptyCells(t *testing.T) {
	out, err := EditTrackerRow(backlogTracker, RowEdit{ID: "#12", Due: strp(""), Owner: strp("Friday / Irfan")})
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Split(out, "\n")[diffLines(t, backlogTracker, out)[0]]
	want := "| 12 | **Ship it** — the thing, with `a\\|b` escaped | | Friday / Irfan | P1 | | In progress — next: mockup | 2026-11-08 |"
	if line != want {
		t.Errorf("row =\n%s\nwant\n%s", line, want)
	}
	// Filling an empty cell, with a pipe and a newline in the value.
	out, err = EditTrackerRow(backlogTracker, RowEdit{ID: "7", Due: strp("Fri 13-11"), Status: strp("Waiting on Irfan — a | b\nc")})
	if err != nil {
		t.Fatal(err)
	}
	line = strings.Split(out, "\n")[diffLines(t, backlogTracker, out)[0]]
	if want := "| 7 | **Ask Irfan** — something | | Irfan | P3 | Fri 13-11 | Waiting on Irfan — a \\| b c | 2026-10-01 |"; line != want {
		t.Errorf("row =\n%s\nwant\n%s", line, want)
	}
	// The written row still parses to the same number of cells.
	if r, err := findOpenRow(out, "7"); err != nil || len(r.Cells) != r.Width || r.get("status") != "Waiting on Irfan — a | b c" {
		t.Errorf("re-read row = %+v, %v", r, err)
	}
}

func TestEditTrackerRowRefuses(t *testing.T) {
	for name, tc := range map[string]struct {
		text string
		edit RowEdit
		want string
	}{
		"unescaped pipe in a code span": {backlogTracker, RowEdit{ID: "13", Status: strp("Parked")}, "has 9 cells"},
		"not found":                     {backlogTracker, RowEdit{ID: "99", Status: strp("Parked")}, "not a row"},
		"only in Completed":             {backlogTracker, RowEdit{ID: "1", Status: strp("Parked")}, "not a row"},
		"duplicate": {strings.Replace(backlogTracker, "| 13 |", "| 12 |", 1),
			RowEdit{ID: "12", Status: strp("Parked")}, "2 times"},
		"no trailing pipe": {strings.Replace(backlogTracker, "Not started | 2026-09-25 |", "Not started | 2026-09-25", 1),
			RowEdit{ID: "2", Status: strp("Parked")}, "cells"},
		"no column": {strings.ReplaceAll(backlogTracker, "| Priority |", "| Prio |"),
			RowEdit{ID: "2", Priority: strp("P1")}, "no column"},
	} {
		if _, err := EditTrackerRow(tc.text, tc.edit); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
}

func TestEditTrackerRowKeepsCRLFAndNoOp(t *testing.T) {
	crlf := strings.ReplaceAll(backlogTracker, "\n", "\r\n")
	out, err := EditTrackerRow(crlf, RowEdit{ID: "2", Status: strp("Parked")})
	if err != nil {
		t.Fatal(err)
	}
	changed := diffLines(t, crlf, out)
	if len(changed) != 1 || !strings.HasSuffix(strings.Split(out, "\n")[changed[0]], "| Parked | 2026-09-25 |\r") {
		t.Errorf("CRLF row = %q", strings.Split(out, "\n")[changed[0]])
	}
	if out, _ := EditTrackerRow(backlogTracker, RowEdit{ID: "2"}); out != backlogTracker {
		t.Error("an empty edit must change nothing")
	}
}

func TestStatusPartsRoundTrip(t *testing.T) {
	for _, tc := range []struct{ in, state, who, note string }{
		{"In progress — next: mockup", StateInProgress, "", "next: mockup"},
		{"Waiting on Irfan — Do it? Recommend: yes.", StateWaiting, "Irfan", "Do it? Recommend: yes."},
		{"Blocked — no supplier", StateBlocked, "", "no supplier"},
		{"Parked", StateParked, "", ""},
		{"Not started", StateNotStarted, "", ""},
		{"awaiting the bank", "", "", "awaiting the bank"},
	} {
		state, who, note := statusParts(tc.in)
		if state != tc.state || who != tc.who || note != tc.note {
			t.Errorf("statusParts(%q) = %q %q %q", tc.in, state, who, note)
		}
		if back, err := composeStatus(state, who, note); err != nil || back != tc.in {
			t.Errorf("composeStatus = %q %v, want %q", back, err, tc.in)
		}
	}
	if _, err := composeStatus(StateWaiting, "", "x"); err == nil {
		t.Error("Waiting on nobody must be refused")
	}
	if _, err := composeStatus("Done", "", ""); err == nil {
		t.Error("an unknown state must be refused")
	}
}

func TestSplitAsk(t *testing.T) {
	q, rec, by, def := splitAsk("Which mockup goes to spec? Recommend: A + B, as decided 08-11. If no answer by 12-11: I write the spec on A + B")
	if q != "Which mockup goes to spec?" || rec != "A + B, as decided 08-11." || by != "12-11" || def != "I write the spec on A + B" {
		t.Errorf("splitAsk = %q | %q | %q | %q", q, rec, by, def)
	}
	if q, rec, _, _ := splitAsk("needs sudo on catalyst"); q != "needs sudo on catalyst" || rec != "" {
		t.Errorf("plain note = %q %q", q, rec)
	}
}

func TestTaskDue(t *testing.T) {
	now := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	for raw, want := range map[string]string{
		"Thu 12-11":    "2026-11-12",
		"~25-02-2027":  "2027-02-25",
		"Mon 05-01":    "2027-01-05", // early next year, not ten months ago
		"TBD":          "",
		"2026-11-01":   "", // ISO dates come from the tracker parser (it.Due)
		"on Irfan's 3": "",
	} {
		d := taskDue(Item{}, raw, now)
		got := ""
		if d != nil {
			got = d.Format("2006-01-02")
		}
		if got != want {
			t.Errorf("taskDue(%q) = %q, want %q", raw, got, want)
		}
	}
}

// tasksFleet is one workspace whose agent keeps backlogTracker.
func tasksFleet(t *testing.T) (parent, tracker string) {
	t.Helper()
	parent = t.TempDir()
	ws := filepath.Join(parent, "personal")
	write(t, filepath.Join(ws, "agents/CONVENTIONS.md"), "---\nprincipal: Irfan\n---\n")
	write(t, filepath.Join(ws, "agents/friday/context.md"), "---\ntitle: Chief of Staff\n---\n")
	tracker = filepath.Join(ws, "agents/friday/actions.md")
	write(t, tracker, backlogTracker)
	if err := os.Chmod(tracker, 0o640); err != nil {
		t.Fatal(err)
	}
	return parent, tracker
}

var hashRe = regexp.MustCompile(`name="id" value="2">\s*<input type="hidden" name="hash" value="([0-9a-f]+)"`)

var tasksNow = time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC)

func tasksOpts(bool) Options { return Options{Now: tasksNow} }

// follow GETs path and returns where its redirect points.
func follow(t *testing.T, h http.Handler, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, localReq("GET", path, nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("%s = %d, want a redirect", path, rec.Code)
	}
	return rec.Header().Get("Location")
}

func TestTasksOverview(t *testing.T) {
	parent, _ := tasksFleet(t)
	write(t, filepath.Join(parent, "personal/agents/q/context.md"), "---\ntitle: Website Engineer\n---\n")
	write(t, filepath.Join(parent, "personal/agents/q/actions.md"), "# Actions\n\n## Open\n\n| # | Action | Owner | Priority | Status |\n|---|---|---|---|---|\n| 1 | **Build it** | Q | P2 | In progress |\n")
	h := NewWebHandler(WebSource{FleetRoot: parent, Options: tasksOpts})

	code, body := get(t, h, "/tasks?ws=personal")
	if code != http.StatusOK {
		t.Fatalf("/tasks = %d %s", code, body)
	}
	for _, want := range []string{`<b class="tk-sum-n you">1</b>`, `href="/tasks/personal/friday"`, "1 for you", `class="tk-arow quiet" data-href="/tasks/personal/q"`, "Website Engineer", `href="/tasks?ws=personal&amp;view=all"`} {
		if !strings.Contains(body, want) {
			t.Errorf("/tasks lacks %q", want)
		}
	}
	if strings.Contains(body, "Repo cleanup") {
		t.Error("the overview lists agents, not items")
	}
	// friday, with something for the principal, comes before quiet q.
	if strings.Index(body, `href="/tasks/personal/friday"`) > strings.Index(body, `href="/tasks/personal/q"`) {
		t.Error("an agent with something waiting must lead")
	}

	_, body = get(t, h, "/tasks?view=all")
	for _, want := range []string{"Repo cleanup", "Ship it", "Build it"} {
		if !strings.Contains(body, want) {
			t.Errorf("all open lacks %q", want)
		}
	}
	if strings.Contains(body, "Duplicate id in Completed") {
		t.Error("completed rows are never shown")
	}
	if _, body = get(t, h, "/tasks?view=overdue"); !strings.Contains(body, "Nothing is overdue.") {
		t.Error("nothing is overdue on 09-11")
	}
	if got := follow(t, h, "/tasks?view=all&ws=personal&agent=friday"); got != "/tasks/personal/friday" {
		t.Errorf("old agent filter → %q", got)
	}
	if got := follow(t, h, "/tasks?go=/tasks/personal/q"); got != "/tasks/personal/q" {
		t.Errorf("menu → %q", got)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, localReq("GET", "/tasks?go=//evil.example", nil))
	if rec.Code != http.StatusOK {
		t.Error("the menu must not redirect off the site")
	}
}

func TestTasksOverviewDensity(t *testing.T) {
	parent, _ := tasksFleet(t)
	write(t, filepath.Join(parent, "personal/agents/q/context.md"), "---\ntitle: Website Engineer\n---\n")
	write(t, filepath.Join(parent, "personal/agents/q/actions.md"), "# Actions\n\n## Open\n\n| # | Action | Owner | Priority | Status |\n|---|---|---|---|---|\n| 1 | **Build it** | Q | P2 | In progress |\n")
	h := NewWebHandler(WebSource{FleetRoot: parent, Options: tasksOpts})

	// Compact by default: a row per agent, the toggle on Compact.
	_, body := get(t, h, "/tasks?ws=personal")
	for _, want := range []string{`class="tk-arow quiet" data-href="/tasks/personal/friday"`, `href="/tasks?density=compact&amp;ws=personal" class="on"`, `href="/tasks?density=detail&amp;ws=personal"`, "offline"} {
		if !strings.Contains(body, want) {
			t.Errorf("compact lacks %q", want)
		}
	}
	if strings.Contains(body, "tk-dcard") {
		t.Error("compact shows no detail cards")
	}

	// ?density=detail shows the cards and is remembered in a cookie.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, localReq("GET", "/tasks?ws=personal&density=detail", nil))
	body = rec.Body.String()
	var dc *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "tasks_density" {
			dc = c
		}
	}
	if dc == nil || dc.Value != "detail" || dc.Path != "/tasks" || !dc.HttpOnly {
		t.Fatalf("density cookie = %+v", dc)
	}
	for _, want := range []string{`class="tk-dcard quiet" data-href="/tasks/personal/friday"`, `href="/tasks/personal/friday?item=personal%2Ffriday%2F`, `<span class="tk-tag">you</span>`, `class="tk-arow quiet" data-href="/tasks/personal/q"`} {
		if !strings.Contains(body, want) {
			t.Errorf("detail lacks %q:\n%s", want, body)
		}
	}
	req := localReq("GET", "/tasks?ws=personal", nil)
	req.AddCookie(dc)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "tk-dcard") {
		t.Error("the density cookie should keep Detail")
	}
}

func TestTasksOverviewOneWorkspace(t *testing.T) {
	parent, _ := tasksFleet(t)
	write(t, filepath.Join(parent, "alpha/agents/CONVENTIONS.md"), "---\nprincipal: Irfan\n---\n")
	write(t, filepath.Join(parent, "alpha/agents/sigrid/context.md"), "---\ntitle: Lead\n---\n")
	write(t, filepath.Join(parent, "alpha/agents/sigrid/actions.md"), "# Actions\n\n## Open\n\n| # | Action | Owner | Priority | Status |\n|---|---|---|---|---|\n| 1 | **Sign it** | Irfan | P1 | Waiting on Irfan — Sign? Recommend: yes. |\n| 2 | **Other** | Irfan | P2 | Waiting on Irfan — Now? |\n")
	h := NewWebHandler(WebSource{FleetRoot: parent, Options: tasksOpts})
	card := func(body, href string) bool {
		return regexp.MustCompile(`class="tk-(?:arow|dcard)[^"]*" data-href="` + regexp.QuoteMeta(href) + `"`).MatchString(body)
	}

	// First visit: the first workspace, alpha, and only its cards.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, localReq("GET", "/tasks", nil))
	body := rec.Body.String()
	if !card(body, "/tasks/alpha/sigrid") || card(body, "/tasks/personal/friday") ||
		!strings.Contains(body, `<b class="tk-sum-n you">2</b>`) || !strings.Contains(body, `href="/tasks/one?ws=alpha"`) {
		t.Errorf("first visit should show alpha only:\n%s", body)
	}
	for _, want := range []string{`href="/tasks?ws=alpha" class="current"`, `href="/tasks?ws=personal"`, `href="/tasks?ws=all"`} {
		if !strings.Contains(body, want) {
			t.Errorf("switcher lacks %q", want)
		}
	}

	// Picking personal shows it, and is remembered by the cookie.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, localReq("GET", "/tasks?ws=personal", nil))
	if card(rec.Body.String(), "/tasks/alpha/sigrid") || !strings.Contains(rec.Body.String(), `<b class="tk-sum-n you">1</b>`) {
		t.Error("?ws=personal should show personal only")
	}
	req := localReq("GET", "/tasks", nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `href="/tasks?ws=personal" class="current"`) {
		t.Error("the cookie should bring back personal")
	}
	// All shows both, and its One by one is global.
	_, body = get(t, h, "/tasks?ws=all")
	if !card(body, "/tasks/alpha/sigrid") || !card(body, "/tasks/personal/friday") || !strings.Contains(body, `<b class="tk-sum-n you">3</b>`) || !strings.Contains(body, `href="/tasks/one"`) {
		t.Errorf("all:\n%s", body)
	}
	// The sidebar follows the workspace: its views count only its items,
	// and only its agents are listed.
	side := func(body string) string {
		a, b := strings.Index(body, `<nav class="tk-side"`), strings.Index(body, "</nav>")
		return body[a:b]
	}
	_, body = get(t, h, "/tasks?ws=alpha")
	if sb := side(body); !strings.Contains(sb, `href="/tasks/one?ws=alpha"`) || !strings.Contains(sb, `<span>One by one</span><span class="tk-n">2</span>`) ||
		!strings.Contains(sb, "/tasks/alpha/sigrid") || strings.Contains(sb, "/tasks/personal/friday") {
		t.Errorf("alpha sidebar:\n%s", sb)
	}
	_, body = get(t, h, "/tasks?ws=all")
	if sb := side(body); !strings.Contains(sb, "/tasks/alpha/sigrid") || !strings.Contains(sb, "/tasks/personal/friday") || !strings.Contains(sb, `<span>One by one</span><span class="tk-n">3</span>`) {
		t.Errorf("all sidebar:\n%s", sb)
	}
	// An agent page shows its own workspace; a cookie for alpha carries to
	// the lists.
	_, body = get(t, h, "/tasks/personal/friday")
	if sb := side(body); strings.Contains(sb, "/tasks/alpha/sigrid") || !strings.Contains(sb, "/tasks/personal/friday") {
		t.Errorf("agent page sidebar:\n%s", sb)
	}
	req = localReq("GET", "/tasks?view=all", nil)
	req.AddCookie(&http.Cookie{Name: wsCookie, Value: "alpha"})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if sb := side(rec.Body.String()); strings.Contains(sb, "/tasks/personal/friday") || !strings.Contains(rec.Body.String(), "Sign it") || strings.Contains(rec.Body.String(), "Repo cleanup") {
		t.Errorf("list with the alpha cookie:\n%s", sb)
	}

	// The overview's list links keep the workspace.
	_, body = get(t, h, "/tasks?ws=alpha")
	if !strings.Contains(body, `href="/tasks?ws=alpha&amp;view=all"`) {
		t.Error("the all-open link should keep the workspace")
	}
	if _, body = get(t, h, "/tasks?ws=alpha&view=all"); !strings.Contains(body, "Sign it") || strings.Contains(body, "Repo cleanup") {
		t.Error("a list in alpha shows alpha's items only")
	}
	// One by one in a workspace walks only its items.
	if got := follow(t, h, "/tasks/one?ws=alpha"); !strings.Contains(got, "alpha%3Asigrid%3A1%2Calpha%3Asigrid%3A2") || strings.Contains(got, "personal") {
		t.Errorf("alpha run = %q", got)
	}
}

func TestTasksAgentPage(t *testing.T) {
	parent, _ := tasksFleet(t)
	h := NewWebHandler(WebSource{FleetRoot: parent, Options: tasksOpts})
	code, body := get(t, h, "/tasks/personal/friday")
	if code != http.StatusOK {
		t.Fatalf("agent page = %d %s", code, body)
	}
	if !strings.Contains(body, `href="/tasks/one?agent=friday&amp;ws=personal"`) || !strings.Contains(body, `One by one<span class="tk-badge">4</span>`) || !strings.Contains(body, `href="/tasks/personal/friday?view=chat" data-view="chat"`) {
		t.Errorf("no sweep button:\n%s", body)
	}
	last := -1
	for _, g := range []string{"Waiting on you", "In progress", "Not started"} {
		i := strings.Index(body, "<h2>"+g)
		if i < 0 || i < last {
			t.Errorf("group %q missing or out of order", g)
		}
		last = i
	}
	// The dots are explained: a legend, and the answered chip.
	if !strings.Contains(body, `class="tk-legend"`) {
		t.Error("the agent page needs the dot legend")
	}
	// Every opened row has a close control; every form a Cancel.
	if strings.Count(body, `class="tk-x" data-close`) != strings.Count(body, `<details class="tk-row"`) || strings.Count(body, "data-cancel") == 0 {
		t.Error("rows need a close control and forms a Cancel")
	}
	if code, _ := get(t, h, "/tasks/personal/nobody"); code != http.StatusNotFound {
		t.Errorf("unknown agent = %d", code)
	}
}

func TestSweepOrder(t *testing.T) {
	d := func(day int) *time.Time { t := time.Date(2026, 11, day, 0, 0, 0, 0, time.UTC); return &t }
	row := func(id, pri, dot string, mine, late bool, due *time.Time) taskRow {
		tr := taskRow{Dot: dot, Mine: mine, Late: late, due: due}
		tr.ID, tr.Priority = id, pri
		tr.rank = priorityRank(pri)*10 + btoi(!late)
		return tr
	}
	rows := []taskRow{
		row("parked", "P1", "parked", false, false, nil),
		row("p3", "P3", "todo", false, false, nil),
		row("late", "P2", "progress", false, true, d(1)),
		row("p1", "P1", "progress", false, false, d(20)),
		row("mineP2", "P2", "me", true, false, nil),
		row("mineP1", "P1", "me", true, false, nil),
		row("p1soon", "P1", "todo", false, false, d(10)),
	}
	sweepOrder(rows)
	var got []string
	for _, r := range rows {
		got = append(got, r.ID)
	}
	if want := "mineP1 mineP2 late p1soon p1 p3 parked"; strings.Join(got, " ") != want {
		t.Errorf("order = %v, want %s", got, want)
	}
}

func TestTasksOneByOne(t *testing.T) {
	parent, tracker := tasksFleet(t)
	var sent [][2]string
	live := func(bool) Options {
		return Options{Now: tasksNow, Herdr: func() ([]LivePane, error) {
			return []LivePane{{Name: "friday", PaneID: "w1:p3", CWD: filepath.Join(parent, "personal"), Agent: "claude", Status: "idle", Running: true}}, nil
		}}
	}
	h := NewWebHandler(WebSource{FleetRoot: parent, Options: live,
		Prompt: func(pane, text string) error { sent = append(sent, [2]string{pane, text}); return nil }})

	// The global run is what waits on the principal; an agent's run is all
	// its open items, that first.
	if got := follow(t, h, "/tasks/one"); !strings.HasSuffix(got, "run=personal%3Afriday%3A7") {
		t.Errorf("global run = %q", got)
	}
	start := follow(t, h, "/tasks/one?ws=personal&agent=friday")
	u, _ := url.Parse(start)
	if run := u.Query().Get("run"); run != "personal:friday:7,personal:friday:12,personal:friday:2,personal:friday:13" {
		t.Errorf("agent run = %q", run)
	}
	_, body := get(t, h, start)
	for _, want := range []string{"1 of 4", "Ask Irfan", "Do it?", "Yes, as recommended", `id="decide"`, `action="/tasks/answer"`, "done=a0", "done=e0", `data-open="do"`, "Skip"} {
		if !strings.Contains(body, want) {
			t.Errorf("first card lacks %q", want)
		}
	}

	// A reply to an item that does not wait on the principal still goes to
	// the inbox; the tracker is untouched.
	if code, msg := post(t, h, "/tasks/answer", url.Values{"ws": {"personal"}, "agent": {"friday"}, "id": {"12"}, "text": {"Ship Friday."}}, "X-GoAgentic", "1"); code != http.StatusNoContent {
		t.Fatalf("answer = %d %s", code, msg)
	}
	if data, _ := os.ReadFile(tracker); string(data) != backlogTracker {
		t.Error("an answer must not edit the tracker")
	}
	inbox, _ := os.ReadFile(filepath.Join(filepath.Dir(tracker), "memory/scheduled/inbox.md"))
	if !strings.Contains(string(inbox), "answer #12") || !strings.Contains(string(inbox), "> Ship Friday.") {
		t.Errorf("inbox:\n%s", inbox)
	}
	if code, _ := post(t, h, "/tasks/answer", url.Values{"ws": {"personal"}, "agent": {"friday"}, "id": {"99"}, "text": {"x"}}, "X-GoAgentic", "1"); code != http.StatusBadRequest {
		t.Errorf("answer to a closed item = %d", code)
	}

	// The second card shows the answer waiting to be applied.
	q := u.Query()
	q.Set("n", "1")
	q.Set("done", "a1")
	_, body = get(t, h, "/tasks/one?"+q.Encode())
	if _, ap := get(t, h, "/tasks/personal/friday"); !strings.Contains(ap, `<span class="tk-chip ok">Answered</span>`) {
		t.Error("an answered row needs the Answered chip")
	}
	if !strings.Contains(body, "2 of 4") || !strings.Contains(body, "Answered, waiting for friday to apply") {
		t.Errorf("second card:\n%s", body)
	}

	// The end: what was answered, and one button to tell the live agent.
	q.Set("n", "4")
	_, body = get(t, h, "/tasks/one?"+q.Encode())
	for _, want := range []string{"Done.", `<span class="tk-what answered">answered</span><span>Ship it</span>`, `<span class="tk-what skipped">skipped</span>`, "Tell friday", `action="/w/personal/friday/send"`, "You have 1 new answer in your inbox; apply it."} {
		if !strings.Contains(body, want) {
			t.Errorf("end lacks %q:\n%s", want, body)
		}
	}
	if code, msg := post(t, h, "/w/personal/friday/send", url.Values{"text": {"You have 1 new answer in your inbox; apply it."}}, "X-GoAgentic", "1"); code != http.StatusNoContent || len(sent) != 1 || sent[0][0] != "w1:p3" {
		t.Errorf("tell = %d %s %v", code, msg, sent)
	}
	// The agent page shows the same button while answers wait.
	if _, body = get(t, h, "/tasks/personal/friday"); !strings.Contains(body, "Tell friday") {
		t.Error("the agent page lacks the Tell button")
	}
}

func TestTasksStartWhenNotRunning(t *testing.T) {
	parent, tracker := tasksFleet(t)
	write(t, filepath.Join(filepath.Dir(tracker), "memory/scheduled/inbox.md"),
		"# Inbox\n\n## 2026-11-09 10:00 — answer #12 (Irfan, via dash) — UNPROCESSED\n\n> Yes.\n")
	none := func(bool) Options {
		return Options{Now: tasksNow, Herdr: func() ([]LivePane, error) { return nil, nil }}
	}
	h := NewWebHandler(WebSource{FleetRoot: parent, Options: none})
	_, body := get(t, h, "/tasks/personal/friday")
	if !strings.Contains(body, "is not running") || strings.Contains(body, "Tell friday") {
		t.Errorf("a stopped agent's page:\n%s", body)
	}
	if strings.Contains(body, "Start friday to apply") && !strings.Contains(body, `action="/w/personal/friday/start"`) {
		t.Error("the Start button must post to the start route")
	}
	// Without Herdr's word, offer neither.
	h = NewWebHandler(WebSource{FleetRoot: parent, Options: tasksOpts})
	if _, body = get(t, h, "/tasks/personal/friday"); strings.Contains(body, "Start friday") || strings.Contains(body, "Tell friday") {
		t.Error("with liveness unknown, no button")
	}
}

func TestTasksEdit(t *testing.T) {
	parent, tracker := tasksFleet(t)
	h := NewWebHandler(WebSource{FleetRoot: parent, Options: tasksOpts})
	_, body := get(t, h, "/tasks?view=all")
	m := hashRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no edit form for #2:\n%s", body)
	}
	form := url.Values{"ws": {"personal"}, "agent": {"friday"}, "id": {"2"}, "hash": {m[1]}, "back": {"/tasks?view=all"},
		"state": {StateInProgress}, "who": {""}, "note": {"listing"}, "priority": {"P2"}, "due": {"TBD"}, "owner": {"Friday"}}

	// The guard: no header, no write.
	if code, _ := post(t, h, "/tasks/edit", form); code != http.StatusForbidden {
		t.Errorf("edit without the header = %d, want 403", code)
	}
	stale := url.Values{}
	for k, v := range form {
		stale[k] = v
	}
	stale.Set("hash", "deadbeef")
	if code, body := post(t, h, "/tasks/edit", stale, "X-GoAgentic", "1"); code != http.StatusBadRequest || !strings.Contains(body, "changed") {
		t.Errorf("stale edit = %d %s", code, body)
	}
	bad := url.Values{}
	for k, v := range form {
		bad[k] = v
	}
	bad.Set("state", StateWaiting)
	if code, body := post(t, h, "/tasks/edit", bad, "X-GoAgentic", "1"); code != http.StatusBadRequest || !strings.Contains(body, "who") {
		t.Errorf("waiting on nobody = %d %s", code, body)
	}
	if data, _ := os.ReadFile(tracker); string(data) != backlogTracker {
		t.Fatal("a refused edit wrote the tracker")
	}

	req := localReq("POST", "/tasks/edit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-GoAgentic", "1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/tasks?view=all" {
		t.Fatalf("edit = %d %q %s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	data, _ := os.ReadFile(tracker)
	changed := diffLines(t, backlogTracker, string(data))
	if len(changed) != 1 || !strings.Contains(string(data), "| Friday | P2 | TBD | In progress — listing | 2026-09-25 |") {
		t.Errorf("tracker after edit:\n%s", data)
	}
	if info, _ := os.Stat(tracker); info.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, want 0640 kept", info.Mode().Perm())
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(tracker), ".actions.md.tmp-*")); len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}
	// The old hash is now stale.
	if code, _ := post(t, h, "/tasks/edit", form, "X-GoAgentic", "1"); code != http.StatusBadRequest {
		t.Errorf("second edit with the old hash = %d, want 400", code)
	}
}

func TestAgentAvatar(t *testing.T) {
	parent, tracker := tasksFleet(t)
	dir := filepath.Dir(tracker)
	write(t, filepath.Join(parent, "personal/agents/q/context.md"), "---\ntitle: Web\n---\n")
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 128 128"><rect width="128" height="128" rx="28" fill="#eee"/></svg>`
	write(t, filepath.Join(dir, "avatar.svg"), svg)
	write(t, filepath.Join(dir, "avatar.png"), "not chosen: svg comes first")
	h := NewWebHandler(WebSource{FleetRoot: parent, Options: tasksOpts})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, localReq("GET", "/w/personal/friday/avatar", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != svg {
		t.Fatalf("avatar = %d %q", rec.Code, rec.Body.String())
	}
	hd := rec.Header()
	if hd.Get("Content-Type") != "image/svg+xml" || hd.Get("X-Content-Type-Options") != "nosniff" ||
		hd.Get("Content-Security-Policy") != "default-src 'none'; style-src 'unsafe-inline'" {
		t.Errorf("headers = %v", hd)
	}
	// Shown in the Tasks tiles.
	_, body := get(t, h, "/tasks?ws=personal")
	if !regexp.MustCompile(`<img class="tk-av img [^"]*" src="/w/personal/friday/avatar\?v=\w+"`).MatchString(body) {
		t.Errorf("overview lacks the image tile")
	}
	if !regexp.MustCompile(`<span class="tk-av [a-z]*" style="--h:`).MatchString(body) {
		t.Error("an agent without an image keeps its initial tile")
	}

	// No image, an unknown agent, a file too big, a link: not served.
	for _, path := range []string{"/w/personal/q/avatar", "/w/personal/nobody/avatar", "/w/nowhere/friday/avatar"} {
		if code, _ := get(t, h, path); code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", path, code)
		}
	}
	os.Remove(filepath.Join(dir, "avatar.png"))
	write(t, filepath.Join(dir, "avatar.svg"), strings.Repeat("x", maxAvatar+1))
	if code, _ := get(t, h, "/w/personal/friday/avatar"); code != http.StatusNotFound {
		t.Errorf("an oversized avatar = %d, want 404", code)
	}
	if _, body = get(t, h, "/tasks?ws=personal"); strings.Contains(body, "/avatar?v=") {
		t.Error("an oversized avatar must fall back to the initial")
	}
	os.Remove(filepath.Join(dir, "avatar.svg"))
	outside := filepath.Join(parent, "secret.png")
	write(t, outside, "secret")
	if err := os.Symlink(outside, filepath.Join(dir, "avatar.png")); err == nil {
		if code, _ := get(t, h, "/w/personal/friday/avatar"); code != http.StatusNotFound {
			t.Errorf("a linked avatar = %d, want 404", code)
		}
	}
}

func TestTasksTheme(t *testing.T) {
	parent, _ := tasksFleet(t)
	h := NewWebHandler(WebSource{FleetRoot: parent, Options: tasksOpts})
	_, body := get(t, h, "/tasks")
	if !strings.Contains(body, `<html lang="en" data-theme="system">`) || !strings.Contains(body, `data-theme-pick="system" aria-label="System" title="System" aria-pressed="true"`) {
		t.Error("System is the default")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, localReq("GET", "/tasks/theme?t=dark&back=/tasks/one", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/tasks/one" {
		t.Fatalf("theme = %d %q", rec.Code, rec.Header().Get("Location"))
	}
	req := localReq("GET", "/tasks/personal/friday", nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if b := rec.Body.String(); !strings.Contains(b, `data-theme="dark"`) || !strings.Contains(b, `data-theme-pick="dark" aria-label="Dark" title="Dark" aria-pressed="true"`) {
		t.Error("the dark cookie should set data-theme on the root")
	}
	if got := follow(t, h, "/tasks/theme?t=light&back=//evil.example"); got != "/tasks" {
		t.Errorf("off-site back = %q", got)
	}
}

func TestTasksDetailPane(t *testing.T) {
	parent, _ := tasksFleet(t)
	h := NewWebHandler(WebSource{FleetRoot: parent, Options: tasksOpts})

	// No item: the pane is there, empty; rows link to it.
	_, body := get(t, h, "/tasks/personal/friday")
	if !strings.Contains(body, `class="tasks has-pane view-tasks"`) || !strings.Contains(body, "Select a task to see it here.") ||
		!strings.Contains(body, `data-href="/tasks/personal/friday?item=personal%2Ffriday%2F7"`) {
		t.Errorf("agent page without an item:\n%s", body)
	}
	// An item: its title, properties and the edit form, the row selected.
	_, body = get(t, h, "/tasks/personal/friday?item=personal/friday/12")
	for _, want := range []string{`class="tasks has-pane pane-open view-tasks"`, `<h2 class="tk-ptitle">Ship it</h2>`, `<span>Since</span><span class="v">2026-11-08</span>`,
		`id="pedit"`, `name="back" value="/tasks/personal/friday?item=personal/friday/12"`, `class="tk-row selected" data-key="personal/friday/12"`,
		`<span class="tk-due">Thu 12 Nov</span>`, `href="/tasks/personal/friday"` + ` data-pane-close`} {
		if !strings.Contains(body, want) {
			t.Errorf("pane lacks %q", want)
		}
	}
	// The same on a flat list, and an unknown item leaves the pane empty.
	_, body = get(t, h, "/tasks?ws=personal&view=all&item=personal/friday/2")
	if !strings.Contains(body, `<h2 class="tk-ptitle">Repo cleanup</h2>`) {
		t.Error("the list's pane should show the item")
	}
	if _, body = get(t, h, "/tasks/personal/friday?item=personal/friday/99"); strings.Contains(body, `has-pane pane-open"`) {
		t.Error("an unknown item opens nothing")
	}
}

func TestTasksChatTab(t *testing.T) {
	parent, _ := tasksFleet(t)
	live := func(bool) Options {
		return Options{Now: tasksNow, Herdr: func() ([]LivePane, error) {
			return []LivePane{{Name: "friday", PaneID: "w1:p3", CWD: filepath.Join(parent, "personal"), Agent: "claude", Status: "idle", Running: true}}, nil
		}}
	}
	h := NewWebHandler(WebSource{FleetRoot: parent, Options: live})
	_, body := get(t, h, "/tasks/personal/friday?view=chat")
	if !strings.Contains(body, `action="/w/personal/friday/send"`) || !strings.Contains(body, `data-view="chat" class="vs-chat on" aria-current="page">`) {
		t.Errorf("a live agent's chat tab:\n%s", body)
	}
	none := func(bool) Options {
		return Options{Now: tasksNow, Herdr: func() ([]LivePane, error) { return nil, nil }}
	}
	h = NewWebHandler(WebSource{FleetRoot: parent, Options: none})
	if _, body = get(t, h, "/tasks/personal/friday?view=chat"); !strings.Contains(body, "is not running.") || strings.Contains(body, "/send") {
		t.Errorf("a stopped agent's chat tab:\n%s", body)
	}
}

func TestDueLabel(t *testing.T) {
	today := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	d := func(y, m, day int) *time.Time { t := time.Date(y, time.Month(m), day, 0, 0, 0, 0, time.UTC); return &t }
	for _, tc := range []struct {
		due    *time.Time
		parked bool
		want   string
	}{
		{d(2026, 11, 9), false, "Today"},
		{d(2026, 11, 10), false, "Tomorrow"},
		{d(2026, 11, 6), false, "Overdue 3d"},
		{d(2026, 11, 6), true, "Fri 6 Nov"},
		{d(2026, 11, 16), false, "Mon 16 Nov"},
		{d(2027, 2, 25), false, "Thu 25 Feb 2027"},
		{nil, false, "TBD"},
	} {
		if got := dueLabel(tc.due, "TBD", today, tc.parked); got != tc.want {
			t.Errorf("dueLabel(%v) = %q, want %q", tc.due, got, tc.want)
		}
	}
	// An empty pane is not shown: the list takes the room.
	parent, _ := tasksFleet(t)
	h := NewWebHandler(WebSource{FleetRoot: parent, Options: tasksOpts})
	if _, body := get(t, h, "/tasks/personal/friday"); strings.Contains(body, `has-pane pane-open"`) {
		t.Error("no item, no open pane")
	}
}

func TestPaneMarkdown(t *testing.T) {
	in := "Two things:\n\n1. **Empty pane** goes.\n2. Heavy bar, *calmer*.\n\n- one\n- `two`\n\nSee [docs](https://example.com).\n<script>alert(1)</script>\n```\ncode <b>\n```"
	got := string(paneMarkdown(in, nil))
	for _, want := range []string{"<p>Two things:</p>", "<ol><li><strong>Empty pane</strong> goes.</li><li>Heavy bar, <em>calmer</em>.</li></ol>",
		"<ul><li>one</li><li><code>two</code></li></ul>", `<a href="https://example.com" rel="noreferrer" target="_blank">docs</a>`,
		"&lt;script&gt;alert(1)&lt;/script&gt;", "<pre><code>code &lt;b&gt;</code></pre>"} {
		if !strings.Contains(got, want) {
			t.Errorf("markdown lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "<script>") || strings.Contains(got, "<b>") {
		t.Error("raw HTML must never pass through")
	}
}

// png is a tiny real PNG.
var pngBytes = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\x00\x01\x00\x00\x05\x00\x01\r\n-\xb4\x00\x00\x00\x00IEND\xaeB`\x82")

func TestChatImageRoute(t *testing.T) {
	parent, _ := tasksFleet(t)
	root := t.TempDir()
	outside := t.TempDir()
	ok := filepath.Join(root, "shot.png")
	os.WriteFile(ok, pngBytes, 0o600)
	os.WriteFile(filepath.Join(root, "Upper.PNG"), pngBytes, 0o600)
	os.WriteFile(filepath.Join(root, "notes.txt"), []byte("secret"), 0o600)
	os.WriteFile(filepath.Join(root, "fake.png"), []byte("not an image at all"), 0o600)
	os.WriteFile(filepath.Join(outside, "secret.png"), pngBytes, 0o600)
	os.Symlink(filepath.Join(outside, "secret.png"), filepath.Join(root, "escape.png"))
	os.Symlink(filepath.Join(root, "notes.txt"), filepath.Join(root, "renamed.png"))
	big := filepath.Join(root, "big.png")
	os.WriteFile(big, append(append([]byte{}, pngBytes...), make([]byte, maxImageBytes)...), 0o600)
	h := NewWebHandler(WebSource{FleetRoot: parent, Options: tasksOpts, ImageRoots: []string{root}})

	img := func(p string) (int, http.Header, string) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, localReq("GET", "/img?"+url.Values{"p": {p}}.Encode(), nil))
		return rec.Code, rec.Header(), rec.Body.String()
	}
	code, hd, body := img(ok)
	if code != http.StatusOK || hd.Get("Content-Type") != "image/png" || hd.Get("X-Content-Type-Options") != "nosniff" || body != string(pngBytes) {
		t.Fatalf("allowed image = %d %v", code, hd)
	}
	if code, _, _ := img(filepath.Join(root, "Upper.PNG")); code != http.StatusOK {
		t.Error("extension case should not matter")
	}
	for name, p := range map[string]string{
		"dot-dot":           root + "/../" + filepath.Base(outside) + "/secret.png",
		"outside":           filepath.Join(outside, "secret.png"),
		"symlink escape":    filepath.Join(root, "escape.png"),
		"link to a non-img": filepath.Join(root, "renamed.png"),
		"wrong extension":   filepath.Join(root, "notes.txt"),
		"not really a png":  filepath.Join(root, "fake.png"),
		"too big":           big,
		"relative":          "shot.png",
		"the root itself":   root,
		"missing":           filepath.Join(root, "nope.png"),
		"empty":             "",
	} {
		if code, _, _ := img(p); code != http.StatusNotFound {
			t.Errorf("%s (%s) = %d, want 404", name, p, code)
		}
	}
	// The guard applies: an unexpected Host is refused.
	req := httptest.NewRequest("GET", "/img?"+url.Values{"p": {ok}}.Encode(), nil)
	req.Host = "evil.example"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("foreign host = %d", rec.Code)
	}

	// Rendering: allowed images become thumbnails, others stay text.
	s := &webServer{src: WebSource{ImageRoots: []string{root}}}
	md := string(paneMarkdown("Look: ![the shot]("+ok+") and ![no]("+filepath.Join(outside, "secret.png")+")", s.chatImageURL))
	if !strings.Contains(md, `<a class="tk-img" href="/img?p=`) || !strings.Contains(md, `alt="the shot"`) || !strings.Contains(md, "![no](") {
		t.Errorf("markdown images:\n%s", md)
	}
	text, links := s.t3Images("Here\n[Attached image \"image.png\" is saved at: " + ok + "]\n[Attached image \"x.png\" is saved at: /etc/x.png]")
	t3 := "Look [Image: image.png; ref=image_26aa] here\n\n<t3_context version=\"1\">\n<context kind=\"image\">\nname: image.png\n</context>\n</t3_context>"
	if got, _ := s.t3Images(t3); got != "Look here" {
		t.Errorf("t3 bookkeeping left in: %q", got)
	}
	if len(links) != 1 || strings.Contains(text, ok) || !strings.Contains(text, "/etc/x.png") {
		t.Errorf("t3 images = %q %v", text, links)
	}
}

func TestTasksLiveAndBoth(t *testing.T) {
	parent, _ := tasksFleet(t)
	live := func(bool) Options {
		return Options{Now: tasksNow, Herdr: func() ([]LivePane, error) {
			return []LivePane{{Name: "friday", PaneID: "w1:p3", CWD: filepath.Join(parent, "personal"), Agent: "claude", Status: "working", Running: true}}, nil
		}}
	}
	h := NewWebHandler(WebSource{FleetRoot: parent, Options: live})
	_, body := get(t, h, "/tasks/personal/friday")
	for _, want := range []string{`<span class="tk-livepill working">Live · working</span>`, `<i class="tk-presence working" title="Live · working">`, `data-view="both"`, `data-agent="personal/friday"`} {
		if !strings.Contains(body, want) {
			t.Errorf("live agent page lacks %q", want)
		}
	}
	// Chat fills the page: no task list, the session and its composer.
	_, body = get(t, h, "/tasks/personal/friday?view=chat")
	if !strings.Contains(body, `view-chat`) || !strings.Contains(body, `<section class="tk-chatmain">`) || strings.Contains(body, `class="tk-listcard"`) || !strings.Contains(body, "friday is working…") {
		t.Errorf("chat view:\n%s", body)
	}
	// Both: the list and the chat side by side; a task opens over the list.
	_, body = get(t, h, "/tasks/personal/friday?view=both&item=personal/friday/12")
	for _, want := range []string{`pane-open view-both`, `<div class="tk-listcol">`, `<h2 class="tk-ptitle">Ship it</h2>`, `<section class="tk-chatcol">`, `class="tk-split" role="separator" aria-orientation="vertical" tabindex="0"`,
		`data-href="/tasks/personal/friday?item=personal%2Ffriday%2F7&amp;view=both"`} {
		if !strings.Contains(body, want) {
			t.Errorf("both view lacks %q", want)
		}
	}
	if strings.Index(body, `<h2 class="tk-ptitle">`) > strings.Index(body, `<section class="tk-chatcol">`) {
		t.Error("in Both the task opens in the list column, before the chat")
	}
	// Off: grey dots, no pill.
	h = NewWebHandler(WebSource{FleetRoot: parent, Options: tasksOpts})
	if _, body = get(t, h, "/tasks/personal/friday"); strings.Contains(body, `class="tk-livepill`) || !strings.Contains(body, `<i class="tk-presence off"`) {
		t.Error("a stopped agent shows a grey dot and no pill")
	}
}

func TestTasksHeaderAndSearch(t *testing.T) {
	parent, _ := tasksFleet(t)
	write(t, filepath.Join(parent, "alpha/agents/CONVENTIONS.md"), "---\nprincipal: Irfan\n---\n")
	write(t, filepath.Join(parent, "alpha/agents/sigrid/context.md"), "---\ntitle: Lead\n---\n")
	write(t, filepath.Join(parent, "alpha/agents/sigrid/actions.md"), "# Actions\n\n## Open\n\n| # | Action | Owner | Priority | Status |\n|---|---|---|---|---|\n| 1 | **Ship the alpha cleanup** | Sigrid | P1 | In progress |\n")
	live := func(bool) Options {
		return Options{Now: tasksNow, Herdr: func() ([]LivePane, error) {
			return []LivePane{{Name: "friday", PaneID: "w1:p3", CWD: filepath.Join(parent, "personal"), Agent: "claude", Status: "idle", Running: true}}, nil
		}}
	}
	h := NewWebHandler(WebSource{FleetRoot: parent, Options: live})
	_, body := get(t, h, "/tasks/personal/friday")
	for _, want := range []string{`<header class="tk-top">`, `<summary>Personal<svg`, `href="/tasks?ws=all"`, `action="/tasks/search"`, `name="ws" value="personal"`,
		`1 live</summary>`, `<a href="/">Classic view</a>`, `<nav class="tk-crumbs" aria-label="Breadcrumb"><a href="/tasks?ws=personal">Personal</a><span aria-hidden="true">/</span><span aria-current="page">friday</span></nav>`} {
		if !strings.Contains(body, want) {
			t.Errorf("header lacks %q", want)
		}
	}
	if strings.Contains(body, "tk-back") || strings.Contains(body, `class="tk-switch"`) {
		t.Error("back links and the switcher strip are gone")
	}

	// Search: items and agents in the space shown; All finds both spaces.
	_, body = get(t, h, "/tasks/search?ws=personal&q=cleanup")
	if !strings.Contains(body, "<b>Repo cleanup</b>") || strings.Contains(body, "Ship the alpha cleanup") {
		t.Errorf("personal search:\n%s", body)
	}
	_, body = get(t, h, "/tasks/search?ws=all&q=cleanup&format=json")
	var hits []searchHit
	if err := json.Unmarshal([]byte(body), &hits); err != nil || len(hits) != 2 {
		t.Fatalf("all search = %v %s", err, body)
	}
	if hits[0].Href == "" || !strings.Contains(hits[0].Href+hits[1].Href, "item=personal%2Ffriday%2F2") {
		t.Errorf("hits = %+v", hits)
	}
	_, body = get(t, h, "/tasks/search?ws=personal&q=friday&format=json")
	if !strings.Contains(body, `"kind":"agent"`) {
		t.Errorf("an agent's name should find the agent: %s", body)
	}
	if _, body = get(t, h, "/tasks/search?ws=personal&q=zzzz"); !strings.Contains(body, "Nothing found.") {
		t.Error("no hits says so")
	}
}

func TestMoveToCompleted(t *testing.T) {
	today := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	out, err := MoveToCompleted(backlogTracker, "12", today)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "| 12 | **Ship it** — the thing, with `a\\|b` escaped | | Friday | P1") {
		t.Error("the open row must leave the Open table")
	}
	want := "| 12 | **Ship it** — the thing, with `a\\|b` escaped | | Friday | 2026-11-09 |"
	if !strings.Contains(out, "|-----------|\n"+want+"\n| 1 | Done thing |") {
		t.Errorf("the row must head the Completed table:\n%s", out)
	}
	// Every other line is as it was.
	inLines, outLines := strings.Split(backlogTracker, "\n"), strings.Split(out, "\n")
	if len(outLines) != len(inLines) {
		t.Fatalf("line count %d → %d", len(inLines), len(outLines))
	}
	var rest []string
	for _, l := range outLines {
		if l != want {
			rest = append(rest, l)
		}
	}
	var orig []string
	for _, l := range inLines {
		if !strings.HasPrefix(l, "| 12 |") {
			orig = append(orig, l)
		}
	}
	if strings.Join(rest, "\n") != strings.Join(orig, "\n") {
		t.Error("bytes outside the moved row changed")
	}
	if r, err := findOpenRow(out, "13"); err != nil || r.get("#") != "13" {
		t.Error("the other open rows are still found")
	}

	// A day-first Completed table gets a day-first date; a ticket column
	// carries over by name; the first sentence only.
	dayFirst := strings.Replace(backlogTracker, "| 1 | Done thing | | Friday | 2026-09-01 |", "| 1 | Done thing | | Friday | 01-09-2026 |", 1)
	dayFirst = strings.Replace(dayFirst, "| 2 | **Repo cleanup** — list, verdict, one commit | |", "| 2 | **Repo cleanup** — Lists the old folders. Then one commit. | GOA-7 |", 1)
	out, err = MoveToCompleted(dayFirst, "#2", today)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| 2 | **Repo cleanup** — Lists the old folders. | GOA-7 | Friday | 09-11-2026 |") {
		t.Errorf("day-first move:\n%s", out)
	}

	for name, tc := range map[string]struct{ text, id, want string }{
		"no Completed table": {strings.Split(backlogTracker, "## Completed")[0], "2", "no Completed table"},
		"not open":           {backlogTracker, "1", "not a row"},
		"duplicate":          {strings.Replace(backlogTracker, "| 13 |", "| 12 |", 1), "12", "2 times"},
		"bad row":            {backlogTracker, "13", "cells"},
	} {
		if _, err := MoveToCompleted(tc.text, tc.id, today); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
	crlf := strings.ReplaceAll(backlogTracker, "\n", "\r\n")
	out, err = MoveToCompleted(crlf, "2", today)
	if err != nil || !strings.Contains(out, "| 2 | **Repo cleanup** — list, verdict, one commit | | Friday | 2026-11-09 |\r\n| 1 | Done thing") {
		t.Errorf("CRLF move = %v\n%q", err, out)
	}
}

// fetchPost posts as the page's script does, and returns the undo id.
func fetchPost(t *testing.T, h http.Handler, path string, form url.Values) (int, string, string) {
	t.Helper()
	req := localReq("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-GoAgentic", "1")
	req.Header.Set("X-Tasks-Fetch", "1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Header().Get("X-Tasks-Undo"), rec.Body.String()
}

func TestTasksDoneAndUndo(t *testing.T) {
	parent, tracker := tasksFleet(t)
	h := NewWebHandler(WebSource{FleetRoot: parent, Options: tasksOpts, Now: func() time.Time { return tasksNow }})
	hash := trackerHash([]byte(backlogTracker))

	// Done moves the row; Undo puts the file back as it was.
	code, undo, body := fetchPost(t, h, "/tasks/done", url.Values{"ws": {"personal"}, "agent": {"friday"}, "id": {"2"}, "hash": {hash}})
	if code != http.StatusNoContent || undo == "" {
		t.Fatalf("done = %d %q %s", code, undo, body)
	}
	data, _ := os.ReadFile(tracker)
	if !strings.Contains(string(data), "|-----------|\n| 2 | **Repo cleanup** — list, verdict, one commit | | Friday | 2026-11-09 |") {
		t.Errorf("after Done:\n%s", data)
	}
	if code, _, body := fetchPost(t, h, "/tasks/done", url.Values{"ws": {"personal"}, "agent": {"friday"}, "id": {"13"}, "hash": {hash}}); code != http.StatusBadRequest || !strings.Contains(body, "changed") {
		t.Errorf("Done with a stale hash = %d %s", code, body)
	}
	if code, _, body := fetchPost(t, h, "/tasks/undo", url.Values{"id": {undo}}); code != http.StatusNoContent {
		t.Fatalf("undo = %d %s", code, body)
	}
	if data, _ := os.ReadFile(tracker); string(data) != backlogTracker {
		t.Error("undo must restore every byte")
	}
	if code, _, _ := fetchPost(t, h, "/tasks/undo", url.Values{"id": {undo}}); code != http.StatusBadRequest {
		t.Error("an undo works once")
	}

	// An edit, then a change by someone else: undo refuses.
	_, undo, _ = fetchPost(t, h, "/tasks/edit", url.Values{"ws": {"personal"}, "agent": {"friday"}, "id": {"2"}, "hash": {hash},
		"state": {StateParked}, "who": {""}, "note": {""}, "priority": {"P2"}, "due": {"TBD"}, "owner": {"Friday"}})
	if undo == "" {
		t.Fatal("an edit gives an undo id")
	}
	data, _ = os.ReadFile(tracker)
	os.WriteFile(tracker, append(data, []byte("\nagent was here\n")...), 0o640)
	if code, _, body := fetchPost(t, h, "/tasks/undo", url.Values{"id": {undo}}); code != http.StatusBadRequest || !strings.Contains(body, "changed since") {
		t.Errorf("undo after a change = %d %s", code, body)
	}
	os.WriteFile(tracker, data, 0o640)
	if code, _, _ := fetchPost(t, h, "/tasks/undo", url.Values{"id": {undo}}); code != http.StatusNoContent {
		t.Error("once the file is back as the edit left it, undo works")
	}
	if data, _ := os.ReadFile(tracker); string(data) != backlogTracker {
		t.Error("the edit is undone")
	}

	// An answer: undo takes it out of the inbox (here: removes the inbox
	// it created); after the agent touched the inbox, it refuses.
	inbox := filepath.Join(filepath.Dir(tracker), "memory/scheduled/inbox.md")
	_, undo, _ = fetchPost(t, h, "/tasks/answer", url.Values{"ws": {"personal"}, "agent": {"friday"}, "id": {"7"}, "text": {"Do it."}})
	if _, err := os.Stat(inbox); err != nil || undo == "" {
		t.Fatal("the answer is in the inbox")
	}
	if code, _, _ := fetchPost(t, h, "/tasks/undo", url.Values{"id": {undo}}); code != http.StatusNoContent {
		t.Fatal("undo the answer")
	}
	if _, err := os.Stat(inbox); !os.IsNotExist(err) {
		t.Error("the inbox it created is gone")
	}
	write(t, inbox, "# Inbox\n")
	_, undo, _ = fetchPost(t, h, "/tasks/answer", url.Values{"ws": {"personal"}, "agent": {"friday"}, "id": {"7"}, "text": {"Do it."}})
	got, _ := os.ReadFile(inbox)
	os.WriteFile(inbox, []byte(strings.Replace(string(got), "UNPROCESSED", "PROCESSED", 1)), 0o644)
	if code, _, _ := fetchPost(t, h, "/tasks/undo", url.Values{"id": {undo}}); code != http.StatusBadRequest {
		t.Error("an answer the agent has processed cannot be undone")
	}
}

func TestTasksOneByOneUpgrade(t *testing.T) {
	parent, _ := tasksFleet(t)
	write(t, filepath.Join(parent, "personal/agents/q/context.md"), "---\ntitle: Web\n---\n")
	live := func(bool) Options {
		return Options{Now: tasksNow, Herdr: func() ([]LivePane, error) {
			return []LivePane{{Name: "friday", PaneID: "w1:p3", CWD: filepath.Join(parent, "personal"), Agent: "claude", Status: "idle", Running: true}}, nil
		}}
	}
	h := NewWebHandler(WebSource{FleetRoot: parent, Options: live})
	start := follow(t, h, "/tasks/one?ws=personal&agent=friday")
	_, body := get(t, h, start)
	for _, want := range []string{`class="tk-post tk-instruct"`, `data-fill="Do it."`, `data-fill="Park it till Monday."`, `data-fill="Hand this to q."`,
		`action="/tasks/done"`, `name="hash" value="` + trackerHash([]byte(backlogTracker)) + `"`, `value="Merge #7 into #12."`, "done=d0", "done=m0", "done=k0",
		// One of four items has a recommendation: Prep is offered, through send.
		"1 of 4 have a recommendation.", `action="/w/personal/friday/send"`, "Prep for a one-by-one sweep: for every open item"} {
		if !strings.Contains(body, want) {
			t.Errorf("card lacks %q", want)
		}
	}
	// Prepping shows the count and polls; an undo id shows the Undo button.
	u, _ := url.Parse(start)
	q := u.Query()
	q.Set("prep", "1")
	q.Set("n", "1")
	q.Set("done", "a0")
	q.Set("undo", "abc123")
	_, body = get(t, h, "/tasks/one?"+q.Encode())
	if !strings.Contains(body, "Prepping… 1 of 4 have a recommendation.") || !strings.Contains(body, `action="/tasks/undo"`) || !strings.Contains(body, `data-key="u"`) {
		t.Errorf("prepping card:\n%s", body)
	}
	// The undo goes back to the card it came from, without its record.
	if !regexp.MustCompile(`id="undoform" method="post" action="/tasks/undo" data-next="[^"]*n=0[^"]*"`).MatchString(body) || strings.Contains(body, `data-next="/tasks/one?agent=friday&amp;done=a0&amp;n=0`) {
		t.Error("undo should return to the previous card with the record dropped")
	}
	// The end lists every item and what happened.
	q.Set("n", "4")
	q.Set("done", "a0,d1,m2")
	_, body = get(t, h, "/tasks/one?"+q.Encode())
	for _, want := range []string{`tk-what answered`, `tk-what done`, `tk-what merged`, `tk-what skipped`} {
		if !strings.Contains(body, want) {
			t.Errorf("end lacks %q", want)
		}
	}
}

// multipartAnswer posts an answer with one image, as the reply box does.
func multipartAnswer(t *testing.T, h http.Handler, form url.Values) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, vs := range form {
		for _, v := range vs {
			_ = mw.WriteField(k, v)
		}
	}
	fw, _ := mw.CreateFormFile("image", "shot.png")
	_, _ = fw.Write(pngBytes)
	_ = mw.Close()
	req := localReq("POST", "/tasks/answer", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-GoAgentic", "1")
	req.Header.Set("X-Tasks-Fetch", "1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// A live agent gets the reply in its chat, named by item, with the image;
// the item still shows as answered. A stopped one gets it in its inbox,
// the image kept under its items/inbox-images/.
func TestTasksReplyChatOrInbox(t *testing.T) {
	parent, _ := tasksFleet(t)
	inbox := filepath.Join(parent, "personal/agents/friday/memory/scheduled/inbox.md")
	var sent [][2]string
	live := func(bool) Options {
		return Options{Now: tasksNow, Herdr: func() ([]LivePane, error) {
			return []LivePane{{Name: "friday", PaneID: "w1:p3", CWD: filepath.Join(parent, "personal"), Agent: "claude", Status: "idle", Running: true}}, nil
		}}
	}
	h := NewWebHandler(WebSource{FleetRoot: parent, Options: live, Now: func() time.Time { return tasksNow },
		Prompt: func(pane, text string) error { sent = append(sent, [2]string{pane, text}); return nil }})
	if _, body := get(t, h, "/tasks/personal/friday?item=personal/friday/7"); !strings.Contains(body, "Goes to friday's live chat") || !strings.Contains(body, `name="to" value="chat"`) || !strings.Contains(body, "Send to chat") || !strings.Contains(body, `data-images`) {
		t.Errorf("a live agent's reply box should say it goes to the chat:\n%s", body)
	}
	code, body := multipartAnswer(t, h, url.Values{"ws": {"personal"}, "agent": {"friday"}, "id": {"7"}, "text": {"Yes, go."}, "to": {"chat"}})
	if code != http.StatusNoContent {
		t.Fatalf("live answer = %d %s", code, body)
	}
	if len(sent) != 1 || sent[0][0] != "w1:p3" || !strings.HasPrefix(sent[0][1], "Re #7 Ask Irfan: Yes, go.\n[Image: ") {
		t.Fatalf("sent = %q", sent)
	}
	data, _ := os.ReadFile(inbox)
	if !strings.Contains(string(data), "answer #7 (Irfan, via dash, sent to chat) — UNPROCESSED") {
		t.Errorf("the chat answer should still mark the item answered:\n%s", data)
	}
	if _, body = get(t, h, "/tasks/personal/friday"); !strings.Contains(body, "Answered") {
		t.Error("the row should show Answered")
	}

	// Not running: the inbox, with the image saved beside it.
	_ = os.Remove(inbox)
	none := func(bool) Options {
		return Options{Now: tasksNow, Herdr: func() ([]LivePane, error) { return nil, nil }}
	}
	sent = nil
	h = NewWebHandler(WebSource{FleetRoot: parent, Options: none, Now: func() time.Time { return tasksNow },
		Prompt: func(pane, text string) error { sent = append(sent, [2]string{pane, text}); return nil }})
	if _, body = get(t, h, "/tasks/personal/friday?item=personal/friday/7"); !strings.Contains(body, "Your answer goes to friday's inbox") {
		t.Errorf("a stopped agent's reply box keeps the inbox wording")
	}
	if code, body = multipartAnswer(t, h, url.Values{"ws": {"personal"}, "agent": {"friday"}, "id": {"7"}, "text": {"Park it."}, "to": {"chat"}}); code != http.StatusNoContent {
		t.Fatalf("inbox answer = %d %s", code, body)
	}
	if len(sent) != 0 {
		t.Errorf("nothing goes to a chat that is not running: %q", sent)
	}
	data, _ = os.ReadFile(inbox)
	shots, _ := filepath.Glob(filepath.Join(parent, "personal/agents/friday/items/inbox-images/20261109-090000-1.png"))
	if len(shots) != 1 || !strings.Contains(string(data), "> [Image: "+shots[0]+"]") || !strings.Contains(string(data), "(Irfan, via dash) — UNPROCESSED") {
		t.Errorf("inbox = %s, images = %v", data, shots)
	}
}
