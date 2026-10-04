package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"vrclib/internal/core"
	"vrclib/internal/library"
	"vrclib/internal/unity"
)

// Covers from prefabs: an asset without a picture gets one drawn by the Unity plugin ("prefab_shot") from a
// prefab of its own, in a project that has the asset imported and is open in Unity.

// coverFolder: an asset's folder in a project.
type coverFolder struct {
	project string // the project's path
	folder  string // Assets/<shop>/<item>
}

// coverFolders: where the asset's files are in the projects of the list — by the usage count's folder, and by
// what this program imported.
func coverFolders(st *core.Store, key string) (name, category string, out []coverFolder, found bool) {
	imported := loadAIConfig().Imported
	st.Mu.RLock()
	defer st.Mu.RUnlock()
	var a *core.Asset
	for _, x := range st.Assets {
		if x.Key == key || (x.AltKey != "" && x.AltKey == key) {
			a = x
			break
		}
	}
	if a == nil {
		return "", "", nil, false
	}
	v := library.BuildView(st, a)
	name, category = v.Name, v.Category
	seen := map[string]bool{}
	add := func(project, folder string) {
		k := core.PathKey(project) + "|" + strings.ToLower(folder)
		if folder != "" && !seen[k] {
			seen[k] = true
			out = append(out, coverFolder{project, folder})
		}
	}
	for _, p := range st.Projects {
		for _, u := range a.Usage {
			if u.Project == filepath.Base(p.Path) {
				add(p.Path, u.Folder)
			}
		}
		for folder, k := range imported[core.PathKey(p.Path)] {
			if k == a.Key || (a.AltKey != "" && k == a.AltKey) {
				add(p.Path, folder)
			}
		}
	}
	return name, category, out, true
}

// CoverStatus: what the asset's details panel says about a cover from Unity.
type CoverStatus struct {
	Real      bool   `json:"real"`              // it has a picture of its own: nothing to generate
	Generated bool   `json:"generated"`         // the cover it shows was made from a prefab
	Prefab    string `json:"prefab,omitempty"`  // …this one
	Can       bool   `json:"can"`               // a project that holds it is open in Unity, with the plugin answering
	Why       string `json:"why,omitempty"`     // why not
	Project   string `json:"project,omitempty"` // the project it would be drawn in
}

func CoverStatusOf(st *core.Store, key string) CoverStatus {
	var s CoverStatus
	_, _, folders, ok := coverFolders(st, key)
	if !ok {
		s.Why = "该素材不在本地素材库中，无法从 Unity 生成封面"
		return s
	}
	st.Mu.RLock()
	for _, a := range st.Assets {
		if a.Key == key {
			s.Real = library.HasRealCover(st, a)
			break
		}
	}
	st.Mu.RUnlock()
	if g := library.GeneratedCoverInfo(key); g != nil {
		s.Generated, s.Prefab = !s.Real, g.Prefab
	}
	if len(folders) == 0 {
		s.Why = "该素材尚未导入任何工程。导入工程并统计使用情况后，才能从 Unity 生成封面"
		return s
	}
	closed := ""
	for _, f := range folders {
		if _, alive := unity.ReadBridgeAlive(f.project); alive {
			s.Can, s.Project = true, filepath.Base(f.project)
			return s
		}
		if closed == "" {
			closed = filepath.Base(f.project)
		}
	}
	s.Project = closed
	s.Why = "请先在 Unity 中打开工程「" + closed + "」，并在流水线页确认插件已连接"
	return s
}

// ---------- which prefab stands for the asset ----------

func coverWords(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

func squash(s string) string { return strings.Join(coverWords(s), "") }

// parts of an outfit that come as prefabs of their own, and helpers nobody wants as a cover
var coverPartWords = []string{"physbone", "collider", "armature", "bone", "icon", "menu", "param", "toggle", "fx", "anim", "particle", "shader", "material", "sample", "test"}

// pickCoverPrefabs: the prefabs to try, the most representative first. The one named after the asset wins;
// among several colours of one outfit, the first; a whole avatar stands for a base body only.
func pickCoverPrefabs(name, category string, list []prefabInfo) []string {
	want := squash(name)
	type cand struct {
		p     prefabInfo
		score int
	}
	var cs []cand
	for _, p := range list {
		if p.Renderers == 0 {
			continue // nothing to see
		}
		c := cand{p: p}
		pn := squash(p.Name)
		switch {
		case pn != "" && pn == want:
			c.score += 100
		case pn != "" && want != "" && (strings.Contains(want, pn) || strings.Contains(pn, want)) && len(pn) >= 3:
			c.score += 60
		default:
			aw := coverWords(name)
			for _, w := range coverWords(p.Name) {
				if len(w) >= 3 && core.ContainsStr(aw, w) {
					c.score += 12
				}
			}
		}
		if p.WholeAvatar != (category == "素体") {
			c.score -= 40 // an outfit shown on somebody's body, or a body's spare part
		}
		for _, w := range coverPartWords {
			if strings.Contains(strings.ToLower(p.Name), w) {
				c.score -= 50
				break
			}
		}
		c.score += min(p.Renderers, 10)           // the whole set rather than one piece of it
		c.score -= 2 * strings.Count(p.Path, "/") // near the top of the package
		c.score -= strings.Count(strings.ToLower(p.Path), "/editor/") * 50
		cs = append(cs, c)
	}
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].score != cs[j].score {
			return cs[i].score > cs[j].score
		}
		return strings.ToLower(cs[i].p.Path) < strings.ToLower(cs[j].p.Path) // the first colour
	})
	var out []string
	for _, c := range cs {
		out = append(out, c.p.Path)
	}
	return out
}

// ---------- drawing one ----------

// prefabErr: this prefab could not be drawn (another one may be). Any other error is Unity not answering.
type prefabErr struct{ msg string }

func (e *prefabErr) Error() string { return e.msg }

const coverTries = 3 // prefabs tried for one asset before giving up

// GenerateCover draws a cover for the asset in a project that is open in Unity (only, when one is named) and
// keeps it as the asset's generated cover. It returns the prefab that was drawn.
func GenerateCover(ctx context.Context, st *core.Store, key, only string) (string, error) {
	name, category, folders, ok := coverFolders(st, key)
	if !ok {
		return "", errors.New("该素材不在本地素材库中")
	}
	var live []coverFolder
	for _, f := range folders {
		if only != "" && core.PathKey(f.project) != core.PathKey(only) {
			continue
		}
		if _, alive := unity.ReadBridgeAlive(f.project); alive {
			live = append(live, f)
		}
	}
	if len(live) == 0 {
		if len(folders) == 0 {
			return "", errors.New("该素材尚未导入任何工程")
		}
		return "", errors.New("请先在 Unity 中打开导入了该素材的工程「" + filepath.Base(folders[0].project) + "」，并确认插件已连接")
	}
	project := live[0].project
	var dirs []string
	for _, f := range live {
		if f.project == project {
			dirs = append(dirs, f.folder)
		}
	}
	raw, err := unity.BridgeCall(ctx, project, "prefabs", map[string]any{"folders": dirs, "limit": 200}, 30*time.Second)
	if err != nil {
		return "", err
	}
	var o struct {
		Prefabs []prefabInfo `json:"prefabs"`
	}
	_ = json.Unmarshal(raw, &o)
	cands := pickCoverPrefabs(name, category, o.Prefabs)
	if len(cands) == 0 {
		return "", &prefabErr{"该素材在工程中没有带网格的 prefab，无法生成封面"}
	}
	if len(cands) > coverTries {
		cands = cands[:coverTries]
	}
	var last error
	for _, prefab := range cands {
		png, err := prefabShot(ctx, project, prefab)
		if err != nil {
			var pe *prefabErr
			if last = err; !errors.As(err, &pe) {
				return "", err // Unity is not answering: the next prefab would fare no better
			}
			continue
		}
		if err := library.SetGeneratedCover(key, png, prefab, project); err != nil {
			return "", err
		}
		return prefab, nil
	}
	return "", last
}

// prefabShot: one prefab's picture, read from the file the plugin wrote.
func prefabShot(ctx context.Context, project, prefab string) ([]byte, error) {
	raw, err := unity.BridgeCall(ctx, project, "prefab_shot", map[string]any{"prefabs": []string{prefab}, "size": 512}, 90*time.Second)
	if err != nil {
		if strings.Contains(err.Error(), "不认识的操作") {
			err = errors.New("该工程的 Unity 插件为旧版，不支持生成封面：请在流水线页点击「更新」，待 Unity 编译完成后重试")
		}
		return nil, err
	}
	var o struct {
		Shots []struct {
			File  string `json:"file"`
			Error string `json:"error"`
		} `json:"shots"`
	}
	if json.Unmarshal(raw, &o) != nil || len(o.Shots) == 0 {
		return nil, errors.New("无法解析 Unity 的响应")
	}
	s := o.Shots[0]
	if s.Error != "" || s.File == "" {
		if s.Error == "" {
			s.Error = "Unity 未能渲染该 prefab"
		}
		return nil, &prefabErr{"「" + strings.TrimSuffix(path.Base(prefab), ".prefab") + "」" + s.Error}
	}
	// only a picture the plugin put into this project is read
	file := filepath.Clean(filepath.FromSlash(s.File))
	if rel, err := filepath.Rel(filepath.Clean(project), file); err != nil || strings.HasPrefix(rel, "..") || !strings.EqualFold(filepath.Ext(file), ".png") {
		return nil, errors.New("Unity 返回的图片路径不在该工程中")
	}
	fi, err := os.Stat(file)
	if err != nil || fi.Size() > 16<<20 {
		return nil, errors.New("Unity 生成的图片无法读取")
	}
	b, err := os.ReadFile(file)
	_ = os.Remove(file) // it has served
	return b, err
}

// ---------- every asset of a project that has no cover ----------

type CoverBatch struct {
	Project string `json:"project"`
	Running bool   `json:"running"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Made    int    `json:"made"`
	Failed  int    `json:"failed"`
	Now     string `json:"now,omitempty"`  // the asset being drawn
	Err     string `json:"err,omitempty"`  // why it stopped early
	Last    string `json:"last,omitempty"` // why the last asset that failed did
	Ended   int64  `json:"ended,omitempty"`
}

var (
	batchMu     sync.Mutex
	batch       CoverBatch
	batchCancel context.CancelFunc
)

func CoverBatchSnapshot() CoverBatch {
	batchMu.Lock()
	defer batchMu.Unlock()
	return batch
}

func CancelCoverBatch() {
	batchMu.Lock()
	defer batchMu.Unlock()
	if batchCancel != nil {
		batchCancel()
	}
}

// coverless: the project's assets (as the pipeline page lists them) that the library knows and that show no
// picture of their own and none made before.
func coverless(st *core.Store, project string) (keys, names []string) {
	seen := map[string]bool{}
	list := projectAssets(st, project)
	st.Mu.RLock()
	defer st.Mu.RUnlock()
	byKey := map[string]*core.Asset{}
	for _, a := range st.Assets {
		byKey[a.Key] = a
	}
	for _, pa := range list {
		a := byKey[pa.Key]
		if a == nil || seen[pa.Key] || pa.Prefabs == 0 {
			continue
		}
		seen[pa.Key] = true
		if v := library.BuildView(st, a); v.Cover == "" {
			keys, names = append(keys, pa.Key), append(names, v.Name)
		}
	}
	return
}

// StartCoverBatch draws covers for the project's assets that have none, one after the other, until it is
// done or cancelled.
func StartCoverBatch(st *core.Store, project string) error {
	if _, alive := unity.ReadBridgeAlive(project); !alive {
		return errors.New("请先在 Unity 中打开该工程，并确认插件已连接")
	}
	batchMu.Lock()
	if batch.Running {
		batchMu.Unlock()
		return errors.New("正在生成封面，请等待完成或先取消")
	}
	keys, names := coverless(st, project)
	if len(keys) == 0 {
		batchMu.Unlock()
		return errors.New("该工程中已导入的素材都已有封面")
	}
	ctx, cancel := context.WithCancel(context.Background())
	batch, batchCancel = CoverBatch{Project: project, Running: true, Total: len(keys)}, cancel
	batchMu.Unlock()
	go func() {
		defer cancel()
		stopped := ""
		for i, key := range keys {
			if ctx.Err() != nil {
				stopped = "已取消"
				break
			}
			batchMu.Lock()
			batch.Now = names[i]
			batchMu.Unlock()
			_, err := GenerateCover(ctx, st, key, project)
			batchMu.Lock()
			batch.Done = i + 1
			if err == nil {
				batch.Made++
			} else if ctx.Err() == nil {
				batch.Failed++
				batch.Last = fmt.Sprintf("「%s」：%s", names[i], err.Error())
			}
			batchMu.Unlock()
			var pe *prefabErr
			if err != nil && ctx.Err() == nil && !errors.As(err, &pe) {
				stopped = err.Error() // Unity went away, or the plugin is an old one: the rest would fail the same way
				break
			}
		}
		if ctx.Err() != nil && stopped == "" {
			stopped = "已取消"
		}
		batchMu.Lock()
		batch.Running, batch.Now, batch.Err, batch.Ended = false, "", stopped, time.Now().Unix()
		batchCancel = nil
		batchMu.Unlock()
	}()
	return nil
}
