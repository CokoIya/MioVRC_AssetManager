package library

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"sync"
	"time"

	"vrclib/internal/core"
)

// Covers made from a prefab in Unity, for assets that have no picture of their own. They are kept apart from
// the library (gencovers.json, covers/generated) and come last: a Booth picture, a picture beside the files
// or one the player picks shows instead as soon as there is one.

type GenCover struct {
	File    string `json:"file"`              // in covers/generated
	Prefab  string `json:"prefab,omitempty"`  // the prefab it shows
	Project string `json:"project,omitempty"` // the project it was drawn in
	At      int64  `json:"at"`
}

var (
	genMu  sync.RWMutex
	genDir string // the data folder the list was read from
	genAll map[string]*GenCover
)

func genCoverDir() string  { return filepath.Join(core.CoversDir(), "generated") }
func genCoverFile() string { return filepath.Join(core.DataDir, "gencovers.json") }

// genCovers: the list, read once per data folder. An entry whose picture is gone is dropped. For looking only
// (under genMu.RLock): a change takes the list inside the lock (genLoadLocked).
func genCovers() map[string]*GenCover {
	genMu.RLock()
	m, ok := genAll, genAll != nil && genDir == core.DataDir
	genMu.RUnlock()
	if ok {
		return m
	}
	genMu.Lock()
	defer genMu.Unlock()
	return genLoadLocked()
}

// genLoadLocked: the list of the data folder, read from its file when it is not the one in memory (a library
// import or its undo drops it at any moment). Caller holds genMu for writing.
func genLoadLocked() map[string]*GenCover {
	if genAll != nil && genDir == core.DataDir {
		return genAll
	}
	var f struct {
		Covers map[string]*GenCover `json:"covers"`
	}
	if b, err := os.ReadFile(genCoverFile()); err == nil {
		_ = json.Unmarshal(b, &f)
	}
	m := map[string]*GenCover{}
	for k, c := range f.Covers {
		if c != nil && c.File != "" && core.StatOK(filepath.Join(genCoverDir(), filepath.Base(c.File))) {
			m[k] = c
		}
	}
	genAll, genDir = m, core.DataDir
	return m
}

func saveGenCoversLocked(m map[string]*GenCover) error {
	b, _ := json.MarshalIndent(map[string]any{"version": 1, "covers": m}, "", " ")
	tmp := genCoverFile() + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, genCoverFile()) // whole at every moment
}

// genChange changes the list and saves it as one step under the lock, on the list in memory at that moment.
// change says whether there is anything to save.
func genChange(change func(m map[string]*GenCover) bool) error {
	genMu.Lock()
	defer genMu.Unlock()
	m := genLoadLocked()
	if !change(m) {
		return nil
	}
	return saveGenCoversLocked(m)
}

// GeneratedCover: the picture made for an asset, "" when it has none.
func GeneratedCover(key string) string {
	m := genCovers()
	genMu.RLock()
	defer genMu.RUnlock()
	if c := m[key]; c != nil {
		return filepath.Join(genCoverDir(), filepath.Base(c.File))
	}
	return ""
}

// GeneratedCoverInfo: where the asset's generated cover came from, nil when it has none.
func GeneratedCoverInfo(key string) *GenCover {
	m := genCovers()
	genMu.RLock()
	defer genMu.RUnlock()
	if c := m[key]; c != nil {
		cp := *c
		return &cp
	}
	return nil
}

// SetGeneratedCover keeps a PNG as the asset's generated cover. Each one gets a file name of its own, so the
// window (which caches a picture by its path) shows the new one.
func SetGeneratedCover(key string, png []byte, prefab, project string) error {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(png))
	if err != nil || format != "png" || cfg.Width < 16 || cfg.Height < 16 {
		return errors.New("Unity 生成的图片无法读取")
	}
	if err := os.MkdirAll(genCoverDir(), 0755); err != nil {
		return err
	}
	h := sha1.Sum([]byte(key))
	name := fmt.Sprintf("%s_%d.png", hex.EncodeToString(h[:6]), time.Now().UnixMilli())
	dst := filepath.Join(genCoverDir(), name)
	if err := os.WriteFile(dst+".part", png, 0644); err != nil {
		return err
	}
	if err := os.Rename(dst+".part", dst); err != nil {
		_ = os.Remove(dst + ".part")
		return err
	}
	var old *GenCover
	err = genChange(func(m map[string]*GenCover) bool {
		old = m[key]
		m[key] = &GenCover{File: name, Prefab: prefab, Project: project, At: time.Now().Unix()}
		return true
	})
	if old != nil && old.File != name {
		_ = os.Remove(filepath.Join(genCoverDir(), filepath.Base(old.File)))
	}
	core.BumpRev() // the card shows it at the window's next look
	return err
}

// RemoveGeneratedCover takes the asset's generated cover away again.
func RemoveGeneratedCover(key string) bool {
	var old *GenCover
	err := genChange(func(m map[string]*GenCover) bool {
		if old = m[key]; old == nil {
			return false
		}
		delete(m, key)
		return true
	})
	if err != nil {
		core.Logf("gencovers.json 保存失败: %v", err)
	}
	if old == nil {
		return false
	}
	_ = os.Remove(filepath.Join(genCoverDir(), filepath.Base(old.File)))
	core.BumpRev()
	return true
}

// HasRealCover: the asset shows a picture that was not generated (Booth's, one beside its files, one the
// player picked) nor taken from its unitypackage. Caller holds st.Mu.
func HasRealCover(st *core.Store, a *core.Asset) bool {
	v := BuildView(st, a)
	return v.Cover != "" && !v.coverGen && !v.CoverPkg
}

// gencovers.json replaced by a library import or its undo: read again at the next use
func init() {
	OnDataImported(func() {
		genMu.Lock()
		genAll = nil
		genMu.Unlock()
	})
}
