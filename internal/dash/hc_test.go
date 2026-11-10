package dash

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHcPane(t *testing.T) {
	name := "varro"
	cases := []struct {
		raw     string
		want    LivePane
		visible bool
	}{
		{ // started in the app by hand: matched by its tab name
			`{"address":"t1","name":null,"cwd":"/home/u","state":"live","labels":{"where":"Acme / Varro"},"foreground":{"name":"claude","cwd":"/w/acme"},"status":{"app":"claude-code","state":"done"}}`,
			LivePane{PaneID: "hc:t1", Label: "Varro", CWD: "/w/acme", Agent: "claude", Status: "idle", Running: true}, true,
		},
		{ // started by the dashboard: named for the agent
			`{"address":"t4","name":"varro","cwd":"/w/acme","state":"live","labels":{"where":"Acme / 2","goagentic.agent":"Varro"},"foreground":{"name":"codex","cwd":"/w/acme"},"status":{"app":"codex","state":"working"}}`,
			LivePane{PaneID: "hc:t4", Name: name, Label: "2", CWD: "/w/acme", Agent: "codex", Status: "working", Running: true}, true,
		},
		{ // its CLI exited: the shell is left
			`{"address":"t5","name":null,"cwd":"/w/acme","state":"live","labels":{"where":"Acme / Varro"},"foreground":{"name":"bash","cwd":"/w/acme"}}`,
			LivePane{PaneID: "hc:t5", Label: "Varro", CWD: "/w/acme"}, true,
		},
		{ // a worker the app does not show and nobody named
			`{"address":"t6","name":null,"cwd":"/tmp","state":"live","labels":{},"foreground":{"name":"cargo","cwd":"/tmp"}}`,
			LivePane{}, false,
		},
	}
	for i, c := range cases {
		var term hcTerminal
		if err := jsonUnmarshal(c.raw, &term); err != nil {
			t.Fatal(err)
		}
		got, ok := hcPane(term)
		if ok != c.visible {
			t.Errorf("case %d: visible %v, want %v", i, ok, c.visible)
			continue
		}
		if ok && (got.PaneID != c.want.PaneID || got.Name != c.want.Name || got.Label != c.want.Label ||
			got.CWD != c.want.CWD || got.Agent != c.want.Agent || got.Status != c.want.Status || got.Running != c.want.Running) {
			t.Errorf("case %d:\n got %+v\nwant %+v", i, got, c.want)
		}
	}
}

func TestAttachLiveHc(t *testing.T) {
	ws := &Workspace{Root: "/w/acme", Agents: []*Agent{{Name: "Varro"}, {Name: "Dario"}}}
	var term hcTerminal
	_ = jsonUnmarshal(`{"address":"t1","cwd":"/home/u","state":"live","labels":{"where":"Acme / Varro"},"foreground":{"name":"claude","cwd":"/w/acme"},"status":{"app":"claude-code","state":"working"}}`, &term)
	p, _ := hcPane(term)
	attachLive(ws, []LivePane{p})
	if ws.Agents[0].Live == nil || !IsHcPane(ws.Agents[0].Live.PaneID) {
		t.Fatalf("Varro not matched to his terminal: %+v", ws.Agents[0].Live)
	}
	if ws.Agents[1].Live != nil {
		t.Errorf("Dario matched a terminal that is not his")
	}
}

func TestHcHarness(t *testing.T) {
	for in, want := range map[string]string{"claude-code": "claude", "claude": "claude", "codex": "codex", "cursor-agent": "cursor", "pi": "pi", "pip": "", "bash": "", "piper": ""} {
		if got := hcHarness(in); got != want {
			t.Errorf("hcHarness(%q) = %q, want %q", in, got, want)
		}
	}
}

// fakeHc puts a stand-in hc first on PATH that logs its arguments and
// answers `get` with labels.
func fakeHc(t *testing.T, getReply string) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s|' \"$@\" >> " + log + "\nprintf '\\n' >> " + log +
		"\nif [ \"$1\" = get ]; then printf '%s' '" + getReply + "'; fi\n"
	if err := os.WriteFile(filepath.Join(dir, "hc"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

func TestHcActionsAndLaunch(t *testing.T) {
	log := fakeHc(t, `{"address":"t2","labels":{"herdr.space":"spcabc"}}`)
	t.Setenv("HC_TERMINAL_ID", "t2")
	t.Setenv("SHELL", "/bin/bash")
	if err := livePrompt("hc:t1", "hello"); err != nil {
		t.Fatal(err)
	}
	if _, err := liveRead("hc:t1"); err != nil {
		t.Fatal(err)
	}
	if err := (&webServer{}).closePane("hc:t1"); err != nil {
		t.Fatal(err)
	}
	ws := &Workspace{Root: "/w/acme"}
	msg, err := Launch(ws, &Agent{Name: "Varro"})
	if err != nil || !strings.Contains(msg, "Herdr app tab") {
		t.Fatalf("launch: %q, %v", msg, err)
	}
	b, _ := os.ReadFile(log)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 5 {
		t.Fatalf("want 5 hc calls, got %d:\n%s", len(lines), b)
	}
	for i, want := range []string{"send|t1|hello|", "read|t1|--lines|60|", "kill|t1|", "get|t2|--json|"} {
		if lines[i] != want {
			t.Errorf("call %d: %q, want %q", i, lines[i], want)
		}
	}
	run := lines[4]
	for _, want := range []string{"run|--bg|", "--no-parent|", "--outlive-parent|", "--keep|", "--name|varro|", "--cwd|/w/acme|",
		"--label|herdr.space=spcabc|", "--label|herdr.tab=tb", "--label|goagentic.agent=Varro|",
		"--|/bin/bash|-lic|claude '/agents:start varro'; exec /bin/bash -l|"} {
		if !strings.Contains(run, want) {
			t.Errorf("run call lacks %q:\n%s", want, run)
		}
	}
}

func TestHcLaunchAlreadyRunning(t *testing.T) {
	log := fakeHc(t, "")
	ws := &Workspace{Root: "/w/acme"}
	a := &Agent{Name: "Varro", Live: &LivePane{PaneID: "hc:t1", Label: "Varro", Running: true}}
	msg, err := LaunchInBackground(ws, a)
	if err != nil || !strings.Contains(msg, "already running") {
		t.Fatalf("got %q, %v", msg, err)
	}
	if b, _ := os.ReadFile(log); len(b) != 0 {
		t.Errorf("a running agent needs no hc call, got %s", b)
	}
	// Running in the terminal Herdr: no second session.
	other := &Agent{Name: "Varro", Live: &LivePane{PaneID: "w1:p3", Running: true}}
	t.Setenv("HC_TERMINAL_ID", "t9")
	if msg, err := Launch(ws, other); err != nil || !strings.Contains(msg, "terminal Herdr") {
		t.Errorf("running in Herdr: got %q, %v", msg, err)
	}
	if b, _ := os.ReadFile(log); len(b) != 0 {
		t.Errorf("no hc call for an agent running in Herdr, got %s", b)
	}
	// Only its shell is left: the start command is typed into it.
	a.Live.Running = false
	if _, err := LaunchInBackground(ws, a); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(log); !strings.Contains(string(b), "send|t1|claude '/agents:start varro'|") {
		t.Errorf("want the start typed into t1, got %s", b)
	}
}

func TestUseHc(t *testing.T) {
	fakeHc(t, "")
	t.Setenv("HC_TERMINAL_ID", "")
	hcOnly := &Workspace{Root: "/w", Panes: []LivePane{{PaneID: "hc:t1", CWD: "/w"}, {PaneID: "w1:p2", CWD: "/elsewhere"}}}
	mixed := &Workspace{Root: "/w", Panes: []LivePane{{PaneID: "hc:t1", CWD: "/w"}, {PaneID: "w1:p2", CWD: "/w/sub"}}}
	none := &Workspace{Root: "/w"}
	if !useHc(hcOnly) || useHc(mixed) || useHc(none) {
		t.Errorf("useHc: hc-only %v (want true), mixed %v, none %v (want false)", useHc(hcOnly), useHc(mixed), useHc(none))
	}
	t.Setenv("HC_TERMINAL_ID", "t9")
	if !useHc(none) {
		t.Error("inside the Herdr app a Start opens there")
	}
}

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }
