package booth

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
)

// pictures of each kind a cover can be
func testPictures() (pngB, jpgB, gifB, webpB []byte) {
	var p, g bytes.Buffer
	_ = png.Encode(&p, image.NewRGBA(image.Rect(0, 0, 4, 4)))
	_ = gif.Encode(&g, image.NewPaletted(image.Rect(0, 0, 4, 4), color.Palette{color.Black, color.White}), nil)
	webpB = []byte("RIFF\x24\x00\x00\x00WEBPVP8 \x18\x00\x00\x00" + strings.Repeat("\x00", 24)) // (what a WebP begins with)
	return p.Bytes(), testkit.JPEG(4, 4, color.RGBA{R: 255, A: 255}), g.Bytes(), webpB
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	ents, _ := os.ReadDir(dir)
	var out []string
	for _, e := range ents {
		if !e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// A cover is kept only when it is a picture, and under the extension of what it is: a page, an SVG or a script
// is refused whatever its address and the server say, and leaves nothing behind.
func TestDownloadToKeepsOnlyPictures(t *testing.T) {
	core.DataDir = t.TempDir()
	pngB, jpgB, gifB, webpB := testPictures()
	files := map[string][]byte{
		"/a.png": pngB, "/b.svg": pngB, "/c": jpgB, "/d.jpg": gifB, "/e.html": webpB,
		"/page.jpg":   []byte("<!DOCTYPE html><html><script>alert(1)</script></html>"),
		"/vector.png": []byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`),
		"/script.gif": []byte("alert(document.cookie)"),
		"/data.webp":  []byte(`<?xml version="1.0"?><x/>`),
		"/empty.jpg":  nil,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png") // (what the server says counts for nothing)
		_, _ = w.Write(b)
	}))
	defer srv.Close()
	c := srv.Client()
	for path, ext := range map[string]string{"/a.png": ".png", "/b.svg?x=1": ".png", "/c": ".jpg", "/d.jpg?f=.gif": ".gif", "/e.html": ".webp"} {
		name := "booth_" + strings.Trim(path[:2], "/")
		p, err := DownloadTo(c, srv.URL+path, name)
		want := filepath.Join(core.CoversDir(), name+ext)
		if err != nil || p != want {
			t.Errorf("%s: %q %v, want %q", path, p, err, want)
			continue
		}
		if b, _ := os.ReadFile(p); !bytes.Equal(b, files[strings.SplitN(path, "?", 2)[0]]) {
			t.Errorf("%s: kept %d bytes that are not the picture", path, len(b))
		}
	}
	for _, path := range []string{"/page.jpg", "/vector.png", "/script.gif", "/data.webp", "/empty.jpg", "/missing.png"} {
		if p, err := DownloadTo(c, srv.URL+path, "remote_"+strings.Trim(path[:2], "/")); err == nil {
			t.Errorf("%s was kept as %s", path, p)
		}
	}
	if got := strings.Join(listDir(t, core.CoversDir()), " "); got != "booth_a.png booth_b.png booth_c.jpg booth_d.gif booth_e.webp" {
		t.Errorf("covers: %s", got)
	}
}

// The cache of Booth's pictures gives only pictures: what an older version left there under another extension
// goes at the first look, and a file under a picture's name that is not that picture is fetched again.
func TestRemoteThumbCacheOnlyPictures(t *testing.T) {
	st := testkit.NewStore(t)
	pngB, _, _, _ := testPictures()
	var asked atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		if r.URL.Path == "/i/thumb.jpg" {
			_, _ = w.Write(pngB) // a PNG at a JPEG's address
			return
		}
		_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
	}))
	defer srv.Close()
	t.Setenv("VRCLIB_BOOTH_WEB", srv.URL)
	dir := filepath.Join(core.CoversDir(), "remote")
	key := func(u string) string { h := sha1.Sum([]byte(u)); return hex.EncodeToString(h[:10]) }
	u := srv.URL + "/i/thumb.jpg"
	old := map[string][]byte{"0a1b.html": []byte("<p>x</p>"), "2c3d.svg": []byte("<svg/>"), "4e5f.jpg.part": pngB, "6a7b.xml": []byte("<x/>"),
		"8c9d.png": pngB, key(u) + ".jpg": []byte("<html>not a picture</html>")}
	_ = os.MkdirAll(dir, 0755)
	for n, b := range old {
		if err := os.WriteFile(filepath.Join(dir, n), b, 0644); err != nil {
			t.Fatal(err)
		}
	}
	p, err := RemoteThumb(st, u)
	if err != nil || p != filepath.Join(dir, key(u)+".png") || asked.Load() != 1 {
		t.Fatalf("thumb: %q %v (asked %d)", p, err, asked.Load())
	}
	if b, _ := os.ReadFile(p); !bytes.Equal(b, pngB) {
		t.Error("the kept file is not the picture")
	}
	want := []string{"8c9d.png", key(u) + ".png"}
	sort.Strings(want)
	if got := listDir(t, dir); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the cache after the first look: %v, want %v", got, want)
	}
	if p2, err := RemoteThumb(st, u); err != nil || p2 != p || asked.Load() != 1 {
		t.Errorf("again: %q %v (asked %d)", p2, err, asked.Load())
	}
	// what is not a picture is not kept, under any name; and Booth's hosts are still the only ones asked
	for _, bad := range []string{srv.URL + "/i/vector.svg", srv.URL + "/i/page.png?x=.jpg", "https://evil.example/x.png", "http://booth.pm/x.png"} {
		if p, err := RemoteThumb(st, bad); err == nil {
			t.Errorf("%s: kept as %s", bad, p)
		}
	}
	if got := len(listDir(t, dir)); got != 2 {
		t.Errorf("the cache holds %d files: %v", got, listDir(t, dir))
	}
}

func TestBoothParse(t *testing.T) {
	live := `{"id":"8562330","name":"九尾オリジナル3Dモデル「輝夜」-kaguya-","description":"desc","price":"¥ 6,000","url":"https://paryi.booth.pm/items/8562330",
	"category":{"id":208,"name":"3Dキャラクター","parent":{"name":"3Dモデル"}},"shop":{"name":"IKUSIA","subdomain":"paryi","thumbnail_url":"x"},
	"tags":[{"name":"髪の毛","url":"u"},{"name":"髪型","url":"u"}],"images":[{"caption":null,"original":"o1","resized":"r1"},{"caption":null,"original":"o2","resized":"r2"}],
	"variations":[{"id":1}],"is_adult":false}`
	it, err := parseBoothItem([]byte(live))
	if err != nil || it.Name == "" || len(it.Tags) != 2 || it.Tags[0] != "髪の毛" || len(it.Images) != 2 || it.Image != "o1" || it.ShopURL != "https://paryi.booth.pm/" || it.Category != "3Dキャラクター" {
		t.Fatalf("%v %+v", err, it)
	}
	old := `{"name":"a","tags":["x","y"],"price":1500,"images":null}`
	it, err = parseBoothItem([]byte(old))
	if err != nil || len(it.Tags) != 2 || it.Price != "1500" {
		t.Fatalf("%v %+v", err, it)
	}
	page := `<section class="shop__text"><h2 class="a">商品説明</h2><p class="b">一行目
二行目 &lt;注意&gt;</p></section><section class="shop__text"><h2 class="a">利用規約</h2><p>VN3<br>再配布禁止</p></section><section class="shop__text"><h2>画像</h2><img src=x></section>`
	got := boothSections([]byte(page))
	want := "【商品説明】\n一行目\n二行目 <注意>\n\n【利用規約】\nVN3\n再配布禁止"
	if got != want {
		t.Fatalf("%q", got)
	}
	if s := trimOgTitle("九尾 - kaguya - IKUSIA - BOOTH"); s != "九尾 - kaguya" {
		t.Fatal(s)
	}
	if s := boothThumb300("https://booth.pximg.net/c/72x72_a2_g5/u/i/1/a_base_resized.jpg"); s != "https://booth.pximg.net/c/300x300_a2_g5/u/i/1/a_base_resized.jpg" {
		t.Fatal(s)
	}
	if s := boothThumb300("https://booth.pximg.net/u/i/1/a_base_resized.jpg"); s != "https://booth.pximg.net/u/i/1/a_base_resized.jpg" {
		t.Fatal(s)
	}
	if s := trimOgTitle("plain"); s != "plain" {
		t.Fatal(s)
	}
}

// A share card whose folder or files carry the Booth item number is that item's — a Google Drive or Dropbox
// card as much as a Baidu one.
func TestAssetBoothOfShareCards(t *testing.T) {
	listing := func(key string) *core.PanListing {
		return &core.PanListing{Surl: key, Title: "8099091", Count: 2, Files: []*core.PanFile{{Name: "8099091", Dir: true, Children: []*core.PanFile{
			{Name: "AONAMI_manuka.zip", Size: 1000}, {Name: "AONAMI_shinano.zip", Size: 1000}}}}}
	}
	st := &core.Store{Settings: core.DefaultSettings(), User: map[string]*core.UserData{}, Pan: map[string]*core.PanListing{
		"1abcDEF":            listing("1abcDEF"),
		"gd:1Folder00000000": listing("gd:1Folder00000000"),
		"db:abc123xyz456789": listing("db:abc123xyz456789"),
	}}
	for _, key := range []string{"pan:1abcDEF", "gd:1Folder00000000", "db:abc123xyz456789"} {
		if id, how := AssetBooth(st, key, nil); id != "8099091" || how != "name" {
			t.Errorf("%s: item %q (%s)", key, id, how)
		}
	}
	if id, _ := AssetBooth(st, "name:8099091", nil); id != "" {
		t.Errorf("a card that is no share's: %q", id)
	}
}
