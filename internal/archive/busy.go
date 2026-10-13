package archive

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"vrclib/internal/core"
)

// Unpacks at the same time. An import, a download that has just ended, the netdisk's and the built-in page's
// downloads each unpack on their own, and two of them may come to one archive. Each works in folders of its
// own, and two unpacks of one archive take turns: the one that waited finds it unpacked, or gone, and leaves it
// at that. What an unpack that never ended (the program closed, the computer went off) left next to an archive
// is cleared once it is old.

// arcLock: one archive, while unpacks of this program are at it.
type arcLock struct {
	sync.Mutex
	users int    // the unpacks holding it or waiting for it (under busyMu)
	made  string // the folder the last of them unpacked it to
}

var (
	busyMu  sync.Mutex
	busy    = map[string]*arcLock{} // core.PathKey of the file that opens an archive → its lock
	working = map[string]bool{}     // core.PathKey of the folders unpacks work in now (workDir)
)

// lockArchive: the archive that main opens, for this unpack alone — it waits while another unpack of this
// program is at it. done: the folder the one it waited for unpacked it to ("" when none did). unlock lets the
// next one have it and takes the folder this one unpacked it to ("" for none); calling it again does nothing.
func lockArchive(main string) (done string, unlock func(made string)) {
	k := core.PathKey(main)
	busyMu.Lock()
	l := busy[k]
	if l == nil {
		l = &arcLock{}
		busy[k] = l
	}
	l.users++
	busyMu.Unlock()
	l.Lock()
	var once sync.Once
	return l.made, func(made string) {
		once.Do(func() {
			if made != "" {
				l.made = made
			}
			l.Unlock()
			busyMu.Lock()
			if l.users--; l.users == 0 {
				delete(busy, k)
			}
			busyMu.Unlock()
		})
	}
}

// workDir: a folder for one unpack to work in, next to what it unpacks — ".<name>.<8 hex digits>.<kind>", kind
// "extracting" or "joining" — that is not there yet and that no other unpack of this program has. (The fixed
// name before was shared by two unpacks of one archive, and each emptied it for itself.) The dot keeps the
// library away from it and the ending keeps every rule that skips these folders working. done: the unpack is
// through with it (removed, or moved into place).
func workDir(parent, name, kind string) (dir string, done func()) {
	busyMu.Lock()
	defer busyMu.Unlock()
	for {
		dir = filepath.Join(parent, fmt.Sprintf(".%s.%08x.%s", name, rand.Uint32(), kind))
		k := core.PathKey(dir)
		if _, err := os.Lstat(dir); err != nil && !working[k] { // (not there — or not to be looked at: making it says why)
			working[k] = true
			return dir, func() {
				busyMu.Lock()
				delete(working, k)
				busyMu.Unlock()
			}
		}
	}
}

var (
	// a folder workDir names
	reWorkDir = regexp.MustCompile(`^\..+\.[0-9a-f]{8}\.(?:extracting|joining)$`)
	// staleAfter: a work folder that has not changed for this long, and that no unpack of this program is in,
	// was left by one that never ended
	staleAfter = time.Hour
)

// clearStale removes what unpacks that never ended left in dir: folders named as workDir names them, that no
// unpack of this program works in, and that have not changed for staleAfter. (The fixed name had the next
// unpack of the same archive empty it; each unpack's own name is never used again.)
func clearStale(dir string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		if !e.IsDir() || !reWorkDir.MatchString(e.Name()) {
			continue
		}
		p := filepath.Join(dir, e.Name())
		busyMu.Lock()
		inUse := working[core.PathKey(p)]
		busyMu.Unlock()
		if fi, err := e.Info(); err == nil && !inUse && time.Since(fi.ModTime()) >= staleAfter {
			core.Logf("删除未完成的解压留下的临时文件夹 %s", p)
			_ = os.RemoveAll(p)
		}
	}
}

// clearOld removes what an unpack of a by an older version of this program left: it worked in a folder of a
// fixed name, ".<name>.extracting" or ".<name>.joining", emptied by the next unpack of the same archive — which
// is this one (it holds a's lock; this program names them as workDir does now).
func clearOld(a archiveSet) {
	parent := filepath.Dir(a.Main)
	for _, kind := range []string{"extracting", "joining"} {
		p := filepath.Join(parent, "."+core.SafeName(a.Name, 100)+"."+kind)
		if fi, err := os.Lstat(p); err == nil && fi.IsDir() {
			core.Logf("删除未完成的解压留下的临时文件夹 %s", p)
			_ = os.RemoveAll(p)
		}
	}
}
