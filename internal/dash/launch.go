package dash

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
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

// harnessOf is the CLI an agent runs under: its own context.md harness:,
// else the workspace's, else claude.
func harnessOf(ws *Workspace, a *Agent) string {
	for _, h := range []string{a.Harness, ws.Harness} {
		switch h {
		case "claude", "codex", "gemini", "opencode", "pi", "cursor":
			return h
		}
	}
	return "claude"
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
	name := strings.ToLower(a.Name)
	harness := harnessOf(ws, a)
	var agentArgs []string
	if m := modelOf(ws, a); m != "" {
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
		return "started " + a.Name + " in its pane", []LaunchStep{
			{Args: start(a.Live.PaneID), Fallback: []string{"pane", "run", a.Live.PaneID,
				executable(harness) + " " + strings.Join(typed, " ")}},
			{Args: []string{"agent", "focus", a.Live.PaneID}},
		}
	default:
		tab := []string{"tab", "create", "--cwd", ws.Root, "--label", a.Name, "--focus"}
		if herdrWorkspace != "" {
			tab = append(tab, "--workspace", herdrWorkspace)
		}
		return "started " + a.Name + " in a new tab", []LaunchStep{
			{Args: tab, NewPane: true},
			{Args: start("")},
		}
	}
}

// Launch opens the agent in Herdr (see LaunchPlan) and returns a one-line
// result for the status bar.
func Launch(ws *Workspace, a *Agent) (string, error) {
	if os.Getenv("HERDR_ENV") != "1" && os.Getenv("HERDR_BIN_PATH") == "" {
		return "", ErrNoHerdr
	}
	bin := herdrBin()
	summary, steps := LaunchPlan(ws, a, os.Getenv("HERDR_WORKSPACE_ID"))
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
