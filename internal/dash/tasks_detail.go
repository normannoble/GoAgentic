package dash

import (
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The detail pane: on a desktop a list page shows the selected item beside
// the list (?item=<ws>/<agent>/<n>, so it can be linked and needs no
// script). On a phone the same pane is a full-screen sheet; rows still
// open in place. An agent's chat (paneChat) lives in the page itself, in
// its Chat and Both views.

type taskDetail struct {
	Row *taskRow
	// Since is the row's Since cell.
	Since string
	// Close is the page without the pane.
	Close string
}

// taskChat is the agent's running session, as its dash page shows it.
type taskChat struct {
	Agent   string
	Live    bool
	Entries []webChat
	Note    string
	// SendURL takes a message (the dash's send route); Next reloads.
	CanSend       bool
	SendURL, Next string
	// Start offers the dash's start route when it is not running.
	Start            bool
	StartURL, Starts string
	Starting         bool
	// State is the session's: working, idle, prompt, or "" when off.
	State string
}

// maxPaneChat is how much of the session the pane shows.
const maxPaneChat = 40

// withParams returns u with the given query values set ("" removes one).
func withParams(u string, kv ...string) string {
	p, err := url.Parse(u)
	if err != nil {
		return u
	}
	q := p.Query()
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] == "" {
			q.Del(kv[i])
		} else {
			q.Set(kv[i], kv[i+1])
		}
	}
	p.RawQuery = q.Encode()
	return p.String()
}

func itemKey(tr taskRow) string {
	return tr.WS + "/" + tr.Agent + "/" + strings.TrimPrefix(tr.ID, "#")
}

// linkRows points every row at the pane and marks the selected one.
func linkRows(groups []taskGroup, here, item string) {
	for gi := range groups {
		for i := range groups[gi].Rows {
			tr := &groups[gi].Rows[i]
			key := itemKey(*tr)
			// The pane keeps its mode: a task picked in Chat stays in Chat.
			tr.Href = withParams(here, "item", key)
			tr.Selected = key == item
		}
	}
}

// attachDetail fills the pane from ?item= and ?tab=. agent is the agent
// whose page this is ("" on a list).
func (s *webServer) attachDetail(p *tasksPage, r *http.Request, agents []*taskAgent, groups []taskGroup) {
	here := r.URL.RequestURI()
	item := r.URL.Query().Get("item")
	linkRows(groups, here, item)
	p.Pane = true
	d := &taskDetail{Close: withParams(here, "item", "")}
	p.Detail = d
	parts := strings.SplitN(item, "/", 3)
	if len(parts) != 3 {
		return
	}
	ta := findTaskAgent(agents, parts[0], parts[1])
	if ta == nil {
		return
	}
	for _, it := range ta.a.Open {
		if strings.TrimPrefix(it.ID, "#") != parts[2] {
			continue
		}
		tr := ta.row(it, here)
		tr.ShowAgent = true
		d.Row = &tr
		if raw, ok := ta.rows[parts[2]]; ok {
			d.Since = cleanMarkdown(raw.get("since"))
		}
		return
	}
}

// paneChat reads the agent's running session the way its dash page does
// (readChat, canSend, stopped), asking Herdr and T3 who is live.
func (s *webServer) paneChat(wsName, agent, here string) *taskChat {
	_, live := s.loadTasks(wsName, true)
	ta := findTaskAgent(live, wsName, agent)
	if ta == nil {
		return &taskChat{Agent: agent, Note: "Could not find " + agent + "."}
	}
	ws, a := ta.ws, ta.a
	c := &taskChat{Agent: a.Name, Next: here}
	wa := &webAgent{Name: a.Name}
	wa.ChatLive = a.Live != nil && a.Live.Running
	c.Live = wa.ChatLive
	c.State = liveState(a)
	if wa.CanSend, wa.SendNote = canSend(a); wa.CanSend {
		c.CanSend, c.SendURL = true, "/w/"+url.PathEscape(ws.Name)+"/"+url.PathEscape(a.Name)+"/send"
	}
	if !wa.ChatLive {
		if !ws.Live {
			c.Note = "Could not ask Herdr or T3 whether " + a.Name + " is running."
			return c
		}
		s.startMu.Lock()
		st := s.launches[ws.Name+"/"+a.Name]
		c.Starting = st != nil && st.err == "" && time.Since(st.at) < launchGiveUp
		s.startMu.Unlock()
		if !c.Starting {
			stopped(ws, a, wa)
			c.Start, c.StartURL, c.Starts, c.Note = wa.CanStart, wa.StartURL, wa.StartsIn, wa.SendNote
		}
		return c
	}
	s.readChat(ws, a, wa)
	if wa.ChatNote != "" {
		c.Note = wa.ChatNote
	} else if !wa.CanSend {
		c.Note = wa.SendNote
	}
	// Real blocks for the agent's words, images where allowed, T3's
	// attachments as pictures, and each run of tool steps as one line.
	link := s.imageLinker(ws, a)
	var chat []webChat
	for _, e := range wa.Chat {
		switch e.Kind {
		case "agent":
			e.HTML = paneMarkdown(e.Text, link)
		case "you":
			text, links := s.t3Images(e.Text, link)
			e.Text, e.Images = text, append(e.Images, links...)
		case "tools":
			if n := len(chat); n > 0 && chat[n-1].Kind == "tools" {
				chat[n-1].Tools = append(chat[n-1].Tools, e.Tools...)
				chat[n-1].Summary = plural(len(chat[n-1].Tools), "step")
				continue
			}
			e.Tools = append([]string(nil), e.Tools...)
			e.Summary = plural(len(e.Tools), "step")
		}
		chat = append(chat, e)
	}
	wa.Chat = chat
	if len(wa.Chat) > maxPaneChat {
		wa.Chat = wa.Chat[len(wa.Chat)-maxPaneChat:]
	}
	c.Entries = wa.Chat
	return c
}
