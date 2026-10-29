package dash

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// T3 Code runs agents as threads instead of Herdr panes. The dashboard reads
// them through the `t3` CLI (`t3 thread list|show|send|start`) and treats a
// thread as a pane whose id is "t3:<thread id>", so the rest of the page
// (live state, typing a line, Wrap, Start) works the same. The live rule is
// the one in template/agents/reference/t3.md.

// t3PanePrefix marks a LivePane that is a T3 thread.
const t3PanePrefix = "t3:"

// ErrNoT3 means the t3 CLI could not be found.
var ErrNoT3 = errors.New("the t3 CLI was not found")

// IsT3Pane reports whether a pane id names a T3 thread.
func IsT3Pane(pane string) bool { return strings.HasPrefix(pane, t3PanePrefix) }

func t3Thread(pane string) string { return strings.TrimPrefix(pane, t3PanePrefix) }

// t3Bin is the t3 CLI: T3CODE_CLI_PATH inside a T3 session, else t3 on PATH
// or the login-shell PATH (a dashboard run from SSH or a service has no T3
// variables).
func t3Bin() (string, error) {
	if bin := os.Getenv("T3CODE_CLI_PATH"); bin != "" {
		return bin, nil
	}
	if bin, err := lookPath("t3"); err == nil {
		return bin, nil
	}
	return "", ErrNoT3
}

type t3ThreadInfo struct {
	ThreadID string `json:"threadId"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Settled  bool   `json:"settled"`
	Provider string `json:"providerInstanceId"`
}

// T3Threads lists the live T3 threads of the project at root as panes. It
// returns an error when t3 is missing or root is not a T3 project.
func T3Threads(root string) ([]LivePane, error) {
	bin, err := t3Bin()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "thread", "list", "--project", root, "--json").Output()
	if err != nil {
		return nil, fmt.Errorf("t3 thread list: %w", err)
	}
	return parseT3Threads(out, root)
}

// parseT3Threads keeps the threads that count as live sessions: peer and
// task threads while running, any other "<Name> - …" thread while unsettled.
// Threads come newest first, so the newest session of an agent wins.
func parseT3Threads(data []byte, root string) ([]LivePane, error) {
	var threads []t3ThreadInfo
	if err := json.Unmarshal(data, &threads); err != nil {
		return nil, err
	}
	var panes []LivePane
	for _, t := range threads {
		name, kind, ok := strings.Cut(t.Title, " - ")
		if !ok || strings.TrimSpace(name) == "" {
			continue
		}
		running := t.Status == "running"
		helper := strings.EqualFold(kind, "peer") || strings.EqualFold(kind, "task")
		if helper && !running || !helper && t.Settled {
			continue
		}
		status := "idle"
		if running {
			status = "working"
		}
		agentName := strings.ToLower(strings.TrimSpace(name))
		if helper {
			agentName += "-" + strings.ToLower(kind)
		}
		panes = append(panes, LivePane{
			Name:    agentName,
			PaneID:  t3PanePrefix + t.ThreadID,
			CWD:     root,
			Agent:   t3Harness(t.Provider),
			Status:  status,
			Label:   t.Title,
			Running: true,
		})
	}
	return panes, nil
}

// t3Harness maps a T3 provider instance ("claudeAgent", "codex") to the
// harness name the framework commands are spelt for.
func t3Harness(provider string) string {
	p := strings.ToLower(provider)
	for _, h := range []string{"claude", "codex", "gemini", "opencode", "cursor", "pi"} {
		if strings.HasPrefix(p, h) {
			return h
		}
	}
	return "claude"
}

// t3Prompt sends text to a thread; a busy thread takes it as its next turn.
func t3Prompt(pane, text string) error {
	bin, err := t3Bin()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "thread", "send", t3Thread(pane), text).CombinedOutput()
	if err != nil {
		return fmt.Errorf("t3 thread send: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// t3Read is the thread's latest messages, for the activity line.
func t3Read(pane string) (string, error) {
	bin, err := t3Bin()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "thread", "show", t3Thread(pane), "--messages", "2").Output()
	return string(out), err
}

// t3Close cannot settle a thread: the t3 CLI has no command for it yet, so
// the principal settles it in T3.
func t3Close(pane string) error {
	return errors.New("settle its thread in T3 Code to close it; the t3 CLI cannot settle threads yet")
}

// t3Launch starts the agent in a new T3 thread of the workspace's project,
// titled the way the router titles a human session.
func t3Launch(ws *Workspace, a *Agent) (string, error) {
	bin, err := t3Bin()
	if err != nil {
		return "", err
	}
	title := a.Name
	if a.Title != "" {
		title += " - " + a.Title
	}
	cmd := frameworkCommand("claude", "start") + " " + a.Name
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "thread", "start", "--title", title, ws.Root, cmd).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("t3 thread start: %s", strings.TrimSpace(string(out)))
	}
	return a.Name + " started in T3 Code", nil
}
