package main

// Downloading Booth purchases inside the program. The Booth login (its session cookie) is taken
// from the window that syncs purchases and kept in the data folder, encrypted for this Windows
// user. booth.pm/downloadables/<id> answers with a redirect to the file on Booth's CDN; the file is
// saved under "<download folder>/<item id> <name>/", so the scan links it to the purchase, and a
// zip is unpacked there.

import (
	"archive/zip"
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
	"unicode/utf8"
)

var errNeedLogin = errors.New("需要登录 Booth")

const boothSessionCookie = "_plaza_session_nktz7u"

type savedCookie struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	Domain  string `json:"domain,omitempty"`
	Expires int64  `json:"expires,omitempty"` // unix seconds, 0 = until the browser closes
}

// DLRecord: a purchase file that has been downloaded (by downloadable id).
type DLRecord struct {
	Item string `json:"item"`
	Path string `json:"path"`
	At   int64  `json:"at"`
}

func boothDLBase() string {
	if v := os.Getenv("VRCLIB_BOOTH_DL"); v != "" { // tests only
		return strings.TrimRight(v, "/")
	}
	return "https://booth.pm"
}

// ---------- the saved login ----------

var sessMu sync.Mutex

func sessionFile() string { return filepath.Join(dataDir, "booth-session.dat") }

func saveBoothSession(cs []savedCookie) error {
	b, _ := json.Marshal(cs)
	enc, err := protectData(b)
	if err != nil {
		return err
	}
	sessMu.Lock()
	defer sessMu.Unlock()
	return os.WriteFile(sessionFile(), enc, 0600)
}

func loadBoothSession() []savedCookie {
	sessMu.Lock()
	b, err := os.ReadFile(sessionFile())
	sessMu.Unlock()
	if err != nil {
		return nil
	}
	dec, err := unprotectData(b)
	if err != nil {
		return nil
	}
	var cs, out []savedCookie
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

func cookieHeader(cs []savedCookie) string {
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
	cs := loadBoothSession()
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

// downloadDir: the folder set in the settings, else the first asset folder. Caller holds st.mu.
// downloadDir: where downloads go. Unless the player chose a folder: "MioVRCdownload" on the disk
// (other than C:) with the most free space — one of the disks the library folders are on, else any
// other hard disk. Once used it stays (st.AutoDLDir), so downloads do not end up all over the place.
// Caller holds st.mu.
func downloadDir(st *Store) string {
	if d := strings.TrimSpace(st.Settings.DownloadDir); d != "" {
		return d
	}
	if st.AutoDLDir != "" && isDir(filepath.Dir(st.AutoDLDir)) {
		return st.AutoDLDir
	}
	return autoDownloadDir(st.Settings.Roots)
}

const autoDLName = "MioVRCdownload"

func autoDownloadDir(roots []string) string {
	pick := func(drives []string) string {
		best, most := "", uint64(0)
		for _, d := range drives {
			if f := diskFree(d + `\`); f > most {
				best, most = d, f
			}
		}
		return best
	}
	var mine []string
	for _, r := range roots {
		d := strings.ToUpper(filepath.VolumeName(r))
		if len(d) == 2 && d[1] == ':' && d != systemDrive() && !containsStr(mine, d) {
			mine = append(mine, d)
		}
	}
	d := pick(mine)
	if d == "" {
		d = pick(fixedDrives())
	}
	if d != "" {
		return d + `\` + autoDLName
	}
	if len(roots) > 0 {
		return roots[0] // only the system disk: the first library folder, as before
	}
	return filepath.Join(dataDir, "downloads")
}

// pinDownloadDir keeps the folder the automatic choice made once something was downloaded there.
func pinDownloadDir(st *Store, dir string) {
	st.mu.Lock()
	changed := strings.TrimSpace(st.Settings.DownloadDir) == "" && st.AutoDLDir != dir
	if changed {
		st.AutoDLDir = dir
	}
	st.mu.Unlock()
	if changed {
		_ = st.Save()
	}
}

var winBad = strings.NewReplacer("<", "", ">", "", ":", "：", "\"", "", "/", "／", "\\", "＼", "|", "｜", "?", "？", "*", "＊")

// safeName makes a Windows-safe file or folder name.
func safeName(s string, max int) string {
	s = winBad.Replace(strings.Map(func(r rune) rune {
		if r < 32 {
			return -1
		}
		return r
	}, s))
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		s = string(r[:max])
	}
	s = strings.TrimRight(s, ". ")
	switch strings.ToUpper(s) {
	case "", "CON", "PRN", "AUX", "NUL", "COM1", "LPT1":
		return "_" + s
	}
	return s
}

// itemFolder: "<item id> <name>" in the download folder; an existing folder of that item is reused.
func itemFolder(dir, id, name string) string {
	if id == "" {
		return filepath.Join(dir, safeName(name, 70))
	}
	if ents, err := os.ReadDir(dir); err == nil {
		for _, e := range ents {
			if e.IsDir() && boothIDFromName(e.Name()) == id && strings.HasPrefix(e.Name(), id) {
				return filepath.Join(dir, e.Name())
			}
		}
	}
	return filepath.Join(dir, safeName(id+" "+name, 70))
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
	taskDownload = &Task{Name: "download", Label: "下载 Booth 已购"}
)

func dlSnapshot() []DLJob {
	dlMu.Lock()
	defer dlMu.Unlock()
	out := make([]DLJob, 0, len(dlJobs))
	for _, j := range dlJobs {
		out = append(out, *j)
	}
	return out
}

func dlNeedLogin() bool {
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
	bumpRev()
}

// setProgress: bytes so far; the window reads it from /api/progress, nothing else changes.
func setProgress(j *DLJob, done, total int64) {
	dlMu.Lock()
	j.Done, j.Total = done, total
	dlMu.Unlock()
}

// QueueDownloads adds files of a purchase (all of them when ids is empty). Returns how many were added.
func QueueDownloads(st *Store, item string, ids []string) (int, error) {
	st.mu.RLock()
	p := st.Purchases[item]
	var add []*DLJob
	if p != nil {
		for i, d := range p.Downloads {
			if len(ids) > 0 && !containsStr(ids, d) {
				continue
			}
			name := ""
			if i < len(p.Files) {
				name = p.Files[i]
			}
			add = append(add, &DLJob{ID: d, Item: item, Name: name, Status: "queued"})
		}
	}
	st.mu.RUnlock()
	if p == nil {
		return 0, errors.New("没有这件已购商品，先同步 Booth 已购")
	}
	if len(add) == 0 {
		return 0, errors.New("这件商品没有可下载的文件")
	}
	return enqueueDownloads(st, add), nil
}

// QueueDownloadFromPage: a download button clicked on the Booth page inside the program. The purchase
// may be newer than the last sync, so the job carries the item's name itself.
func QueueDownloadFromPage(st *Store, id, item, itemName, file string) int {
	st.mu.RLock()
	for pid, p := range st.Purchases {
		if containsStr(p.Downloads, id) {
			item = pid
		}
	}
	st.mu.RUnlock()
	itemName = strings.TrimSpace(itemName)
	if item == "" && itemName == "" {
		itemName = "Booth 下载 " + id
	}
	return enqueueDownloads(st, []*DLJob{{ID: id, Item: item, Name: file, ItemName: itemName, Status: "queued"}})
}

func enqueueDownloads(st *Store, add []*DLJob) int {
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
	bumpRev()
	if start {
		go dlWorker(st)
	}
	return n
}

// ResumeDownloads: after logging in again, the jobs that waited for it go on.
func ResumeDownloads(st *Store) {
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
	bumpRev()
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

func dlWorker(st *Store) {
	dlCancel.Store(false)
	got := 0
	triedLogin := false
	run(taskDownload, func() {
		for {
			j := nextJob()
			if j == nil {
				break
			}
			pos, total := got, got+dlLeft()
			taskDownload.Set(pos, total, j.Name)
			err := downloadJob(st, j)
			if errors.Is(err, errNeedLogin) && !triedLogin {
				triedLogin = true
				taskDownload.Set(pos, total, "正在确认 Booth 登录…")
				if EnsureBoothLogin(st) {
					err = downloadJob(st, j)
				}
			}
			switch {
			case err == nil:
				got++
			case errors.Is(err, errNeedLogin):
				dlMu.Lock()
				for _, o := range dlJobs {
					if o == j || o.Status == "queued" {
						o.Status, o.Err = "login", ""
					}
				}
				dlRunning = false
				dlMu.Unlock()
				taskDownload.Set(0, 0, "需要登录 Booth 才能下载")
				logf("下载需要登录 Booth")
				bumpRev()
				return
			case dlCancel.Load():
				setJob(j, func(j *DLJob) { j.Status, j.Err = "failed", "已取消" })
			default:
				logf("下载失败 %s: %v", j.ID, err)
				setJob(j, func(j *DLJob) { j.Status, j.Err = "failed", err.Error() })
				dropPendingImport(j.Item)
			}
			if dlCancel.Load() {
				CancelDownloads()
			}
		}
		if got > 0 {
			taskDownload.Set(1, 1, fmt.Sprintf("完成：下载了 %d 个文件", got))
		} else {
			taskDownload.Set(0, 0, "")
		}
	})
	if got > 0 {
		st.mu.Lock()
		dir := downloadDir(st)
		inRoots := false
		for _, r := range st.Settings.Roots {
			if underDir(dir, r) {
				inRoots = true
			}
		}
		if !inRoots {
			st.Settings.Roots = append(st.Settings.Roots, dir) // so the downloads show up in the library
		}
		auto := st.Settings.AutoBooth
		st.mu.Unlock()
		_ = st.Save()
		StartPipeline(st, true, true, auto, false, nil)
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
func resolveDownload(st *Store, id string) (string, string, error) {
	cs := loadBoothSession()
	if len(cs) == 0 {
		return "", "", errNeedLogin
	}
	src := boothDLBase() + "/downloadables/" + id
	req, _ := http.NewRequest("GET", src, nil)
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept-Language", "ja,zh-CN;q=0.8")
	req.Header.Set("Referer", boothAccountsBase()+"/library")
	req.Header.Set("Cookie", cookieHeader(cs))
	c := httpClient(st)
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := c.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("连不上 Booth（%s）", friendlyNetErr(err))
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

func downloadJob(st *Store, j *DLJob) error {
	cdn, cdnName, err := resolveDownload(st, j.ID)
	if err != nil {
		return err
	}
	st.mu.RLock()
	dir := downloadDir(st)
	p := st.Purchases[j.Item]
	itemName := j.Item
	if p != nil && p.Name != "" {
		itemName = p.Name
	} else if j.ItemName != "" {
		itemName = j.ItemName
	}
	extract, keep := !st.Settings.NoExtract, st.Settings.KeepZip
	st.mu.RUnlock()
	name := j.Name
	if strings.Contains(cdnName, ".") && len(cdnName) > 3 {
		name = cdnName
	}
	if name == "" {
		name = j.ID + ".zip"
	}
	name = safeName(name, 120)
	folder := itemFolder(dir, j.Item, itemName)
	if err := os.MkdirAll(folder, 0755); err != nil {
		return fmt.Errorf("建不了文件夹：%v", err)
	}
	pinDownloadDir(st, dir)
	setJob(j, func(j *DLJob) { j.Name = name })
	dst := filepath.Join(folder, name)
	if err := fetchFile(st, cdn, dst, j); err != nil {
		return err
	}
	final := dst
	if extract && isArchiveFile(dst) {
		final = folder
		// split archives come as several files: unpack once the item's last file is here, then
		// what was inside (PSD packs in the main zip …)
		if !moreOfItem(j) {
			setJob(j, func(j *DLJob) { j.Status = "unpacking" })
			taskDownload.Set(0, 0, "正在解压 "+name)
			var remove func([]string) error
			if !keep {
				remove = removeFiles
			}
			res := unpackAll([]string{folder}, "", remove, nil)
			for a, e := range res.Failed {
				logf("解压失败 %s: %s", a, e)
			}
		}
	}
	st.mu.Lock()
	if st.Downloaded == nil {
		st.Downloaded = map[string]*DLRecord{}
	}
	st.Downloaded[j.ID] = &DLRecord{Item: j.Item, Path: final, At: time.Now().Unix()}
	st.mu.Unlock()
	_ = st.Save()
	setJob(j, func(j *DLJob) { j.Status, j.Path, j.Err = "done", final, "" })
	logf("已下载 %s → %s", name, final)
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
	pendImports = map[string]ImportReq{} // Booth item id → where to import it
)

func setPendingImport(item string, req ImportReq) {
	pendMu.Lock()
	pendImports[item] = req
	pendMu.Unlock()
}

func importAfterDownload(st *Store, j *DLJob, folder string) {
	pendMu.Lock()
	req, ok := pendImports[j.Item]
	delete(pendImports, j.Item)
	pendMu.Unlock()
	if !ok || j.Item == "" {
		return
	}
	req.Key, req.Paths = "purchase:"+j.Item, []string{folder}
	startImportWhenFree(st, req)
}

// dropPendingImport: the download failed, so the import it was waiting for is off.
func dropPendingImport(item string) {
	pendMu.Lock()
	delete(pendImports, item)
	pendMu.Unlock()
}

// fetchFile downloads u into dst (through dst.part), reporting progress on the job.
func fetchFile(st *Store, u, dst string, j *DLJob) error {
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

func fetchOnce(st *Store, u, dst string, j *DLJob) error {
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Referer", "https://booth.pm/")
	c := httpClient(st)
	c.Timeout = 0 // big files; the transport still times out a silent server
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("下载失败（%s）", friendlyNetErr(err))
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
					taskDownload.Set(int(got>>10), int(total>>10), fmt.Sprintf("%s  %s / %s", j.Name, fmtMB(got), fmtMB(total)))
				}
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			os.Remove(part)
			return fmt.Errorf("下载中断（%s）", friendlyNetErr(rerr))
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	if total > 0 && got != total {
		os.Remove(part)
		return fmt.Errorf("下载不完整（%s / %s）", fmtMB(got), fmtMB(total))
	}
	setProgress(j, got, got)
	_ = os.Remove(dst)
	return os.Rename(part, dst)
}

// ---------- unpacking ----------

// zipEntryName: the name of an entry as the author saw it. Zips from Japanese Windows store names
// in Shift-JIS without saying so; a name that is not valid UTF-8 is read that way.
func zipEntryName(f *zip.File) string {
	n := f.Name
	if f.NonUTF8 && !utf8.ValidString(n) {
		if s, ok := decodeCP932([]byte(n)); ok {
			n = s
		}
	}
	return strings.ReplaceAll(n, "\\", "/")
}

// extractZip unpacks zipPath into parent: a zip holding a single folder keeps that folder, anything
// else goes into a folder named after the zip. Returns the folder that was made.
func extractZip(zipPath, parent string) (string, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", fmt.Errorf("打不开压缩包：%v", err)
	}
	defer zr.Close()
	type ent struct {
		f   *zip.File
		rel string
	}
	var ents []ent
	tops := map[string]bool{}
	nested := true
	for _, f := range zr.File {
		rel := path.Clean("/" + zipEntryName(f))[1:] // no "..", no absolute paths
		if rel == "" || rel == "." || strings.HasPrefix(rel, "__MACOSX") || strings.HasSuffix(rel, ".DS_Store") {
			continue
		}
		parts := strings.Split(rel, "/")
		tops[parts[0]] = true
		if len(parts) == 1 && !f.FileInfo().IsDir() {
			nested = false
		}
		ents = append(ents, ent{f, rel})
	}
	if len(ents) == 0 {
		return "", errors.New("压缩包是空的")
	}
	var target, strip string
	if len(tops) == 1 && nested {
		for t := range tops {
			strip = t + "/"
			target = filepath.Join(parent, safeName(t, 120))
		}
	} else {
		target = filepath.Join(parent, safeName(stripArchiveExt(filepath.Base(zipPath)), 120))
	}
	for i := 2; ; i++ { // never write into something that is already there
		if _, err := os.Stat(target); os.IsNotExist(err) {
			break
		}
		base := strings.TrimSuffix(target, fmt.Sprintf(" (%d)", i-1))
		target = fmt.Sprintf("%s (%d)", base, i)
	}
	tmp := filepath.Join(parent, "."+filepath.Base(target)+".extracting")
	_ = os.RemoveAll(tmp)
	for _, e := range ents {
		rel := strings.TrimPrefix(e.rel, strip)
		if rel == "" || rel == strings.TrimSuffix(strip, "/") {
			continue
		}
		segs := strings.Split(rel, "/")
		for i := range segs {
			segs[i] = safeName(segs[i], 200)
		}
		out := filepath.Join(append([]string{tmp}, segs...)...)
		if e.f.FileInfo().IsDir() {
			if err := os.MkdirAll(out, 0755); err != nil {
				return "", err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(out), 0755); err != nil {
			return "", err
		}
		if err := writeZipEntry(e.f, out); err != nil {
			_ = os.RemoveAll(tmp)
			return "", fmt.Errorf("解压出错：%v", err)
		}
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.RemoveAll(tmp)
		return "", err
	}
	return target, nil
}

func writeZipEntry(f *zip.File, out string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	w, err := os.Create(out)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, rc); err != nil {
		w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	if !f.Modified.IsZero() {
		_ = os.Chtimes(out, f.Modified, f.Modified)
	}
	return nil
}
