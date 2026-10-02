package main

// Updates from the GitHub releases of the project. The portable zip of a newer release replaces the
// running exe in place (a running exe can be renamed on Windows, not overwritten), then the new exe
// is started and this one quits. A release with only an installer gets that installer started.

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// The repository was called CokoIya/vrclib before 1.6; GitHub redirects the old name, so older
// versions still find the releases published here.
const updateRepo = "CokoIya/MioVRC_AssetManager"

type UpdateAsset struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Size   int64  `json:"size"`
	Digest string `json:"digest,omitempty"` // "sha256:…" when GitHub provides it
}

type UpdateInfo struct {
	Version   string       `json:"version"`
	Tag       string       `json:"tag"`
	Name      string       `json:"name"`
	Notes     string       `json:"notes"`
	URL       string       `json:"url"` // release page
	Published string       `json:"published"`
	Zip       *UpdateAsset `json:"zip,omitempty"`
	Setup     *UpdateAsset `json:"setup,omitempty"`
}

var (
	reVerNum     = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)+`)
	taskUpdate   = &Task{Name: "update", Label: "更新"}
	updateBusy   atomic.Bool
	updateDoneCh = make(chan struct{}, 1) // tells main() to quit once the new exe is started
)

func githubAPI() string {
	if v := os.Getenv("VRCLIB_GITHUB_API"); v != "" { // tests only
		return strings.TrimRight(v, "/")
	}
	return "https://api.github.com"
}

// githubSite: the web site itself, asked when the API will not answer (it allows 60 requests an hour per
// address, which a shared proxy uses up for everyone behind it).
func githubSite() string {
	if v := os.Getenv("VRCLIB_GITHUB_SITE"); v != "" { // tests only
		return strings.TrimRight(v, "/")
	}
	return "https://github.com"
}

func releasesPage() string { return "https://github.com/" + updateRepo + "/releases" }

func parseVer(s string) []int {
	m := reVerNum.FindString(s)
	if m == "" {
		return nil
	}
	var out []int
	for _, p := range strings.Split(m, ".") {
		n, _ := strconv.Atoi(p)
		out = append(out, n)
	}
	return out
}

// versionNewer: is a a later version than b ("v1.4.0" > "1.3.1")?
func versionNewer(a, b string) bool {
	va, vb := parseVer(a), parseVer(b)
	if va == nil {
		return false
	}
	for i := 0; i < len(va) || i < len(vb); i++ {
		x, y := 0, 0
		if i < len(va) {
			x = va[i]
		}
		if i < len(vb) {
			y = vb[i]
		}
		if x != y {
			return x > y
		}
	}
	return false
}

func updateClient(st *Store, timeout time.Duration) *http.Client {
	c := httpClient(st) // same proxy as Booth
	c.Timeout = timeout
	return c
}

// fetchLatestRelease reads the newest published (not draft, not pre-release) release: from the API, and from
// the release pages when the API cannot be reached or has had enough of this address.
func fetchLatestRelease(st *Store) (*UpdateInfo, error) {
	info, err := fetchLatestAPI(st)
	if err == nil || errors.Is(err, errNoRelease) {
		return info, err
	}
	if alt, e2 := fetchLatestSite(st); e2 == nil {
		logf("检查更新：API 没有回答（%v），改从发布页读到 %s", err, alt.Version)
		return alt, nil
	}
	return nil, err
}

var errNoRelease = errors.New("GitHub 上还没有发布版本")

// fetchLatestSite: /releases/latest redirects to the newest release's page, which gives the tag; the files are
// where the releases of this program always put them.
func fetchLatestSite(st *Store) (*UpdateInfo, error) {
	c := updateClient(st, 25*time.Second)
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	base := githubSite() + "/" + updateRepo + "/releases"
	req, _ := http.NewRequest("GET", base+"/latest", nil)
	req.Header.Set("User-Agent", appID+"/"+appVersion)
	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连不上 GitHub（%s）", friendlyNetErr(err))
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	i := strings.LastIndex(loc, "/releases/tag/")
	if resp.StatusCode < 300 || resp.StatusCode > 399 || i < 0 {
		return nil, fmt.Errorf("GitHub 的发布页返回 %d", resp.StatusCode)
	}
	tag := loc[i+len("/releases/tag/"):]
	if t, err := url.PathUnescape(tag); err == nil {
		tag = t
	}
	info := &UpdateInfo{Tag: tag, Version: reVerNum.FindString(tag), URL: base + "/tag/" + url.PathEscape(tag)}
	if info.Version == "" {
		return nil, errors.New("发布版本的标签里没有版本号（例如 v1.4.0）")
	}
	head := updateClient(st, 25*time.Second)
	probe := func(name string) *UpdateAsset {
		u := base + "/download/" + url.PathEscape(tag) + "/" + name
		a := &UpdateAsset{Name: name, URL: u}
		req, _ := http.NewRequest("HEAD", u, nil)
		req.Header.Set("User-Agent", appID+"/"+appVersion)
		if resp, err := head.Do(req); err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				if resp.ContentLength > 0 {
					a.Size = resp.ContentLength
				}
				return a
			}
			if resp.StatusCode == 404 {
				return nil
			}
		}
		// a file store that will not answer HEAD: ask for the first byte instead
		req, _ = http.NewRequest("GET", u, nil)
		req.Header.Set("User-Agent", appID+"/"+appVersion)
		req.Header.Set("Range", "bytes=0-0")
		resp, err := head.Do(req)
		if err != nil {
			return nil
		}
		resp.Body.Close()
		if resp.StatusCode != 200 && resp.StatusCode != 206 {
			return nil
		}
		if _, total, ok := strings.Cut(resp.Header.Get("Content-Range"), "/"); ok {
			if n, err := strconv.ParseInt(total, 10, 64); err == nil && n > 0 {
				a.Size = n
			}
		}
		return a
	}
	info.Zip = probe(appID + "-portable-" + info.Version + ".zip")
	info.Setup = probe(appID + "-setup-" + info.Version + ".exe")
	return info, nil
}

func fetchLatestAPI(st *Store) (*UpdateInfo, error) {
	req, _ := http.NewRequest("GET", githubAPI()+"/repos/"+updateRepo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", appID+"/"+appVersion)
	resp, err := updateClient(st, 25*time.Second).Do(req)
	if err != nil {
		return nil, fmt.Errorf("连不上 GitHub（%s）", friendlyNetErr(err))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	switch resp.StatusCode {
	case 200:
	case 404:
		return nil, errNoRelease
	case 403, 429:
		return nil, errors.New("GitHub 暂时限制了访问次数，过一会儿再试")
	default:
		return nil, fmt.Errorf("GitHub 返回 %d", resp.StatusCode)
	}
	var r struct {
		Tag       string `json:"tag_name"`
		Name      string `json:"name"`
		Body      string `json:"body"`
		HTMLURL   string `json:"html_url"`
		Published string `json:"published_at"`
		Assets    []struct {
			Name   string `json:"name"`
			URL    string `json:"browser_download_url"`
			Size   int64  `json:"size"`
			Digest string `json:"digest"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, errors.New("GitHub 的回复看不懂")
	}
	info := &UpdateInfo{Tag: r.Tag, Name: r.Name, Notes: strings.TrimSpace(r.Body), URL: r.HTMLURL, Published: r.Published}
	if v := reVerNum.FindString(r.Tag); v != "" {
		info.Version = v
	} else {
		info.Version = reVerNum.FindString(r.Name)
	}
	if info.URL == "" {
		info.URL = releasesPage()
	}
	// any zip / exe will do; names saying "portable" / "setup" win
	score := func(name string, words ...string) int {
		l := strings.ToLower(name)
		for _, w := range words {
			if strings.Contains(l, w) {
				return 2
			}
		}
		return 1
	}
	zipScore, setupScore := 0, 0
	for _, a := range r.Assets {
		ua := &UpdateAsset{Name: a.Name, URL: a.URL, Size: a.Size, Digest: a.Digest}
		switch strings.ToLower(filepath.Ext(a.Name)) {
		case ".zip":
			if s := score(a.Name, "portable", "便携", "win"); s > zipScore {
				info.Zip, zipScore = ua, s
			}
		case ".exe":
			if s := score(a.Name, "setup", "install", "安装"); s > setupScore {
				info.Setup, setupScore = ua, s
			}
		}
	}
	if info.Version == "" {
		return nil, errors.New("发布版本的标签里没有版本号（例如 v1.4.0）")
	}
	return info, nil
}

// CheckUpdate asks GitHub for the latest release (at most once an hour unless forced). A check that failed
// does not count: the next one asks again.
func CheckUpdate(st *Store, force bool) (*UpdateInfo, error) {
	st.mu.RLock()
	cached, last := st.Update, st.UpdateChecked
	st.mu.RUnlock()
	if !force && cached != nil && time.Now().Unix()-last < 3600 {
		return cached, nil
	}
	info, err := fetchLatestRelease(st)
	st.mu.Lock()
	if err == nil {
		st.UpdateChecked = time.Now().Unix()
		st.Update = info
	}
	st.mu.Unlock()
	_ = st.Save()
	bumpRev()
	if err == nil {
		logf("检查更新：最新 %s，当前 %s", info.Version, appVersion)
	}
	return info, err
}

// ---------- install ----------

func download(st *Store, a *UpdateAsset, dst string, prog *Task) error {
	req, _ := http.NewRequest("GET", a.URL, nil)
	req.Header.Set("User-Agent", appID+"/"+appVersion)
	resp, err := updateClient(st, 15*time.Minute).Do(req)
	if err != nil {
		return fmt.Errorf("下载失败（%s）", friendlyNetErr(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("下载失败（%d）", resp.StatusCode)
	}
	total := a.Size
	if total <= 0 {
		total = resp.ContentLength
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	h := sha256.New()
	var got int64
	buf := make([]byte, 64<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				return werr
			}
			h.Write(buf[:n])
			got += int64(n)
			if total > 0 {
				prog.Set(int(got>>10), int(total>>10), fmt.Sprintf("正在下载 %s / %s", fmtMB(got), fmtMB(total)))
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return fmt.Errorf("下载中断（%s）", friendlyNetErr(rerr))
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	if a.Size > 0 && got != a.Size {
		return fmt.Errorf("下载不完整（%d / %d 字节）", got, a.Size)
	}
	if d, ok := strings.CutPrefix(strings.ToLower(a.Digest), "sha256:"); ok && d != hex.EncodeToString(h.Sum(nil)) {
		return errors.New("下载的文件校验不通过，可能被改动过，已停止更新")
	}
	return nil
}

func fmtMB(n int64) string { return fmt.Sprintf("%.1f MB", float64(n)/(1<<20)) }

// exeFromZip writes the program inside a portable zip to dst.
func exeFromZip(zipPath, dst, want string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return errors.New("更新包打不开")
	}
	defer zr.Close()
	var pick *zip.File
	for _, f := range zr.File {
		if strings.EqualFold(filepath.Ext(f.Name), ".exe") && !strings.Contains(f.Name, "卸载") {
			if pick == nil || strings.EqualFold(filepath.Base(f.Name), want) {
				pick = f
			}
		}
	}
	if pick == nil {
		return errors.New("更新包里没有程序文件")
	}
	rc, err := pick.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		b := make([]byte, 2)
		if f, err := os.Open(dst); err == nil {
			_, _ = io.ReadFull(f, b)
			f.Close()
		}
		if string(b) != "MZ" {
			return errors.New("更新包里的程序文件不对")
		}
	}
	_ = os.Chmod(dst, 0755)
	return nil
}

// ApplyUpdate downloads the latest release and switches to it. On success the process is told to
// quit (updateDoneCh) after the new one has been started.
func ApplyUpdate(st *Store) error {
	if !updateBusy.CompareAndSwap(false, true) {
		return errors.New("正在更新中")
	}
	st.mu.RLock()
	info := st.Update
	st.mu.RUnlock()
	if info == nil || !versionNewer(info.Version, appVersion) {
		updateBusy.Store(false)
		return errors.New("已经是最新版本")
	}
	exe, err := os.Executable()
	if err == nil {
		exe, _ = filepath.EvalSymlinks(exe)
	}
	if err != nil || exe == "" {
		updateBusy.Store(false)
		return errors.New("找不到程序自己的位置")
	}
	dir := filepath.Dir(exe)
	inPlace := info.Zip != nil && dirWritable(dir)
	if !inPlace && info.Setup == nil {
		updateBusy.Store(false)
		if info.Zip != nil {
			return errors.New("程序所在文件夹不能写入，请到发布页手动下载")
		}
		return errors.New("这个版本没有可下载的安装包，请到发布页手动下载")
	}
	go run(taskUpdate, func() {
		defer updateBusy.Store(false)
		fail := func(err error) {
			logf("更新失败: %v", err)
			taskUpdate.Set(0, 0, "更新失败："+err.Error())
		}
		tmp := filepath.Join(dataDir, "update")
		_ = os.MkdirAll(tmp, 0755)
		if inPlace {
			zp := filepath.Join(tmp, appID+"-"+info.Version+".zip")
			if err := download(st, info.Zip, zp, taskUpdate); err != nil {
				fail(err)
				return
			}
			taskUpdate.Set(1, 1, "正在替换程序")
			newExe, oldExe := exe+".new", exe+".old"
			if err := exeFromZip(zp, newExe, filepath.Base(exe)); err != nil {
				os.Remove(newExe)
				fail(err)
				return
			}
			os.Remove(oldExe)
			if err := os.Rename(exe, oldExe); err != nil {
				os.Remove(newExe)
				fail(fmt.Errorf("替换程序失败（%v）", err))
				return
			}
			if err := os.Rename(newExe, exe); err != nil {
				_ = os.Rename(oldExe, exe)
				fail(fmt.Errorf("替换程序失败（%v）", err))
				return
			}
			os.Remove(zp)
			if isInstalledDir(dir) {
				setInstalledVersion(info.Version)
			}
			logf("已更新到 %s，正在重启", info.Version)
			taskUpdate.Set(1, 1, "已更新，正在重启")
			if err := restartSelf(exe); err != nil {
				// the new exe is in place; it will be used next time
				fail(fmt.Errorf("已更新，但没能自动重启，请手动重新打开（%v）", err))
				return
			}
		} else {
			sp := filepath.Join(tmp, appID+"-setup-"+info.Version+".exe")
			if err := download(st, info.Setup, sp, taskUpdate); err != nil {
				fail(err)
				return
			}
			taskUpdate.Set(1, 1, "正在打开安装程序")
			if err := exec.Command(sp).Start(); err != nil {
				fail(fmt.Errorf("打不开安装程序（%v）", err))
				return
			}
		}
		_ = st.Save()
		time.Sleep(600 * time.Millisecond) // let the window see the last progress message
		select {
		case updateDoneCh <- struct{}{}:
		default:
		}
	})
	return nil
}

// restartSelf starts the (new) exe, which waits for this process to exit before taking over.
func restartSelf(exe string) error {
	var args []string
	skip := false
	for _, a := range os.Args[1:] {
		if skip {
			skip = false
			continue
		}
		if a == "--wait-pid" || a == "-wait-pid" || a == "--updated-from" || a == "-updated-from" {
			skip = true
			continue
		}
		if strings.HasPrefix(a, "--wait-pid=") || strings.HasPrefix(a, "-wait-pid=") || strings.Contains(a, "updated-from=") {
			continue
		}
		args = append(args, a)
	}
	args = append(args, "--wait-pid", strconv.Itoa(os.Getpid()), "--updated-from", appVersion)
	cmd := exec.Command(exe, args...)
	cmd.Dir = filepath.Dir(exe)
	return cmd.Start()
}

// afterUpdate runs in the new exe: waits for the old process, then removes the old exe.
func afterUpdate(waitPid int) {
	if waitPid > 0 {
		waitPidExit(waitPid, 20*time.Second)
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if isInstalledDir(filepath.Dir(exe)) {
		setInstalledVersion(appVersion) // also the new name, for copies installed before the rename
	}
	go func() {
		for i := 0; i < 20; i++ {
			if err := os.Remove(exe + ".old"); err == nil || os.IsNotExist(err) {
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
	}()
}

// autoCheckUpdate: a quiet check some seconds after start, then every two hours while the program stays open
// (a release published in the meantime is noticed without a restart).
func autoCheckUpdate(st *Store) {
	time.Sleep(8 * time.Second)
	for {
		st.mu.RLock()
		off := st.Settings.NoUpdateCheck
		st.mu.RUnlock()
		if !off && !updateBusy.Load() {
			if _, err := CheckUpdate(st, false); err != nil {
				logf("检查更新失败: %v", err)
			}
		}
		time.Sleep(2 * time.Hour)
	}
}
