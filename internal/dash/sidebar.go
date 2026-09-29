package dash

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
)

// SidebarToken is the Herdr workspace metadata token the summary is written
// to; a Space sidebar row shows it as $agents.
const SidebarToken = "agents"

const sidebarSource = "goagentic.dash"

type herdrPane struct {
	WorkspaceID string `json:"workspace_id"`
	CWD         string `json:"cwd"`
}

// ReportSidebar writes each Herdr workspace's agent summary into its
// $agents token. A Herdr workspace belongs to the agent workspace its panes
// sit in; Herdr workspaces with no agent workspace get the token cleared.
// It returns the summaries it reported, keyed by Herdr workspace id.
func ReportSidebar() (map[string]string, error) {
	bin := herdrBin()
	all, err := listHerdrPanes(bin)
	if err != nil {
		return nil, err
	}
	rootOf := sidebarRoots(all)

	panes, _ := parseHerdrPanes(bin)
	summaries := map[string]string{}
	byRoot := map[string]string{}
	ids := make([]string, 0, len(rootOf))
	for id := range rootOf {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		root := rootOf[id]
		summary := ""
		if root != "" {
			s, done := byRoot[root]
			if !done {
				ws, err := Scan(root, Options{Herdr: func() ([]LivePane, error) { return panes, nil }})
				if err == nil {
					s = ws.Summary()
				}
				byRoot[root] = s
			}
			summary = s
		}
		// Herdr's parser wants the workspace id before the flags.
		args := []string{"workspace", "report-metadata", id, "--source", sidebarSource}
		if summary == "" {
			args = append(args, "--clear-token", SidebarToken)
		} else {
			args = append(args, "--token", SidebarToken+"="+summary)
		}
		if err := exec.Command(bin, args...).Run(); err != nil {
			return summaries, fmt.Errorf("herdr workspace report-metadata %s: %w", id, err)
		}
		summaries[id] = summary
	}
	return summaries, nil
}

// sidebarRoots maps each Herdr workspace to the agent workspace most of its
// panes sit in ("" when none do).
func sidebarRoots(panes []herdrPane) map[string]string {
	votes := map[string]map[string]int{}
	for _, p := range panes {
		if votes[p.WorkspaceID] == nil {
			votes[p.WorkspaceID] = map[string]int{}
		}
		root, err := FindRoot(p.CWD)
		if err != nil || p.CWD == "" {
			root = ""
		}
		votes[p.WorkspaceID][root]++
	}
	out := map[string]string{}
	for id, v := range votes {
		best, n := "", 0
		for root, c := range v {
			if root != "" && (c > n || (c == n && root < best)) {
				best, n = root, c
			}
		}
		out[id] = best
	}
	return out
}

func parseHerdrPanes(bin string) ([]LivePane, error) {
	out, err := exec.Command(bin, "agent", "list").Output()
	if err != nil {
		return nil, err
	}
	return parseHerdrAgents(out)
}

// HerdrWorkspaceRoot is the agent workspace most of a Herdr workspace's panes
// sit in, for when the focused pane itself is outside any workspace.
func HerdrWorkspaceRoot(herdrWorkspaceID string) (string, error) {
	all, err := listHerdrPanes(herdrBin())
	if err != nil {
		return "", err
	}
	if root := sidebarRoots(all)[herdrWorkspaceID]; root != "" {
		return root, nil
	}
	return "", ErrNoWorkspace
}

func herdrBin() string {
	if bin := os.Getenv("HERDR_BIN_PATH"); bin != "" {
		return bin
	}
	return "herdr"
}

func listHerdrPanes(bin string) ([]herdrPane, error) {
	out, err := exec.Command(bin, "pane", "list").Output()
	if err != nil {
		return nil, fmt.Errorf("herdr pane list: %w", err)
	}
	var resp struct {
		Result struct {
			Panes []herdrPane `json:"panes"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, err
	}
	return resp.Result.Panes, nil
}
