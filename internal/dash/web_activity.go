package dash

import (
	"context"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// A working session's own status line, as its harness draws it under the
// conversation: Claude Code's "✻ Percolating… (26s · ↓ 482 tokens)", or
// Codex's "• Working (12s • esc to interrupt)". The page shows it in place
// of a bare "working".

// activity is the status line's word and what follows it: time, tokens.
type activity struct {
	Word, Meta string
}

// activityLines is how many of the pane's last lines are searched: the
// status line sits just above the input box and footer.
const activityLines = 30

// activityRe matches a status line: a spinner glyph, a capitalised word or
// two (with Claude Code's ellipsis), and an optional bracket of details.
var activityRe = regexp.MustCompile(`^\s*[·✢✳✶✻✽*•◦]\s+([A-Z][A-Za-z'’ -]{1,40}?(?:…|\.\.\.)?)\s*(?:\(([^)]*)\))?\s*$`)

// parseActivity finds the last status line in a pane's text, or nil. A
// line counts only if it has the ellipsis or a bracket starting with a
// time, so a bulleted "• Done" in the chat does not.
func parseActivity(text string) *activity {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-activityLines; i-- {
		m := activityRe.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		word, meta := strings.TrimSpace(m[1]), m[2]
		timed := meta != "" && meta[0] >= '0' && meta[0] <= '9'
		if !strings.HasSuffix(word, "…") && !strings.HasSuffix(word, "...") && !timed {
			continue
		}
		var keep []string
		for _, part := range regexp.MustCompile(`\s+[·•]\s+`).Split(meta, -1) {
			part = strings.TrimSpace(part)
			if part == "" || strings.Contains(part, "esc to") || strings.Contains(part, "ctrl+") || strings.Contains(part, "to interrupt") {
				continue
			}
			keep = append(keep, part)
		}
		return &activity{Word: word, Meta: strings.Join(keep, " · ")}
	}
	return nil
}

// activityOf reads a working session's status line from its pane, or nil
// when it is not working or the line cannot be found.
func (s *webServer) activityOf(a *Agent) *activity {
	if a.Live == nil || !a.Live.Running || a.Live.Status != "working" || a.Live.PaneID == "" {
		return nil
	}
	read := s.src.ReadPane
	if read == nil {
		read = liveRead
	}
	text, err := read(a.Live.PaneID)
	if err != nil {
		return nil
	}
	return parseActivity(text)
}

// liveRead is a Herdr pane's last lines or a T3 thread's latest messages.
func liveRead(pane string) (string, error) {
	if IsT3Pane(pane) {
		return t3Read(pane)
	}
	return herdrRead(pane)
}

// herdrRead is the pane's last lines, from `herdr pane read`.
func herdrRead(pane string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, herdrBin(), "pane", "read", pane, "--lines", "40").Output()
	return string(out), err
}
