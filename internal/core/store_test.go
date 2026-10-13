package core

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func readLibrary(t *testing.T, file string) (data bool, valid bool) {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		return false, false
	}
	return strings.Contains(string(b), "precious"), json.Valid(b)
}

// Saves from several goroutines (handlers, the pipeline, the translator): library.json is there and whole
// after every round, and no save fails.
func TestConcurrentSave(t *testing.T) {
	p := filepath.Join(t.TempDir(), "library.json")
	st := LoadStore(p)
	for i := 0; i < 300; i++ {
		st.User["k"+Itoa(i)] = &UserData{Notes: strings.Repeat("x", 200)}
	}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 60; round++ {
		var wg sync.WaitGroup
		for g := 0; g < 4; g++ {
			wg.Add(1)
			go func(g int) {
				defer wg.Done()
				st.Mu.Lock()
				st.User["k0"].Notes = strings.Repeat("y", 100+g*50000) // sizes differ between writers, as real edits do
				st.Mu.Unlock()
				if err := st.Save(); err != nil {
					t.Errorf("round %d: %v", round, err)
				}
			}(g)
		}
		wg.Wait()
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("round %d: library.json is gone: %v", round, err)
		}
		var on Store
		if err := json.Unmarshal(b, &on); err != nil {
			t.Fatalf("round %d: library.json is damaged: %v", round, err)
		}
		st.Mu.RLock()
		want := st.User["k0"].Notes
		st.Mu.RUnlock()
		if on.User["k0"].Notes != want {
			t.Fatalf("round %d: the last change is not on disk", round)
		}
	}
	if _, valid := readLibrary(t, p+".bak"); !valid {
		t.Error("library.json.bak is not a whole library")
	}
	if left, _ := filepath.Glob(p + "*.tmp"); len(left) > 0 {
		t.Errorf("temporary files left: %v", left)
	}
}

func savedLibrary(t *testing.T) (string, []byte) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "library.json")
	st := LoadStore(p)
	st.Settings.Roots = []string{`D:\assets`}
	st.Settings.SetupDone = true
	st.User["name:x"] = &UserData{Notes: "precious"}
	for i := 0; i < 2; i++ { // the second save makes the backup
		if err := st.Save(); err != nil {
			t.Fatal(err)
		}
	}
	good, _ := os.ReadFile(p)
	if data, valid := readLibrary(t, p+".bak"); !data || !valid {
		t.Fatal("no backup after two saves")
	}
	return p, good
}

// A library.json cut short (power loss): the backup is used, the window is told, the damaged file is kept,
// and saving does not replace the backup before library.json is whole again.
func TestLoadDamagedUsesBackup(t *testing.T) {
	p, good := savedLibrary(t)
	if err := os.WriteFile(p, good[:len(good)/2], 0644); err != nil {
		t.Fatal(err)
	}
	st := LoadStore(p)
	if u := st.User["name:x"]; u == nil || u.Notes != "precious" || !st.Settings.SetupDone || len(st.Settings.Roots) != 1 {
		t.Fatalf("not restored: users=%d setupDone=%v roots=%v", len(st.User), st.Settings.SetupDone, st.Settings.Roots)
	}
	if n := st.Notices(); len(n) != 1 || !strings.Contains(n[0], "已从备份恢复（备份时间 ") {
		t.Errorf("notices %q", n)
	}
	broken, _ := filepath.Glob(p + ".broken-*")
	if len(broken) != 1 {
		t.Fatalf("damaged copy: %v", broken)
	}
	for i := 0; i < 3; i++ {
		if err := st.Save(); err != nil {
			t.Fatal(err)
		}
		for _, f := range []string{p, p + ".bak"} {
			if data, valid := readLibrary(t, f); !data || !valid {
				t.Fatalf("after save %d: %s data=%v valid=%v", i+1, filepath.Base(f), data, valid)
			}
		}
	}
	if b, _ := os.ReadFile(broken[0]); len(b) != len(good)/2 {
		t.Error("the damaged copy was touched")
	}
	// the next start reads library.json itself again, with nothing to tell
	if st2 := LoadStore(p); len(st2.Notices()) != 0 || st2.User["name:x"] == nil {
		t.Errorf("after the repair: notices %q", st2.Notices())
	}
}

// A damaged library.json of which no copy can be written (the name is taken, the disk refuses): it is not
// written over either, but set aside under that name before the first save.
func TestLoadDamagedWithoutCopySetsItAside(t *testing.T) {
	p, good := savedLibrary(t)
	if err := os.WriteFile(p, good[:len(good)/2], 0644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i := 0; i < 5; i++ { // folders where the copy would go: it cannot be written
		_ = os.MkdirAll(p+".broken-"+now.Add(time.Duration(i)*time.Second).Format("20060102-150405"), 0755)
	}
	st := LoadStore(p)
	if u := st.User["name:x"]; u == nil || u.Notes != "precious" {
		t.Fatal("not restored from the backup")
	}
	if n := st.Notices(); len(n) != 1 || !strings.Contains(n[0], "原文件保持原样，不会被覆盖") {
		t.Errorf("notices before the save %q", n)
	}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	var aside []string
	all, _ := filepath.Glob(p + ".broken-*")
	for _, f := range all {
		if fi, err := os.Stat(f); err == nil && !fi.IsDir() {
			aside = append(aside, f)
		}
	}
	if len(aside) != 1 {
		t.Fatalf("set aside: %v", all)
	}
	if b, _ := os.ReadFile(aside[0]); len(b) != len(good)/2 {
		t.Error("the damaged file was not kept as it was")
	}
	if data, valid := readLibrary(t, p); !data || !valid {
		t.Errorf("library.json after the save: data=%v valid=%v", data, valid)
	}
	if n := st.Notices(); len(n) != 1 || !strings.Contains(n[0], "原文件已改名保留为 "+filepath.Base(aside[0])) {
		t.Errorf("notices after the save %q", n)
	}
}

// library.json missing with a backup next to it (older versions renamed it away for a moment while saving).
func TestLoadMissingUsesBackup(t *testing.T) {
	p, _ := savedLibrary(t)
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	st := LoadStore(p)
	if st.User["name:x"] == nil || !st.Settings.SetupDone {
		t.Fatalf("not restored: users=%d setupDone=%v", len(st.User), st.Settings.SetupDone)
	}
	if n := st.Notices(); len(n) != 1 || !strings.Contains(n[0], "素材库文件缺失，已从备份恢复") {
		t.Errorf("notices %q", n)
	}
	if broken, _ := filepath.Glob(p + "*.broken-*"); len(broken) != 0 {
		t.Errorf("nothing was damaged, yet: %v", broken)
	}
	_ = st.Save()
	if data, valid := readLibrary(t, p); !data || !valid {
		t.Error("library.json not written back")
	}
}

// Both files damaged: a new library as before, but both damaged files stay, whatever is saved later.
func TestLoadBothDamaged(t *testing.T) {
	p, good := savedLibrary(t)
	_ = os.WriteFile(p, good[:len(good)/2], 0644)
	_ = os.WriteFile(p+".bak", []byte("\x00\x00\x00"), 0644)
	st := LoadStore(p)
	if len(st.User) != 0 || st.Settings.SetupDone {
		t.Fatalf("users=%d setupDone=%v", len(st.User), st.Settings.SetupDone)
	}
	if n := st.Notices(); len(n) != 1 || !strings.Contains(n[0], "没有可用的备份") {
		t.Errorf("notices %q", n)
	}
	for i := 0; i < 3; i++ {
		_ = st.Save()
	}
	broken, _ := filepath.Glob(p + "*.broken-*")
	if len(broken) != 2 {
		t.Fatalf("damaged copies: %v", broken)
	}
	if b, _ := os.ReadFile(broken[1]); len(b) != len(good)/2 { // sorted: the backup's copy first
		t.Error("the copy of library.json is not what was on disk")
	}
	// a first start: no files, no notice
	if st := LoadStore(filepath.Join(t.TempDir(), "library.json")); len(st.Notices()) != 0 || st.Settings.SetupDone {
		t.Errorf("first start: %q", st.Notices())
	}
}

// An empty library.json (cut off at its first byte) and no backup: the notice does not speak of a copy kept,
// since there was nothing to keep.
func TestLoadEmptyWithoutBackup(t *testing.T) {
	p := filepath.Join(t.TempDir(), "library.json")
	if err := os.WriteFile(p, nil, 0644); err != nil {
		t.Fatal(err)
	}
	st := LoadStore(p)
	if n := st.Notices(); len(n) != 1 || !strings.HasPrefix(n[0], "素材库文件已损坏，且没有可用的备份") || strings.Contains(n[0], ".broken") {
		t.Errorf("notices %q", n)
	}
	if broken, _ := filepath.Glob(p + "*.broken-*"); len(broken) != 0 {
		t.Errorf("copies of nothing: %v", broken)
	}
}

// unreadable takes the files' read permission away (as a virus scanner or a sync client holding them, a file
// in the cloud that cannot be fetched, or a missing permission do) and returns what gives it back. Root reads
// any file and Windows has no such mode: the tests that need it are left out there.
func unreadable(t *testing.T, files ...string) (giveBack func()) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a file that cannot be read: run as a user other than root")
	}
	patience := readPatience
	readPatience = 50 * time.Millisecond
	t.Cleanup(func() { readPatience = patience })
	for _, f := range files {
		if err := os.Chmod(f, 0); err != nil {
			t.Fatal(err)
		}
	}
	giveBack = func() {
		for _, f := range files {
			_ = os.Chmod(f, 0644)
		}
	}
	t.Cleanup(giveBack)
	return giveBack
}

// keptAs: what is kept of a file under another name ("unread": renamed, "broken": a copy of a damaged one).
func keptAs(file, kind string) []string {
	m, _ := filepath.Glob(file + "." + kind + "-*")
	return m
}

const (
	olderLib = `{"version":1,"settings":{"roots":["D:\\older"],"setupDone":true},"user":{"name:x":{"notes":"precious"}}}`
	newerLib = `{"version":1,"settings":{"roots":["D:\\newer"],"setupDone":true},"user":{"name:x":{"notes":"precious"},"name:y":{"notes":"newer"}}}`
)

// library.json is there but cannot be read at the start, for longer than the retries wait (held by another
// program, in the cloud and offline, no access): the backup is used, the window is told what happened, and
// the file is renamed out of the way before anything is saved in its place — never written over.
func TestLoadUnreadableKeepsTheFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "library.json")
	_ = os.WriteFile(p+".bak", []byte(olderLib), 0644)
	_ = os.WriteFile(p, []byte(newerLib), 0644)
	giveBack := unreadable(t, p)
	st := LoadStore(p)
	giveBack() // let go of again
	if len(st.Settings.Roots) != 1 || st.Settings.Roots[0] != `D:\older` || st.User["name:y"] != nil {
		t.Fatalf("not started from the backup: %v", st.Settings.Roots)
	}
	n := st.Notices()
	if len(n) != 1 || !strings.HasPrefix(n[0], "素材库文件暂时无法读取（") || !strings.Contains(n[0], "的备份启动；原文件保持原样，不会被覆盖") || strings.Contains(n[0], ".broken") {
		t.Errorf("notices %q", n)
	}
	if broken, _ := filepath.Glob(p + "*.broken-*"); len(broken) != 0 || len(keptAs(p, "unread")) != 0 {
		t.Errorf("files before the first save: %v %v", broken, keptAs(p, "unread"))
	}
	for i := 0; i < 3; i++ { // (the second save refreshes the backup)
		if err := st.Save(); err != nil {
			t.Fatal(err)
		}
	}
	aside := keptAs(p, "unread")
	if len(aside) != 1 {
		t.Fatalf("the file that could not be read: %v", aside)
	}
	if b, _ := os.ReadFile(aside[0]); string(b) != newerLib {
		t.Errorf("not kept as it was: %s", b)
	}
	if b, _ := os.ReadFile(p); !strings.Contains(string(b), `D:\\older`) || strings.Contains(string(b), "newer") {
		t.Errorf("library.json after the saves: %s", b)
	}
	if n := st.Notices(); len(n) != 1 || !strings.Contains(n[0], "；原文件已改名保留为 "+filepath.Base(aside[0])+"，") {
		t.Errorf("notices after the save: %q", n)
	}
	if data, valid := readLibrary(t, p+".bak"); !data || !valid {
		t.Error("library.json.bak is not a whole library")
	}
}

// Both library.json and its backup cannot be read: a new library, whose saves write over neither of them.
func TestLoadBothUnreadable(t *testing.T) {
	p := filepath.Join(t.TempDir(), "library.json")
	_ = os.WriteFile(p+".bak", []byte(olderLib), 0644)
	_ = os.WriteFile(p, []byte(newerLib), 0644)
	giveBack := unreadable(t, p, p+".bak")
	st := LoadStore(p)
	giveBack()
	if len(st.User) != 0 || st.Settings.SetupDone {
		t.Fatalf("users=%d setupDone=%v", len(st.User), st.Settings.SetupDone)
	}
	if n := st.Notices(); len(n) != 1 || !strings.Contains(n[0], "暂时无法读取") || !strings.Contains(n[0], "且没有可用的备份，已按全新素材库启动；原文件保持原样") || strings.Contains(n[0], ".broken") {
		t.Errorf("notices %q", n)
	}
	for i := 0; i < 3; i++ {
		if err := st.Save(); err != nil {
			t.Fatal(err)
		}
	}
	main, bak := keptAs(p, "unread"), keptAs(p+".bak", "unread")
	if broken, _ := filepath.Glob(p + "*.broken-*"); len(main) != 1 || len(bak) != 1 || len(broken) != 0 {
		t.Fatalf("kept: %v %v %v", main, bak, broken)
	}
	for f, want := range map[string]string{main[0]: newerLib, bak[0]: olderLib} {
		if b, _ := os.ReadFile(f); string(b) != want {
			t.Errorf("%s: %s", filepath.Base(f), b)
		}
	}
	for _, f := range []string{p, p + ".bak"} {
		if _, valid := readLibrary(t, f); !valid {
			t.Errorf("%s is not a whole library", filepath.Base(f))
		}
	}
}

// The file that could not be read cannot be renamed either: nothing is saved, the window says why (and the
// log once), and the next save tries again.
func TestSaveWaitsUntilUnreadableIsAside(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	_ = os.MkdirAll(dir, 0755)
	p := filepath.Join(dir, "library.json")
	_ = os.WriteFile(p+".bak", []byte(olderLib), 0644)
	_ = os.WriteFile(p, []byte(newerLib), 0644)
	giveBack := unreadable(t, p)
	st := LoadStore(p)
	giveBack()
	var logged strings.Builder
	old := Logger
	Logger = log.New(&logged, "", 0)
	defer func() { Logger = old }()
	_ = os.Chmod(dir, 0555) // nothing can be renamed in it
	t.Cleanup(func() { _ = os.Chmod(dir, 0755) })
	for i := 0; i < 2; i++ {
		if err := st.Save(); err == nil {
			t.Fatal("saved although library.json could not be put aside")
		}
	}
	n := st.Notices()
	if len(n) != 2 || !strings.HasSuffix(n[0], "原文件保持原样，不会被覆盖") || !strings.Contains(n[1], "素材库保存失败") || !strings.Contains(n[1], "无法改名保留") {
		t.Errorf("notices %q", n)
	}
	if c := strings.Count(logged.String(), "保存失败"); c != 1 {
		t.Errorf("logged %d times:\n%s", c, logged.String())
	}
	if b, _ := os.ReadFile(p); string(b) != newerLib {
		t.Errorf("library.json was written over: %s", b)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*")); len(left) != 2 {
		t.Errorf("files: %v", left)
	}
	_ = os.Chmod(dir, 0755)
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	aside := keptAs(p, "unread")
	if len(aside) != 1 || len(st.Notices()) != 1 || !strings.Contains(st.Notices()[0], "原文件已改名保留为 library.json.unread-") {
		t.Fatalf("after the folder lets go: %v %q", aside, st.Notices())
	}
	if b, _ := os.ReadFile(aside[0]); string(b) != newerLib {
		t.Errorf("not kept as it was: %s", b)
	}
}

// A save that cannot be written is reported once and shown in the window until saving works again.
func TestSaveFailureIsShown(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	_ = os.MkdirAll(dir, 0755)
	st := LoadStore(filepath.Join(dir, "library.json"))
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	st.Path = filepath.Join(dir, "gone", "library.json") // the folder is not there
	rev := CurRev()
	if st.Save() == nil || st.Save() == nil {
		t.Fatal("saving into a missing folder worked")
	}
	if n := st.Notices(); len(n) != 1 || !strings.Contains(n[0], "素材库保存失败") {
		t.Errorf("notices %q", n)
	}
	if CurRev() != rev+1 {
		t.Errorf("the window was told %d times", CurRev()-rev)
	}
	st.Path = filepath.Join(dir, "library.json")
	if err := st.Save(); err != nil || len(st.Notices()) != 0 {
		t.Errorf("after it works again: %v %q", err, st.Notices())
	}
}

// Base bodies added to the table after a library was made are added to it once; what the player has under
// another row or took out again stays as the player has it.
func TestBaseAdditions(t *testing.T) {
	p := filepath.Join(t.TempDir(), "library.json")
	old := `{"version":1,"settings":{"roots":["/x"],"setupDone":true,"bases":["Plum=plum|プラム","Mine=キプフェル"]}}`
	if err := os.WriteFile(p, []byte(old), 0644); err != nil {
		t.Fatal(err)
	}
	st := LoadStore(p)
	bases := strings.Join(st.Settings.Bases, "\n")
	if st.Settings.BasesAdded != len(BaseAdditions) || strings.Contains(bases, "Kipfel=") || !strings.Contains(bases, "Nemesis=nemesis|ネメシス") ||
		!strings.HasPrefix(bases, "Plum=plum|プラム\nMine=キプフェル\n") {
		t.Fatalf("merged: %d %q", st.Settings.BasesAdded, bases)
	}
	// taken out again, and saved: it stays out
	var kept []string
	for _, b := range st.Settings.Bases {
		if !strings.HasPrefix(b, "Nemesis=") {
			kept = append(kept, b)
		}
	}
	st.Settings.Bases = kept
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	if again := LoadStore(p); strings.Contains(strings.Join(again.Settings.Bases, "\n"), "Nemesis") {
		t.Errorf("put back: %v", again.Settings.Bases)
	}
	// a new library has them all, and nothing to add
	d := DefaultSettings()
	if d.BasesAdded != len(BaseAdditions) || len(MergeBaseAdditions(d.Bases, 0)) != len(d.Bases) {
		t.Errorf("defaults: %d rows, merged %d", len(d.Bases), len(MergeBaseAdditions(d.Bases, 0)))
	}
}
