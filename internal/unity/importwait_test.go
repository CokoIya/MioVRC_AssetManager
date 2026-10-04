package unity

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"vrclib/internal/library"
	"vrclib/internal/testkit"
)

func TestImportAndWait(t *testing.T) {
	st := testkit.NewStore(t)
	st.Settings.NoWatch, st.Settings.AutoBooth = true, false
	old := importPoll
	importPoll = 5 * time.Millisecond
	t.Cleanup(func() {
		importPoll = old
		for i := 0; i < 400 && library.PipelineBusy(); i++ {
			time.Sleep(25 * time.Millisecond)
		}
		DismissImport()
	})
	projA, projB := newProject(t), newProject(t)
	pkg := filepath.Join(t.TempDir(), "Dress_v2.unitypackage")
	makeUnityPackage(t, pkg, []pkgFile{{guid: g("a"), path: "Assets/Dress", dir: true}, {guid: g("b"), path: "Assets/Dress/Dress.prefab", body: "v2"}})
	// the file is in project A already, somewhere else: it is replaced where it is
	_ = os.MkdirAll(filepath.Join(projA, "Assets", "Moved"), 0755)
	_ = os.WriteFile(filepath.Join(projA, "Assets", "Moved", "Dress.prefab"), []byte("v1"), 0644)
	_ = os.WriteFile(filepath.Join(projA, "Assets", "Moved", "Dress.prefab.meta"), []byte("fileFormatVersion: 2\nguid: "+g("b")+"\n"), 0644)

	var stages []string
	for _, p := range []string{projA, projB} {
		j, err := ImportAndWait(context.Background(), st, ImportReq{Paths: []string{pkg}, Project: p}, func(j ImportJob) { stages = append(stages, j.Stage) })
		if err != nil || j.Stage != "done" || j.Files != 1 || j.Project != p {
			t.Fatalf("%s: %+v %v", p, j, err)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(projA, "Assets", "Moved", "Dress.prefab")); string(b) != "v2" {
		t.Errorf("project A: the file was not updated where it is (%q)", b)
	}
	if b, _ := os.ReadFile(filepath.Join(projB, "Assets", "Dress", "Dress.prefab")); string(b) != "v2" {
		t.Errorf("project B: %q", b)
	}
	if len(stages) == 0 || stages[len(stages)-1] != "done" {
		t.Errorf("stages told: %v", stages)
	}
	// not a project: said at once; cancelled before its turn: never started
	if _, err := ImportAndWait(context.Background(), st, ImportReq{Paths: []string{pkg}, Project: t.TempDir()}, nil); err == nil {
		t.Error("a folder that is no project was accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ImportAndWait(ctx, st, ImportReq{Paths: []string{pkg}, Project: projB}, nil); err != context.Canceled {
		t.Errorf("cancelled: %v", err)
	}
	// an import the player has to choose in is called off when the work it belongs to is cancelled
	both := t.TempDir()
	makeUnityPackage(t, filepath.Join(both, "Dress_Plum.unitypackage"), []pkgFile{{guid: g("c"), path: "Assets/P.prefab", body: "p"}})
	makeUnityPackage(t, filepath.Join(both, "Dress_Chocolat.unitypackage"), []pkgFile{{guid: g("d"), path: "Assets/C.prefab", body: "c"}})
	ctx, cancel = context.WithCancel(context.Background())
	go func() {
		for i := 0; i < 400; i++ {
			if j := ImportSnapshot(); j != nil && j.Stage == "choose" {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		cancel()
	}()
	j, err := ImportAndWait(ctx, st, ImportReq{Paths: []string{both}, Project: projB}, nil)
	if err != nil || j.Stage != "failed" || j.Msg != "已取消" {
		t.Errorf("cancelled at the choice: %+v %v", j, err)
	}
	if core := filepath.Join(projB, "Assets", "P.prefab"); fileThere(core) {
		t.Error("imported although cancelled")
	}
}

func fileThere(p string) bool { _, err := os.Stat(p); return err == nil }
