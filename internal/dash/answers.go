package dash

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Answers: the principal answers an item that waits on them from the web
// dashboard, and the answer lands in the agent's scheduled-run inbox as an
// "answer #N" entry. The agent drains the inbox at its next start (startup
// step 16) and applies the answer itself; the dashboard never edits a
// tracker. Until then the item counts as answered, not as waiting on the
// principal.

// Answer is one pending answer to an item.
type Answer struct {
	At   time.Time
	Text string
}

// answerHeadRe matches an answer entry heading:
// "## 2026-10-25 12:30 — answer #50 (Norman, via dash) — UNPROCESSED".
var answerHeadRe = regexp.MustCompile(`^#{2,3}\s+(.+?)\s+—\s+answer #([\w.-]+)\b[^—]*—\s*UNPROCESSED\s*$`)

const answerTime = "2006-01-02 15:04"

// isAnswerHeading tells answer entries from tick entries, so unread runs are
// counted without them.
func isAnswerHeading(line string) bool {
	parts := strings.Split(line, "—")
	for _, p := range parts[1:] {
		if strings.HasPrefix(strings.TrimSpace(p), "answer #") {
			return true
		}
	}
	return false
}

// parseAnswers returns the unprocessed answers in an inbox, by item id
// (without the #). The body is the entry's quoted lines.
func parseAnswers(inbox string) map[string][]Answer {
	out := map[string][]Answer{}
	var id string
	var cur *Answer
	flush := func() {
		if cur != nil {
			cur.Text = strings.TrimSpace(cur.Text)
			out[id] = append(out[id], *cur)
		}
		cur = nil
	}
	for _, line := range strings.Split(inbox, "\n") {
		if strings.HasPrefix(line, "#") {
			flush()
			if m := answerHeadRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
				at, _ := time.ParseInLocation(answerTime, m[1], time.Local)
				id, cur = m[2], &Answer{At: at}
			}
			continue
		}
		if cur != nil {
			text, quoted := strings.CutPrefix(line, ">")
			if quoted {
				cur.Text += strings.TrimPrefix(text, " ") + "\n"
			}
		}
	}
	flush()
	if len(out) == 0 {
		return nil
	}
	return out
}

// Answered reports whether the item has an answer the agent has not yet
// picked up.
func (a *Agent) Answered(it Item) bool {
	return len(a.Answers[strings.TrimPrefix(it.ID, "#")]) > 0
}

// AnsweredItems lists the rows that wait on the principal but have been
// answered from the dashboard: now waiting on the agent to pick them up.
func (a *Agent) AnsweredItems(principal string) []Item {
	var out []Item
	for _, it := range a.Open {
		if waitsOnPrincipal(it, principal) && a.Answered(it) {
			out = append(out, it)
		}
	}
	return out
}

var answerIDRe = regexp.MustCompile(`^[\w.-]+$`)

// maxAnswer bounds one answer; anything longer belongs in a session.
const maxAnswer = 4000

// AppendAnswer adds the principal's answer to item id to the agent's inbox,
// creating the inbox if the agent has none. Every body line is quoted, so an
// answer can never start a heading of its own.
func AppendAnswer(a *Agent, principal, id, text string, now time.Time) error {
	return appendAnswer(a, principal, id, text, now, false)
}

// appendAnswer writes the entry; inChat says the answer also went to the
// agent's live chat, so an agent that applied it there only marks it done.
func appendAnswer(a *Agent, principal, id, text string, now time.Time, inChat bool) error {
	id = strings.TrimPrefix(strings.TrimSpace(id), "#")
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r", ""))
	if !answerIDRe.MatchString(id) {
		return errors.New("bad item id")
	}
	if text == "" {
		return errors.New("the answer is empty")
	}
	if len(text) > maxAnswer {
		return fmt.Errorf("the answer is over %d characters: say it in a session instead", maxAnswer)
	}
	var item *Item
	for i := range a.Open {
		if strings.TrimPrefix(a.Open[i].ID, "#") == id {
			item = &a.Open[i]
		}
	}
	if item == nil {
		return fmt.Errorf("#%s is not an open item of %s", id, a.Name)
	}

	var b strings.Builder
	via := "via dash"
	if inChat {
		via = "via dash, sent to chat"
	}
	fmt.Fprintf(&b, "\n## %s — answer #%s (%s, %s) — UNPROCESSED\n\n", now.Format(answerTime), id, firstName(principal), via)
	if inChat {
		fmt.Fprintf(&b, "%s answered #%s **%s** from the web dashboard, and it was sent to your live chat as \"Re #%s\". If you applied it there, just mark this entry PROCESSED; if not, apply it as their word on that item, then mark it PROCESSED.\n\n",
			firstName(principal), id, item.Headline(), id)
	} else {
		fmt.Fprintf(&b, "%s answered #%s **%s** from the web dashboard. Apply it as their word on that item: update the row, act on it within your autonomy, then mark this entry PROCESSED.\n\n",
			firstName(principal), id, item.Headline())
	}
	for _, line := range strings.Split(text, "\n") {
		b.WriteString(strings.TrimRight("> "+line, " ") + "\n")
	}

	path := filepath.Join(a.Dir, "memory", "scheduled", "inbox.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	header := ""
	if !fileExists(path) {
		header = "# Scheduled-run inbox — " + a.Name + "\n"
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	// One write, so a tick appending at the same moment cannot interleave.
	_, err = f.WriteString(header + b.String())
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}
