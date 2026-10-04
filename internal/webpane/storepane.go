package webpane

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"vrclib/internal/core"
)

// Jinxxy pages in the pane. Jinxxy has no interface a buyer's program could ask for purchases, so the player
// uses the site itself here, and two things are done around it: a link that leads to another shop (its market
// lists Payhip and Gumroad products too) opens in the default browser, and a file the page downloads is taken
// into the library instead of being left in the browser's download folder.

// jinxxyOrigin: a page of jinxxy.com (in tests: of the stand-in for it).
func jinxxyOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	if u.Scheme == "https" && (h == "jinxxy.com" || strings.HasSuffix(h, ".jinxxy.com")) {
		return true
	}
	if b, err := url.Parse(core.JinxxyBase()); err == nil && !strings.HasSuffix(b.Hostname(), "jinxxy.com") && b.Scheme == u.Scheme && b.Host == u.Host {
		return true // another host only in tests
	}
	return false
}

// paneScriptFor: the pane's script with the test for a Jinxxy page put in.
func paneScriptFor(jinxxyBase string) string {
	test := `/(^|\.)jinxxy\.com$/i.test(location.hostname)`
	if b, err := url.Parse(jinxxyBase); err == nil && b.Host != "" && !strings.HasSuffix(b.Hostname(), "jinxxy.com") {
		o, _ := json.Marshal(b.Scheme + "://" + b.Host)
		test += "||location.origin===" + string(o)
	}
	return strings.Replace(paneLinkSource, "__JX__", test, 1)
}

// PaneOpenExternal opens an address in the default browser (tests look at what it was given).
var PaneOpenExternal = core.OpenURL

// externalClicked: a link to another site clicked on a Jinxxy page. Only such a page's call counts, and only
// for an http(s) address that is not Jinxxy's own.
func (p *webPane) externalClicked(ctx int, link string) {
	p.Mu.Lock()
	origin, known := p.origins[ctx]
	p.Mu.Unlock()
	if !known || origin == "" {
		origin = p.currentURL()
	}
	u, err := url.Parse(link)
	if !jinxxyOrigin(origin) || err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || jinxxyOrigin(u.Scheme+"://"+u.Host) {
		return
	}
	core.Logf("内置页面：%s 的链接已在默认浏览器中打开", u.Hostname()) // (the host only: an address can carry a token)
	_ = PaneOpenExternal(u.String())
}

// ---------- files the pane's browser downloads ----------

// PaneFile: a file downloaded on a page in the pane, on its way into the library.
type PaneFile struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Done    int64  `json:"done"`
	Total   int64  `json:"total"`
	Status  string `json:"status"` // running, saving, done, failed
	Path    string `json:"path,omitempty"`
	Err     string `json:"err,omitempty"`
	At      int64  `json:"at"`
	Foreign bool   `json:"foreign,omitempty"` // not begun by a Jinxxy page: stopped, not taken in

	begun   bool // its start was reported (the name, and the frame that began it, come with that)
	checked bool // … and whose page that frame shows has been looked up
}

// errForeignFile: what is said of a download that some other site's page began.
const errForeignFile = "该下载不是由 Jinxxy 页面发起的，已取消，未收录到素材库"

// What is done with such a file. The downloads set these (purchases/jinxxy.go), so the pane does not depend
// on them.
var (
	PaneIncomingDir = func(st *core.Store) string { return "" }                                    // where the browser is to save; "" = leave downloads to the browser
	PaneFileSaved   = func(st *core.Store, path, name string) (string, error) { return path, nil } // into the library; returns where it is now
)

const paneFilesKeep = 20

// paneBeginWait: how long a file that is whole waits for the report that it began (tests shorten it).
var paneBeginWait = 5 * time.Second

var (
	paneFileMu sync.Mutex
	paneFiles  []*PaneFile
	reGUID     = regexp.MustCompile(`^[0-9A-Fa-f-]{8,64}$`)
)

func PaneFiles() []PaneFile {
	paneFileMu.Lock()
	defer paneFileMu.Unlock()
	out := make([]PaneFile, 0, len(paneFiles))
	for _, f := range paneFiles {
		out = append(out, *f)
	}
	return out
}

func paneFilesRunning() bool {
	paneFileMu.Lock()
	defer paneFileMu.Unlock()
	for _, f := range paneFiles {
		if f.Status == "running" {
			return true
		}
	}
	return false
}

// downloadMode: while the pane is Jinxxy's, its browser saves downloads into a folder of the program's and
// reports them; on any other page downloads are the browser's own business, as before. Caller holds p.Mu.
func (p *webPane) downloadMode(c *cdpConn) {
	if c == nil || p.St == nil {
		return
	}
	dir := ""
	if p.kind == "jinxxy" {
		dir = PaneIncomingDir(p.St)
	}
	switch {
	case dir != "" && (p.dlConn != c || p.dlDir != dir):
		if err := os.MkdirAll(dir, 0755); err != nil {
			core.Logf("内置浏览器：无法创建下载文件夹: %v", err)
			return
		}
		_, err := c.call("Browser.setDownloadBehavior", map[string]any{"behavior": "allowAndName", "downloadPath": dir, "eventsEnabled": true}, 5*time.Second)
		if err != nil {
			core.Logf("内置浏览器：无法接管下载，文件将由浏览器自行保存: %v", err)
			return
		}
		p.dlConn, p.dlDir = c, dir
	case dir == "" && p.dlConn == c && !paneFilesRunning(): // (a file still on its way keeps it until it is here)
		_, _ = c.call("Browser.setDownloadBehavior", map[string]any{"behavior": "default"}, 5*time.Second)
		p.dlConn, p.dlDir = nil, ""
	}
}

// jinxxyBegan: did a Jinxxy page begin a download? Told by the page in the frame the browser names — every
// script context known for that frame must be Jinxxy's — else (a frame not heard of) by the page the pane is
// on. The pane is Jinxxy's tab, but not every page in it is Jinxxy's: its search is a search engine's page,
// and a link there leads anywhere.
func (p *webPane) jinxxyBegan(frame string) bool {
	known, all := false, true
	p.Mu.Lock()
	for ctx, f := range p.frames {
		if frame != "" && f == frame && p.origins[ctx] != "" {
			known = true
			all = all && jinxxyOrigin(p.origins[ctx])
		}
	}
	p.Mu.Unlock()
	if known {
		return all
	}
	return jinxxyOrigin(p.currentURL())
}

// stopDownload: the browser is told to drop a download, and what it wrote of it is removed.
func (p *webPane) stopDownload(dir, guid string) {
	p.Mu.Lock()
	c := p.dlConn
	p.Mu.Unlock()
	if c != nil && c.alive() {
		_, _ = c.call("Browser.cancelDownload", map[string]any{"guid": guid}, 5*time.Second)
	}
	_ = os.Remove(filepath.Join(dir, guid))
	_ = os.Remove(filepath.Join(dir, guid+".crdownload"))
}

// downloadEvent: the browser started a download, or got further with one. Each event comes on a goroutine of
// its own, so they are not taken to arrive in order: a small file can be reported as finished before it is
// reported as begun. Only a download that a Jinxxy page began is taken in; any other is stopped and said so.
func (p *webPane) downloadEvent(method string, params json.RawMessage) {
	var e struct {
		GUID  string  `json:"guid"`
		Frame string  `json:"frameId"`
		Name  string  `json:"suggestedFilename"`
		Total float64 `json:"totalBytes"`
		Done  float64 `json:"receivedBytes"`
		State string  `json:"state"`
	}
	if json.Unmarshal(params, &e) != nil || !reGUID.MatchString(e.GUID) {
		return
	}
	p.Mu.Lock()
	dir, st := p.dlDir, p.St
	p.Mu.Unlock()
	if dir == "" || st == nil {
		return
	}
	paneFileMu.Lock()
	var f *PaneFile
	for _, x := range paneFiles {
		if x.ID == e.GUID {
			f = x
		}
	}
	if f == nil {
		f = &PaneFile{ID: e.GUID, Status: "running", At: time.Now().Unix()}
		paneFiles = append(paneFiles, f)
		for i := 0; i < len(paneFiles) && len(paneFiles) > paneFilesKeep; {
			if s := paneFiles[i].Status; s == "done" || s == "failed" {
				paneFiles = append(paneFiles[:i], paneFiles[i+1:]...)
				continue
			}
			i++
		}
		core.Downloading.Add(1)
		defer core.BumpRev()
	}
	src := filepath.Join(dir, e.GUID)
	// over, and not taken in: counted off, and the browser gets its downloads back when the pane has moved on
	drop := func() {
		_ = os.Remove(src)
		core.Downloading.Add(-1)
		core.BumpRev()
		p.downloadsOver()
	}
	if method == "Browser.downloadWillBegin" {
		if f.begun {
			paneFileMu.Unlock()
			return
		}
		f.begun = true
		f.Name = core.SafeName(filepath.Base(strings.ReplaceAll(e.Name, `\`, "/")), 120)
		if f.Name == "" || f.Name == "." {
			f.Name = "download"
		}
		name := f.Name
		paneFileMu.Unlock()
		mine := p.jinxxyBegan(e.Frame) // (asked without the list held: the page may have to be asked)
		paneFileMu.Lock()
		f.checked, f.Foreign = true, !mine
		refuse := !mine && f.Status == "running" // (one that is whole already is turned away where it waits, below)
		if refuse {
			f.Status, f.Err = "failed", errForeignFile
		}
		paneFileMu.Unlock()
		switch {
		case refuse:
			core.Logf("内置页面：%s 不是由 Jinxxy 页面发起的下载，已取消", name) // (never the address: it is signed)
			p.stopDownload(dir, e.GUID)
			drop()
		case mine:
			core.Logf("内置页面开始下载 %s", name)
		}
		return
	}
	if f.Status != "running" {
		paneFileMu.Unlock()
		return
	}
	if e.State != "canceled" {
		f.Done, f.Total = max(f.Done, int64(e.Done)), int64(e.Total)
	}
	switch e.State {
	case "completed":
		f.Status = "saving"
	case "canceled":
		f.Status, f.Err = "failed", "下载已中断"
	}
	status := f.Status
	paneFileMu.Unlock()
	switch status {
	case "saving":
		// its name, and whose page began it, come with the report that it began
		name, checked, foreign := "", false, false
		for end := time.Now().Add(paneBeginWait); !checked; {
			paneFileMu.Lock()
			name, checked, foreign = f.Name, f.checked, f.Foreign
			paneFileMu.Unlock()
			if checked || time.Now().After(end) {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if !checked || foreign { // not known to be a Jinxxy page's: not taken in
			paneFileMu.Lock()
			f.Status, f.Err, f.Foreign = "failed", errForeignFile, true
			if f.Name == "" {
				f.Name = "download"
			}
			paneFileMu.Unlock()
			core.Logf("内置页面：一个不是由 Jinxxy 页面发起的下载未收录")
			drop()
			return
		}
		path, err := PaneFileSaved(st, src, name)
		paneFileMu.Lock()
		if err != nil {
			f.Status, f.Err = "failed", err.Error()
			core.Logf("内置页面下载的 %s 未能收录: %v", name, err)
		} else {
			f.Status, f.Path = "done", path
		}
		paneFileMu.Unlock()
		core.Downloading.Add(-1)
		core.BumpRev()
		p.downloadsOver()
	case "failed":
		drop()
	}
}

// downloadsOver: the pane went over to another site while a file was still coming: now the browser gets its
// downloads back.
func (p *webPane) downloadsOver() {
	p.Mu.Lock()
	defer p.Mu.Unlock()
	if p.kind != "jinxxy" && p.dlConn != nil && p.dlConn.alive() {
		p.downloadMode(p.dlConn)
	}
}
