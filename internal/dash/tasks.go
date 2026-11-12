package dash

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"hash/fnv"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The Tasks view: every agent's open tracker rows across the workspaces,
// shown calmly. The home is an overview, a card per agent; an agent's page
// lists its open items by state; "One by one" walks items a card at a time
// (an agent's, or everything waiting on the principal) to decide, reply,
// change or skip each; flat lists by view stay one tap away. Answers go to
// the agent's inbox (answers.go); edits change one row of the tracker
// (tasks_edit.go).

//go:embed web/tasks.tmpl web/tasks.css
var tasksFiles embed.FS

// tasksState is the Tasks view's share of the web server: Undo's last
// actions (tasks_undo.go) and who is live (tasks_live.go).
type tasksState struct {
	undos     undoStore
	liveCache liveCache
}

// tasksRoutes adds the Tasks view's routes to the dashboard's.
func (s *webServer) tasksRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /w/{ws}/{agent}/avatar", s.avatar)
	mux.HandleFunc("GET /tasks", s.tasks)
	mux.HandleFunc("GET /tasks/one", s.tasksOne)
	mux.HandleFunc("GET /tasks/theme", s.tasksTheme)
	mux.HandleFunc("GET /tasks/search", s.tasksSearch)
	mux.HandleFunc("GET /tasks/{ws}/{agent}", s.agentTasks)
	mux.HandleFunc("POST /tasks/edit", s.tasksEdit)
	mux.HandleFunc("POST /tasks/answer", s.tasksAnswer)
	mux.HandleFunc("POST /tasks/done", s.tasksDone)
	mux.HandleFunc("POST /tasks/undo", s.tasksUndo)
	mux.HandleFunc("GET /img", s.chatImage)
	mux.HandleFunc("GET /tasks.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		// The page names the sheet by its hash, so a new build is fetched.
		w.Header().Set("Cache-Control", "no-cache")
		data, _ := tasksFiles.ReadFile("web/tasks.css")
		_, _ = w.Write(data)
	})
}

var tasksTemplate = template.Must(template.New("tasks.tmpl").Funcs(template.FuncMap{
	"tasksVersion": func() string { return tasksVersion },
	"plural":       plural,
	"initial": func(name string) string {
		if name == "" {
			return "?"
		}
		return strings.ToUpper(name[:1])
	},
	"hue": avatarHue,
	"themeQuery": func(t, back string) string {
		return url.Values{"t": {t}, "back": {back}}.Encode()
	},
	"tile": func(name, avatar, class string, live ...string) map[string]string {
		m := map[string]string{"Name": name, "Avatar": avatar, "Class": class, "Live": "off"}
		if len(live) > 0 {
			m["Live"] = live[0]
			if live[0] == "" {
				m["Live"] = "off"
			}
		}
		return m
	},
	"trimID": func(id string) string { return strings.TrimPrefix(id, "#") },
	"replyArgs": func(tr taskRow, next string) map[string]any {
		return map[string]any{"Row": tr, "Next": next}
	},
	"doArgs": func(e *taskEdit, next string) map[string]any {
		return map[string]any{"Edit": e, "Next": next}
	},
}).ParseFS(tasksFiles, "web/tasks.tmpl"))

// tasksVersion is the Tasks stylesheet's hash, in its link.
var tasksVersion = func() string {
	data, _ := tasksFiles.ReadFile("web/tasks.css")
	h := fnv.New32a()
	_, _ = h.Write(data)
	return strconv.FormatUint(uint64(h.Sum32()), 36)
}()

// taskAgent is one agent of the Tasks view, with its tracker's hash so an
// edit can tell the file has not changed since the page was drawn.
type taskAgent struct {
	ws   *Workspace
	a    *Agent
	hash string
	rows map[string]openRow
	// avatar links the agent's image, "" for none (web_avatar.go).
	avatar string
	// live is its session: working, idle, prompt, or "" when off.
	live string
}

// liveState is the agent's session as the Tasks view shows it, from the
// same live detection as the dash (Herdr panes, T3 threads): "" when not
// running or only as a peer or task pane.
func liveState(a *Agent) string {
	p := a.Live
	if p == nil || !p.Running || !sessionPane(p) {
		return ""
	}
	switch {
	case p.Status == "working":
		return "working"
	case p.Status == "blocked":
		return "prompt"
	}
	return "idle"
}

// taskRoots lists the workspace roots served, or only the one named.
func (s *webServer) taskRoots(only string) []string {
	var roots []string
	if s.src.FleetRoot != "" {
		roots = FleetDirs(s.src.FleetRoot)
	} else {
		roots = []string{s.src.Root}
	}
	if only == "" {
		return roots
	}
	for _, r := range roots {
		if filepath.Base(r) == only {
			return []string{r}
		}
	}
	return nil
}

// loadTasks scans the workspaces served (or only the one named). Unless
// live, it does not ask Herdr, T3 or the transcripts: most of the Tasks
// view needs only the trackers and inboxes. Each tracker is read once, and
// its items are parsed from the same bytes the hash is taken of.
func (s *webServer) loadTasks(only string, live bool) ([]*Workspace, []*taskAgent) {
	opts := s.src.Options(false)
	if !live {
		// Who is live comes from a short cache, so every page can show it
		// without asking Herdr and T3 on each load; transcripts are only
		// read for the chat.
		opts.Herdr, opts.T3 = s.tasksView.liveCache.herdr(opts.Herdr), s.tasksView.liveCache.t3(opts.T3)
		opts.Transcripts, opts.Cache = "", nil
	}
	roots := s.taskRoots(only)
	s.mu.Lock()
	defer s.mu.Unlock()
	var wss []*Workspace
	var agents []*taskAgent
	for _, root := range roots {
		ws, err := Scan(root, opts)
		if err != nil {
			continue
		}
		wss = append(wss, ws)
		for _, a := range s.agentsInOrder(ws) {
			ta := &taskAgent{ws: ws, a: a, rows: map[string]openRow{}, avatar: avatarURL(ws.Name, a.Name, a.Dir), live: liveState(a)}
			if data, err := os.ReadFile(filepath.Join(a.Dir, "actions.md")); err == nil {
				ta.hash = trackerHash(data)
				a.Open = parseActions(string(data)).open
				for _, r := range openRows(string(data)) {
					ta.rows[strings.TrimPrefix(r.get("#"), "#")] = r
				}
			}
			agents = append(agents, ta)
		}
	}
	return wss, agents
}

// findTaskAgent picks one agent out of a load.
func findTaskAgent(agents []*taskAgent, ws, name string) *taskAgent {
	for _, ta := range agents {
		if ta.ws.Name == ws && ta.a.Name == name {
			return ta
		}
	}
	return nil
}

// items is the agent's open items as rows.
func (ta *taskAgent) items(back string) []taskRow {
	out := make([]taskRow, 0, len(ta.a.Open))
	for _, it := range ta.a.Open {
		out = append(out, ta.row(it, back))
	}
	return out
}

// pendingAnswers counts the answers in the agent's inbox it has not yet
// applied.
func (a *Agent) pendingAnswers() int {
	n := 0
	for _, list := range a.Answers {
		n += len(list)
	}
	return n
}

// taskRow is one item as the Tasks view shows it.
type taskRow struct {
	webItem
	WS string
	// Dot and StateText say the item's state: me (waits on the principal),
	// answered, waiting (on someone else), progress, todo, blocked, parked,
	// free (a free-text status).
	Dot, StateText string
	// Note is the status after its state; Question, Rec, By and Default are
	// its parts when it asks the principal something.
	Note, Question, Rec, By, Default string
	DueText                          string
	due                              *time.Time
	Late, Today                      bool
	// ToChat: the agent's session is live, so a reply goes to its chat.
	ToChat bool
	// Mine: waits on the principal and not yet answered; Answered: answered
	// from the dashboard, waiting for the agent to apply it.
	Mine, Answered bool
	// Avatar links the agent's image, "" for none.
	Avatar string
	// DueLabel is the due date in one short form ("Mon 9 Nov", "Today",
	// "Overdue 3d"), or the cell as written when it holds no date
	// (DueRaw).
	DueLabel string
	DueRaw   bool
	// Soon: due today or tomorrow. Href opens the row in the detail pane;
	// Selected: it is the one open there.
	Soon     bool
	Href     string
	Selected bool
	// ShowAgent: the row is in a list of many agents' items.
	ShowAgent bool
	// Back is the page the row is shown on, where a form returns to.
	Back string
	Edit *taskEdit
	rank int
}

// taskEdit is a row's edit form, prefilled with the cells as written.
type taskEdit struct {
	WS, Agent, ID, Hash, Back string
	State, Who, Note          string
	Priority, Due, Owner      string
	States, Priorities        []taskOpt
	// HasPriority, HasDue: the table has the column.
	HasPriority, HasDue bool
}

type taskOpt struct {
	Value, Label string
	Selected     bool
}

// askRe splits a status that asks the principal something:
// "<question>? Recommend: <answer>. If no answer by <date>: <default>."
var (
	recommendRe = regexp.MustCompile(`(?i)\brecommend(?:ation|ed)?:\s*`)
	defaultRe   = regexp.MustCompile(`(?i)\bif no (?:answer|reply) by ([^:]+):\s*`)
)

func splitAsk(note string) (question, rec, by, def string) {
	rest := note
	if m := defaultRe.FindStringSubmatchIndex(rest); m != nil {
		by, def = strings.TrimSpace(rest[m[2]:m[3]]), strings.TrimSpace(rest[m[1]:])
		rest = rest[:m[0]]
	}
	if m := recommendRe.FindStringIndex(rest); m != nil {
		rec = strings.TrimSpace(rest[m[1]:])
		rest = rest[:m[0]]
	}
	return strings.TrimSpace(rest), strings.TrimRight(rec, " "), by, def
}

// dayMonthRe finds a "12-11" or "25-02-2027" date in a Due/Target cell;
// the trackers write dates day first.
var dayMonthRe = regexp.MustCompile(`(?:^|[^\d-])(\d{1,2})-(\d{1,2})(?:-(\d{4}))?(?:[^\d-]|$)`)

// taskDue reads the due date of a Due/Target cell: an ISO date as the
// tracker parser reads it, else a day-first one. A date with no year is
// taken in the year that puts it nearest to now.
func taskDue(it Item, raw string, now time.Time) *time.Time {
	if it.Due != nil {
		return it.Due
	}
	m := dayMonthRe.FindStringSubmatch(cleanMarkdown(raw))
	if m == nil {
		return nil
	}
	day, _ := strconv.Atoi(m[1])
	month, _ := strconv.Atoi(m[2])
	if day < 1 || day > 31 || month < 1 || month > 12 {
		return nil
	}
	year := now.Year()
	if m[3] != "" {
		year, _ = strconv.Atoi(m[3])
	}
	d := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if m[3] == "" {
		if d.Sub(now) > 183*24*time.Hour {
			d = d.AddDate(-1, 0, 0)
		} else if now.Sub(d) > 183*24*time.Hour {
			d = d.AddDate(1, 0, 0)
		}
	}
	return &d
}

func (ta *taskAgent) row(it Item, back string) taskRow {
	ws, a := ta.ws, ta.a
	id := strings.TrimPrefix(it.ID, "#")
	tr := taskRow{webItem: makeWebItem(ws, a, it, false), WS: ws.Name, Back: back, Avatar: ta.avatar, ToChat: ta.live == "working" || ta.live == "idle"}
	raw, ok := ta.rows[id]
	statusRaw := it.Status
	if ok {
		statusRaw = raw.get("status")
	}
	_, _, tr.Note = statusParts(statusRaw)
	tr.Note = cleanMarkdown(tr.Note)

	today := time.Date(ws.Scanned.Year(), ws.Scanned.Month(), ws.Scanned.Day(), 0, 0, 0, 0, time.UTC)
	if ok {
		tr.DueText = cleanMarkdown(raw.get(raw.dueColumn()))
		if placeholder(tr.DueText) {
			tr.DueText = ""
		}
		tr.due = taskDue(it, raw.get(raw.dueColumn()), today)
	}
	if tr.due != nil && !it.Parked() {
		tr.Late = tr.due.Before(today)
		tr.Today = !tr.due.After(today)
		tr.Soon = !tr.Late && !tr.due.After(today.AddDate(0, 0, 1))
	}

	tr.DueLabel = dueLabel(tr.due, tr.DueText, today, it.Parked())
	tr.DueRaw = tr.due == nil && tr.DueText != ""
	state, who, std := it.State()
	tr.Mine = waitsOnPrincipal(it, ws.Principal) && !a.Answered(it)
	tr.Answered = len(tr.Answers) > 0
	switch {
	case it.Parked():
		tr.Dot, tr.StateText = "parked", "Parked"
	case it.Blocked():
		tr.Dot, tr.StateText = "blocked", "Blocked"
	case tr.Answered:
		tr.Dot, tr.StateText = "answered", "Answered, waiting for "+a.Name+" to apply"
	case tr.Mine:
		tr.Dot, tr.StateText = "me", "Waiting on you"
	case it.Waiting():
		tr.Dot, tr.StateText = "waiting", "Waiting"
		if who != "" {
			tr.StateText = "Waiting on " + who
		}
	case std && state == StateInProgress:
		tr.Dot, tr.StateText = "progress", "In progress"
	case it.NotStarted():
		tr.Dot, tr.StateText = "todo", "Not started"
	default:
		tr.Dot, tr.StateText = "free", cut(it.Status, 40)
	}
	if q, rec, by, def := splitAsk(tr.Note); tr.Mine || tr.Answered || rec != "" || def != "" {
		tr.Question, tr.Rec, tr.By, tr.Default = q, rec, by, def
	}

	tr.rank = priorityRank(it.Priority)*10 + btoi(!tr.Late)
	if ok && ta.hash != "" {
		tr.Edit = makeTaskEdit(ws, a, id, ta.hash, back, raw)
	}
	return tr
}

// dueLabel is a due date as the list shows it.
func dueLabel(due *time.Time, text string, today time.Time, parked bool) string {
	if due == nil {
		return text
	}
	days := int(due.Sub(today).Hours() / 24)
	switch {
	case days < 0 && !parked:
		return "Overdue " + strconv.Itoa(-days) + "d"
	case days == 0:
		return "Today"
	case days == 1:
		return "Tomorrow"
	case due.Year() != today.Year():
		return due.Format("Mon 2 Jan 2006")
	}
	return due.Format("Mon 2 Jan")
}

func makeTaskEdit(ws *Workspace, a *Agent, id, hash, back string, raw openRow) *taskEdit {
	e := &taskEdit{WS: ws.Name, Agent: a.Name, ID: id, Hash: hash, Back: back}
	e.State, e.Who, e.Note = statusParts(raw.get("status"))
	e.Owner = raw.get("owner")
	_, e.HasPriority = raw.Cols["priority"]
	e.Priority = raw.get("priority")
	e.HasDue = raw.dueColumn() != ""
	e.Due = raw.get(raw.dueColumn())
	if e.State == "" {
		e.States = append(e.States, taskOpt{"", "As written", true})
	}
	for _, st := range []string{StateNotStarted, StateInProgress, StateWaiting, StateBlocked, StateParked} {
		e.States = append(e.States, taskOpt{st, st, st == e.State})
	}
	known := false
	for _, p := range []string{"P1", "P2", "P3"} {
		known = known || p == e.Priority
		e.Priorities = append(e.Priorities, taskOpt{p, p, p == e.Priority})
	}
	if !known {
		label := e.Priority
		if label == "" {
			label = "None"
		}
		e.Priorities = append([]taskOpt{{e.Priority, label, true}}, e.Priorities...)
	}
	return e
}

// themeCookie keeps the Tasks pages' theme: light, dark or system.
const themeCookie = "tasks_theme"

func themeOf(r *http.Request) string {
	if c, err := r.Cookie(themeCookie); err == nil && (c.Value == "light" || c.Value == "dark") {
		return c.Value
	}
	return "system"
}

// tasksTheme sets the theme from the switch when the page runs no script,
// then goes back.
func (s *webServer) tasksTheme(w http.ResponseWriter, r *http.Request) {
	t := r.URL.Query().Get("t")
	if t != "light" && t != "dark" {
		t = "system"
	}
	http.SetCookie(w, &http.Cookie{Name: themeCookie, Value: t, Path: "/tasks", MaxAge: 365 * 24 * 3600, SameSite: http.SameSiteLaxMode})
	back := r.URL.Query().Get("back")
	if !strings.HasPrefix(back, "/tasks") || strings.HasPrefix(back, "//") {
		back = "/tasks"
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (s *webServer) renderTasks(w http.ResponseWriter, r *http.Request, p tasksPage) {
	p.Theme, p.Here = themeOf(r), r.URL.RequestURI()
	var buf bytes.Buffer
	if err := tasksTemplate.ExecuteTemplate(&buf, "tasks", p); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(buf.Bytes())
}

// tasksAnswer appends the principal's answer to any open item to the
// agent's inbox: the Tasks view's reply, which unlike the agent page's is
// not limited to items waiting on the principal. The tracker is not
// touched; the agent applies the answer.
func (s *webServer) tasksAnswer(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		r.Body = http.MaxBytesReader(w, r.Body, (maxImages+1)*maxImageBytes)
		if err := r.ParseMultipartForm(16 << 20); err != nil {
			http.Error(w, "could not read the answer: "+err.Error(), http.StatusBadRequest)
			return
		}
		defer r.MultipartForm.RemoveAll()
	}
	_, agents := s.loadTasks(r.FormValue("ws"), true)
	ta := findTaskAgent(agents, r.FormValue("ws"), r.FormValue("agent"))
	if ta == nil {
		http.Error(w, "no such agent", http.StatusNotFound)
		return
	}
	now := time.Now
	if s.src.Now != nil {
		now = s.src.Now
	}
	a := ta.a
	id := strings.TrimPrefix(strings.TrimSpace(r.FormValue("id")), "#")
	text := strings.TrimSpace(r.FormValue("text"))
	images := r.MultipartForm != nil && len(r.MultipartForm.File["image"]) > 0
	if text == "" && !images {
		http.Error(w, "the answer is empty", http.StatusBadRequest)
		return
	}
	var item *Item
	for i := range a.Open {
		if strings.TrimPrefix(a.Open[i].ID, "#") == id {
			item = &a.Open[i]
		}
	}
	if item == nil {
		http.Error(w, fmt.Sprintf("#%s is not an open item of %s", id, a.Name), http.StatusBadRequest)
		return
	}
	// The item's own reply box and Yes/No ask for the chat; One by one
	// keeps its answers in the inbox and tells the agent once at the end.
	toChat := r.FormValue("to") == "chat" && s.takesMessages(ta.ws, a)
	var paths []string
	var err error
	if toChat {
		paths, err = s.saveImages(r)
	} else {
		paths, err = saveInboxImages(a, r, now())
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	text = withImages(text, paths)
	if toChat {
		prompt := s.src.Prompt
		if prompt == nil {
			prompt = livePrompt
		}
		if err := prompt(a.Live.PaneID, "Re #"+id+" "+item.Headline()+": "+text); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
	}
	s.mu.Lock()
	path := inboxPath(a)
	before, rerr := os.ReadFile(path)
	err = appendAnswer(a, ta.ws.Principal, id, text, now(), toChat)
	undo := ""
	if err == nil && !toChat {
		// A chat message cannot be unsent, so only the inbox path has Undo.
		undo = s.remember(path, before, rerr == nil, "the answer to #"+id)
	}
	s.mu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	done(w, r, undo, "")
}

// takesMessages: the agent's own session is live and can take a message
// now (not starting, not at a prompt, not wrapping up), so an answer goes
// to its chat; otherwise it goes to the inbox.
func (s *webServer) takesMessages(ws *Workspace, a *Agent) bool {
	if ok, _ := canSend(a); !ok {
		return false
	}
	key := ws.Name + "/" + a.Name
	s.startMu.Lock()
	defer s.startMu.Unlock()
	if st := s.launches[key]; st != nil && st.err == "" && !st.done && time.Since(st.at) < launchGiveUp {
		return false
	}
	return s.closes[key] == nil
}

// saveInboxImages keeps an inbox answer's images under the agent's
// items/inbox-images/, named by time, and returns their absolute paths.
func saveInboxImages(a *Agent, r *http.Request, now time.Time) ([]string, error) {
	if r.MultipartForm == nil || len(r.MultipartForm.File["image"]) == 0 {
		return nil, nil
	}
	files := r.MultipartForm.File["image"]
	if len(files) > maxImages {
		return nil, fmt.Errorf("at most %d images per answer", maxImages)
	}
	dir := filepath.Join(a.Dir, "items", "inbox-images")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var paths []string
	for i, fh := range files {
		tmp, err := saveImage(dir, fh)
		if err != nil {
			return nil, err
		}
		dst := filepath.Join(dir, fmt.Sprintf("%s-%d%s", now.Format("20060102-150405"), i+1, filepath.Ext(tmp)))
		if err := os.Rename(tmp, dst); err != nil {
			return nil, err
		}
		abs, _ := filepath.Abs(dst)
		paths = append(paths, abs)
	}
	return paths, nil
}

// tasksEdit changes one tracker row from the form in an opened row, then
// sends the browser back to the view it came from.
func (s *webServer) tasksEdit(w http.ResponseWriter, r *http.Request) {
	refuse := func(msg string) { http.Error(w, msg, http.StatusBadRequest) }
	wsName, agentName, id := r.FormValue("ws"), r.FormValue("agent"), strings.TrimPrefix(r.FormValue("id"), "#")
	path, err := s.trackerPath(wsName, agentName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if r.FormValue("hash") != trackerHash(data) {
		refuse(agentName + "'s tracker changed after this page was drawn. Reload and make the change again.")
		return
	}
	text := string(data)
	row, err := findOpenRow(text, id)
	if err != nil {
		refuse(err.Error())
		return
	}
	edit, err := formEdit(r, id, row)
	if err != nil {
		refuse(err.Error())
		return
	}
	out, err := EditTrackerRow(text, edit)
	if err != nil {
		refuse(err.Error())
		return
	}
	undo := ""
	if out != text {
		if err := writeAtomic(path, []byte(out)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		undo = s.remember(path, data, true, "the change to #"+id)
	}
	back := r.FormValue("back")
	if back == "" {
		back = "/tasks"
	}
	done(w, r, undo, back)
}

// formEdit turns the form into a RowEdit that changes only the fields whose
// value differs from the cell as written.
func formEdit(r *http.Request, id string, row openRow) (RowEdit, error) {
	e := RowEdit{ID: id}
	oldState, oldWho, oldNote := statusParts(row.get("status"))
	state, who, note := r.FormValue("state"), strings.TrimSpace(r.FormValue("who")), strings.TrimSpace(r.FormValue("note"))
	if state != oldState || who != oldWho || note != oldNote {
		if state != StateWaiting {
			who = ""
		}
		st, err := composeStatus(state, who, note)
		if err != nil {
			return e, err
		}
		e.Status = &st
	}
	changed := func(field, col string) *string {
		v, ok := r.Form[field]
		if !ok || len(v) == 0 {
			return nil
		}
		val := strings.TrimSpace(v[0])
		if val == strings.TrimSpace(row.get(col)) {
			return nil
		}
		return &val
	}
	if v := changed("priority", "priority"); v != nil {
		if !priorityRe.MatchString(*v) || len(*v) != 2 {
			return e, errors.New("priority is P1, P2 or P3")
		}
		e.Priority = v
	}
	if col := row.dueColumn(); col != "" {
		e.Due = changed("due", col)
	}
	e.Owner = changed("owner", "owner")
	return e, nil
}

// trackerPath finds an agent's actions.md by workspace and agent name,
// without a full scan.
func (s *webServer) trackerPath(wsName, agent string) (string, error) {
	dir, ok := s.agentDir(wsName, agent)
	if !ok {
		return "", errors.New("no such agent")
	}
	return filepath.Join(dir, "actions.md"), nil
}
