package dash

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// Search: the header's box finds open items (title, description, detail)
// and agents (name, title) in the space shown, or everywhere for All.
// GET /tasks/search?q= is a page of results; with format=json it feeds the
// box's live drop-down.

type searchHit struct {
	Kind   string `json:"kind"` // "item" or "agent"
	Title  string `json:"title"`
	Sub    string `json:"sub"`
	Href   string `json:"href"`
	Agent  string `json:"agent"`
	WS     string `json:"ws"`
	Avatar string `json:"avatar,omitempty"`
	Dot    string `json:"dot,omitempty"`
	score  int
}

const maxHits = 30

func searchTasks(agents []*taskAgent, shown, q string) []searchHit {
	terms := strings.Fields(strings.ToLower(q))
	if len(terms) == 0 {
		return nil
	}
	match := func(fields ...string) (int, bool) {
		text := strings.ToLower(strings.Join(fields, " \x00 "))
		score := 0
		for _, t := range terms {
			i := strings.Index(text, t)
			if i < 0 {
				return 0, false
			}
			// A hit in the first field (the title or name) ranks higher.
			if j := strings.Index(text, "\x00"); j < 0 || i < j {
				score += 2
			} else {
				score++
			}
		}
		return score, true
	}
	var hits []searchHit
	for _, ta := range agents {
		if shown != "all" && shown != "" && ta.ws.Name != shown {
			continue
		}
		if sc, ok := match(ta.a.Name, ta.a.Title); ok {
			hits = append(hits, searchHit{Kind: "agent", Title: ta.a.Name, Sub: ta.a.Title, Href: agentHref(ta.ws.Name, ta.a.Name),
				Agent: ta.a.Name, WS: ta.ws.Name, Avatar: ta.avatar, score: sc + 2})
		}
		for _, tr := range ta.items("") {
			sc, ok := match(tr.Headline, tr.Desc, tr.Detail, tr.Note, ta.a.Name)
			if !ok {
				continue
			}
			sub := tr.Desc
			if sub == "" {
				sub = tr.StateText
			}
			hits = append(hits, searchHit{Kind: "item", Title: tr.Headline, Sub: sub, Agent: ta.a.Name, WS: ta.ws.Name, Dot: tr.Dot,
				Href: agentHref(ta.ws.Name, ta.a.Name) + "?" + url.Values{"item": {itemKey(tr)}}.Encode(), score: sc})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if len(hits) > maxHits {
		hits = hits[:maxHits]
	}
	return hits
}

type searchView struct {
	Q    string
	Hits []searchHit
}

func (s *webServer) tasksSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	wss, agents := s.loadTasks("", false)
	shown := resolveWS(wss, pickWS(r))
	hits := searchTasks(agents, shown, q)
	if r.URL.Query().Get("format") == "json" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if hits == nil {
			hits = []searchHit{}
		}
		_ = json.NewEncoder(w).Encode(hits)
		return
	}
	p := tasksPage{Title: "Search", Heading: "Search"}
	p.Nav, p.Select = pageNav(wss, agents, "search", shown)
	p.Crumbs = []taskNavLink{spaceCrumb(shown), {Label: "Search"}}
	p.Search = &searchView{Q: q, Hits: hits}
	if q != "" {
		p.Sub = plural(len(hits), "result") + " for “" + q + "”"
	}
	s.renderTasks(w, r, p)
}
