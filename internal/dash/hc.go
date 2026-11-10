package dash

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// The Herdr app (the desktop app, with herdr-core) runs terminals and agents
// in herdr-core, driven by the `hc` CLI, not in the terminal Herdr. The
// dashboard reads them with `hc ls` and `hc get` and treats each agent
// terminal as a pane whose id is "hc:<address>", so live state, typing a
// line, Wrap, Close and Start work as they do for Herdr panes. A terminal
// carrying the labels herdr.space=<id> and herdr.tab=<id> shows in that
// Space and tab of the app; an id the app does not know becomes a new one.

// hcPanePrefix marks a LivePane that is a herdr-core terminal.
const hcPanePrefix = "hc:"

// ErrNoHc means the hc CLI could not be found.
var ErrNoHc = errors.New("the hc CLI (herdr-core) was not found")

// IsHcPane reports whether a pane id names a herdr-core terminal.
func IsHcPane(pane string) bool { return strings.HasPrefix(pane, hcPanePrefix) }

func hcAddress(pane string) string { return strings.TrimPrefix(pane, hcPanePrefix) }

// hcBin is the hc CLI: on PATH or the login-shell PATH, as `hc` or by its
// long name.
func hcBin() (string, error) {
	for _, name := range []string{"hc", "herdr-core"} {
		if bin, err := lookPath(name); err == nil {
			return bin, nil
		}
	}
	return "", ErrNoHc
}

// inHcTerminal is true when the dashboard itself runs in a herdr-core
// terminal (opened from the Herdr app).
func inHcTerminal() bool { return os.Getenv("HC_TERMINAL_ID") != "" }

func hcRun(timeout time.Duration, args ...string) ([]byte, error) {
	bin, err := hcBin()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, args...).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("hc %s: %s", args[0], strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("hc %s: %w", args[0], err)
	}
	return out, nil
}

type hcListed struct {
	Address string `json:"address"`
	Agent   string `json:"agent"`
	State   string `json:"state"`
	Status  string `json:"status"`
}

type hcTerminal struct {
	Address    string            `json:"address"`
	Name       *string           `json:"name"`
	CWD        string            `json:"cwd"`
	State      string            `json:"state"`
	Labels     map[string]string `json:"labels"`
	Foreground *struct {
		Name string `json:"name"`
		CWD  string `json:"cwd"`
	} `json:"foreground"`
	Status *struct {
		App   string `json:"app"`
		State string `json:"state"`
	} `json:"status"`
}

// HcTerminals lists the live agent terminals herdr-core runs on this machine
// as panes. It returns an error when hc is missing or its server is down.
func HcTerminals() ([]LivePane, error) {
	out, err := hcRun(5*time.Second, "ls", "--json")
	if err != nil {
		return nil, err
	}
	var list struct {
		Terminals []hcListed `json:"terminals"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, err
	}
	var panes []LivePane
	for _, t := range list.Terminals {
		if t.State != "live" || !hcAgentLike(t) {
			continue
		}
		raw, err := hcRun(3*time.Second, "get", t.Address, "--json")
		if err != nil {
			continue
		}
		var full hcTerminal
		if json.Unmarshal(raw, &full) != nil {
			continue
		}
		if p, ok := hcPane(full); ok {
			panes = append(panes, p)
		}
	}
	return panes, nil
}

// hcAgentLike keeps the terminals worth a closer look: one whose program
// reports a status, or runs a coding-agent CLI, or a shell (an agent's
// terminal after its CLI exited, which the dashboard shows as "shell").
func hcAgentLike(t hcListed) bool {
	return t.Status != "" || hcHarness(t.Agent) != "" || isShell(t.Agent)
}

func isShell(name string) bool {
	switch filepath.Base(name) {
	case "sh", "bash", "zsh", "fish", "nu", "dash", "ksh", "tcsh", "csh", "pwsh", "-bash", "-zsh":
		return true
	}
	return false
}

// hcHarness maps a program or status app ("claude-code", "codex",
// "cursor-agent") to the harness name the framework commands are spelt for,
// or "" when it is not a coding-agent CLI.
func hcHarness(program string) string {
	p := strings.ToLower(filepath.Base(program))
	for _, h := range []string{"claude", "codex", "gemini", "opencode", "cursor", "pi"} {
		if p == h || strings.HasPrefix(p, h+"-") || strings.HasPrefix(p, h+"_") {
			return h
		}
	}
	return ""
}

// hcPane turns one terminal into a pane: named by its hc name when it has
// one (the dashboard starts agents with their lowercased name), labelled by
// its tab in the app ("Acme / Varro" → "Varro"), in its program's folder.
func hcPane(t hcTerminal) (LivePane, bool) {
	p := LivePane{PaneID: hcPanePrefix + t.Address, CWD: t.CWD}
	if t.Name != nil {
		p.Name = *t.Name
	}
	if where := t.Labels["where"]; where != "" {
		if i := strings.LastIndex(where, " / "); i >= 0 {
			where = where[i+3:]
		}
		p.Label = where
	}
	if n := t.Labels["goagentic.agent"]; n != "" && p.Name == "" {
		p.Name = strings.ToLower(n)
	}
	program := ""
	if t.Foreground != nil {
		program = t.Foreground.Name
		if t.Foreground.CWD != "" {
			p.CWD = t.Foreground.CWD
		}
	}
	if h := hcHarness(program); h != "" {
		p.Agent, p.Running = h, true
	} else if t.Status != nil && !isShell(program) && program != "" {
		p.Agent, p.Running = hcHarness(t.Status.App), hcHarness(t.Status.App) != ""
	}
	if p.Name == "" && p.Label == "" {
		return p, false
	}
	switch {
	case !p.Running:
		p.Status = ""
	case t.Status == nil:
		p.Status = "idle"
	case t.Status.State == "done":
		p.Status = "idle" // finished while nobody looked: idle in Herdr's words
	default:
		p.Status = t.Status.State
	}
	return p, true
}

// hcPrompt pastes text into a terminal and presses Enter.
func hcPrompt(pane, text string) error {
	_, err := hcRun(15*time.Second, "send", hcAddress(pane), text)
	return err
}

// hcRead is the terminal's last lines, for the activity line.
func hcRead(pane string) (string, error) {
	out, err := hcRun(3*time.Second, "read", hcAddress(pane), "--lines", "60")
	return string(out), err
}

// hcClose ends the terminal; the Herdr app closes its pane with it.
func hcClose(pane string) error {
	_, err := hcRun(10*time.Second, "kill", hcAddress(pane))
	return err
}

// hcSpace is the app Space a Start for this workspace opens in: the
// dashboard's own Space when it runs in the app, else the Space of a
// terminal already in the workspace, else "" (a new Space).
func hcSpace(ws *Workspace) string {
	if id := os.Getenv("HC_TERMINAL_ID"); id != "" {
		if s := hcLabel(id, "herdr.space"); s != "" {
			return s
		}
	}
	for _, p := range ws.Panes {
		if IsHcPane(p.PaneID) {
			if s := hcLabel(hcAddress(p.PaneID), "herdr.space"); s != "" {
				return s
			}
		}
	}
	return ""
}

func hcLabel(address, key string) string {
	raw, err := hcRun(3*time.Second, "get", address, "--json")
	if err != nil {
		return ""
	}
	var t hcTerminal
	if json.Unmarshal(raw, &t) != nil {
		return ""
	}
	return t.Labels[key]
}

func hcNewID(prefix string) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// hcStartArgs is the `hc run` that opens the agent in a new tab of space (a
// new Space when space is ""): its CLI runs in the user's login shell, which
// stays open after the CLI exits, as a Herdr tab does.
func hcStartArgs(ws *Workspace, a *Agent, space, name string) []string {
	harness := harnessOf(ws, a)
	line := []string{executable(harness)}
	if m := agentModel(ws, a); m != "" {
		line = append(line, "--model", shellQuote(m))
	}
	line = append(line, shellQuote(startCommand(harness, strings.ToLower(a.Name))))
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	if space == "" {
		space = hcNewID("spc")
	}
	args := []string{"run", "--bg", "--json", "--no-parent", "--outlive-parent", "--keep"}
	if name != "" {
		args = append(args, "--name", name)
	}
	return append(args,
		"--cwd", ws.Root,
		"--label", "herdr.space="+space,
		"--label", "herdr.tab="+hcNewID("tb"),
		"--label", "goagentic.agent="+a.Name,
		"--", shell, "-lic", strings.Join(line, " ")+"; exec "+shell+" -l")
}

// hcLaunch opens the agent in the Herdr app: nothing to do when it is already
// running there (the app cannot be switched from here), its start command
// typed into its terminal when only the shell is left, else a new tab.
func hcLaunch(ws *Workspace, a *Agent) (string, error) {
	harness := harnessOf(ws, a)
	if a.Live != nil && IsHcPane(a.Live.PaneID) {
		if a.Live.Running {
			where := "its tab"
			if a.Live.Label != "" {
				where = "tab " + a.Live.Label
			}
			return a.Name + " is already running in the Herdr app (" + where + ")", nil
		}
		line := executable(harness) + " " + shellQuote(startCommand(harness, strings.ToLower(a.Name)))
		if err := hcPrompt(a.Live.PaneID, line); err != nil {
			return "", err
		}
		return "started " + a.Name + " (" + harness + ") in its tab", nil
	}
	if a.Live != nil && a.Live.Running {
		// Running in the terminal Herdr (or T3): a second session would
		// fight the first over its tracker.
		return a.Name + " is already running in the terminal Herdr; wrap it there before starting it here", nil
	}
	space := hcSpace(ws)
	name := strings.ToLower(a.Name)
	_, err := hcRun(30*time.Second, hcStartArgs(ws, a, space, name)...)
	if err != nil && strings.Contains(err.Error(), "name") {
		// Its name is taken by a live terminal: start it unnamed; the
		// goagentic.agent label still ties it to the agent.
		_, err = hcRun(30*time.Second, hcStartArgs(ws, a, space, "")...)
	}
	if err != nil {
		return "", err
	}
	return "started " + a.Name + " (" + harness + ") in a new Herdr app tab", nil
}

// useHc picks the Herdr app for a Start: hc is installed and its server
// answers, and either the dashboard runs in the app, or the workspace's live
// sessions are app terminals and none are terminal-Herdr panes.
func useHc(ws *Workspace) bool {
	if _, err := hcBin(); err != nil {
		return false
	}
	if inHcTerminal() {
		return true
	}
	hc, other := false, false
	root := filepath.Clean(ws.Root)
	for _, p := range ws.Panes {
		cwd := filepath.Clean(p.CWD)
		if cwd != root && !strings.HasPrefix(cwd, root+string(filepath.Separator)) {
			continue
		}
		switch {
		case IsHcPane(p.PaneID):
			hc = true
		case !IsT3Pane(p.PaneID):
			other = true
		}
	}
	return hc && !other
}
