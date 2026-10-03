package purchases

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
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
}

var (
	dlMu         sync.Mutex
	dlJobs       []*DLJob
	dlRunning    bool
	dlCancel     atomic.Bool
	TaskDownload = &core.Task{Name: "download", Label: "下载 Booth 已购"}
)

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
		return 0, errors.New("没有这件已购商品，先同步 Booth 已购")
	}
	if len(add) == 0 {
		return 0, errors.New("这件商品没有可下载的文件")
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

// QueueDownloadFromPage: a download button clicked on the Booth page inside the program. The purchase
// may be newer than the last sync, so the job carries the item's name itself.
func QueueDownloadFromPage(st *core.Store, id, item, itemName, file string) int {
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

func enqueueDownloads(st *core.Store, add []*DLJob) int {
	n := 0
	dlMu.Lock()
	for _, j := range add {
		dup := false
		for i, o := range dlJobs {
			if o.ID == j.ID {
				if o.Status == "queued" || o.Status == "running" || o.Status == "unpacking" || o.Status == "login" {
					dup = true
				} else {
					dlJobs = append(dlJobs[:i], dlJobs[i+1:]...) // finished before: run again
				}
				break
			}
		}
		if !dup {
			dlJobs = append(dlJobs, j)
			n++
		}
	}
	if len(dlJobs) > 200 {
		dlJobs = dlJobs[len(dlJobs)-200:]
	}
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
	start := n > 0 && !dlRunning
	if start {
		dlRunning = true
	}
	dlMu.Unlock()
	if start {
		go dlWorker(st)
	}
}

func CancelDownloads() {
	dlCancel.Store(true)
	dlMu.Lock()
	for _, j := range dlJobs {
		if j.Status == "queued" || j.Status == "login" {
			j.Status, j.Err = "failed", "已取消"
		}
	}
	dlMu.Unlock()
	core.BumpRev()
}

func nextJob() *DLJob {
	dlMu.Lock()
	defer dlMu.Unlock()
	for _, j := range dlJobs {
		if j.Status == "queued" {
			j.Status = "running"
			return j
		}
	}
	dlRunning = false
	return nil
}

func dlWorker(st *core.Store) {
	dlCancel.Store(false)
	got := 0
	triedLogin := false
	core.RunTask(TaskDownload, func() {
		for {
			j := nextJob()
			if j == nil {
				break
			}
			pos, total := got, got+dlLeft()
			TaskDownload.Set(pos, total, j.Name)
			err := downloadJob(st, j)
			if errors.Is(err, errNeedLogin) && !triedLogin {
				triedLogin = true
				TaskDownload.Set(pos, total, "正在确认 Booth 登录…")
				if EnsureBoothLogin(st) {
					err = downloadJob(st, j)
				}
			}
			switch {
			case err == nil:
				got++
			case errors.Is(err, errGumLogin):
				setJob(j, func(j *DLJob) {
					j.Status, j.Err = "failed", "要重新登录 Gumroad：在左边「Gumroad 已购」点「登录」，再点下载"
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
				dlMu.Unlock()
				TaskDownload.Set(0, 0, "需要登录 Booth 才能下载")
				core.Logf("下载需要登录 Booth")
				core.BumpRev()
				return
			case dlCancel.Load():
				setJob(j, func(j *DLJob) { j.Status, j.Err = "failed", "已取消" })
			default:
				core.Logf("下载失败 %s: %v", j.ID, err)
				setJob(j, func(j *DLJob) { j.Status, j.Err = "failed", err.Error() })
				dropPendingImport(j.Item)
			}
			if dlCancel.Load() {
				CancelDownloads()
			}
		}
		if got > 0 {
			TaskDownload.Set(1, 1, fmt.Sprintf("完成：下载了 %d 个文件", got))
		} else {
			TaskDownload.Set(0, 0, "")
		}
	})
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
		if j.Status == "queued" || j.Status == "running" || j.Status == "unpacking" {
			n++
		}
	}
	return n
}

// resolveDownload asks Booth where the file is. needCookie: the answer was the file itself.
func resolveDownload(st *core.Store, id string) (string, string, error) {
	if core.IsGumID(id) {
		return resolveGumDownload(st, id)
	}
	cs := LoadBoothSession()
	if len(cs) == 0 {
		return "", "", errNeedLogin
	}
	src := core.BoothDLBase() + "/downloadables/" + id
	req, _ := http.NewRequest("GET", src, nil)
	req.Header.Set("User-Agent", core.UA)
	req.Header.Set("Accept-Language", "ja,zh-CN;q=0.8")
	req.Header.Set("Referer", core.BoothAccountsBase()+"/library")
	req.Header.Set("Cookie", cookieHeader(cs))
	c := core.HTTPClient(st)
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := c.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("连不上 Booth（%s）", core.FriendlyNetErr(err))
	}
	resp.Body.Close()
	refreshSession(resp)
	switch {
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		loc, err := resp.Location()
		if err != nil {
			return "", "", errors.New("Booth 没有给出下载地址")
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
		return "", "", errors.New("Booth 上找不到这个文件（可能已删除）")
	case resp.StatusCode == 200:
		return "", "", errNeedLogin // a page instead of a file: the login page
	}
	return "", "", fmt.Errorf("Booth 返回 %d", resp.StatusCode)
}

func downloadJob(st *core.Store, j *DLJob) error {
	cdn, cdnName, err := resolveDownload(st, j.ID)
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
		return fmt.Errorf("建不了文件夹：%v", err)
	}
	PinDownloadDir(st, dir)
	setJob(j, func(j *DLJob) { j.Name = name })
	dst := filepath.Join(folder, name)
	if err := fetchFile(st, cdn, dst, j); err != nil {
		return err
	}
	final := dst
	if extract && archive.IsArchiveFile(dst) {
		final = folder
		// split archives come as several files: unpack once the item's last file is here, then
		// what was inside (PSD packs in the main zip …)
		if !moreOfItem(j) {
			setJob(j, func(j *DLJob) { j.Status = "unpacking" })
			TaskDownload.Set(0, 0, "正在解压 "+name)
			var remove func([]string) error
			if !keep {
				remove = archive.RemoveFiles
			}
			res := archive.UnpackAll([]string{folder}, "", remove, nil)
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

// moreOfItem: other files of the same item still waiting or downloading.
func moreOfItem(j *DLJob) bool {
	dlMu.Lock()
	defer dlMu.Unlock()
	for _, o := range dlJobs {
		if o == j || (o.Status != "queued" && o.Status != "running") {
			continue
		}
		if o.Item == j.Item && (j.Item != "" || o.ItemName == j.ItemName) {
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

// fetchFile downloads u into dst (through dst.part), reporting progress on the job.
func fetchFile(st *core.Store, u, dst string, j *DLJob) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if dlCancel.Load() {
			return errors.New("已取消")
		}
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*3) * time.Second)
		}
		lastErr = fetchOnce(st, u, dst, j)
		if lastErr == nil {
			return nil
		}
	}
	return lastErr
}

func fetchOnce(st *core.Store, u, dst string, j *DLJob) error {
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", core.UA)
	req.Header.Set("Referer", "https://booth.pm/")
	c := core.HTTPClient(st)
	c.Timeout = 0 // big files; the transport still times out a silent server
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("下载失败（%s）", core.FriendlyNetErr(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("下载失败（%d）", resp.StatusCode)
	}
	// a connection that goes silent mid-file would otherwise hang forever
	idle := time.AfterFunc(90*time.Second, func() { resp.Body.Close() })
	defer idle.Stop()
	total := resp.ContentLength
	part := dst + ".part"
	f, err := os.Create(part)
	if err != nil {
		return err
	}
	var got int64
	buf := make([]byte, 256<<10)
	last := time.Time{}
	for {
		if dlCancel.Load() {
			f.Close()
			os.Remove(part)
			return errors.New("已取消")
		}
		n, rerr := resp.Body.Read(buf)
		idle.Reset(90 * time.Second)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				os.Remove(part)
				return fmt.Errorf("写入失败：%v", werr)
			}
			got += int64(n)
			if time.Since(last) > 400*time.Millisecond {
				last = time.Now()
				setProgress(j, got, total)
				if total > 0 {
					TaskDownload.Set(int(got>>10), int(total>>10), fmt.Sprintf("%s  %s / %s", j.Name, core.FmtMB(got), core.FmtMB(total)))
				}
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			os.Remove(part)
			return fmt.Errorf("下载中断（%s）", core.FriendlyNetErr(rerr))
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	if total > 0 && got != total {
		os.Remove(part)
		return fmt.Errorf("下载不完整（%s / %s）", core.FmtMB(got), core.FmtMB(total))
	}
	setProgress(j, got, got)
	_ = os.Remove(dst)
	return os.Rename(part, dst)
}

// ---------- unpacking ----------
