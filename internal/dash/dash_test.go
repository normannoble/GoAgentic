package dash

import (
	"bytes"
	"encoding/json"

	"github.com/charmbracelet/x/ansi"
	"os"
	"path/filepath"
	"strings"
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
		"Waiting on Vishen/Dario":           true,
		"Draft delivered — awaiting Norman": true,
		"Gated by budget":                   true,
		"Unblocked (MV side); part waits":   false,
		"In progress — delegated to Pax":    false,
		"Open":                              false,
	} {
		if got := (Item{Status: status}).Blocked(); got != want {
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
	if (Item{Status: "awaiting Norman"}).BlockedOnOthers("Norman Noble") {
		t.Error("an item waiting on the principal is not blocked on others")
	}
	if !(Item{Status: "waiting on Dario"}).BlockedOnOthers("Norman Noble") {
		t.Error("waiting on Dario is blocked on others")
	}
}

func TestDetailBlockedSection(t *testing.T) {
	ws := &Workspace{Principal: "Norman Noble", Scanned: now}
	a := &Agent{Name: "Mimir", Open: []Item{
		{ID: "67", Action: "Close-out", Owner: "Mimir", Priority: "P1", Status: "In progress"},
		{ID: "53", Action: "Rescope", Owner: "Mimir", Priority: "P1", Status: "Unblocked (MV side)"},
		{ID: "64", Action: "Re-cut", Owner: "Mimir/Norman", Priority: "P1", Status: "awaiting Norman"},
		{ID: "68", Action: "Airtable renewal", Owner: "Norman/Dario", Priority: "P1", Status: "waiting on Dario"},
		{ID: "44", Action: "InfoSec KRs", Owner: "Mimir", Priority: "P2", Status: "Awaiting draft"},
	}}
	out := ansi.Strip(strings.Join(Detail(ws, a, 120, 0, NewStyles(true)), "\n"))
	blockedAt := strings.Index(out, "Blocked items")
	openAt := strings.Index(out, "Open actions")
	waitAt := strings.Index(out, "Waiting on Norman")
	if blockedAt < 0 || openAt < blockedAt || waitAt > blockedAt {
		t.Fatalf("want Waiting, then Blocked items, then Open actions:\n%s", out)
	}
	blocked, open := out[blockedAt:openAt], out[openAt:]
	if !strings.Contains(blocked, "#68") || strings.Contains(blocked, "#53") || strings.Contains(blocked, "#64") || strings.Contains(blocked, "#44") {
		t.Errorf("blocked section wrong:\n%s", blocked)
	}
	if strings.Contains(open, "#68") || !strings.Contains(open, "#53") || !strings.Contains(open, "#44") {
		t.Errorf("open list should drop only the blocked P1:\n%s", open)
	}
	if !strings.Contains(out[waitAt:blockedAt], "#64") {
		t.Errorf("#64 should wait on Norman:\n%s", out)
	}
	if !strings.Contains(open, "P1 4 · P2 1") {
		t.Errorf("counts should include blocked items:\n%s", open)
	}
	if a.BlockedCount("Norman Noble") != 1 {
		t.Errorf("blocked count = %d, want 1", a.BlockedCount("Norman Noble"))
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
	if !(pos("#70") < pos("#15") && pos("#15") < pos("#66") && pos("#66") < pos("#19") && pos("#19") < pos("#9 ")) {
		t.Errorf("want P2s under way, then not started, then parked, then P3:\n%s", out)
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
	if !a.Open[1].Blocked() || a.Open[0].Blocked() {
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
	write(t, filepath.Join(sig, "memory/scheduled/inbox.md"), "## a\nUNPROCESSED\n## b\nPROCESSED\n")

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
	WriteSnapshot(&buf, ws, 120, ViewTable, true, NewStyles(true))
	out := buf.String()
	for _, want := range []string{
		"✋ 3 for you", "● Sigrid", "● Varro", "Waiting on Norman:", "Sigrid #5 Tidy memory and links", "Sigrid #6 Sign the contract",
		"1 scheduled run(s) not yet read",
		"» #3 ship the thing → #5 tidy", // the agent's own plan wins
		"(no open actions)",
		"09-27 New session",
		"Open actions — P1 1 · P2 3 · P3 1",
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
	if got.Agents[0].NextUp == nil || got.Agents[0].NextUp.ID != "3" || got.Agents[1].Health != "fail" {
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

	c := NewTranscriptCache()
	starts := c.Starts(dir)
	if got := starts["sigrid"].Started.Format(time.RFC3339); got != "2026-09-28T01:00:00Z" {
		t.Errorf("sigrid started = %s", got)
	}
	if _, ok := starts["varro"]; ok {
		t.Error("peer sessions and non-jsonl files must not count")
	}
	// A second pass reuses the cache and gives the same answer.
	if again := c.Starts(dir); !again["sigrid"].Started.Equal(starts["sigrid"].Started) || len(again["sigrid"].All) != 2 {
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
	if err != nil || sum != "started Varro in a new tab" {
		t.Fatalf("launch = %q, %v", sum, err)
	}
	calls, _ := os.ReadFile(logPath)
	want := "tab create --cwd /ws --label Varro --focus --workspace w1\nagent start varro --kind claude --pane w1:p9 -- /agents:start varro\n"
	if string(calls) != want {
		t.Errorf("calls:\n%s\nwant:\n%s", calls, want)
	}

	t.Setenv("HERDR_BIN_PATH", "")
	t.Setenv("HERDR_ENV", "")
	if _, err := Launch(&Workspace{}, &Agent{Name: "x"}); err != ErrNoHerdr {
		t.Errorf("outside Herdr = %v", err)
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

func TestTileView(t *testing.T) {
	ws, err := Scan(fixture(t), Options{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	WriteSnapshot(&buf, ws, 80, ViewTiles, false, NewStyles(true))
	out := buf.String()
	for _, want := range []string{"✋ 3 for you", "╭─ Sigrid", "╭─ Varro", "#3 ship the thing", "nothing open", "✋3", "⚠"} {
		if !strings.Contains(out, want) {
			t.Errorf("tiles lack %q:\n%s", want, out)
		}
	}
	// Two 38-wide tiles fit side by side in 80 columns, each line exactly
	// the same width.
	lines := strings.Split(out, "\n")
	if TileColumns(80) != 2 {
		t.Errorf("columns at 80 = %d", TileColumns(80))
	}
	for _, l := range lines {
		if n := ansiWidth(l); n > 80 {
			t.Errorf("line wider than 80 (%d): %q", n, l)
		}
	}
	grid, _ := TileGrid(ws, 80, 0, NewStyles(true))
	if len(grid) != tileHeight {
		t.Fatalf("grid rows = %d, want one row of tiles", len(grid))
	}
	w := ansiWidth(grid[0])
	for _, l := range grid {
		if ansiWidth(l) != w {
			t.Errorf("ragged tile row: %q", l)
		}
	}
	if !strings.Contains(grid[0], "┏") {
		t.Error("selected tile should use the heavy border")
	}
}

func TestActivityAndDue(t *testing.T) {
	a := &Agent{
		starts:   []time.Time{now.Add(-time.Hour), now.Add(-time.Hour * 2), now.Add(-48 * time.Hour)},
		Sessions: []Session{{Date: now.AddDate(0, 0, -5)}, {Date: now.AddDate(0, 0, -30)}},
	}
	got := activity(a, now)
	if len(got) != activityDays || got[11] != 2 || got[9] != 1 || got[6] != 1 || got[0] != 0 {
		t.Errorf("activity = %v", got)
	}
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
