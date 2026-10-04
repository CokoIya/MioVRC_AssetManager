package library

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"io"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
)

// one asset inside a unitypackage: its path, its preview (nil: none), and whether the preview is written
// before the pathname (Unity writes the pathname first; the reader must not depend on it)
type pkgEnt struct {
	guid, path  string
	preview     []byte
	previewPrev bool
	body        int // bytes of "asset"
}

func writePkg(t *testing.T, out string, ents []pkgEnt) {
	t.Helper()
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	put := func(name string, b []byte) {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(b)), Typeflag: tar.TypeReg, ModTime: time.Now()}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range ents {
		put("./"+e.guid+"/asset", bytes.Repeat([]byte{0xa5}, e.body))
		put("./"+e.guid+"/asset.meta", []byte("fileFormatVersion: 2\nguid: "+e.guid+"\n"))
		if e.preview != nil && e.previewPrev {
			put("./"+e.guid+"/preview.png", e.preview)
		}
		put("./"+e.guid+"/pathname", []byte(e.path+"\n00"))
		if e.preview != nil && !e.previewPrev {
			put("./"+e.guid+"/preview.png", e.preview)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
}

func encPNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// flatPNG: one colour (a prefab Unity could not draw, an empty texture)
func flatPNG(t *testing.T, c color.RGBA) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 128, 128))
	for y := 0; y < 128; y++ {
		for x := 0; x < 128; x++ {
			img.Set(x, y, c)
		}
	}
	return encPNG(t, img)
}

// shotPNG: something drawn on Unity's grey gradient — a disc with a lighter top
func shotPNG(t *testing.T, c color.RGBA, r int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 128, 128))
	for y := 0; y < 128; y++ {
		g := uint8(0x60 - y/4) // the gradient
		for x := 0; x < 128; x++ {
			if (x-64)*(x-64)+(y-64)*(y-64) < r*r {
				k := uint8(y / 2)
				img.Set(x, y, color.RGBA{c.R - min(c.R, k), c.G - min(c.G, k), c.B - min(c.B, k), 255})
			} else {
				img.Set(x, y, color.RGBA{g, g, g + 4, 255})
			}
		}
	}
	return encPNG(t, img)
}

// texPNG: a texture full of detail
func texPNG(t *testing.T, seed int64) []byte {
	rnd := rand.New(rand.NewSource(seed))
	img := image.NewRGBA(image.Rect(0, 0, 128, 128))
	for y := 0; y < 128; y++ {
		for x := 0; x < 128; x++ {
			v := uint8(rnd.Intn(256))
			img.Set(x, y, color.RGBA{v, uint8(x * 2), uint8(y * 2), 255})
		}
	}
	return encPNG(t, img)
}

// the run after a scan would outlive the test that scanned (and write into the next test's data folder)
func init() { pkgAuto = false }

func pickFrom(t *testing.T, file string, names ...string) (*pkgPick, error) {
	t.Helper()
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	return pickPreview(f, names, nil)
}

// The prefab named after the asset wins over textures, maps, materials and prefabs Unity could not draw.
func TestPickPreview(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "Sailor Dress.unitypackage")
	writePkg(t, pkg, []pkgEnt{
		{guid: g("1"), path: "Assets/Shop/SailorDress/Textures/SailorDress_N.png", preview: texPNG(t, 1), body: 5000},
		{guid: g("2"), path: "Assets/Shop/SailorDress/Textures/SailorDress.png", preview: texPNG(t, 2), body: 5000},
		{guid: g("3"), path: "Assets/Shop/SailorDress/Materials/SailorDress.mat", preview: shotPNG(t, color.RGBA{200, 60, 60, 255}, 40), body: 300},
		{guid: g("4"), path: "Assets/Shop/SailorDress/Prefabs/SailorDress_PhysBone.prefab", preview: shotPNG(t, color.RGBA{60, 60, 200, 255}, 30), body: 300},
		{guid: g("5"), path: "Assets/Shop/SailorDress/Prefabs/SailorDress_Empty.prefab", preview: flatPNG(t, color.RGBA{0x50, 0x50, 0x54, 255}), body: 300},
		{guid: g("6"), path: "Assets/Shop/SailorDress/SailorDress.prefab", preview: shotPNG(t, color.RGBA{230, 230, 240, 255}, 48), previewPrev: true, body: 300},
		{guid: g("7"), path: "Assets/Shop/SailorDress/Editor/Icon.png", preview: texPNG(t, 7), body: 100},
		{guid: g("8"), path: "Assets/Shop/SailorDress/Audio/Rustle.wav", preview: texPNG(t, 8), body: 100},
		{guid: g("9"), path: "Assets/Shop/SailorDress/Prefabs", body: 0},
	})
	pk, err := pickFrom(t, pkg, "Sailor Dress")
	if err != nil || pk.entry != "Assets/Shop/SailorDress/SailorDress.prefab" || !pk.strong {
		t.Fatalf("picked %+v %v", pk, err)
	}
	// without a usable prefab: the texture that is not a map, not an icon
	writePkg(t, pkg, []pkgEnt{
		{guid: g("1"), path: "Assets/Shop/SailorDress/Textures/SailorDress_N.png", preview: texPNG(t, 1), body: 50},
		{guid: g("2"), path: "Assets/Shop/SailorDress/Textures/SailorDress.png", preview: texPNG(t, 2), body: 50},
		{guid: g("5"), path: "Assets/Shop/SailorDress/Prefabs/SailorDress.prefab", preview: flatPNG(t, color.RGBA{0x50, 0x50, 0x54, 255}), body: 50},
		{guid: g("7"), path: "Assets/Shop/SailorDress/Icon.png", preview: texPNG(t, 7), body: 50},
	})
	pk, err = pickFrom(t, pkg, "Sailor Dress")
	if err != nil || pk.entry != "Assets/Shop/SailorDress/Textures/SailorDress.png" || pk.strong {
		t.Fatalf("fallback picked %+v %v", pk, err)
	}
	// nothing usable at all
	writePkg(t, pkg, []pkgEnt{
		{guid: g("5"), path: "Assets/Shop/SailorDress/Prefabs/SailorDress.prefab", preview: flatPNG(t, color.RGBA{0x50, 0x50, 0x54, 255}), body: 50},
		{guid: g("3"), path: "Assets/Shop/SailorDress/Materials/SailorDress.mat", preview: shotPNG(t, color.RGBA{200, 60, 60, 255}, 40), body: 50},
		{guid: g("a"), path: "Assets/Shop/SailorDress/Readme.txt", body: 50},
	})
	if _, err = pickFrom(t, pkg, "Sailor Dress"); err != errPkgNone {
		t.Fatalf("nothing usable: %v", err)
	}
	if err := os.WriteFile(pkg, []byte("not a package at all"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = pickFrom(t, pkg, "Sailor Dress"); err == nil {
		t.Fatal("not a package")
	}
}

// Previews whose pathname comes later are held, and let go when there are too many of them.
func TestPickPreviewHeld(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "p.unitypackage")
	var ents []pkgEnt
	for i := 0; i < pkgMaxHeld+20; i++ {
		ents = append(ents, pkgEnt{guid: strings.Repeat("b", 24) + fmtHex(i), path: "Assets/X/Tex" + fmtHex(i) + ".png", preview: texPNG(t, int64(i)), previewPrev: true, body: 10})
	}
	// the one that matters comes last, its preview first (held across all the others)
	ents = append(ents, pkgEnt{guid: g("c"), path: "Assets/X/Hat.prefab", preview: shotPNG(t, color.RGBA{250, 200, 80, 255}, 44), previewPrev: true, body: 10})
	writePkg(t, pkg, ents)
	pk, err := pickFrom(t, pkg, "Hat")
	if err != nil || pk.entry != "Assets/X/Hat.prefab" {
		t.Fatalf("picked %+v %v", pk, err)
	}
}

func fmtHex(i int) string {
	const h = "0123456789abcdef"
	return string([]byte{h[(i>>4)&15], h[i&15], h[(i>>8)&15], h[(i>>12)&15], 'a', 'b', 'c', 'd'})
}

func g(n string) string { return strings.Repeat(n, 32)[:32] }

// A cover from a package shows only while nothing else gives a picture; what was read is remembered, with
// what failed, so a package is not read again until it changes.
func TestPackageCovers(t *testing.T) {
	root := t.TempDir()
	sailor := filepath.Join(root, "Sailor Dress", "SailorDress_v1.unitypackage")
	_ = os.MkdirAll(filepath.Dir(sailor), 0755)
	writePkg(t, sailor, []pkgEnt{
		{guid: g("1"), path: "Assets/Shop/SailorDress/Textures/SailorDress.png", preview: texPNG(t, 2), body: 2000},
		{guid: g("6"), path: "Assets/Shop/SailorDress/SailorDress.prefab", preview: shotPNG(t, color.RGBA{230, 230, 240, 255}, 48), body: 300},
	})
	empty := filepath.Join(root, "Empty Pack", "Empty.unitypackage")
	_ = os.MkdirAll(filepath.Dir(empty), 0755)
	writePkg(t, empty, []pkgEnt{{guid: g("5"), path: "Assets/E/Empty.prefab", preview: flatPNG(t, color.RGBA{0x50, 0x50, 0x54, 255}), body: 300}})
	// a package inside a zip, and a zip with nothing in it
	var zbuf bytes.Buffer
	inner := filepath.Join(t.TempDir(), "Hat.unitypackage")
	writePkg(t, inner, []pkgEnt{{guid: g("c"), path: "Assets/X/Hat.prefab", preview: shotPNG(t, color.RGBA{250, 200, 80, 255}, 44), body: 10}})
	ib, _ := os.ReadFile(inner)
	zbuf.Write(testkit.ZipBytes(t, map[string][]byte{"Hat/Hat.unitypackage": ib, "Hat/readme.txt": []byte("x")}))
	if err := os.WriteFile(filepath.Join(root, "Hat.zip"), zbuf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	testkit.MakeZip(t, filepath.Join(root, "Notes.zip"), map[string]string{"notes.txt": "x"})
	// a picture of its own: nothing to read
	testkit.WriteFile(t, filepath.Join(root, "Pictured", "Pictured.unitypackage"), 100)
	testkit.WriteFile(t, filepath.Join(root, "Pictured", "Pictured.png"), 30*1024)

	st, byName := scanFor(t, root)
	a := byName["Sailor Dress"]
	if a == nil || byName["Empty Pack"] == nil || byName["Hat"] == nil || byName["Notes"] == nil || byName["Pictured"] == nil {
		t.Fatalf("assets: %v", byName)
	}
	if v := BuildView(st, a); v.Cover != "" || v.CoverPkg {
		t.Fatalf("before: %+v", v.Cover)
	}
	rev := core.CurRev()
	c, err := ExtractPkgCover(st, a.Key)
	if err != nil || c.Entry != "Assets/Shop/SailorDress/SailorDress.prefab" || c.Package != sailor || c.Size == 0 || c.MTime == 0 {
		t.Fatalf("extract: %+v %v", c, err)
	}
	if core.CurRev() == rev {
		t.Error("the window is not told")
	}
	file := PackageCover(st, a.Key)
	if file == "" || filepath.Dir(file) != filepath.Join(core.DataDir, "covers", "unitypackage") || !core.StatOK(file) {
		t.Fatalf("cover file %q", file)
	}
	if b, _ := os.ReadFile(file); !bytes.HasPrefix(b, []byte("\x89PNG")) {
		t.Error("the PNG was not kept as it is")
	}
	st.Mu.RLock()
	v := BuildView(st, a)
	real := HasRealCover(st, a)
	st.Mu.RUnlock()
	if !v.CoverPkg || !strings.Contains(v.Cover, "unitypackage") || real {
		t.Errorf("the card does not show it: %q pkg=%v", v.Cover, v.CoverPkg)
	}
	if s := PkgCoverStatusOf(st, a.Key); !s.Has || s.Cover == nil || s.Failed != "" || s.Off || s.Busy {
		t.Errorf("status %+v", s)
	}
	// anything else comes first: a picture beside the files, one the player picks, one drawn in Unity
	a.Covers = []string{filepath.Join(core.DataDir, "later.png")}
	if v := BuildView(st, a); v.CoverPkg || !strings.Contains(v.Cover, "later.png") {
		t.Errorf("a real cover found later: %q", v.Cover)
	}
	a.Covers = nil
	if err := SetGeneratedCover(a.Key, pngBytes(64, 64), "Assets/S.prefab", "/proj"); err != nil {
		t.Fatal(err)
	}
	if v := BuildView(st, a); v.CoverPkg || !v.coverGen {
		t.Errorf("a generated cover: %q", v.Cover)
	}
	RemoveGeneratedCover(a.Key)
	if v := BuildView(st, a); !v.CoverPkg {
		t.Errorf("back to the package's: %q", v.Cover)
	}
	// the file on disk: a restart reads it again; an entry whose picture is gone is dropped
	b, _ := os.ReadFile(filepath.Join(core.DataDir, "pkgcovers.json"))
	var saved pkgFile
	if json.Unmarshal(b, &saved) != nil || saved.Covers[a.Key] == nil || saved.Covers[a.Key].Entry != c.Entry || core.StatOK(filepath.Join(core.DataDir, "pkgcovers.json.tmp")) {
		t.Errorf("pkgcovers.json: %s", b)
	}
	pkgMu.Lock()
	pkgAll = nil
	pkgMu.Unlock()
	if PackageCover(st, a.Key) != file {
		t.Error("after a restart")
	}

	// the run after a scan: the empty one fails and is remembered, the zipped one is read, the pictured and
	// the one done already are left alone
	startPkgBatch(st, false)
	startPkgBatch(st, false) // (asked again meanwhile: once more after, finding nothing to do)
	for i := 0; i < 400 && PkgBatchSnapshot().Running; i++ {
		time.Sleep(25 * time.Millisecond)
	}
	snap := PkgBatchSnapshot()
	if snap.Running || snap.Total != 0 || snap.Ended == 0 { // the second round
		t.Fatalf("batch: %+v", snap)
	}
	hat := PackageCoverInfo(st, byName["Hat"].Key)
	if hat == nil || !strings.Contains(hat.Package, "Hat.zip!") || hat.Entry != "Assets/X/Hat.prefab" {
		t.Errorf("from the zip: %+v", hat)
	}
	if s := PkgCoverStatusOf(st, byName["Empty Pack"].Key); s.Failed == "" || s.Cover != nil {
		t.Errorf("empty pack: %+v", s)
	}
	// nothing to do the second time; the explicit run tries the failed ones again
	if todo := pkgTodos(st, false); len(todo) != 0 {
		t.Errorf("todo again: %d", len(todo))
	}
	if todo := pkgTodos(st, true); len(todo) != 2 {
		t.Errorf("todo by hand: %d", len(todo))
	}
	// the package changed: read again
	time.Sleep(20 * time.Millisecond)
	writePkg(t, empty, []pkgEnt{{guid: g("5"), path: "Assets/E/Empty.prefab", preview: shotPNG(t, color.RGBA{90, 200, 90, 255}, 40), body: 4000}})
	if todo := pkgTodos(st, false); len(todo) != 1 || todo[0].key != byName["Empty Pack"].Key {
		t.Errorf("after a change: %d", len(todo))
	}
	runPkgBatch(st, false)
	if PackageCover(st, byName["Empty Pack"].Key) == "" {
		t.Error("not read again after it changed")
	}
	// removed by the player: gone, and not read again by itself — the button does
	if !RemovePkgCover(st, a.Key) || PackageCover(st, a.Key) != "" || core.StatOK(file) || RemovePkgCover(st, a.Key) {
		t.Error("removing it")
	}
	if v := BuildView(st, a); v.Cover != "" || v.CoverPkg {
		t.Errorf("cover after removal: %q", v.Cover)
	}
	if s := PkgCoverStatusOf(st, a.Key); !s.Off {
		t.Errorf("not marked as removed: %+v", s)
	}
	for _, t2 := range pkgTodos(st, true) {
		if t2.key == a.Key {
			t.Error("read again by itself after the player removed it")
		}
	}
	if _, err := ExtractPkgCover(st, a.Key); err != nil || PackageCover(st, a.Key) == "" {
		t.Errorf("the button: %v", err)
	}
	if s := PkgCoverStatusOf(st, a.Key); s.Off {
		t.Error("still marked as removed")
	}
	if _, err := ExtractPkgCover(st, "name:nothing"); err == nil {
		t.Error("an asset that is not there")
	}
}

// A synthetic 300 MB package with 2,000 entries: how long one read takes (VRCLIB_BIG=1 to run it).
func TestPickPreviewBig(t *testing.T) {
	if os.Getenv("VRCLIB_BIG") == "" {
		t.Skip("VRCLIB_BIG=1 runs it")
	}
	dir := t.TempDir()
	pkg := filepath.Join(dir, "big.unitypackage")
	f, err := os.Create(pkg)
	if err != nil {
		t.Fatal(err)
	}
	gz, _ := gzip.NewWriterLevel(f, gzip.BestSpeed)
	tw := tar.NewWriter(gz)
	put := func(name string, b []byte) {
		_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(b)), Typeflag: tar.TypeReg})
		_, _ = tw.Write(b)
	}
	rnd := rand.New(rand.NewSource(1))
	body := make([]byte, 150*1024) // 2,000 × 150 KB ≈ 300 MB, most of it hard to compress, like textures
	start := time.Now()
	for i := 0; i < 2000; i++ {
		guid := strings.Repeat("d", 24) + fmtHex(i)
		if i%4 != 0 {
			rnd.Read(body)
		} else {
			for j := range body {
				body[j] = byte(j)
			}
		}
		put("./"+guid+"/asset", body)
		put("./"+guid+"/asset.meta", []byte("fileFormatVersion: 2\n"))
		p := "Assets/Big/Tex" + fmtHex(i) + ".png"
		pv := texPNG(t, int64(i))
		if i == 1500 {
			p, pv = "Assets/Big/Big.prefab", shotPNG(t, color.RGBA{200, 120, 220, 255}, 46)
		}
		put("./"+guid+"/pathname", []byte(p+"\n00"))
		put("./"+guid+"/preview.png", pv)
	}
	tw.Close()
	gz.Close()
	f.Close()
	fi, _ := os.Stat(pkg)
	t.Logf("written in %v: %.1f MB", time.Since(start), float64(fi.Size())/(1<<20))
	start = time.Now()
	pk, err := pickFrom(t, pkg, "Big")
	if err != nil || pk.entry != "Assets/Big/Big.prefab" {
		t.Fatalf("picked %+v %v", pk, err)
	}
	t.Logf("read in %v", time.Since(start))
}

// a speck in the middle of a prefab preview is cut out and enlarged; a preview that fills its frame is left alone
func TestFramePreview(t *testing.T) {
	draw := func(x0, y0, x1, y1 int) (image.Image, []byte) {
		img := image.NewNRGBA(image.Rect(0, 0, 128, 128))
		for y := 0; y < 128; y++ {
			for x := 0; x < 128; x++ {
				c := color.NRGBA{82, 82, 82, 255}
				if x >= x0 && x < x1 && y >= y0 && y < y1 {
					c = color.NRGBA{220, uint8(40 + x), 60, 255}
				}
				img.SetNRGBA(x, y, c)
			}
		}
		var b bytes.Buffer
		png.Encode(&b, img)
		return img, b.Bytes()
	}
	img, data := draw(56, 44, 72, 84) // 16 × 40 in the middle
	out, err := png.Decode(bytes.NewReader(framePreview(img, data)))
	if err != nil || out.Bounds().Dx() != 200 || out.Bounds().Dy() != 200 {
		t.Fatalf("speck: %v %v", err, out.Bounds())
	}
	if r, _, _, _ := out.At(100, 100).RGBA(); r>>8 < 200 {
		t.Errorf("the subject is not in the middle: %d", r>>8)
	}
	if r, _, _, _ := out.At(4, 100).RGBA(); r>>8 != 82 {
		t.Errorf("the margin is not background: %d", r>>8)
	}
	img, data = draw(10, 10, 118, 118)
	if got := framePreview(img, data); !bytes.Equal(got, data) {
		t.Error("a full preview was changed")
	}
	img, data = draw(0, 0, 0, 0)
	if got := framePreview(img, data); !bytes.Equal(got, data) {
		t.Error("an empty preview was changed")
	}
	img, data = draw(2, 100, 22, 126) // in a corner: the square stays inside the frame
	if out, err := png.Decode(bytes.NewReader(framePreview(img, data))); err != nil || out.Bounds().Dx() != 128 {
		t.Errorf("corner: %v %v", err, out.Bounds())
	}
}

// runBatch: one run over the library, here and now, counted from nothing.
func runBatch(st *core.Store) {
	pkgJobMu.Lock()
	pkgJob = PkgBatch{}
	pkgJobMu.Unlock()
	runPkgBatch(st, false)
}

// bombPNG: a PNG whose header declares w × h (16-bit RGBA) and holds next to nothing.
func bombPNG(w, h uint32) []byte {
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	chunk := func(typ string, data []byte) {
		_ = binary.Write(&b, binary.BigEndian, uint32(len(data)))
		b.WriteString(typ)
		b.Write(data)
		c := crc32.NewIEEE()
		c.Write([]byte(typ))
		c.Write(data)
		_ = binary.Write(&b, binary.BigEndian, c.Sum32())
	}
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 16, 6
	chunk("IHDR", ihdr)
	chunk("IDAT", []byte{0x78, 0x9c, 0x63, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01}) // a tiny zlib stream
	chunk("IEND", nil)
	return b.Bytes()
}

// A preview whose header declares a huge picture is turned down by its header: nothing of that size is
// allocated (40000 × 40000 would end the program, not the test).
func TestPreviewSizeFromHeader(t *testing.T) {
	for _, side := range []uint32{1025, 12000, 40000} {
		p := filepath.Join(t.TempDir(), "x.unitypackage")
		writePkg(t, p, []pkgEnt{{guid: g("1"), path: "Assets/X/X.prefab", preview: bombPNG(side, side), body: 10}})
		var m0, m1 runtime.MemStats
		runtime.ReadMemStats(&m0)
		_, err := pickFrom(t, p, "X")
		runtime.ReadMemStats(&m1)
		if err != errPkgNone {
			t.Errorf("%d px a side: %v", side, err)
		}
		if grew := (m1.TotalAlloc - m0.TotalAlloc) >> 20; grew > 32 {
			t.Errorf("%d px a side: %d MB allocated for a preview that is not taken", side, grew)
		}
	}
	// the smallest and the largest that are taken, and one beside each
	for side, want := range map[int]bool{15: false, 16: true, 1024: true} {
		img := image.NewRGBA(image.Rect(0, 0, side, side))
		for y := 0; y < side; y++ {
			for x := 0; x < side; x++ {
				img.Set(x, y, color.RGBA{uint8(x * 255 / side), uint8(y * 255 / side), uint8((x ^ y) * 16), 255})
			}
		}
		got := scorePreview("Assets/X/X.prefab", encPNG(t, img), []string{"x"}, nil) != nil
		if got != want {
			t.Errorf("%d px a side: taken %v, want %v", side, got, want)
		}
	}
}

// A package the program did not come back from reading is not read again by itself: the run writes it down
// before it reads, and takes the note away when the read ends, whichever way.
func TestPkgReadingIsNoted(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "Sailor Dress", "SailorDress.unitypackage")
	_ = os.MkdirAll(filepath.Dir(p), 0755)
	writePkg(t, p, []pkgEnt{{guid: g("6"), path: "Assets/Shop/SailorDress/SailorDress.prefab", preview: shotPNG(t, color.RGBA{230, 230, 240, 255}, 48), body: 300}})
	st, byName := scanFor(t, root)
	a, dir := byName["Sailor Dress"], dataDirOf(st)
	// what the open sees is what a start after a crash would find
	var during *PkgFail
	open := pkgOpenFile
	pkgOpenFile = func(p string) (io.ReadCloser, error) {
		pkgMu.RLock()
		during = pkgCovers(dir).Failed[a.Key]
		pkgMu.RUnlock()
		var onDisk pkgFile
		b, _ := os.ReadFile(pkgCoverFile(dir))
		if json.Unmarshal(b, &onDisk) != nil || onDisk.Failed[a.Key] == nil || onDisk.Failed[a.Key].Why != pkgWhyCrashed {
			t.Errorf("not on the disk before the read: %s", b)
		}
		return open(p)
	}
	defer func() { pkgOpenFile = open }()
	runBatch(st)
	if during == nil || during.Why != pkgWhyCrashed || during.Sig != pkgSig(a) {
		t.Fatalf("while reading: %+v", during)
	}
	if s := PkgCoverStatusOf(st, a.Key); s.Cover == nil || s.Failed != "" {
		t.Errorf("after a read that went well: %+v", s)
	}
	// the state a crash leaves: not read by itself, read when asked for, and when the package changes
	RemovePkgCover(st, a.Key)
	pkgMu.Lock()
	delete(pkgAll.Off, a.Key)
	pkgMu.Unlock()
	setPkgFail(dir, a.Key, pkgSig(a), pkgWhyCrashed)
	if todo := pkgTodos(st, false); len(todo) != 0 {
		t.Errorf("read again by itself after a crash: %d", len(todo))
	}
	if todo := pkgTodos(st, true); len(todo) != 1 {
		t.Errorf("not read when asked for: %d", len(todo))
	}
}

// "!" is an ordinary character of a path: a cover from a package under "NEW! Dress" or "!VRC" is not taken
// for one out of a zip, found stale and read again after every scan.
func TestPkgCoverBangPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "!VRC")
	p := filepath.Join(root, "NEW! Sailor Dress", "SailorDress.unitypackage")
	_ = os.MkdirAll(filepath.Dir(p), 0755)
	writePkg(t, p, []pkgEnt{{guid: g("6"), path: "Assets/Shop/SailorDress/SailorDress.prefab", preview: shotPNG(t, color.RGBA{230, 230, 240, 255}, 48), body: 300}})
	// a package in a zip, under a folder with a "!" and in a zip with one
	inner := filepath.Join(t.TempDir(), "Hat.unitypackage")
	writePkg(t, inner, []pkgEnt{{guid: g("c"), path: "Assets/X/Hat.prefab", preview: shotPNG(t, color.RGBA{250, 200, 80, 255}, 44), body: 10}})
	ib, _ := os.ReadFile(inner)
	zp := filepath.Join(root, "SALE! Hat.ZIP")
	if err := os.WriteFile(zp, testkit.ZipBytes(t, map[string][]byte{"Hat!/Hat.unitypackage": ib}), 0644); err != nil {
		t.Fatal(err)
	}
	st, byName := scanFor(t, root)
	if len(st.Assets) != 2 {
		t.Fatalf("assets: %v", keysOf(byName))
	}
	runBatch(st)
	for _, a := range st.Assets {
		if PackageCover(st, a.Key) == "" {
			t.Fatalf("%q got no cover", a.Name)
		}
	}
	for round := 1; round <= 2; round++ {
		RunFolderScan(st, &core.Task{})
		if todo := pkgTodos(st, false); len(todo) != 0 {
			t.Errorf("round %d: %d read again", round, len(todo))
		}
	}
	for _, a := range st.Assets {
		c := PackageCoverInfo(st, a.Key)
		want := p
		if strings.Contains(c.Package, ".ZIP!") {
			want = zp
		}
		if got := pkgCoverSource(c.Package); got != want {
			t.Errorf("the file of %q: %q", c.Package, got)
		}
	}
	// the package changed, or went: read again
	time.Sleep(20 * time.Millisecond)
	writePkg(t, p, []pkgEnt{{guid: g("6"), path: "Assets/Shop/SailorDress/SailorDress.prefab", preview: shotPNG(t, color.RGBA{90, 200, 90, 255}, 40), body: 4000}})
	if todo := pkgTodos(st, false); len(todo) != 1 {
		t.Errorf("after a change: %d", len(todo))
	}
	if got := pkgCoverSource(filepath.Join(root, "gone.zip") + "!x.unitypackage"); got != filepath.Join(root, "gone.zip")+"!x.unitypackage" {
		t.Errorf("a zip that is gone: %q", got)
	}
}

// A read that was cancelled half way keeps nothing: the best preview of the first half of a package (here a
// normal map) would stand as the cover of the whole and never be read again.
func TestPkgCoverCancelKeepsNothing(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "Sailor Dress", "SailorDress.unitypackage")
	_ = os.MkdirAll(filepath.Dir(p), 0755)
	writePkg(t, p, []pkgEnt{
		{guid: g("1"), path: "Assets/Shop/SailorDress/Textures/body_normal.png", preview: texPNG(t, 2), body: 2000},
		{guid: g("2"), path: "Assets/Shop/SailorDress/Mat/filler.mat", body: 2000},
		{guid: g("6"), path: "Assets/Shop/SailorDress/SailorDress.prefab", preview: shotPNG(t, color.RGBA{230, 230, 240, 255}, 48), body: 300},
	})
	st, byName := scanFor(t, root)
	a := byName["Sailor Dress"]
	as, names := pkgAssetOf(st, a.Key)
	calls := 0
	c, err := extractPkgCover(dataDirOf(st), a.Key, as, names, func() bool { calls++; return calls > 6 }) // stops after a few entries
	if c != nil || err != errPkgCancelled {
		t.Fatalf("cancelled half way: %+v %v", c, err)
	}
	if PackageCover(st, a.Key) != "" || PkgCoverStatusOf(st, a.Key).Failed != "" {
		t.Errorf("something was kept: %+v", PkgCoverStatusOf(st, a.Key))
	}
	if todo := pkgTodos(st, false); len(todo) != 1 {
		t.Errorf("not read at the next scan: %d", len(todo))
	}
	// the same through the run, cancelled while it reads: neither a cover nor a failure, and the note is gone
	open := pkgOpenFile
	pkgOpenFile = func(p string) (io.ReadCloser, error) { CancelPkgBatch(); return open(p) }
	runBatch(st)
	pkgOpenFile = open
	snap := PkgBatchSnapshot()
	if s := PkgCoverStatusOf(st, a.Key); s.Cover != nil || s.Failed != "" || snap.Made != 0 || snap.Failed != 0 || snap.Err == "" {
		t.Errorf("a cancelled run: %+v %+v", s, snap)
	}
	if c, err := ExtractPkgCover(st, a.Key); err != nil || c.Entry != "Assets/Shop/SailorDress/SailorDress.prefab" {
		t.Errorf("a full read: %+v %v", c, err)
	}
}

// A package that cannot be opened or read just now (held by another program, still being written by a
// download) is not written down as having nothing in it: its size and time have not changed when it can be
// read, and it would never be looked at again.
func TestPkgCoverDiskErrorNotKept(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "Sailor Dress", "SailorDress.unitypackage")
	_ = os.MkdirAll(filepath.Dir(p), 0755)
	writePkg(t, p, []pkgEnt{{guid: g("6"), path: "Assets/Shop/SailorDress/SailorDress.prefab", preview: shotPNG(t, color.RGBA{230, 230, 240, 255}, 48), body: 300}})
	empty := filepath.Join(root, "Empty Pack", "Empty.unitypackage")
	_ = os.MkdirAll(filepath.Dir(empty), 0755)
	writePkg(t, empty, []pkgEnt{{guid: g("5"), path: "Assets/E/Empty.prefab", preview: flatPNG(t, color.RGBA{0x50, 0x50, 0x54, 255}), body: 300}})
	st, byName := scanFor(t, root)
	a := byName["Sailor Dress"]
	open := pkgOpenFile
	defer func() { pkgOpenFile = open }()
	busy := &fs.PathError{Op: "open", Path: p, Err: errors.New("The process cannot access the file because it is being used by another process.")}
	for what, f := range map[string]func(string) (io.ReadCloser, error){
		"cannot be opened": func(q string) (io.ReadCloser, error) {
			if q == p {
				return nil, busy
			}
			return open(q)
		},
		"cannot be read to its end": func(q string) (io.ReadCloser, error) {
			rc, err := open(q)
			if q != p || err != nil {
				return rc, err
			}
			return &failAfter{rc, 64, busy}, nil
		},
	} {
		pkgOpenFile = f
		runBatch(st)
		snap := PkgBatchSnapshot()
		if s := PkgCoverStatusOf(st, a.Key); s.Cover != nil || s.Failed != "" {
			t.Errorf("%s: kept %+v", what, s)
		}
		if snap.Failed == 0 || !strings.Contains(snap.Last, "无法读取 unitypackage") {
			t.Errorf("%s: the run does not say so: %+v", what, snap)
		}
		todo := pkgTodos(st, false)
		if len(todo) != 1 || todo[0].key != a.Key { // (the empty one is remembered: it was read, and has nothing)
			t.Errorf("%s: to read at the next scan: %d", what, len(todo))
		}
	}
	pkgOpenFile = open
	runBatch(st)
	if PackageCover(st, a.Key) == "" {
		t.Error("not read once it can be")
	}
	// a file that ends early is damaged, not busy: remembered until it changes
	b, _ := os.ReadFile(p)
	RemovePkgCover(st, a.Key)
	pkgMu.Lock()
	delete(pkgAll.Off, a.Key)
	pkgMu.Unlock()
	if err := os.WriteFile(p, b[:len(b)/3], 0644); err != nil {
		t.Fatal(err)
	}
	runBatch(st)
	if s := PkgCoverStatusOf(st, a.Key); s.Failed == "" || len(pkgTodos(st, false)) != 0 {
		t.Errorf("a damaged package: %+v", s)
	}
}

// failAfter: a file that reads n bytes and then gives err.
type failAfter struct {
	io.ReadCloser
	n   int
	err error
}

func (f *failAfter) Read(p []byte) (int, error) {
	if f.n <= 0 {
		return 0, f.err
	}
	if len(p) > f.n {
		p = p[:f.n]
	}
	n, err := f.ReadCloser.Read(p)
	f.n -= n
	return n, err
}

// The window is told of the covers made so far when there are new ones, not every few seconds for the rest
// of the run (each telling makes it load every view again).
func TestPkgBatchTellsOnlyNews(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "A Dress", "ADress.unitypackage")
	_ = os.MkdirAll(filepath.Dir(p), 0755)
	writePkg(t, p, []pkgEnt{{guid: g("6"), path: "Assets/Shop/ADress/ADress.prefab", preview: shotPNG(t, color.RGBA{230, 230, 240, 255}, 48), body: 300}})
	for _, n := range []string{"B Pack", "C Pack", "D Pack", "E Pack"} {
		q := filepath.Join(root, n, "Empty.unitypackage")
		_ = os.MkdirAll(filepath.Dir(q), 0755)
		writePkg(t, q, []pkgEnt{{guid: g("5"), path: "Assets/E/Empty.prefab", preview: flatPNG(t, color.RGBA{0x50, 0x50, 0x54, 255}), body: 300}})
	}
	st, _ := scanFor(t, root)
	if todo := pkgTodos(st, false); len(todo) != 5 || todo[0].name != "A Dress" {
		t.Fatalf("to read: %d", len(todo))
	}
	every := pkgShowEvery
	pkgShowEvery = 0
	defer func() { pkgShowEvery = every }()
	rev := core.CurRev()
	runBatch(st)
	if snap := PkgBatchSnapshot(); snap.Made != 1 || snap.Failed != 4 {
		t.Fatalf("run: %+v", snap)
	}
	if told := core.CurRev() - rev; told != 1 {
		t.Errorf("the window was told %d times of one cover", told)
	}
}

// A library import (or its undo) drops the list in memory at any moment. A change made then goes into the
// list as the import left it — it is not lost, does not put the old list back, and does not bring the program
// down with the lock held.
func TestPkgCoversAcrossImport(t *testing.T) {
	core.DataDir = t.TempDir()
	dir := core.DataDir
	setPkgFail(dir, "old", "sig", "why")
	old := pkgCovers(dir) // (what a caller looked at before the import)
	// the import writes its own file and tells everyone
	if err := os.WriteFile(pkgCoverFile(dir), []byte(`{"version":1,"covers":{},"failed":{"imported":{"sig":"s","why":"w","at":1}}}`), 0644); err != nil {
		t.Fatal(err)
	}
	for _, f := range dataReloaders {
		f()
	}
	setPkgFail(dir, "new", "sig", "why")
	var onDisk pkgFile
	b, _ := os.ReadFile(pkgCoverFile(dir))
	if json.Unmarshal(b, &onDisk) != nil || onDisk.Failed["imported"] == nil || onDisk.Failed["new"] == nil || onDisk.Failed["old"] != nil {
		t.Errorf("after the import: %s", b)
	}
	if now := pkgCovers(dir); now == old || now.Failed["new"] == nil {
		t.Error("the change went into the list the import dropped")
	}
	// the same for the covers drawn in Unity
	if err := SetGeneratedCover("k1", pngBytes(64, 64), "Assets/A.prefab", "/proj"); err != nil {
		t.Fatal(err)
	}
	for _, f := range dataReloaders {
		f()
	}
	genMu.Lock()
	gone := genAll == nil
	genMu.Unlock()
	if err := SetGeneratedCover("k2", pngBytes(64, 64), "Assets/B.prefab", "/proj"); err != nil || !gone {
		t.Fatalf("after the import: %v (dropped: %v)", err, gone)
	}
	if GeneratedCover("k1") == "" || GeneratedCover("k2") == "" || !RemoveGeneratedCover("k1") {
		t.Error("generated covers after the import")
	}

	// both at once, for a while: no panic, and the locks are free afterwards
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				for _, f := range dataReloaders {
					f()
				}
			}
		}
	}()
	panicked := ""
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = fmt.Sprint(r)
			}
		}()
		png := pngBytes(64, 64)
		for i, end := 0, time.Now().Add(1500*time.Millisecond); time.Now().Before(end); i++ {
			k := fmt.Sprint("k", i%50)
			setPkgFail(dir, k, "sig", "why")
			_ = swapPkgFail(dir, k, nil)
			if i%20 == 0 {
				_ = SetGeneratedCover(k, png, "", "")
				RemoveGeneratedCover(k)
			}
		}
	}()
	close(stop)
	wg.Wait()
	if panicked != "" {
		t.Errorf("panic: %s", panicked)
	}
	free := make(chan bool, 1)
	go func() { pkgMu.Lock(); pkgMu.Unlock(); genMu.Lock(); genMu.Unlock(); free <- true }()
	select {
	case <-free:
	case <-time.After(2 * time.Second):
		t.Fatal("a lock is still held: every later look at a cover would wait for ever")
	}
}

// An import cancels the run over the library and waits for it; no run starts while the import is under way.
func TestPkgBatchStopsForImport(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 12; i++ {
		p := filepath.Join(root, fmt.Sprintf("Dress %c", 'A'+i), "pkg.unitypackage")
		_ = os.MkdirAll(filepath.Dir(p), 0755)
		writePkg(t, p, []pkgEnt{{guid: g("6"), path: "Assets/Shop/D/D.prefab", preview: shotPNG(t, color.RGBA{230, 230, 240, 255}, 48), body: 300}})
	}
	st, _ := scanFor(t, root)
	open := pkgOpenFile
	pkgOpenFile = func(p string) (io.ReadCloser, error) { time.Sleep(30 * time.Millisecond); return open(p) }
	defer func() { pkgOpenFile = open }()
	startPkgBatch(st, false)
	startPkgBatch(st, false) // (asked for once more meanwhile: not after a stop)
	if !PkgBatchSnapshot().Running {
		t.Fatal("the run did not start")
	}
	if !StopPkgBatch(10 * time.Second) {
		t.Fatal("the run did not stop")
	}
	if snap := PkgBatchSnapshot(); snap.Running || snap.Done >= 12 {
		t.Errorf("after the stop: %+v", snap)
	}
	if !StopPkgBatch(0) {
		t.Error("nothing runs: nothing to wait for")
	}
	if !transferBegin("import", "") {
		t.Fatal("transfer")
	}
	startPkgBatch(st, false)
	err := StartPkgBatch(st)
	running := PkgBatchSnapshot().Running
	transferSet(func(r *TransferRun) { r.Running = false })
	if running || err == nil {
		t.Errorf("a run started under an import (%v)", err)
	}
}

// What a name says is read word by word: a helper's word inside another word does not make a prefab a part.
func TestNameWords(t *testing.T) {
	for in, want := range map[string]string{
		"SailorDress_PhysBone": "sailor dress phys bone",
		"sailorDress-v1.2":     "sailor dress v 1 2",
		"FXLayer":              "fx layer",
		"VRCFury Toggle":       "vrc fury toggle",
		"Test01":               "test 01",
		"NDMF":                 "ndmf",
		"body_N":               "body n",
		"メインicon(2)":           "メイン icon 2",
		"":                     "",
	} {
		if got := nameWords(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	for name, want := range map[string]bool{
		"Animal Ears": false, "Anime Eyes": false, "Silicone Body": false, "Latest": false, "Trombone": false,
		"Parametric Dress": false, "Menuet": false, "Boneless": false, "Shadery": false, "Effects": false,
		"Dress_PhysBone": true, "Dress PhysBones": true, "Dress_FX": true, "FXLayer": true, "MenuIcons": true, "Kikyo_Anim": true,
		"Kikyo Animator": true, "test01": true, "Dress_Colliders": true, "(VRCFury) PCS": true, "lilToon": true, "Params": true,
		"ExpressionParameters": true, "Toggles": true, "Hat Bone": true, "Sample": true,
	} {
		if got := isPartName(name); got != want {
			t.Errorf("%q a part: %v", name, got)
		}
	}
}

// A texture is taken for what its name says, not for being the only picture there: a menu glyph in a folder of
// icons, a matcap or a map does not stand for the asset, however much there is to see in it.
func TestTextureMustBeNamed(t *testing.T) {
	keys := nameKeys([]string{"DMCustom_PCS_v1.9.2", "Sailor Dress"})
	tex := texPNG(t, 3)
	for p, want := range map[string]bool{
		"Assets/!Dismay Custom/PCS/Assets/Icons/custom.png": false, // ("custom" is in the shop's name: still an icon)
		"Assets/Shop/PCS/icon/1.png":                        false,
		"Assets/Shop/PCS/MenuIcons/SailorDress.png":         false,
		"Assets/Shop/SailorDress/Textures/ring.png":         false, // says nothing
		"Assets/Shop/SailorDress/Textures/Remain.png":       false, // ("main" is not a word of it)
		"Assets/Shop/SailorDress/Textures/Discover.png":     false,
		"Assets/Shop/SailorDress/Textures/Silicone.png":     false,
		"Assets/Shop/SailorDress/SailorDressNormal.png":     false, // a map, also written without "_"
		"Assets/Shop/SailorDress/SailorDress_Matcap01.png":  false,
		"Assets/Shop/SailorDress/SailorDress_Icon.png":      false,
		"Assets/Shop/SailorDress/Textures/SailorDress.png":  true, // named like the asset
		"Assets/Shop/SailorDress/Textures/Sailor.png":       true,
		"Assets/Shop/SailorDress/Thumbnail.png":             true, // named like a picture of it
		"Assets/Shop/SailorDress/Doc/preview_01.jpg":        true,
		"Assets/Shop/SailorDress/a/b/c/d/e/f/Main.png":      true, // (how deep it lies does not decide it)
		"Assets/Shop/SailorDress/商品サムネ.png":                 true,
	} {
		if got := scorePreview(p, tex, keys, nil) != nil; got != want {
			t.Errorf("%s: taken %v, want %v", p, got, want)
		}
	}
	// a package like the one this was found in: prefabs Unity could not draw, a mesh, and textures for a menu
	dir := t.TempDir()
	pkg := filepath.Join(dir, "(VRCFury) PCS v1.9.2.unitypackage")
	ents := []pkgEnt{
		{guid: g("1"), path: "Assets/!Dismay Custom/PCS/PCS.prefab", preview: flatPNG(t, color.RGBA{0x50, 0x50, 0x54, 255}), body: 100},
		{guid: g("2"), path: "Assets/!Dismay Custom/PCS/Assets/Icons/custom.png", preview: texPNG(t, 1), body: 100},
		{guid: g("3"), path: "Assets/!Dismay Custom/PCS/Assets/Icons/activate.png", preview: texPNG(t, 2), body: 100},
		{guid: g("4"), path: "Assets/!Dismay Custom/PCS/Assets/Particles/CFX2_T_Heart.png", preview: texPNG(t, 3), body: 100},
	}
	writePkg(t, pkg, ents)
	if pk, err := pickFrom(t, pkg, "DMCustom_PCS_v1.9.2", filepath.Base(pkg)); err != errPkgNone {
		t.Errorf("only icons and a particle to choose from: %+v %v", pk, err)
	}
	writePkg(t, pkg, append(ents, pkgEnt{guid: g("5"), path: "Assets/!Dismay Custom/PCS/Assets/Materials/cone.fbx", preview: shotPNG(t, color.RGBA{200, 200, 200, 255}, 30), body: 100}))
	if pk, err := pickFrom(t, pkg, "DMCustom_PCS_v1.9.2", filepath.Base(pkg)); err != nil || pk.entry != "Assets/!Dismay Custom/PCS/Assets/Materials/cone.fbx" {
		t.Errorf("with a mesh: %+v %v", pk, err)
	}
}
