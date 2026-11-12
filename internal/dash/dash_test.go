package dash

import (
	"bytes"
	"encoding/json"
	"errors"
	"os/exec"

	"github.com/charmbracelet/x/ansi"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var now = time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const sectionedTracker = `# Actions

Last reviewed: 2026-09-28
Next session: #3 ship the thing → #5 tidy

## Open

### P1 — this week
| # | Action | Linear | Owner | Priority | Due/Target | Status | Since |
|---|--------|--------|-------|----------|------------|--------|-------|
| 3 | **Ship the thing.** Long detail \| with a pipe | GOA-1 | Sigrid | P1 | | In progress | 2026-09-01 |
| — | *(none)* | | | | | | |
| 4 | ~~Old struck row~~ | | Sigrid | P1 | | Done | |

### P2
| # | Action | Linear | Owner | Priority | Due/Target | Status | Since |
|---|--------|--------|-------|----------|------------|--------|-------|
| 5 | Tidy [[memory]] and [links](x.md) | | Sigrid | P2 | | Blocked on Norman | |
| 6 | Sign the contract | | Norman | P2 | | Open | |
| 7 | Shared review | | Sigrid/Norman | P2 | | Open | |

### P3
| # | Action | Linear | Owner | Priority | Due/Target | Status | Since |
|---|--------|--------|-------|----------|------------|--------|-------|
| 8 | Someday | | Sigrid | P3 | | Parked | |

## Completed

| # | Action | Linear | Owner | Completed |
|---|--------|--------|-------|-----------|
| 1 | Done thing | | Sigrid | 2026-09-01 |
`

func TestParseActionsSectioned(t *testing.T) {
	tr := parseActions(sectionedTracker)
	if got := tr.lastReviewed.Format("2006-01-02"); got != "2026-09-28" {
		t.Errorf("last reviewed = %s", got)
	}
	if tr.nextSession != "#3 ship the thing → #5 tidy" {
		t.Errorf("next session = %q", tr.nextSession)
	}
	var ids []string
	for _, it := range tr.open {
		ids = append(ids, it.ID)
	}
	if got := strings.Join(ids, ","); got != "3,5,6,7,8" {
		t.Fatalf("open ids = %s (placeholder, struck and completed rows must be skipped)", got)
	}
	first := tr.open[0]
	if first.Headline() != "Ship the thing" || first.Priority != "P1" || first.Owner != "Sigrid" {
		t.Errorf("first = %+v headline %q", first, first.Headline())
	}
	if !strings.Contains(first.Action, "with a pipe") {
		t.Errorf("escaped pipe split the row: %q", first.Action)
	}
	if got := tr.open[1].Headline(); got != "Tidy memory and links" {
		t.Errorf("markdown not cleaned: %q", got)
	}
}

func TestParseActionsFlatTableUsesPriorityColumn(t *testing.T) {
	tr := parseActions(`Last reviewed: 2026-06-09 (session 1)

## Open

| # | Action | Ticket | Owner | Priority | Due/Target | Status | Since |
|---|--------|--------|-------|----------|------------|--------|-------|
| 1 | Low thing | | A | P3 | | Open | |
| 2 | High thing | | A | **P1** | | Open | |
`)
	if len(tr.open) != 2 || tr.open[1].Priority != "P1" {
		t.Fatalf("open = %+v", tr.open)
	}
	if tr.lastReviewed.IsZero() {
		t.Error("date with trailing text not parsed")
	}
	a := &Agent{Open: tr.open}
	if it, _ := a.NextUp("Norman"); it.ID != "2" {
		t.Errorf("next up = %s, want the P1", it.ID)
	}
}

func TestBlockedDetection(t *testing.T) {
	for status, want := range map[string]bool{
		"Blocked on legal":                  true,
		"Waiting on Astrid/Dario":           true,
		"Draft delivered — awaiting Norman": true,
		"Gated by budget":                   true,
		"Unblocked (MV side); part waits":   false,
		"In progress — delegated to Pax":    false,
		"Open":                              false,
	} {
		if got := (Item{Status: status}).Waiting(); got != want {
			t.Errorf("Blocked(%q) = %v, want %v", status, got, want)
		}
	}
	if !(Item{Status: "awaiting Norman"}).WaitsOn("Norman Noble") {
		t.Error("awaiting Norman should wait on the principal")
	}
	for _, status := range []string{"Waiting on Norman's posting", "Blocked on Norman", "Gated by Norman"} {
		if !(Item{Status: status}).WaitsOn("Norman Noble") {
			t.Errorf("%q should wait on the principal", status)
		}
	}
	if (Item{Status: "Blocked — Heinrich on holiday (Norman 09-27)"}).WaitsOn("Norman Noble") {
		t.Error("a name that only says who reported the block is not a wait on the principal")
	}
	if (Item{Status: "awaiting Norman"}).WaitsOnOthers("Norman Noble") {
		t.Error("an item waiting on the principal is not blocked on others")
	}
	if !(Item{Status: "waiting on Dario"}).WaitsOnOthers("Norman Noble") {
		t.Error("waiting on Dario is blocked on others")
	}
}

func TestDetailSections(t *testing.T) {
	ws := &Workspace{Principal: "Norman Noble", Scanned: now}
	a := &Agent{Name: "Sigrid", NextSession: "#67 close-out", Open: []Item{
		{ID: "67", Action: "Close-out", Owner: "Sigrid", Priority: "P1", Status: "In progress"},
		{ID: "53", Action: "Rescope", Owner: "Sigrid", Priority: "P1", Status: "Unblocked (MV side)"},
		{ID: "64", Action: "Re-cut", Owner: "Sigrid/Norman", Priority: "P1", Status: "awaiting Norman"},
		{ID: "68", Action: "Airtable renewal", Owner: "Norman/Dario", Priority: "P1", Status: "waiting on Dario"},
		{ID: "44", Action: "InfoSec KRs", Owner: "Sigrid", Priority: "P2", Status: "Awaiting draft"},
		{ID: "70", Action: "Vendor exit", Owner: "Sigrid", Priority: "P3", Status: "Blocked — no supplier left"},
		{ID: "71", Action: "Old idea", Owner: "Sigrid", Priority: "P3", Status: "Parked"},
	}}
	out := ansi.Strip(strings.Join(Detail(ws, a, 120, 0, NewStyles(true)), "\n"))
	heads := []string{"Next session", "Waiting on Norman", "Waiting on others ◆", "Blocked ■", "Open actions — P1 2 · P2 0 · P3 0", "Parked — 1", "Health"}
	at := make([]int, len(heads))
	for i, h := range heads {
		at[i] = strings.Index(out, h)
		if at[i] < 0 || (i > 0 && at[i] < at[i-1]) {
			t.Fatalf("want sections in order %q:\n%s", heads, out)
		}
	}
	body := func(i int) string { return out[at[i]:at[i+1]] }
	for i, want := range [][]string{{"#67"}, {"#64"}, {"#68", "#44"}, {"#70"}, {"#67", "#53"}, {"#71"}} {
		for _, id := range want {
			if !strings.Contains(body(i), id) {
				t.Errorf("%s should list %s:\n%s", heads[i], id, body(i))
			}
		}
	}
	for i, not := range [][]string{nil, {"#68"}, {"#64", "#70"}, {"#68"}, {"#64", "#68", "#44", "#70", "#71"}} {
		for _, id := range not {
			if strings.Contains(body(i), id) {
				t.Errorf("%s should not list %s:\n%s", heads[i], id, body(i))
			}
		}
	}
	if a.WaitingCount("Norman Noble") != 1 || a.BlockedCount() != 1 {
		t.Errorf("waiting %d, blocked %d; want 1 and 1", a.WaitingCount("Norman Noble"), a.BlockedCount())
	}

	// Empty sections say None in the full view and fold into one line in
	// the preview.
	quiet := &Agent{Name: "Varro", Open: []Item{{ID: "1", Action: "Ship", Owner: "Varro", Priority: "P2", Status: "In progress"}}}
	full := ansi.Strip(strings.Join(Detail(ws, quiet, 120, 0, NewStyles(true)), "\n"))
	if strings.Count(full, "  None") != 5 {
		t.Errorf("want five empty sections saying None:\n%s", full)
	}
	preview := ansi.Strip(strings.Join(Detail(ws, quiet, 120, 12, NewStyles(true)), "\n"))
	if strings.Contains(preview, "  None") || !strings.Contains(preview, "None: Next session (agent's own plan) · Waiting on Norman · Waiting on others · Blocked · Parked") {
		t.Errorf("preview should fold empty sections into one line:\n%s", preview)
	}
}

func TestParkedFoldsInLiveView(t *testing.T) {
	ws := &Workspace{Principal: "Norman Noble", Scanned: now, Agents: []*Agent{{Name: "Sigrid", Open: []Item{
		{ID: "1", Action: "Ship", Owner: "Sigrid", Priority: "P2", Status: "In progress"},
		{ID: "8", Action: "Someday", Owner: "Sigrid", Priority: "P3", Status: "Parked — after launch"},
	}}}}
	m := &model{ws: ws, st: NewStyles(true), width: 100, height: 40}
	m.key("p") // from the table: opens the detail at the parked list
	if out := ansi.Strip(m.render()); !m.full || !strings.Contains(out, "#8   Someday  after launch") {
		t.Fatalf("p from the table should open the detail with parked listed:\n%s", out)
	}
	m.key("left")
	m.key("p")
	m.key("right")
	if out := ansi.Strip(m.render()); !strings.Contains(out, "Parked — 1") || !strings.Contains(out, "p to show") || strings.Contains(out, "Someday") {
		t.Fatalf("parked items should fold by default:\n%s", out)
	}
	m.key("p")
	if out := ansi.Strip(m.render()); !strings.Contains(out, "#8   Someday  after launch") || strings.Contains(out, "p to show") {
		t.Fatalf("p should list the parked items:\n%s", out)
	}
	m.key("p")
	if out := ansi.Strip(m.render()); strings.Contains(out, "Someday") {
		t.Fatalf("p again should fold them:\n%s", out)
	}
	// The printed snapshot always lists them.
	if out := ansi.Strip(strings.Join(Detail(ws, ws.Agents[0], 120, 0, NewStyles(true)), "\n")); !strings.Contains(out, "Someday") {
		t.Errorf("snapshot detail should list parked items:\n%s", out)
	}
}

func TestBlockedState(t *testing.T) {
	for status, blocked := range map[string]bool{
		"Blocked":                        true,
		"Blocked — no supplier left":     true,
		"blocked: licence withdrawn":     true,
		"Blocked (since 10-20)":          true,
		"Blocked on #5":                  false, // older free text: names what it waits on
		"Blocked on Norman":              false,
		"Waiting on Dario":               false,
		"In progress — blocked by a bug": false,
	} {
		it := Item{Status: status}
		if it.Blocked() != blocked {
			t.Errorf("Blocked(%q) = %v, want %v", status, it.Blocked(), blocked)
		}
		if blocked && (it.Waiting() || it.Parked() || it.NotStarted() || it.WaitsOn("Norman Noble")) {
			t.Errorf("%q is blocked, nothing else", status)
		}
	}
	ws := &Workspace{Principal: "Norman Noble", Scanned: now}
	tr := parseActions("## Open\n\n| # | Action | Owner | Priority | Due/Target | Status |\n|---|---|---|---|---|---|\n" +
		"| 1 | Quote | A | P1 | | Waiting on Ivan — reply |\n| 2 | Exit | A | P3 | | Blocked — no way out |\n")
	if got := agentTone(ws, &Agent{Open: tr.open}); got != toneBlocked {
		t.Errorf("a blocked item beats a P1 waiting: tone = %v, want red", got)
	}
	if c := (&Workspace{Principal: "Norman Noble", Agents: []*Agent{{Open: tr.open}}}).Counts(); c.Waiting != 1 || c.Blocked != 1 {
		t.Errorf("counts = %+v", c)
	}
}

func TestDetailLiveBeforeParked(t *testing.T) {
	ws := &Workspace{Principal: "Norman Noble", Scanned: now}
	a := &Agent{Name: "Cato", Open: []Item{
		{ID: "66", Action: "Held thing", Owner: "Cato", Priority: "P2", Status: "Explicit hold"},
		{ID: "15", Action: "Another live thing", Owner: "Cato", Priority: "P2", Status: "Not started"},
		{ID: "70", Action: "Live thing", Owner: "Cato", Priority: "P2", Status: "In progress"},
		{ID: "19", Action: "Dormant thing", Owner: "Cato", Priority: "P2", Status: "Dormant"},
		{ID: "9", Action: "Low live thing", Owner: "Cato", Priority: "P3", Status: "Open"},
	}}
	out := ansi.Strip(strings.Join(Detail(ws, a, 120, 0, NewStyles(true)), "\n"))
	pos := func(id string) int { return strings.Index(out, id) }
	if !(pos("#70") < pos("#15") && pos("#15") < pos("#9 ") && pos("#9 ") < pos("Parked — 2") &&
		pos("Parked — 2") < pos("#66") && pos("#66") < pos("#19")) {
		t.Errorf("want P2s under way, then not started, then P3, then a Parked section:\n%s", out)
	}
	if !strings.Contains(out, "Open actions — P1 0 · P2 2 · P3 1") {
		t.Errorf("open heading should not count parked items:\n%s", out)
	}

	for status, want := range map[string]bool{
		"Not started": true, "Not Started — waits on #51": true, "Open": true, "": true, "To do": true,
		"In progress": false, "Opened PR #12": false, "Newsletter drafted": false, "Parked": false,
	} {
		if got := (Item{Status: status}).NotStarted(); got != want {
			t.Errorf("NotStarted(%q) = %v, want %v", status, got, want)
		}
	}

	// The item cap keeps live items: with room for two, both live P2s show.
	capped := ansi.Strip(strings.Join(Detail(ws, a, 120, 2, NewStyles(true)), "\n"))
	if !strings.Contains(capped, "#70") || !strings.Contains(capped, "#15") || strings.Contains(capped, "#66") {
		t.Errorf("capped list should show the live items first:\n%s", capped)
	}
}

func TestPlanSteps(t *testing.T) {
	a := &Agent{
		NextSession: "#20 apply legal answers → #18 framework sync -> #99 gone → chase Dario",
		Open: []Item{
			{ID: "20", Priority: "P2", Status: "Not started"},
			{ID: "18", Priority: "P3", Status: "Open"},
		},
	}
	got := a.PlanSteps()
	want := []PlanStep{
		{"P2", "#20  apply legal answers"},
		{"P3", "#18  framework sync"},
		{"", "#99 gone"}, // not open: kept as written, no priority
		{"", "chase Dario"},
	}
	if len(got) != len(want) {
		t.Fatalf("steps = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("step %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	ws := &Workspace{Principal: "Norman Noble", Scanned: now}
	a.Name = "Astrid"
	a.Open = append(a.Open, Item{ID: "11", Action: "Dario + Shaf", Owner: "Norman", Priority: "P1", Status: "In Progress"})
	out := ansi.Strip(strings.Join(Detail(ws, a, 120, 0, NewStyles(true)), "\n"))
	plain := strings.Join(Detail(ws, a, 120, 0, NewStyles(true)), "\n")
	if strings.Contains(plain, "\x1b[") {
		t.Errorf("no-color detail contains escape codes:\n%q", plain)
	}
	for _, line := range []string{"  P2 #20  apply legal answers", "  P3 #18  framework sync", "  P1 #11  Dario + Shaf"} {
		if !strings.Contains(out, line+"\n") {
			t.Errorf("detail lacks line %q:\n%s", line, out)
		}
	}
}

func TestStandardStates(t *testing.T) {
	const norman = "Norman Noble"
	cases := []struct {
		status, state, who                       string
		blocked, waitsOn, others, parked, notYet bool
	}{
		{"Not started", StateNotStarted, "", false, false, false, false, true},
		{"In Progress — read done", StateInProgress, "", false, false, false, false, false}, // "done" in the note is not parked
		{"In progress — waiting on Dario for one figure", StateInProgress, "", false, false, false, false, false},
		{"Waiting on Norman", StateWaiting, "Norman", true, true, false, false, false},
		{"Waiting on Norman's posting", StateWaiting, "Norman's posting", true, true, false, false, false},
		{"Waiting on Dario — tech = 25 seats set", StateWaiting, "Dario", true, false, true, false, false},
		{"Waiting on Astrid/Dario (Norman chasing)", StateWaiting, "Astrid/Dario", true, false, true, false, false},
		{"Waiting on #51", StateWaiting, "#51", true, false, true, false, false},
		{"Parked — revisit after TE go", StateParked, "", false, false, false, true, false},
	}
	for _, c := range cases {
		it := Item{Status: c.status}
		state, who, ok := it.State()
		if !ok || state != c.state || who != c.who {
			t.Errorf("State(%q) = %q, %q, %v; want %q, %q", c.status, state, who, ok, c.state, c.who)
		}
		if it.Waiting() != c.blocked || it.WaitsOn(norman) != c.waitsOn || it.WaitsOnOthers(norman) != c.others ||
			it.Parked() != c.parked || it.NotStarted() != c.notYet {
			t.Errorf("%q: blocked %v waitsOn %v others %v parked %v notStarted %v", c.status,
				it.Waiting(), it.WaitsOn(norman), it.WaitsOnOthers(norman), it.Parked(), it.NotStarted())
		}
	}
	if _, _, ok := (Item{Status: "Deferred to Q4"}).State(); ok {
		t.Error("free text is not a standard state")
	}
}

func TestNextUpAndPrincipal(t *testing.T) {
	a := &Agent{Open: parseActions(sectionedTracker).open}
	it, ok := a.NextUp("Norman Noble")
	if !ok || it.ID != "3" {
		t.Fatalf("next up = %+v", it)
	}
	var mine []string
	for _, it := range a.ForPrincipal("Norman Noble") {
		mine = append(mine, it.ID)
	}
	if got := strings.Join(mine, ","); got != "5,6" {
		t.Errorf("for principal = %s, want the solely owned row and the one blocked on Norman", got)
	}
	if !a.Open[1].Waiting() || a.Open[0].Waiting() {
		t.Error("blocked detection wrong")
	}
	if !a.Open[4].Parked() {
		t.Error("parked detection wrong")
	}

	onlyPrincipal := &Agent{Open: []Item{{ID: "1", Action: "x", Owner: "Norman", Priority: "P1"}}}
	if _, ok := onlyPrincipal.NextUp("Norman"); ok {
		t.Error("a principal-only row is not the agent's next item")
	}
}

// fixture builds a workspace with one healthy agent, one unhealthy agent and
// one retired agent.
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, filepath.Join(root, "agents/CONVENTIONS.md"), "---\nextends: plugin\nprincipal: Norman Noble   # the human\n---\n# x\n")

	sig := filepath.Join(root, "agents/Sigrid")
	write(t, filepath.Join(sig, "context.md"), "---\nscope: Acme\ntitle: Platform Lead\n---\n")
	for _, f := range coreFiles {
		write(t, filepath.Join(sig, f), "x")
	}
	write(t, filepath.Join(sig, "actions.md"), sectionedTracker)
	write(t, filepath.Join(sig, "memory/sessions/2026-09-20-old.md"), "---\ndate: 2026-09-20\n---\n# Old session\n\nOld.\n")
	write(t, filepath.Join(sig, "memory/sessions/2026-09-27-new.md"), "---\ndate: 2026-09-27\ntype: session\n---\n\n# New session\n\nDid the **new** thing.\nAnd more.\n\n## Details\nignored\n")
	write(t, filepath.Join(sig, "memory/scheduled/inbox.md"), "Drain `UNPROCESSED` entries at startup.\n### <ISO-8601 timestamp> — <task-id> — UNPROCESSED\n### 2026-09-26T09:07+08:00 — watchdog — UNPROCESSED\nbody\n### 2026-09-25T09:07+08:00 — watchdog — PROCESSED (drained)\n")

	bad := filepath.Join(root, "agents/Varro")
	write(t, filepath.Join(bad, "context.md"), "---\ntitle: Ops\n---\n")
	write(t, filepath.Join(bad, "actions.md"), "Last reviewed: 2026-08-01\n")
	for i := 0; i < 7; i++ {
		write(t, filepath.Join(bad, "memory/standing", string(rune('a'+i))+".md"), "x")
	}

	old := filepath.Join(root, "agents/Cato")
	write(t, filepath.Join(old, "context.md"), "---\ntitle: Old\nstatus: retired\n---\n")
	return root
}

func TestScanFixture(t *testing.T) {
	root := fixture(t)
	nested := filepath.Join(root, "agents", "Sigrid", "memory")
	found, err := FindRoot(nested)
	if err != nil || found != root {
		t.Fatalf("FindRoot = %q, %v", found, err)
	}
	if _, err := FindRoot(t.TempDir()); err != ErrNoWorkspace {
		t.Errorf("FindRoot outside a workspace = %v", err)
	}

	ws, err := Scan(root, Options{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if ws.Principal != "Norman Noble" {
		t.Errorf("principal = %q (frontmatter comment must be stripped)", ws.Principal)
	}
	if len(ws.Agents) != 2 {
		t.Fatalf("agents = %d, retired must be hidden", len(ws.Agents))
	}
	sig, varro := ws.Agents[0], ws.Agents[1]
	if sig.Name != "Sigrid" || sig.Title != "Platform Lead" {
		t.Errorf("sigrid = %+v", sig)
	}
	s, _ := sig.LastSession()
	if s.Title != "New session" || s.Summary != "Did the new thing. And more." {
		t.Errorf("last session = %+v", s)
	}
	if sig.InboxUnprocessed != 1 {
		t.Errorf("inbox = %d", sig.InboxUnprocessed)
	}
	if sig.Health.Level != OK {
		t.Errorf("sigrid health = %+v", sig.Health)
	}
	if varro.Health.Level != Fail {
		t.Errorf("varro health = %+v", varro.Health)
	}
	joined := strings.Join(varro.Health.Findings, "; ")
	for _, want := range []string{"missing", "standing memory has 7 files", "last reviewed 8w ago"} {
		if !strings.Contains(joined, want) {
			t.Errorf("varro findings %q lack %q", joined, want)
		}
	}
	if got := ws.Summary(); got != "3 for you · 1 !" {
		t.Errorf("summary = %q", got)
	}

	all, _ := Scan(root, Options{Now: now, IncludeRetired: true})
	if len(all.Agents) != 3 {
		t.Errorf("with retired = %d", len(all.Agents))
	}
}

func TestSnapshotAndJSON(t *testing.T) {
	ws, err := Scan(fixture(t), Options{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	WriteSnapshot(&buf, ws, 120, true, NewStyles(true))
	out := buf.String()
	for _, want := range []string{
		"✋ 3 for you", "● Sigrid", "  Varro", // health fails, nothing blocked: no dot
		"Waiting on Norman:", "Sigrid #5 Tidy memory and links", "Sigrid #6 Sign the contract",
		"1 scheduled run(s) not yet read",
		"» #3 ship the thing → #5 tidy", // the agent's own plan wins
		"(no open actions)",
		"New session", // last session, in the detail now
		"AGENT   HARNESS   LIVE",
		"harness claude (default)",
		"Open actions — P1 1 · P2 1 · P3 0", // #5 and #6 wait on Norman, so not here
		"Parked — 1",
		"P3 #8   Someday",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("snapshot lacks %q:\n%s", want, out)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if n := len([]rune(line)); n > 120 {
			t.Errorf("line wider than 120 (%d): %q", n, line)
		}
	}

	buf.Reset()
	if err := WriteJSON(&buf, ws); err != nil {
		t.Fatal(err)
	}
	var got JSON
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Agents[0].NextUp == nil || got.Agents[0].NextUp.ID != "3" || got.Agents[1].Health != "fail" || got.Agents[0].Harness != "claude" {
		t.Errorf("json = %s", buf.String())
	}
}

func TestHerdrLive(t *testing.T) {
	panes, err := parseHerdrAgents([]byte(`{"id":"x","result":{"type":"agent_list","agents":[
		{"name":"sigrid","agent":null,"agent_status":"unknown","cwd":"/ws","pane_id":"w1:p1"},
		{"name":"sigrid-2","agent":"claude","agent_status":"idle","cwd":"/ws/sub","pane_id":"w1:p2"},
		{"name":"varro","agent":"claude","agent_status":"working","cwd":"/other","pane_id":"w2:p1"},
		{"name":"sigridx","agent":"claude","agent_status":"working","cwd":"/ws","pane_id":"w1:p3"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	ws := &Workspace{Root: "/ws", Live: true, Agents: []*Agent{{Name: "Sigrid"}, {Name: "Varro"}}}
	attachLive(ws, panes)
	if l := ws.Agents[0].Live; l == nil || l.PaneID != "w1:p2" || l.State() != "idle" {
		t.Errorf("sigrid live = %+v (running -2 pane beats a shell pane; sigridx is not sigrid)", l)
	}
	if ws.Agents[1].Live != nil {
		t.Error("a pane under another root must not match")
	}
	// No agent name, but the router's label says whose pane it is.
	byLabel := &Workspace{Root: "/ws", Live: true, Agents: []*Agent{{Name: "Astrid"}, {Name: "Aud"}}}
	attachLive(byLabel, []LivePane{{Agent: "claude", Running: true, Status: "working", CWD: "/ws", PaneID: "w9:p9",
		Label: "Astrid - Intercompany Contracts Partner"}})
	if l := byLabel.Agents[0].Live; l == nil || l.PaneID != "w9:p9" {
		t.Errorf("astrid by label = %+v", l)
	}
	if byLabel.Agents[1].Live != nil {
		t.Error("Aud must not match Astrid's label")
	}
	if (&LivePane{}).State() != "shell" {
		t.Error("exited CLI should read as shell")
	}
}

func TestTranscriptStarts(t *testing.T) {
	dir := t.TempDir()
	line := func(args, ts string) string {
		return `{"type":"user","message":{"content":"<command-name>/agents:start</command-name>\n<command-args>` + args + `</command-args>"},"timestamp":"` + ts + `"}` + "\n"
	}
	write(t, filepath.Join(dir, "a.jsonl"), line("sigrid", "2026-09-27T01:00:00Z")+`{"type":"assistant"}`+"\n")
	write(t, filepath.Join(dir, "b.jsonl"), line("Sigrid close", "2026-09-28T01:00:00Z"))
	write(t, filepath.Join(dir, "c.jsonl"), line("varro peer peer/req.md", "2026-09-28T05:00:00Z"))
	write(t, filepath.Join(dir, "d.txt"), line("varro", "2026-09-28T05:00:00Z"))
	write(t, filepath.Join(dir, "e.jsonl"), line("varro task peer/2026-09-28-0600-task-from-sigrid.md", "2026-09-28T06:00:00Z"))
	// Arguments may hold angle brackets ("record 'Waiting on Norman — <the one thing>'").
	write(t, filepath.Join(dir, "f.jsonl"), line("dario commit it; record <the one thing> then wrap", "2026-09-29T01:00:00Z"))

	c := NewTranscriptCache()
	starts := c.Starts(dir)
	if got := starts["sigrid"].Started.Format(time.RFC3339); got != "2026-09-28T01:00:00Z" {
		t.Errorf("sigrid started = %s", got)
	}
	if got := starts["dario"].Started.Format(time.RFC3339); got != "2026-09-29T01:00:00Z" {
		t.Errorf("a start whose arguments hold < must count: dario started = %s", got)
	}
	if said, agent := userText("<command-name>/agents:start</command-name>\n<command-args>dario fix <it>\nthen wrap</command-args>"); agent != "dario" || said != "/agents:start dario fix <it>\nthen wrap" {
		t.Errorf("userText = %q, %q", said, agent)
	}
	if _, ok := starts["varro"]; ok {
		t.Error("peer and task sessions and non-jsonl files must not count")
	}
	// A second pass reuses the cache and gives the same answer.
	if again := c.Starts(dir); !again["sigrid"].Started.Equal(starts["sigrid"].Started) {
		t.Error("cached result differs")
	}
	if got := projectDirName("/home/n/agents/Go.Agentic"); got != "-home-n-agents-Go-Agentic" {
		t.Errorf("project dir = %s", got)
	}
}

func TestHumanAge(t *testing.T) {
	for d, want := range map[time.Duration]string{
		30 * time.Second: "now", 5 * time.Minute: "5m", 3 * time.Hour: "3h",
		72 * time.Hour: "3d", 30 * 24 * time.Hour: "4w",
	} {
		if got := humanAge(d); got != want {
			t.Errorf("humanAge(%s) = %s, want %s", d, got, want)
		}
	}
}

func TestSidebarRoots(t *testing.T) {
	root := fixture(t)
	other := t.TempDir()
	got := sidebarRoots([]herdrPane{
		{WorkspaceID: "w1", CWD: root},
		{WorkspaceID: "w1", CWD: filepath.Join(root, "agents", "Sigrid")},
		{WorkspaceID: "w1", CWD: other},
		{WorkspaceID: "w2", CWD: other},
	})
	if got["w1"] != root || got["w2"] != "" {
		t.Errorf("roots = %v", got)
	}
}

func TestLaunchPlan(t *testing.T) {
	ws := &Workspace{Root: "/ws", Harness: "codex"}
	join := func(steps []LaunchStep) string {
		var parts []string
		for _, s := range steps {
			parts = append(parts, strings.Join(s.Args, " "))
		}
		return strings.Join(parts, " | ")
	}

	running := &Agent{Name: "Sigrid", Live: &LivePane{PaneID: "w1:p2", Agent: "claude", Running: true}}
	if sum, steps := LaunchPlan(ws, running, "w1"); join(steps) != "agent focus w1:p2" || sum != "switched to Sigrid" {
		t.Errorf("running: %s / %s", sum, join(steps))
	}

	shell := &Agent{Name: "Sigrid", Harness: "claude", Live: &LivePane{PaneID: "w1:p3"}}
	if _, steps := LaunchPlan(ws, shell, "w1"); join(steps) != "agent start sigrid --kind claude --pane w1:p3 -- /agents:start sigrid | agent focus w1:p3" {
		t.Errorf("shell: %s", join(steps))
	}

	// No pane: new tab, and the workspace harness (codex) applies.
	none := &Agent{Name: "Varro"}
	_, steps := LaunchPlan(ws, none, "w1")
	if got := join(steps); got != "tab create --cwd /ws --label Varro --focus --workspace w1 | agent start varro --kind codex --pane  -- $agents-start varro" || !steps[0].NewPane {
		t.Errorf("no pane: %s", got)
	}

	// Cursor with a model: the model goes before the start command, and the
	// fallback types the cursor-agent executable.
	cur := &Agent{Name: "Ada", Harness: "cursor", Model: "gpt-5", Live: &LivePane{PaneID: "w1:p4"}}
	_, steps = LaunchPlan(ws, cur, "w1")
	if got := join(steps); got != "agent start ada --kind cursor --pane w1:p4 -- --model gpt-5 /agents-start ada | agent focus w1:p4" {
		t.Errorf("cursor: %s", got)
	}
	if got := strings.Join(steps[0].Fallback, " "); got != "pane run w1:p4 cursor-agent '--model' 'gpt-5' '/agents-start ada'" {
		t.Errorf("cursor fallback: %s", got)
	}

	// Pi, model from the workspace.
	piWS := &Workspace{Root: "/ws", Harness: "pi", Model: "openrouter/qwen/qwen3-coder"}
	_, steps = LaunchPlan(piWS, &Agent{Name: "Bo", Live: &LivePane{PaneID: "w1:p5"}}, "w1")
	if got := join(steps); got != "agent start bo --kind pi --pane w1:p5 -- --model openrouter/qwen/qwen3-coder /agents-start bo | agent focus w1:p5" {
		t.Errorf("pi: %s", got)
	}
}

func TestLaunchRunsHerdr(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.log")
	fake := filepath.Join(dir, "herdr")
	write(t, fake, `#!/bin/sh
echo "$@" >> "`+logPath+`"
case "$1 $2" in
"tab create") echo '{"id":"x","result":{"type":"tab_created","root_pane":{"pane_id":"w1:p9"},"tab":{"tab_id":"w1:t4"}}}' ;;
"agent start") echo '{"id":"x","result":{}}' ;;
esac
`)
	if err := os.Chmod(fake, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_BIN_PATH", fake)
	t.Setenv("HERDR_WORKSPACE_ID", "w1")

	sum, err := Launch(&Workspace{Root: "/ws"}, &Agent{Name: "Varro"})
	if err != nil || sum != "started Varro (claude) in a new tab" {
		t.Fatalf("launch = %q, %v", sum, err)
	}
	calls, _ := os.ReadFile(logPath)
	want := "tab create --cwd /ws --label Varro --focus --workspace w1\nagent start varro --kind claude --pane w1:p9 -- /agents:start varro\n"
	if string(calls) != want {
		t.Errorf("calls:\n%s\nwant:\n%s", calls, want)
	}

	t.Setenv("HERDR_BIN_PATH", "")
	t.Setenv("HERDR_ENV", "")
	saved := lookPath
	t.Cleanup(func() { lookPath = saved })
	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	if _, err := Launch(&Workspace{}, &Agent{Name: "x"}); err != ErrNoHerdr {
		t.Errorf("outside Herdr, no herdr installed = %v", err)
	}
	// A dashboard run as a service has no Herdr variables but herdr is
	// installed: it gets as far as running it.
	lookPath = func(string) (string, error) { return "/nonexistent/herdr", nil }
	if _, err := Launch(&Workspace{}, &Agent{Name: "x"}); err == ErrNoHerdr {
		t.Error("herdr installed, no Herdr variables: refused")
	}
}

func TestLaunchFallsBackToTypingTheCommand(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.log")
	fake := filepath.Join(dir, "herdr")
	write(t, fake, `#!/bin/sh
echo "$@" >> "`+logPath+`"
case "$1 $2" in
"agent start") echo '{"error":{"code":"name_in_use","message":"name in use"}}'; exit 1 ;;
esac
`)
	if err := os.Chmod(fake, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_BIN_PATH", fake)
	a := &Agent{Name: "Seneca", Live: &LivePane{PaneID: "w2:pB"}}
	if _, err := Launch(&Workspace{Root: "/ws"}, a); err != nil {
		t.Fatal(err)
	}
	calls, _ := os.ReadFile(logPath)
	want := "agent start seneca --kind claude --pane w2:pB -- /agents:start seneca\n" +
		"pane run w2:pB claude '/agents:start seneca'\n" +
		"agent focus w2:pB\n"
	if string(calls) != want {
		t.Errorf("calls:\n%s\nwant:\n%s", calls, want)
	}
}

func TestDue(t *testing.T) {
	tr := parseActions("## Open\n\n| # | Action | Owner | Priority | Due/Target | Status |\n|---|---|---|---|---|---|\n" +
		"| 1 | Late | A | P1 | 2026-09-01 | Open |\n| 2 | Later | A | P1 | 2026-12-01 | Open |\n| 3 | Parked late | A | P1 | 2026-09-01 | Parked |\n| 4 | Vague | A | P1 | When quiet | Open |\n")
	ag := &Agent{Open: tr.open}
	if n := ag.OverdueCount(now); n != 1 {
		t.Errorf("overdue = %d, want 1 (future, parked and free-text dates do not count)", n)
	}
	if got := (Item{Action: "**Deploy pipeline redesign (Norman's target model, 2026-09-23)**"}).Short(); got != "Deploy pipeline redesign" {
		t.Errorf("short = %q", got)
	}
}

func ansiWidth(s string) int { return ansi.StringWidth(s) }

func TestUnwrapped(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "memory/sessions/2026-09-27-s.md"), "# s\n")
	write(t, filepath.Join(dir, "actions.md"), "Last reviewed: 2026-09-27\n")
	old := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	later := old.Add(time.Hour)
	set := func(p string, at time.Time) {
		if err := os.Chtimes(filepath.Join(dir, p), at, at); err != nil {
			t.Fatal(err)
		}
	}

	set("memory/sessions/2026-09-27-s.md", old)
	set("actions.md", later)
	if !unwrapped(dir, true) {
		t.Error("dirty tracker newer than the last session log must read as unwrapped")
	}
	if unwrapped(dir, false) {
		t.Error("a committed tracker is never unwrapped")
	}

	set("memory/sessions/2026-09-27-s.md", later.Add(time.Minute))
	if unwrapped(dir, true) {
		t.Error("a session log written after the tracker is a wrap, not unwrapped")
	}

	set("memory/sessions/2026-09-27-s.md", old)
	write(t, filepath.Join(dir, "memory/tasks/2026-09-27-t.md"), "# t\n")
	set("memory/tasks/2026-09-27-t.md", later.Add(time.Minute))
	if unwrapped(dir, true) {
		t.Error("a task log written after the tracker is a wrap, not unwrapped")
	}

	if !(dirtyPaths{"agents/Sigrid/actions.md"}).has(filepath.Join("agents/Sigrid", "actions.md")) {
		t.Error("has must match the exact path")
	}
}

func TestFleet(t *testing.T) {
	parent := t.TempDir()
	for _, name := range []string{"Beta", "alpha"} {
		ws := filepath.Join(parent, name)
		write(t, filepath.Join(ws, "agents/CONVENTIONS.md"), "---\nprincipal: Norman Noble\n---\n")
		write(t, filepath.Join(ws, "agents/Sigrid/context.md"), "---\ntitle: Lead\n---\n")
		write(t, filepath.Join(ws, "agents/Sigrid/actions.md"), sectionedTracker)
	}
	write(t, filepath.Join(parent, "notes/readme.md"), "not a workspace")
	if err := os.Symlink(filepath.Join(parent, "Beta"), filepath.Join(parent, "OldBeta")); err != nil {
		t.Fatal(err)
	}

	dirs := FleetDirs(parent)
	if len(dirs) != 2 || filepath.Base(dirs[0]) != "alpha" || filepath.Base(dirs[1]) != "Beta" {
		t.Fatalf("fleet dirs = %v (symlinks and non-workspaces skipped, sorted ignoring case)", dirs)
	}

	calls := 0
	f, err := ScanFleet(parent, Options{Now: now, Herdr: func() ([]LivePane, error) {
		calls++
		return nil, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("herdr asked %d times, want once per fleet scan", calls)
	}
	one := f.Workspaces[0].Counts()
	if all := f.Counts(); all.Agents != 2 || all.ForYou != 2*one.ForYou || all.Blocked != 2*one.Blocked {
		t.Errorf("fleet counts = %+v, workspace = %+v", all, one)
	}

	var buf bytes.Buffer
	WriteFleet(&buf, f, 120, NewStyles(true))
	out := buf.String()
	for _, want := range []string{"FLEET", "WORKSPACE", "alpha", "Beta", "NEEDS YOU"} {
		if !strings.Contains(out, want) {
			t.Errorf("fleet output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "OldBeta") {
		t.Error("a symlinked workspace must not be listed twice")
	}

	if _, err := ScanFleet(filepath.Join(parent, "notes"), Options{Now: now}); err == nil {
		t.Error("a directory with no workspaces must be an error")
	}
}

func TestParkedNote(t *testing.T) {
	for in, want := range map[string]string{
		"Parked — shelf reference": "shelf reference",
		"parked":                   "",
		"Parked: until Q1":         "until Q1",
		"Deferred by Norman":       "Deferred by Norman",
	} {
		if got := parkedNote(in); got != want {
			t.Errorf("parkedNote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHeadlineShortensLongActions(t *testing.T) {
	for action, want := range map[string]string{
		"Get the real agreed Rate Card (Alex Dogliotti / Dragana). Deprioritised 2026-09-23 — time matters":              "Get the real agreed Rate Card (Alex Dogliotti / Dragana)",
		"Sync work/contracts/data-sharing/framework.md with the deck: \"customer\" not \"learner\"":                      "Sync work/contracts/data-sharing/framework.md with the deck",
		"Wire the Drive push into _generate_sows.py (or a sibling script) so a sheet change re-generates and re-uploads": "Wire the Drive push into _generate_sows.py (or a sibling script) so a…",
		"Short action": "Short action",
		"Backend test coverage: notification domain.":                                                   "Backend test coverage: notification domain.", // short: left whole
		"Atticus GitHub read account + goaspire org invite (low urgency — Norman's gh works for reads)": "Atticus GitHub read account + goaspire org invite (low urgency…",
		"**Bold lead.** with a long tail ...":                                                           "Bold lead",
	} {
		if got := (Item{Action: action}).Headline(); got != want {
			t.Errorf("Headline(%q) = %q, want %q", action, got, want)
		}
	}
}

func TestHarnessSource(t *testing.T) {
	ws := &Workspace{Harness: "codex"}
	for _, c := range []struct {
		ws          *Workspace
		agent       string
		want, where string
	}{
		{&Workspace{}, "", "claude", "default"},
		{&Workspace{Harness: "bogus"}, "", "claude", "default"},
		{ws, "", "codex", "workspace"},
		{ws, "pi", "pi", "agent"},
		{ws, "bogus", "codex", "workspace"},
	} {
		h, where := HarnessSource(c.ws, &Agent{Harness: c.agent})
		if h != c.want || where != c.where {
			t.Errorf("HarnessSource(%q, %q) = %s, %s; want %s, %s", c.ws.Harness, c.agent, h, where, c.want, c.where)
		}
	}
	// The workspace model belongs to the workspace harness (claude here), so
	// an agent set to pi does not take it; its own model it does.
	a := &Agent{Name: "Bo", Harness: "pi"}
	if got := harnessFact(&Workspace{Model: "claude-sonnet-5-5"}, a); got != "harness pi (set for this agent)" {
		t.Errorf("harnessFact = %q", got)
	}
	a.Model = "openrouter/qwen/qwen3-coder"
	if got := harnessFact(&Workspace{Model: "claude-sonnet-5-5"}, a); got != "harness pi (set for this agent) · model openrouter/qwen/qwen3-coder" {
		t.Errorf("harnessFact = %q", got)
	}
	if got := harnessFact(&Workspace{Harness: "pi", Model: "gpt-5"}, &Agent{}); got != "harness pi (workspace default) · model gpt-5" {
		t.Errorf("harnessFact = %q", got)
	}
}

func TestHarnessAvailable(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	installed := map[string]bool{"claude": true, "codex": true, "pi": true, "cursor-agent": true}
	defer func(orig func(string) (string, error)) { lookPath = orig }(lookPath)
	lookPath = func(cli string) (string, error) {
		if installed[cli] {
			return "/bin/" + cli, nil
		}
		return "", errors.New("not found")
	}
	write := func(path string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ws := &Workspace{Root: root}
	write(filepath.Join(root, ".agents/skills/agents-start/SKILL.md")) // codex + cursor, project scope
	write(filepath.Join(home, ".pi/agent/prompts/agents-start.md"))    // pi, user scope

	want := map[string]string{
		"claude":   "",
		"codex":    "",
		"cursor":   "",
		"pi":       "",
		"gemini":   "gemini not installed",
		"opencode": "opencode not installed",
	}
	for h, reason := range want {
		if ok, got := HarnessAvailable(ws, h); got != reason || ok != (reason == "") {
			t.Errorf("%s: %v %q, want %q", h, ok, got, reason)
		}
	}

	installed["opencode"] = true
	if _, got := HarnessAvailable(ws, "opencode"); got != "no framework commands here: run harness/install.sh opencode" {
		t.Errorf("opencode without commands: %q", got)
	}
}

func TestLaunchPlanModelFollowsItsHarness(t *testing.T) {
	ws := &Workspace{Root: "/ws", Model: "claude-sonnet-5-5"} // claude workspace
	join := func(a *Agent) string {
		_, steps := LaunchPlan(ws, a, "w1")
		return strings.Join(steps[0].Args, " ")
	}
	if got := join(&Agent{Name: "Sigrid", Live: &LivePane{PaneID: "w1:p3"}}); got != "agent start sigrid --kind claude --pane w1:p3 -- --model claude-sonnet-5-5 /agents:start sigrid" {
		t.Errorf("workspace harness: %s", got)
	}
	if got := join(&Agent{Name: "Sigrid", Harness: "pi", Live: &LivePane{PaneID: "w1:p3"}}); got != "agent start sigrid --kind pi --pane w1:p3 -- /agents-start sigrid" {
		t.Errorf("own harness, no model: %s", got)
	}
	if got := join(&Agent{Name: "Sigrid", Harness: "pi", Model: "openrouter/x", Live: &LivePane{PaneID: "w1:p3"}}); got != "agent start sigrid --kind pi --pane w1:p3 -- --model openrouter/x /agents-start sigrid" {
		t.Errorf("own harness and model: %s", got)
	}
}

func TestHarnessMenu(t *testing.T) {
	defer func(orig func(string) (string, error)) { lookPath = orig }(lookPath)
	lookPath = func(cli string) (string, error) {
		if cli == "claude" || cli == "codex" {
			return "/bin/" + cli, nil
		}
		return "", errors.New("not found")
	}
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".agents/skills/agents-start"), 0o755)
	os.WriteFile(filepath.Join(root, ".agents/skills/agents-start/SKILL.md"), nil, 0o644)
	ws := &Workspace{Root: root, Harness: "codex", Agents: []*Agent{{Name: "Sigrid"}}}
	m := &model{ws: ws, st: NewStyles(true), width: 100, height: 40}

	m.key("c")
	if m.menu == nil || Harnesses[m.menu.cursor] != "codex" {
		t.Fatalf("menu should open on the current harness: %+v", m.menu)
	}
	out := m.render()
	for _, want := range []string{"Harness for Sigrid", " › ● codex     current · workspace default", "○ claude", "○ gemini    gemini not installed", "↑↓ choose · enter save · esc cancel"} {
		if !strings.Contains(out, want) {
			t.Errorf("menu lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(m.render(), "Harness for") || strings.Contains(out, "c harness") {
		t.Errorf("the menu footer should replace the table footer")
	}

	// A harness that cannot run here says why and the menu stays open.
	m.menuKey("down")
	if cmd := m.menuKey("enter"); cmd != nil || m.menu == nil || m.status != "✗ gemini: gemini not installed" {
		t.Errorf("unavailable: status %q, menu %v", m.status, m.menu)
	}
	// The current harness: nothing to save.
	m.menuKey("up")
	m.menuKey("enter")
	if m.menu != nil || m.status != "Sigrid already uses codex" {
		t.Errorf("same: status %q", m.status)
	}
	// q and esc close the menu, not the dashboard.
	m.key("c")
	if cmd := m.menuKey("q"); cmd != nil || m.menu != nil {
		t.Errorf("q should only close the menu")
	}
	if !strings.Contains(m.render(), "enter open · c harness") {
		t.Errorf("table footer should name c")
	}
}

func TestLookPathUsesTheLoginShellPath(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "nvm", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "pi"), []byte("not executable"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Startup noise, an alias line, then the PATH.
	shell := filepath.Join(dir, "fakeshell")
	script := "#!/bin/sh\necho 'Welcome back'\necho \"alias claude='claude --flag'\"\nprintf '\\nGOAGENTIC_PATH=%s\\n' '/nowhere:" + bin + "'\n"
	if err := os.WriteFile(shell, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", shell)
	t.Setenv("PATH", dir) // nothing on the plugin's own PATH

	defer func(orig func() string) { shellPath = orig }(shellPath)
	shellPath = sync.OnceValue(probeShellPath)
	if path, err := lookPath("codex"); err != nil || path != filepath.Join(bin, "codex") {
		t.Errorf("codex via the shell PATH = %q, %v", path, err)
	}
	for _, cli := range []string{"claude", "pi", "gemini"} {
		if path, err := lookPath(cli); err == nil {
			t.Errorf("%s should not be found, got %q", cli, path)
		}
	}
}

func TestSetHarness(t *testing.T) {
	write := func(t *testing.T, front string) (*Agent, string) {
		dir := t.TempDir()
		path := filepath.Join(dir, "context.md")
		if err := os.WriteFile(path, []byte("---\n"+front+"---\n\n# Context\n\nscope: not frontmatter\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return &Agent{Name: "Sigrid", Dir: dir}, path
	}
	read := func(t *testing.T, path string) string {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	ws := &Workspace{} // inherits claude

	// No line yet: one is added, the rest of the file untouched.
	a, path := write(t, "scope: Acme\ntitle: Ops\n")
	if sum, err := SetHarness(ws, a, "codex"); err != nil || sum != "Sigrid → codex" {
		t.Fatalf("add: %q %v", sum, err)
	}
	if got := read(t, path); got != "---\nscope: Acme\ntitle: Ops\nharness: codex\n---\n\n# Context\n\nscope: not frontmatter\n" {
		t.Errorf("add:\n%s", got)
	}

	// A different harness replaces the line and drops the agent's model.
	a, path = write(t, "scope: Acme\nharness: codex   # trial\nmodel: gpt-5\ntitle: Ops\n")
	a.Harness, a.Model = "codex", "gpt-5"
	if sum, err := SetHarness(ws, a, "pi"); err != nil || sum != "Sigrid → pi; its model gpt-5 was removed" {
		t.Fatalf("replace: %q %v", sum, err)
	}
	if got := read(t, path); !strings.HasPrefix(got, "---\nscope: Acme\ntitle: Ops\nharness: pi\n---\n") {
		t.Errorf("replace:\n%s", got)
	}

	// Back to what the workspace gives: the agent's own line goes.
	a.Harness, a.Model = "pi", ""
	if sum, err := SetHarness(ws, a, "claude"); err != nil || sum != "Sigrid → claude (workspace default)" {
		t.Fatalf("remove: %q %v", sum, err)
	}
	if got := read(t, path); !strings.HasPrefix(got, "---\nscope: Acme\ntitle: Ops\n---\n") {
		t.Errorf("remove:\n%s", got)
	}

	// Same harness again keeps the model.
	a, path = write(t, "harness: pi\nmodel: openrouter/x\n")
	a.Harness, a.Model = "pi", "openrouter/x"
	if _, err := SetHarness(ws, a, "pi"); err != nil || !strings.Contains(read(t, path), "model: openrouter/x") {
		t.Errorf("same harness dropped the model:\n%s", read(t, path))
	}

	// No frontmatter: refuse, change nothing.
	dir := t.TempDir()
	plain := filepath.Join(dir, "context.md")
	os.WriteFile(plain, []byte("# Context\n"), 0o644)
	if _, err := SetHarness(ws, &Agent{Name: "X", Dir: dir}, "codex"); err == nil || read(t, plain) != "# Context\n" {
		t.Errorf("no frontmatter: %v", err)
	}
}

func TestHarnessMenuSavesWithoutOpening(t *testing.T) {
	defer func(orig func(string) (string, error)) { lookPath = orig }(lookPath)
	lookPath = func(cli string) (string, error) { return "/bin/" + cli, nil }
	root := t.TempDir()
	dir := filepath.Join(root, "agents", "Sigrid")
	os.MkdirAll(filepath.Join(root, ".agents/skills/agents-start"), 0o755)
	os.WriteFile(filepath.Join(root, ".agents/skills/agents-start/SKILL.md"), nil, 0o644)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "context.md"), []byte("---\nscope: Acme\n---\n"), 0o644)
	// Running in claude now.
	agent := &Agent{Name: "Sigrid", Dir: dir, Live: &LivePane{PaneID: "w1:p2", Running: true}}
	ws := &Workspace{Root: root, Agents: []*Agent{agent}}
	scans := 0
	m := &model{ws: ws, st: NewStyles(true), width: 100, height: 40, scan: func(bool) (*Workspace, error) {
		scans++
		return &Workspace{Root: root, Agents: []*Agent{{Name: "Sigrid", Dir: dir, Harness: "codex"}}}, nil
	}}
	m.key("c")
	m.menuKey("down") // codex
	if cmd := m.menuKey("enter"); cmd != nil {
		t.Fatalf("saving must not open the agent")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "context.md"))
	if string(data) != "---\nscope: Acme\nharness: codex\n---\n" {
		t.Errorf("context.md:\n%s", data)
	}
	if m.menu != nil || scans != 1 || harnessOf(m.ws, m.selected()) != "codex" ||
		m.status != "Sigrid → codex; it is running now, so this applies from its next start" {
		t.Errorf("after save: scans %d, status %q", scans, m.status)
	}
}

func TestOrangeMeansP1WaitingOnOthers(t *testing.T) {
	tr := parseActions("## Open\n\n| # | Action | Owner | Priority | Due/Target | Status |\n|---|---|---|---|---|---|\n" +
		"| 1 | Late | A | P1 | 2026-09-01 | In progress |\n")
	ws := &Workspace{Principal: "Norman Noble", Scanned: now}
	overdue := &Agent{Open: tr.open, Health: Health{Level: Fail}}
	if got := agentTone(ws, overdue); got >= toneWaiting {
		t.Error("an overdue item or a health failure alone must not colour the dot")
	}
	tr = parseActions("## Open\n\n| # | Action | Owner | Priority | Due/Target | Status |\n|---|---|---|---|---|---|\n" +
		"| 1 | Quote | A | P1 | | Waiting on Ivan — reply |\n")
	if got := agentTone(ws, &Agent{Open: tr.open}); got != toneWaiting {
		t.Errorf("a P1 waiting on someone else: tone = %v, want orange", got)
	}
}

func TestAgentNameFromNameFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dario")
	if got := agentName(dir); got != "dario" {
		t.Errorf("no name.md: %q", got)
	}
	write(t, filepath.Join(dir, "name.md"), "# Dario — Name\n\n## The Name\n")
	if got := agentName(dir); got != "Dario" {
		t.Errorf("name.md spelling: %q", got)
	}
	write(t, filepath.Join(dir, "name.md"), "# Astrid — Name\n")
	if got := agentName(dir); got != "dario" {
		t.Errorf("a heading naming someone else must not rename: %q", got)
	}
}

// A retired agent's items stay out of the workspace badge, also when the
// page shown (an agent's, a team's) scanned with retired agents.
func TestWorkspaceBadgeSkipsRetired(t *testing.T) {
	root := fixture(t)
	write(t, filepath.Join(root, "agents/Cato/actions.md"), sectionedTracker)
	shown, _ := Scan(root, Options{Now: now})
	withRetired, _ := Scan(root, Options{Now: now, IncludeRetired: true})
	want := workspaceLink(shown, false).Badge
	if got := workspaceLink(withRetired, false).Badge; got != want {
		t.Errorf("badge with retired scanned = %d, want %d", got, want)
	}
	if got := workspaceLink(withRetired, true).Badge; got <= want {
		t.Errorf("badge with retired shown = %d, want more than %d", got, want)
	}
}

// A workspace row's split pill: waiting on you left, P1 items waiting on
// others right, the same counts as the terminal header.
func TestWorkspaceLinkSplitsBadge(t *testing.T) {
	root := fixture(t)
	write(t, filepath.Join(root, "agents/Varro/actions.md"), "# Actions\n\n## Open\n\n"+
		"| # | Action | Ticket | Owner | Priority | Due/Target | Status | Since |\n|---|---|---|---|---|---|---|---|\n"+
		"| 1 | **Get the quote** | | Varro | P1 | | Waiting on Dario | 2026-09-01 |\n"+
		"| 2 | **File it** | | Varro | P2 | | Waiting on Dario | 2026-09-01 |\n")
	ws, _ := Scan(root, Options{Now: now})
	c := ws.Counts()
	if c.Waiting != 1 {
		t.Fatalf("P1 waiting on others = %d, want 1", c.Waiting)
	}
	l := workspaceLink(ws, false)
	if l.Badge != c.ForYou || l.Others != c.Waiting {
		t.Errorf("link = %d|%d, want %d|%d", l.Badge, l.Others, c.ForYou, c.Waiting)
	}
}

// An agent's sidebar row carries the same split pill as a workspace's.
func TestAgentNavLinkSplitsBadge(t *testing.T) {
	root := fixture(t)
	write(t, filepath.Join(root, "agents/Varro/actions.md"), "# Actions\n\n## Open\n\n"+
		"| # | Action | Ticket | Owner | Priority | Due/Target | Status | Since |\n|---|---|---|---|---|---|---|---|\n"+
		"| 1 | **Get the quote** | | Varro | P1 | | Waiting on Dario | 2026-09-01 |\n")
	ws, _ := Scan(root, Options{Now: now})
	s := &webServer{src: WebSource{Root: root, Options: webOptions}}
	for _, l := range s.workspaceNav(ws, "", false).Agents {
		if l.Name == "Varro" && l.Others != 1 {
			t.Errorf("Varro others = %d, want 1", l.Others)
		}
	}
}

// A scheduled T3 thread runs in a worktree under ~/.t3/worktrees/<root's
// name>/, so its transcript sits in that worktree's project directory; it
// is the agent's session, not an older one left in the root's.
func TestTranscriptDirsT3Worktrees(t *testing.T) {
	tr, t3home := t.TempDir(), t.TempDir()
	t.Setenv("T3CODE_HOME", t3home)
	root := "/home/someone/mindvalley"
	line := func(args, ts string) string {
		return `{"type":"user","message":{"content":"<command-name>/agents:start</command-name>\n<command-args>` + args + `</command-args>"},"timestamp":"` + ts + `"}` + "\n"
	}
	write(t, filepath.Join(tr, projectDirName(root), "old.jsonl"), line("Jeeves", "2026-11-09T12:40:00Z"))
	wt := projectDirName(filepath.Join(t3home, "worktrees", "mindvalley", "t3-721a37df"))
	write(t, filepath.Join(tr, wt, "new.jsonl"), line("Jeeves Scheduled morning run", "2026-11-10T01:15:00Z"))
	// Another project whose name starts the same is not this workspace's.
	write(t, filepath.Join(tr, projectDirName(filepath.Join(t3home, "worktrees", "mindvalley2", "t3-1")), "x.jsonl"), line("Jeeves", "2026-11-11T01:00:00Z"))

	dirs := transcriptDirs(tr, root)
	if len(dirs) != 2 {
		t.Fatalf("dirs = %v", dirs)
	}
	c := NewTranscriptCache()
	var best Start
	for _, d := range dirs {
		if s, ok := c.Starts(d)["jeeves"]; ok && s.Started.After(best.Started) {
			best = s
		}
	}
	if filepath.Base(best.Path) != "new.jsonl" {
		t.Errorf("Jeeves's session = %s, want the worktree's", best.Path)
	}
}
