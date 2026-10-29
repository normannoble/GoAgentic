package dash

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseT3Threads(t *testing.T) {
	data := []byte(`[
		{"threadId":"a","title":"Sigrid - Contracts Partner","status":"completed","settled":false,"providerInstanceId":"claudeAgent"},
		{"threadId":"b","title":"Sigrid - Chief of Staff","status":"completed","settled":true,"providerInstanceId":"claudeAgent"},
		{"threadId":"c","title":"Bo - Peer","status":"completed","settled":false,"providerInstanceId":"codex"},
		{"threadId":"d","title":"Bo - Task","status":"running","settled":false,"providerInstanceId":"codex"},
		{"threadId":"e","title":"/agents:start Bo","status":"running","settled":false,"providerInstanceId":"claudeAgent"},
		{"threadId":"f","title":"Astrid - Partner","status":"running","settled":false,"providerInstanceId":"claudeAgent"}
	]`)
	panes, err := parseT3Threads(data, "/w")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]LivePane{}
	for _, p := range panes {
		got[p.PaneID] = p
	}
	if len(panes) != 3 {
		t.Fatalf("want 3 live threads (a, d, f), got %d: %+v", len(panes), panes)
	}
	if p := got["t3:a"]; p.Name != "sigrid" || p.Status != "idle" || !p.Running || p.CWD != "/w" || p.Agent != "claude" {
		t.Errorf("open human session: %+v", p)
	}
	if p := got["t3:d"]; p.Name != "bo-task" || p.Status != "working" || p.Agent != "codex" || sessionPane(&p) {
		t.Errorf("running task thread: %+v", p)
	}
	if p := got["t3:f"]; p.Status != "working" {
		t.Errorf("running session: %+v", p)
	}
}

func TestAttachLiveT3(t *testing.T) {
	ws := &Workspace{Root: "/w", Agents: []*Agent{{Name: "Sigrid"}}}
	panes, _ := parseT3Threads([]byte(`[{"threadId":"a","title":"Sigrid - Contracts Partner","status":"completed","settled":false,"providerInstanceId":"claudeAgent"}]`), "/w")
	attachLive(ws, panes)
	if ws.Agents[0].Live == nil || !IsT3Pane(ws.Agents[0].Live.PaneID) {
		t.Fatalf("Sigrid not matched to her thread: %+v", ws.Agents[0].Live)
	}
}

// fakeT3 points T3CODE_CLI_PATH at a script that logs its arguments.
func fakeT3(t *testing.T, reply string) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "args")
	bin := filepath.Join(dir, "t3")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >> " + log + "\nprintf '%s' '" + reply + "'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("T3CODE_CLI_PATH", bin)
	return log
}

func fakeArgs(t *testing.T, log string) string {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(strings.TrimSpace(string(b)), "\n", " ")
}

func TestT3Actions(t *testing.T) {
	log := fakeT3(t, "")
	if err := livePrompt("t3:abc", "hello there"); err != nil {
		t.Fatal(err)
	}
	if _, err := liveRead("t3:abc"); err != nil {
		t.Fatal(err)
	}
	ws := &Workspace{Root: "/w"}
	if _, err := t3Launch(ws, &Agent{Name: "Sigrid", Title: "Contracts Partner"}); err != nil {
		t.Fatal(err)
	}
	want := "thread send abc hello there thread show abc --messages 2 thread start --title Sigrid - Contracts Partner /w /agents:start Sigrid"
	if got := fakeArgs(t, log); got != want {
		t.Errorf("t3 calls:\n got %q\nwant %q", got, want)
	}
	if err := (&webServer{}).closePane("t3:abc"); err == nil || !strings.Contains(err.Error(), "settle") {
		t.Errorf("closing a thread should ask to settle it in T3, got %v", err)
	}
}

func TestT3ThreadsScan(t *testing.T) {
	log := fakeT3(t, `[{"threadId":"a","title":"Sigrid - Partner","status":"running","settled":false,"providerInstanceId":"claudeAgent"}]`)
	panes, err := T3Threads("/w")
	if err != nil || len(panes) != 1 || panes[0].Status != "working" {
		t.Fatalf("panes %+v, err %v", panes, err)
	}
	if got := fakeArgs(t, log); got != "thread list --project /w --json" {
		t.Errorf("t3 call %q", got)
	}
}

func TestUseT3(t *testing.T) {
	t3ws := &Workspace{Panes: []LivePane{{PaneID: "t3:a"}}}
	plain := &Workspace{}
	fakeT3(t, "")
	cases := []struct {
		ws                    *Workspace
		herdrUp, herdrHasRoot bool
		want                  bool
	}{
		{plain, false, false, true}, // Herdr down: T3
		{t3ws, true, false, true},   // Herdr up elsewhere, workspace in T3
		{t3ws, true, true, false},   // workspace has Herdr panes: Herdr
		{plain, true, false, false}, // nothing live anywhere, Herdr up: Herdr
	}
	for i, c := range cases {
		if got := useT3(c.ws, c.herdrUp, c.herdrHasRoot); got != c.want {
			t.Errorf("case %d: useT3 = %v, want %v", i, got, c.want)
		}
	}
	t.Setenv("T3CODE_CLI_PATH", "")
	saved := lookPath
	t.Cleanup(func() { lookPath = saved })
	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	if useT3(plain, false, false) {
		t.Error("no t3 CLI: never T3")
	}
}
