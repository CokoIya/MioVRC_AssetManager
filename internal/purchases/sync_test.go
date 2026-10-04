package purchases

import (
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
	"vrclib/internal/webpane"
)

// the pane's driver has to answer what boothLoop asks of it
var _ interface{ UserLeft() bool } = (*webpane.PaneDriver)(nil)

// fakeDriver: a page that stays on Booth's login form.
type fakeDriver struct {
	left  atomic.Bool
	evals atomic.Int32
}

func (d *fakeDriver) Eval(expr string, timeout time.Duration) (json.RawMessage, error) {
	d.evals.Add(1)
	return json.Marshal(core.BoothAccountsBase() + "/users/sign_in")
}
func (d *fakeDriver) Navigate(u string) error                           { return nil }
func (d *fakeDriver) Reconnect() error                                  { return nil }
func (d *fakeDriver) Dead() bool                                        { return false }
func (d *fakeDriver) Close()                                            {}
func (d *fakeDriver) CloseBrowser()                                     {}
func (d *fakeDriver) Cookies(urls []string) ([]core.SavedCookie, error) { return nil, nil }
func (d *fakeDriver) UserLeft() bool                                    { return d.left.Load() }

// Leaving the login page ends the sync at once, instead of holding it (and the logout, the quiet login check
// and the exit that wait for it) for twenty minutes.
func TestBoothSyncEndsWhenPlayerLeaves(t *testing.T) {
	st := testkit.NewStore(t)
	PurchaseCancel.Store(false)
	d := &fakeDriver{}
	prog := &core.Task{}
	closeWin := true
	done := make(chan bool, 1)
	go func() { done <- boothLoop(st, prog, d, false, "请登录", &closeWin) }()
	time.Sleep(50 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("the sync ended while the player was still on the page")
	default:
	}
	d.left.Store(true)
	select {
	case ok := <-done:
		if ok || !strings.Contains(prog.Snapshot().Msg, "未登录") {
			t.Errorf("ok %v, status %q", ok, prog.Snapshot().Msg)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the sync went on waiting after the player left")
	}
}

func boothIDs(st *core.Store) string {
	st.Mu.RLock()
	defer st.Mu.RUnlock()
	var ids []string
	for _, id := range core.SortedKeys(st.Purchases) {
		if !core.IsGumID(id) {
			ids = append(ids, id)
		}
	}
	return strings.Join(ids, " ")
}

// A sync that did not read everything adds what it found and drops nothing; a complete one still replaces
// the list. Orders known before stay when the order pages were not read.
func TestMergeKeepsWhatWasNotRead(t *testing.T) {
	st := testkit.NewStore(t)
	item := func(id string) scrapeItem {
		return scrapeItem{ID: id, Name: "Item " + id, Files: []string{id + ".zip"}, Downloads: []string{id + "0"}}
	}
	first := &scrapeResult{Orders: []scrapeOrder{{ID: "o1", Date: "2026-01-05", Items: []string{"101"}}, {ID: "o2", Date: "2026-02-01", Items: []string{"102"}}}}
	for _, id := range []string{"101", "102", "103", "104", "105", "106"} {
		first.Library = append(first.Library, item(id))
	}
	if n, partial := mergePurchases(st, first); n != 6 || partial {
		t.Fatalf("first sync: %d, partial %v", n, partial)
	}
	// the order pages failed: the dates stay
	second := &scrapeResult{Library: first.Library, OrderError: "HTTP 500 /orders?page=1"}
	if n, partial := mergePurchases(st, second); n != 6 || partial {
		t.Fatalf("second sync: %d, partial %v", n, partial)
	}
	st.Mu.RLock()
	if o := st.Purchases["101"].Orders; len(o) != 1 || o[0].ID != "o1" || o[0].Date != "2026-01-05" {
		t.Errorf("orders of 101 after a sync without order pages: %+v", o)
	}
	st.Mu.RUnlock()
	// a page in the middle did not load: one new purchase came, nothing goes
	n, partial := mergePurchases(st, &scrapeResult{Library: []scrapeItem{item("101"), item("107")}, Incomplete: "已购列表第 2 页未能加载"})
	if n != 7 || !partial || boothIDs(st) != "101 102 103 104 105 106 107" {
		t.Errorf("incomplete sync: %d, partial %v, kept %s", n, partial, boothIDs(st))
	}
	// nothing said to be missing, but most of the list is gone: not believed
	n, partial = mergePurchases(st, &scrapeResult{Library: []scrapeItem{item("101"), item("102")}})
	if n != 7 || !partial || boothIDs(st) != "101 102 103 104 105 106 107" {
		t.Errorf("a list that shrank: %d, partial %v, kept %s", n, partial, boothIDs(st))
	}
	// one purchase gone from a complete list: that is what Booth says now
	var most []scrapeItem
	for _, id := range []string{"101", "102", "103", "104", "105", "106"} {
		most = append(most, item(id))
	}
	if n, partial = mergePurchases(st, &scrapeResult{Library: most}); n != 6 || partial || boothIDs(st) != "101 102 103 104 105 106" {
		t.Errorf("a complete sync: %d, partial %v, kept %s", n, partial, boothIDs(st))
	}
	if !strings.Contains(syncPartialNote, "同步未完整，已保留原有记录") {
		t.Error("the note the window shows")
	}
}

// The scraper run outside a browser (needs node; skipped without it): a page in the middle that comes back
// empty or does not load is not taken for the end of the list, and the result says the list is not whole.
func TestScraperReportsGaps(t *testing.T) {
	var r scrapeResult
	if json.Unmarshal([]byte(`{"library":[],"incomplete":"已购列表第 3 页没有内容"}`), &r) != nil || r.Incomplete == "" {
		t.Errorf("result %+v", r)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	out, err := exec.Command(node, "testdata/scraper_harness.js", "booth_scraper.js").Output()
	if err != nil {
		t.Fatalf("harness: %v", err)
	}
	var got map[string]struct{ Error, IDs, Incomplete, Downloads string }
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("harness output %q: %v", out, err)
	}
	for name, want := range map[string][2]string{
		"whole":       {"1 2 3", ""},
		"emptyMiddle": {"1 3", "已购列表第 2 页没有内容"},
		"failedPage":  {"1 3", "已购列表第 2 页未能加载"},
		"repeats":     {"1 3", ""},
	} {
		if g := got[name]; g.Error != "" || g.IDs != want[0] || g.Incomplete != want[1] {
			t.Errorf("%s: %+v, want items %q and incomplete %q", name, g, want[0], want[1])
		}
	}
	if got["whole"].Downloads != "10 20 30" {
		t.Errorf("downloads %q", got["whole"].Downloads)
	}
}

func TestRedactPage(t *testing.T) {
	page := `<meta name="csrf-token" content="SECRETTOKEN" /><input type="hidden" name="authenticity_token" value="FORMTOKEN">
<a href="/library">mio@example.com</a><div class="item">Moon Dress</div>`
	got := redactPage(page)
	for _, gone := range []string{"SECRETTOKEN", "FORMTOKEN", "mio@example.com"} {
		if strings.Contains(got, gone) {
			t.Errorf("%s is still in the page kept for debugging", gone)
		}
	}
	if !strings.Contains(got, "Moon Dress") || !strings.Contains(got, `name="csrf-token"`) {
		t.Errorf("too much was taken out: %s", got)
	}
}

// Fewer purchases than Gumroad counts (a page came back empty): what was kept before is not taken for refunded.
func TestGumroadSyncShortList(t *testing.T) {
	f, st := newFakeGumroad(t, 32)
	if !RunGumroadSync(st, &core.Task{}) {
		t.Fatal("sync failed")
	}
	f.mu.Lock()
	f.emptyPage = 2
	f.mu.Unlock()
	prog := &core.Task{}
	if !RunGumroadSync(st, prog) {
		t.Fatal("second sync failed")
	}
	st.Mu.RLock()
	gum := 0
	for id := range st.Purchases {
		if core.IsGumID(id) {
			gum++
		}
	}
	kept := st.Purchases["gr_p20"] != nil && st.Booth["gr_p20"] != nil
	st.Mu.RUnlock()
	if gum != 33 || !kept || !strings.Contains(prog.Snapshot().Msg, "同步未完整，已保留原有记录") {
		t.Errorf("%d purchases kept (p20: %v), status %q", gum, kept, prog.Snapshot().Msg)
	}
	f.mu.Lock()
	if f.hits["/library"] < 4 { // the empty page was not taken for the end of the list
		t.Errorf("requests %v", f.hits)
	}
	f.mu.Unlock()
}

// The wait for a Gumroad login: nothing is asked of gumroad.com while the page is on the login form; after
// that less and less often; a protection page does not end the wait; leaving the page does.
func TestGumroadLoginWatch(t *testing.T) {
	t.Setenv("VRCLIB_GUMROAD_BASE", "https://gumroad.test")
	var pageURL atomic.Value
	pageURL.Store("https://gumroad.test/login?next=%2Flibrary")
	var probes, blockedSaid atomic.Int32
	var left, in atomic.Bool
	w := &gumWatch{tick: time.Millisecond, gap: 40 * time.Millisecond, maxGap: 80 * time.Millisecond, limit: 5 * time.Second,
		running: func() bool { return true },
		left:    left.Load,
		pageURL: func() string { return pageURL.Load().(string) },
		probe: func() (*gumSession, bool, error) {
			n := probes.Add(1)
			if in.Load() {
				return &gumSession{Name: "Mio"}, true, nil
			}
			if n == 1 {
				return nil, true, errGumBlocked
			}
			return nil, true, nil
		},
		blocked: func(string) { blockedSaid.Add(1) },
	}
	done := make(chan *gumSession, 1)
	go func() { done <- w.wait() }()
	time.Sleep(60 * time.Millisecond)
	if probes.Load() != 0 {
		t.Fatalf("gumroad.com was asked %d times while the login form was open", probes.Load())
	}
	pageURL.Store("https://gumroad.test/library")
	time.Sleep(200 * time.Millisecond)
	if n := probes.Load(); n < 2 || n > 5 || blockedSaid.Load() != 1 { // ~200 ticks went by
		t.Errorf("asked %d times in 200ms (gap 40ms, doubling), protection page reported %d times", n, blockedSaid.Load())
	}
	in.Store(true)
	select {
	case s := <-done:
		if s == nil || s.Name != "Mio" {
			t.Errorf("login %+v", s)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the login was not noticed after the protection page")
	}
	// the player goes to another tab: the wait ends
	in.Store(false)
	go func() { done <- w.wait() }()
	time.Sleep(20 * time.Millisecond)
	left.Store(true)
	select {
	case s := <-done:
		if s != nil {
			t.Errorf("a login out of nowhere: %+v", s)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the wait went on after the player left")
	}
	for u, want := range map[string]bool{"https://gumroad.test/login": true, "https://gumroad.test/two-factor?next=x": true,
		"https://accounts.google.com/o/oauth2": true, "about:blank": true, "https://gumroad.test/library": false,
		"https://app.gumroad.test/dashboard": false, "": false} {
		if gumStillLoggingIn(u) != want {
			t.Errorf("%q: still logging in = %v", u, !want)
		}
	}
	if errors.Is(errGumBlocked, errGumLogin) {
		t.Error("a protection page is not a lost login")
	}
}
