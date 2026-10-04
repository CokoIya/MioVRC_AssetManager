package follow

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"vrclib/internal/archive"
	"vrclib/internal/core"
	"vrclib/internal/library"
	"vrclib/internal/naming"
	"vrclib/internal/pandl"
	"vrclib/internal/purchases"
	"vrclib/internal/unity"
)

// ProjectRun: one project the new version goes into.
type ProjectRun struct {
	Path     string   `json:"path"`
	Name     string   `json:"name"`
	Status   string   `json:"status"` // waiting, running, choose, done, failed, skipped, cancelled
	Msg      string   `json:"msg,omitempty"`
	Pkgs     int      `json:"pkgs,omitempty"`  // unitypackages imported
	Files    int      `json:"files,omitempty"` // files written
	Kept     int      `json:"kept,omitempty"`  // files of installed packages left alone
	KeptPkgs []string `json:"keptPkgs,omitempty"`
}

// Job: one asset's update, from the download to the last project.
type Job struct {
	ID         int64              `json:"id,string"` // (as text: too long for a number in the window)
	Key        string             `json:"key"`
	Name       string             `json:"name"`
	Stage      string             `json:"stage"` // queued, download, import, done, failed, cancelled
	Msg        string             `json:"msg"`
	Done       int64              `json:"done"` // of the download, in bytes
	Total      int64              `json:"total"`
	Login      string             `json:"login,omitempty"` // the download waits for a login: "booth", "gumroad" or "baidu"
	Projects   []ProjectRun       `json:"projects"`
	Note       library.UpdateNote `json:"note"`
	Folder     string             `json:"folder,omitempty"`   // where the new files are
	Packages   []string           `json:"packages,omitempty"` // the new files that are imported
	Recycle    bool               `json:"recycle,omitempty"`  // the player lets the earlier version go to the Recycle Bin
	Recycled   []library.OldCopy  `json:"recycled,omitempty"`
	RecycleErr string             `json:"recycleErr,omitempty"`
	Err        string             `json:"err,omitempty"`
	Started    int64              `json:"started"`
	Ended      int64              `json:"ended,omitempty"`

	retry  bool // the download is done already: only the projects that failed
	ctx    context.Context
	cancel context.CancelFunc
}

var (
	mu      sync.Mutex
	jobs    []*Job
	running bool
	lastID  int64

	// what the work is made of (tests put their own in)
	queueBooth  = purchases.QueueDownloads
	boothJobs   = purchases.DLSnapshot
	cancelBooth = purchases.CancelDownloads
	queuePan    = pandl.QueuePanDownload
	panJobs     = pandl.PanJobsSnapshot
	cancelPan   = pandl.CancelPanDownloads
	importInto  = unity.ImportAndWait
	recycle     = core.RecycleOnly
	poll        = 400 * time.Millisecond
)

const keep = 20 // finished jobs the window still lists

func over(stage string) bool { return stage == "done" || stage == "failed" || stage == "cancelled" }

// Snapshot: every job the window may show, oldest first.
func Snapshot() []Job {
	mu.Lock()
	defer mu.Unlock()
	out := make([]Job, 0, len(jobs))
	for _, j := range jobs {
		c := *j
		c.Projects = append([]ProjectRun(nil), j.Projects...)
		out = append(out, c)
	}
	return out
}

// Active: is anything queued or under way?
func Active() bool {
	mu.Lock()
	defer mu.Unlock()
	for _, j := range jobs {
		if !over(j.Stage) {
			return true
		}
	}
	return false
}

// set changes a job; the window reloads when its stage or a project's status changes, and reads the rest
// (progress, messages) when it asks.
func set(j *Job, f func(j *Job)) {
	mu.Lock()
	was := sig(j)
	f(j)
	changed := sig(j) != was
	mu.Unlock()
	if changed {
		core.BumpRev()
	}
}

func sig(j *Job) string {
	s := j.Stage + "|" + j.Login
	for _, p := range j.Projects {
		s += "|" + p.Status
	}
	return s
}

// Start queues an update for an asset: download what is newer, then import it into each project given
// (none: only the download). recycleOld: once all of it went well, the earlier version's folder goes to the
// Recycle Bin.
func Start(st *core.Store, key string, projects []string, recycleOld bool) (*Job, error) {
	n := library.UpdateNotes(st)[key]
	if n == nil {
		return nil, errors.New("该素材当前没有可下载的更新")
	}
	if !n.CanDL {
		return nil, errors.New("该素材的网盘分享是手动关联的，无法自动下载，请打开网盘分享后手动下载")
	}
	runs, err := projectRuns(st, projects)
	if err != nil {
		return nil, err
	}
	name := key
	st.Mu.RLock()
	for _, a := range st.Assets {
		if a.Key == key {
			name = a.Name
		}
	}
	st.Mu.RUnlock()
	return enqueue(st, &Job{Key: key, Name: name, Note: *n, Projects: runs, Recycle: recycleOld && len(n.Old) > 0})
}

// Retry imports the files of a finished job once more into the projects that did not get them.
func Retry(st *core.Store, id int64) (*Job, error) {
	mu.Lock()
	var old *Job
	for _, j := range jobs {
		if j.ID == id && over(j.Stage) {
			old = j
		}
	}
	if old == nil || len(old.Packages) == 0 {
		mu.Unlock()
		return nil, errors.New("没有可重试的更新")
	}
	nj := &Job{Key: old.Key, Name: old.Name, Note: old.Note, Folder: old.Folder, Packages: old.Packages, retry: true}
	for _, p := range old.Projects {
		if p.Status != "done" {
			nj.Projects = append(nj.Projects, ProjectRun{Path: p.Path, Name: p.Name, Status: "waiting"})
		}
	}
	mu.Unlock()
	if len(nj.Projects) == 0 {
		return nil, errors.New("所有工程均已更新")
	}
	return enqueue(st, nj)
}

func projectRuns(st *core.Store, projects []string) ([]ProjectRun, error) {
	var runs []ProjectRun
	seen := map[string]bool{}
	for _, p := range projects {
		p = filepath.Clean(strings.Trim(strings.TrimSpace(p), `"`))
		if seen[core.PathKey(p)] {
			continue
		}
		seen[core.PathKey(p)] = true
		known, ok := unity.KnownProject(st, p)
		if !ok {
			return nil, errors.New("该工程不在工程列表中：" + p)
		}
		if !core.IsUnityProject(known) {
			return nil, errors.New("该文件夹不是 Unity 工程（未找到 Assets 和 ProjectSettings 文件夹）：" + known)
		}
		runs = append(runs, ProjectRun{Path: known, Name: filepath.Base(known), Status: "waiting"})
	}
	return runs, nil
}

func enqueue(st *core.Store, j *Job) (*Job, error) {
	mu.Lock()
	for _, o := range jobs {
		if o.Key == j.Key && !over(o.Stage) {
			mu.Unlock()
			return nil, errors.New("该素材的更新已在进行中")
		}
	}
	// the earlier results for this asset give way; over the limit the oldest finished ones go
	kept := jobs[:0]
	for _, o := range jobs {
		if o.Key != j.Key {
			kept = append(kept, o)
		}
	}
	jobs = kept
	for i := 0; i < len(jobs) && len(jobs) >= keep; {
		if over(jobs[i].Stage) {
			jobs = append(jobs[:i], jobs[i+1:]...)
			continue
		}
		i++
	}
	j.ID = time.Now().UnixNano()
	if j.ID <= lastID {
		j.ID = lastID + 1
	}
	lastID = j.ID
	j.Stage, j.Msg, j.Started = "queued", "排队中", time.Now().Unix()
	j.ctx, j.cancel = context.WithCancel(context.Background())
	jobs = append(jobs, j)
	start := !running
	running = true
	c := *j
	mu.Unlock()
	core.BumpRev()
	if start {
		go worker(st)
	}
	return &c, nil
}

// Cancel stops a job: one that waits never starts; one that downloads stops waiting for its files (and
// stops the download when nothing else is being downloaded); the project being written is finished, the
// ones after it are left alone.
func Cancel(id int64) {
	mu.Lock()
	for _, j := range jobs {
		if j.ID == id && !over(j.Stage) {
			j.cancel()
			if j.Stage == "queued" {
				j.Stage, j.Msg, j.Ended = "cancelled", "已取消", time.Now().Unix()
				for i := range j.Projects {
					j.Projects[i].Status = "cancelled"
				}
			}
		}
	}
	mu.Unlock()
	core.BumpRev()
}

// Dismiss: the player closed a finished job's result.
func Dismiss(id int64) {
	mu.Lock()
	kept := jobs[:0]
	for _, j := range jobs {
		if j.ID != id || !over(j.Stage) {
			kept = append(kept, j)
		}
	}
	jobs = kept
	mu.Unlock()
	core.BumpRev()
}

func next() *Job {
	mu.Lock()
	defer mu.Unlock()
	for _, j := range jobs {
		if j.Stage == "queued" {
			j.Stage = "download"
			if j.retry {
				j.Stage = "import"
			}
			return j
		}
	}
	running = false
	return nil
}

func worker(st *core.Store) {
	for {
		j := next()
		if j == nil {
			return
		}
		core.BumpRev()
		func() {
			defer func() { // a job that goes wrong is over, and the ones behind it still run
				if r := recover(); r != nil {
					core.Logf("更新任务出错 %s: %v", j.Key, r)
					set(j, func(j *Job) {
						j.Stage, j.Err, j.Msg, j.Ended = "failed", "更新出错，详见 library.log", "更新出错，详见 library.log", time.Now().Unix()
					})
				}
			}()
			run(st, j)
		}()
	}
}

func run(st *core.Store, j *Job) {
	end := func(stage, msg, errText string) {
		set(j, func(j *Job) {
			j.Stage, j.Msg, j.Err, j.Ended, j.Login = stage, msg, errText, time.Now().Unix(), ""
			for i := range j.Projects {
				if s := j.Projects[i].Status; s == "waiting" || s == "running" || s == "choose" {
					j.Projects[i].Status = "cancelled"
					if stage == "failed" {
						j.Projects[i].Status, j.Projects[i].Msg = "skipped", "下载未完成，未导入"
					}
				}
			}
		})
	}
	if !j.retry {
		before := payloads(watchDirs(st, j))
		places, err := download(st, j)
		if j.ctx.Err() != nil || (err != nil && err.Error() == "已取消") { // (the player may also stop the download itself)
			end("cancelled", "已取消", "")
			return
		}
		if err != nil {
			core.Logf("更新下载失败 %s: %v", j.Key, err)
			end("failed", err.Error(), err.Error())
			return
		}
		fresh := unpackReplaced(st, freshPayloads(before, places))
		folder := ""
		if len(places) > 0 {
			folder = places[0]
			if fi, err := os.Stat(folder); err == nil && !fi.IsDir() {
				folder = filepath.Dir(folder)
			}
		}
		set(j, func(j *Job) { j.Packages, j.Folder = fresh, folder })
		// the asset on disk has the new files from here on, whatever happens to the projects
		library.AckUpdate(st, j.Key, nil)
	}
	set(j, func(j *Job) { j.Stage, j.Msg, j.Done, j.Total = "import", "正在更新工程", 0, 0 })
	failed := 0
	for i := range j.Projects {
		if j.ctx.Err() != nil {
			break
		}
		p := j.Projects[i]
		if len(j.Packages) == 0 {
			set(j, func(j *Job) {
				j.Projects[i].Status, j.Projects[i].Msg = "skipped", "新版本中未发现可导入的 unitypackage"
			})
			continue
		}
		set(j, func(j *Job) { j.Projects[i].Status, j.Msg = "running", "正在更新工程 "+p.Name })
		res, err := importInto(j.ctx, st, unity.ImportReq{Key: j.Key, Paths: j.Packages, Project: p.Path}, func(ij unity.ImportJob) {
			set(j, func(j *Job) {
				j.Projects[i].Msg = ij.Msg
				if ij.Stage == "choose" {
					j.Projects[i].Status = "choose"
				} else if j.Projects[i].Status == "choose" {
					j.Projects[i].Status = "running"
				}
			})
		})
		switch {
		case err != nil && j.ctx.Err() != nil:
			set(j, func(j *Job) { j.Projects[i].Status, j.Projects[i].Msg = "cancelled", "已取消" })
		case err != nil:
			failed++
			set(j, func(j *Job) { j.Projects[i].Status, j.Projects[i].Msg = "failed", err.Error() })
		case res.Stage != "done":
			msg := res.Err
			if msg == "" {
				msg = res.Msg
			}
			if j.ctx.Err() != nil || msg == "已取消" {
				set(j, func(j *Job) { j.Projects[i].Status, j.Projects[i].Msg = "cancelled", "已取消" })
			} else {
				failed++
				set(j, func(j *Job) { j.Projects[i].Status, j.Projects[i].Msg = "failed", msg })
			}
			unity.DismissImport()
		default:
			set(j, func(j *Job) {
				r := &j.Projects[i]
				r.Status, r.Msg, r.Pkgs, r.Files, r.Kept, r.KeptPkgs = "done", "", len(res.Imported), res.Files, res.Kept, res.KeptPkgs
			})
			unity.DismissImport() // the result is in this job's summary
		}
	}
	if j.ctx.Err() != nil {
		end("cancelled", "已取消", "")
		return
	}
	if j.Recycle && failed == 0 {
		done, err := recycleOld(st, j)
		set(j, func(j *Job) {
			j.Recycled = done
			if err != nil {
				j.RecycleErr = err.Error()
			}
		})
		if len(done) > 0 {
			library.StartPipeline(st, true, true, false, false, nil)
		}
	}
	end("done", "更新完成", "")
}

// download has the new files fetched and waits for them. It returns where they are.
func download(st *core.Store, j *Job) ([]string, error) {
	n := j.Note
	asked := func() { set(j, func(j *Job) { j.Msg = "正在下载新版本" }) } // (said once the download is in its queue)
	if n.Source == "pan" {
		if err := queuePan(st, n.PanKey, n.Paths, nil); err != nil && !strings.Contains(err.Error(), "已在下载队列中") {
			return nil, err
		}
		asked()
		for {
			var mine *pandl.PanJob
			others := false
			for _, pj := range panJobs() {
				pj := pj
				if pj.Key == n.PanKey {
					mine = &pj
				} else if pj.Stage != "done" && pj.Stage != "failed" && pj.Stage != "login" {
					others = true
				}
			}
			switch {
			case mine == nil:
				return nil, errors.New("网盘下载已被取消")
			case mine.Stage == "done":
				if mine.Dir == "" {
					return nil, nil
				}
				return []string{mine.Dir}, nil
			case mine.Stage == "failed":
				return nil, errors.New(firstOf(mine.Err, mine.Msg, "网盘下载失败"))
			}
			set(j, func(j *Job) {
				j.Done, j.Total, j.Login = mine.Done, mine.Total, ""
				j.Msg = firstOf(mine.Msg, "正在下载新版本")
				if mine.Stage == "download" {
					j.Msg = "正在下载 " + mine.File
				}
				if mine.Stage == "login" {
					j.Login, j.Msg = "baidu", "需要登录百度网盘，登录后自动继续"
				}
			})
			if !wait(j.ctx) {
				if !others {
					cancelPan()
				}
				return nil, j.ctx.Err()
			}
		}
	}
	if _, err := queueBooth(st, n.Item, n.DLs); err != nil {
		return nil, err
	}
	asked()
	for {
		var places []string
		var done, total int64
		left, login, others := 0, false, false
		errText := ""
		found := map[string]bool{}
		for _, d := range boothJobs() {
			if !core.ContainsStr(n.DLs, d.ID) {
				others = others || d.Status == "queued" || d.Status == "running" || d.Status == "unpacking"
				continue
			}
			found[d.ID] = true
			done, total = done+d.Done, total+d.Total
			switch d.Status {
			case "done":
				if d.Path != "" && !core.ContainsStr(places, d.Path) {
					places = append(places, d.Path)
				}
			case "failed":
				errText = firstOf(errText, d.Err, "下载失败")
			case "login":
				login, left = true, left+1
			default:
				left++
			}
		}
		if errText != "" {
			return nil, errors.New(errText)
		}
		if len(found) < len(n.DLs) {
			return nil, errors.New("下载已被取消")
		}
		if left == 0 {
			return places, nil
		}
		set(j, func(j *Job) {
			j.Done, j.Total, j.Login, j.Msg = done, total, "", "正在下载新版本"
			if login {
				j.Login, j.Msg = "booth", "需要登录 Booth，登录后自动继续"
				if n.Gumroad {
					j.Login, j.Msg = "gumroad", "需要登录 Gumroad，登录后请重新开始更新"
				}
			}
		})
		if !wait(j.ctx) {
			if !others {
				cancelBooth()
			}
			return nil, j.ctx.Err()
		}
	}
}

func firstOf(list ...string) string {
	for _, s := range list {
		if s != "" {
			return s
		}
	}
	return ""
}

// wait: one tick; false when the job was cancelled meanwhile.
func wait(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(poll):
		return true
	}
}

// watchDirs: where the new files may turn up — the asset's own places, the folder a netdisk card was
// downloaded into, and the purchase's folder in the download folder.
func watchDirs(st *core.Store, j *Job) []string {
	var dirs []string
	st.Mu.RLock()
	for _, a := range st.Assets {
		if a.Key == j.Key {
			for _, l := range a.Locations {
				dirs = append(dirs, l.Path)
			}
		}
	}
	if u := st.User[j.Note.PanKey]; j.Note.PanKey != "" && u != nil && u.Downloaded != "" {
		dirs = append(dirs, u.Downloaded)
	}
	id, dl, name := j.Note.Item, "", ""
	if id != "" {
		dl = purchases.DownloadDir(st)
		if p := st.Purchases[id]; p != nil {
			name = core.SafeName(p.Name, 70)
		}
	}
	st.Mu.RUnlock()
	if dl != "" { // (the folder is read once the library is let go of)
		if ents, err := os.ReadDir(dl); err == nil {
			for _, e := range ents {
				if e.IsDir() && (naming.BoothIDFromName(e.Name()) == id || (name != "" && e.Name() == name)) {
					dirs = append(dirs, filepath.Join(dl, e.Name()))
				}
			}
		}
	}
	return dirs
}

type fileMark struct {
	size  int64
	mtime int64
}

// payloads: the unitypackages and archives under the given places, by path.
func payloads(places []string) map[string]fileMark {
	out := map[string]fileMark{}
	for _, p := range places {
		_ = filepath.WalkDir(p, func(fp string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if strings.HasSuffix(d.Name(), ".extracting") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.EqualFold(filepath.Ext(fp), ".unitypackage") && !archive.IsArchiveFile(fp) {
				return nil
			}
			if fi, err := d.Info(); err == nil {
				out[core.PathKey(fp)] = fileMark{fi.Size(), fi.ModTime().UnixNano()}
			}
			return nil
		})
	}
	return out
}

// freshPayloads: the unitypackages and archives under places that were not there before the download, or
// are not the files they were.
func freshPayloads(before map[string]fileMark, places []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range places {
		_ = filepath.WalkDir(p, func(fp string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if strings.HasSuffix(d.Name(), ".extracting") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.EqualFold(filepath.Ext(fp), ".unitypackage") && !archive.IsArchiveFile(fp) {
				return nil
			}
			k := core.PathKey(fp)
			fi, err := d.Info()
			if err != nil || seen[k] {
				return nil
			}
			if old, ok := before[k]; !ok || old.size != fi.Size() || old.mtime != fi.ModTime().UnixNano() {
				seen[k] = true
				out = append(out, fp)
			}
			return nil
		})
	}
	sort.Strings(out)
	return out
}

// unpackReplaced: among the new files, an archive that came under the name of an earlier one has its folder
// there already, so the download left it packed. It is unpacked into a folder of its own, next to the earlier
// one (which stays), and its packages take its place in the list.
func unpackReplaced(st *core.Store, fresh []string) []string {
	var arcs, out []string
	for _, p := range fresh {
		if archive.IsArchiveFile(p) {
			arcs = append(arcs, p)
		} else {
			out = append(out, p)
		}
	}
	if len(arcs) == 0 {
		return fresh
	}
	st.Mu.RLock()
	keepZip := st.Settings.KeepZip
	st.Mu.RUnlock()
	for _, set := range archive.Sets(arcs) {
		dir, again, err := archive.UnpackAgain(set, "")
		switch {
		case !again:
			out = append(out, set.Parts...) // the import unpacks it
		case err != nil:
			core.Logf("新版本压缩包解压失败 %s: %v", set.Main, err)
			out = append(out, set.Parts...)
		default:
			core.Logf("新版本已解压到 %s（原有文件夹保留）", dir)
			if !keepZip { // as after any unpack — but to the Recycle Bin: it carries the name of a file the player had
				if err := recycle(set.Parts); err != nil {
					core.Logf("新版本压缩包未移至回收站 %s: %v", set.Main, err)
				}
			}
			out = append(out, freshPayloads(nil, []string{dir})...)
		}
	}
	sort.Strings(out)
	return out
}

// recycleOld moves the earlier version's folders to the Recycle Bin, each only when it is what the player
// was shown: still there, inside the library, not a library folder itself, not holding the new files, and
// nothing a Unity project lives in.
func recycleOld(st *core.Store, j *Job) ([]library.OldCopy, error) {
	st.Mu.RLock()
	allowed := append([]string{}, st.Settings.Roots...)
	if dl := purchases.DownloadDir(st); dl != "" {
		allowed = append(allowed, dl)
	}
	var projects []string
	for _, p := range st.Projects {
		projects = append(projects, p.Path)
	}
	projects = append(projects, st.Settings.ProjectRoots...)
	st.Mu.RUnlock()
	var done []library.OldCopy
	var firstErr error
	for _, o := range j.Note.Old {
		if err := library.CheckRemovable(o.Path, allowed, projects, append([]string{j.Folder}, j.Packages...)); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s：%v", o.Path, err)
			}
			continue
		}
		if err := recycle([]string{o.Path}); err != nil {
			core.Logf("旧版本没能移至回收站 %s: %v", o.Path, err)
			if firstErr == nil {
				firstErr = fmt.Errorf("%s：无法移至回收站（%s）", o.Path, core.TrimErr(err))
			}
			continue
		}
		core.Logf("旧版本已移至回收站：%s", o.Path)
		done = append(done, o)
	}
	return done, firstErr
}
