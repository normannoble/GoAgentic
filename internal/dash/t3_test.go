package dash

import "testing"

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
