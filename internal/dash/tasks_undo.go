package dash

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Undo for the Tasks view: each tracker edit, Done and answer keeps the
// file as it was before, and the hash of what it wrote. Undo puts the old
// file back only while the file is still exactly what the action wrote;
// if anything else changed it since (the agent applied the answer, a tick
// wrote the inbox), it refuses and says so. Records live in memory, for
// the last maxUndos actions.

type undoRec struct {
	path    string
	before  []byte
	existed bool
	after   string // trackerHash of the file the action wrote
	what    string
}

type undoStore struct {
	mu    sync.Mutex
	recs  map[string]undoRec
	order []string
}

const maxUndos = 100

func (u *undoStore) put(rec undoRec) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return u.putID(hex.EncodeToString(b), rec)
}

func (u *undoStore) putID(id string, rec undoRec) string {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.recs == nil {
		u.recs = map[string]undoRec{}
	}
	u.recs[id] = rec
	u.order = append(u.order, id)
	for len(u.order) > maxUndos {
		delete(u.recs, u.order[0])
		u.order = u.order[1:]
	}
	return id
}

func (u *undoStore) take(id string) (undoRec, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	rec, ok := u.recs[id]
	delete(u.recs, id)
	return rec, ok
}

// remember records a write to path; before is the file before (nil and
// existed false when there was none). It returns the undo id, "" when the
// file cannot be read back.
func (s *webServer) remember(path string, before []byte, existed bool, what string) string {
	after, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return s.tasksView.undos.put(undoRec{path: path, before: before, existed: existed, after: trackerHash(after), what: what})
}

// done answers a Tasks write: to the page's script, 204 with the undo id
// in X-Tasks-Undo (it goes to the form's next page itself); otherwise a
// redirect to back.
func done(w http.ResponseWriter, r *http.Request, undo, back string) {
	if undo != "" {
		w.Header().Set("X-Tasks-Undo", undo)
	}
	if r.Header.Get("X-Tasks-Fetch") == "1" || back == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !strings.HasPrefix(back, "/tasks") || strings.HasPrefix(back, "//") {
		back = "/tasks"
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// tasksUndo reverses one recorded action.
func (s *webServer) tasksUndo(w http.ResponseWriter, r *http.Request) {
	id := r.FormValue("id")
	rec, ok := s.tasksView.undos.take(id)
	if !ok {
		http.Error(w, "Nothing to undo: it was undone already, or the dashboard restarted since.", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := os.ReadFile(rec.path)
	if err != nil || trackerHash(cur) != rec.after {
		s.tasksView.undos.putID(id, rec) // still undoable if the file goes back
		http.Error(w, filepath.Base(rec.path)+" changed since; undo "+rec.what+" by hand.", http.StatusBadRequest)
		return
	}
	if !rec.existed {
		err = os.Remove(rec.path)
	} else {
		err = writeAtomic(rec.path, rec.before)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	done(w, r, "", r.FormValue("back"))
}

// tasksDone moves a row to the Completed table.
func (s *webServer) tasksDone(w http.ResponseWriter, r *http.Request) {
	path, err := s.trackerPath(r.FormValue("ws"), r.FormValue("agent"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	now := time.Now
	if s.src.Now != nil {
		now = s.src.Now
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if r.FormValue("hash") != trackerHash(data) {
		http.Error(w, r.FormValue("agent")+"'s tracker changed after this page was drawn. Reload and try again.", http.StatusBadRequest)
		return
	}
	out, err := MoveToCompleted(string(data), r.FormValue("id"), now())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := writeAtomic(path, []byte(out)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	done(w, r, s.remember(path, data, true, "Done on #"+strings.TrimPrefix(r.FormValue("id"), "#")), r.FormValue("back"))
}

func inboxPath(a *Agent) string { return filepath.Join(a.Dir, "memory", "scheduled", "inbox.md") }
