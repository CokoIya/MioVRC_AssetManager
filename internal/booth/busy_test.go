package booth

import (
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

// Booth answering 429 (or 403) ends the batch there: the items after it are not asked for, nothing is written
// down against them, and the rest is fetched once Booth has had its pause.
func TestBoothBatchStopsWhenTurnedAway(t *testing.T) {
	first, gap := boothPauseFirst, boothGap
	boothPauseFirst, boothGap = 150*time.Millisecond, time.Millisecond
	defer func() {
		boothMu.Lock()
		boothPauseFirst, boothGap, boothPauseStep, boothPausedTill, boothBrokenOff, boothBrokeAt = first, gap, 0, time.Time{}, false, ""
		boothMu.Unlock()
	}()
	var mu sync.Mutex
	var asked []string
	busy := map[string]int{"7000002": http.StatusTooManyRequests}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/ja/items/"), ".json")
		if !strings.HasSuffix(r.URL.Path, ".json") {
			fmt.Fprint(w, "<html></html>")
			return
		}
		mu.Lock()
		asked = append(asked, id)
		code := busy[id]
		mu.Unlock()
		if code != 0 {
			w.WriteHeader(code)
			return
		}
		fmt.Fprintf(w, `{"name":"Item %s","price":"¥ 100"}`, id)
	}))
	defer srv.Close()
	t.Setenv("VRCLIB_BOOTH_WEB", srv.URL)
	st := testkit.NewStore(t)
	for i, id := range []string{"7000001", "7000002", "7000003", "7000004"} { // newest purchase first
		st.Purchases[id] = &core.Purchase{ID: id, Name: "p", First: int64(1000 - i)}
	}
	prog := &core.Task{}
	RunBoothFetch(st, prog, false, nil)
	mu.Lock()
	got := strings.Join(asked, " ")
	asked = nil
	mu.Unlock()
	if got != "7000001 7000002" || !strings.Contains(prog.Snapshot().Msg, "暂时限制了访问") {
		t.Fatalf("asked for %q, status %q", got, prog.Snapshot().Msg)
	}
	st.Mu.RLock()
	if st.Booth["7000001"] == nil || st.Booth["7000002"] != nil || st.Booth["7000003"] != nil {
		t.Errorf("recorded: %v", core.SortedKeys(st.Booth))
	}
	st.Mu.RUnlock()
	// during the pause nothing is fetched by itself
	if ResumeDue() {
		t.Error("due again at once")
	}
	RunBoothFetch(st, prog, false, nil)
	mu.Lock()
	if len(asked) != 0 {
		t.Errorf("asked during the pause: %v", asked)
	}
	busy = map[string]int{}
	mu.Unlock()
	time.Sleep(200 * time.Millisecond)
	if !ResumeDue() {
		t.Fatal("not due after the pause")
	}
	RunBoothFetch(st, prog, false, nil)
	mu.Lock()
	got = strings.Join(asked, " ")
	mu.Unlock()
	if got != "7000002 7000003 7000004" || ResumeDue() {
		t.Errorf("after the pause asked for %q, still due %v", got, ResumeDue())
	}
	// an item Booth refuses by itself (403 again, first thing after the pause) does not hold up the others for ever
	st.Purchases["7000005"] = &core.Purchase{ID: "7000005", Name: "p", First: 2000}
	st.Purchases["7000006"] = &core.Purchase{ID: "7000006", Name: "p", First: 1500}
	mu.Lock()
	asked, busy = nil, map[string]int{"7000005": http.StatusForbidden}
	mu.Unlock()
	RunBoothFetch(st, prog, false, nil)
	time.Sleep(350 * time.Millisecond)
	RunBoothFetch(st, prog, false, nil)
	mu.Lock()
	got = strings.Join(asked, " ")
	mu.Unlock()
	st.Mu.RLock()
	defer st.Mu.RUnlock()
	if got != "7000005 7000005 7000006" || st.Booth["7000005"] == nil || st.Booth["7000005"].Err == "" || st.Booth["7000006"] == nil {
		t.Errorf("an item refused by itself: asked %q, recorded %v", got, core.SortedKeys(st.Booth))
	}
}
