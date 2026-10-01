package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

func makeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(files[n]))
	}
	_ = zw.Close()
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
}

func listTree(root string) []string {
	var out []string
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			r, _ := filepath.Rel(root, p)
			out = append(out, filepath.ToSlash(r))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func TestExtractZip(t *testing.T) {
	dir := t.TempDir()
	// one folder inside: kept as it is
	z1 := filepath.Join(dir, "Kaguya_v1.07.zip")
	makeZip(t, z1, map[string]string{"Kaguya/Kaguya.unitypackage": "pkg", "Kaguya/readme.txt": "hi"})
	out, err := extractZip(z1, dir)
	if err != nil || filepath.Base(out) != "Kaguya" {
		t.Fatalf("single folder: %v %v", out, err)
	}
	// loose files: a folder named after the zip; "../" cannot escape
	z2 := filepath.Join(dir, "Dress.zip")
	makeZip(t, z2, map[string]string{"Dress.unitypackage": "pkg", "Texture/a.png": "png", "../../evil.txt": "x"})
	out, err = extractZip(z2, dir)
	if err != nil || filepath.Base(out) != "Dress" {
		t.Fatalf("loose files: %v %v", out, err)
	}
	if got := strings.Join(listTree(out), ","); got != "Dress.unitypackage,Texture/a.png,evil.txt" {
		t.Errorf("Dress contents: %s", got)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "evil.txt")); err == nil {
		t.Error("zip slip")
	}
	// unpacking again does not write into the first copy
	out2, err := extractZip(z2, dir)
	if err != nil || filepath.Base(out2) != "Dress (2)" {
		t.Errorf("second unpack: %v %v", out2, err)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(dir, ".*extracting")); len(leftovers) > 0 {
		t.Errorf("temp folders left: %v", leftovers)
	}
}

func TestSafeName(t *testing.T) {
	for in, want := range map[string]string{
		"9000003 【8アバター対応】Moon Dress": "9000003 【8アバター対応】Moon Dress",
		"a/b:c*d?": "a／b：c＊d？",
		"name. ":   "name",
		"CON":      "_CON",
	} {
		if got := safeName(in, 70); got != want {
			t.Errorf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestShopURLs(t *testing.T) {
	bases := []string{"Plum=plum|プラム", "Chocolat=chocolat|ショコラ"}
	q := ShopQuery{Cats: []string{"衣服", "头发"}, Bases: []string{"Plum"}, Styles: []string{"可爱", "H"}, Sort: "new"}
	us := shopURLs(q, bases, defaultStyles)
	if len(us) != 2 {
		t.Fatalf("one address per category: %v", us)
	}
	u0, _ := url.Parse(us[0])
	if u0.Path != "/ja/browse/3D衣装" || u0.Query().Get("q") != "プラム かわいい" || u0.Query().Get("sort") != "new" || u0.Query().Get("adult") != "include" {
		t.Errorf("clothes: %s", us[0])
	}
	u1, _ := url.Parse(us[1])
	if u1.Path != "/ja/browse/3Dモデル" || u1.Query().Get("q") != "プラム かわいい 髪型" {
		t.Errorf("hair: %s", us[1])
	}
	if us := shopURLs(ShopQuery{}, bases, defaultStyles); len(us) != 1 || !strings.Contains(us[0], "sort=wish_lists") {
		t.Errorf("nothing picked: %v", us)
	}
}

func TestDownloadJob(t *testing.T) {
	dataDir = t.TempDir()
	lib := t.TempDir()
	var zipBytes bytes.Buffer
	zw := zip.NewWriter(&zipBytes)
	w, _ := zw.Create("MoonDress/MoonDress.unitypackage")
	_, _ = w.Write([]byte("pkg"))
	_ = zw.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/downloadables/"):
			if !strings.Contains(r.Header.Get("Cookie"), boothSessionCookie+"=good") {
				http.Redirect(w, r, "/users/sign_in", http.StatusFound)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: boothSessionCookie, Value: "good", Path: "/"})
			http.Redirect(w, r, "/cdn/abc/MoonDress_v1.2.zip", http.StatusFound)
		case strings.HasPrefix(r.URL.Path, "/cdn/"):
			_, _ = w.Write(zipBytes.Bytes())
		}
	}))
	defer srv.Close()
	t.Setenv("VRCLIB_BOOTH_DL", srv.URL)
	t.Setenv("VRCLIB_BOOTH_BASE", srv.URL)
	st := &Store{path: filepath.Join(dataDir, "library.json"), Settings: Settings{Roots: []string{lib}}, Purchases: map[string]*Purchase{
		"9000003": {ID: "9000003", Name: "【8アバター対応】Moon Dress", Files: []string{"MoonDress_v1.2.zip"}, Downloads: []string{"90000030"}}}}

	// no login saved yet
	if _, _, err := resolveDownload(st, "90000030"); !errors.Is(err, errNeedLogin) {
		t.Fatalf("without a login: %v", err)
	}
	_ = saveBoothSession([]savedCookie{{Name: boothSessionCookie, Value: "bad"}})
	if _, _, err := resolveDownload(st, "90000030"); !errors.Is(err, errNeedLogin) {
		t.Fatalf("expired login: %v", err)
	}
	_ = saveBoothSession([]savedCookie{{Name: boothSessionCookie, Value: "good", Expires: time.Now().Add(time.Hour).Unix()}})
	j := &DLJob{ID: "90000030", Item: "9000003", Name: "MoonDress_v1.2.zip", Status: "running"}
	if err := downloadJob(st, j); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(lib, "9000003 【8アバター対応】Moon Dress") // the item's folder: everything unpacks in there
	if j.Path != want || j.Status != "done" {
		t.Errorf("job: %+v", j)
	}
	if got := strings.Join(listTree(filepath.Join(lib, "9000003 【8アバター対応】Moon Dress")), ","); got != "MoonDress/MoonDress.unitypackage" {
		t.Errorf("item folder (zip removed after unpacking): %s", got)
	}
	if boothIDFromName("9000003 【8アバター対応】Moon Dress") != "9000003" {
		t.Error("the folder name has to carry the item id")
	}
	if st.Downloaded["90000030"] == nil {
		t.Error("not recorded")
	}
}

// Zips made on Japanese Windows keep Shift-JIS names without the UTF-8 flag.
func TestShiftJISZip(t *testing.T) {
	dir := t.TempDir()
	sjis := []byte{0x82, 0xd3, 0x82, 0xed, 0x82, 0xd3, 0x82, 0xed, 0x90, 0x4b, 0x94, 0xf6, 0x2f, 0x90, 0x4b, 0x94, 0xf6, 0x2e, 0x75, 0x6e, 0x69, 0x74, 0x79, 0x70, 0x61, 0x63, 0x6b, 0x61, 0x67, 0x65}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.CreateHeader(&zip.FileHeader{Name: string(sjis), NonUTF8: true, Method: zip.Deflate})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("pkg"))
	_ = zw.Close()
	zp := filepath.Join(dir, "fluffy_tail.zip")
	_ = os.WriteFile(zp, buf.Bytes(), 0644)
	out, err := extractZip(zp, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := decodeCP932(sjis); !ok {
		t.Skip("no code page conversion on this system")
	}
	if filepath.Base(out) != "ふわふわ尻尾" {
		t.Errorf("folder %q", filepath.Base(out))
	}
	if _, err := os.Stat(filepath.Join(out, "尻尾.unitypackage")); err != nil {
		t.Error(err)
	}
}

func TestSessionFile(t *testing.T) {
	dataDir = t.TempDir()
	cs := []savedCookie{{Name: boothSessionCookie, Value: "abc", Expires: time.Now().Add(time.Hour).Unix()}, {Name: "old", Value: "x", Expires: 1}}
	if err := saveBoothSession(cs); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(sessionFile())
	if runtime.GOOS == "windows" && bytes.Contains(raw, []byte("abc")) {
		t.Error("the login is stored readable")
	}
	got := loadBoothSession()
	if len(got) != 1 || got[0].Value != "abc" {
		t.Errorf("loaded %+v", got)
	}
	if h := cookieHeader(got); h != "adult=t; "+boothSessionCookie+"=abc" {
		t.Errorf("header %q", h)
	}
}
