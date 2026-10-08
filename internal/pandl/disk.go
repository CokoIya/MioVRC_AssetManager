package pandl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"vrclib/internal/archive"
	"vrclib/internal/core"
	"vrclib/internal/library"
	"vrclib/internal/purchases"
	"vrclib/internal/webpane"
)

// The player's own netdisk: a folder (or a file) of it downloaded into the library — what the player saved
// there from a share with Baidu's own app or page, where any captcha is Baidu's to ask and the player's to
// read. Nothing is saved into the netdisk for it, and nothing in the netdisk is changed.

// diskPrefix: the job of a download from the player's netdisk is "pandisk:" and the netdisk path.
const diskPrefix = "pandisk:"

// IsDiskKey: a job that downloads from the player's own netdisk.
func IsDiskKey(key string) bool { return strings.HasPrefix(key, diskPrefix) }

// DiskEntry: a folder or a file of the player's netdisk, as the window lists it.
type DiskEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Dir   bool   `json:"dir"`
	Size  int64  `json:"size"`
	Mtime int64  `json:"mtime"`
}

// diskPath: a netdisk path as the program uses it ("/a/b"); "/" is the netdisk itself.
func diskPath(p string) string {
	return path.Clean("/" + strings.TrimSpace(strings.ReplaceAll(p, `\`, "/")))
}

// ListDisk: what is in a folder of the player's netdisk, folders first. ErrBaiduLogin: not logged in (any more).
func ListDisk(st *core.Store, dir string) ([]DiskEntry, string, error) {
	dir = diskPath(dir)
	s := LoadBaiduSession()
	if s == nil {
		return nil, dir, ErrBaiduLogin
	}
	b := NewBDClient(st, s)
	defer b.c.CloseIdleConnections()
	ents, there, err := b.listDir(dir)
	if errors.Is(err, ErrBaiduLogin) {
		ForgetBaiduSession() // Baidu turned the saved login away: the login page asks again
	}
	if err != nil {
		return nil, dir, err
	}
	if !there {
		return nil, dir, errors.New("网盘中没有这个文件夹（可能已被移动或删除）")
	}
	out := make([]DiskEntry, 0, len(ents))
	for _, e := range ents {
		sz, _ := e.Size.Int64()
		mt, _ := e.Mtime.Int64()
		out = append(out, DiskEntry{Name: e.Name, Path: e.Path, Dir: e.IsDir.String() == "1", Size: sz, Mtime: mt})
	}
	sort.SliceStable(out, func(i, k int) bool {
		if out[i].Dir != out[k].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[k].Name)
	})
	return out, dir, nil
}

// QueueDiskDownload downloads a folder or a file of the player's netdisk into the library.
func QueueDiskDownload(st *core.Store, remote string) error {
	remote = diskPath(remote)
	if remote == "/" {
		return errors.New("请选择要下载的文件夹或文件")
	}
	return enqueuePan(st, &PanJob{ID: time.Now().UnixNano(), Key: diskPrefix + remote, Title: path.Base(remote), Stage: "queued", Msg: "排队中"})
}

// runDiskJob: the download of a folder or a file of the player's netdisk, into a folder of its name in the
// download folder (one that stopped goes on in the folder it had), unpacked and taken into the library as a
// share's download is — a collection of products (合集包) is split into a card for each.
func runDiskJob(ctx context.Context, st *core.Store, j *PanJob) error {
	s := LoadBaiduSession()
	if webpane.PaneMode() != "" {
		webpane.Pane.Mu.Lock()
		running := webpane.Pane.Port > 0
		webpane.Pane.Mu.Unlock()
		if running { // the built-in page keeps its login fresh
			if fresh, err := captureBaiduLogin(st); err == nil && fresh != nil {
				s = fresh
			}
		}
	}
	if s == nil {
		return ErrBaiduLogin
	}
	b := NewBDClient(st, s)
	b.ctx = ctx
	defer b.c.CloseIdleConnections()
	msg := func(m string) {
		setPan(j, func(j *PanJob) { j.Msg = m })
		TaskPanDL.Set(0, 0, j.Title+"："+m)
	}
	msg("正在检查百度网盘登录状态")
	if _, _, err := b.Whoami(); err != nil {
		return err
	}
	remote := diskPath(strings.TrimPrefix(j.Key, diskPrefix))
	st.Mu.RLock()
	extract, keep := !st.Settings.NoExtract, st.Settings.KeepZip
	dlRoot := purchases.DownloadDir(st)
	prev := ""
	if u := st.User[j.Key]; u != nil {
		prev = u.DownloadDir // a download that stopped goes on where it was
	}
	st.Mu.RUnlock()

	// 1. what is there: the folder's files, or the file
	msg("正在获取网盘文件列表")
	ents, there, err := b.listDir(path.Dir(remote))
	if err != nil {
		return err
	}
	var me *bdEntry
	for k := range ents {
		if ents[k].Path == remote || (ents[k].Path == "" && ents[k].Name == path.Base(remote)) {
			me = &ents[k]
			break
		}
	}
	if !there || me == nil {
		return errors.New("网盘中未找到「" + path.Base(remote) + "」（可能已被移动或删除）")
	}
	name := path.Base(remote)
	var files []bdFile
	if me.IsDir.String() == "1" {
		if files, err = b.tree(remote); err != nil {
			return err
		}
	} else {
		sz, _ := me.Size.Int64()
		files = []bdFile{{Path: remote, Rel: me.Name, Size: sz}}
		name = core.StripArchiveExt(name) // a file comes into a folder of its own name
	}
	if len(files) == 0 {
		return errors.New("网盘中的文件夹为空")
	}
	local := prev
	if local == "" || !core.IsDir(local) {
		local = archive.UniquePath(filepath.Join(dlRoot, core.SafeName(name, 80)))
	}
	localOf := func(rel string) string {
		segs := strings.Split(rel, "/")
		for k := range segs {
			segs[k] = core.SafeName(segs[k], 200)
		}
		return filepath.Join(append([]string{local}, segs...)...)
	}
	var total, need int64
	for _, f := range files {
		total += f.Size
		if fi, err := os.Stat(localOf(f.Rel)); err != nil || fi.Size() != f.Size {
			need += f.Size
		}
	}
	setPan(j, func(j *PanJob) {
		j.Stage, j.Dir, j.Total, j.Done, j.FileN, j.Files = "download", local, total, 0, len(files), 0
	})
	// the folder is remembered, so that a retry goes on in it
	st.Mu.Lock()
	u := st.User[j.Key]
	if u == nil {
		u = &core.UserData{}
		st.User[j.Key] = u
	}
	u.DownloadDir = local
	st.Mu.Unlock()
	_ = st.Save()
	if err := os.MkdirAll(local, 0755); err != nil {
		return purchases.WriteErr(local, err)
	}
	if err := purchases.CheckSpace(local, need); err != nil {
		return err
	}
	purchases.PinDownloadDir(st, dlRoot)

	// 2. download, 3. unpack
	if err := b.fetchFiles(j, files, localOf, total, s.VIP != 2); err != nil {
		return err
	}
	var failed, unpacked, from []string
	if extract {
		failed, unpacked, from = unpackDownloaded(j, files, localOf, keep)
	}
	bundle := library.MarkBundles(st, local, library.Unpacked(unpacked, from))

	// 4. into the library
	st.Mu.Lock()
	if u := st.User[j.Key]; u != nil {
		u.Downloaded, u.DownloadDir = local, ""
	}
	keepInRootsLocked(st, local)
	auto := st.Settings.AutoBooth
	st.Mu.Unlock()
	_ = st.Save()
	note := fmt.Sprintf("已下载 %d 个文件（%s）", len(files), fmtBytes(total))
	if len(failed) > 0 {
		note += "；部分压缩包解压失败"
	}
	said, _ := bundleEnd(bundle, false)
	note += said
	setPan(j, func(j *PanJob) { j.Stage, j.Msg, j.Failed, j.File = "done", note, failed, "" })
	core.Logf("网盘文件夹下载完成 %s → %s", remote, local)
	library.StartPipeline(st, true, true, auto, false, nil)
	return nil
}
