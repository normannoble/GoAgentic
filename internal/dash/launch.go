package dash

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ErrNoHerdr means opening an agent was asked for outside Herdr.
var ErrNoHerdr = errors.New("opening an agent needs Herdr")

// startCommand is the harness-specific way to start an agent (conventions
// § Invocation).
func startCommand(harness, name string) string {
	switch harness {
	case "codex":
		return "$agents-start " + name
	case "opencode", "pi", "cursor":
		return "/agents-start " + name
	default: // claude, gemini
		return "/agents:start " + name
	}
}

// executable is the harness's CLI command, where it differs from its name.
func executable(harness string) string {
	if harness == "cursor" {
		return "cursor-agent"
	}
	return harness
}

// Harnesses are the CLIs an agent can open in (conventions § Invocation), in
// the order the harness menu lists them.
var Harnesses = []string{"claude", "codex", "gemini", "opencode", "pi", "cursor"}

func knownHarness(h string) bool {
	for _, k := range Harnesses {
		if h == k {
			return true
		}
	}
	return false
}

// harnessOf is the CLI an agent runs under: its own context.md harness:,
// else the workspace's, else claude.
func harnessOf(ws *Workspace, a *Agent) string {
	h, _ := HarnessSource(ws, a)
	return h
}

// HarnessSource is the agent's harness and where it comes from: "agent"
// (its context.md), "workspace" (the conventions frontmatter) or "default".
func HarnessSource(ws *Workspace, a *Agent) (harness, source string) {
	if knownHarness(a.Harness) {
		return a.Harness, "agent"
	}
	if knownHarness(ws.Harness) {
		return ws.Harness, "workspace"
	}
	return "claude", "default"
}

// commandFiles are the files whose presence means a harness has the
// framework commands, relative to the workspace root and to HOME
// (harness/install.sh, project and --user scope). Claude has them through the
// plugin, so it needs none.
var commandFiles = map[string][2]string{
	"codex":    {".agents/skills/agents-start/SKILL.md", ".agents/skills/agents-start/SKILL.md"},
	"cursor":   {".agents/skills/agents-start/SKILL.md", ".agents/skills/agents-start/SKILL.md"},
	"gemini":   {".gemini/commands/agents/start.toml", ".gemini/commands/agents/start.toml"},
	"opencode": {".opencode/commands/agents-start.md", ".config/opencode/commands/agents-start.md"},
	"pi":       {".pi/prompts/agents-start.md", ".pi/agent/prompts/agents-start.md"},
}

// lookPath finds a CLI on PATH, else on the user's login-shell PATH; a
// variable so tests can fake it.
var lookPath = func(cli string) (string, error) {
	if path, err := exec.LookPath(cli); err == nil {
		return path, nil
	}
	for _, dir := range filepath.SplitList(shellPath()) {
		path := filepath.Join(dir, cli)
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return path, nil
		}
	}
	return "", exec.ErrNotFound
}

// shellPath is the PATH the user's login shell sets up, asked for once.
// Herdr starts the dashboard as a plugin with the system PATH only, while
// the CLIs often live where the shell's startup files put them (nvm,
// ~/.local/bin, Homebrew); Herdr starts agents through that shell, so that
// is where they must be found. Only PATH is taken: a CLI name may be an
// alias or function in the shell, which says nothing about the file. Run
// starts this in the background so the menu does not wait for it.
var shellPath = sync.OnceValue(probeShellPath)

func probeShellPath() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-lic", `printf '\nGOAGENTIC_PATH=%s\n' "$PATH"`)
	// No controlling terminal: an interactive shell must not touch the
	// dashboard's.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	out, _ := cmd.Output()
	return parseShellPath(out)
}

// parseShellPath finds the PATH line, skipping anything else the shell's
// startup files print.
func parseShellPath(out []byte) string {
	path := ""
	for _, line := range strings.Split(string(out), "\n") {
		if rest, ok := strings.CutPrefix(line, "GOAGENTIC_PATH="); ok {
			path = strings.TrimSpace(rest)
		}
	}
	return path
}

// HarnessAvailable says whether an agent of this workspace can open in the
// harness, and if not, why: the CLI must be installed and the workspace
// must have the framework commands for it.
func HarnessAvailable(ws *Workspace, harness string) (ok bool, reason string) {
	if _, err := lookPath(executable(harness)); err != nil {
		return false, executable(harness) + " not installed"
	}
	files, needs := commandFiles[harness]
	if !needs {
		return true, ""
	}
	home, _ := os.UserHomeDir()
	for _, path := range []string{filepath.Join(ws.Root, files[0]), filepath.Join(home, files[1])} {
		if _, err := os.Stat(path); err == nil {
			return true, ""
		}
	}
	return false, "no framework commands here: run harness/install.sh " + harness
}

// modelOf is the model an agent runs on: its own context.md model:, else the
// workspace's, else none (the CLI's default).
func modelOf(ws *Workspace, a *Agent) string {
	if a.Model != "" {
		return a.Model
	}
	return ws.Model
}

// LaunchStep is one Herdr CLI call a launch makes.
type LaunchStep struct {
	Args []string
	// NewPane marks the tab-creation call whose root pane the next step
	// starts the agent in.
	NewPane bool
	// Fallback runs when Args fails: typing the start command into the pane
	// works even where Herdr will not hand the pane to `agent start` (for
	// example a pane that still holds the agent's name after its CLI exited).
	Fallback []string
}

// LaunchPlan says how Enter opens an agent, without doing it: focus a pane
// where the agent is already running, start it again in its leftover shell
// pane, or start it in a new tab of the current Space.
func LaunchPlan(ws *Workspace, a *Agent, herdrWorkspace string) (summary string, steps []LaunchStep) {
	return LaunchPlanWith(ws, a, harnessOf(ws, a), herdrWorkspace)
}

// LaunchPlanWith is LaunchPlan in a harness picked for this launch. The
// configured model goes along only in the configured harness: a model name
// for one CLI means nothing to another.
func LaunchPlanWith(ws *Workspace, a *Agent, harness, herdrWorkspace string) (summary string, steps []LaunchStep) {
	name := strings.ToLower(a.Name)
	var agentArgs []string
	if m := modelOf(ws, a); m != "" && harness == harnessOf(ws, a) {
		agentArgs = []string{"--model", m}
	}
	agentArgs = append(agentArgs, startCommand(harness, name))
	start := func(pane string) []string {
		return append([]string{"agent", "start", name, "--kind", harness, "--pane", pane, "--"}, agentArgs...)
	}
	var typed []string
	for _, arg := range agentArgs {
		typed = append(typed, shellQuote(arg))
	}
	switch {
	case a.Live != nil && a.Live.Running:
		return "switched to " + a.Name, []LaunchStep{
			{Args: []string{"agent", "focus", a.Live.PaneID}},
		}
	case a.Live != nil:
		return "started " + a.Name + " (" + harness + ") in its pane", []LaunchStep{
			{Args: start(a.Live.PaneID), Fallback: []string{"pane", "run", a.Live.PaneID,
				executable(harness) + " " + strings.Join(typed, " ")}},
			{Args: []string{"agent", "focus", a.Live.PaneID}},
		}
	default:
		tab := []string{"tab", "create", "--cwd", ws.Root, "--label", a.Name, "--focus"}
		if herdrWorkspace != "" {
			tab = append(tab, "--workspace", herdrWorkspace)
		}
		return "started " + a.Name + " (" + harness + ") in a new tab", []LaunchStep{
			{Args: tab, NewPane: true},
			{Args: start("")},
		}
	}
}

// Launch opens the agent in Herdr (see LaunchPlan) and returns a one-line
// result for the status bar.
func Launch(ws *Workspace, a *Agent) (string, error) {
	return LaunchWith(ws, a, harnessOf(ws, a))
}

// LaunchWith is Launch in a harness picked for this launch.
func LaunchWith(ws *Workspace, a *Agent, harness string) (string, error) {
	if os.Getenv("HERDR_ENV") != "1" && os.Getenv("HERDR_BIN_PATH") == "" {
		return "", ErrNoHerdr
	}
	bin := herdrBin()
	summary, steps := LaunchPlanWith(ws, a, harness, os.Getenv("HERDR_WORKSPACE_ID"))
	newPane := ""
	for _, step := range steps {
		args := step.Args
		if newPane != "" {
			for i, arg := range args {
				if arg == "--pane" && i+1 < len(args) && args[i+1] == "" {
					args[i+1] = newPane
				}
			}
		}
		out, err := exec.Command(bin, args...).CombinedOutput()
		if err != nil && step.Fallback != nil {
			args = step.Fallback
			out, err = exec.Command(bin, args...).CombinedOutput()
		}
		if err != nil {
			return "", fmt.Errorf("herdr %s %s: %s", args[0], args[1], herdrError(out, err))
		}
		if step.NewPane {
			if newPane = rootPane(out); newPane == "" {
				return "", fmt.Errorf("herdr tab create: no pane id in reply")
			}
		}
	}
	return summary, nil
}

func rootPane(out []byte) string {
	var resp struct {
		Result struct {
			RootPane struct {
				PaneID string `json:"pane_id"`
			} `json:"root_pane"`
		} `json:"result"`
	}
	if json.Unmarshal(out, &resp) != nil {
		return ""
	}
	return resp.Result.RootPane.PaneID
}

// herdrError pulls the message out of a Herdr error reply.
func herdrError(out []byte, err error) string {
	var resp struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(out, &resp) == nil && resp.Error.Message != "" {
		return resp.Error.Message
	}
	if s := strings.TrimSpace(string(out)); s != "" {
		return s
	}
	return err.Error()
}

// shellQuote single-quotes s for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
