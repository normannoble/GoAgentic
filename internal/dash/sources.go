package dash

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// LivePane is one Herdr pane tagged with an agent name.
type LivePane struct {
	Name   string `json:"name"`
	PaneID string `json:"pane_id"`
	CWD    string `json:"cwd"`
	// Agent is the detected CLI (claude, codex, …); empty when the pane is
	// still named for the agent but its CLI has exited.
	Agent  string `json:"agent"`
	Status string `json:"agent_status"`
	// Running is true when a coding-agent CLI is live in the pane.
	Running bool `json:"-"`
}

// State is the short Live cell: "working", "idle", "blocked", or "shell"
// when the pane is open but the CLI has exited.
func (p *LivePane) State() string {
	if !p.Running {
		return "shell"
	}
	if p.Status == "" || p.Status == "unknown" {
		return "running"
	}
	return p.Status
}

// HerdrPanes runs `herdr agent list`, using HERDR_BIN_PATH when a Herdr
// plugin launched us. It returns an error outside Herdr.
func HerdrPanes() ([]LivePane, error) {
	bin := herdrBin()
	if _, err := exec.LookPath(bin); err != nil {
		return nil, err
	}
	return parseHerdrPanes(bin)
}

func parseHerdrAgents(data []byte) ([]LivePane, error) {
	var resp struct {
		Result struct {
			Agents []LivePane `json:"agents"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	panes := resp.Result.Agents
	for i := range panes {
		panes[i].Running = panes[i].Agent != ""
	}
	return panes, nil
}

// attachLive matches panes to agents: same workspace root, and pane name
// equal to the agent name lowercased (or with a -2 style suffix). A running
// pane wins over a leftover shell pane.
func attachLive(ws *Workspace, panes []LivePane) {
	root := filepath.Clean(ws.Root)
	for _, a := range ws.Agents {
		name := strings.ToLower(a.Name)
		for i := range panes {
			p := &panes[i]
			cwd := filepath.Clean(p.CWD)
			if cwd != root && !strings.HasPrefix(cwd, root+string(filepath.Separator)) {
				continue
			}
			base := strings.ToLower(p.Name)
			if base != name && !regexp.MustCompile(`^`+regexp.QuoteMeta(name)+`-\d+$`).MatchString(base) {
				continue
			}
			if a.Live == nil || (p.Running && !a.Live.Running) {
				a.Live = p
			}
		}
	}
}

// Start is when an agent was last started, from Claude Code transcripts.
type Start struct {
	Started time.Time
	// Active is the transcript's last write: roughly when the session last
	// did anything.
	Active time.Time
	// All is every start seen, for the activity sparkline.
	All []time.Time
}

// TranscriptCache remembers what each transcript file said so a refresh only
// re-reads files that changed.
type TranscriptCache struct {
	mu    sync.Mutex
	files map[string]transcriptEntry
}

type transcriptEntry struct {
	size   int64
	mtime  time.Time
	starts []namedStart
}

type namedStart struct {
	name string
	at   time.Time
}

// NewTranscriptCache returns an empty cache.
func NewTranscriptCache() *TranscriptCache {
	return &TranscriptCache{files: map[string]transcriptEntry{}}
}

var (
	startCmdRe   = regexp.MustCompile(`<command-name>/?agents:start</command-name>`)
	startArgsRe  = regexp.MustCompile(`<command-args>([^<]*)</command-args>`)
	timestampRe  = regexp.MustCompile(`"timestamp":"([^"]+)"`)
	maxHeadBytes = int64(512 * 1024)
)

// Starts scans the Claude Code project directory for /agents:start commands
// and returns the latest start per agent (lowercased name). Peer sessions are
// not counted: they are another agent's request, not the principal's.
func (c *TranscriptCache) Starts(projectDir string) map[string]Start {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := map[string]Start{}
	matches, _ := filepath.Glob(filepath.Join(projectDir, "*.jsonl"))
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		e, ok := c.files[path]
		if !ok || e.size != info.Size() || !e.mtime.Equal(info.ModTime()) {
			// Starts sit near the top of a transcript; once seen they never
			// change, so a grown file only needs its mtime refreshed.
			if ok && info.Size() > e.size && e.size >= maxHeadBytes {
				e.size, e.mtime = info.Size(), info.ModTime()
			} else {
				e = transcriptEntry{size: info.Size(), mtime: info.ModTime(), starts: scanStarts(path)}
			}
			c.files[path] = e
		}
		for _, s := range e.starts {
			cur := out[s.name]
			cur.All = append(cur.All, s.at)
			if s.at.After(cur.Started) {
				cur.Started, cur.Active = s.at, e.mtime
			}
			out[s.name] = cur
		}
	}
	return out
}

func scanStarts(path string) []namedStart {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []namedStart
	sc := bufio.NewScanner(io.LimitReader(f, maxHeadBytes))
	sc.Buffer(make([]byte, 256*1024), int(maxHeadBytes))
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.Contains(line, []byte("agents:start")) || !startCmdRe.Match(line) {
			continue
		}
		m := startArgsRe.FindSubmatch(line)
		if m == nil {
			continue
		}
		args := strings.Fields(string(m[1]))
		if len(args) == 0 || (len(args) > 1 && strings.EqualFold(args[1], "peer")) {
			continue
		}
		ts := timestampRe.FindSubmatch(line)
		if ts == nil {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, string(ts[1]))
		if err != nil {
			continue
		}
		out = append(out, namedStart{name: strings.ToLower(args[0]), at: at})
	}
	return out
}

// projectDirName is Claude Code's directory name for a working directory:
// every character outside [A-Za-z0-9] becomes "-".
func projectDirName(root string) string {
	return regexp.MustCompile(`[^A-Za-z0-9]`).ReplaceAllString(root, "-")
}

// DefaultTranscripts is ~/.claude/projects, or "" when it does not exist.
func DefaultTranscripts() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, ".claude", "projects")
	if !dirExists(dir) {
		return ""
	}
	return dir
}

// dirtyPaths are uncommitted paths relative to the workspace root.
type dirtyPaths []string

func (d dirtyPaths) under(rel string) int {
	prefix := filepath.ToSlash(rel) + "/"
	n := 0
	for _, p := range d {
		if strings.HasPrefix(p, prefix) {
			n++
		}
	}
	return n
}

// uncommittedByPath lists modified and untracked files, or nothing when the
// workspace is not a git repository.
func uncommittedByPath(root string) dirtyPaths {
	cmd := exec.Command("git", "-C", root, "status", "--porcelain", "--untracked-files=all", "--", "agents", "Agents")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var paths dirtyPaths
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 4 {
			continue
		}
		p := strings.TrimSpace(line[3:])
		if _, after, ok := strings.Cut(p, " -> "); ok {
			p = after
		}
		paths = append(paths, strings.Trim(p, `"`))
	}
	return paths
}
