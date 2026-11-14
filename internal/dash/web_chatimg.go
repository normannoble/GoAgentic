package dash

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Images in the chat pane: what the principal attached (dash uploads, T3
// attachments) and what an agent shows with ![alt](/abs/path.png). They are
// served through one read-only route, /img?ws=&agent=&p=<absolute path>,
// that takes only an image file no bigger than maxImageBytes whose real
// path lies in an allowed folder. Anything else stays plain text in the chat.

var chatImageExt = map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp"}

// imageRoots are the folders /img may serve from: the dash's uploads and
// T3 Code's attachments (or WebSource's own), plus scratch, the scratch
// folder of the one session whose chat shows the image, if any. Never all
// of /tmp/claude-<uid>: that is every session's scratch, downloads too.
func (s *webServer) imageRoots(scratch string) []string {
	roots := s.src.ImageRoots
	if roots == nil {
		roots = []string{s.uploadDir()}
		if home, err := os.UserHomeDir(); err == nil {
			roots = append(roots, filepath.Join(home, ".t3", "userdata", "attachments"))
		}
	}
	if scratch != "" {
		roots = append(append([]string(nil), roots...), scratch)
	}
	return roots
}

// sessionIDRe is a Claude Code session id as it may appear in a path: no
// separators, never "." or "..".
var sessionIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// sessionScratch is the scratch folder of the agent's live Claude Code
// session, <base>/<project dir>/<session id>, or "" when it has none.
func (s *webServer) sessionScratch(ws *Workspace, a *Agent) string {
	if a.Live == nil || !sessionIDRe.MatchString(a.Live.ClaudeSession()) {
		return ""
	}
	path, ok, _ := s.chatPath(ws, a)
	if !ok || path == "" {
		return ""
	}
	base := s.src.ScratchDir
	if base == "" {
		base = fmt.Sprintf("/tmp/claude-%d", os.Getuid())
	}
	return filepath.Join(base, filepath.Base(filepath.Dir(path)), a.Live.ClaudeSession())
}

// chatImagePath checks p and returns its real path and content type: an
// absolute path with an image extension, a regular file no bigger than
// maxImageBytes, whose real path (links resolved) is inside a real root.
func (s *webServer) chatImagePath(p, scratch string) (string, string, bool) {
	if p == "" || !filepath.IsAbs(p) || strings.ContainsRune(p, 0) {
		return "", "", false
	}
	ctype, ok := chatImageExt[strings.ToLower(filepath.Ext(p))]
	if !ok {
		return "", "", false
	}
	real, err := filepath.EvalSymlinks(filepath.Clean(p))
	if err != nil {
		return "", "", false
	}
	if _, ok := chatImageExt[strings.ToLower(filepath.Ext(real))]; !ok {
		return "", "", false
	}
	inside := false
	for _, root := range s.imageRoots(scratch) {
		rr, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		if rel, err := filepath.Rel(rr, real); err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
			inside = true
			break
		}
	}
	if !inside {
		return "", "", false
	}
	info, err := os.Lstat(real)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > maxImageBytes {
		return "", "", false
	}
	return real, ctype, true
}

// imageLinker returns the page link for an allowed image in a's chat, ""
// otherwise. The link names the agent, so /img can allow its session's
// scratch folder and no other.
func (s *webServer) imageLinker(ws *Workspace, a *Agent) func(string) string {
	scratch := s.sessionScratch(ws, a)
	return func(p string) string {
		if _, _, ok := s.chatImagePath(p, scratch); !ok {
			return ""
		}
		return "/img?" + url.Values{"ws": {ws.Name}, "agent": {a.Name}, "p": {p}}.Encode()
	}
}

// chatImage serves one allowed image.
func (s *webServer) chatImage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	real, ctype, ok := s.chatImagePath(q.Get("p"), "")
	if !ok && q.Get("ws") != "" && q.Get("agent") != "" {
		// Not in the fixed folders: look up the agent's live session (a
		// full scan, so only for these) and allow its scratch folder.
		_, agents := s.loadTasks(q.Get("ws"), true)
		if ta := findTaskAgent(agents, q.Get("ws"), q.Get("agent")); ta != nil {
			if scratch := s.sessionScratch(ta.ws, ta.a); scratch != "" {
				real, ctype, ok = s.chatImagePath(q.Get("p"), scratch)
			}
		}
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(real)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxImageBytes+1))
	if err != nil || len(data) > maxImageBytes || !strings.HasPrefix(http.DetectContentType(data), "image/") {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", ctype)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	h.Set("Cache-Control", "private, max-age=86400")
	_, _ = w.Write(data)
}

// t3AttachRe is how T3 Code records an image sent with a message; its
// context block and inline image markers are bookkeeping for the agent.
var (
	t3AttachRe  = regexp.MustCompile(`\[Attached image "[^"\n]*" is saved at: ([^\]\n]+)\]`)
	t3ContextRe = regexp.MustCompile(`(?s)\s*<t3_context\b[^>]*>.*?</t3_context>\s*`)
	t3MarkerRe  = regexp.MustCompile(`\s?\[Image: [^;\]\n]*; ref=[\w-]+\]`)
)

// t3Images takes T3's attachment notes out of a message and returns the
// rest and the allowed images' links. A note naming a file that is not
// allowed stays in the text.
func (s *webServer) t3Images(text string, link func(string) string) (string, []string) {
	var links []string
	out := t3AttachRe.ReplaceAllStringFunc(text, func(m string) string {
		p := strings.TrimSpace(t3AttachRe.FindStringSubmatch(m)[1])
		if u := link(p); u != "" {
			links = append(links, u)
			return ""
		}
		return m
	})
	out = t3ContextRe.ReplaceAllString(out, "\n")
	out = t3MarkerRe.ReplaceAllString(out, "")
	return strings.TrimSpace(out), links
}
