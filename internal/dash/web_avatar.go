package dash

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
)

// Agent images: an agent may keep avatar.svg, .png, .webp or .jpg in its
// folder, and the dashboard shows it in the agent's tile. Nothing needs
// one; without it the tile is the coloured initial. The route serves only
// that one file of a known agent: the name never comes from the request.

var avatarNames = []struct{ name, ctype string }{
	{"avatar.svg", "image/svg+xml"},
	{"avatar.png", "image/png"},
	{"avatar.webp", "image/webp"},
	{"avatar.jpg", "image/jpeg"},
}

// maxAvatar bounds an avatar file; a bigger one is ignored.
const maxAvatar = 512 << 10

// avatarFile returns the agent's avatar (path, content type and info), the
// first of avatarNames that is a regular file no bigger than maxAvatar.
func avatarFile(dir string) (string, string, os.FileInfo) {
	for _, n := range avatarNames {
		path := filepath.Join(dir, n.name)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > maxAvatar {
			continue
		}
		return path, n.ctype, info
	}
	return "", "", nil
}

// avatarURL is the link to an agent's avatar, "" when it has none. The
// file's time is in the link, so a new image is fetched.
func avatarURL(ws, agent, dir string) string {
	_, _, info := avatarFile(dir)
	if info == nil {
		return ""
	}
	return "/w/" + url.PathEscape(ws) + "/" + url.PathEscape(agent) + "/avatar?v=" + strconv.FormatInt(info.ModTime().Unix(), 36)
}

// agentDir finds an agent's folder by workspace and agent name, without a
// full scan.
func (s *webServer) agentDir(wsName, agent string) (string, bool) {
	var root string
	if s.src.FleetRoot == "" {
		if wsName == filepath.Base(s.src.Root) {
			root = s.src.Root
		}
	} else {
		for _, dir := range FleetDirs(s.src.FleetRoot) {
			if filepath.Base(dir) == wsName {
				root = dir
			}
		}
	}
	if root == "" || agent == "" {
		return "", false
	}
	agentsDir := filepath.Join(root, "agents")
	if !dirExists(agentsDir) {
		agentsDir = filepath.Join(root, "Agents")
	}
	for _, pattern := range []string{"*/context.md", "*/*/context.md"} {
		matches, _ := filepath.Glob(filepath.Join(agentsDir, pattern))
		for _, m := range matches {
			if dir := filepath.Dir(m); agentName(dir) == agent {
				return dir, true
			}
		}
	}
	return "", false
}

// avatar serves an agent's avatar file. An SVG may hold script, so it is
// sent with a policy that lets it run nothing and fetch nothing.
func (s *webServer) avatar(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.agentDir(r.PathValue("ws"), r.PathValue("agent"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	path, ctype, info := avatarFile(dir)
	if info == nil {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, maxAvatar+1))
	f.Close()
	if err != nil || len(data) > maxAvatar {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", ctype)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	h.Set("Cache-Control", "private, max-age=86400")
	http.ServeContent(w, r, "", info.ModTime(), bytes.NewReader(data))
}
