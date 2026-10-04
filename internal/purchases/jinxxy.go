package purchases

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"vrclib/internal/archive"
	"vrclib/internal/core"
	"vrclib/internal/library"
	"vrclib/internal/webpane"
)

// Jinxxy has no interface a buyer's program could read purchases from, and its inventory pages could not be
// looked at without an account, so there is no sync for it. What is done instead: a file the player downloads
// on a Jinxxy page inside the program is saved by the pane's browser into the download folder and taken into
// the library like any other download (a folder of its own, unpacked, scanned).

func init() {
	webpane.PaneIncomingDir = paneIncomingDir
	webpane.PaneFileSaved = adoptPaneFile
}

// paneIncomingDir: where the pane's browser saves a file while it downloads. Inside the download folder, so
// that the file only has to be renamed afterwards; the library skips folders whose name starts with a dot.
func paneIncomingDir(st *core.Store) string {
	st.Mu.RLock()
	dir := DownloadDir(st)
	st.Mu.RUnlock()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, ".incoming")
}

// moveFile renames, and copies when the two places are on different disks.
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	out, err := os.Create(dst + ".part")
	if err != nil {
		in.Close()
		return err
	}
	_, err = io.Copy(out, in)
	in.Close()
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(dst+".part", dst)
	}
	if err != nil {
		_ = os.Remove(dst + ".part")
		return err
	}
	return os.Remove(src)
}

// adoptPaneFile takes a file the pane's browser has finished downloading (src, under paneIncomingDir) into
// the library: "<download folder>/<file name without extension>/<file name>", unpacked like a Booth download,
// then the library looks at its folders again. It returns where the file (or what was in it) is.
func adoptPaneFile(st *core.Store, src, name string) (string, error) {
	fi, err := os.Stat(src)
	if err != nil || fi.IsDir() {
		return "", errors.New("未找到已下载的文件，可能已由浏览器保存到系统的「下载」文件夹")
	}
	name = core.SafeName(filepath.Base(name), 120)
	stem := core.SafeName(strings.TrimSpace(core.StripArchiveExt(name)), 70)
	if stem == "" || stem == "." {
		stem = "Jinxxy 下载"
	}
	st.Mu.RLock()
	dir := DownloadDir(st)
	extract, keep := !st.Settings.NoExtract, st.Settings.KeepZip
	st.Mu.RUnlock()
	folder := filepath.Join(dir, stem)
	if err := os.MkdirAll(folder, 0755); err != nil {
		return "", WriteErr(folder, err)
	}
	dst := archive.UniquePath(filepath.Join(folder, name)) // downloaded before: the earlier file is not overwritten
	if err := moveFile(src, dst); err != nil {
		return "", WriteErr(dst, err)
	}
	PinDownloadDir(st, dir)
	final := dst
	if extract && archive.IsArchiveFile(dst) {
		final = folder
		var remove func([]string) error
		if !keep {
			remove = archive.RemoveFiles
		}
		// only the file that was just downloaded: the folder goes by a name the site gave, and may be one the
		// player keeps things in
		res := archive.UnpackFiles([]string{dst}, "", remove, nil)
		for a, e := range res.Failed {
			core.Logf("解压失败 %s: %s", a, e)
		}
	}
	core.Logf("内置页面下载的 %s 已保存到 %s", name, final)
	st.Mu.Lock()
	inRoots := false
	for _, r := range st.Settings.Roots {
		if core.UnderDir(dir, r) {
			inRoots = true
		}
	}
	if !inRoots {
		st.Settings.Roots = append(st.Settings.Roots, dir) // so the download shows up in the library
	}
	auto := st.Settings.AutoBooth
	st.Mu.Unlock()
	_ = st.Save()
	library.StartPipeline(st, true, true, auto, false, nil)
	return final, nil
}
