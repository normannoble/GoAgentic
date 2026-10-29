package dash

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"hash/fnv"
	"html/template"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The web dashboard: the same picture as the TUI, served as HTML. It reads
// through the same scan and the same section logic, so the two agree. It
// writes two things, never a tracker: an answer to an item that waits on the
// principal, appended to the agent's inbox (answers.go), and a message typed
// into an agent's live session pane through Herdr (send). It can also start
// a stopped agent in Herdr (start), as the TUI's enter does.

//go:embed web/page.tmpl web/style.css
var webFiles embed.FS

// styleVersion is the stylesheet's hash, in its link, so a browser never
// keeps an old one.
var styleVersion = func() string {
	data, _ := webFiles.ReadFile("web/style.css")
	h := fnv.New32a()
	_, _ = h.Write(data)
	return strconv.FormatUint(uint64(h.Sum32()), 36)
}()

var webTemplate = template.Must(template.New("page.tmpl").Funcs(template.FuncMap{
	"trimHash": func(id string) string { return strings.TrimPrefix(id, "#") },
	"trimPlan": func(next string) string { return strings.TrimPrefix(next, "» ") },
	// stepArgs feeds the "steprow" template: a plan step or priority that
	// names no open tracker item.
	"stepArgs": func(pri, text, desc string) map[string]string {
		return map[string]string{"Priority": pri, "Text": text, "Desc": desc}
	},
	"plural":       plural,
	"dur":          roundDur,
	"styleVersion": func() string { return styleVersion },
}).ParseFS(webFiles, "web/page.tmpl"))

// WebSource tells the server what to show. With FleetRoot set it serves
// every workspace under it; otherwise only the workspace at Root.
type WebSource struct {
	FleetRoot string
	Root      string
	// Options builds the scan options; all includes retired agents.
	Options func(all bool) Options
	// ListenHost is the address the server listens on when it is not a
	// loopback one; requests must name it or a loopback host (see guard).
	ListenHost string
	// AllowHosts are more names requests may use, such as the public
	// hostname a Cloudflare Tunnel serves the dashboard on. They need Access.
	AllowHosts []string
	// Access, when set, requires a valid Cloudflare Access token on every
	// request (web_access.go).
	Access *Access
	// Now stamps answers; nil means time.Now.
	Now func() time.Time
	// Prompt types text into a live pane and submits it; nil means
	// `herdr agent prompt`.
	Prompt func(pane, text string) error
	// CodexHome holds codex's sessions; "" means CodexHome().
	CodexHome string
	// ClosePane closes a pane; nil means `herdr pane close`.
	ClosePane func(pane string) error
	// OrderFile keeps the sidebar's order; "" means dash-order.json in the
	// user's config folder (web_order.go).
	OrderFile string
	// UploadDir keeps images sent with a message; "" means
	// goagentic/uploads in the user's cache folder (web_images.go).
	UploadDir string
	// ReadPane is a pane's last lines; nil means `herdr pane read`.
	ReadPane func(pane string) (string, error)
	// CloseEvery is how often a Wrap & close is checked on the server, so
	// the pane closes when the wrap is done whether or not a page is open;
	// 0 means every 3 s, below 0 only when the agent's page refreshes.
	CloseEvery time.Duration
	// Launch starts a stopped agent; nil means LaunchInBackground.
	Launch func(ws *Workspace, a *Agent) (string, error)
}

// Serve runs the web dashboard on addr until ctx ends.
func Serve(ctx context.Context, addr string, src WebSource, log io.Writer) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	if !loopback(ln.Addr()) {
		src.ListenHost = ln.Addr().(*net.TCPAddr).IP.String()
	}
	srv := &http.Server{Handler: NewWebHandler(src), ReadHeaderTimeout: 10 * time.Second}
	fmt.Fprintf(log, "Dashboard on http://%s/ — Ctrl-C to stop.\n", ln.Addr())
	if src.Access != nil {
		fmt.Fprintf(log, "Cloudflare Access required on every request (team %s).\n", src.Access.Team)
	}
	if src.ListenHost != "" {
		fmt.Fprintln(log, "Not a loopback address: anyone who can reach it can read every tracker shown, answer as you, and type into your agents' sessions.")
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func loopback(addr net.Addr) bool {
	tcp, ok := addr.(*net.TCPAddr)
	return ok && tcp.IP.IsLoopback()
}

type webServer struct {
	src WebSource
	// mu keeps scans one at a time: each asks Herdr and git, and an
	// auto-refreshing tab or two must not pile them up.
	mu   sync.Mutex
	chat *ChatCache
	// fleet caches the sidebar's workspace list (web_nav.go).
	fleetMu sync.Mutex
	fleet   fleetCache
	orderMu sync.Mutex
	// launches holds the agents (ws/agent) a Start opened and that are not
	// yet ready, so the page shows them loading and a second click or tab
	// does not open a second session.
	startMu  sync.Mutex
	launches map[string]*launchState
	// closes holds the agents a Wrap & close is ending (under startMu).
	closes map[string]*closeState
}

// closeState is one Wrap & close: the wrap command was typed at at; the
// pane closes once the wrap has written its session log and settled.
type closeState struct {
	at   time.Time
	busy bool // seen working since
	err  string
}

// launchState is one Start: running until done, then loading until the
// agent has finished its start-up turn; err when it failed.
type launchState struct {
	at, doneAt time.Time
	done       bool
	err        string
}

// launchGiveUp is how long a Start stays shown as loading or failed.
const launchGiveUp = 10 * time.Minute

// NewWebHandler returns the dashboard's routes.
func NewWebHandler(src WebSource) http.Handler {
	s := &webServer{src: src, chat: NewChatCache(), launches: map[string]*launchState{}, closes: map[string]*closeState{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /w/{ws}", s.workspace)
	mux.HandleFunc("GET /w/{ws}/{agent}", s.agent)
	mux.HandleFunc("GET /t/{ws}/{lead}", s.team)
	mux.HandleFunc("GET /w/{ws}/{agent}/file", s.file)
	mux.HandleFunc("POST /w/{ws}/{agent}/answer", s.answer)
	mux.HandleFunc("POST /w/{ws}/{agent}/send", s.send)
	mux.HandleFunc("POST /w/{ws}/{agent}/start", s.start)
	mux.HandleFunc("POST /w/{ws}/{agent}/close", s.close)
	mux.HandleFunc("POST /order", s.reorder)
	mux.HandleFunc("GET /uploads/{name}", s.upload)
	mux.HandleFunc("GET /style.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		// The page names the sheet by its hash, so a new build is fetched.
		w.Header().Set("Cache-Control", "no-cache")
		data, _ := webFiles.ReadFile("web/style.css")
		_, _ = w.Write(data)
	})
	return s.guard(mux)
}

// guard turns away requests a browser was tricked into sending. The Host
// must be a loopback name or the address we listen on (against DNS
// rebinding). A write must carry the X-GoAgentic header, which a page on
// another site cannot add without a CORS preflight this server never
// grants, and any Origin must pass the same host check.
func (s *webServer) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.src.Access != nil {
			if _, err := s.src.Access.Check(r); err != nil {
				http.Error(w, "Cloudflare Access sign-in required: "+err.Error(), http.StatusForbidden)
				return
			}
		}
		if !s.allowedHost(r.Host) {
			http.Error(w, "unexpected Host", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			origin := r.Header.Get("Origin")
			host := strings.TrimPrefix(strings.TrimPrefix(origin, "http://"), "https://")
			if r.Header.Get("X-GoAgentic") != "1" || (origin != "" && (host == origin || !s.allowedHost(host))) {
				http.Error(w, "cross-site write refused", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *webServer) allowedHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" || (s.src.ListenHost != "" && host == s.src.ListenHost) {
		return true
	}
	for _, h := range s.src.AllowHosts {
		if strings.EqualFold(host, h) {
			return true
		}
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// page is everything the template needs; one of Workspace, Agent or File
// is set.
type page struct {
	Title   string
	Heading string
	All     bool
	Scanned time.Time
	Live    bool
	Counts  *Counts
	First   string
	// Queue, Others and Overdue follow the workspace's agents as cards: what
	// waits on the principal, on others, and what is overdue (web_views.go).
	Queue, Others, Overdue *queueView
	// Nav is the app shell: workspace picker and sidebar (web_nav.go).
	Nav *nav

	Workspace *webWorkspace
	Agent     *webAgent
	Team      *webTeam
	File      *webFile
}

type webWorkspace struct {
	Rows []agentRow
	// Teams are the workspace's leads and their members (web_team.go).
	Teams []teamCard
	// Over and Soon count running sessions past or near the context
	// budget.
	Over, Soon int
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

type agentRow struct {
	Row
	Href, Tone, LiveClass string
	NextP1                bool
	// OnYou counts what waits on the principal: items and unread runs.
	OnYou int
	// OnOthers counts P1 items waiting on someone else.
	OnOthers int
	Title    string
	// Now is the agent's running session, when it has one; Ctx its
	// session health.
	Now *nowLine
	Ctx *webCtx
}

type webAgent struct {
	Name, Title string
	Head        webHead
	// Focus is the running session's agreed priorities.
	Focus *sessionFocus
	// Usage is the running session's model and context use.
	Usage *sessionUsage
	// Initial and Hue make the header's tile.
	Initial string
	Hue     int
	// Active: a session is running or starting, so the page leads with the
	// chat; otherwise with the tracker. State says what the session does.
	Active            bool
	State, StateClass string
	// Activity is the harness's own status line while it works.
	Activity *activity
	Plan     []webStep
	Mine     []webItem
	Answered []webItem
	Inbox    int
	// Scheduled is its tasks in the register; SchedMissed: one missed a run.
	Scheduled   []webTask
	SchedMissed bool
	Others      []webItem
	Blocked     []webItem
	Open        []webItem
	OpenCounts  map[string]int
	Parked      []webItem
	LastSession *Session
	// Chat is the running session as a conversation, read only while
	// ChatLive (the agent is running in a pane). A stopped agent shows no
	// chat, only when its last session ended (LastEnded).
	Chat      []webChat
	ChatLive  bool
	ChatNote  string
	LastEnded time.Time
	// CanSend: the agent's own session is running in a pane, so the page
	// offers a send box posting to SendURL. SendNote says why not.
	CanSend  bool
	SendURL  string
	SendNote string
	// CanStart: the agent is not running, so the page offers a Start
	// button posting to StartURL; Harness is what it starts in.
	CanStart bool
	StartURL string
	Harness  string
	// StartsIn is where Start opens it: "a new Herdr tab" or "a new T3
	// Code thread".
	StartsIn string
	// Loading: a session is starting and has not finished its start-up
	// turn; the page shows LoadingText with a spinner instead of the chat.
	// Prompt says it stopped at a prompt in its pane instead.
	Loading     bool
	LoadingText string
	Prompt      bool
	StartError  string
	// Wrapping: a Wrap & close is running; WrapStopped: the agent stopped
	// without writing its session log, so the page offers Close anyway.
	CloseURL    string
	Busy        bool
	Wrapping    bool
	WrapStopped bool
	CloseError  string
	// readable: the chat came from a session the page can read, which
	// says when a start is done; otherwise the pane settling does.
	readable    bool
	SessionHref string
	Health      Health
	HealthLine  string
}

type webChat struct {
	ChatEntry
	HTML    template.HTML
	Summary string
	// Images are the screenshots sent with a message, as page links.
	Images []string
}

type webItem struct {
	Item
	Headline, Detail, Note, History string
	// Desc is the first sentence after the title, shown under it on the
	// closed item; Detail then holds the rest of the cell.
	Desc string
	// Step is the agent's own words for the item where a session plan or
	// priority list names it, shown first in the open panel after StepLabel.
	StepLabel, Step string
	// Key names the item uniquely on a page (agent/id), so open panels
	// survive a refresh.
	Key string
	// CanAnswer: the item waits on the principal, so the page offers an
	// answer box posting to AnswerURL. Answers are the ones not yet picked
	// up by the agent.
	CanAnswer bool
	AnswerURL string
	Answers   []Answer
	Agent     string
	AgentLive bool
	// Late: past its due date.
	Late bool
	// Reply: the agent's own session is running, so an answer is typed into
	// it (SendURL) instead of its inbox, and it sees it now.
	Reply   bool
	SendURL string
}

type webFile struct {
	Name, Text string
}

func (s *webServer) all(r *http.Request) bool { return r.URL.Query().Get("all") == "1" }

// link keeps ?all=1 on every link once it is set.
func link(all bool, parts ...string) string {
	p := "/" + strings.Join(parts, "/")
	if all {
		p += "?all=1"
	}
	return p
}

// home opens a workspace: the one last looked at (the ws cookie), else the
// first.
func (s *webServer) home(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(s.src.Root)
	if s.src.FleetRoot != "" {
		dirs := FleetDirs(s.src.FleetRoot)
		if len(dirs) == 0 {
			http.Error(w, "no workspaces under "+s.src.FleetRoot, http.StatusNotFound)
			return
		}
		name = filepath.Base(dirs[0])
		if c, err := r.Cookie("ws"); err == nil {
			for _, dir := range dirs {
				if filepath.Base(dir) == c.Value {
					name = c.Value
				}
			}
		}
	}
	http.Redirect(w, r, link(s.all(r), "w", name), http.StatusFound)
}

// scanWorkspace finds a workspace by its directory name and scans it.
func (s *webServer) scanWorkspace(name string, all bool) (*Workspace, error) {
	root := ""
	if s.src.FleetRoot == "" {
		if name == filepath.Base(s.src.Root) {
			root = s.src.Root
		}
	} else {
		for _, dir := range FleetDirs(s.src.FleetRoot) {
			if filepath.Base(dir) == name {
				root = dir
			}
		}
	}
	if root == "" {
		return nil, errNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return Scan(root, s.src.Options(all))
}

var errNotFound = errors.New("not found")

func (s *webServer) fail(w http.ResponseWriter, err error) {
	if errors.Is(err, errNotFound) {
		http.NotFound(w, nil)
		return
	}
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

func (s *webServer) workspace(w http.ResponseWriter, r *http.Request) {
	all := s.all(r)
	ws, err := s.scanWorkspace(r.PathValue("ws"), all)
	if err != nil {
		s.fail(w, err)
		return
	}
	c := ws.Counts()
	p := page{
		Title: ws.Name, Heading: ws.Name, All: all, Scanned: ws.Scanned, Live: ws.Live,
		Counts: &c, First: firstName(ws.Principal), Workspace: &webWorkspace{Teams: teamCards(ws, all)},
		Nav: s.workspaceNav(ws, "", all),
	}
	// Three cards lead: what waits on the principal, on others, and what
	// is overdue; then a card per agent.
	p.Queue = makeQueue(queueLines(ws, all), 6)
	p.Queue.Title, p.Queue.Class, p.Queue.Empty, p.Queue.Key = "Waiting on you", "foryou", "Nothing waiting on you.", "q-you"
	p.Others = makeQueue(othersLines(ws, all), 6)
	p.Others.Title, p.Others.Class, p.Others.Empty, p.Others.Key = "Blocked by others", "waiting", "Nothing waiting on others.", "q-others"
	p.Overdue = makeQueue(overdueLines(ws, all), 6)
	p.Overdue.Title, p.Overdue.Class, p.Overdue.Empty, p.Overdue.Key = "Overdue", "blocked", "Nothing overdue.", "q-overdue"
	// The home opens this workspace next time.
	http.SetCookie(w, &http.Cookie{Name: "ws", Value: ws.Name, Path: "/", MaxAge: 365 * 24 * 3600, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	now := map[string]nowLine{}
	for _, l := range s.nowLines(ws, all) {
		now[l.Agent] = l
	}
	for _, a := range s.agentsInOrder(ws) {
		row := MakeRow(ws, a)
		ar := agentRow{
			Row: row, Href: link(all, "w", ws.Name, a.Name), Tone: toneClass(agentTone(ws, a)), LiveClass: liveClass(row.Live),
			NextP1: strings.HasPrefix(row.Next, "P1 "), OnYou: len(a.ForPrincipal(ws.Principal)) + a.InboxUnprocessed, OnOthers: a.WaitingCount(ws.Principal), Title: a.Title,
		}
		if l, ok := now[a.Name]; ok {
			ar.Now = &l
			if path, ok, _ := s.chatPath(ws, a); ok && path != "" {
				if ar.Ctx = contextHealth(readUsage(path), ws.ContextBudget); ar.Ctx != nil {
					p.Workspace.Over += btoi(ar.Ctx.Over)
					p.Workspace.Soon += btoi(ar.Ctx.Soon)
				}
			}
		}
		p.Workspace.Rows = append(p.Workspace.Rows, ar)
	}
	s.render(w, r, p)
}

func (s *webServer) findAgent(w http.ResponseWriter, r *http.Request) (*Workspace, *Agent, bool) {
	ws, err := s.scanWorkspace(r.PathValue("ws"), true)
	if err != nil {
		s.fail(w, err)
		return nil, nil, false
	}
	for _, a := range ws.Agents {
		if a.Name == r.PathValue("agent") {
			return ws, a, true
		}
	}
	http.NotFound(w, r)
	return nil, nil, false
}

func (s *webServer) agent(w http.ResponseWriter, r *http.Request) {
	all := s.all(r)
	ws, a, ok := s.findAgent(w, r)
	if !ok {
		return
	}
	sec := DetailSections(ws, a)
	items := func(list []Item) []webItem {
		out := make([]webItem, len(list))
		for i, it := range list {
			out[i] = makeWebItem(ws, a, it, all)
		}
		return out
	}
	wa := &webAgent{
		Name: a.Name, Title: a.Title,
		Initial: strings.ToUpper(a.Name[:1]), Hue: avatarHue(a.Name),
		Mine: items(sec.Mine), Answered: items(sec.Answered), Inbox: sec.Inbox, Scheduled: webTasks(ws.Scanned, a.Scheduled),
		Others: items(sec.Others), Blocked: items(sec.Blocked), Open: items(sec.Open),
		OpenCounts: sec.OpenCounts, Parked: items(sec.Parked), Health: a.Health,
		HealthLine: fmt.Sprintf("standing %d/%d · sessions %d/%d · actions %d KB",
			len(a.Standing), maxStanding, len(a.Sessions), maxSessions, a.ActionsSize/1024),
	}
	wa.Plan = linkPlan(sec.Plan, wa.Mine, wa.Answered, wa.Others, wa.Blocked, wa.Open, wa.Parked)
	for _, t := range wa.Scheduled {
		wa.SchedMissed = wa.SchedMissed || t.Missed != ""
	}
	wa.ChatLive = a.Live != nil && a.Live.Running
	if wa.CanSend, wa.SendNote = canSend(a); wa.CanSend {
		wa.SendURL = "/w/" + ws.Name + "/" + a.Name + "/send"
	}
	if !wa.ChatLive {
		stopped(ws, a, wa)
		wa.LastEnded = a.LastActive
	} else {
		s.readChat(ws, a, wa)
	}
	s.applyLaunch(ws, a, wa)
	s.applyClose(ws, a, wa)
	wa.Head = makeHead(ws, a, wa.Usage)

	if last, ok := a.LastSession(); ok {
		wa.LastSession = &last
		wa.SessionHref = fileLink(ws, a, filepath.Join("memory", "sessions", last.File), all)
	}
	wa.Active = wa.ChatLive || wa.Loading || wa.Prompt
	if wa.Loading || wa.Prompt || wa.Wrapping {
		// Not taking messages: answers go to the inbox.
		for _, list := range [][]webItem{wa.Mine, wa.Answered} {
			for i := range list {
				list[i].Reply = false
			}
		}
	}
	wa.State, wa.StateClass = agentState(a, wa)
	if wa.State == "working" || wa.State == "starting" {
		wa.Activity = s.activityOf(a)
	}
	// No count header: the workspace's counts are not this agent's.
	s.render(w, r, page{
		Title: a.Name + " · " + ws.Name,
		All:   all, Scanned: ws.Scanned, Live: ws.Live, First: firstName(ws.Principal), Agent: wa,
		Nav: s.workspaceNav(ws, a.Name, all),
	})
}

// agentState is the agent page's one word on its session.
func agentState(a *Agent, wa *webAgent) (string, string) {
	switch {
	case wa.Wrapping:
		return "wrapping up", "accent"
	case wa.Loading:
		return "starting", "accent"
	case wa.Prompt:
		return "prompt in its pane", "blocked"
	case !wa.ChatLive:
		return "not running", "dim"
	case !sessionPane(a.Live):
		return "peer or task pane", "dim"
	case a.Live.Status == "working":
		return "working", "ok"
	case settled(a.Live):
		return "your turn", "foryou"
	}
	return a.Live.State(), "dim"
}

// file shows one Markdown file from the agent's own folder: an item's
// history note or a session log.
func (s *webServer) file(w http.ResponseWriter, r *http.Request) {
	all := s.all(r)
	ws, a, ok := s.findAgent(w, r)
	if !ok {
		return
	}
	rel := r.URL.Query().Get("p")
	path, ok := agentFile(a.Dir, rel)
	if !ok {
		http.NotFound(w, r)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.render(w, r, page{
		Title: rel + " · " + a.Name,
		All:   all, Scanned: ws.Scanned, Live: ws.Live, File: &webFile{Name: rel, Text: string(data)},
		Nav: s.workspaceNav(ws, a.Name, all),
	})
}

// answer appends the principal's answer to an item to the agent's inbox.
// The page refreshes itself afterwards, so the reply is only a status.
func (s *webServer) answer(w http.ResponseWriter, r *http.Request) {
	ws, a, ok := s.findAgent(w, r)
	if !ok {
		return
	}
	id := strings.TrimPrefix(r.FormValue("id"), "#")
	var item *Item
	for i := range a.Open {
		if strings.TrimPrefix(a.Open[i].ID, "#") == id {
			item = &a.Open[i]
		}
	}
	if item == nil || !waitsOnPrincipal(*item, ws.Principal) {
		http.Error(w, "#"+id+" is not an open item waiting on "+firstName(ws.Principal), http.StatusBadRequest)
		return
	}
	now := time.Now
	if s.src.Now != nil {
		now = s.src.Now
	}
	s.mu.Lock()
	err := AppendAnswer(a, ws.Principal, id, r.FormValue("text"), now())
	s.mu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// send types the principal's message into the agent's live session pane and
// submits it, as if typed there. It never starts an agent, and never writes
// to a peer or task pane.
func (s *webServer) send(w http.ResponseWriter, r *http.Request) {
	_, a, ok := s.findAgent(w, r)
	if !ok {
		return
	}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		r.Body = http.MaxBytesReader(w, r.Body, (maxImages+1)*maxImageBytes)
		if err := r.ParseMultipartForm(16 << 20); err != nil {
			http.Error(w, "could not read the message: "+err.Error(), http.StatusBadRequest)
			return
		}
		defer r.MultipartForm.RemoveAll()
	}
	text := strings.TrimSpace(r.FormValue("text"))
	images := r.MultipartForm != nil && len(r.MultipartForm.File["image"]) > 0
	id := strings.TrimPrefix(r.FormValue("id"), "#")
	switch {
	case id != "" && r.FormValue("ask") == "more":
		// More info on an item: the shorthand for what it needs from you.
		text = "#" + id + "?"
	case text == "" && !images:
		http.Error(w, "nothing to send", http.StatusBadRequest)
		return
	case id != "":
		// An update on an item, from its box: say which.
		text = "Update on #" + id + ": " + text
	case len(text) > maxAnswer:
		http.Error(w, fmt.Sprintf("message longer than %d characters", maxAnswer), http.StatusBadRequest)
		return
	}
	if ok, why := canSend(a); !ok {
		http.Error(w, why, http.StatusConflict)
		return
	}
	paths, err := s.saveImages(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	text = withImages(text, paths)
	prompt := s.src.Prompt
	if prompt == nil {
		prompt = livePrompt
	}
	if err := prompt(a.Live.PaneID, text); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// stopped offers Start for an agent that is not running, or says why not.
func stopped(ws *Workspace, a *Agent, wa *webAgent) {
	wa.Harness = harnessOf(ws, a)
	if ok, why := HarnessAvailable(ws, wa.Harness); ok {
		wa.CanStart, wa.StartURL = true, "/w/"+ws.Name+"/"+a.Name+"/start"
		wa.StartsIn = "a new Herdr tab"
		if t3, _ := startsInT3(ws); t3 {
			wa.StartsIn = "a new T3 Code thread"
		}
	} else {
		wa.SendNote = a.Name + " is not running, and cannot start in " + wa.Harness + ": " + why + "."
	}
}

// readChat fills the chat from the session running in the agent's pane:
// for Claude Code the transcript Herdr names, for codex the agent's newest
// session in the workspace (begun after a Start from this page, if there
// was one), so an earlier session never shows as this one. A session still
// on its start-up turn shows loading.
func (s *webServer) readChat(ws *Workspace, a *Agent, wa *webAgent) {
	path, ok, note := s.chatPath(ws, a)
	if !ok {
		wa.ChatNote = note
		return
	}
	wa.readable = true
	if path != "" {
		wa.Usage = readUsage(path)
	}
	if path == "" || !fileExists(path) {
		wa.Loading, wa.LoadingText = true, "Starting "+a.Name+"…"
		if settled(a.Live) && s.launchedAt(ws, a).IsZero() {
			// Not starting: it is running a session this page cannot find.
			wa.Loading, wa.LoadingText = false, ""
			wa.ChatNote = "No session of " + a.Name + " found in this workspace's transcripts."
		}
		return
	}
	entries, err := s.chat.Session(path, a.Name)
	if err != nil {
		wa.ChatNote = "Could not read the session: " + err.Error()
	}
	wa.Focus = findFocus(entries)
	linkFocus(wa.Focus, wa.Mine, wa.Answered, wa.Others, wa.Blocked, wa.Open, wa.Parked)
	if len(entries) > maxChat {
		entries = entries[len(entries)-maxChat:]
	}
	replied := false
	for _, e := range entries {
		wc := webChat{ChatEntry: e}
		switch e.Kind {
		case "you":
			wc.Text, wc.Images = s.chatImages(unwrapPaste(e.Text))
		case "agent":
			wc.HTML = chatMarkdown(e.Text)
			replied = true
		case "tools":
			wc.Summary = ToolSummary(e.Tools)
		}
		wa.Chat = append(wa.Chat, wc)
	}
	// Started with its start command and no reply yet: still loading.
	if !replied && len(entries) > 0 && isStartText(entries[0].Text) && !settled(a.Live) {
		wa.Loading, wa.LoadingText = true, "Starting "+a.Name+"…"
	}
}

// chatPath is the session file of the agent's running session, "" while it
// is not written yet; ok is false for a CLI the chat cannot read, with why.
func (s *webServer) chatPath(ws *Workspace, a *Agent) (path string, ok bool, note string) {
	switch {
	case a.Live.ClaudeSession() != "":
		dir := filepath.Dir(a.Transcript)
		if a.Transcript == "" {
			dir = filepath.Join(s.src.Options(true).Transcripts, projectDirName(ws.Root))
		}
		return filepath.Join(dir, a.Live.ClaudeSession()+".jsonl"), true, ""
	case a.Live.Agent == "codex":
		home := s.src.CodexHome
		if home == "" {
			home = CodexHome()
		}
		// A session that began before this page's Start is an earlier one.
		return CodexSession(home, ws.Root, a.Name, s.launchedAt(ws, a).Add(-30*time.Second)), true, ""
	case a.Live.Agent == "claude":
		return a.Transcript, true, "" // a Herdr too old to name the session
	}
	return "", false, a.Name + " runs in " + a.Live.Agent + "; the chat reads Claude Code and codex sessions only."
}

// launchedAt is when this page last started the agent, or zero.
func (s *webServer) launchedAt(ws *Workspace, a *Agent) time.Time {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	if st := s.launches[ws.Name+"/"+a.Name]; st != nil {
		return st.at
	}
	return time.Time{}
}

// applyLaunch shows a Start in progress: loading until the agent is up and
// idle after its start-up turn (for Claude, until it has replied), the
// prompt it stopped at, or why it failed.
func (s *webServer) applyLaunch(ws *Workspace, a *Agent, wa *webAgent) {
	live := a.Live != nil && a.Live.Running
	key := ws.Name + "/" + a.Name
	s.startMu.Lock()
	defer s.startMu.Unlock()
	st := s.launches[key]
	if st == nil || time.Since(st.at) > launchGiveUp {
		delete(s.launches, key)
		if wa.Loading && a.Live.Status == "blocked" {
			wa.Loading, wa.Prompt = false, true
		}
		return
	}
	switch {
	case st.err != "":
		wa.StartError = st.err
		return
	case live && a.Live.Status == "blocked":
		wa.Loading, wa.Prompt = false, true
		return
	case !st.done || !live:
		wa.Loading, wa.LoadingText = true, "Starting "+a.Name+"…"
	case !wa.readable && (!settled(a.Live) || time.Since(st.doneAt) < 5*time.Second):
		// No session to watch: wait for the pane to settle.
		wa.Loading, wa.LoadingText = true, "Starting "+a.Name+"…"
	}
	if !wa.Loading && live {
		delete(s.launches, key)
	}
}

// settled: the CLI has finished its turn and waits for input.
func settled(p *LivePane) bool { return p.Status == "idle" || p.Status == "done" }

// start opens a stopped agent in Herdr: a new tab in its workspace's Space
// (or its leftover shell pane), running its start command, without moving
// the principal's view. The agent then shows as live and the send box
// appears.
func (s *webServer) start(w http.ResponseWriter, r *http.Request) {
	ws, a, ok := s.findAgent(w, r)
	if !ok {
		return
	}
	if a.Live != nil && a.Live.Running {
		http.Error(w, a.Name+" is already running", http.StatusConflict)
		return
	}
	if ok, why := HarnessAvailable(ws, harnessOf(ws, a)); !ok {
		http.Error(w, why, http.StatusConflict)
		return
	}
	key := ws.Name + "/" + a.Name
	s.startMu.Lock()
	if st := s.launches[key]; st != nil && st.err == "" && time.Since(st.at) < launchGiveUp {
		s.startMu.Unlock()
		http.Error(w, a.Name+" is already starting", http.StatusConflict)
		return
	}
	st := &launchState{at: time.Now()}
	s.launches[key] = st
	s.startMu.Unlock()
	launch := s.src.Launch
	if launch == nil {
		launch = LaunchInBackground
	}
	// Opening a pane waits for the CLI to be ready; the page shows it
	// loading meanwhile, so the request does not wait.
	go func() {
		_, err := launch(ws, a)
		s.startMu.Lock()
		defer s.startMu.Unlock()
		st.done, st.doneAt = true, time.Now()
		// Stopped at a prompt (folder trust, say): not a failure, the page
		// shows the prompt waiting in its pane.
		if err != nil && !strings.Contains(err.Error(), "not ready") && !strings.Contains(err.Error(), "blocked") {
			st.err = "Start failed: " + err.Error()
		}
	}()
	w.WriteHeader(http.StatusAccepted)
}

// close ends the agent's session: it types the harness's wrap command into
// the pane, and the page closes the pane once the wrap is done
// (applyClose). With force=1 it closes the pane at once, wrapped or not.
func (s *webServer) close(w http.ResponseWriter, r *http.Request) {
	ws, a, ok := s.findAgent(w, r)
	if !ok {
		return
	}
	if a.Live == nil || !a.Live.Running || !sessionPane(a.Live) {
		http.Error(w, a.Name+" has no session of yours running", http.StatusConflict)
		return
	}
	key := ws.Name + "/" + a.Name
	if r.FormValue("force") == "1" {
		if err := s.closePane(a.Live.PaneID); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		s.startMu.Lock()
		delete(s.closes, key)
		s.startMu.Unlock()
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if ok, why := canSend(a); !ok {
		http.Error(w, why, http.StatusConflict)
		return
	}
	if !settled(a.Live) {
		http.Error(w, a.Name+" is busy; wrap it when it has finished its turn", http.StatusConflict)
		return
	}
	s.startMu.Lock()
	if st := s.closes[key]; st != nil && st.err == "" {
		s.startMu.Unlock()
		http.Error(w, a.Name+" is already wrapping up", http.StatusConflict)
		return
	}
	s.closes[key] = &closeState{at: time.Now()}
	s.startMu.Unlock()
	prompt := s.src.Prompt
	if prompt == nil {
		prompt = livePrompt
	}
	if err := prompt(a.Live.PaneID, frameworkCommand(a.Live.Agent, "wrap")); err != nil {
		s.startMu.Lock()
		delete(s.closes, key)
		s.startMu.Unlock()
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if s.src.CloseEvery >= 0 {
		go s.watchClose(ws.Name, a.Name, key)
	}
	w.WriteHeader(http.StatusAccepted)
}

// closeWatchFor bounds how long a wrap is watched.
const closeWatchFor = 30 * time.Minute

// watchClose closes the agent's pane once its wrap is done, checking on
// its own so that no page needs to be open.
func (s *webServer) watchClose(wsName, agent, key string) {
	every := s.src.CloseEvery
	if every == 0 {
		every = 3 * time.Second
	}
	for start := time.Now(); time.Since(start) < closeWatchFor; {
		time.Sleep(every)
		s.startMu.Lock()
		_, watching := s.closes[key]
		s.startMu.Unlock()
		if !watching {
			return // closed, cancelled, or stopped
		}
		ws, err := s.scanWorkspace(wsName, false)
		if err != nil {
			continue
		}
		for _, a := range ws.Agents {
			if a.Name == agent {
				s.startMu.Lock()
				s.checkClose(ws, a)
				s.startMu.Unlock()
			}
		}
	}
}

func (s *webServer) closePane(pane string) error {
	if s.src.ClosePane != nil {
		return s.src.ClosePane(pane)
	}
	if IsT3Pane(pane) {
		return t3Close(pane)
	}
	out, err := exec.Command(herdrBin(), "pane", "close", pane).CombinedOutput()
	if err != nil {
		return fmt.Errorf("herdr pane close: %s", herdrError(out, err))
	}
	return nil
}

// applyClose follows a Wrap & close on each look at the page: the pane
// closes once the agent has settled with a session log written since the
// wrap was typed; settled without one (it asked something, or stopped)
// leaves the pane open and offers Close anyway.
func (s *webServer) applyClose(ws *Workspace, a *Agent, wa *webAgent) {
	live := a.Live != nil && a.Live.Running
	if live && sessionPane(a.Live) && !wa.Loading {
		wa.CloseURL = "/w/" + ws.Name + "/" + a.Name + "/close"
		wa.Busy = !settled(a.Live)
	}
	key := ws.Name + "/" + a.Name
	s.startMu.Lock()
	defer s.startMu.Unlock()
	st := s.closes[key]
	if st == nil {
		return
	}
	// A session that is wrapping up is past starting, whatever its chat shows.
	wa.Loading = false
	if live && wa.CloseURL == "" && sessionPane(a.Live) {
		wa.CloseURL = "/w/" + ws.Name + "/" + a.Name + "/close"
	}
	switch s.checkClose(ws, a) {
	case closeGone, closeDone:
		wa.ChatLive, wa.CanSend, wa.CloseURL, wa.Chat, wa.Focus = false, false, "", nil, nil
		wa.LastEnded = time.Now()
		stopped(ws, a, wa)
	case closeFailed:
		wa.CloseError = st.err
	case closeWrapping:
		wa.Wrapping = true
	case closeStopped:
		wa.WrapStopped = true
	}
}

type closeStep int

const (
	closeNone     closeStep = iota // no Wrap & close
	closeWrapping                  // the wrap is running
	closeDone                      // wrapped: its pane was closed now
	closeGone                      // its pane is gone
	closeFailed                    // closing failed (st.err)
	closeStopped                   // it stopped without writing its log
)

// checkClose moves a Wrap & close on: once the agent has settled with its
// session log written, its pane is closed. Callers hold startMu.
func (s *webServer) checkClose(ws *Workspace, a *Agent) closeStep {
	key := ws.Name + "/" + a.Name
	st := s.closes[key]
	switch {
	case st == nil:
		return closeNone
	case a.Live == nil || !a.Live.Running:
		delete(s.closes, key)
		return closeGone
	case st.err != "":
		return closeFailed
	case !settled(a.Live):
		st.busy = true
		return closeWrapping
	case wroteSessionSince(a.Dir, st.at):
		if err := s.closePane(a.Live.PaneID); err != nil {
			st.err = "Close failed: " + err.Error()
			return closeFailed
		}
		delete(s.closes, key)
		return closeDone
	case !st.busy && time.Since(st.at) < 20*time.Second:
		// Settled with no log: give it a moment to start.
		return closeWrapping
	}
	return closeStopped
}

// wroteSessionSince says whether a session log (or a task log) in the
// agent's folder changed after t: the wrap's step 4.
func wroteSessionSince(dir string, t time.Time) bool {
	for _, sub := range []string{"sessions", "tasks"} {
		entries, _ := os.ReadDir(filepath.Join(dir, "memory", sub))
		for _, e := range entries {
			if info, err := e.Info(); err == nil && info.ModTime().After(t) {
				return true
			}
		}
	}
	return false
}

// canSend says whether the page may type into the agent's pane: only its
// own session, running, and not waiting on a prompt in the pane. Otherwise
// the reason, for the page.
func canSend(a *Agent) (bool, string) {
	switch {
	case a.Live == nil || !a.Live.Running:
		return false, a.Name + " is not running."
	case !sessionPane(a.Live):
		return false, a.Name + " is open only as a peer or task pane; those answer their caller, not you."
	case a.Live.Status == "blocked":
		return false, a.Name + " is waiting on a prompt in its pane. Answer it there."
	}
	return true, ""
}

// sessionPane is false for the panes /agents:ask and /agents:delegate open
// ("sigrid-peer", "Sigrid - Task"), which match the agent by label too.
func sessionPane(p *LivePane) bool {
	name, label := strings.ToLower(p.Name), strings.ToLower(strings.TrimSpace(p.Label))
	for _, kind := range []string{"peer", "task"} {
		if strings.HasSuffix(name, "-"+kind) || strings.HasSuffix(label, " - "+kind) {
			return false
		}
	}
	return true
}

// livePrompt submits text to a Herdr pane or a T3 thread.
func livePrompt(pane, text string) error {
	if IsT3Pane(pane) {
		return t3Prompt(pane, text)
	}
	return herdrPrompt(pane, text)
}

// herdrPrompt submits text to a pane with `herdr agent prompt`. Herdr writes
// text and Enter; it cannot always confirm the agent took it, so the page
// shows the message once the transcript has it.
func herdrPrompt(pane, text string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, herdrBin(), "agent", "prompt", pane, text).CombinedOutput()
	if err != nil {
		return fmt.Errorf("herdr agent prompt: %s", herdrError(out, err))
	}
	return nil
}

// agentFile resolves rel inside the agent's folder: a .md file, no way out
// of the folder by .. or a symlink.
func agentFile(dir, rel string) (string, bool) {
	if rel == "" || filepath.IsAbs(rel) || filepath.Ext(rel) != ".md" {
		return "", false
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", false
	}
	base, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", false
	}
	path, err := filepath.EvalSymlinks(filepath.Join(dir, clean))
	if err != nil || !strings.HasPrefix(path, base+string(filepath.Separator)) {
		return "", false
	}
	return path, true
}

func fileLink(ws *Workspace, a *Agent, rel string, all bool) string {
	q := "?p=" + template.URLQueryEscaper(filepath.ToSlash(rel))
	if all {
		q += "&all=1"
	}
	return "/w/" + ws.Name + "/" + a.Name + "/file" + q
}

// makeWebItem adds what the page shows beside the item: the rest of the
// Action cell, a parked note, and a link to its history note if one exists.
func makeWebItem(ws *Workspace, a *Agent, it Item, all bool) webItem {
	id := strings.TrimPrefix(it.ID, "#")
	wi := webItem{
		Item: it, Headline: it.Headline(), Key: a.Name + "/" + id,
		CanAnswer: waitsOnPrincipal(it, ws.Principal), Answers: a.Answers[id],
		AnswerURL: "/w/" + ws.Name + "/" + a.Name + "/answer",
		Agent:     a.Name, AgentLive: a.Live != nil && a.Live.Running,
	}
	wi.Late = it.Overdue(ws.Scanned)
	if ok, _ := canSend(a); ok && wi.CanAnswer {
		wi.Reply, wi.SendURL = true, "/w/"+ws.Name+"/"+a.Name+"/send"
	}
	if detail := strings.TrimSpace(strings.TrimPrefix(cleanMarkdown(it.Action), wi.Headline)); detail != "" && detail != wi.Headline {
		wi.Detail = strings.TrimSpace(strings.TrimLeft(detail, "—–-.: "))
		wi.Desc, wi.Detail = splitDesc(wi.Detail)
	}
	if it.Parked() {
		wi.Note = parkedNote(it.Status)
	}
	if note := itemNote(a.Dir, it.ID); note != "" {
		wi.History = fileLink(ws, a, note, all)
	}
	return wi
}

// descMax is the longest description, in runes, shown under an item's title.
const descMax = 160

// splitDesc takes the first sentence of an item's detail as its description
// (conventions § actions.md: title, then one sentence saying what the item is
// for) and returns the rest. A history pointer is never a description.
func splitDesc(detail string) (desc, rest string) {
	if detail == "" || strings.HasPrefix(detail, "History") {
		return "", detail
	}
	cut := len(detail)
	for from := 0; ; {
		j := strings.Index(detail[from:], ". ")
		if j < 0 {
			break
		}
		j += from
		if strings.Count(detail[:j], "(") <= strings.Count(detail[:j], ")") {
			cut = j + 1
			break
		}
		from = j + 2
	}
	desc, rest = strings.TrimSpace(detail[:cut]), strings.TrimSpace(detail[cut:])
	if len([]rune(desc)) > descMax {
		return "", detail
	}
	return desc, rest
}

// itemNote finds items/<id>-<slug>.md (or items/<id>.md) for an item.
func itemNote(dir, id string) string {
	id = strings.TrimPrefix(id, "#")
	if id == "" {
		return ""
	}
	if fileExists(filepath.Join(dir, "items", id+".md")) {
		return filepath.Join("items", id+".md")
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "items", id+"-*.md")); len(m) > 0 {
		return filepath.Join("items", filepath.Base(m[0]))
	}
	return ""
}

func toneClass(t tone) string {
	return [...]string{"off", "idle", "working", "foryou", "waiting", "blocked"}[t]
}

func liveClass(live string) string {
	switch live {
	case "working", "blocked":
		return "ok"
	case "idle", "running":
		return "accent"
	}
	return "dim"
}

// render writes the page, or only its <main> when the page's own refresh
// asks (X-Partial), so the view updates without a reload.
func (s *webServer) render(w http.ResponseWriter, r *http.Request, p page) {
	name := "page"
	if r.Header.Get("X-Partial") == "1" {
		name = "main"
	}
	var buf bytes.Buffer
	if err := webTemplate.ExecuteTemplate(&buf, name, p); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(buf.Bytes())
}
