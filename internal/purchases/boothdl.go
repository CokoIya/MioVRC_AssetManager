package purchases

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"vrclib/internal/archive"
	"vrclib/internal/core"
	"vrclib/internal/library"
	"vrclib/internal/naming"
	"vrclib/internal/unity"
	"vrclib/internal/webpane"
)

var errNeedLogin = errors.New("需要登录 Booth")

const boothSessionCookie = "_plaza_session_nktz7u"

// ---------- the saved login ----------

var sessMu sync.Mutex

func sessionFile() string { return filepath.Join(core.DataDir, "booth-session.dat") }

func saveBoothSession(cs []core.SavedCookie) error {
	b, _ := json.Marshal(cs)
	enc, err := core.ProtectData(b)
	if err != nil {
		return err
	}
	sessMu.Lock()
	defer sessMu.Unlock()
	return os.WriteFile(sessionFile(), enc, 0600)
}

func LoadBoothSession() []core.SavedCookie {
	sessMu.Lock()
	b, err := os.ReadFile(sessionFile())
	sessMu.Unlock()
	if err != nil {
		return nil
	}
	dec, err := core.UnprotectData(b)
	if err != nil {
		return nil
	}
	var cs, out []core.SavedCookie
	_ = json.Unmarshal(dec, &cs)
	now := time.Now().Unix()
	for _, c := range cs {
		if c.Expires == 0 || c.Expires > now {
			out = append(out, c)
		}
	}
	return out
}

func forgetBoothSession() { _ = os.Remove(sessionFile()) }

func cookieHeader(cs []core.SavedCookie) string {
	parts := []string{"adult=t"}
	for _, c := range cs {
		if c.Name != "adult" {
			parts = append(parts, c.Name+"="+c.Value)
		}
	}
	return strings.Join(parts, "; ")
}

// refreshSession keeps a cookie Booth renewed in a response.
func refreshSession(resp *http.Response) {
	set := resp.Cookies()
	if len(set) == 0 {
		return
	}
	cs := LoadBoothSession()
	changed := false
	for _, n := range set {
		for i := range cs {
			if cs[i].Name == n.Name && n.Value != "" && cs[i].Value != n.Value {
				cs[i].Value = n.Value
				if !n.Expires.IsZero() {
					cs[i].Expires = n.Expires.Unix()
				}
				changed = true
			}
		}
	}
	if changed {
		_ = saveBoothSession(cs)
	}
}

// ---------- where files go ----------

// DownloadDir: the folder set in the settings, else the first asset folder. Caller holds st.mu.
// downloadDir: where downloads go. Unless the player chose a folder: "MioVRCdownload" on the disk
// (other than C:) with the most free space — one of the disks the library folders are on, else any
// other hard disk. Once used it stays (st.AutoDLDir), so downloads do not end up all over the place.
// Caller holds st.mu.
func DownloadDir(st *core.Store) string {
	if d := strings.TrimSpace(st.Settings.DownloadDir); d != "" {
		return d
	}
	if st.AutoDLDir != "" && core.IsDir(filepath.Dir(st.AutoDLDir)) {
		return st.AutoDLDir
	}
	return autoDownloadDir(st.Settings.Roots)
}

const autoDLName = "MioVRCdownload"

func autoDownloadDir(roots []string) string {
	pick := func(drives []string) string {
		best, most := "", uint64(0)
		for _, d := range drives {
			if f := core.DiskFree(d + `\`); f > most {
				best, most = d, f
			}
		}
		return best
	}
	var mine []string
	for _, r := range roots {
		d := strings.ToUpper(filepath.VolumeName(r))
		if len(d) == 2 && d[1] == ':' && d != core.SystemDrive() && !core.ContainsStr(mine, d) {
			mine = append(mine, d)
		}
	}
	d := pick(mine)
	if d == "" {
		d = pick(core.FixedDrives())
	}
	if d != "" {
		return d + `\` + autoDLName
	}
	if len(roots) > 0 {
		return roots[0] // only the system disk: the first library folder, as before
	}
	return filepath.Join(core.DataDir, "downloads")
}

// PinDownloadDir keeps the folder the automatic choice made once something was downloaded there.
func PinDownloadDir(st *core.Store, dir string) {
	st.Mu.Lock()
	changed := strings.TrimSpace(st.Settings.DownloadDir) == "" && st.AutoDLDir != dir
	if changed {
		st.AutoDLDir = dir
	}
	st.Mu.Unlock()
	if changed {
		_ = st.Save()
	}
}

// itemFolder: "<item id> <name>" in the download folder; an existing folder of that item is reused.
func itemFolder(dir, id, name string) string {
	if id == "" || core.IsGumID(id) { // a Gumroad purchase has no number to show: the folder is found again by its path
		return filepath.Join(dir, core.SafeName(name, 70))
	}
	if ents, err := os.ReadDir(dir); err == nil {
		for _, e := range ents {
			if e.IsDir() && naming.BoothIDFromName(e.Name()) == id && strings.HasPrefix(e.Name(), id) {
				return filepath.Join(dir, e.Name())
			}
		}
	}
	return filepath.Join(dir, core.SafeName(id+" "+name, 70))
}

// ---------- jobs ----------

type DLJob struct {
	ID       string `json:"id"`                 // downloadable id
	Item     string `json:"item"`               // Booth item id
	Name     string `json:"name"`               // file name
	Status   string `json:"status"`             // queued, running, unpacking, done, failed, login
	ItemName string `json:"itemName,omitempty"` // when the purchase is not synced yet (a download clicked on the Booth page)
	Done     int64  `json:"done"`
	Total    int64  `json:"total"`
	Path     string `json:"path,omitempty"`
	Err      string `json:"err,omitempty"`

	counted bool   // in core.Downloading
	file    string // the file this job downloaded, until it has been handed to the unpacking (itemFiles)
}

var (
	dlMu         sync.Mutex
	dlJobs       []*DLJob
	dlRunning    bool
	dlCtx        context.Context // ends when the player cancels; what is queued after that gets a new one
	dlStop       context.CancelFunc
	dlWorkers    atomic.Int32 // workers that have not ended yet (one that found the queue empty still saves and starts the rescan)
	TaskDownload = &core.Task{Name: "download", Label: "下载已购文件"}
)

const dlKeep = 200 // finished jobs the window still lists

func dlActive(status string) bool {
	return status == "queued" || status == "running" || status == "unpacking"
}

// recountLocked keeps core.Downloading in step with the jobs: a job counts from being queued until it is over
// for whatever reason (one waiting for a login does not: nothing is running for it). Caller holds dlMu.
func recountLocked() {
	for _, j := range dlJobs {
		if a := dlActive(j.Status); a != j.counted {
			j.counted = a
			if a {
				core.Downloading.Add(1)
			} else {
				core.Downloading.Add(-1)
			}
		}
	}
}

// forgetLocked takes job i off the list. Caller holds dlMu.
func forgetLocked(i int) {
	if j := dlJobs[i]; j.counted {
		j.counted = false
		core.Downloading.Add(-1)
	}
	dlJobs = append(dlJobs[:i], dlJobs[i+1:]...)
}

// trimLocked drops the oldest finished jobs over the limit. A job that waits or runs is never dropped: with
// more than that many of them the list is simply longer. Caller holds dlMu.
func trimLocked() {
	for i := 0; i < len(dlJobs) && len(dlJobs) > dlKeep; {
		if s := dlJobs[i].Status; s == "done" || s == "failed" {
			forgetLocked(i)
			continue
		}
		i++
	}
}

func DLSnapshot() []DLJob {
	dlMu.Lock()
	defer dlMu.Unlock()
	out := make([]DLJob, 0, len(dlJobs))
	for _, j := range dlJobs {
		out = append(out, *j)
	}
	return out
}

func DLNeedLogin() bool {
	dlMu.Lock()
	defer dlMu.Unlock()
	for _, j := range dlJobs {
		if j.Status == "login" {
			return true
		}
	}
	return false
}

func setJob(j *DLJob, f func(j *DLJob)) {
	dlMu.Lock()
	f(j)
	recountLocked()
	dlMu.Unlock()
	core.BumpRev()
}

// setProgress: bytes so far; the window reads it from /api/progress, nothing else changes.
func setProgress(j *DLJob, done, total int64) {
	dlMu.Lock()
	j.Done, j.Total = done, total
	dlMu.Unlock()
}

// QueueDownloads adds files of a purchase (all of them when ids is empty). Returns how many were added.
func QueueDownloads(st *core.Store, item string, ids []string) (int, error) {
	st.Mu.RLock()
	p := st.Purchases[item]
	var add []*DLJob
	if p != nil {
		for i, d := range p.Downloads {
			if len(ids) > 0 && !core.ContainsStr(ids, d) {
				continue
			}
			name := ""
			if i < len(p.Files) {
				name = p.Files[i]
			}
			add = append(add, &DLJob{ID: d, Item: item, Name: name, Status: "queued"})
		}
	}
	st.Mu.RUnlock()
	if p == nil {
		return 0, errors.New("未找到该已购商品，请先同步 Booth 已购")
	}
	if len(add) == 0 {
		return 0, errors.New("该商品没有可下载的文件")
	}
	return enqueueDownloads(st, add), nil
}

func init() {
	webpane.PaneDownloadsLeft = dlLeft
	webpane.PaneDownloadClicked = downloadFromPane
}

// downloadFromPane: a download button clicked on a Booth page inside the program goes into the program's
// own downloads.
func downloadFromPane(st *core.Store, id, item, name, file string) {
	if cs, err := webpane.Pane.Cookies(webpane.BoothCookieURLs()); err == nil && hasBoothSession(cs) {
		_ = saveBoothSession(cs) // the page is logged in: so are the downloads
	}
	n := QueueDownloadFromPage(st, id, item, name, file)
	core.Logf("Booth 页面里点了下载 %s（%s）：加入 %d 个", id, file, n)
}

// allDigits: Booth's ids are numbers; anything else a page hands over is not put into an address.
func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != "" && len(s) <= 20
}

// QueueDownloadFromPage: a download button clicked on the Booth page inside the program. The purchase
// may be newer than the last sync, so the job carries the item's name itself.
func QueueDownloadFromPage(st *core.Store, id, item, itemName, file string) int {
	if !allDigits(id) {
		return 0
	}
	if !allDigits(item) {
		item = ""
	}
	st.Mu.RLock()
	for pid, p := range st.Purchases {
		if core.ContainsStr(p.Downloads, id) {
			item = pid
		}
	}
	st.Mu.RUnlock()
	itemName = strings.TrimSpace(itemName)
	if item == "" && itemName == "" {
		itemName = "Booth 下载 " + id
	}
	return enqueueDownloads(st, []*DLJob{{ID: id, Item: item, Name: file, ItemName: itemName, Status: "queued"}})
}

// enqueueDownloads returns how many jobs are now waiting that were not before.
func enqueueDownloads(st *core.Store, add []*DLJob) int {
	n := 0
	dlMu.Lock()
	for _, j := range add {
		dup := false
		for i, o := range dlJobs {
			if o.ID == j.ID {
				if dlActive(o.Status) || o.Status == "login" {
					dup = true
				} else {
					forgetLocked(i) // finished before: run again
				}
				break
			}
		}
		if !dup {
			dlJobs = append(dlJobs, j)
			n++
		}
	}
	trimLocked()
	recountLocked()
	start := !dlRunning && n > 0
	if start {
		dlRunning = true
	}
	dlMu.Unlock()
	core.BumpRev()
	if start {
		go dlWorker(st)
	}
	return n
}

// ResumeDownloads: after logging in again, the jobs that waited for it go on.
func ResumeDownloads(st *core.Store) {
	dlMu.Lock()
	n := 0
	for _, j := range dlJobs {
		if j.Status == "login" {
			j.Status, j.Err = "queued", ""
			n++
		}
	}
	recountLocked()
	start := n > 0 && !dlRunning
	if start {
		dlRunning = true
	}
	dlMu.Unlock()
	if start {
		go dlWorker(st)
	}
}

// CancelDownloads stops the file being downloaded (its request is ended, not waited for) and takes the
// waiting ones off the queue.
func CancelDownloads() {
	dlMu.Lock()
	if dlStop != nil {
		dlStop()
	}
	for _, j := range dlJobs {
		if j.Status == "queued" || j.Status == "login" {
			j.Status, j.Err = "failed", "已取消"
		}
	}
	recountLocked()
	dlMu.Unlock()
	core.BumpRev()
}

// CancelAllDownloads: the program is closing. core.Downloading is back at zero once the running file has let go.
func CancelAllDownloads() { CancelDownloads() }

// nextJob: the next waiting job and the context it runs under.
func nextJob() (*DLJob, context.Context) {
	dlMu.Lock()
	defer dlMu.Unlock()
	for _, j := range dlJobs {
		if j.Status == "queued" {
			j.Status = "running"
			if dlCtx == nil || dlCtx.Err() != nil { // cancelled before: what was queued since then runs
				dlCtx, dlStop = context.WithCancel(context.Background())
			}
			return j, dlCtx
		}
	}
	dlRunning = false
	return nil, nil
}

// runDownload: one job (tests put their own here).
var runDownload = downloadJob

func dlWorker(st *core.Store) {
	dlWorkers.Add(1)
	defer dlWorkers.Add(-1)
	got := 0
	triedLogin := false
	again := false
	core.RunTask(TaskDownload, func() {
		var cur *DLJob
		clean := false
		defer func() {
			if clean {
				return
			}
			// a panic (RunTask reports it): that job is over, and the queue is not left stuck behind it
			dlMu.Lock()
			if cur != nil && dlActive(cur.Status) {
				cur.Status, cur.Err = "failed", "下载出错，详见 library.log"
				for _, o := range dlJobs {
					again = again || o.Status == "queued"
				}
			}
			dlRunning = again
			recountLocked()
			dlMu.Unlock()
		}()
		for {
			j, ctx := nextJob()
			if j == nil {
				break
			}
			cur = j
			pos, total := got, got+dlLeft()
			TaskDownload.Set(pos, total, j.Name)
			err := runDownload(ctx, st, j)
			if errors.Is(err, errNeedLogin) && !triedLogin && ctx.Err() == nil {
				triedLogin = true
				TaskDownload.Set(pos, total, "正在确认 Booth 登录状态…")
				if EnsureBoothLogin(st) {
					err = runDownload(ctx, st, j)
				}
			}
			switch {
			case err == nil:
				got++
			case ctx.Err() != nil:
				setJob(j, func(j *DLJob) { j.Status, j.Err = "failed", "已取消" })
				dropPendingImport(j.Item)
			case errors.Is(err, errGumLogin):
				setJob(j, func(j *DLJob) {
					j.Status, j.Err = "failed", "Gumroad 登录已失效，请在左侧「Gumroad 已购」点击「登录」后重新下载"
				})
				dropPendingImport(j.Item)
			case errors.Is(err, errNeedLogin):
				dlMu.Lock()
				for _, o := range dlJobs {
					if o == j || o.Status == "queued" {
						o.Status, o.Err = "login", ""
					}
				}
				dlRunning = false
				recountLocked()
				dlMu.Unlock()
				TaskDownload.Set(0, 0, "下载前需登录 Booth")
				core.Logf("下载需要登录 Booth")
				core.BumpRev()
				clean = true
				return
			default:
				core.Logf("下载失败 %s: %v", j.ID, err)
				setJob(j, func(j *DLJob) { j.Status, j.Err = "failed", err.Error() })
				dropPendingImport(j.Item)
			}
		}
		clean = true
		if got > 0 {
			TaskDownload.Set(1, 1, fmt.Sprintf("完成：已下载 %d 个文件", got))
		} else {
			TaskDownload.Set(0, 0, "")
		}
	})
	if again {
		go dlWorker(st) // the jobs behind the one that crashed
	}
	if got > 0 {
		st.Mu.Lock()
		dir := DownloadDir(st)
		inRoots := false
		for _, r := range st.Settings.Roots {
			if core.UnderDir(dir, r) {
				inRoots = true
			}
		}
		if !inRoots {
			st.Settings.Roots = append(st.Settings.Roots, dir) // so the downloads show up in the library
		}
		auto := st.Settings.AutoBooth
		st.Mu.Unlock()
		_ = st.Save()
		library.StartPipeline(st, true, true, auto, false, nil)
	}
}

// dlLeft: files still to do (queued or in progress).
func dlLeft() int {
	dlMu.Lock()
	defer dlMu.Unlock()
	n := 0
	for _, j := range dlJobs {
		if dlActive(j.Status) {
			n++
		}
	}
	return n
}

// resolveDownload asks Booth where the file is. needCookie: the answer was the file itself.
func resolveDownload(ctx context.Context, st *core.Store, id string) (string, string, error) {
	if core.IsGumID(id) {
		return resolveGumDownload(ctx, st, id)
	}
	if !allDigits(id) {
		return "", "", errors.New("下载编号无效")
	}
	cs := LoadBoothSession()
	if len(cs) == 0 {
		return "", "", errNeedLogin
	}
	src := core.BoothDLBase() + "/downloadables/" + id
	req, _ := http.NewRequestWithContext(ctx, "GET", src, nil)
	req.Header.Set("User-Agent", core.UA)
	req.Header.Set("Accept-Language", "ja,zh-CN;q=0.8")
	req.Header.Set("Referer", core.BoothAccountsBase()+"/library")
	req.Header.Set("Cookie", cookieHeader(cs))
	c := core.HTTPClient(st)
	defer c.CloseIdleConnections()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := c.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("无法连接 Booth（%s）", NetErrText(err))
	}
	resp.Body.Close()
	refreshSession(resp)
	switch {
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		loc, err := resp.Location()
		if err != nil {
			return "", "", errors.New("Booth 未返回下载地址")
		}
		lp := strings.ToLower(loc.Path)
		if strings.Contains(lp, "sign_in") || strings.Contains(lp, "login") || strings.Contains(loc.Host, "accounts.") {
			return "", "", errNeedLogin
		}
		name, _ := url.PathUnescape(path.Base(loc.Path))
		return loc.String(), name, nil
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return "", "", errNeedLogin
	case resp.StatusCode == 404:
		return "", "", errors.New("Booth 上未找到该文件（可能已删除）")
	case resp.StatusCode == 200:
		return "", "", errNeedLogin // a page instead of a file: the login page
	}
	return "", "", fmt.Errorf("Booth 返回错误（HTTP %d）", resp.StatusCode)
}

func downloadJob(ctx context.Context, st *core.Store, j *DLJob) error {
	cdn, cdnName, err := resolveDownload(ctx, st, j.ID)
	if err != nil {
		return err
	}
	st.Mu.RLock()
	dir := DownloadDir(st)
	p := st.Purchases[j.Item]
	itemName := j.Item
	if p != nil && p.Name != "" {
		itemName = p.Name
	} else if j.ItemName != "" {
		itemName = j.ItemName
	}
	extract, keep := !st.Settings.NoExtract, st.Settings.KeepZip
	st.Mu.RUnlock()
	name := j.Name
	if strings.Contains(cdnName, ".") && len(cdnName) > 3 {
		name = cdnName
	}
	if name == "" {
		name = j.ID + ".zip"
	}
	name = core.SafeName(name, 120)
	folder := itemFolder(dir, j.Item, itemName)
	if err := os.MkdirAll(folder, 0755); err != nil {
		return WriteErr(folder, err)
	}
	PinDownloadDir(st, dir)
	setJob(j, func(j *DLJob) { j.Name = name })
	dst := freePlace(j, folder, name)
	err = fetchFile(ctx, st, cdn, dst, j)
	if errors.Is(err, errStaleURL) && ctx.Err() == nil {
		// the file's address is only good for a while: ask for it again and go on from what is here
		if cdn, _, err = resolveDownload(ctx, st, j.ID); err == nil {
			err = fetchFile(ctx, st, cdn, dst, j)
		}
	}
	if errors.Is(err, errNotFile) && onLoginPage(err) {
		err = errNeedLogin
		if core.IsGumID(j.ID) {
			err = errGumLogin
		}
	}
	if err != nil {
		if ctx.Err() == nil {
			core.Logf("下载失败 %s（%s）", j.ID, LogURL(cdn))
		}
		return err
	}
	final := dst
	if extract && archive.IsArchiveFile(dst) {
		final = folder
	}
	setJob(j, func(j *DLJob) { j.file = dst })
	// split archives come as several files: unpack once the item's last file is here, then what was inside
	// (PSD packs in the main zip …). Only the files these downloads brought are unpacked and, unless they are
	// kept, removed: the folder may hold what the player keeps there — an earlier version, still packed
	if !moreOfItem(j) {
		var arcs []string
		for _, f := range itemFiles(j, dst) { // (this round of the item's downloads is over, unpacked or not)
			if extract && archive.IsArchiveFile(f) {
				arcs = append(arcs, f)
			}
		}
		if len(arcs) > 0 {
			setJob(j, func(j *DLJob) { j.Status = "unpacking" })
			TaskDownload.Set(0, 0, "正在解压 "+name)
			var remove func([]string) error
			if !keep {
				remove = archive.RemoveFiles
			}
			res := archive.UnpackFiles(arcs, "", remove, nil)
			for a, e := range res.Failed {
				core.Logf("解压失败 %s: %s", a, e)
			}
		}
	}
	st.Mu.Lock()
	if st.Downloaded == nil {
		st.Downloaded = map[string]*core.DLRecord{}
	}
	st.Downloaded[j.ID] = &core.DLRecord{Item: j.Item, Path: final, At: time.Now().Unix()}
	st.Mu.Unlock()
	_ = st.Save()
	setJob(j, func(j *DLJob) { j.Status, j.Path, j.Err = "done", final, "" })
	core.Logf("已下载 %s → %s", name, final)
	if !moreOfItem(j) {
		importAfterDownload(st, j, folder)
	}
	return nil
}

// freePlace: where in the item's folder a download called name is saved. A file that is there already — an
// earlier version, a download kept packed, the player's own — is never replaced: the new one goes next to it
// under a name of its own ("Dress (2).zip"). The number goes before what makes a file a volume, and is the
// same for every volume of a set ("Dress (2).part1.rar", "Dress (2).part2.rar"), so the set still opens; the
// item's own files of this round of downloads are not in the way of each other.
func freePlace(j *DLJob, folder, name string) string {
	mine := map[string]bool{}
	dlMu.Lock()
	for _, o := range dlJobs {
		if o.file != "" && sameItem(j, o) {
			mine[core.PathKey(o.file)] = true
		}
	}
	dlMu.Unlock()
	taken := map[string]bool{} // the sets the other files in the folder belong to
	ents, _ := os.ReadDir(folder)
	for _, e := range ents {
		if e.IsDir() || mine[core.PathKey(filepath.Join(folder, e.Name()))] {
			continue
		}
		_, _, key := archive.VolumeName(e.Name())
		taken[key] = true
	}
	stem, rest, _ := archive.VolumeName(name)
	for n := 1; ; n++ {
		as := name
		if n > 1 {
			as = fmt.Sprintf("%s (%d)%s", stem, n, rest)
		}
		// (a name that is there all the same: two files of one name in the same round, or a folder of that name)
		if _, _, key := archive.VolumeName(as); !taken[key] && !core.StatOK(filepath.Join(folder, as)) {
			return filepath.Join(folder, as)
		}
	}
}

// sameItem: do two jobs download files of one item?
func sameItem(j, o *DLJob) bool {
	return o.Item == j.Item && (j.Item != "" || o.ItemName == j.ItemName)
}

// itemFiles: the files the item's downloads brought since its files were last unpacked — this job's (file)
// and those of the item's other jobs. They are handed over once.
func itemFiles(j *DLJob, file string) []string {
	dlMu.Lock()
	defer dlMu.Unlock()
	out := []string{file}
	j.file = ""
	for _, o := range dlJobs {
		if o != j && o.file != "" && sameItem(j, o) {
			if o.file != file {
				out = append(out, o.file)
			}
			o.file = ""
		}
	}
	return out
}

// moreOfItem: other files of the same item still waiting or downloading.
func moreOfItem(j *DLJob) bool {
	dlMu.Lock()
	defer dlMu.Unlock()
	for _, o := range dlJobs {
		if o == j || (o.Status != "queued" && o.Status != "running") {
			continue
		}
		if sameItem(j, o) {
			return true
		}
	}
	return false
}

// ---------- "下载并导入": an import that waits for the item's downloads ----------

var (
	pendMu      sync.Mutex
	pendImports = map[string]unity.ImportReq{} // Booth item id → where to import it
)

func SetPendingImport(item string, req unity.ImportReq) {
	pendMu.Lock()
	pendImports[item] = req
	pendMu.Unlock()
}

func importAfterDownload(st *core.Store, j *DLJob, folder string) {
	pendMu.Lock()
	req, ok := pendImports[j.Item]
	delete(pendImports, j.Item)
	pendMu.Unlock()
	if !ok || j.Item == "" {
		return
	}
	req.Key, req.Paths = "purchase:"+j.Item, []string{folder}
	unity.StartImportWhenFree(st, req)
}

// dropPendingImport: the download failed, so the import it was waiting for is off.
func dropPendingImport(item string) {
	pendMu.Lock()
	delete(pendImports, item)
	pendMu.Unlock()
}

// ---------- fetching one file ----------

var (
	// errStaleURL: the file's (signed) address is no longer taken; a new one has to be asked for.
	errStaleURL = errors.New("下载地址已失效或被拒绝访问，请重新下载")
	// errNotFile: the answer was a web page (an error, maintenance or login page), not the file.
	errNotFile = errors.New("服务器返回的是网页而不是文件（网站可能正在维护，或登录已失效），请稍后重试")
)

// notFileAt: errNotFile, with the page it was.
type notFileAt struct{ login bool }

func (e notFileAt) Error() string   { return errNotFile.Error() }
func (e notFileAt) Is(t error) bool { return t == errNotFile }

func onLoginPage(err error) bool {
	var e notFileAt
	return errors.As(err, &e) && e.login
}

// partTag: what the server said about the file a .part belongs to, so that only the same file is continued.
// Kept while the program runs; a .part left by an earlier run is started again.
type partTag struct {
	total int64
	valid string // ETag, else Last-Modified: sent back as If-Range
}

var (
	partMu   sync.Mutex
	partTags = map[string]partTag{}
)

func partTagOf(part string) (partTag, bool) {
	partMu.Lock()
	defer partMu.Unlock()
	t, ok := partTags[part]
	return t, ok
}

func setPartTag(part string, t *partTag) {
	partMu.Lock()
	defer partMu.Unlock()
	if t == nil {
		delete(partTags, part)
		return
	}
	partTags[part] = *t
}

func dropPart(part string) {
	setPartTag(part, nil)
	_ = os.Remove(part)
}

func fileSize(p string) int64 {
	if fi, err := os.Stat(p); err == nil {
		return fi.Size()
	}
	return 0
}

// fetchPause: between tries (tests shorten it).
var fetchPause = 3 * time.Second

// fetchFile downloads u into dst through dst.part, reporting progress on the job. A try that ends early is
// continued from what it left (Range); only tries that brought nothing count against the limit. What the
// disk refuses, and a page instead of the file, are not asked for again.
func fetchFile(ctx context.Context, st *core.Store, u, dst string, j *DLJob) error {
	part := dst + ".part"
	var lastErr error
	for try, fails := 0, 0; fails < 3 && try < 30; try++ {
		if ctx.Err() != nil {
			dropPart(part)
			return ErrCancelled
		}
		if fails > 0 && !Sleep(ctx, time.Duration(fails)*fetchPause) {
			dropPart(part)
			return ErrCancelled
		}
		before := fileSize(part)
		err := fetchOnce(ctx, st, u, dst, j)
		switch {
		case err == nil:
			return nil
		case ctx.Err() != nil:
			dropPart(part)
			return ErrCancelled
		case IsLocalErr(err):
			dropPart(part) // it only takes up the room that is missing
			return err
		case errors.Is(err, errNotFile):
			dropPart(part)
			return err
		case errors.Is(err, errStaleURL):
			return err
		}
		lastErr = err
		if fileSize(part) <= before {
			fails++
		}
	}
	return lastErr
}

// contentRange reads "bytes 100-999/1000" (total -1 when the server does not say).
func contentRange(h string) (start, total int64, ok bool) {
	h, found := strings.CutPrefix(strings.TrimSpace(h), "bytes ")
	rng, tot, _ := strings.Cut(h, "/")
	a, _, cut := strings.Cut(rng, "-")
	start, err := strconv.ParseInt(strings.TrimSpace(a), 10, 64)
	if !found || !cut || err != nil {
		return 0, 0, false
	}
	if total, err = strconv.ParseInt(strings.TrimSpace(tot), 10, 64); err != nil {
		total = -1
	}
	return start, total, true
}

// looksHTML: a web page, by what the server calls it or by how it begins.
func looksHTML(contentType string, head []byte) bool {
	ct := strings.ToLower(contentType)
	if strings.HasPrefix(ct, "text/html") || strings.HasPrefix(ct, "application/xhtml") {
		return true
	}
	h := strings.ToLower(strings.TrimLeft(strings.TrimPrefix(string(head), "\xef\xbb\xbf"), " \t\r\n"))
	return strings.HasPrefix(h, "<!doctype html") || strings.HasPrefix(h, "<html")
}

// zipEnds: a zip closes with its directory; one cut short does not have it.
func zipEnds(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	n := min(fi.Size(), 65557)
	tail := make([]byte, n)
	if _, err := f.ReadAt(tail, fi.Size()-n); err != nil && err != io.EOF {
		return false
	}
	return bytes.Contains(tail, []byte("PK\x05\x06"))
}

func fetchOnce(ctx context.Context, st *core.Store, u, dst string, j *DLJob) error {
	part := dst + ".part"
	tag, known := partTagOf(part)
	have := fileSize(part)
	if !known || have <= 0 || tag.total <= 0 || have > tag.total {
		dropPart(part) // nothing to go on from, or left by an earlier run: from the start
		have, known = 0, false
	}
	finish := func() error {
		setPartTag(part, nil)
		// (nothing is removed to make room: downloadJob picked a name that was free, and a file that has
		// appeared under it since is not this download's to replace)
		if core.StatOK(dst) {
			_ = os.Remove(part)
			return WriteErr(dst, fs.ErrExist)
		}
		if err := os.Rename(part, dst); err != nil {
			return WriteErr(dst, err)
		}
		return nil
	}
	if known && have == tag.total {
		return finish() // all of it arrived last time
	}
	g := NewStallGuard(ctx)
	defer g.Stop()
	netErr := func(what string, err error) error {
		switch {
		case ctx.Err() != nil:
			return ErrCancelled
		case g.Stalled():
			return fmt.Errorf("%s（%d 秒内未收到数据）", what, int(StallAfter.Seconds()))
		}
		return fmt.Errorf("%s（%s）", what, NetErrText(err))
	}
	req, _ := http.NewRequestWithContext(g.Ctx, "GET", u, nil)
	req.Header.Set("User-Agent", core.UA)
	req.Header.Set("Referer", "https://booth.pm/")
	if have > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
		if tag.valid != "" {
			req.Header.Set("If-Range", tag.valid)
		}
	}
	c := core.HTTPClient(st)
	c.Timeout = 0 // big files: no limit on the whole; the guard gives up a connection that went silent
	defer c.CloseIdleConnections()
	resp, err := c.Do(req)
	if err != nil {
		return netErr("下载失败", err)
	}
	defer resp.Body.Close()
	g.Kick()
	total := tag.total
	switch {
	case resp.StatusCode == 200:
		have, total = 0, resp.ContentLength // the whole file (again)
	case resp.StatusCode == 206 && have > 0:
		if a, t, ok := contentRange(resp.Header.Get("Content-Range")); !ok || a != have || (t >= 0 && t != tag.total) {
			dropPart(part)
			return errors.New("下载失败（服务器返回的数据范围有误）")
		}
	case resp.StatusCode == 416:
		dropPart(part)
		return errors.New("下载失败（服务器不接受续传）")
	case resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 410:
		return errStaleURL
	default:
		return fmt.Errorf("下载失败（HTTP %d）", resp.StatusCode)
	}
	// a page where the file should be: an error or maintenance page, or the login page
	var head []byte
	if resp.StatusCode == 200 {
		head = make([]byte, 512)
		n, err := io.ReadFull(resp.Body, head)
		if head = head[:n]; err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return netErr("下载中断", err)
		}
		g.Kick()
	}
	if ext := core.LowerExt(dst); ext != ".html" && ext != ".htm" && looksHTML(resp.Header.Get("Content-Type"), head) {
		lp := strings.ToLower(resp.Request.URL.Path)
		return notFileAt{login: strings.Contains(lp, "sign_in") || strings.Contains(lp, "login")}
	}
	if total > 0 {
		if err := CheckSpace(filepath.Dir(dst), total-have); err != nil {
			return err
		}
	}
	var f *os.File
	if have > 0 {
		f, err = os.OpenFile(part, os.O_WRONLY|os.O_APPEND, 0644)
	} else {
		f, err = os.Create(part)
		valid := resp.Header.Get("ETag")
		if valid == "" || strings.HasPrefix(valid, "W/") {
			valid = resp.Header.Get("Last-Modified")
		}
		setPartTag(part, &partTag{total: total, valid: valid})
	}
	if err != nil {
		return WriteErr(part, err)
	}
	got := have
	last := time.Time{}
	write := func(b []byte) error {
		if _, err := f.Write(b); err != nil {
			f.Close()
			return WriteErr(part, err)
		}
		got += int64(len(b))
		if time.Since(last) > 400*time.Millisecond {
			last = time.Now()
			setProgress(j, got, total)
			if total > 0 {
				TaskDownload.Set(int(got>>10), int(total>>10), fmt.Sprintf("%s  %s / %s", j.Name, core.FmtMB(got), core.FmtMB(total)))
			}
		}
		return nil
	}
	if err := write(head); err != nil {
		return err
	}
	buf := make([]byte, 256<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			g.Kick()
			if err := write(buf[:n]); err != nil {
				return err
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return netErr("下载中断", rerr)
		}
	}
	if err := f.Close(); err != nil {
		return WriteErr(part, err)
	}
	switch {
	case total >= 0 && got != total:
		if got > total {
			dropPart(part)
		}
		return fmt.Errorf("下载不完整（%s / %s）", core.FmtMB(got), core.FmtMB(total))
	case total < 0 && resp.ProtoMajor < 2 && len(resp.TransferEncoding) == 0 && core.LowerExt(dst) == ".zip" && !zipEnds(part):
		// no length and no chunks: the body ends where the connection does, so only the file itself can
		// tell that it ended early
		dropPart(part)
		return errors.New("下载不完整（压缩包缺少结尾）")
	}
	setProgress(j, got, got)
	return finish()
}
