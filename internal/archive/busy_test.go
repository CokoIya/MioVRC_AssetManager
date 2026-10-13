package archive

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
)

// users: the unpacks holding or waiting for the archive main opens.
func users(main string) int {
	busyMu.Lock()
	defer busyMu.Unlock()
	if l := busy[core.PathKey(main)]; l != nil {
		return l.users
	}
	return 0
}

func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); !ok(); time.Sleep(time.Millisecond) {
		if time.Now().After(end) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// An unpack of an archive another unpack is at waits for it, and then looks again: unpacked by the other (into a
// folder of any name), or gone, it is left as an archive unpacked before is — not unpacked again, not failed, not
// removed. Only when the other did not unpack it does this one.
func TestUnpackWaitsForAnotherOfTheSameArchive(t *testing.T) {
	for _, c := range []struct {
		name  string
		first func(t *testing.T, z string) string // what the other unpack does with it: the folder it made, or ""
		done  int                                 // folders this one makes
		left  bool                                // the archive is still there afterwards
	}{
		{"unpacked by the other, into a folder of another name", func(t *testing.T, z string) string {
			out := filepath.Join(filepath.Dir(z), "Somewhere else")
			_ = os.Mkdir(out, 0755)
			return out
		}, 0, true},
		{"unpacked and removed by the other", func(t *testing.T, z string) string {
			out, err := extractArchive(findArchives(z)[0], "")
			if err != nil {
				t.Fatal(err)
			}
			_ = os.Remove(z)
			return out
		}, 0, false},
		{"gone, with nothing to say where to", func(t *testing.T, z string) string {
			_ = os.Remove(z)
			return ""
		}, 0, false},
		{"not unpacked by the other", func(t *testing.T, z string) string { return "" }, 1, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			z := filepath.Join(t.TempDir(), "Dress_v2.zip")
			testkit.MakeZip(t, z, map[string]string{"Dress/a.txt": "a", "Dress/b.txt": "b"})
			_, unlock := lockArchive(z) // the other unpack is at it
			var removed []string
			got := make(chan unpackResult)
			go func() {
				got <- UnpackFiles([]string{z}, "", func(p []string) error { removed = append(removed, p...); return RemoveFiles(p) }, nil)
			}()
			waitUntil(t, "the second unpack to wait", func() bool { return users(z) == 2 })
			unlock(c.first(t, z))
			res := <-got
			if len(res.Failed) != 0 || len(res.Done) != c.done || len(removed) != c.done {
				t.Errorf("done %v, failed %v, removed %v", res.Done, res.Failed, removed)
			}
			if core.FileExists(z) != c.left {
				t.Errorf("the archive is there: %v", core.FileExists(z))
			}
			if users(z) != 0 {
				t.Error("the archive is still held")
			}
		})
	}
	// UnpackAgain waits too, and takes what the other made of it
	dir := t.TempDir()
	z := filepath.Join(dir, "Dress.zip")
	testkit.MakeZip(t, z, map[string]string{"Dress/a.txt": "new"})
	_ = os.MkdirAll(filepath.Join(dir, "Dress"), 0755) // the earlier version's folder
	_, unlock := lockArchive(z)
	type again struct {
		dir   string
		again bool
		err   error
	}
	got := make(chan again)
	go func() {
		d, a, err := UnpackAgain(Sets([]string{z})[0], "")
		got <- again{d, a, err}
	}()
	waitUntil(t, "UnpackAgain to wait", func() bool { return users(z) == 2 })
	other := filepath.Join(dir, "Dress (2)")
	_ = os.Mkdir(other, 0755)
	unlock(other)
	if r := <-got; r.dir != other || !r.again || r.err != nil {
		t.Errorf("UnpackAgain after the other: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(dir, "Dress (3)")); err == nil {
		t.Error("unpacked once more")
	}
}

// Unpacks of one archive at the same time — a download's of the file, an import's of its folder: it is unpacked
// once, whole, and nothing fails; no folder of theirs is left.
func TestUnpacksOfOneArchiveAtOnce(t *testing.T) {
	for round := 0; round < 8; round++ {
		dir := t.TempDir()
		entries := map[string]string{}
		for i := 0; i < 150; i++ {
			entries[fmt.Sprintf("Dress/f%03d.txt", i)] = strings.Repeat("x", 2000)
		}
		z := filepath.Join(dir, "Dress.zip")
		testkit.MakeZip(t, z, entries)
		var wg sync.WaitGroup
		var mu sync.Mutex
		var done, failed []string
		for k := 0; k < 3; k++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var res unpackResult
				if k == 0 {
					res = UnpackAll([]string{dir}, "", RemoveFiles, nil)
				} else {
					res = UnpackFiles([]string{z}, "", RemoveFiles, nil)
				}
				mu.Lock()
				done = append(done, res.Done...)
				for a, e := range res.Failed {
					failed = append(failed, a+": "+e)
				}
				mu.Unlock()
			}()
		}
		wg.Wait()
		if len(done) != 1 || len(failed) != 0 {
			t.Fatalf("round %d: done %v, failed %v", round, done, failed)
		}
		if got := testkit.ListTree(dir); len(got) != len(entries) || core.FileExists(z) {
			t.Fatalf("round %d: %d files there (of %d), the archive there: %v", round, len(got), len(entries), core.FileExists(z))
		}
		if left, _ := filepath.Glob(filepath.Join(dir, ".*")); len(left) > 0 {
			t.Fatalf("round %d: left %v", round, left)
		}
	}
}

// Each unpack works in a folder of its own, next to the archive, named so that every rule that passes over such
// folders still does (a dot first, ".extracting" or ".joining" last), and what lies in one is no archive of the
// folder's.
func TestWorkDirsOfTheirOwn(t *testing.T) {
	dir := t.TempDir()
	a, doneA := workDir(dir, "Dress", "extracting")
	b, doneB := workDir(dir, "Dress", "extracting")
	j, doneJ := workDir(dir, "Dress", "joining")
	defer doneA()
	defer doneB()
	defer doneJ()
	if a == b || filepath.Dir(a) != dir {
		t.Fatalf("%s, %s", a, b)
	}
	for _, p := range []string{a, b, j} {
		n := filepath.Base(p)
		if !strings.HasPrefix(n, ".Dress.") || !(strings.HasSuffix(n, ".extracting") || strings.HasSuffix(n, ".joining")) || !reWorkDir.MatchString(n) {
			t.Errorf("work folder %s", n)
		}
	}
	for _, p := range []string{a, j} {
		_ = os.MkdirAll(p, 0755)
		testkit.MakeZip(t, filepath.Join(p, "Half.zip"), map[string]string{"x.txt": "x"})
	}
	if sets := findArchives(dir); len(sets) != 0 {
		t.Errorf("an archive in a work folder was taken: %+v", sets)
	}
}

// What an unpack that never ended left is removed the next time an archive next to it is unpacked, once it is an
// hour old: only folders named as this program names its own, and none an unpack is working in.
func TestStaleWorkDirsCleared(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-2 * time.Hour)
	mk := func(name string, at time.Time) string {
		p := filepath.Join(dir, name)
		testkit.WriteFile(t, filepath.Join(p, "Dress", "a.txt"), 10)
		_ = os.Chtimes(p, at, at)
		return p
	}
	gone := []string{mk(".Dress.0badf00d.extracting", old), mk(".Dress v1.2.12345678.joining", old),
		mk(".Coat.extracting", time.Now()), mk(".Coat.joining", old)} // an older version's names, of the archive unpacked
	inUse, done := workDir(dir, "Hair", "extracting")
	defer done()
	testkit.WriteFile(t, filepath.Join(inUse, "Hair", "a.txt"), 10)
	_ = os.Chtimes(inUse, old, old)
	kept := []string{
		inUse,
		mk(".Dress.abcdef01.extracting", time.Now()), // not an hour old: may be another program's that is running
		mk(".Dress.extracting", old),                 // an older version's name, of another archive
		mk("Dress.0badf00d.extracting", old),
		mk(".Coat.FEEDBEEF.extracting", old),
		mk(".git", old),
	}
	f := filepath.Join(dir, ".Coat.0badf00d.extracting") // a file: no folder of an unpack
	_ = os.WriteFile(f, []byte("x"), 0644)
	_ = os.Chtimes(f, old, old)
	kept = append(kept, f)
	testkit.MakeZip(t, filepath.Join(dir, "Coat.zip"), map[string]string{"Coat/a.txt": "a"})
	if res := UnpackAll([]string{filepath.Join(dir, "Coat.zip")}, "", nil, nil); len(res.Done) != 1 {
		t.Fatalf("unpack: %+v", res)
	}
	for _, p := range gone {
		if core.StatOK(p) {
			t.Errorf("left: %s", filepath.Base(p))
		}
	}
	for _, p := range kept {
		if !core.StatOK(p) {
			t.Errorf("removed: %s", filepath.Base(p))
		}
	}
}
