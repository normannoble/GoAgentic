// Package dash builds a read-only status snapshot of one agent workspace from
// its files alone, so a principal can see what every agent wants to do next
// without starting a coding-agent session.
package dash

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Workspace is one agent workspace: the directory that holds agents/.
type Workspace struct {
	Root      string
	Name      string
	Principal string
	// Harness is the workspace's default CLI (conventions frontmatter).
	Harness string
	Agents  []*Agent
	// Live is false when Herdr could not be queried, so the Live column is
	// unknown rather than empty.
	Live    bool
	Scanned time.Time
}

// ErrNoWorkspace means no agents/CONVENTIONS.md was found at or above the
// starting directory.
var ErrNoWorkspace = errors.New("no agent workspace found (looked for agents/CONVENTIONS.md in this directory and its parents)")

// FindRoot walks up from dir to the nearest directory holding
// agents/CONVENTIONS.md (either case of the agents directory).
func FindRoot(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		for _, sub := range []string{"agents", "Agents"} {
			if fileExists(filepath.Join(dir, sub, "CONVENTIONS.md")) {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ErrNoWorkspace
		}
		dir = parent
	}
}

// Options control a scan.
type Options struct {
	// IncludeRetired keeps agents whose context.md says status: retired.
	IncludeRetired bool
	// Now is the reference time for ages; zero means time.Now().
	Now time.Time
	// Herdr lists live agent panes; nil skips the Live column.
	Herdr func() ([]LivePane, error)
	// Transcripts is the Claude Code projects directory
	// (~/.claude/projects); empty skips "last started".
	Transcripts string
	// Cache carries transcript results between refreshes; may be nil.
	Cache *TranscriptCache
}

// Scan reads the whole workspace at root.
func Scan(root string, opts Options) (*Workspace, error) {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	agentsDir := filepath.Join(root, "agents")
	if !dirExists(agentsDir) {
		agentsDir = filepath.Join(root, "Agents")
	}
	conv := readFrontmatter(filepath.Join(agentsDir, "CONVENTIONS.md"))
	ws := &Workspace{
		Root:      root,
		Name:      filepath.Base(root),
		Principal: conv["principal"],
		Harness:   conv["harness"],
		Scanned:   opts.Now,
	}

	// Single-domain agents/<name>/ and multi-domain agents/<scope>/<name>/.
	var dirs []string
	for _, pattern := range []string{"*/context.md", "*/*/context.md"} {
		matches, _ := filepath.Glob(filepath.Join(agentsDir, pattern))
		for _, m := range matches {
			dirs = append(dirs, filepath.Dir(m))
		}
	}
	sort.Strings(dirs)

	dirty := uncommittedByPath(root)
	for _, dir := range dirs {
		a := loadAgent(dir, ws.Principal, opts.Now)
		if a.Retired && !opts.IncludeRetired {
			continue
		}
		rel, _ := filepath.Rel(root, dir)
		a.Uncommitted = dirty.under(rel)
		a.Unwrapped = unwrapped(dir, dirty.has(filepath.Join(rel, "actions.md")))
		ws.Agents = append(ws.Agents, a)
	}

	if opts.Herdr != nil {
		if panes, err := opts.Herdr(); err == nil {
			ws.Live = true
			attachLive(ws, panes)
		}
	}
	if opts.Transcripts != "" {
		cache := opts.Cache
		if cache == nil {
			cache = NewTranscriptCache()
		}
		starts := cache.Starts(filepath.Join(opts.Transcripts, projectDirName(root)))
		for _, a := range ws.Agents {
			if s, ok := starts[strings.ToLower(a.Name)]; ok {
				a.LastStarted = s.Started
				a.LastActive = s.Active
				a.starts = s.All
			}
		}
	}
	for _, a := range ws.Agents {
		a.Health = assessHealth(a, opts.Now)
		a.Activity = activity(a, opts.Now)
	}
	return ws, nil
}

// readFrontmatter returns the key: value pairs of a leading --- block.
// Comments after " #" are dropped and quotes trimmed.
func readFrontmatter(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "---" {
		return out
	}
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "---" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.HasPrefix(line, " ") {
			continue
		}
		if i := strings.Index(value, " #"); i >= 0 {
			value = value[:i]
		}
		out[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return out
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
