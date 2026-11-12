package dash

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Tracker edits from the Tasks view. The dashboard otherwise never writes a
// tracker; this is the one exception, and it is deliberately narrow: one
// row of an Open table, found by its # cell, and only the Status, Priority,
// Due/Target and Owner cells of it. Every other byte of the file, the other
// cells of the row included, is written back as it was read.

// openRow is one data row of a table under "## Open", located in the file.
type openRow struct {
	// Line is the row's index in the file's lines (split on "\n").
	Line int
	// Cols maps header names to cell indexes, as headerColumns makes them;
	// Width is the header's cell count.
	Cols  map[string]int
	Width int
	// Cells are the row's cells as splitRow reads them: trimmed, escaped
	// pipes turned back into pipes.
	Cells []string
	// Section is the ### heading the row sits under.
	Section string
}

func (r openRow) get(name string) string { return cell(r.Cells, r.Cols, name) }

// dueColumn is the name of the row's due column, "" if the table has none.
func (r openRow) dueColumn() string {
	for _, name := range []string{"due/target", "due"} {
		if _, ok := r.Cols[name]; ok {
			return name
		}
	}
	return ""
}

// openRows lists the data rows of the Open tables, walking the file the way
// parseActions does, so a row the dashboard shows is a row found here.
// Separator, header and placeholder rows are not data rows.
func openRows(text string) []openRow {
	var out []openRow
	inOpen := false
	section := ""
	var cols map[string]int
	width := 0
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "## "):
			inOpen = strings.HasPrefix(strings.ToLower(strings.TrimSpace(line[3:])), "open")
			section, cols = "", nil
			continue
		case strings.HasPrefix(line, "### "):
			section, cols = strings.TrimSpace(line[4:]), nil
			continue
		}
		if !inOpen || !strings.HasPrefix(line, "|") {
			if line != "" && !strings.HasPrefix(line, "|") {
				cols = nil
			}
			continue
		}
		if separatorRe.MatchString(line) {
			continue
		}
		cells := splitRow(line)
		if cols == nil {
			cols = headerColumns(cells)
			width = len(cells)
			if _, ok := cols["action"]; !ok {
				cols = nil
			}
			continue
		}
		out = append(out, openRow{Line: i, Cols: cols, Width: width, Cells: cells, Section: section})
	}
	return out
}

// findOpenRow finds the one Open row whose # cell is id ("12" or "#12").
func findOpenRow(text, id string) (openRow, error) {
	id = strings.TrimPrefix(strings.TrimSpace(id), "#")
	if id == "" {
		return openRow{}, errors.New("no item id")
	}
	var found []openRow
	for _, r := range openRows(text) {
		if strings.TrimPrefix(r.get("#"), "#") == id {
			found = append(found, r)
		}
	}
	switch len(found) {
	case 0:
		return openRow{}, fmt.Errorf("#%s is not a row of the open tables", id)
	case 1:
		return found[0], nil
	}
	return openRow{}, fmt.Errorf("#%s is in the open tables %d times; fix the tracker by hand", id, len(found))
}

// RowEdit is a change to one tracker row. A nil field is left as it is.
type RowEdit struct {
	ID                           string
	Status, Priority, Due, Owner *string
}

// cellSpans returns the byte span of each cell's content in a table row:
// the text between two unescaped pipes. The row must open and close with a
// pipe (surrounding whitespace aside).
func cellSpans(line string) ([][2]int, bool) {
	start := len(line) - len(strings.TrimLeft(line, " \t"))
	end := len(strings.TrimRight(line, " \t\r"))
	if end-start < 2 || line[start] != '|' || line[end-1] != '|' || (end >= 2 && line[end-2] == '\\') {
		return nil, false
	}
	var spans [][2]int
	from := start + 1
	for i := start + 1; i < end; i++ {
		if line[i] == '|' && line[i-1] != '\\' {
			spans = append(spans, [2]int{from, i})
			from = i + 1
		}
	}
	return spans, true
}

// cellText makes a value safe for one table cell: one line, pipes escaped.
func cellText(v string) string {
	v = strings.Join(strings.Fields(strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(v)), " ")
	v = strings.ReplaceAll(v, `\|`, "|")
	return strings.ReplaceAll(v, "|", `\|`)
}

// EditTrackerRow applies e to the tracker text and returns the new text.
// It refuses when the row is not in the open tables exactly once, when the
// row's cell count is not its header's, or when a field to change has no
// column. Only the changed cells' bytes differ in the result.
func EditTrackerRow(text string, e RowEdit) (string, error) {
	row, err := findOpenRow(text, e.ID)
	if err != nil {
		return "", err
	}
	lines := strings.Split(text, "\n")
	line := lines[row.Line]
	spans, ok := cellSpans(line)
	if !ok || len(spans) != row.Width || len(row.Cells) != row.Width {
		return "", fmt.Errorf("#%s has %d cells but its table's header has %d; fix the row by hand", strings.TrimPrefix(e.ID, "#"), len(row.Cells), row.Width)
	}
	type change struct {
		col int
		val string
	}
	var changes []change
	for _, f := range []struct {
		name string
		val  *string
	}{{"status", e.Status}, {"priority", e.Priority}, {row.dueColumn(), e.Due}, {"owner", e.Owner}} {
		if f.val == nil {
			continue
		}
		col, ok := row.Cols[f.name]
		if f.name == "" || !ok {
			return "", fmt.Errorf("the tracker has no column for that field")
		}
		changes = append(changes, change{col, cellText(*f.val)})
	}
	if len(changes) == 0 {
		return text, nil
	}
	// Splice from the right, so earlier spans stay where they are.
	for i := len(spans) - 1; i >= 0; i-- {
		for _, c := range changes {
			if c.col != i {
				continue
			}
			v := " " + c.val + " "
			if c.val == "" {
				v = " "
			}
			line = line[:spans[i][0]] + v + line[spans[i][1]:]
		}
	}
	lines[row.Line] = line
	return strings.Join(lines, "\n"), nil
}

// trackerHash names a tracker's content, so an edit made from a page can
// tell the file changed after the page was drawn.
func trackerHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:12])
}

// writeAtomic replaces path with data through a temporary file in the same
// directory and a rename, keeping the file's permissions.
func writeAtomic(path string, data []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // after a successful rename there is nothing left to remove
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// statusParts splits a raw Status cell into its state, who it waits on
// (for "Waiting on"), and the note after them. A free-text status has no
// state; all of it is the note.
func statusParts(status string) (state, who, note string) {
	status = strings.TrimSpace(status)
	m := stateRe.FindStringSubmatch(status)
	if m == nil {
		return "", "", status
	}
	word := strings.TrimRight(m[1], " \t—–:(-")
	for _, st := range []string{StateNotStarted, StateInProgress, StateWaiting, StateBlocked, StateParked} {
		if strings.EqualFold(word, st) {
			state = st
		}
	}
	rest := status[len(word):]
	if state == StateWaiting {
		rest = strings.TrimSpace(rest)
		who = strings.TrimSpace(whoRe.FindStringSubmatch(rest)[1])
		rest = rest[len(whoRe.FindStringSubmatch(rest)[1]):]
	}
	note = strings.TrimSpace(strings.TrimLeft(rest, " \t—–-:;,"))
	return state, who, note
}

// composeStatus is statusParts' inverse in the tracker's own form:
// "Waiting on Irfan — note", "In progress — note", "Parked".
func composeStatus(state, who, note string) (string, error) {
	note = strings.TrimSpace(note)
	switch state {
	case "":
		return note, nil
	case StateWaiting:
		who = strings.TrimSpace(who)
		if who == "" {
			return "", errors.New("say who it is waiting on")
		}
		state += " " + who
	case StateNotStarted, StateInProgress, StateBlocked, StateParked:
	default:
		return "", fmt.Errorf("%q is not a status", state)
	}
	if note == "" {
		return state, nil
	}
	return state + " — " + note, nil
}

// completedDateRe tells a Completed table that writes dates day first.
var completedDateRe = regexp.MustCompile(`^\d{2}-\d{2}-\d{4}$`)

// MoveToCompleted takes the open row #id out of the Open tables and puts it
// at the top of the Completed table, dated today, its Action cell cut to
// the bold title and the first sentence. The Completed row is built by the
// Completed header's column names (#, Action, Owner, a ticket column, the
// date column); every other byte of the file stays. It refuses when the
// row is not open exactly once, a cell count does not match its header,
// or the tracker has no Completed table.
func MoveToCompleted(text, id string, today time.Time) (string, error) {
	row, err := findOpenRow(text, id)
	if err != nil {
		return "", err
	}
	lines := strings.Split(text, "\n")
	if spans, ok := cellSpans(lines[row.Line]); !ok || len(spans) != row.Width || len(row.Cells) != row.Width {
		return "", fmt.Errorf("#%s has %d cells but its table's header has %d; fix the row by hand", strings.TrimPrefix(id, "#"), len(row.Cells), row.Width)
	}
	// The Completed table: its header and the line after its separator.
	head, sep := -1, -1
	inDone := false
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "## ") {
			inDone = strings.HasPrefix(strings.ToLower(strings.TrimSpace(line[3:])), "completed") || strings.HasPrefix(strings.ToLower(strings.TrimSpace(line[3:])), "done")
			continue
		}
		if !inDone || !strings.HasPrefix(line, "|") {
			continue
		}
		if head < 0 {
			head = i
			continue
		}
		if separatorRe.MatchString(line) {
			sep = i
		}
		break
	}
	if head < 0 || sep != head+1 {
		return "", errors.New("the tracker has no Completed table to move it to")
	}
	hcells := splitRow(lines[head])
	if hs, ok := cellSpans(lines[head]); !ok || len(hs) != len(hcells) {
		return "", errors.New("the Completed table's header is malformed; fix it by hand")
	}
	cols := headerColumns(hcells)

	date := today.Format("2006-01-02")
	if sep+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[sep+1]), "|") {
		first := splitRow(lines[sep+1])
		for _, name := range []string{"completed", "done", "date", "closed"} {
			if i, ok := cols[name]; ok && i < len(first) && completedDateRe.MatchString(first[i]) {
				date = today.Format("02-01-2006")
			}
		}
	}
	cells := make([]string, len(hcells))
	for name, i := range cols {
		switch name {
		case "#":
			cells[i] = row.get("#")
		case "action":
			cells[i] = completedAction(row.get("action"))
		case "completed", "done", "date", "closed":
			cells[i] = date
		default:
			cells[i] = row.get(name)
		}
	}
	var b strings.Builder
	b.WriteString("|")
	for _, c := range cells {
		if c = cellText(c); c == "" {
			b.WriteString(" |")
		} else {
			b.WriteString(" " + c + " |")
		}
	}
	newRow := b.String()
	if got := len(splitRow(newRow)); got != len(hcells) {
		return "", fmt.Errorf("the Completed row would have %d cells, not %d", got, len(hcells))
	}
	// Keep the file's line ending on the new row.
	if strings.HasSuffix(lines[head], "\r") {
		newRow += "\r"
	}
	out := make([]string, 0, len(lines))
	for i, l := range lines {
		if i == row.Line {
			continue
		}
		out = append(out, l)
		if i == sep {
			out = append(out, newRow)
		}
	}
	return strings.Join(out, "\n"), nil
}

// completedAction is an Action cell cut for the Completed table: the bold
// title and the first sentence after it.
func completedAction(action string) string {
	action = strings.TrimSpace(action)
	m := leadBoldRe.FindStringSubmatchIndex(action)
	if m == nil {
		desc, _ := splitDesc(action)
		if desc == "" {
			return action
		}
		return desc
	}
	title, rest := action[:m[1]], strings.TrimSpace(action[m[1]:])
	rest = strings.TrimSpace(strings.TrimLeft(rest, "—–-.: "))
	if rest == "" {
		return title
	}
	first, _ := splitDesc(rest)
	if first == "" {
		if i := strings.Index(rest, ". "); i > 0 {
			first = rest[:i+1]
		} else {
			first = rest
		}
	}
	return title + " — " + first
}
