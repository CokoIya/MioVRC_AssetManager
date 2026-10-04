package server

import (
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"vrclib/internal/core"
)

// The page is told its language: "?" before anything is chosen on a fresh install, Chinese for an install
// from before, and what the settings say after that. Only the languages there are can be saved.
func TestPageLanguage(t *testing.T) {
	core.DataDir = t.TempDir()
	t.Cleanup(settle)
	st := core.LoadStore(filepath.Join(core.DataDir, "library.json"))
	apiToken = "t"
	srv := httptest.NewServer(NewMux(st))
	defer srv.Close()
	page := func() string {
		t.Helper()
		resp, err := srv.Client().Get(srv.URL + "/")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		i := strings.Index(string(b), `window.LANG = "`)
		if i < 0 {
			t.Fatalf("the page is not told its language")
		}
		s := string(b)[i+len(`window.LANG = "`):]
		return s[:strings.Index(s, `"`)]
	}
	if got := page(); got != "?" {
		t.Fatalf("fresh install: %q, want ?", got)
	}
	for _, c := range [][2]string{{"ja", "ja"}, {"en", "en"}, {"fr", ""}, {"", ""}} {
		if r := postJSON(t, srv, "/api/settings", map[string]any{"settings": map[string]any{"roots": []string{}, "lang": c[0]}, "noRescan": true}); r["ok"] != true {
			t.Fatalf("save %q: %v", c[0], r)
		}
		if got := page(); got != c[1] {
			t.Fatalf("saved %q: the page is told %q, want %q", c[0], got, c[1])
		}
	}
	// an install from before the setting: set up, no language
	st.Mu.Lock()
	st.Settings = core.Settings{SetupDone: true}
	st.Mu.Unlock()
	if got := page(); got != "" {
		t.Fatalf("install from before: %q, want Chinese", got)
	}
}
