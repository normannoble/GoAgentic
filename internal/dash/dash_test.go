package dash

import (
	"bytes"
	"encoding/json"
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
	if got := strings.Join(mine, ","); got != "6" {
		t.Errorf("for principal = %s, want only the solely owned row", got)
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
	if got := ws.Summary(); got != "2 for you · 1 !" {
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
		"2 agents", "Waiting on Norman:", "Sigrid #6 Sign the contract",
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
	if again := c.Starts(dir); again["sigrid"] != starts["sigrid"] {
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
