package library

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
)

// fakeBing answers like Bing's web translator: every line comes back with "译" in front.
func fakeBing(t *testing.T) *int {
	t.Helper()
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/translator" {
			fmt.Fprint(w, `<div data-iid="translator.5026"></div><script>IG:"ABCDEF0123",params_AbusePreventionHelper = [1700000000000,"token",3600000];</script>`)
			return
		}
		requests++
		_ = r.ParseForm()
		lines := strings.Split(r.Form.Get("text"), "\n")
		for i := range lines {
			lines[i] = "译" + lines[i]
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{"translations": []map[string]string{{"text": strings.Join(lines, "\n")}}}})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("VRCLIB_BING_BASE", srv.URL)
	return &requests
}

// Many names are translated in several batches; the window is not made to reload every card after each.
func TestTranslateBumpsRarely(t *testing.T) {
	requests := fakeBing(t)
	pause := transPause
	transPause = time.Millisecond
	defer func() { transPause = pause }()
	st := testStore()
	st.Trans = map[string]string{}
	var names []string
	for i := 0; i < 200; i++ {
		names = append(names, fmt.Sprintf("Summer Dress %d", i))
	}
	rev := core.CurRev()
	RunTranslate(st, &core.Task{}, names)
	if *requests != 5 || len(st.Trans) != 200 || st.Trans["Summer Dress 7"] != "译Summer Dress 7" {
		t.Fatalf("%d requests, %d names translated, %q", *requests, len(st.Trans), st.Trans["Summer Dress 7"])
	}
	if n := core.CurRev() - rev; n != 0 {
		t.Errorf("%d reloads for 5 batches done within two seconds", n)
	}
	// a long run still shows what it has so far, now and then
	transBumpEvery = 0
	defer func() { transBumpEvery = 2 * time.Second }()
	st.Trans = map[string]string{}
	rev = core.CurRev()
	RunTranslate(st, &core.Task{}, names)
	if n := core.CurRev() - rev; n != 4 {
		t.Errorf("%d reloads between 5 batches", n)
	}
}

// A name that comes in just as a run of the translator ends (a purchase list read then, say) is not left for a
// later run: from the moment the run has found no more requests, a request starts a run of its own.
func TestTranslateRequestAtTheEnd(t *testing.T) {
	fakeBing(t)
	pause := transPause
	transPause = time.Millisecond
	defer func() { transPause = pause }()
	waitIdle()
	st := testkit.NewStore(t)
	st.Assets = []*core.Asset{{Key: "name:summer dress", Name: "Summer Dress"}}
	var once sync.Once
	asked := make(chan struct{})
	transMu.Lock()
	transEnded = func() {
		once.Do(func() {
			st.Mu.Lock()
			st.Assets = append(st.Assets, &core.Asset{Key: "name:winter coat", Name: "Winter Coat"})
			st.Mu.Unlock()
			KickTranslate(st)
			close(asked)
		})
	}
	transMu.Unlock()
	t.Cleanup(func() {
		waitIdle()
		transMu.Lock()
		transEnded = nil
		transMu.Unlock()
	})
	KickTranslate(st)
	select {
	case <-asked:
	case <-time.After(20 * time.Second):
		t.Fatal("the run does not end")
	}
	waitIdle()
	st.Mu.RLock()
	defer st.Mu.RUnlock()
	if st.Trans["Summer Dress"] != "译Summer Dress" || st.Trans["Winter Coat"] != "译Winter Coat" {
		t.Errorf("translated: %v", st.Trans)
	}
}
