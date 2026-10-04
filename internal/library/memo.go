package library

import (
	"sync"

	"vrclib/internal/core"
	"vrclib/internal/naming"
)

// nameMemo keeps what was worked out from a name (the Booth search words, the stem downloads of one product
// share, "For_X" base bodies …). Building the cards asks for these again for every card each time the window
// refreshes, the names hardly ever change in between, and working them out is most of the cost.
type nameMemo[V any] struct {
	mu sync.RWMutex
	m  map[memoKey]V
}

// memoKey: the name, and what else the answer depends on — the table of base bodies in use.
type memoKey struct {
	defs *naming.BaseDef
	flag bool
	name string
}

const memoMax = 50000 // entries; a memo that has grown past this starts over

func defsID(defs []naming.BaseDef) *naming.BaseDef {
	if len(defs) == 0 {
		return nil
	}
	return &defs[0] // one parsed table per list of base bodies (naming.DefsCache), so its address tells them apart
}

func (c *nameMemo[V]) get(k memoKey, work func() V) V {
	c.mu.RLock()
	v, ok := c.m[k]
	c.mu.RUnlock()
	if ok {
		return v
	}
	v = work()
	c.mu.Lock()
	if c.m == nil || len(c.m) >= memoMax {
		c.m = map[memoKey]V{}
	}
	c.m[k] = v
	c.mu.Unlock()
	return v
}

// dirSeen: whether a folder is there, asked of the disk at most once between two scans (and again after a
// while): the cards are built while the store is held, which is no time to wait for a disk to spin up.
var dirSeen struct {
	mu sync.Mutex
	m  map[string]dirState
}

type dirState struct {
	ok bool
	at int64
}

const dirSeenKeep = 60 // seconds

func isDirCached(p string, now int64) bool {
	dirSeen.mu.Lock()
	s, ok := dirSeen.m[p]
	dirSeen.mu.Unlock()
	if ok && now-s.at < dirSeenKeep {
		return s.ok
	}
	s = dirState{core.IsDir(p), now}
	dirSeen.mu.Lock()
	if dirSeen.m == nil {
		dirSeen.m = map[string]dirState{}
	}
	dirSeen.m[p] = s
	dirSeen.mu.Unlock()
	return s.ok
}

// forgetDirs: folders may have come or gone (a scan ended, a download was unpacked).
func forgetDirs() {
	dirSeen.mu.Lock()
	dirSeen.m = nil
	dirSeen.mu.Unlock()
}
