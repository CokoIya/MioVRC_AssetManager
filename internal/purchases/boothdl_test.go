package purchases

import (
	"archive/zip"
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/naming"
	"vrclib/internal/testkit"
)

func TestDownloadJob(t *testing.T) {
	core.DataDir = t.TempDir()
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
	st := &core.Store{Path: filepath.Join(core.DataDir, "library.json"), Settings: core.Settings{Roots: []string{lib}, DownloadDir: lib}, Purchases: map[string]*core.Purchase{
		"9000003": {ID: "9000003", Name: "【8アバター対応】Moon Dress", Files: []string{"MoonDress_v1.2.zip"}, Downloads: []string{"90000030"}}}}

	// no login saved yet
	if _, _, err := resolveDownload(st, "90000030"); !errors.Is(err, errNeedLogin) {
		t.Fatalf("without a login: %v", err)
	}
	_ = saveBoothSession([]core.SavedCookie{{Name: boothSessionCookie, Value: "bad"}})
	if _, _, err := resolveDownload(st, "90000030"); !errors.Is(err, errNeedLogin) {
		t.Fatalf("expired login: %v", err)
	}
	_ = saveBoothSession([]core.SavedCookie{{Name: boothSessionCookie, Value: "good", Expires: time.Now().Add(time.Hour).Unix()}})
	j := &DLJob{ID: "90000030", Item: "9000003", Name: "MoonDress_v1.2.zip", Status: "running"}
	if err := downloadJob(st, j); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(lib, "9000003 【8アバター対応】Moon Dress") // the item's folder: everything unpacks in there
	if j.Path != want || j.Status != "done" {
		t.Errorf("job: %+v", j)
	}
	if got := strings.Join(testkit.ListTree(filepath.Join(lib, "9000003 【8アバター対応】Moon Dress")), ","); got != "MoonDress/MoonDress.unitypackage" {
		t.Errorf("item folder (zip removed after unpacking): %s", got)
	}
	if naming.BoothIDFromName("9000003 【8アバター対応】Moon Dress") != "9000003" {
		t.Error("the folder name has to carry the item id")
	}
	if st.Downloaded["90000030"] == nil {
		t.Error("not recorded")
	}
}

func TestSessionFile(t *testing.T) {
	core.DataDir = t.TempDir()
	cs := []core.SavedCookie{{Name: boothSessionCookie, Value: "abc", Expires: time.Now().Add(time.Hour).Unix()}, {Name: "old", Value: "x", Expires: 1}}
	if err := saveBoothSession(cs); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(sessionFile())
	if runtime.GOOS == "windows" && bytes.Contains(raw, []byte("abc")) {
		t.Error("the login is stored readable")
	}
	got := LoadBoothSession()
	if len(got) != 1 || got[0].Value != "abc" {
		t.Errorf("loaded %+v", got)
	}
	if h := cookieHeader(got); h != "adult=t; "+boothSessionCookie+"=abc" {
		t.Errorf("header %q", h)
	}
}
