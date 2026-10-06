package pandl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"vrclib/internal/archive"
	"vrclib/internal/cloudshare"
	"vrclib/internal/core"
	"vrclib/internal/library"
	"vrclib/internal/naming"
	"vrclib/internal/netdisk"
	"vrclib/internal/purchases"
	"vrclib/internal/unity"
)

// A Google Drive / Dropbox card goes through the same queue and job list as a Baidu share's, with two of
// its stages: no sign-in, no copy into a netdisk — the files come straight from the service — and then the
// unpacking and the library's intake, the same as a Baidu share's.

// cloudFile: one file of the card to fetch.
type cloudFile struct {
	tree string        // where the card's file list shows it ("/folder/file.zip")
	rel  string        // its path under the asset's folder
	dst  string        // where it goes on this disk
	node *core.PanFile // the listing's entry (its size when known, its id on Drive)
}

// cloudPause: between tries (tests shorten it).
var cloudPause = 3 * time.Second

// cloudOpen: how a file is asked for (tests put their own here).
var cloudOpen = func(ctx context.Context, c *http.Client, src cloudshare.Source, from int64, ifRange string) (*http.Response, error) {
	return src.Open(ctx, c, from, ifRange)
}

func runCloudJob(ctx context.Context, st *core.Store, j *PanJob) error {
	surl, sub := netdisk.SplitPanKey(j.Key)
	st.Mu.RLock()
	link := ""
	if u := st.User[surl]; u != nil {
		link = u.ShareURL
	}
	extract, keep := !st.Settings.NoExtract, st.Settings.KeepZip
	dlRoot := purchases.DownloadDir(st)
	prev, got := "", []string(nil)
	if u := st.User[j.Key]; u != nil {
		prev = u.DownloadDir // a download that stopped goes on where it was
		if prev == "" {
			prev = u.Downloaded
		}
		got = u.PanGot
	}
	listing := st.Pan[surl]
	st.Mu.RUnlock()
	share := cloudshare.Parse(link)
	if share == nil {
		return errors.New("该素材没有 Google Drive 或 Dropbox 分享链接")
	}
	service := cloudshare.Label(share.Service)
	msg := func(m string) {
		setPan(j, func(j *PanJob) { j.Msg = m })
		TaskPanDL.Set(0, 0, j.Title+"："+m)
	}
	setPan(j, func(j *PanJob) { j.Stage = "fetch" })
	msg("正在读取 " + service + " 分享")
	// the share's listing: the one read before, or read now (a card added a moment ago, or whose read failed)
	read := func() error {
		fresh, err := cloudshare.FetchListingCtx(ctx, st, link) // cancel ends a long walk through its folders too
		if err != nil {
			return err
		}
		st.Mu.Lock()
		netdisk.DiffPan(st.Pan[surl], fresh)
		st.Pan[surl] = fresh
		st.Mu.Unlock()
		_ = st.Save()
		core.BumpRev()
		return nil
	}
	if listing == nil || len(listing.Files) == 0 {
		if err := read(); err != nil {
			return err
		}
	}
	st.Mu.RLock()
	part, item := netdisk.PanSub(st, j.Key)
	st.Mu.RUnlock()
	if part == nil || (sub != "" && item == nil) {
		return errors.New("分享中未找到该素材（分享内容可能已变更，请刷新后重试）")
	}
	if len(part.Files) == 0 {
		return errors.New("分享内容为空")
	}
	if j.Title == netdisk.SharePlaceholder(surl) { // added a moment ago, before its listing was read
		t := strings.TrimSpace(part.Title)
		if len(part.Files) == 1 && !part.Files[0].Dir {
			t = core.StripArchiveExt(part.Files[0].Name)
		}
		if t = naming.CleanName(t); t != "" {
			setPan(j, func(j *PanJob) { j.Title = t })
		}
	}
	// what to fetch: all of the card, or the parts picked in its file list; parts of a picked folder that were
	// downloaded before (their archives unpacked and gone) are left out, picking a downloaded part itself fetches
	// it again
	oneDir := len(part.Files) == 1 && part.Files[0].Dir
	whole := len(j.Paths) == 0
	strip := ""
	if oneDir {
		strip = part.Files[0].Name + "/" // one folder: its contents go straight into the asset's folder
	}
	var files []cloudFile
	skipped := 0
	collect := func(fs []*core.PanFile, tree string, pick string) {
		var walk func(fs []*core.PanFile, tree string)
		walk = func(fs []*core.PanFile, tree string) {
			for _, f := range fs {
				t := tree + "/" + f.Name
				if f.Dir {
					walk(f.Children, t)
					continue
				}
				if pick != "" && pick != t && panCovered(t, got) && !panCovered(pick, got) {
					skipped++
					continue
				}
				files = append(files, cloudFile{tree: t, rel: strings.TrimPrefix(t, "/"), node: f})
			}
		}
		walk(fs, tree)
	}
	// what counts as downloaded afterwards: all of it ("/") or the picked parts — unless the listing stops short
	// of what the share holds (too deep, too many entries): then only what was listed in full, and the note says so
	var trees []string
	short := false
	if whole {
		collect(part.Files, "", "")
		var all bool
		trees, all = cloudListed("", part.Files, part.Truncated)
		short = !all
	} else {
		for _, p := range j.Paths {
			node, parent := cloudNode(part.Files, p)
			if node == nil {
				return errors.New("分享中未找到「" + path.Base(p) + "」（分享内容可能已变更，请刷新后重试）")
			}
			if node.Dir {
				collect(node.Children, p, p)
				listed, all := cloudListed(p, node.Children, node.Partial)
				trees, short = append(trees, listed...), short || !all
			} else {
				collect([]*core.PanFile{node}, parent, p)
				trees = append(trees, p)
			}
		}
	}
	if len(files) == 0 && skipped == 0 {
		return errors.New("分享中没有可下载的文件")
	}
	// where it goes on this disk: the folder of an earlier download, else a new one
	name := core.SafeName(naming.CleanName(j.Title), 60)
	if name == "" || name == "_" {
		name = core.SafeName(service+" "+share.ID, 60)
	}
	local := prev
	if local == "" || !core.IsDir(local) {
		local, got = archive.UniquePath(filepath.Join(dlRoot, name)), nil
	}
	localOf := func(rel string) string {
		segs := strings.Split(strings.TrimPrefix(rel, strip), "/")
		for k := range segs {
			segs[k] = core.SafeName(segs[k], 200)
		}
		return filepath.Join(append([]string{local}, segs...)...)
	}
	// two entries that would land on one path (Drive allows equal names in a folder; names that differ only in
	// case, or only in what a file name cannot hold): the later ones get " (2)", " (3)" … before the extension
	taken := map[string]bool{}
	for i := range files {
		dst := localOf(files[i].rel)
		ext := filepath.Ext(dst)
		for n, base := 2, strings.TrimSuffix(dst, ext); taken[strings.ToLower(dst)]; n++ {
			dst = fmt.Sprintf("%s (%d)%s", base, n, ext)
		}
		taken[strings.ToLower(dst)] = true
		files[i].dst = dst
	}
	var total, need int64
	unknown := 0
	for _, f := range files {
		total += f.node.Size
		if f.node.Size == 0 {
			unknown++
		}
		if fi, err := os.Stat(f.dst); err != nil || fi.Size() != f.node.Size {
			need += f.node.Size
		}
	}
	// a listing without sizes (the share pages): the total grows as the files come, so the bar goes by the
	// number of files, the one being fetched counted by its own bytes
	byCount := unknown > 0
	setPan(j, func(j *PanJob) {
		j.Stage, j.Dir, j.Total, j.Done, j.FileN, j.Files, j.ByCount, j.Pct = "download", local, total, 0, len(files), 0, byCount, 0
	})
	// what is in the folder already counts as done; remember the folder so a retry continues there
	st.Mu.Lock()
	u := st.User[j.Key]
	if u == nil {
		u = &core.UserData{}
		st.User[j.Key] = u
	}
	if u.Downloaded != local { // more of it into the folder downloaded before: that folder's card stays
		u.Downloaded = ""
	}
	if local != prev {
		u.PanGot = nil // a new folder: nothing downloaded into it yet
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
	c := cloudshare.Client(st)
	c.Timeout = 0 // big files: no limit on the whole; the guard gives up a connection that went silent
	defer c.CloseIdleConnections()
	var done int64
	winStart, winBytes := time.Now(), int64(0)
	lastShow := time.Time{}
	// renew: the entries of the files from `from` on, as the listing read a moment ago has them at their places;
	// did the first of them get another id?
	renew := func(from int) bool {
		st.Mu.RLock()
		fresh, _ := netdisk.PanSub(st, j.Key)
		st.Mu.RUnlock()
		if fresh == nil {
			return false
		}
		now := map[string]*core.PanFile{}
		var walk func(fs []*core.PanFile, tree string)
		walk = func(fs []*core.PanFile, tree string) {
			for _, f := range fs {
				if t := tree + "/" + f.Name; f.Dir {
					walk(f.Children, t)
				} else if now[t] == nil {
					now[t] = f
				}
			}
		}
		walk(fresh.Files, "")
		changed := false
		for k := from; k < len(files); k++ {
			if n := now[files[k].tree]; n != nil && n.Ref != files[k].node.Ref {
				files[k].node = n
				changed = changed || k == from
			}
		}
		return changed
	}
	reread := false
	for i := range files {
		f := &files[i]
		setPan(j, func(j *PanJob) { j.File, j.Files = path.Base(f.rel), i })
		base := done // the file's bytes are counted by where it stands: a try that starts over counts nothing twice
		fetch := func() error {
			at, sized := int64(0), f.node.Size > 0
			return fetchCloudFile(ctx, c, cloudshare.SourceFor(share, f.node), f.node.Size, f.dst, func(pos int64, size int64) {
				if pos > at {
					winBytes += pos - at
				}
				at, done = pos, base+pos
				if size > 0 && !sized { // the listing did not know the size: the server does
					sized, total = true, total+size
				}
				now := time.Now()
				if now.Sub(winStart) >= 2*time.Second {
					speed := int64(float64(winBytes) / now.Sub(winStart).Seconds())
					winStart, winBytes = now, 0
					setPan(j, func(j *PanJob) { j.Speed, j.Total = speed, total })
				}
				if now.Sub(lastShow) > 500*time.Millisecond {
					lastShow = now
					pct := 0
					if byCount {
						n := float64(i) // the files that are there, and how much of this one
						if size > 0 {
							n += min(float64(pos)/float64(size), 1)
						}
						pct = int(n / float64(len(files)) * 100)
					}
					setPan(j, func(j *PanJob) { j.Done, j.Total, j.Pct = done, total, pct })
					switch {
					case byCount:
						TaskPanDL.Set(pct, 100, fmt.Sprintf("%s  %d / %d  %s", j.Title, i+1, len(files), fmtBytes(done)))
					case total > 0:
						TaskPanDL.Set(int(done>>10), int(total>>10), fmt.Sprintf("%s  %s / %s", j.Title, fmtBytes(done), fmtBytes(total)))
					}
				}
			})
		}
		err := fetch()
		if cloudshare.Gone(err) && !reread && f.node.Ref != "" && f.node.Ref != share.ID {
			// "gone" for a file of a folder: its id is the one the listing had when it was read, up to a day
			// ago, and a file the seller took away and put there again has another. The share is read once
			// more and the file asked for by its place in it; when it is not there either, it is gone.
			reread = true
			if read() == nil && renew(i) {
				done = base
				err = fetch()
			}
		}
		if err != nil {
			return err
		}
	}
	if total < done {
		total = done
	}
	setPan(j, func(j *PanJob) { j.Done, j.Total, j.Files, j.Speed = total, total, len(files), 0 })
	// unpack (archives inside archives too), then into the library: the folder replaces the card — as a Baidu
	// share's download ends (runPanJob)
	var failed, unpacked, from []string
	if extract {
		setPan(j, func(j *PanJob) { j.Stage, j.Msg = "unpack", "正在解压" })
		var remove func([]string) error
		if !keep {
			remove = archive.RemoveFiles
		}
		// only the files this job downloaded (those of an earlier try that stopped are among them): the folder
		// may be the asset's own, with archives the player keeps packed
		var mine []string
		for _, f := range files {
			mine = append(mine, localOf(f.rel))
		}
		res := archive.UnpackFiles(mine, "", remove, func(n string, i, k int) {
			setPan(j, func(j *PanJob) { j.Msg = "正在解压 " + n })
			TaskPanDL.Set(i, k, j.Title+"：正在解压 "+n)
		})
		for a, e := range res.Failed {
			failed = append(failed, filepath.Base(a)+"："+e)
		}
		unpacked, from = res.Done, res.From
	}
	bundle := library.MarkBundles(st, local, library.Unpacked(unpacked, from)) // a collection of products: a card for each
	st.Mu.Lock()
	if u := st.User[j.Key]; u != nil {
		u.Downloaded, u.DownloadDir = local, ""
		u.PanGot = mergePanParts(u.PanGot, trees)
	}
	inRoots := false
	for _, r := range st.Settings.Roots {
		if core.UnderDir(local, r) {
			inRoots = true
		}
	}
	if !inRoots {
		st.Settings.Roots = append(st.Settings.Roots, filepath.Dir(local))
	}
	auto := st.Settings.AutoBooth
	st.Mu.Unlock()
	_ = st.Save()
	note := fmt.Sprintf("已下载 %d 个文件（%s）", len(files), fmtBytes(total))
	if !whole {
		note = fmt.Sprintf("已下载所选的 %d 项，共 %d 个文件（%s）", len(j.Paths), len(files), fmtBytes(total))
	}
	if skipped > 0 {
		note += fmt.Sprintf("；已跳过此前下载过的 %d 个文件", skipped)
		if len(files) == 0 {
			note = "所选内容此前均已下载"
		}
	}
	if short {
		note += "；分享未完全列出，部分文件未下载"
	}
	if len(failed) > 0 {
		note += "；部分压缩包解压失败"
	}
	said, noImport := bundleEnd(bundle, j.imp != nil)
	note += said
	setPan(j, func(j *PanJob) { j.Stage, j.Msg, j.Failed, j.File = "done", note, failed, "" })
	core.Logf("%s 下载完成 %s → %s", service, j.Key, local)
	library.StartPipeline(st, true, true, auto, false, nil)
	if j.imp != nil && !noImport {
		req := *j.imp
		req.Key, req.Paths = j.Key, []string{local}
		if !whole { // only what was picked (an archive in it is a folder now)
			req.Paths = nil
			for _, f := range files {
				if core.StatOK(f.dst) {
					req.Paths = append(req.Paths, f.dst)
				}
			}
			req.Paths = core.UniqStrings(append(req.Paths, unpacked...))
		}
		unity.StartImportWhenFree(st, req)
	}
	return nil
}

// cloudNode: the entry at a path of the card's file list ("/a/b"), and its folder's path. A Drive name may
// hold a "/" itself, so the path is matched against the entries' own paths, not cut at its slashes.
func cloudNode(fs []*core.PanFile, p string) (*core.PanFile, string) {
	var find func(fs []*core.PanFile, tree string) (*core.PanFile, string)
	find = func(fs []*core.PanFile, tree string) (*core.PanFile, string) {
		for _, f := range fs {
			t := tree + "/" + f.Name
			if t == p {
				return f, tree
			}
			if f.Dir && strings.HasPrefix(p, t+"/") {
				if n, parent := find(f.Children, t); n != nil {
					return n, parent
				}
			}
		}
		return nil, ""
	}
	return find(fs, "")
}

// cloudListed: what of a part of the file list (the entries fs of the folder at tree; cut: that folder is
// marked as not fully listed) counts as downloaded once its files are — the part itself when all of it is
// listed (all), else the files and the fully listed folders in it.
func cloudListed(tree string, fs []*core.PanFile, cut bool) (parts []string, all bool) {
	var walk func(tree string, fs []*core.PanFile, cut bool) ([]string, bool)
	walk = func(tree string, fs []*core.PanFile, cut bool) ([]string, bool) {
		var out []string
		all := !cut
		for _, f := range fs {
			t := tree + "/" + f.Name
			if !f.Dir {
				out = append(out, t)
				continue
			}
			sub, whole := walk(t, f.Children, f.Partial)
			if all = all && whole; whole {
				sub = []string{t}
			}
			out = append(out, sub...)
		}
		return out, all
	}
	if parts, all = walk(tree, fs, cut); all {
		if tree == "" {
			tree = "/"
		}
		parts = []string{tree}
	}
	return parts, all
}

// ---------- fetching one file ----------

// cloudTag: what the server said about the file a .part belongs to, so that only the same file is continued.
// Kept while the program runs; a .part left by an earlier run is started again.
type cloudTag struct {
	total int64
	valid string // ETag, else Last-Modified: sent back as If-Range
}

var (
	cloudPartMu sync.Mutex
	cloudParts  = map[string]cloudTag{}
)

func cloudPart(part string) (cloudTag, bool) {
	cloudPartMu.Lock()
	defer cloudPartMu.Unlock()
	t, ok := cloudParts[part]
	return t, ok
}

func setCloudPart(part string, t *cloudTag) {
	cloudPartMu.Lock()
	defer cloudPartMu.Unlock()
	if t == nil {
		delete(cloudParts, part)
		return
	}
	cloudParts[part] = *t
}

func dropCloudPart(part string) {
	setCloudPart(part, nil)
	_ = os.Remove(part)
}

func partSize(p string) int64 {
	if fi, err := os.Stat(p); err == nil {
		return fi.Size()
	}
	return 0
}

// errCloudPage: the answer was a web page where the file should be. It is never saved as the file.
var errCloudPage = errors.New("服务器返回的是网页而不是文件，请稍后重试")

// cloudRenameErr: all of the file arrived; only its name could not be given to it.
type cloudRenameErr struct{ error }

func (e cloudRenameErr) Unwrap() error { return e.error }

// fetchCloudFile fetches src into dst through dst.part, calling prog with how much of the file is there as it
// arrives (and the file's size once the server says it). A try that ends early is continued from what it left
// (Range); only tries that brought nothing count against the limit. What the disk refuses, and what the
// service refuses for a reason of its own (gone, not public, throttled), are not asked for again.
func fetchCloudFile(ctx context.Context, c *http.Client, src cloudshare.Source, size int64, dst string, prog func(pos, size int64)) error {
	had := int64(-1)
	if fi, err := os.Stat(dst); err == nil {
		switch {
		case size > 0 && fi.Size() == size:
			prog(size, size)
			return nil // downloaded before
		case size == 0 && fi.Mode().IsRegular():
			// a listing without sizes (the share pages) cannot tell whether this is the file the share holds
			// now: the server's answer does, by its length (fetchCloudOnce)
			had = fi.Size()
		}
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return purchases.WriteErr(filepath.Dir(dst), err)
	}
	part := dst + ".part"
	var lastErr error
	for try, fails := 0, 0; fails < 3 && try < 30; try++ {
		if ctx.Err() != nil {
			dropCloudPart(part)
			return errPanCancelled
		}
		if fails > 0 && !purchases.Sleep(ctx, time.Duration(fails)*cloudPause) {
			dropCloudPart(part)
			return errPanCancelled
		}
		before := partSize(part)
		err := fetchCloudOnce(ctx, c, src, dst, had, prog)
		var named cloudRenameErr
		switch {
		case err == nil:
			return nil
		case ctx.Err() != nil:
			dropCloudPart(part)
			return errPanCancelled
		case errors.As(err, &named):
			// the name is held by something else just now (the old file open in another program, a virus
			// scanner): the part file is all of the download and stays — the next try only gives it its name
			return named.error
		case purchases.IsLocalErr(err):
			dropCloudPart(part) // it only takes up the room that is missing
			return err
		case cloudshare.ErrRange(err):
			dropCloudPart(part) // the file changed under the part: from the start
		case !cloudshare.Temporary(err):
			return err
		}
		lastErr = err
		if partSize(part) <= before {
			fails++
		}
	}
	return lastErr
}

// looksHTML: a web page, by how it begins.
func looksHTML(head []byte) bool {
	h := strings.ToLower(strings.TrimLeft(strings.TrimPrefix(string(head), "\xef\xbb\xbf"), " \t\r\n"))
	return strings.HasPrefix(h, "<!doctype html") || strings.HasPrefix(h, "<html")
}

// fetchCloudOnce: one try. had: the size of a file that is at dst already and may be the one asked for (-1:
// there is none, or the listing knows the size and it is another).
func fetchCloudOnce(ctx context.Context, c *http.Client, src cloudshare.Source, dst string, had int64, prog func(pos, size int64)) error {
	part := dst + ".part"
	tag, known := cloudPart(part)
	have := partSize(part)
	if !known || have <= 0 || (tag.total > 0 && have > tag.total) {
		dropCloudPart(part) // nothing to go on from, or left by an earlier run: from the start
		have, known = 0, false
	}
	finish := func(size int64) error {
		_ = os.Remove(dst)
		if err := os.Rename(part, dst); err != nil {
			tag.total = size // what is in the part file is all of it
			setCloudPart(part, &tag)
			return cloudRenameErr{purchases.WriteErr(dst, err)}
		}
		setCloudPart(part, nil)
		return nil
	}
	if known && tag.total > 0 && have == tag.total {
		if err := finish(have); err != nil { // all of it arrived last time
			return err
		}
		prog(have, have)
		return nil
	}
	fresh := have == 0 // nothing of an earlier try to go on from
	g := purchases.NewStallGuard(ctx)
	defer g.Stop()
	netErr := func(what string, err error) error {
		switch {
		case ctx.Err() != nil:
			return errPanCancelled
		case g.Stalled():
			return cloudshare.Temp(fmt.Errorf("%s（%d 秒内未收到数据）", what, int(purchases.StallAfter.Seconds())))
		}
		return cloudshare.Temp(fmt.Errorf("%s（%s）", what, purchases.NetErrText(err)))
	}
	resp, err := cloudOpen(g.Ctx, c, src, have, tag.valid)
	if err != nil {
		if ctx.Err() != nil || g.Stalled() {
			return netErr("下载失败", err)
		}
		return err // as the service's reader classed it: a passing one is tried again, the rest ends the job
	}
	defer resp.Body.Close()
	g.Kick()
	total := tag.total
	switch {
	case resp.StatusCode == http.StatusOK || have == 0:
		have, total = 0, resp.ContentLength // the whole file (again)
	case resp.StatusCode == http.StatusPartialContent:
		a, t := int64(-1), int64(-1)
		if cr := resp.Header.Get("Content-Range"); cr != "" {
			_, _ = fmt.Sscanf(strings.TrimPrefix(cr, "bytes "), "%d-", &a)
			t = cloudshare.TotalSize(resp)
		}
		if a != have || (t >= 0 && tag.total > 0 && t != tag.total) {
			dropCloudPart(part)
			return cloudshare.Temp(errors.New("下载失败（服务器返回的数据范围有误）"))
		}
		if t > 0 {
			total = t
		}
	default:
		return cloudshare.Temp(fmt.Errorf("下载失败（HTTP %d）", resp.StatusCode))
	}
	if fresh && had >= 0 && total == had {
		prog(had, had)
		return nil // downloaded before: what is there is as long as what the server would send
	}
	// a page where the file should be (the service says so by its type; this is the look at the bytes)
	var head []byte
	if have == 0 {
		head = make([]byte, 512)
		n, err := io.ReadFull(resp.Body, head)
		if head = head[:n]; err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return netErr("下载中断", err)
		}
		g.Kick()
		if ext := core.LowerExt(dst); ext != ".html" && ext != ".htm" && looksHTML(head) {
			return errCloudPage
		}
	}
	if total > 0 {
		if err := purchases.CheckSpace(filepath.Dir(dst), total-have); err != nil {
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
		tag = cloudTag{total: total, valid: valid}
		setCloudPart(part, &tag)
	}
	if err != nil {
		return purchases.WriteErr(part, err)
	}
	got := have
	write := func(b []byte) error {
		if _, err := f.Write(b); err != nil {
			f.Close()
			return purchases.WriteErr(part, err)
		}
		got += int64(len(b))
		prog(got, max(total, 0))
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
		return purchases.WriteErr(part, err)
	}
	if total >= 0 && got != total {
		if got > total {
			dropCloudPart(part)
		}
		return cloudshare.Temp(fmt.Errorf("下载不完整（%s / %s）", fmtBytes(got), fmtBytes(total)))
	}
	if total < 0 {
		prog(got, got) // a file of a size the server never said: it is what arrived
	}
	return finish(got)
}
