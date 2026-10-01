package dash

import (
	"fmt"
	"time"
)

// Limits from the conventions (§ Consolidation trigger, § actions.md).
const (
	maxStanding        = 5
	maxSessions        = 10
	maxStandingBytes   = 15 * 1024
	maxActionsBytes    = 20 * 1024
	maxLastReviewedLen = 600
	staleTracker       = 14 * 24 * time.Hour
	maxPeerOpen        = 10
)

// Level orders health from good to bad.
type Level int

const (
	OK Level = iota
	Warn
	Fail
)

// Health is the cheap, file-only subset of /agents:doctor.
type Health struct {
	Level    Level
	Findings []string
}

func (h *Health) add(level Level, format string, args ...any) {
	if level > h.Level {
		h.Level = level
	}
	h.Findings = append(h.Findings, fmt.Sprintf(format, args...))
}

func assessHealth(a *Agent, now time.Time) Health {
	var h Health
	if len(a.Missing) > 0 {
		h.add(Fail, "missing %v", a.Missing)
	}
	if n := len(a.Standing); n > maxStanding {
		h.add(Warn, "standing memory has %d files (limit %d)", n, maxStanding)
	}
	for _, f := range a.Standing {
		if f.Size > maxStandingBytes {
			h.add(Warn, "standing file %s is %d KB (limit 15)", f.Name, f.Size/1024)
		}
	}
	if n := len(a.Sessions); n > maxSessions {
		h.add(Warn, "%d session logs (limit %d)", n, maxSessions)
	}
	if a.ActionsSize > maxActionsBytes {
		h.add(Warn, "actions.md is %d KB (limit 20)", a.ActionsSize/1024)
	}
	if a.LastReviewedLen > maxLastReviewedLen {
		h.add(Warn, "Last reviewed line is %d chars (keep it to the date)", a.LastReviewedLen)
	}
	if !a.Retired {
		if a.LastReviewed.IsZero() {
			h.add(Warn, "no Last reviewed date")
		} else if age := now.Sub(a.LastReviewed); age > staleTracker {
			h.add(Warn, "tracker last reviewed %s ago", humanAge(age))
		}
	}
	if a.PeerOpen > maxPeerOpen {
		h.add(Warn, "%d peer files still open or answered (tracker cleanup archives them)", a.PeerOpen)
	}
	// A live agent mid-session is expected to have uncommitted files.
	if a.Uncommitted > 0 && (a.Live == nil || !a.Live.Running) {
		if a.Unwrapped {
			h.add(Warn, "a session ended without a wrap (tracker changed after the last session log) — start it and wrap")
		} else {
			h.add(Warn, "%d uncommitted file(s) with no session running", a.Uncommitted)
		}
	}
	return h
}

// Symbol renders the level for a table cell.
func (l Level) Symbol() string {
	switch l {
	case Fail:
		return "✗"
	case Warn:
		return "!"
	default:
		return "✓"
	}
}

// humanAge renders a duration as a short age: 5m, 3h, 2d, 6w.
func humanAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dw", int(d.Hours()/24/7))
	}
}
