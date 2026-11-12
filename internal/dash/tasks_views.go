package dash

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The Tasks view's pages: the overview (home), the flat lists by view, an
// agent's page, and One by one.

// taskViews are the flat lists, in sidebar order.
var taskViews = []struct{ Key, Label string }{
	{"mine", "Waiting on me"},
	{"today", "Today"},
	{"overdue", "Overdue"},
	{"all", "All open"},
	{"parked", "Parked"},
}

type tasksPage struct {
	// Title is the browser tab's; Heading and Sub head the page.
	Title, Heading, Sub string
	// Live is the agent's session on its page: working, idle, prompt, "".
	Live string
	// Theme is the root's data-theme; Here is the page, for the theme
	// switch's links.
	Theme, Here string
	// Tile, on an agent's page, puts its image (or initial) by the heading;
	// Sweep and Chat are the header's buttons there, Total the open count.
	Tile      map[string]string
	SweepHref string
	// View is the agent page's: tasks, chat or both; Views link them and
	// Chat is its session for chat and both.
	View       string
	Views      []taskNavLink
	Chat       *taskChat
	SweepTotal int
	// Crumbs is the breadcrumb above the heading; the last is the page.
	Crumbs []taskNavLink
	Nav    taskNav
	// Select is the phone's menu, the sidebar as one <select>.
	Select []taskOpt
	// Pane: the page has the detail pane; Detail is what it shows.
	Pane     bool
	Detail   *taskDetail
	Overview *taskOverview
	Search   *searchView
	List     *taskList
	Agent    *agentTasks
	Sweep    *sweepView
}

type taskNav struct {
	Views      []taskNavLink
	Workspaces []taskNavLink
	// The app header: the space picker (Spaces, "All" last), the space
	// shown (Space, its label and its ?ws= value), and who is live there.
	Spaces     []taskNavLink
	Space      string
	SpaceLabel string
	Live       []taskNavLink
}

type taskNavLink struct {
	Label, Href string
	// Avatar: an agent link's image, "" for none.
	Avatar, Live string
	Count        int
	Current      bool
	// Agents: a workspace heading's agents.
	Agents []taskNavLink
}

// taskCounts are one agent's numbers, or a total.
type taskCounts struct {
	Waiting, Answered, Overdue, Today, Open, Parked int
}

func (c *taskCounts) add(tr taskRow) {
	switch {
	case tr.Dot == "parked":
		c.Parked++
		return
	case tr.Mine:
		c.Waiting++
	case tr.Answered:
		c.Answered++
	}
	c.Open++
	if tr.Late {
		c.Overdue++
	}
	if tr.Today {
		c.Today++
	}
}

func agentHref(ws, agent string) string {
	return "/tasks/" + url.PathEscape(ws) + "/" + url.PathEscape(agent)
}

func listHref(view string) string { return "/tasks?view=" + view }

// pickWS is the workspace the principal chose: ?ws= first, then the
// cookie. resolveWS turns it into one that exists, "all", or the first.
func pickWS(r *http.Request) string {
	if ws := r.URL.Query().Get("ws"); ws != "" {
		return ws
	}
	if c, err := r.Cookie(wsCookie); err == nil {
		return c.Value
	}
	return ""
}

func resolveWS(wss []*Workspace, pick string) string {
	if pick == "all" && len(wss) > 1 {
		return "all"
	}
	for _, ws := range wss {
		if ws.Name == pick {
			return pick
		}
	}
	if len(wss) > 0 {
		return wss[0].Name
	}
	return ""
}

// rememberWS keeps a workspace named in the URL for the next visit.
func rememberWS(w http.ResponseWriter, r *http.Request, shown string) {
	if shown == "" || r.URL.Query().Get("ws") == "" {
		return
	}
	http.SetCookie(w, &http.Cookie{Name: wsCookie, Value: shown, Path: "/tasks", MaxAge: 365 * 24 * 3600, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

// wsQuery is the ?ws= part of a link for the workspace shown.
func wsQuery(shown string) string {
	if shown == "" {
		return ""
	}
	return "ws=" + url.QueryEscape(shown)
}

// pageNav builds the sidebar and the phone menu for the workspace shown
// ("all" for every one): its views with its counts, then its agents.
// current names the page: "overview", "one", a view key, or an agent's
// href.
func pageNav(wss []*Workspace, agents []*taskAgent, current, shown string) (taskNav, []taskOpt) {
	var total taskCounts
	per := map[*taskAgent]taskCounts{}
	var in []*taskAgent
	for _, ta := range agents {
		if shown != "all" && shown != "" && ta.ws.Name != shown {
			continue
		}
		in = append(in, ta)
		var c taskCounts
		for _, tr := range ta.items("") {
			c.add(tr)
		}
		per[ta] = c
		total.Waiting += c.Waiting
		total.Overdue += c.Overdue
		total.Today += c.Today
		total.Open += c.Open
		total.Parked += c.Parked
	}
	q := wsQuery(shown)
	join := func(path, extra string) string {
		parts := []string{}
		for _, x := range []string{q, extra} {
			if x != "" {
				parts = append(parts, x)
			}
		}
		if len(parts) == 0 {
			return path
		}
		return path + "?" + strings.Join(parts, "&")
	}
	var nav taskNav
	var sel []taskOpt
	add := func(key, label, href string, n int) {
		nav.Views = append(nav.Views, taskNavLink{Label: label, Href: href, Count: n, Current: key == current})
		text := label
		if n > 0 {
			text = fmt.Sprintf("%s (%d)", label, n)
		}
		sel = append(sel, taskOpt{href, text, key == current})
	}
	add("overview", "Overview", join("/tasks", ""), 0)
	add("one", "One by one", join("/tasks/one", ""), total.Waiting)
	counts := map[string]int{"mine": total.Waiting, "today": total.Today, "overdue": total.Overdue, "all": total.Open, "parked": total.Parked}
	for _, v := range taskViews {
		add(v.Key, v.Label, join("/tasks", "view="+v.Key), counts[v.Key])
	}
	for _, ws := range wss {
		wl := taskNavLink{Label: ws.Name}
		for _, ta := range in {
			if ta.ws != ws {
				continue
			}
			href := agentHref(ws.Name, ta.a.Name)
			c := per[ta]
			wl.Agents = append(wl.Agents, taskNavLink{Label: ta.a.Name, Href: href, Count: c.Waiting, Current: href == current, Avatar: ta.avatar, Live: ta.live})
			text := ta.a.Name
			if shown == "all" {
				text = ws.Name + " · " + text
			}
			if c.Waiting > 0 {
				text += fmt.Sprintf(" (%d)", c.Waiting)
			}
			sel = append(sel, taskOpt{href, text, href == current})
		}
		if len(wl.Agents) > 0 {
			nav.Workspaces = append(nav.Workspaces, wl)
		}
	}
	for _, ws := range wss {
		nav.Spaces = append(nav.Spaces, taskNavLink{Label: spaceLabel(ws.Name), Href: "/tasks?ws=" + url.QueryEscape(ws.Name), Current: ws.Name == shown})
	}
	if len(wss) > 1 {
		nav.Spaces = append(nav.Spaces, taskNavLink{Label: "All", Href: "/tasks?ws=all", Current: shown == "all"})
	}
	nav.Space, nav.SpaceLabel = shown, spaceLabel(shown)
	for _, ta := range in {
		if ta.live != "" {
			nav.Live = append(nav.Live, taskNavLink{Label: ta.a.Name, Href: agentHref(ta.ws.Name, ta.a.Name), Live: ta.live, Avatar: ta.avatar})
		}
	}
	return nav, sel
}

// spaceLabel is a workspace's name as the header shows it.
func spaceLabel(ws string) string {
	switch ws {
	case "", "all":
		return "All spaces"
	}
	return strings.ToUpper(ws[:1]) + ws[1:]
}

// spaceCrumb is the breadcrumb's first step: the space's overview.
func spaceCrumb(shown string) taskNavLink {
	href := "/tasks"
	if shown != "" {
		href += "?ws=" + url.QueryEscape(shown)
	}
	return taskNavLink{Label: spaceLabel(shown), Href: href}
}

// The overview: a row (Compact) or a card (Detail) per agent, by workspace.
type taskOverview struct {
	Total      taskCounts
	Workspaces []overviewWS
	// Switch picks the workspace shown, "All" last; One is One by one over
	// what waits on the principal there.
	Switch []taskNavLink
	One    string
	// Lists starts the links to the flat lists, for the workspace shown.
	Lists string
	// Density is "compact" or "detail"; Compact and Detail link the toggle.
	Density         string
	Compact, Detail string
}

// wsCookie remembers the workspace the overview last showed; densityCookie
// remembers Compact or Detail.
const (
	wsCookie      = "tasks_ws"
	densityCookie = "tasks_density"
)

// pickDensity is the overview's density from ?density= (remembered in a
// cookie) or the cookie, "compact" by default.
func pickDensity(w http.ResponseWriter, r *http.Request) string {
	if d := r.URL.Query().Get("density"); d == "compact" || d == "detail" {
		http.SetCookie(w, &http.Cookie{Name: densityCookie, Value: d, Path: "/tasks", MaxAge: 365 * 24 * 3600, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		return d
	}
	if c, err := r.Cookie(densityCookie); err == nil && c.Value == "detail" {
		return "detail"
	}
	return "compact"
}

type overviewWS struct {
	Name   string
	Agents []overviewAgent
	// In Detail: Cards are the agents with something for the principal or
	// overdue, live first, laid out in Cols (two stacks, each card in the
	// shorter one, so the page reads left to right without gaps; on a phone
	// the stacks dissolve and each card's Order keeps the sequence); Rest
	// the others, shown as compact rows below.
	Cards, Rest []overviewAgent
	Cols        [2][]overviewAgent
}

type overviewAgent struct {
	Name, Title, Href string
	Avatar, Live      string
	taskCounts
	// Quiet: the agent has no live session, so the card is dimmed and
	// working and offline agents tell apart at a glance.
	Quiet bool
	// Doing is the headline of its first item in progress; Last the date
	// of its newest session (DD-MM); Chat its chat, LateHref the overdue list.
	Doing, Last, Chat, LateHref string
	// Items are what waits on the principal, then what is overdue, at
	// most four; More counts the rest. Others counts what waits on someone
	// else; Order is the card's place in Detail.
	Items  []overviewItem
	More   int
	Others int
	Order  int
}

type overviewItem struct {
	Title, Href, Due string
	Late, Soon       bool
}

// overviewPage shows one workspace (or all): pick is the ?ws= or cookie
// value, "" for the first workspace. It returns the workspace shown.
func (s *webServer) overviewPage(pick, density string) (tasksPage, string) {
	wss, agents := s.loadTasks("", false)
	p := tasksPage{Title: "Tasks", Heading: "Tasks"}
	shown := resolveWS(wss, pick)
	p.Nav, p.Select = pageNav(wss, agents, "overview", shown)
	ov := &taskOverview{One: "/tasks/one", Lists: "/tasks?view=", Density: density}
	for _, ws := range wss {
		ov.Switch = append(ov.Switch, taskNavLink{Label: ws.Name, Href: "/tasks?ws=" + url.QueryEscape(ws.Name), Current: ws.Name == shown})
	}
	if len(wss) > 1 {
		ov.Switch = append(ov.Switch, taskNavLink{Label: "All", Href: "/tasks?ws=all", Current: shown == "all"})
	} else {
		ov.Switch = nil
	}
	if shown != "all" {
		ov.One += "?ws=" + url.QueryEscape(shown)
		ov.Lists = "/tasks?ws=" + url.QueryEscape(shown) + "&view="
		p.Heading = strings.ToUpper(shown[:1]) + shown[1:]
	}
	toggle := func(d string) string {
		v := url.Values{"density": {d}}
		if shown != "" {
			v.Set("ws", shown)
		}
		return "/tasks?" + v.Encode()
	}
	ov.Compact, ov.Detail = toggle("compact"), toggle("detail")
	for _, ws := range wss {
		if shown != "all" && ws.Name != shown {
			continue
		}
		ow := overviewWS{Name: ws.Name}
		for _, ta := range agents {
			if ta.ws != ws {
				continue
			}
			oa := overviewAgent{Name: ta.a.Name, Title: ta.a.Title, Href: agentHref(ws.Name, ta.a.Name), Avatar: ta.avatar, Live: ta.live, LateHref: ov.Lists + "overdue"}
			oa.Chat = oa.Href + "?view=chat"
			var mine, late []overviewItem
			for _, tr := range ta.items("") {
				oa.add(tr)
				if tr.Dot == "parked" {
					continue
				}
				if oa.Doing == "" && tr.Dot == "progress" {
					oa.Doing = tr.Headline
				}
				if tr.Dot == "waiting" {
					oa.Others++
				}
				it := overviewItem{Title: tr.Headline, Href: oa.Href + "?" + url.Values{"item": {itemKey(tr)}}.Encode(), Due: tr.DueLabel, Late: tr.Late, Soon: tr.Soon}
				switch {
				case tr.Mine:
					mine = append(mine, it)
				case tr.Late:
					late = append(late, it)
				}
			}
			all := append(mine, late...)
			if len(all) > 4 {
				oa.More = len(all) - 4
				all = all[:4]
			}
			oa.Items = all
			if se, ok := ta.a.LastSession(); ok && !se.Date.IsZero() {
				oa.Last = se.Date.Format("02-01")
			}
			oa.Quiet = oa.Live == ""
			ov.Total.Waiting += oa.Waiting
			ov.Total.Answered += oa.Answered
			ov.Total.Overdue += oa.Overdue
			ov.Total.Open += oa.Open
			ow.Agents = append(ow.Agents, oa)
		}
		// What needs the principal leads; the sidebar order holds otherwise.
		sort.SliceStable(ow.Agents, func(i, j int) bool { return !ow.Agents[i].Quiet && ow.Agents[j].Quiet })
		var height [2]int
		for _, oa := range ow.Agents {
			if oa.Waiting == 0 && oa.Overdue == 0 {
				ow.Rest = append(ow.Rest, oa)
				continue
			}
			oa.Order = len(ow.Cards)
			ow.Cards = append(ow.Cards, oa)
			// A card's height in lines, near enough: head, state, counts,
			// and an item each (a long title may wrap, counted once more).
			h := 4 + len(oa.Items)
			if oa.More > 0 {
				h++
			}
			col := 0
			if height[1] < height[0] {
				col = 1
			}
			ow.Cols[col] = append(ow.Cols[col], oa)
			height[col] += h
		}
		if len(ow.Agents) > 0 {
			ov.Workspaces = append(ov.Workspaces, ow)
		}
	}
	p.Overview = ov
	return p, shown
}

// A flat list: one view over every agent, grouped by priority.
type taskList struct {
	Groups []taskGroup
	Empty  string
}

type taskGroup struct {
	Title string
	Key   string
	Rows  []taskRow
	// Closed: the group starts collapsed.
	Closed bool
}

func viewMatch(view string, tr taskRow) bool {
	switch view {
	case "mine":
		return tr.Mine
	case "today":
		return tr.Today
	case "overdue":
		return tr.Late
	case "parked":
		return tr.Dot == "parked"
	}
	return tr.Dot != "parked"
}

func sortTasks(rows []taskRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		if (a.due == nil) != (b.due == nil) {
			return a.due != nil
		}
		if a.due != nil && !a.due.Equal(*b.due) {
			return a.due.Before(*b.due)
		}
		return false
	})
}

// listPage is a flat list over every workspace, or only ws.
func (s *webServer) listPage(r *http.Request, view, pick string) (tasksPage, string) {
	wss, agents := s.loadTasks("", false)
	shown := resolveWS(wss, pick)
	ws := shown
	if ws == "all" {
		ws = ""
	}
	href := listHref(view)
	self := href
	if q := wsQuery(shown); q != "" {
		self += "&" + q
	}
	p := tasksPage{}
	p.Nav, p.Select = pageNav(wss, agents, view, shown)
	for _, v := range taskViews {
		if v.Key == view {
			p.Heading = v.Label
		}
	}
	p.Title = p.Heading
	p.Crumbs = []taskNavLink{spaceCrumb(shown), {Label: p.Heading}}
	var picked []taskRow
	for _, ta := range agents {
		if ws != "" && ta.ws.Name != ws {
			continue
		}
		for _, tr := range ta.items(self) {
			tr.ShowAgent = true
			if viewMatch(view, tr) {
				picked = append(picked, tr)
			}
		}
	}
	sortTasks(picked)
	l := &taskList{}
	byKey := map[string]int{}
	for _, tr := range picked {
		key := tr.Priority
		if key == "" {
			key = "No priority"
		}
		i, ok := byKey[key]
		if !ok {
			i = len(l.Groups)
			byKey[key] = i
			l.Groups = append(l.Groups, taskGroup{Title: key, Key: key})
		}
		l.Groups[i].Rows = append(l.Groups[i].Rows, tr)
	}
	sort.SliceStable(l.Groups, func(i, j int) bool { return priorityRank(l.Groups[i].Key) < priorityRank(l.Groups[j].Key) })
	p.Sub = plural(len(picked), "item")
	if ws != "" {
		p.Sub += " in " + ws
	}
	switch view {
	case "mine":
		l.Empty = "Nothing is waiting on you."
	case "overdue":
		l.Empty = "Nothing is overdue."
	case "today":
		l.Empty = "Nothing is due today."
	default:
		l.Empty = "Nothing here."
	}
	s.attachDetail(&p, r, agents, l.Groups)
	p.List = l
	return p, shown
}

// tasks serves the home: the overview, or a flat list with ?view=.
func (s *webServer) tasks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if v := q.Get("go"); strings.HasPrefix(v, "/tasks") && !strings.HasPrefix(v, "//") {
		// The phone menu without script: follow the picked link.
		http.Redirect(w, r, v, http.StatusSeeOther)
		return
	}
	// The first version's agent filter now has a page of its own.
	if ws, agent := q.Get("ws"), q.Get("agent"); ws != "" && agent != "" {
		http.Redirect(w, r, agentHref(ws, agent), http.StatusSeeOther)
		return
	}
	view := q.Get("view")
	for _, v := range taskViews {
		if v.Key == view {
			p, shown := s.listPage(r, view, pickWS(r))
			rememberWS(w, r, shown)
			s.renderTasks(w, r, p)
			return
		}
	}
	p, shown := s.overviewPage(pickWS(r), pickDensity(w, r))
	if shown != "" {
		http.SetCookie(w, &http.Cookie{Name: wsCookie, Value: shown, Path: "/tasks", MaxAge: 365 * 24 * 3600, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	}
	s.renderTasks(w, r, p)
}

// An agent's page: its open items by state.
type agentTasks struct {
	WS, Name, Title, Next string
	Counts                taskCounts
	// Total is every open item, parked too: what One by one walks.
	Total  int
	Groups []taskGroup
	// Sweep starts One by one over all its open items.
	Sweep  string
	Action *agentAction
}

// agentGroups are the agent page's groups, in order, by row dot.
var agentGroups = []struct {
	Key, Title string
	Dots       []string
}{
	{"you", "Waiting on you", []string{"me", "answered"}},
	{"progress", "In progress", []string{"progress", "free"}},
	{"others", "Waiting on others", []string{"waiting", "blocked"}},
	{"todo", "Not started", []string{"todo"}},
	{"parked", "Parked", []string{"parked"}},
}

func (s *webServer) agentTasks(w http.ResponseWriter, r *http.Request) {
	wsName, name := r.PathValue("ws"), r.PathValue("agent")
	wss, agents := s.loadTasks("", false)
	ta := findTaskAgent(agents, wsName, name)
	if ta == nil {
		http.NotFound(w, r)
		return
	}
	href := agentHref(wsName, name)
	p := tasksPage{Title: name, Heading: name, Sub: ta.a.Title}
	// The sidebar shows the agent's own workspace unless All was chosen.
	shown := resolveWS(wss, pickWS(r))
	if shown != "all" {
		shown = wsName
	}
	p.Nav, p.Select = pageNav(wss, agents, href, shown)
	p.Crumbs = []taskNavLink{spaceCrumb(wsName), {Label: name}}
	p.Tile = map[string]string{"Name": name, "Avatar": ta.avatar, "Class": "lg", "Live": "off"}
	if ta.live != "" {
		p.Tile["Live"] = ta.live
	}
	p.Live = ta.live
	at := &agentTasks{WS: wsName, Name: name, Title: ta.a.Title, Next: ta.a.NextSession,
		Sweep: "/tasks/one?" + url.Values{"ws": {wsName}, "agent": {name}}.Encode()}
	rows := ta.items(href)
	sortTasks(rows)
	for _, g := range agentGroups {
		tg := taskGroup{Title: g.Title, Key: g.Key, Closed: g.Key == "parked"}
		for _, tr := range rows {
			for _, d := range g.Dots {
				if tr.Dot == d {
					tg.Rows = append(tg.Rows, tr)
				}
			}
		}
		if len(tg.Rows) > 0 {
			at.Groups = append(at.Groups, tg)
		}
	}
	for _, tr := range rows {
		at.Counts.add(tr)
	}
	at.Total = len(rows)
	s.attachDetail(&p, r, agents, at.Groups)
	p.SweepHref, p.SweepTotal = at.Sweep, at.Total

	// The page's view: the tasks, the chat, or both side by side.
	here := r.URL.RequestURI()
	p.View = r.URL.Query().Get("view")
	if p.View != "chat" && p.View != "both" {
		p.View = "tasks"
	}
	for _, v := range []struct{ key, label string }{{"tasks", "Tasks"}, {"chat", "Chat"}, {"both", "Both"}} {
		param := v.key
		if param == "tasks" {
			param = ""
		}
		p.Views = append(p.Views, taskNavLink{Label: v.label, Href: withParams(here, "view", param, "item", ""), Current: v.key == p.View, Live: v.key})
	}
	if p.View != "tasks" {
		p.Chat = s.paneChat(wsName, name, here)
		// The chat read the session fresh; the header follows it.
		p.Live, p.Tile["Live"] = p.Chat.State, "off"
		if p.Live != "" {
			p.Tile["Live"] = p.Live
		}
	}
	if ta.a.pendingAnswers() > 0 {
		// Only now ask Herdr and T3 whether the agent is running.
		_, live := s.loadTasks(wsName, true)
		if lt := findTaskAgent(live, wsName, name); lt != nil {
			at.Action = s.agentAction(lt, href)
		}
	}
	p.Agent = at
	s.renderTasks(w, r, p)
}

// agentAction is the one button that gets the principal's answers to an
// agent: tell it, when its session is running, or start it.
type agentAction struct {
	Agent, State string
	// Class colours the state dot: on, busy or off.
	Class   string
	Pending int
	// Tell posts Text to SendURL; Start posts to StartURL. Note says why
	// neither is offered.
	Tell             bool
	SendURL, Text    string
	Start            bool
	StartURL, Starts string
	Note             string
	// Next is the page to reload after the button.
	Next string
}

func (s *webServer) agentAction(ta *taskAgent, next string) *agentAction {
	ws, a := ta.ws, ta.a
	n := a.pendingAnswers()
	if n == 0 {
		return nil
	}
	act := &agentAction{Agent: a.Name, Pending: n, Next: next, Class: "off"}
	defer func() {
		switch act.State {
		case "running", "working":
			act.Class = "on"
		case "starting":
			act.Class = "busy"
		}
	}()
	base := "/w/" + url.PathEscape(ws.Name) + "/" + url.PathEscape(a.Name)
	running := a.Live != nil && a.Live.Running
	if ok, why := canSend(a); ok {
		act.Tell, act.SendURL = true, base+"/send"
		act.Text = fmt.Sprintf("You have %s in your inbox; apply them.", plural(n, "new answer"))
		if n == 1 {
			act.Text = "You have 1 new answer in your inbox; apply it."
		}
		act.State = "running"
		if a.Live.Status == "working" {
			act.State = "working"
		}
		return act
	} else if running {
		act.State, act.Note = "running", why
		return act
	}
	if !ws.Live {
		act.State, act.Note = "unknown", "Could not ask Herdr or T3 whether "+a.Name+" is running."
		return act
	}
	s.startMu.Lock()
	st := s.launches[ws.Name+"/"+a.Name]
	starting := st != nil && st.err == "" && time.Since(st.at) < launchGiveUp
	failed := ""
	if st != nil && st.err != "" && time.Since(st.at) < launchGiveUp {
		failed = st.err
	}
	s.startMu.Unlock()
	if starting {
		act.State = "starting"
		return act
	}
	act.State = "not running"
	wa := &webAgent{}
	stopped(ws, a, wa)
	act.Start, act.StartURL, act.Starts, act.Note = wa.CanStart, wa.StartURL, wa.StartsIn, wa.SendNote
	if failed != "" {
		act.Note = failed
	}
	return act
}

// One by one. A run is fixed when it starts: the items' order is put in
// the URL (run), so answering or changing one does not reshuffle the rest.
// done records what this run answered (a) and changed (e), by position.
type sweepView struct {
	Scope      string
	Pos, Total int
	// Percent fills the progress bar.
	Percent int
	Row     *taskRow
	// Gone: the item is no longer open.
	Gone           string
	BackHref, Next string
	// AnswerNext and EditNext are where the page goes after an answer or a
	// change: the next item, with the run's record updated.
	AnswerNext, EditNext string
	// DoneNext, MergeNext and SkipNext follow Done, Merge and Skip;
	// Hash is the tracker's, for Done.
	DoneNext, MergeNext, SkipNext string
	Hash                          string
	// Undo reverses the last action of the run (UndoID), back to its card.
	UndoID, UndoNext string
	// Others are the agents of the same space, for "Hand to"; Merge the
	// agent's other open items.
	Others []string
	Merge  []taskOpt
	// Prep asks a per-agent run's agent for a recommendation on every item.
	Prep *sweepPrep
	// Outcomes is the end's list: each item and what happened to it.
	Outcomes []sweepLine
	// The end of the run.
	End      bool
	Answered []sweepLine
	Changed  []sweepLine
	Actions  []*agentAction
	Again    string
	// Empty: nothing to walk.
	Empty string
}

type sweepLine struct {
	Agent, Title, Href string
	What               string
}

// sweepPrep is the banner that gets the agent to write the run's missing
// recommendations.
type sweepPrep struct {
	Agent      string
	Have, Of   int
	Prepping   bool
	CanSend    bool
	SendURL    string
	CanStart   bool
	StartURL   string
	Live       string
	Next, Text string
}

// prepText is what Prep sends the agent.
const prepText = "Prep for a one-by-one sweep: for every open item in your actions.md that has no `Recommend:`, add to its Status cell `Recommend: <answer, one reason>.` Keep the existing state word first. Edit cells only, change nothing else, then say done."

type sweepRef struct {
	WS, Agent, ID string
}

func (r sweepRef) token() string { return r.WS + ":" + r.Agent + ":" + r.ID }

func parseRun(run string) []sweepRef {
	var out []sweepRef
	for _, tok := range strings.Split(run, ",") {
		parts := strings.SplitN(tok, ":", 3)
		if len(parts) == 3 && parts[2] != "" {
			out = append(out, sweepRef{parts[0], parts[1], parts[2]})
		}
	}
	return out
}

func sweepURL(ws, agent, run string, n int, done string) string {
	q := url.Values{}
	if ws != "" {
		q.Set("ws", ws)
	}
	if agent != "" {
		q.Set("agent", agent)
	}
	q.Set("run", run)
	q.Set("n", strconv.Itoa(n))
	if done != "" {
		q.Set("done", done)
	}
	return "/tasks/one?" + q.Encode()
}

// sweepRank orders an agent's run: what waits on the principal, then the
// overdue, then the rest by priority, the parked last.
func sweepRank(tr taskRow) int {
	switch {
	case tr.Mine:
		return 0
	case tr.Late:
		return 1
	case tr.Dot == "parked":
		return 3
	}
	return 2
}

func sweepOrder(rows []taskRow) {
	sortTasks(rows)
	sort.SliceStable(rows, func(i, j int) bool { return sweepRank(rows[i]) < sweepRank(rows[j]) })
}

func (s *webServer) tasksOne(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	wsName, name := q.Get("ws"), q.Get("agent")
	scope := "Waiting on you"
	if name != "" {
		scope = name
	}
	run := q.Get("run")
	if run == "" {
		// Start a run: fix the order now.
		_, agents := s.loadTasks(wsName, false)
		var rows []taskRow
		for _, ta := range agents {
			if name != "" && (ta.ws.Name != wsName || ta.a.Name != name) {
				continue
			}
			for _, tr := range ta.items("") {
				if name != "" || tr.Mine {
					rows = append(rows, tr)
				}
			}
		}
		if name != "" {
			sweepOrder(rows)
		} else {
			sortTasks(rows)
		}
		var toks []string
		for _, tr := range rows {
			toks = append(toks, sweepRef{tr.WS, tr.Agent, strings.TrimPrefix(tr.ID, "#")}.token())
		}
		if len(toks) > 0 {
			http.Redirect(w, r, sweepURL(wsName, name, strings.Join(toks, ","), 0, ""), http.StatusSeeOther)
			return
		}
	}
	refs := parseRun(run)
	n, _ := strconv.Atoi(q.Get("n"))
	n = max(0, min(n, len(refs)))
	done := q.Get("done")

	wss, all := s.loadTasks("", false)
	p := tasksPage{Title: "One by one", Heading: "One by one", Sub: scope}
	cur := "one"
	if name != "" {
		cur = agentHref(wsName, name)
	}
	pick := wsName
	if pick == "" {
		pick = pickWS(r)
	}
	shown := resolveWS(wss, pick)
	p.Nav, p.Select = pageNav(wss, all, cur, shown)
	p.Crumbs = []taskNavLink{spaceCrumb(shown)}
	if name != "" {
		p.Crumbs = append(p.Crumbs, taskNavLink{Label: name, Href: agentHref(wsName, name)})
	}
	p.Crumbs = append(p.Crumbs, taskNavLink{Label: "One by one"})
	sv := &sweepView{Scope: scope, Total: len(refs)}
	p.Sweep = sv
	if len(refs) == 0 {
		sv.Empty = "Nothing is waiting on you."
		if name != "" {
			sv.Empty = name + " has nothing open."
		}
		s.renderTasks(w, r, p)
		return
	}
	prep := q.Get("prep") == "1"
	link := func(n int, done string) string {
		u := sweepURL(wsName, name, run, n, done)
		if prep {
			u = withParams(u, "prep", "1")
		}
		return u
	}
	here := link(n, done)
	if n > 0 {
		sv.BackHref = link(n-1, done)
		if id := q.Get("undo"); id != "" {
			toks := strings.Split(done, ",")
			sv.UndoID, sv.UndoNext = id, link(n-1, strings.Join(toks[:len(toks)-1], ","))
		}
	}
	if name != "" {
		sv.Prep = s.sweepPrep(all, refs, wsName, name, prep, withParams(here, "prep", "1"))
	}
	if n >= len(refs) {
		s.sweepEnd(sv, refs, done, here)
		sv.Prep = nil
		sv.Again = "/tasks/one"
		if name != "" {
			sv.Again += "?" + url.Values{"ws": {wsName}, "agent": {name}}.Encode()
		}
		s.renderTasks(w, r, p)
		return
	}
	ref := refs[n]
	sv.Pos = n + 1
	sv.Percent = n * 100 / len(refs)
	sv.Next = link(n+1, done)
	mark := func(kind string) string {
		return link(n+1, strings.TrimPrefix(done+","+kind+strconv.Itoa(n), ","))
	}
	sv.AnswerNext, sv.EditNext, sv.DoneNext, sv.MergeNext, sv.SkipNext = mark("a"), mark("e"), mark("d"), mark("m"), mark("k")
	ta := findTaskAgent(all, ref.WS, ref.Agent)
	if ta != nil {
		for _, it := range ta.a.Open {
			if strings.TrimPrefix(it.ID, "#") == ref.ID {
				tr := ta.row(it, sv.EditNext)
				sv.Row = &tr
			}
		}
		sv.Hash = ta.hash
		for _, o := range all {
			if o.ws == ta.ws && o != ta {
				sv.Others = append(sv.Others, o.a.Name)
			}
		}
		for _, it := range ta.a.Open {
			if id := strings.TrimPrefix(it.ID, "#"); id != ref.ID && !it.Parked() {
				sv.Merge = append(sv.Merge, taskOpt{Value: "Merge #" + ref.ID + " into #" + id + ".", Label: "#" + id + " " + it.Headline()})
			}
		}
	}
	if sv.Row == nil {
		sv.Gone = fmt.Sprintf("%s #%s is no longer open.", ref.Agent, ref.ID)
	}
	s.renderTasks(w, r, p)
}

// sweepEnd fills the end of a run: what it answered and changed, and for
// each agent with answers waiting, the button that gets them applied.
func (s *webServer) sweepEnd(sv *sweepView, refs []sweepRef, done, here string) {
	sv.End = true
	seen := map[string]bool{}
	type entry struct {
		kind byte
		ref  sweepRef
	}
	var entries []entry
	for _, d := range strings.Split(done, ",") {
		if len(d) < 2 || seen[d] {
			continue
		}
		seen[d] = true
		i, err := strconv.Atoi(d[1:])
		if err != nil || i < 0 || i >= len(refs) {
			continue
		}
		entries = append(entries, entry{d[0], refs[i]})
	}
	byWS := map[string][]*taskAgent{}
	for _, ref := range refs {
		if _, ok := byWS[ref.WS]; !ok {
			_, byWS[ref.WS] = s.loadTasks(ref.WS, true)
		}
	}
	title := func(ref sweepRef) string {
		if ta := findTaskAgent(byWS[ref.WS], ref.WS, ref.Agent); ta != nil {
			for _, it := range ta.a.Open {
				if strings.TrimPrefix(it.ID, "#") == ref.ID {
					return it.Headline()
				}
			}
		}
		return "#" + ref.ID
	}
	what := map[int]string{}
	for _, d := range strings.Split(done, ",") {
		if len(d) < 2 {
			continue
		}
		i, err := strconv.Atoi(d[1:])
		if err != nil || i < 0 || i >= len(refs) {
			continue
		}
		what[i] = map[byte]string{'a': "answered", 'e': "edited", 'd': "done", 'm': "merged", 'k': "skipped"}[d[0]]
	}
	for i, ref := range refs {
		w := what[i]
		if w == "" {
			w = "skipped"
		}
		sv.Outcomes = append(sv.Outcomes, sweepLine{Agent: ref.Agent, Title: title(ref), What: w,
			Href: agentHref(ref.WS, ref.Agent) + "?" + url.Values{"item": {ref.WS + "/" + ref.Agent + "/" + ref.ID}}.Encode()})
	}
	for _, e := range entries {
		line := sweepLine{Agent: e.ref.Agent, Title: title(e.ref), Href: agentHref(e.ref.WS, e.ref.Agent)}
		switch e.kind {
		case 'a', 'm':
			sv.Answered = append(sv.Answered, line)
		case 'e', 'd':
			sv.Changed = append(sv.Changed, line)
		}
	}
	shown := map[string]bool{}
	for _, ref := range refs {
		key := ref.WS + "/" + ref.Agent
		if shown[key] {
			continue
		}
		shown[key] = true
		if ta := findTaskAgent(byWS[ref.WS], ref.WS, ref.Agent); ta != nil {
			if act := s.agentAction(ta, here); act != nil {
				sv.Actions = append(sv.Actions, act)
			}
		}
	}
}

// sweepPrep counts the run's items that carry a recommendation and offers
// Prep when fewer than four in five do, or while a prep is under way.
func (s *webServer) sweepPrep(agents []*taskAgent, refs []sweepRef, ws, name string, prepping bool, next string) *sweepPrep {
	ta := findTaskAgent(agents, ws, name)
	if ta == nil {
		return nil
	}
	p := &sweepPrep{Agent: name, Of: len(refs), Prepping: prepping, Live: ta.live, Next: next, Text: prepText}
	open := map[string]taskRow{}
	for _, tr := range ta.items("") {
		open[strings.TrimPrefix(tr.ID, "#")] = tr
	}
	for _, ref := range refs {
		if tr, ok := open[ref.ID]; ok && tr.Rec != "" {
			p.Have++
		}
	}
	if !prepping && p.Have*5 >= p.Of*4 {
		return nil
	}
	if prepping && p.Have == p.Of {
		return nil
	}
	base := "/w/" + url.PathEscape(ws) + "/" + url.PathEscape(name)
	if ok, _ := canSend(ta.a); ok {
		p.CanSend, p.SendURL = true, base+"/send"
	} else if ta.live == "" {
		p.CanStart, p.StartURL = true, base+"/start"
	}
	return p
}
