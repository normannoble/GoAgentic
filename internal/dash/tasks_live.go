package dash

import (
	"sync"
	"time"
)

// liveCache keeps what Herdr and T3 said about live sessions, so the Tasks
// pages can mark every agent live or not on each load without waiting on
// them. The first answer is waited for; after that a page gets the last
// answer at once, and an answer older than liveCacheFor is refreshed in
// the background for the next one.
type liveCache struct {
	mu      sync.Mutex
	entries map[string]*liveEntry
}

type liveEntry struct {
	at      time.Time
	panes   []LivePane
	err     error
	loading bool
}

const liveCacheFor = 4 * time.Second

func (c *liveCache) get(key string, f func() ([]LivePane, error)) ([]LivePane, error) {
	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]*liveEntry{}
	}
	e := c.entries[key]
	if e == nil {
		c.mu.Unlock()
		panes, err := f()
		c.mu.Lock()
		c.entries[key] = &liveEntry{at: time.Now(), panes: panes, err: err}
		c.mu.Unlock()
		return panes, err
	}
	if time.Since(e.at) > liveCacheFor && !e.loading {
		e.loading = true
		go func() {
			panes, err := f()
			c.mu.Lock()
			e.at, e.panes, e.err, e.loading = time.Now(), panes, err, false
			c.mu.Unlock()
		}()
	}
	panes, err := e.panes, e.err
	c.mu.Unlock()
	return panes, err
}

func (c *liveCache) herdr(f func() ([]LivePane, error)) func() ([]LivePane, error) {
	if f == nil {
		return nil
	}
	return func() ([]LivePane, error) { return c.get("herdr", f) }
}

func (c *liveCache) t3(f func(string) ([]LivePane, error)) func(string) ([]LivePane, error) {
	if f == nil {
		return nil
	}
	return func(root string) ([]LivePane, error) {
		return c.get("t3:"+root, func() ([]LivePane, error) { return f(root) })
	}
}
