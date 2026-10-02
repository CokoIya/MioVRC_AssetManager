package main

// What a project holds that the pipeline can work with: the folders under Assets/ that carry prefabs, each
// tied to the library asset it came from when that is known (the usage scan's GUID match, or the record the
// import left), with the library's category. Unity adds what it knows (whole avatars, Modular Avatar setup,
// what is on the avatar already) when it is open.

import (
	"context"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type ProjAsset struct {
	Folder  string `json:"folder"`           // Assets/<shop>/<item>
	Name    string `json:"name"`             // the library's name for it, else the folder's
	Kind    string `json:"kind"`             // 素体, 衣服, 头发, 配饰, 道具, 其他
	Key     string `json:"key,omitempty"`    // library asset
	Prefabs int    `json:"prefabs"`          // .prefab files under the folder
	Source  string `json:"source,omitempty"` // how it was tied to the library: 匹配 (GUIDs), 导入 (this program put it there)
	Avatar  bool   `json:"avatar,omitempty"` // a whole avatar is among its prefabs (Unity said)
	MAReady bool   `json:"maReady,omitempty"`
	Worn    bool   `json:"worn,omitempty"` // an instance of one of its prefabs is on the avatar
}

var pipelineKinds = []string{"素体", "衣服", "头发", "配饰", "道具"}

// folders under Assets that are tools, not assets
var skipAssetDirs = map[string]bool{"vrcsdk": true, "gesture manager": true, "blackstartx": true, "liltoon": true, "_backup": true, "miovrca": true,
	"_claudetemp": true, "editor": true, "scenes": true, "streamingassets": true, "plugins": true, "resources": true, "samples": true, "textmesh pro": true,
	"_mechagirl": true, "mikusu icon": true, "zzz_generatedassets": true, "vrcfury": true, "__generated": true, "thry": true, "poiyomi": true, "_poiyomishaders": true}

// scanProjectFolders: prefab counts by asset folder (Assets/<shop>/<item>). Sibling folders named after base
// bodies are one asset in several versions, so "Assets/Dress/Kaguya" and "Assets/Dress/Plum" fold into "Assets/Dress".
func scanProjectFolders(st *Store, project string) map[string]int {
	st.mu.RLock()
	defs := parseBases(st.Settings.Bases)
	st.mu.RUnlock()
	root := filepath.Join(project, "Assets")
	counts := map[string]int{}
	_ = filepath.WalkDir(root, func(fp string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			n := strings.ToLower(d.Name())
			if fp != root && (skipAssetDirs[n] || strings.HasPrefix(n, ".") || strings.HasSuffix(n, "~")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(d.Name()), ".prefab") {
			return nil
		}
		rel, _ := filepath.Rel(root, fp)
		parts := strings.Split(filepath.ToSlash(rel), "/")
		folder := "Assets"
		switch {
		case len(parts) >= 3:
			folder = "Assets/" + parts[0] + "/" + parts[1]
		case len(parts) == 2:
			folder = "Assets/" + parts[0]
		}
		counts[folder]++
		return nil
	})
	isBase := func(name string) bool {
		bs := detectBases(name, defs)
		return len(bs) == 1 && strings.EqualFold(bs[0], strings.TrimSpace(name))
	}
	byParent := map[string][]string{}
	for f := range counts {
		if p := parentFolder(f); p != "Assets" && p != "" && isBase(f[len(p)+1:]) {
			byParent[p] = append(byParent[p], f)
		}
	}
	for p, kids := range byParent {
		if len(kids) < 2 {
			continue
		}
		for _, k := range kids {
			counts[p] += counts[k]
			delete(counts, k)
		}
	}
	return counts
}

// projectAssets lists the project's assets for the pipeline page.
func projectAssets(st *Store, project string) []ProjAsset {
	counts := scanProjectFolders(st, project)
	// what the library knows about folders in this project
	type known struct {
		key, name, cat, source string
	}
	byFolder := map[string]known{}
	st.mu.RLock()
	pname := filepath.Base(project)
	for _, a := range st.Assets {
		for _, u := range a.Usage {
			if u.Project == pname && u.Folder != "" {
				if _, have := byFolder[strings.ToLower(u.Folder)]; !have {
					byFolder[strings.ToLower(u.Folder)] = known{a.Key, a.Name, a.Category, "匹配"}
				}
			}
		}
	}
	imported := loadAIConfig().Imported[pathKey(project)]
	for folder, key := range imported {
		for _, a := range st.Assets {
			if a.Key == key || a.AltKey == key {
				byFolder[strings.ToLower(folder)] = known{a.Key, a.Name, a.Category, "导入"}
				break
			}
		}
	}
	st.mu.RUnlock()
	kindOf := func(cat string) string {
		if containsStr(pipelineKinds, cat) {
			return cat
		}
		return "其他"
	}
	st.mu.RLock()
	defs := parseBases(st.Settings.Bases)
	st.mu.RUnlock()
	isBase := func(name string) bool {
		bs := detectBases(name, defs)
		return len(bs) == 1 && strings.EqualFold(bs[0], strings.TrimSpace(name))
	}
	var out []ProjAsset
	seen := map[string]bool{}
	for folder, n := range counts {
		pa := ProjAsset{Folder: folder, Name: strings.TrimPrefix(folder, "Assets/"), Prefabs: n}
		// called by its own folder; a folder named after the base body is a version of what sits above it,
		// unless it holds the body itself (IKUSIA/kaguya with kaguya.prefab in it)
		parts := strings.Split(pa.Name, "/")
		if last := parts[len(parts)-1]; len(parts) > 1 && isBase(last) && !statOK(filepath.Join(project, filepath.FromSlash(folder), last+".prefab")) && !statOK(filepath.Join(project, filepath.FromSlash(folder), last+".fbx")) {
			pa.Name = parts[len(parts)-2]
		} else {
			pa.Name = last
		}
		if folder == "Assets" {
			pa.Name = "（Assets 根目录）"
		}
		// the library's record for this folder, or for the folder it sits in
		low := strings.ToLower(folder)
		for f := low; f != ""; f = parentFolder(f) {
			if k, ok := byFolder[f]; ok {
				pa.Key, pa.Name, pa.Kind, pa.Source = k.key, k.name, kindOf(k.cat), k.source
				break
			}
		}
		if pa.Kind == "" {
			pa.Kind = "其他"
			// the folder's own name first, then the folders above it ("_头发/樱发": 头发)
			for _, cand := range append([]string{pa.Name}, reverseStrings(parts)...) {
				if c := classify(strings.TrimLeft(cand, "_")); containsStr(pipelineKinds, c) {
					pa.Kind = c
					break
				}
			}
		}
		seen[low] = true
		out = append(out, pa)
	}
	// library records for folders the scan found no prefab in (a tool package, a folder the player renamed)
	for f, k := range byFolder {
		if seen[f] || k.cat == "插件" || k.cat == "材质" || k.cat == "音效" || k.cat == "字体" {
			continue
		}
		full := f
		if !statOK(filepath.Join(project, filepath.FromSlash(full))) {
			continue
		}
		out = append(out, ProjAsset{Folder: f, Name: k.name, Kind: kindOf(k.cat), Key: k.key, Source: k.source})
	}
	rank := func(k string) int {
		for i, x := range pipelineKinds {
			if x == k {
				return i
			}
		}
		return len(pipelineKinds)
	}
	sort.Slice(out, func(i, j int) bool {
		if rank(out[i].Kind) != rank(out[j].Kind) {
			return rank(out[i].Kind) < rank(out[j].Kind)
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func reverseStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for i := len(in) - 1; i >= 0; i-- {
		out = append(out, in[i])
	}
	return out
}

func parentFolder(f string) string {
	i := strings.LastIndex(f, "/")
	if i < 0 {
		return ""
	}
	return f[:i]
}

// ---------- what Unity adds ----------

var (
	enrichMu    sync.Mutex
	enrichCache = map[string]struct {
		at  time.Time
		out []ProjAsset
	}{}
)

// projectAssetsLive: the list with Unity's knowledge folded in when the editor is open (cached briefly;
// fresh asks Unity again, for after the line ran).
func projectAssetsLive(st *Store, project string, fresh bool) []ProjAsset {
	list := projectAssets(st, project)
	if _, ok := bridgeAlive(project); !ok || len(list) == 0 {
		return list
	}
	key := pathKey(project)
	enrichMu.Lock()
	c, ok := enrichCache[key]
	enrichMu.Unlock()
	if ok && !fresh && time.Since(c.at) < 20*time.Second && len(c.out) == len(list) {
		return c.out
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	var folders []string
	for _, a := range list {
		folders = append(folders, a.Folder)
	}
	raw, err := bridgeCall(ctx, project, "prefabs", map[string]any{"folders": folders, "limit": 400}, 20*time.Second)
	if err == nil {
		var o struct {
			Prefabs []prefabInfo `json:"prefabs"`
		}
		_ = json.Unmarshal(raw, &o)
		for i := range list {
			n := 0
			for _, p := range o.Prefabs {
				if !under(p.Path, list[i].Folder) {
					continue
				}
				n++
				if p.WholeAvatar {
					list[i].Avatar = true
				}
				if p.MAReady {
					list[i].MAReady = true
				}
			}
			if n > 0 {
				list[i].Prefabs = n
			}
			if list[i].Avatar && list[i].Kind == "其他" {
				list[i].Kind = "素体"
			}
		}
	}
	if raw, err := bridgeCall(ctx, project, "inspect", map[string]any{}, 20*time.Second); err == nil {
		var o struct {
			Avatars []struct {
				Prefab   string `json:"prefab"`
				Children []struct {
					Prefab string `json:"prefab"`
					Kind   string `json:"kind"`
				} `json:"children"`
			} `json:"avatars"`
		}
		_ = json.Unmarshal(raw, &o)
		for i := range list {
			for _, av := range o.Avatars {
				if under(av.Prefab, list[i].Folder) {
					list[i].Worn = true
				}
				for _, c := range av.Children {
					if c.Prefab != "" && under(c.Prefab, list[i].Folder) {
						list[i].Worn = true
					}
				}
			}
		}
	}
	enrichMu.Lock()
	enrichCache[key] = struct {
		at  time.Time
		out []ProjAsset
	}{time.Now(), list}
	enrichMu.Unlock()
	return list
}

func under(path, folder string) bool {
	p, f := strings.ToLower(strings.ReplaceAll(path, `\`, "/")), strings.ToLower(folder)
	return p == f || strings.HasPrefix(p, f+"/")
}

// a project folder the player typed or picked, for the page's asset list
func projectAssetsOf(st *Store, project string, fresh bool) ([]ProjAsset, bool) {
	if !isUnityProject(project) {
		return nil, false
	}
	return projectAssetsLive(st, project, fresh), true
}
