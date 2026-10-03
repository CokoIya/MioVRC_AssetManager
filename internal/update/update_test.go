package update

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vrclib/internal/core"
)

func TestVersionNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v1.4.1", "1.4.0", true}, {"1.4.0", "1.4.0", false}, {"v1.10.0", "1.9.9", true}, {"1.3", "1.3.1", false},
		{"VRC素材库 2.0", "1.9", true}, {"release", "1.0", false},
	}
	for _, c := range cases {
		if got := VersionNewer(c.a, c.b); got != c.want {
			t.Errorf("%s > %s: got %v", c.a, c.b, got)
		}
	}
}

// The API has had enough of this address (403): the release page's redirect gives the tag, and the files are
// found where the releases always put them. A failed check does not count as a check.
func TestUpdateFallback(t *testing.T) {
	core.DataDir = t.TempDir()
	st := core.LoadStore(core.DataDir + "/library.json")
	st.Settings.Proxy = "direct"
	apiHits, limited, published := 0, true, "v9.9.9"
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case strings.HasPrefix(p, "/repos/"):
			apiHits++
			if limited {
				w.WriteHeader(403)
				return
			}
			_, _ = w.Write([]byte(`{"tag_name":"v9.9.9","name":"MioVRCA 9.9.9","body":"- notes","html_url":"x","assets":[{"name":"MioVRC_AssetManager-portable-9.9.9.zip","browser_download_url":"u","size":5}]}`))
		case strings.HasSuffix(p, "/releases/latest"):
			if published == "" {
				http.Redirect(w, r, srv.URL+"/"+UpdateRepo+"/releases", 302)
				return
			}
			http.Redirect(w, r, srv.URL+"/"+UpdateRepo+"/releases/tag/"+published, 302)
		case strings.HasSuffix(p, "/releases/download/v9.9.9/MioVRC_AssetManager-portable-9.9.9.zip"):
			if r.Method == "HEAD" { // this store answers GET only
				w.WriteHeader(403)
				return
			}
			w.Header().Set("Content-Range", "bytes 0-0/1234")
			w.WriteHeader(206)
			_, _ = w.Write([]byte("P"))
		case strings.HasSuffix(p, "/releases/download/v9.9.9/MioVRC_AssetManager-setup-9.9.9.exe"):
			w.Header().Set("Content-Length", "777")
			w.WriteHeader(200)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	t.Setenv("VRCLIB_GITHUB_API", srv.URL)
	t.Setenv("VRCLIB_GITHUB_SITE", srv.URL)

	info, err := CheckUpdate(st, false)
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "9.9.9" || info.Tag != "v9.9.9" || info.Zip == nil || info.Zip.Size != 1234 || info.Setup == nil || info.Setup.Size != 777 {
		t.Fatalf("fallback info %+v zip %+v setup %+v", info, info.Zip, info.Setup)
	}
	if !strings.HasSuffix(info.URL, "/releases/tag/v9.9.9") || !VersionNewer(info.Version, core.AppVersion) {
		t.Errorf("url %q", info.URL)
	}
	// within the hour the answer is kept; a forced check asks again and takes the API's answer when it is back
	n := apiHits
	if _, err := CheckUpdate(st, false); err != nil || apiHits != n {
		t.Errorf("asked again within the hour (%d → %d) %v", n, apiHits, err)
	}
	limited = false
	if info, err = CheckUpdate(st, true); err != nil || info.Notes != "- notes" || info.Name != "MioVRCA 9.9.9" {
		t.Errorf("api answer %+v %v", info, err)
	}
	// nothing can be reached: the error comes back, and the check is not remembered as done
	limited, published = true, ""
	st.Update, st.UpdateChecked = nil, 0
	if _, err := CheckUpdate(st, false); err == nil {
		t.Error("no error when neither the API nor the page answers")
	}
	if st.UpdateChecked != 0 {
		t.Error("a failed check was remembered")
	}
	// a tag without assets under the usual names: the version is known, the files are not
	published = "v9.9.8"
	if info, err = CheckUpdate(st, false); err != nil || info.Version != "9.9.8" || info.Zip != nil || info.Setup != nil {
		t.Errorf("no assets: %+v %v", info, err)
	}
}
