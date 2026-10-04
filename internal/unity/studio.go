package unity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"vrclib"
	"vrclib/internal/core"
)

// ---------- the photo studio (摄影棚) ----------
//
// com.miovrc.studio poses, lights and photographs the avatar in Unity's Play mode. The program puts the package
// into the project and asks for the studio by leaving UserSettings/MioVRCA/studio/open.json there: the package's
// editor code looks for it once a second (an editor that is still opening finds it once it has compiled), takes
// it away and enters Play mode. So the request works the same whether Unity is open already or not.

const StudioPkg = "com.miovrc.studio"

// one install, request or removal at a time (a second click must not move the package while the first writes it)
var studioMu sync.Mutex

// StudioDataDir: the studio's settings and saved poses, and the program's request to open it.
func StudioDataDir(p string) string { return filepath.Join(p, "UserSettings", "MioVRCA", "studio") }

// StudioInstalled: our studio package is in the project.
func StudioInstalled(p string) bool {
	name, _, author := pkgVersion(filepath.Join(p, "Packages", StudioPkg))
	return name == StudioPkg && author == "MioVRC"
}

// EmbeddedStudioVersion: the version of the studio package this program carries.
func EmbeddedStudioVersion() string { return embeddedVersion(StudioPkg) }

// studioHashFile: the fingerprint of the files an install put in, kept in the installed package. Unity skips
// names that begin with a dot, so it needs no .meta and is never imported.
const studioHashFile = ".miovrca-hash"

// studioDelSuffix: where a removal moves the package before deleting it. Unity skips names that end in "~", so
// what could not be deleted (a file still held) is out of the project already and goes on the next install or removal.
const studioDelSuffix = ".del~"

// studioRunningFresh: the package rewrites StudioDataDir/running every 5 s while the studio is open in Unity and
// deletes it when the studio closes; younger than this, the studio is open (or was until a crash moments ago).
const studioRunningFresh = 20 * time.Second

// embeddedStudioHash: SHA-256 over the paths and bytes of the studio files this program carries, so a changed file
// is put in again even when package.json keeps its version. fs.WalkDir goes in lexical order: the same files give
// the same hash.
var embeddedStudioHash = sync.OnceValue(func() string {
	root := "unityhelper/" + StudioPkg
	h := sha256.New()
	_ = fs.WalkDir(vrclib.UnityHelper, root, func(fp string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := vrclib.UnityHelper.ReadFile(fp)
		if err != nil {
			return err
		}
		// lengths first: a byte cannot move from one file (or name) to the next and give the same hash
		rel := strings.TrimPrefix(fp, root+"/")
		fmt.Fprintf(h, "%d:%s%d:", len(rel), rel, len(b))
		h.Write(b)
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))
})

// studioCurrent: the copy in the project (ours, version ver) is the one this program carries, whole: the same
// version, the same files (by the fingerprint the install left), none of them gone (a package folder Unity finds
// without its scripts does not open the studio and says nothing).
func studioCurrent(dir, ver string) bool {
	if ver != EmbeddedStudioVersion() {
		return false
	}
	if b, err := os.ReadFile(filepath.Join(dir, studioHashFile)); err != nil || strings.TrimSpace(string(b)) != embeddedStudioHash() {
		return false
	}
	root := "unityhelper/" + StudioPkg
	whole := true
	_ = fs.WalkDir(vrclib.UnityHelper, root, func(fp string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && !core.StatOK(filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(fp, root+"/")))) {
			whole = false
			return fs.SkipAll
		}
		return nil
	})
	return whole
}

// writeStudioPackage: the embedded package, and the fingerprint of it, before the folder is moved into place (so an
// installed copy without the fingerprint is an older or broken one).
func writeStudioPackage(tmp string) error {
	if err := writeEmbeddedPackage(StudioPkg)(tmp); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(tmp, studioHashFile), []byte(embeddedStudioHash()+"\n"), 0644)
}

// studioRunning: the studio is open in Unity's Play mode now (its running file is fresh). A time ahead of the clock
// is the clock having been set back, not a studio.
func studioRunning(p string) bool {
	fi, err := os.Stat(filepath.Join(StudioDataDir(p), "running"))
	if err != nil {
		return false
	}
	age := time.Since(fi.ModTime())
	return age < studioRunningFresh && age > -studioRunningFresh
}

// OpenStudio puts the studio package into the project (again when the copy there is another version, has other
// files or lacks some), asks for the studio, and opens the project in Unity when it is not open. lang is the
// language of the studio's interface. note says what happens next; that Unity could not be opened is a note too
// (the package is in).
func OpenStudio(p, lang string) (note string, err error) {
	studioMu.Lock()
	defer studioMu.Unlock()
	dir := filepath.Join(p, "Packages", StudioPkg)
	_ = os.RemoveAll(dir + studioDelSuffix) // what an earlier removal could not delete
	name, ver, author := pkgVersion(dir)
	had := core.StatOK(dir)
	if had && !(name == StudioPkg && author == "MioVRC") {
		return "", errors.New("工程的 Packages 中已存在 " + StudioPkg + "，但不是由本软件安装，未作改动")
	}
	want := EmbeddedStudioVersion()
	fresh := !had || !studioCurrent(dir, ver) // Unity has to import (and compile) the package first
	if fresh {
		if err := putPackage(p, StudioPkg, writeStudioPackage); err != nil {
			return "", fmt.Errorf("摄影棚插件安装失败：%v（文件可能被 Unity 占用，请关闭 Unity 后重试）", err)
		}
		if had && ver != want {
			core.Logf("摄影棚插件已更新（%s → %s）：%s", ver, want, p)
		} else if had {
			core.Logf("摄影棚插件的文件与本软件所带的不同，已重新放入：%s", p)
		} else {
			core.Logf("摄影棚插件已放进 %s", p)
		}
	}
	if lang != "en" && lang != "ja" {
		lang = "zh"
	}
	if err := writeStudioRequest(p, lang); err != nil {
		return "", fmt.Errorf("无法写入摄影棚请求：%v", err)
	}
	core.Logf("已请求打开摄影棚：%s", p)
	if ProjectRunning(p) {
		if !fresh {
			return "切换到 Unity 窗口即可进入摄影棚", nil
		}
		// an open editor sees new files only when it looks again: the pipeline package, when it runs, has it look now
		if _, ok := ReadBridgeAlive(p); ok {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				_, _ = BridgeCall(ctx, p, "refresh", map[string]any{}, 15*time.Second)
			}()
			return "摄影棚插件已放进工程，Unity 将自动导入并进入摄影棚", nil
		}
		return "摄影棚插件已放进工程。切换到 Unity 窗口，导入并编译完成后会自动进入摄影棚", nil
	}
	// a Unity started moments ago (from this button or 打开 Unity) is still on its way: it finds the request once
	// it has opened, and a second one would only report the project as in use
	if unityStarting(p) {
		core.Logf("Unity 正在打开 %s，不再启动第二个", p)
	} else if err := OpenInUnity(p); err != nil {
		return "摄影棚插件已安装，但无法打开 Unity：" + err.Error(), nil
	}
	if fresh {
		return "正在打开 Unity，首次需导入插件并编译（约一至两分钟），之后自动进入摄影棚", nil
	}
	return "正在打开 Unity，打开后自动进入摄影棚", nil
}

// writeStudioRequest leaves open.json for the package, whole or not at all (it may be reading the folder right now).
func writeStudioRequest(p, lang string) error {
	dir := StudioDataDir(p)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	b, _ := json.Marshal(struct {
		At   int64  `json:"at"`
		Lang string `json:"lang"`
	}{time.Now().Unix(), lang})
	tmp, dst := filepath.Join(dir, "open.json.tmp"), filepath.Join(dir, "open.json")
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	var err error
	for try := 0; try < 5; try++ { // Unity may hold an older request open for a moment
		if err = os.Rename(tmp, dst); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = os.Remove(tmp)
	return err
}

// RemoveStudio takes the studio package out of the project, with its settings and saved poses. The pictures stay
// (they are in the Pictures folder; a studio without one keeps them in its photos folder, which stays too).
// Not while the studio is open: Unity would drop its scripts in Play mode, before they put the camera and lights
// they made back.
func RemoveStudio(p string) error {
	studioMu.Lock()
	defer studioMu.Unlock()
	if studioRunning(p) {
		return errors.New("摄影棚正在 Unity 中打开，请先退出 Play 模式再移除")
	}
	dir := filepath.Join(p, "Packages", StudioPkg)
	gone := dir + studioDelSuffix
	_ = os.RemoveAll(gone) // what an earlier removal could not delete
	if core.StatOK(dir) {
		if name, _, author := pkgVersion(dir); !(name == StudioPkg && author == "MioVRC") {
			return errors.New("工程中的 " + StudioPkg + " 不是由本软件安装，未作改动")
		}
		// the folder steps aside whole first: deleting it in place stops halfway when Unity holds a file, and a
		// package left without its package.json is no longer recognisably ours (to remove or to replace)
		if err := os.Rename(dir, gone); err != nil {
			return fmt.Errorf("摄影棚插件移除失败：%v（如 Unity 正在运行，请关闭后重试）", err)
		}
		if err := os.RemoveAll(gone); err != nil {
			core.Logf("摄影棚插件的旧文件未能全部删除（Unity 不会读取），下次安装或移除时再删：%v", err)
		}
	}
	data := StudioDataDir(p)
	if ents, err := os.ReadDir(data); err == nil {
		for _, e := range ents {
			if e.Name() != "photos" {
				_ = os.RemoveAll(filepath.Join(data, e.Name()))
			}
		}
	}
	_ = os.Remove(filepath.Join(data, "photos"))               // only when empty
	_ = os.Remove(data)                                        // only when no photos are left in it
	_ = os.Remove(filepath.Join(p, "UserSettings", "MioVRCA")) // only when nothing else (cover pictures, the pipeline's) is in it
	core.Logf("摄影棚插件已从 %s 移除", p)
	return nil
}
