package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
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
