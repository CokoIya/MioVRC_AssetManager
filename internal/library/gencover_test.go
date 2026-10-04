package library

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
)

func pngBytes(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0x40
	}
	img.Set(1, 1, color.RGBA{200, 80, 100, 255})
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

// A cover made in Unity shows only while nothing else gives a picture.
func TestGeneratedCover(t *testing.T) {
	st := testkit.NewStore(t)
	bare := &core.Asset{Key: "name:bare", Name: "Bare Dress", Category: "衣服"}
	own := &core.Asset{Key: "name:own", Name: "Own Dress", Category: "衣服", Covers: []string{filepath.Join(core.DataDir, "own.png")}}
	st.Assets = []*core.Asset{bare, own}
	rev := core.CurRev()

	if GeneratedCover(bare.Key) != "" || HasRealCover(st, bare) || !HasRealCover(st, own) {
		t.Fatal("before anything was generated")
	}
	if err := SetGeneratedCover(bare.Key, []byte("not a picture"), "Assets/x.prefab", "/p"); err == nil {
		t.Error("something that is no PNG was kept")
	}
	if err := SetGeneratedCover(bare.Key, pngBytes(64, 64), "Assets/Shop/Bare/Bare.prefab", "/proj"); err != nil {
		t.Fatal(err)
	}
	if err := SetGeneratedCover(own.Key, pngBytes(64, 64), "Assets/Shop/Own/Own.prefab", "/proj"); err != nil {
		t.Fatal(err)
	}
	if core.CurRev() == rev {
		t.Error("the window is not told")
	}
	first := GeneratedCover(bare.Key)
	if first == "" || !core.StatOK(first) || filepath.Dir(first) != filepath.Join(core.DataDir, "covers", "generated") {
		t.Fatalf("generated cover %q", first)
	}
	v := BuildView(st, bare)
	if !v.coverGen || !strings.Contains(v.Cover, "generated") || HasRealCover(st, bare) {
		t.Errorf("the card does not show it: %q gen=%v", v.Cover, v.coverGen)
	}
	// a picture of its own comes first, generated or not
	if v := BuildView(st, own); v.coverGen || !strings.Contains(v.Cover, "own.png") || !HasRealCover(st, own) {
		t.Errorf("own cover: %q gen=%v", v.Cover, v.coverGen)
	}
	// …also one that turns up later (a scan finds a picture beside the files, the player picks one)
	bare.Covers = []string{filepath.Join(core.DataDir, "later.png")}
	if v := BuildView(st, bare); v.coverGen || !strings.Contains(v.Cover, "later.png") {
		t.Errorf("a real cover found later: %q", v.Cover)
	}
	bare.Covers = nil
	st.User[bare.Key] = &core.UserData{Cover: filepath.Join(core.DataDir, "picked.png")}
	if v := BuildView(st, bare); v.coverGen || !strings.Contains(v.Cover, "picked.png") {
		t.Errorf("a cover the player picked: %q", v.Cover)
	}
	delete(st.User, bare.Key)

	// made again: another file (the window caches pictures by path), the old one goes
	if err := SetGeneratedCover(bare.Key, pngBytes(32, 32), "Assets/Shop/Bare/Bare_B.prefab", "/proj"); err != nil {
		t.Fatal(err)
	}
	second := GeneratedCover(bare.Key)
	if second == first || core.StatOK(first) || !core.StatOK(second) {
		t.Errorf("regenerated: %q then %q", first, second)
	}
	if g := GeneratedCoverInfo(bare.Key); g == nil || g.Prefab != "Assets/Shop/Bare/Bare_B.prefab" || g.Project != "/proj" || g.At == 0 {
		t.Errorf("info %+v", g)
	}
	// read again from the file (a restart); an entry whose picture is gone is dropped
	b, _ := os.ReadFile(filepath.Join(core.DataDir, "gencovers.json"))
	if !strings.Contains(string(b), "Bare_B.prefab") || core.StatOK(filepath.Join(core.DataDir, "gencovers.json.tmp")) {
		t.Errorf("gencovers.json: %s", b)
	}
	_ = os.Remove(GeneratedCover(own.Key))
	genMu.Lock()
	genAll = nil
	genMu.Unlock()
	if GeneratedCover(bare.Key) != second || GeneratedCover(own.Key) != "" {
		t.Errorf("after a restart: %q %q", GeneratedCover(bare.Key), GeneratedCover(own.Key))
	}
	if !RemoveGeneratedCover(bare.Key) || GeneratedCover(bare.Key) != "" || core.StatOK(second) || RemoveGeneratedCover(bare.Key) {
		t.Error("removing it")
	}
	if v := BuildView(st, bare); v.Cover != "" {
		t.Errorf("cover after removal: %q", v.Cover)
	}
}
