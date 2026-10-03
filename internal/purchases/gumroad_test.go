package purchases

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"vrclib/internal/booth"
	"vrclib/internal/core"
	"vrclib/internal/library"
	"vrclib/internal/testkit"
)

// fakeGumroad answers like gumroad.com does to a buyer: Inertia pages (JSON when asked with X-Inertia, else
// the same JSON in data-page), a library of 15 purchases a page, download pages, and file redirects to
// another host.
type fakeGumroad struct {
	mu       sync.Mutex
	t        *testing.T
	files    *httptest.Server
	cards    []map[string]any // newest first
	archived []map[string]any
	hits     map[string]int
	loggedIn bool
	blocked  bool
	fileAuth []string // the Cookie header the file host saw
}

func gumCardJSON(id, name, creator string) map[string]any {
	return map[string]any{
		"product":  map[string]any{"name": name, "creator": map[string]any{"name": creator, "profile_url": "https://" + strings.ToLower(creator) + ".gumroad.com/", "avatar_url": ""}, "thumbnail_url": "", "native_type": "digital"},
		"purchase": map[string]any{"id": id + "==", "is_archived": false, "download_url": "", "variants": ""},
	}
}

func (f *fakeGumroad) inertia(w http.ResponseWriter, r *http.Request, component string, props map[string]any) {
	props["logged_in_user"] = map[string]any{"name": "Mio", "email": "mio@example.com"}
	page, _ := json.Marshal(map[string]any{"component": component, "props": props, "url": r.URL.String(), "version": nil})
	if r.Header.Get("X-Inertia") == "true" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Inertia", "true")
		_, _ = w.Write(page)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, `<!DOCTYPE html><html><body><div id="app" data-page="`+html.EscapeString(string(page))+`"></div></body></html>`)
}

func (f *fakeGumroad) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hits[r.URL.Path]++
	if f.blocked { // the protection in front of the site
		w.Header().Set("Cf-Mitigated", "challenge")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "<html><title>Just a moment...</title></html>")
		return
	}
	in := f.loggedIn && strings.Contains(r.Header.Get("Cookie"), "_gumroad_app_session=ok")
	base := "http://" + r.Host
	switch {
	case r.URL.Path == "/login":
		f.inertia(w, r, "Logins/New", map[string]any{})
	case !in:
		http.Redirect(w, r, "/login?next="+r.URL.Path, http.StatusFound)
	case r.URL.Path == "/library":
		list := f.cards
		if r.URL.Query().Get("show_archived_only") == "true" {
			list = f.archived
		}
		page := 1
		fmt.Sscan(r.URL.Query().Get("page"), &page)
		pages := (len(list) + 14) / 15
		from, to := (page-1)*15, page*15
		if from > len(list) {
			from = len(list)
		}
		if to > len(list) {
			to = len(list)
		}
		var res []map[string]any
		for _, c := range list[from:to] {
			id := strings.TrimSuffix(c["purchase"].(map[string]any)["id"].(string), "==")
			c["purchase"].(map[string]any)["download_url"] = base + "/d/tok" + id
			c["product"].(map[string]any)["thumbnail_url"] = f.files.URL + "/thumb/" + id + ".png"
			res = append(res, c)
		}
		f.inertia(w, r, "Library/Index", map[string]any{"results": res, "pagination": map[string]any{"page": page, "pages": pages, "count": len(list)}})
	case strings.HasPrefix(r.URL.Path, "/d/tok"):
		id := strings.TrimPrefix(r.URL.Path, "/d/tok")
		if id == "p03" {
			f.inertia(w, r, "UrlRedirects/MembershipInactive", map[string]any{})
			return
		}
		file := func(fid, name, ext string, size int, dl bool) map[string]any {
			m := map[string]any{"type": "file", "file_name": name, "extension": ext, "file_size": size, "id": fid, "download_url": nil, "external_link_url": nil}
			if dl {
				m["download_url"] = "/r/tok" + id + "/product_files?product_file_ids%5B%5D=" + fid
			}
			return m
		}
		items := []any{
			map[string]any{"type": "folder", "id": "fo1", "name": "Unity", "children": []any{file("f"+id+"a", "Dress_"+id, "UNITYPACKAGE", 2048, true)}},
			file("f"+id+"b", "Dress_"+id+"_PSD", "ZIP", 4096, true),
			file("f"+id+"c", "Promo video", "MP4", 9, false), // streamed only
		}
		f.inertia(w, r, "UrlRedirects/DownloadPage", map[string]any{"token": "tok" + id,
			"content":  map[string]any{"content_items": items, "rich_content_pages": nil},
			"purchase": map[string]any{"id": id + "==", "product_permalink": "perm" + id, "product_name": "Dress " + id, "product_long_url": "https://creator.gumroad.com/l/perm" + id, "created_at": "2026-09-01T10:00:00Z"}})
	case strings.HasPrefix(r.URL.Path, "/r/tok"):
		fid := r.URL.Query().Get("product_file_ids[]")
		http.Redirect(w, r, f.files.URL+"/files/"+fid+".bin?response-content-disposition="+
			strings.ReplaceAll(`attachment; filename="Real Name `+fid+`.zip"`, " ", "%20")+"&X-Amz-Signature=abc", http.StatusFound)
	default:
		w.WriteHeader(404)
	}
}

func newFakeGumroad(t *testing.T, n int) (*fakeGumroad, *core.Store) {
	t.Helper()
	st := testkit.NewStore(t)
	f := &fakeGumroad{t: t, hits: map[string]int{}, loggedIn: true}
	f.files = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.fileAuth = append(f.fileAuth, r.Header.Get("Cookie"))
		f.mu.Unlock()
		if strings.HasPrefix(r.URL.Path, "/thumb/") {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(testkit.JPEG(64, 64, struct{ R, G, B, A uint8 }{200, 100, 50, 255}))
			return
		}
		_, _ = io.WriteString(w, "file body of "+r.URL.Path)
	}))
	t.Cleanup(f.files.Close)
	for i := 1; i <= n; i++ {
		f.cards = append(f.cards, gumCardJSON(fmt.Sprintf("p%02d", i), fmt.Sprintf("Gum Dress %02d", i), "Atelier"))
	}
	f.archived = []map[string]any{gumCardJSON("old1", "Archived Hair", "Atelier")}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	t.Setenv("VRCLIB_GUMROAD_BASE", srv.URL)
	if err := saveGumSession(&gumSession{Cookies: []core.SavedCookie{{Name: "_gumroad_app_session", Value: "ok"}, {Name: "_gumroad_guid", Value: "g"}}}); err != nil {
		t.Fatal(err)
	}
	return f, st
}

func TestGumroadSync(t *testing.T) {
	f, st := newFakeGumroad(t, 17)
	// a Booth purchase that was there before stays through a Gumroad sync, and the other way round
	st.Purchases["5550001"] = &core.Purchase{ID: "5550001", Name: "Booth Thing", Files: []string{"booth_thing.zip"}, Downloads: []string{"900"}}
	if !RunGumroadSync(st, &core.Task{}) {
		t.Fatal("sync failed")
	}
	st.Mu.RLock()
	gum := 0
	for id := range st.Purchases {
		if core.IsGumID(id) {
			gum++
		}
	}
	p := st.Purchases["gr_p01"]
	if gum != 18 || p == nil || st.Purchases["5550001"] == nil || st.Purchases["gr_old1"] == nil {
		t.Fatalf("%d gumroad purchases; p01 %v", gum, p)
	}
	if p.Source != "gumroad" || p.Shop != "Atelier" || p.PageURL != "https://creator.gumroad.com/l/permp01" || !strings.HasSuffix(p.DLPage, "/d/tokp01") ||
		fmt.Sprint(p.Files) != "[Dress_p01.unitypackage Dress_p01_PSD.zip]" || fmt.Sprint(p.Downloads) != "[gr_fp01a gr_fp01b]" ||
		len(p.DLPaths) != 2 || !strings.HasPrefix(p.DLPaths[0], "/r/tokp01/product_files?") || len(p.Orders) != 1 || p.Orders[0].Date != "2026-09-01" {
		t.Errorf("p01 %+v", p)
	}
	if p.Cover == "" || !core.FileExists(p.Cover) {
		t.Errorf("no cover: %q", p.Cover)
	}
	if n := st.Purchases["gr_p03"]; n == nil || len(n.Downloads) != 0 || !strings.Contains(n.Note, "会员") {
		t.Errorf("a purchase without access: %+v", n)
	}
	bi := st.Booth["gr_p01"]
	if bi == nil || bi.Name != "Gum Dress 01" || bi.Shop != "Atelier" || bi.URL != p.PageURL || bi.Cover != p.Cover {
		t.Errorf("product info %+v", bi)
	}
	v := library.PurchaseOnlyView(st, p)
	if !v.Virtual || v.Purchase == nil || v.Purchase.Source != "gumroad" || v.Purchase.ProductURL != p.PageURL || !strings.HasSuffix(v.Purchase.LibraryURL, "/library") ||
		len(v.Purchase.Orders) != 1 || v.Purchase.Orders[0].URL != p.DLPage || v.Cover == "" {
		t.Errorf("view %+v / %+v", v, v.Purchase)
	}
	acc := CurrentGumroadAccount(st)
	st.Mu.RUnlock()
	if !acc.LoggedIn || acc.Count != 18 || acc.Name != "Mio" || acc.LastSync == 0 {
		t.Errorf("account %+v", acc)
	}
	f.mu.Lock()
	if f.hits["/library"] != 3 || f.hits["/d/tokp01"] != 1 || len(f.fileAuth) == 0 {
		t.Errorf("requests %v", f.hits)
	}
	for _, a := range f.fileAuth {
		if a != "" {
			t.Errorf("the login went to the file host: %q", a)
		}
	}
	// a second sync reads the lists again but no download page it has read before; a refunded purchase goes
	f.hits = map[string]int{}
	f.cards = f.cards[1:]
	f.cards = append([]map[string]any{gumCardJSON("new1", "Brand New Coat", "Atelier")}, f.cards...)
	f.mu.Unlock()
	if !RunGumroadSync(st, &core.Task{}) {
		t.Fatal("second sync failed")
	}
	f.mu.Lock()
	if f.hits["/d/toknew1"] != 1 || f.hits["/d/tokp02"] != 0 || f.hits["/d/tokp03"] != 1 { // p03 had no files: asked again
		t.Errorf("second sync requests %v", f.hits)
	}
	f.mu.Unlock()
	st.Mu.RLock()
	if st.Purchases["gr_p01"] != nil || st.Booth["gr_p01"] != nil || st.Purchases["gr_new1"] == nil || st.Purchases["gr_p02"].First == 0 || st.Purchases["5550001"] == nil {
		t.Error("the second sync did not replace the list")
	}
	st.Mu.RUnlock()
	// a Booth sync does not forget what was bought on Gumroad
	if n := mergePurchases(st, &scrapeResult{Library: []scrapeItem{{ID: "5550002", Name: "Other", Files: []string{"o.zip"}, Downloads: []string{"901"}}}}); n != 1 {
		t.Errorf("booth merge counted %d", n)
	}
	st.Mu.RLock()
	if st.Purchases["gr_p02"] == nil || st.Purchases["5550001"] != nil || st.Purchases["5550002"] == nil {
		t.Error("the Booth sync dropped Gumroad purchases, or kept an old Booth one")
	}
	st.Mu.RUnlock()
	// nothing is asked of Booth for a Gumroad id
	asked := 0
	boothSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "gr_") {
			asked++
		}
		w.WriteHeader(404)
	}))
	defer boothSrv.Close()
	t.Setenv("VRCLIB_BOOTH_WEB", boothSrv.URL)
	booth.RunBoothFetch(st, &core.Task{}, true, nil)
	if asked != 0 {
		t.Errorf("Booth was asked about %d Gumroad purchases", asked)
	}
	// the HTML form of a page reads the same
	g := newGumClient(st, LoadGumSession())
	resp, _ := g.get(core.GumroadBase()+"/library?page=1", false)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if m := reDataPageAttr.FindSubmatch(body); m == nil || !strings.Contains(html.UnescapeString(string(m[1])), `"Library/Index"`) {
		t.Errorf("plain page: %.200s", body)
	}
}

func TestGumroadLoginGone(t *testing.T) {
	f, st := newFakeGumroad(t, 3)
	f.loggedIn = false
	prog := &core.Task{}
	if RunGumroadSync(st, prog) {
		t.Fatal("synced without a login")
	}
	if LoadGumSession() != nil || !strings.Contains(prog.Msg, "重新登录") {
		t.Errorf("session kept, or no word about it: %q", prog.Msg)
	}
	if _, _, err := resolveGumDownload(st, "gr_x"); err != errGumLogin {
		t.Errorf("download without a login: %v", err)
	}
}

// The protection page is not a lost login: the login stays, and the player is told what happened.
func TestGumroadBlocked(t *testing.T) {
	f, st := newFakeGumroad(t, 3)
	f.blocked = true
	prog := &core.Task{}
	if RunGumroadSync(st, prog) {
		t.Fatal("synced through the protection page")
	}
	if LoadGumSession() == nil || !strings.Contains(prog.Msg, "Cloudflare") {
		t.Errorf("login dropped, or no word about it: %q", prog.Msg)
	}
	st.Mu.Lock()
	st.Purchases["gr_x"] = &core.Purchase{ID: "gr_x", Source: "gumroad", Downloads: []string{"gr_x_1"}, DLPaths: []string{"/r/tok/product_files?product_file_ids[]=1"}}
	st.Mu.Unlock()
	if _, _, err := resolveGumDownload(st, "gr_x_1"); err != errGumBlocked {
		t.Errorf("download through the protection page: %v", err)
	}
	if LoadGumSession() == nil {
		t.Error("the download dropped the login")
	}
}

func TestGumroadDownload(t *testing.T) {
	f, st := newFakeGumroad(t, 2)
	root := t.TempDir()
	st.Settings.Roots = []string{root}
	st.Settings.DownloadDir = root
	st.Settings.NoExtract = true
	if !RunGumroadSync(st, &core.Task{}) {
		t.Fatal("sync failed")
	}
	u, name, err := resolveGumDownload(st, "gr_fp01b")
	if err != nil || !strings.HasPrefix(u, f.files.URL+"/files/fp01b.bin") || name != "Real Name fp01b.zip" {
		t.Fatalf("resolve: %q %q %v", u, name, err)
	}
	if _, _, err := resolveGumDownload(st, "gr_nosuch"); err == nil {
		t.Error("an unknown file resolved")
	}
	n, err := QueueDownloads(st, "gr_p01", nil)
	if err != nil || n != 2 {
		t.Fatalf("queue: %d %v", n, err)
	}
	for i := 0; i < 400; i++ {
		done := 0
		for _, j := range DLSnapshot() {
			if j.Item == "gr_p01" && (j.Status == "done" || j.Status == "failed") {
				done++
			}
		}
		if done == 2 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	folder := filepath.Join(root, "Gum Dress 01")
	for _, j := range DLSnapshot() {
		if j.Item == "gr_p01" && (j.Status != "done" || !strings.HasPrefix(j.Path, folder)) {
			t.Errorf("job %+v", j)
		}
	}
	b, err := os.ReadFile(filepath.Join(folder, "Real Name fp01a.zip"))
	if err != nil || !strings.Contains(string(b), "fp01a") {
		t.Fatalf("downloaded file: %q %v", b, err)
	}
	// the scan that follows a download finds the folder and knows it is that purchase
	for i := 0; i < 400 && library.PipelineBusy(); i++ {
		time.Sleep(25 * time.Millisecond)
	}
	library.RunFolderScan(st, &core.Task{})
	st.Mu.RLock()
	defer st.Mu.RUnlock()
	var a *core.Asset
	for _, x := range st.Assets {
		if x.Name == "Gum Dress 01" {
			a = x
		}
	}
	if a == nil || a.BoothID != "gr_p01" || !a.BoothFromLib {
		t.Fatalf("asset %+v", a)
	}
	v := library.BuildView(st, a)
	if v.Purchase == nil || v.Purchase.Source != "gumroad" || v.Booth == nil || v.Booth.Shop != "Atelier" || len(v.Purchase.Got) != 2 || v.Purchase.Got[0] == "" {
		t.Errorf("view purchase %+v booth %+v", v.Purchase, v.Booth)
	}
	for _, x := range library.AllViews(st) {
		if x.Virtual && x.BoothID == "gr_p01" {
			t.Error("still listed as not downloaded")
		}
	}
	var out map[string]any
	j, _ := json.Marshal(v)
	_ = json.Unmarshal(j, &out)
	if out["boothId"] != "gr_p01" {
		t.Errorf("view id %v", out["boothId"])
	}
}
