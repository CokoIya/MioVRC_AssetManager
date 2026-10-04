package ai

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/library"
	"vrclib/internal/naming"
	"vrclib/internal/unity"
)

// Outfitting recipes (装配方案): what a run of the line put on an avatar and the menu it built, kept so the
// same can be done in another project or on another base body, or handed to a friend as a file. A recipe
// names things by their place in a project (Assets/…) and by the library's record; it holds no path of this
// computer and nothing of an account.

const (
	recipeFormat  = "miovrca-recipe"
	recipeSchema  = 1
	RecipeExt     = ".miovrca-recipe.json"
	RecipeMaxFile = 512 << 10
)

type RecipeBase struct {
	Prefab string `json:"prefab,omitempty"` // the avatar's prefab in the project
	Name   string `json:"name,omitempty"`   // the base body, as the table of base bodies calls it
}

type RecipeAsset struct {
	Key     string `json:"key,omitempty"`     // the library's record
	Name    string `json:"name"`              // what the library (or the folder) calls it
	BoothID string `json:"boothId,omitempty"` // its shop item, when the library knows it
	URL     string `json:"url,omitempty"`     // … and the item's page
	Prefab  string `json:"prefab"`            // in the project
	Kind    string `json:"kind"`
	Version string `json:"version,omitempty"` // the base body whose version of it was chosen
	Object  string `json:"object"`            // what it was called under the avatar: the menu plan's paths start with it
	Place   bool   `json:"place,omitempty"`   // a plugin, put under the avatar as it is
	Active  *bool  `json:"active,omitempty"`  // shown or hidden, when it is newly put on
}

type RecipeMaterial struct {
	Object   string `json:"object"`
	Slot     int    `json:"slot"`
	Material string `json:"material"`
}

// RecipeItem: one entry of a build_menu plan.
type RecipeItem struct {
	Kind          string           `json:"kind"`
	Path          []string         `json:"path,omitempty"`
	Label         string           `json:"label"`
	Objects       []string         `json:"objects,omitempty"`
	Default       bool             `json:"default,omitempty"`
	Show          bool             `json:"show,omitempty"`
	Parameter     string           `json:"parameter,omitempty"`
	MaterialsFrom string           `json:"materialsFrom,omitempty"`
	Materials     []RecipeMaterial `json:"materials,omitempty"`
}

// RecipeMenu: the arguments of one build_menu call.
type RecipeMenu struct {
	Root      string       `json:"root,omitempty"`
	Parameter string       `json:"parameter,omitempty"`
	Items     []RecipeItem `json:"items"`
}

type Recipe struct {
	Format    string        `json:"format"`
	Schema    int           `json:"schema"`
	ID        string        `json:"id,omitempty"` // the file's name here; an exported file has none
	Name      string        `json:"name"`
	Note      string        `json:"note,omitempty"`
	Created   int64         `json:"created,omitempty"`
	App       string        `json:"app,omitempty"`    // the program's version
	Plugin    string        `json:"plugin,omitempty"` // the Unity plugin's
	Base      RecipeBase    `json:"base"`
	Hierarchy string        `json:"hierarchy,omitempty"`
	Assets    []RecipeAsset `json:"assets"`
	Menus     []RecipeMenu  `json:"menus"`
	Imported  bool          `json:"imported,omitempty"` // it came from a file (here only)
}

// ---------- what a run did, noted as it goes ----------

type recStep struct {
	Prefab string
	Object string
	Place  bool
	Active *bool
	at     int // the session's count of changes once this was done (0: it changed nothing)
}

type recMenu struct {
	RecipeMenu
	at int
}

// runRecord: what the line put on the avatar and built since it was last started, in order.
type runRecord struct {
	assets       []aiAsset // the rows the run was started with
	hierarchy    string
	avatar       string // the avatar worked on, and its prefab
	avatarPrefab string
	steps        []recStep
	menus        []recMenu
}

func (s *aiSession) recAvatar(name, prefab string) {
	s.mu.Lock()
	s.rec.avatar, s.rec.avatarPrefab = name, prefab
	s.mu.Unlock()
}

// recStep notes a prefab that was dressed or placed (once: the same prefab again is the object it became).
func (s *aiSession) recStep(st recStep, changed bool) {
	if st.Prefab == "" || st.Object == "" {
		return
	}
	st.Prefab = strings.ReplaceAll(st.Prefab, `\`, "/")
	s.mu.Lock()
	defer s.mu.Unlock()
	if changed {
		st.at = len(s.changes)
	}
	for i := range s.rec.steps {
		if strings.EqualFold(s.rec.steps[i].Prefab, st.Prefab) {
			s.rec.steps[i].Object = st.Object
			return
		}
	}
	s.rec.steps = append(s.rec.steps, st)
}

// recMenu notes the arguments of a build_menu call that was carried out.
func (s *aiSession) recMenu(root, parameter string, items any, changed bool) {
	m := recMenu{RecipeMenu: RecipeMenu{Root: root, Parameter: parameter}}
	switch l := items.(type) {
	case []any:
		for _, e := range l {
			if o, ok := e.(map[string]any); ok {
				m.Items = append(m.Items, recipeItemOf(o))
			}
		}
	case []map[string]any:
		for _, o := range l {
			m.Items = append(m.Items, recipeItemOf(o))
		}
	}
	if len(m.Items) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if changed {
		m.at = len(s.changes)
	}
	s.rec.menus = append(s.rec.menus, m)
}

// recUndone: the last change was taken back; what was noted for it goes with it.
func (s *aiSession) recUndone() {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.changes)
	var steps []recStep
	for _, x := range s.rec.steps {
		if x.at <= n {
			steps = append(steps, x)
		}
	}
	var menus []recMenu
	for _, x := range s.rec.menus {
		if x.at <= n {
			menus = append(menus, x)
		}
	}
	s.rec.steps, s.rec.menus = steps, menus
}

// recipeItemOf: a menu item as a tool call (or the line's own plan) wrote it.
func recipeItemOf(o map[string]any) RecipeItem {
	it := RecipeItem{Kind: strings.TrimSpace(argStr(o, "kind")), Label: strings.TrimSpace(argStr(o, "label")), Objects: argStrs(o, "objects"),
		Parameter: strings.TrimSpace(argStr(o, "parameter")), MaterialsFrom: strings.TrimSpace(strings.ReplaceAll(argStr(o, "materialsFrom"), `\`, "/"))}
	if it.Kind == "pick" || it.Kind == "hair" { // the plugin reads these as outfit
		it.Kind = "outfit"
	}
	for _, p := range argStrs(o, "path") {
		if p = strings.TrimSpace(p); p != "" && p != "主菜单" {
			it.Path = append(it.Path, p)
		}
	}
	if l, ok := o["path"].([]string); ok { // the line's own plan
		it.Path = append([]string{}, l...)
	}
	if l, ok := o["objects"].([]string); ok {
		it.Objects = append([]string{}, l...)
	}
	it.Default, _ = o["default"].(bool)
	it.Show, _ = o["show"].(bool)
	if l, ok := o["materials"].([]any); ok {
		for _, e := range l {
			m, _ := e.(map[string]any)
			slot, _ := m["slot"].(float64)
			it.Materials = append(it.Materials, RecipeMaterial{Object: argStr(m, "object"), Slot: int(slot), Material: strings.ReplaceAll(argStr(m, "material"), `\`, "/")})
		}
	}
	return it
}

// args: the item as build_menu takes it.
func (it RecipeItem) args() map[string]any {
	o := map[string]any{"kind": it.Kind, "path": append([]string{}, it.Path...), "label": it.Label}
	if len(it.Objects) > 0 {
		o["objects"] = it.Objects
	}
	if it.Default {
		o["default"] = true
	}
	if it.Kind == "toggle" && !it.Default {
		o["default"] = false
	}
	if it.Show {
		o["show"] = true
	}
	if it.Parameter != "" {
		o["parameter"] = it.Parameter
	}
	if it.MaterialsFrom != "" {
		o["materialsFrom"] = it.MaterialsFrom
	}
	if len(it.Materials) > 0 {
		var l []map[string]any
		for _, m := range it.Materials {
			l = append(l, map[string]any{"object": m.Object, "slot": m.Slot, "material": m.Material})
		}
		o["materials"] = l
	}
	return o
}

// ---------- the files ----------

func recipesDir() string { return filepath.Join(core.DataDir, "recipes") }

var reRecipeID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{3,39}$`)

func newRecipeID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(b)
}

// writeRecipe puts a recipe in its file: written whole beside it first, then swapped in, so the file is
// never half there.
func writeRecipe(r *Recipe) error {
	if !reRecipeID.MatchString(r.ID) {
		return errors.New("方案编号无效")
	}
	if err := os.MkdirAll(recipesDir(), 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", " ")
	if err != nil {
		return err
	}
	file := filepath.Join(recipesDir(), r.ID+".json")
	if err := writeFileWhole(file, b); err != nil {
		return errors.New("方案保存失败（" + core.TrimErr(err) + "）")
	}
	return nil
}

func writeFileWhole(file string, b []byte) (err error) {
	tmp := file + ".tmp"
	defer func() {
		if err != nil {
			_ = os.Remove(tmp)
		}
	}()
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	// a scanner holding the old file for a moment makes Windows refuse the swap: tried a few times
	for i := 0; ; i++ {
		if err = os.Rename(tmp, file); err == nil || i == 3 {
			return err
		}
		time.Sleep(time.Duration(i+1) * 50 * time.Millisecond)
	}
}

// recipeWarned: the files that were found unusable, each said once in the log (the list is read often).
var recipeWarned sync.Map

// LoadRecipe reads a saved recipe. The file is held to what a recipe may say (validateRecipe) every time: it
// need not be one this program wrote — a library import puts the recipes of another computer into the folder.
func LoadRecipe(id string) (*Recipe, error) {
	if !reRecipeID.MatchString(id) {
		return nil, errors.New("未找到该方案")
	}
	file := filepath.Join(recipesDir(), id+".json")
	fi, err := os.Stat(file)
	if err != nil || fi.IsDir() {
		return nil, errors.New("未找到该方案")
	}
	mark := file + "|" + fi.ModTime().String()
	bad := func(why string) (*Recipe, error) {
		if _, told := recipeWarned.LoadOrStore(mark, true); !told {
			core.Logf("装配方案 %s 无法使用，已跳过：%s", id+".json", why)
		}
		return nil, errors.New("方案文件已损坏，无法读取")
	}
	if _, known := recipeWarned.Load(mark); known {
		return bad("") // (as it was found the last time: not read again)
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, errors.New("未找到该方案")
	}
	var r Recipe
	if json.Unmarshal(b, &r) != nil || r.Format != recipeFormat {
		return bad("不是 MioVRCA 装配方案文件")
	}
	if err := validateRecipe(&r); err != nil {
		return bad(err.Error())
	}
	r.ID = id
	return &r, nil
}

// RecipeInfo: a recipe as the list shows it.
type RecipeInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Note     string `json:"note,omitempty"`
	Created  int64  `json:"created"`
	Base     string `json:"base,omitempty"`
	Assets   int    `json:"assets"`
	Items    int    `json:"items"`
	Imported bool   `json:"imported,omitempty"`
}

func (r *Recipe) info() RecipeInfo {
	n := 0
	for _, m := range r.Menus {
		n += len(m.Items)
	}
	return RecipeInfo{ID: r.ID, Name: r.Name, Note: r.Note, Created: r.Created, Base: r.Base.Name, Assets: len(r.Assets), Items: n, Imported: r.Imported}
}

// ListRecipes: the saved recipes, the newest first. A file that cannot be read is left out.
func ListRecipes() []RecipeInfo {
	out := []RecipeInfo{}
	ents, _ := os.ReadDir(recipesDir())
	for _, e := range ents {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || e.IsDir() {
			continue
		}
		if r, err := LoadRecipe(id); err == nil {
			out = append(out, r.info())
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Created != out[j].Created {
			return out[i].Created > out[j].Created
		}
		return out[i].ID > out[j].ID
	})
	return out
}

func DeleteRecipe(id string) error {
	if _, err := LoadRecipe(id); err != nil {
		return err
	}
	return os.Remove(filepath.Join(recipesDir(), id+".json"))
}

func RenameRecipe(id, name, note string) (*Recipe, error) {
	r, err := LoadRecipe(id)
	if err != nil {
		return nil, err
	}
	if name, err = tidyRecipeName(name); err != nil {
		return nil, err
	}
	r.Name, r.Note = name, tidyRecipeNote(note)
	if err := validateRecipe(r); err != nil {
		return nil, err
	}
	return r, writeRecipe(r)
}

func tidyRecipeName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return "", errors.New("请填写方案名称")
	}
	if len([]rune(name)) > recipeNameMax {
		return "", fmt.Errorf("方案名称不能超过 %d 个字", recipeNameMax)
	}
	return name, nil
}

func tidyRecipeNote(note string) string {
	note = strings.TrimSpace(strings.ReplaceAll(note, "\r\n", "\n"))
	if r := []rune(note); len(r) > recipeNoteMax {
		note = string(r[:recipeNoteMax])
	}
	return note
}

// ---------- what a recipe may hold ----------

const (
	recipeNameMax   = 60
	recipeNoteMax   = 500
	recipePathMax   = 260 // a path in a project, an object's path under the avatar
	recipeLabelMax  = 60
	recipeAssetsMax = 60
	recipeMenusMax  = 24
	recipeItemsMax  = 400 // in all menus together
)

var recipeItemKinds = []string{"outfit", "part", "toggle", "strip", "skin", "install"}

// cleanText: a line of text with nothing in it a name has no use for, and no longer than max.
func cleanText(s string, max int, what string) error {
	if len([]rune(s)) > max {
		return fmt.Errorf("%s过长（最多 %d 个字）", what, max)
	}
	for _, c := range s {
		if c < 0x20 && c != '\n' && c != '\t' || c == 0x7f {
			return errors.New(what + "含有无法显示的字符")
		}
	}
	return nil
}

// assetPath: a file's place in a project — under Assets/, written with forward slashes, nothing in it that
// leads out of the project.
func assetPath(p, ext, what string) error {
	if err := cleanText(p, recipePathMax, what); err != nil {
		return err
	}
	low := strings.ToLower(p)
	if !strings.HasPrefix(p, "Assets/") || strings.ContainsAny(p, "\\:*?\"<>|\n\t") || !strings.HasSuffix(low, ext) {
		return fmt.Errorf("%s须为工程内以 Assets/ 开头、以 %s 结尾的路径：%s", what, ext, core.Truncate(p, 80))
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "." || seg == ".." || strings.TrimSpace(seg) == "" {
			return fmt.Errorf("%s不是有效的工程内路径：%s", what, core.Truncate(p, 80))
		}
	}
	return nil
}

// objectPath: an object's path under the avatar ("Outfit/Jacket").
func objectPath(p, what string) error {
	if err := cleanText(p, recipePathMax, what); err != nil {
		return err
	}
	if p == "" || strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\\n\t") {
		return errors.New(what + "不是有效的物体路径")
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return errors.New(what + "不是有效的物体路径")
		}
	}
	return nil
}

func paramName(p, what string) error {
	if len(p) > 64 || strings.ContainsAny(p, " \t\r\n\"'") {
		return errors.New(what + "的参数名无效")
	}
	return nil
}

// validateRecipe: everything a recipe says is of the kind and the size it may be. A file from somebody else
// is held to this before anything of it is used; a recipe saved here is too, so what is exported can be read back.
func validateRecipe(r *Recipe) error {
	if r.Format != recipeFormat {
		return errors.New("不是 MioVRCA 装配方案文件")
	}
	if r.Schema > recipeSchema {
		return errors.New("该方案由更新版本的软件导出，请先更新 MioVRCA 后再导入")
	}
	if r.Schema < 1 {
		return errors.New("方案文件的版本标记无效")
	}
	if strings.TrimSpace(r.Name) == "" {
		return errors.New("方案缺少名称")
	}
	for _, c := range []struct {
		s    string
		max  int
		what string
	}{{r.Name, recipeNameMax, "方案名称"}, {r.Note, recipeNoteMax, "方案备注"}, {r.App, 20, "软件版本"}, {r.Plugin, 20, "插件版本"}, {r.Hierarchy, 200, "菜单层级"}, {r.Base.Name, recipeLabelMax, "素体名称"}} {
		if err := cleanText(c.s, c.max, c.what); err != nil {
			return err
		}
	}
	if strings.ContainsAny(r.Name, "\n\t") {
		return errors.New("方案名称含有无法显示的字符")
	}
	if r.Created < 0 || r.Created > 1<<40 {
		return errors.New("方案的创建时间无效")
	}
	if r.Base.Prefab != "" {
		if err := assetPath(r.Base.Prefab, ".prefab", "素体 prefab "); err != nil {
			return err
		}
	}
	if len(r.Assets) > recipeAssetsMax {
		return fmt.Errorf("方案中的素材过多（最多 %d 个）", recipeAssetsMax)
	}
	if len(r.Assets) == 0 && len(r.Menus) == 0 {
		return errors.New("方案中没有素材，也没有菜单")
	}
	seen := map[string]bool{}
	for i := range r.Assets {
		a := &r.Assets[i]
		what := fmt.Sprintf("第 %d 个素材", i+1)
		if err := assetPath(a.Prefab, ".prefab", what+"的 prefab "); err != nil {
			return err
		}
		if seen[strings.ToLower(a.Prefab)] {
			return errors.New(what + "与前面的素材重复")
		}
		seen[strings.ToLower(a.Prefab)] = true
		if strings.TrimSpace(a.Name) == "" {
			return errors.New(what + "缺少名称")
		}
		for _, c := range []struct {
			s    string
			max  int
			what string
		}{{a.Name, 120, what + "的名称"}, {a.Key, 200, what + "的素材库标识"}, {a.Version, recipeLabelMax, what + "的素体版本"}, {a.URL, 300, what + "的链接"}} {
			if err := cleanText(c.s, c.max, c.what); err != nil {
				return err
			}
		}
		if !core.ContainsStr(pipelineKinds, a.Kind) && a.Kind != "其他" {
			return errors.New(what + "的类别无效")
		}
		if a.BoothID != "" && !reShopID.MatchString(a.BoothID) {
			return errors.New(what + "的商品编号无效")
		}
		if a.URL != "" && !shopURL(a.URL) {
			return errors.New(what + "的链接不是可识别的商品页面")
		}
		if err := objectPath(a.Object, what+"的物体名"); err != nil {
			return err
		}
		if strings.Contains(a.Object, "/") {
			return errors.New(what + "的物体名无效")
		}
	}
	if len(r.Menus) > recipeMenusMax {
		return fmt.Errorf("方案中的菜单计划过多（最多 %d 个）", recipeMenusMax)
	}
	items := 0
	for mi := range r.Menus {
		m := &r.Menus[mi]
		if err := cleanText(m.Root, recipeLabelMax, "菜单根物体名"); err != nil {
			return err
		}
		if strings.ContainsAny(m.Root, "/\\\n\t") {
			return errors.New("菜单根物体名无效")
		}
		if err := paramName(m.Parameter, "菜单"); err != nil {
			return err
		}
		if len(m.Items) == 0 {
			return errors.New("方案中有空的菜单计划")
		}
		if items += len(m.Items); items > recipeItemsMax {
			return fmt.Errorf("方案中的菜单项过多（最多 %d 项）", recipeItemsMax)
		}
		for _, it := range m.Items {
			what := "菜单项「" + core.Truncate(it.Label, 40) + "」"
			if !core.ContainsStr(recipeItemKinds, it.Kind) {
				return errors.New(what + "的类型无效")
			}
			if strings.TrimSpace(it.Label) == "" {
				return errors.New("方案中有菜单项缺少名称")
			}
			if err := cleanText(it.Label, recipeLabelMax, "菜单项名称"); err != nil {
				return err
			}
			if strings.ContainsAny(it.Label, "\n\t") {
				return errors.New(what + "的名称无效")
			}
			if len(it.Path) > 6 {
				return errors.New(what + "的菜单层级过深")
			}
			for _, p := range it.Path {
				if err := cleanText(p, recipeLabelMax, what+"的子菜单名"); err != nil {
					return err
				}
				if strings.TrimSpace(p) == "" || strings.ContainsAny(p, "\n\t") {
					return errors.New(what + "的子菜单名无效")
				}
			}
			if len(it.Objects) > 64 || len(it.Materials) > 128 {
				return errors.New(what + "控制的物体过多")
			}
			for _, o := range it.Objects {
				if err := objectPath(o, what+"的物体"); err != nil {
					return err
				}
			}
			if err := paramName(it.Parameter, what); err != nil {
				return err
			}
			if it.MaterialsFrom != "" {
				if err := assetPath(it.MaterialsFrom, ".prefab", what+"的配色 prefab "); err != nil {
					return err
				}
			}
			for _, mt := range it.Materials {
				if err := objectPath(mt.Object, what+"的网格"); err != nil {
					return err
				}
				if mt.Slot < 0 || mt.Slot > 255 {
					return errors.New(what + "的材质槽序号无效")
				}
				if err := assetPath(mt.Material, ".mat", what+"的材质"); err != nil {
					return err
				}
			}
			if it.Kind != "strip" && it.Kind != "skin" && len(it.Objects) == 0 {
				return errors.New(what + "未指定所控制的物体")
			}
		}
	}
	return nil
}

var reShopID = regexp.MustCompile(`^(gr_)?[A-Za-z0-9_-]{1,40}$`)

// shopURL: a page of a shop the program knows (the link is shown to the player and opened when clicked).
func shopURL(u string) bool {
	p, err := url.Parse(u)
	if err != nil || p.Scheme != "https" || p.User != nil || p.Port() != "" {
		return false
	}
	h := strings.ToLower(p.Hostname())
	for _, d := range []string{"booth.pm", "gumroad.com"} {
		if h == d || strings.HasSuffix(h, "."+d) {
			return true
		}
	}
	return false
}

// ImportRecipe reads a recipe file somebody else made. Nothing of it is trusted: it has to be of the form
// and the size a recipe has, with no field this version does not know; what it says about this computer's
// library (the id, where it came from) is not taken from it. save=false only says what is in it.
func ImportRecipe(text []byte, save bool) (*Recipe, error) {
	if len(text) > RecipeMaxFile {
		return nil, fmt.Errorf("文件过大（超过 %d KB），不是有效的装配方案", RecipeMaxFile>>10)
	}
	text = bytes.TrimPrefix(text, []byte("\xef\xbb\xbf"))
	dec := json.NewDecoder(bytes.NewReader(text))
	dec.DisallowUnknownFields()
	var r Recipe
	if err := dec.Decode(&r); err != nil {
		if strings.Contains(err.Error(), "unknown field") {
			return nil, errors.New("文件中含有无法识别的内容，可能不是装配方案，或由更新版本的软件导出")
		}
		return nil, errors.New("无法读取该文件：不是有效的装配方案（JSON 格式有误）")
	}
	if dec.More() {
		return nil, errors.New("无法读取该文件：方案之后还有多余的内容")
	}
	if err := validateRecipe(&r); err != nil {
		return nil, err
	}
	r.ID, r.Imported = "", true
	r.Name = strings.Join(strings.Fields(r.Name), " ")
	for i := range r.Assets {
		if k := r.Assets[i].Key; k != "" && !portableKey(k) {
			r.Assets[i].Key = ""
		}
	}
	if !save {
		return &r, nil
	}
	r.ID = newRecipeID()
	if err := writeRecipe(&r); err != nil {
		return nil, err
	}
	return &r, nil
}

// portableKey: a library record that means the same on another computer — a shop item, or a name. (A
// record named after a folder of this computer's library does not.)
func portableKey(k string) bool {
	return strings.HasPrefix(k, "booth:") || strings.HasPrefix(k, "name:")
}

// exported: the recipe as it leaves this computer.
func (r *Recipe) exported() ([]byte, error) {
	c := *r
	c.ID, c.Imported = "", false
	c.Assets = append([]RecipeAsset{}, r.Assets...)
	for i := range c.Assets {
		if !portableKey(c.Assets[i].Key) {
			c.Assets[i].Key = ""
		}
	}
	return json.MarshalIndent(&c, "", "  ")
}

// ExportRecipe writes the recipe as one file into dir and says where.
func ExportRecipe(id, dir string) (string, error) {
	r, err := LoadRecipe(id)
	if err != nil {
		return "", err
	}
	b, err := r.exported()
	if err != nil {
		return "", err
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return "", errors.New("文件夹不存在：" + dir)
	}
	stem := core.SafeName(r.Name, 80)
	file := filepath.Join(dir, stem+RecipeExt)
	for n := 2; core.StatOK(file); n++ {
		file = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, n, RecipeExt))
	}
	if err := writeFileWhole(file, b); err != nil {
		return "", errors.New("导出失败（" + core.TrimErr(err) + "）")
	}
	return file, nil
}

// ---------- saving a run ----------

// RecipeReady: the project's session has something a recipe can be made of.
func RecipeReady(project string) bool {
	s := aiSessionFor(project)
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.busy && (len(s.rec.steps) > 0 || len(s.rec.menus) > 0)
}

// libraryRecord: what the library knows about one of its records, for a recipe.
type libraryRecord struct {
	key, name, boothID, url string
	bases                   []string
}

// libraryRecordOf: the record with this key (or this shop item). Caller holds no lock.
func libraryRecordOf(st *core.Store, key, boothID string) (libraryRecord, bool) {
	st.Mu.RLock()
	defer st.Mu.RUnlock()
	for _, a := range st.Assets {
		byKey := key != "" && (a.Key == key || a.AltKey == key)
		if !byKey && (boothID == "" || a.BoothID != boothID && a.Key != "booth:"+boothID) {
			continue
		}
		v := library.BuildView(st, a)
		if !byKey && v.BoothID != boothID {
			continue
		}
		rec := libraryRecord{key: a.Key, name: v.Name, boothID: v.BoothID, bases: v.Bases}
		switch {
		case v.Booth != nil && shopURL(v.Booth.URL):
			rec.url = v.Booth.URL
		case v.Purchase != nil && shopURL(v.Purchase.PageURL):
			rec.url = v.Purchase.PageURL
		case reBoothNo.MatchString(v.BoothID):
			rec.url = "https://booth.pm/ja/items/" + v.BoothID
		}
		if !reShopID.MatchString(rec.boothID) {
			rec.boothID = ""
		}
		return rec, true
	}
	return libraryRecord{}, false
}

var reBoothNo = regexp.MustCompile(`^[0-9]{5,9}$`)

// baseVersion: the base body a prefab's path names ("Assets/Dress/Kaguya/Dress.prefab" → Kaguya), and the
// folder above the one that names it, where the versions for other bodies sit beside it.
func baseVersion(st *core.Store, prefab string) (base, above string) {
	st.Mu.RLock()
	defs := naming.ParseBases(st.Settings.Bases)
	st.Mu.RUnlock()
	segs := strings.Split(strings.TrimSuffix(prefab, path.Ext(prefab)), "/")
	named := func(seg string) string {
		if bs := naming.DetectBases(seg, defs); len(bs) == 1 {
			return bs[0]
		}
		return ""
	}
	for i := len(segs) - 1; i >= 1; i-- { // the nearest folder that says so wins …
		if b := named(segs[i]); b != "" {
			for i > 1 && named(segs[i-1]) == b { // … and of "Kaguya/Kaguya prefab" the upper one is the version's folder
				i--
			}
			return b, strings.Join(segs[:i], "/")
		}
	}
	return "", ""
}

// SaveRecipe makes a recipe of what the project's last run did.
func SaveRecipe(st *core.Store, project, name, note string) (*Recipe, error) {
	name, err := tidyRecipeName(name)
	if err != nil {
		return nil, err
	}
	s := aiSessionFor(project)
	s.mu.Lock()
	busy, rec := s.busy, s.rec
	rec.steps = append([]recStep{}, s.rec.steps...)
	rec.menus = append([]recMenu{}, s.rec.menus...)
	s.mu.Unlock()
	if busy {
		return nil, errors.New("流水线仍在运行，请等待其完成后再保存")
	}
	if len(rec.steps) == 0 && len(rec.menus) == 0 {
		return nil, errors.New("当前工程还没有可保存的结果，请先运行一次流水线")
	}
	r := &Recipe{Format: recipeFormat, Schema: recipeSchema, ID: newRecipeID(), Name: name, Note: tidyRecipeNote(note), Created: time.Now().Unix(),
		App: core.AppVersion, Plugin: unity.EmbeddedPipelineVersion(), Hierarchy: rec.hierarchy, Assets: []RecipeAsset{}, Menus: []RecipeMenu{}}
	if a, ok := unity.ReadBridgeAlive(project); ok && a.Bridge != "" {
		r.Plugin = a.Bridge // the plugin that did the work
	}
	if strings.HasPrefix(rec.avatarPrefab, "Assets/") {
		r.Base.Prefab = rec.avatarPrefab
	}
	if al := baseAliases(st, avatarInfo{Name: rec.avatar, Prefab: rec.avatarPrefab}); len(al) > 0 {
		r.Base.Name = al[0]
	}
	rows := projectAssets(st, project)
	for _, x := range rec.steps {
		a := RecipeAsset{Prefab: x.Prefab, Object: x.Object, Place: x.Place, Active: x.Active, Name: strings.TrimSuffix(baseName(x.Prefab), ".prefab"),
			Kind: map[bool]string{true: "其他", false: "衣服"}[x.Place]}
		a.Version, _ = baseVersion(st, x.Prefab)
		// the row of the run it came from says what kind it is; the project's list, which record it belongs to
		for _, row := range rec.assets {
			if under(x.Prefab, row.Folder) {
				a.Kind = row.Kind
				if row.Name != "" {
					a.Name = row.Name
				}
				break
			}
		}
		for _, row := range rows {
			if under(x.Prefab, row.Folder) && row.Key != "" {
				a.Key = row.Key
				if lr, ok := libraryRecordOf(st, row.Key, ""); ok {
					a.Name, a.BoothID, a.URL = lr.name, lr.boothID, lr.url
				}
				break
			}
		}
		if r := []rune(a.Name); len(r) > 120 {
			a.Name = string(r[:120])
		}
		r.Assets = append(r.Assets, a)
	}
	for _, m := range rec.menus {
		r.Menus = append(r.Menus, m.RecipeMenu)
	}
	if err := validateRecipe(r); err != nil {
		return nil, errors.New("无法保存为方案：" + err.Error())
	}
	return r, writeRecipe(r)
}

// RecipeNeed: an asset a recipe asks for, with where to get it and whether this library has it.
type RecipeNeed struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Version string `json:"version,omitempty"`
	Link    string `json:"link,omitempty"`   // its shop page
	LibKey  string `json:"libKey,omitempty"` // the library's record of it, when there is one
}

// RecipeNeeds: 所需素材 — what somebody has to have to use the recipe.
func RecipeNeeds(st *core.Store, r *Recipe) []RecipeNeed {
	out := []RecipeNeed{}
	for _, a := range r.Assets {
		n := RecipeNeed{Name: a.Name, Kind: a.Kind, Version: a.Version, Link: a.link()}
		if lr, ok := libraryRecordOf(st, a.Key, a.BoothID); ok {
			n.LibKey = lr.key
			if n.Link == "" {
				n.Link = lr.url
			}
		}
		out = append(out, n)
	}
	return out
}

// link: the asset's shop page — the one the recipe names when it is a shop's, else the item's page on Booth.
func (a RecipeAsset) link() string {
	switch {
	case shopURL(a.URL):
		return a.URL
	case reBoothNo.MatchString(a.BoothID):
		return "https://booth.pm/ja/items/" + a.BoothID
	}
	return ""
}

// ---------- a recipe in another project ----------

// RecipeRow: one asset of a recipe, as it is found in a project.
type RecipeRow struct {
	RecipeAsset
	Status    string `json:"status"`             // dress: will be put on; worn: on the avatar already; missing: not in the project
	Resolved  string `json:"resolved,omitempty"` // its prefab in this project
	Via       string `json:"via,omitempty"`      // path: at the recipe's path; library: found through the library's record; version: this base body's version of it
	OtherBase string `json:"otherBase,omitempty"`
	LibKey    string `json:"libKey,omitempty"` // the library has it: it can be imported from there
	Link      string `json:"link,omitempty"`   // its shop page
}

type RecipePreview struct {
	Recipe   RecipeInfo  `json:"recipe"`
	Rows     []RecipeRow `json:"rows"`
	Alive    bool        `json:"alive"` // Unity answers: what is on the avatar is known
	Avatar   string      `json:"avatar,omitempty"`
	Notes    []string    `json:"notes"`
	Items    int         `json:"items"`   // menu items the recipe has
	Dropped  int         `json:"dropped"` // … of which those that need a missing asset
	CanApply bool        `json:"canApply"`
	Why      string      `json:"why,omitempty"`
}

// prefabsNamed: the prefabs under a folder of the project whose file is called so.
func prefabsNamed(project, folder, file string) []string {
	var out []string
	root := filepath.Join(project, filepath.FromSlash(folder))
	_ = filepath.WalkDir(root, func(fp string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.EqualFold(d.Name(), file) {
			return nil
		}
		rel, _ := filepath.Rel(project, fp)
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(out)
	return out
}

func hasAlias(p string, aliases []string) bool {
	low := strings.ToLower(p)
	for _, a := range aliases {
		if a != "" && strings.Contains(low, strings.ToLower(a)) {
			return true
		}
	}
	return false
}

// commonTail: how many path segments two paths share at their end.
func commonTail(a, b string) int {
	x, y := strings.Split(strings.ToLower(a), "/"), strings.Split(strings.ToLower(b), "/")
	n := 0
	for i, j := len(x)-1, len(y)-1; i >= 0 && j >= 0 && x[i] == y[j]; i, j = i-1, j-1 {
		n++
	}
	return n
}

// resolveRecipe finds each asset of the recipe in the project: at the path the recipe has; else in the
// folder the library ties to its record; else it is missing. With an avatar on another base body than the
// recipe's, the version of the outfit made for that body is taken when the package has one.
func resolveRecipe(st *core.Store, project string, r *Recipe, av *avatarInfo) []RecipeRow {
	var aliases []string
	if av != nil {
		aliases = baseAliases(st, *av)
	}
	exists := func(p string) bool { return core.FileExists(filepath.Join(project, filepath.FromSlash(p))) }
	rows := projectAssets(st, project)
	var out []RecipeRow
	for _, a := range r.Assets {
		row := RecipeRow{RecipeAsset: a, Status: "missing", Link: a.link()}
		lr, inLib := libraryRecordOf(st, a.Key, a.BoothID)
		if inLib {
			row.LibKey = lr.key
			if row.Link == "" {
				row.Link = lr.url
			}
		}
		file := baseName(a.Prefab)
		pickOf := func(cands []string) string {
			best, score := "", -1
			for _, c := range cands {
				sc := commonTail(c, a.Prefab) * 2
				if hasAlias(c, aliases) {
					sc += 100
				}
				if sc > score {
					best, score = c, sc
				}
			}
			return best
		}
		switch {
		case exists(a.Prefab):
			row.Resolved, row.Via = a.Prefab, "path"
			// another base body than the one this version is for: the package's version for it, if it has one
			if v, above := baseVersion(st, a.Prefab); v != "" && strings.Count(above, "/") >= 1 && len(aliases) > 0 && !core.ContainsStr(lowerAll(aliases), strings.ToLower(v)) {
				var mine []string
				for _, c := range prefabsNamed(project, above, file) {
					if hasAlias(strings.TrimPrefix(c, above), aliases) {
						mine = append(mine, c)
					}
				}
				if len(mine) > 0 {
					row.Resolved, row.Via = pickOf(mine), "version"
				}
			}
		case inLib:
			for _, pr := range rows {
				if pr.Key != lr.key {
					continue
				}
				if c := pickOf(prefabsNamed(project, pr.Folder, file)); c != "" {
					row.Resolved, row.Via = c, "library"
					break
				}
			}
		}
		if row.Resolved != "" {
			row.Status = "dress"
			if av != nil && onAvatar(*av, row.Resolved) {
				row.Status = "worn"
			}
			// made for another base body: by the folder it sits in, else by what the library says
			if len(aliases) > 0 {
				if v, _ := baseVersion(st, row.Resolved); v != "" && !core.ContainsStr(lowerAll(aliases), strings.ToLower(v)) {
					row.OtherBase = v
				} else if v == "" && inLib {
					if fits, known := baseFits(lr.bases, aliases); known && !fits {
						row.OtherBase = strings.Join(lr.bases, "、")
					}
				}
			}
		}
		out = append(out, row)
	}
	return out
}

func lowerAll(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = strings.ToLower(s)
	}
	return out
}

// sceneAvatars: the avatars of the open scene, the shown ones when some are.
func sceneAvatars(ctx context.Context, project string, wait time.Duration) ([]avatarInfo, error) {
	raw, err := unity.BridgeCall(ctx, project, "inspect", map[string]any{}, wait)
	if err != nil {
		return nil, err
	}
	var o struct {
		Avatars []avatarInfo `json:"avatars"`
	}
	_ = json.Unmarshal(raw, &o)
	var act []avatarInfo
	for _, a := range o.Avatars {
		if a.Active {
			act = append(act, a)
		}
	}
	if len(act) == 0 {
		act = o.Avatars
	}
	return act, nil
}

// paramOn: the parameter an outfit (or skin) item of a recipe's plan picks on with this avatar: the avatar's
// own outfit or hair parameter when it has such a menu already — the recipe's outfits join it, as the line's
// do — else the one the recipe has.
func paramOn(av *avatarInfo, m RecipeMenu, it RecipeItem) string {
	plan := m.Parameter
	if plan == "" {
		plan = "Clothtoggle"
	}
	p := it.Parameter
	switch {
	case it.Kind == "skin":
		if p == "" {
			p = skinParam
		}
		return p
	case p == "":
		p = plan
	}
	if av == nil {
		return p
	}
	if isHairParam(p) {
		if _, hp := hairMenu(*av); hp != "" {
			return hp
		}
	} else if p == plan {
		if _, tp := outfitMenu(*av); tp != "" {
			return tp
		}
	}
	return p
}

// planFor: the recipe's menu plans as they can be built on this avatar. Objects are called what dress
// called them here (names: the recipe's object → this scene's); an item whose objects are all of assets
// that are not there is left out, and counted.
func planFor(r *Recipe, av *avatarInfo, names map[string]string, gone map[string]bool, project string) (menus []RecipeMenu, dropped int, notes []string) {
	onAv := map[string]bool{}
	if av != nil {
		for _, c := range av.Children {
			onAv[c.Name] = true
		}
	}
	// the avatar wears something by default on a parameter already: a recipe does not take that away
	keepDefault := func(param string) bool { return av != nil && wearsByDefault(*av, param) }
	lost := 0
	for _, m := range r.Menus {
		out := RecipeMenu{Root: m.Root, Parameter: paramOn(av, m, RecipeItem{Kind: "outfit"})}
		if av != nil && out.Root == "" {
			out.Root, _ = outfitMenu(*av)
		}
		for _, it := range m.Items {
			it.Path = append([]string{}, it.Path...)
			var objs []string
			missing := false
			for _, o := range it.Objects {
				first, rest, _ := strings.Cut(o, "/")
				switch {
				case names[first] != "":
					first = names[first]
				case gone[first]:
					missing = true
					continue
				case av != nil && !onAv[first]: // something of the avatar the recipe was made on, not of this one
					lost++
					continue
				}
				if rest != "" {
					first += "/" + rest
				}
				objs = append(objs, first)
			}
			var mats []RecipeMaterial
			for _, mt := range it.Materials {
				first, _, _ := strings.Cut(mt.Object, "/")
				if av != nil && !onAv[first] || !core.FileExists(filepath.Join(project, filepath.FromSlash(mt.Material))) {
					missing = true
					continue
				}
				mats = append(mats, mt)
			}
			it.Objects, it.Materials = objs, mats
			if it.MaterialsFrom != "" && !core.FileExists(filepath.Join(project, filepath.FromSlash(it.MaterialsFrom))) {
				dropped++ // a colour whose prefab is not in the project
				continue
			}
			plain := it.Kind == "skin" && it.MaterialsFrom == "" && len(it.Materials) == 0 && !missing // the look the scene has
			if it.Kind != "strip" && !plain && len(it.Objects) == 0 && len(it.Materials) == 0 {
				dropped++
				continue
			}
			if it.Kind == "outfit" || it.Kind == "strip" || it.Kind == "skin" {
				p := paramOn(av, m, it)
				if it.Default && keepDefault(p) {
					it.Default = false
				}
				if it.Parameter = p; p == out.Parameter && it.Kind != "skin" {
					it.Parameter = "" // the plan's own
				}
			}
			out.Items = append(out.Items, it)
		}
		// what is left with nothing to choose between goes too: a skin menu with only the look the scene has,
		// "take everything off" with no outfit to take off
		picks := map[string]int{}
		for _, it := range out.Items {
			if it.Kind == "outfit" || it.Kind == "skin" && (it.MaterialsFrom != "" || len(it.Materials) > 0) {
				picks[it.Kind+paramOn(av, m, it)]++
			}
		}
		kept := out.Items[:0]
		for _, it := range out.Items {
			p := paramOn(av, m, it)
			alone := it.Kind == "skin" && picks["skin"+p] == 0 || it.Kind == "strip" && len(it.Objects) == 0 && picks["outfit"+p] == 0
			if alone && (av == nil || !paramInUse(*av, p)) {
				dropped++
				continue
			}
			kept = append(kept, it)
		}
		if out.Items = kept; len(out.Items) > 0 {
			menus = append(menus, out)
		}
	}
	if lost > 0 {
		notes = append(notes, fmt.Sprintf("方案的菜单中有 %d 处引用了原模型自带的物体，当前模型上没有同名物体，已略过", lost))
	}
	return menus, dropped, notes
}

// PreviewRecipe: what applying the recipe to the project would do, without doing any of it.
func PreviewRecipe(st *core.Store, id, project string) (*RecipePreview, error) {
	r, err := LoadRecipe(id)
	if err != nil {
		return nil, err
	}
	pv := &RecipePreview{Recipe: r.info(), Notes: []string{}}
	var av *avatarInfo
	if _, ok := unity.ReadBridgeAlive(project); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		avs, err := sceneAvatars(ctx, project, 20*time.Second)
		cancel()
		if err == nil {
			pv.Alive = true
			switch len(avs) {
			case 0:
				if r.Base.Prefab != "" && core.FileExists(filepath.Join(project, filepath.FromSlash(r.Base.Prefab))) {
					pv.Notes = append(pv.Notes, "当前场景中没有模型，应用时将先把方案的素体「"+strings.TrimSuffix(baseName(r.Base.Prefab), ".prefab")+"」放入新场景")
				} else {
					pv.Why = "当前场景中没有模型，方案的素体 prefab 也不在该工程中。请先在 Unity 中打开包含模型的场景"
				}
			case 1:
				av = &avs[0]
				pv.Avatar = av.Name
			default:
				pv.Why = fmt.Sprintf("场景中有 %d 个模型，请在 Unity 中仅保留一个处于显示状态", len(avs))
			}
		}
	}
	if !pv.Alive {
		pv.Why = "Unity 尚未连接：请先在「工程」步骤中打开 Unity。下方仅按工程中的文件判断素材是否已导入"
	}
	pv.Rows = resolveRecipe(st, project, r, av)
	names, gone := map[string]string{}, map[string]bool{}
	todo, other := 0, 0
	for _, row := range pv.Rows {
		switch row.Status {
		case "missing":
			gone[row.Object] = true
		default:
			names[row.Object] = row.Object
			todo++
		}
		if row.OtherBase != "" {
			other++
		}
	}
	for _, m := range r.Menus {
		pv.Items += len(m.Items)
	}
	_, pv.Dropped, _ = planFor(r, nil, names, gone, project)
	if av != nil && r.Base.Name != "" {
		if al := baseAliases(st, *av); len(al) > 0 && !core.ContainsStr(lowerAll(al), strings.ToLower(r.Base.Name)) {
			pv.Notes = append(pv.Notes, "该方案基于素体「"+r.Base.Name+"」制作，当前模型的素体为「"+al[0]+"」：仍可应用，衣服是否合身需在 Unity 中检查")
		}
	}
	if other > 0 {
		pv.Notes = append(pv.Notes, fmt.Sprintf("有 %d 项素材是为其他素体制作的版本，仍会装配，可能不合身", other))
	}
	if av != nil {
		if root, param := outfitMenu(*av); param != "" {
			pv.Notes = append(pv.Notes, "模型已有衣服菜单「"+root+"」（参数 "+param+"），方案中的衣服将加入该菜单")
		}
	}
	if busy, _, _ := aiSessionFor(project).snapshot(); busy && pv.Why == "" {
		pv.Why = "流水线正在该工程中运行，请等待其完成"
	}
	if pv.Why == "" && todo == 0 {
		pv.Why = "方案中的素材均未导入该工程，暂无可装配的内容"
	}
	pv.CanApply = pv.Why == ""
	return pv, nil
}

// StartRecipeRun applies a recipe to the project: dress and build_menu with the plan that was saved, no AI.
// It is a run of the line like any other — its steps show in the record, each can be undone.
func StartRecipeRun(st *core.Store, project, id string) error {
	r, err := LoadRecipe(id)
	if err != nil {
		return err
	}
	s := aiSessionFor(project)
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return errors.New("上一个请求仍在进行，请等待其完成，或先点击「停止」")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.busy, s.cancel = true, cancel
	s.client, s.st, s.outImgs, s.done = nil, st, nil, runDone{}
	s.rec = runRecord{hierarchy: r.Hierarchy}
	for _, a := range r.Assets {
		s.rec.assets = append(s.rec.assets, aiAsset{Folder: a.Prefab, Kind: a.Kind, Name: a.Name})
	}
	s.mu.Unlock()
	s.add(AIStep{Kind: "user", Text: "应用装配方案「" + r.Name + "」"})
	go func() {
		defer func() {
			if x := recover(); x != nil {
				s.add(AIStep{Kind: "error", Text: fmt.Sprint("程序内部错误：", x)})
			}
			cancel()
			s.mu.Lock()
			s.busy, s.cancel = false, nil
			s.mu.Unlock()
		}()
		if err := applyRecipe(ctx, st, s, r); err != nil {
			s.finishStep(false, "已中断")
			s.add(AIStep{Kind: "error", Text: s.abortText(err)})
		}
	}()
	return nil
}

func applyRecipe(ctx context.Context, st *core.Store, s *aiSession, r *Recipe) error {
	p := s.project
	step := func(tool, title string, f func() (string, error)) error {
		s.add(AIStep{Kind: "tool", Tool: tool, Text: title, Busy: true})
		out, err := f()
		if err != nil {
			s.finishStep(false, err.Error())
			return err
		}
		s.finishStep(true, out)
		return nil
	}
	if err := step("refresh", "刷新 Unity 资源以加载新导入的文件", func() (string, error) {
		_, err := unity.BridgeCall(ctx, p, "refresh", map[string]any{}, 10*time.Minute)
		return "", err
	}); err != nil {
		return err
	}
	var avs []avatarInfo
	var notes, warns []string
	if err := step("inspect_avatar", "查看模型和现有菜单", func() (string, error) {
		var err error
		if avs, err = sceneAvatars(ctx, p, 0); err != nil {
			return "", err
		}
		if len(avs) == 0 {
			return "场景中暂无模型", nil
		}
		names := []string{}
		for _, a := range avs {
			names = append(names, a.Name)
		}
		return "模型「" + strings.Join(names, "」「") + "」", nil
	}); err != nil {
		return err
	}
	if len(avs) == 0 {
		if r.Base.Prefab == "" || !core.FileExists(filepath.Join(p, filepath.FromSlash(r.Base.Prefab))) {
			return errors.New("当前场景中没有模型，方案的素体 prefab 也不在该工程中：请先在 Unity 中打开包含模型的场景")
		}
		if err := step("place_avatar", "将素体「"+strings.TrimSuffix(baseName(r.Base.Prefab), ".prefab")+"」放入新场景", func() (string, error) {
			raw, err := unity.BridgeCall(ctx, p, "place_avatar", map[string]any{"prefab": r.Base.Prefab}, 3*time.Minute)
			if err != nil {
				return "", err
			}
			var o struct {
				Scene string `json:"scene"`
			}
			_ = json.Unmarshal(raw, &o)
			notes = append(notes, "已新建场景 "+o.Scene+"，素体已放入并保存")
			if avs, err = sceneAvatars(ctx, p, 0); err != nil {
				return "", err
			}
			if len(avs) == 0 {
				return "", errors.New("素体已放入场景，但 Unity 未检测到模型，请查看 Console 是否有报错")
			}
			return "新场景 " + o.Scene, nil
		}); err != nil {
			return err
		}
	}
	if len(avs) > 1 {
		names := []string{}
		for _, a := range avs {
			names = append(names, a.Name)
		}
		return errors.New("场景中有 " + fmt.Sprint(len(avs)) + " 个模型（" + strings.Join(names, "、") + "）：请在 Unity 中仅保留一个处于显示状态后重试")
	}
	av := avs[0]
	s.recAvatar(av.Name, av.Prefab)
	rows := resolveRecipe(st, p, r, &av)

	// which objects must not be shown when they are newly put on: those of a default outfit, on an avatar
	// that wears another by default already
	hide := map[string]bool{}
	for _, m := range r.Menus {
		for _, it := range m.Items {
			if it.Kind == "outfit" && it.Default && wearsByDefault(av, paramOn(&av, m, it)) {
				for _, o := range it.Objects {
					first, _, _ := strings.Cut(o, "/")
					hide[first] = true
				}
			}
		}
	}

	names, gone := map[string]string{}, map[string]bool{}
	var dressed, worn, missing []string
	bones := 0
	for _, row := range rows {
		if row.Status == "missing" {
			gone[row.Object] = true
			missing = append(missing, row.Name)
			continue
		}
		if row.OtherBase != "" {
			warns = append(warns, "「"+row.Name+"」是为「"+row.OtherBase+"」制作的版本，可能不合身")
		}
		row := row
		var object string
		existing := false
		cmd, title := "dress", "装配「"+row.Name+"」"
		if row.Place {
			cmd, title = "place", "放置「"+row.Name+"」"
		}
		err := step(map[bool]string{true: "place_prefab", false: "dress"}[row.Place], title, func() (string, error) {
			args := map[string]any{"avatar": av.Path, "prefab": row.Resolved}
			if !row.Place && !onAvatar(av, row.Resolved) {
				active := row.Active == nil || *row.Active
				args["active"] = active && !hide[row.Object]
			}
			raw, err := unity.BridgeCall(ctx, p, cmd, args, 3*time.Minute)
			if err != nil {
				return "", oldPlugin(err)
			}
			var o struct {
				Existing  bool     `json:"existing"`
				Changed   *bool    `json:"changed"`
				Warnings  []string `json:"warnings"`
				PhysBones int      `json:"physBones"`
				Object    string   `json:"object"`
				Outfit    struct {
					Object string `json:"object"`
				} `json:"outfit"`
			}
			_ = json.Unmarshal(raw, &o)
			if object = o.Outfit.Object; row.Place {
				object = o.Object
			}
			existing, bones = o.Existing, max(bones, o.PhysBones)
			changed := o.Changed != nil && *o.Changed || o.Changed == nil && !o.Existing
			if changed {
				s.changed(false)
			}
			if !o.Existing {
				s.did(func(d *runDone) { d.dressed = append(d.dressed, row.Name) })
			}
			rec := recStep{Prefab: row.Resolved, Object: object, Place: row.Place}
			if v, ok := args["active"].(bool); ok {
				rec.Active = &v
			}
			s.recStep(rec, changed)
			out := map[bool]string{true: "已在模型上", false: map[bool]string{true: "已放置", false: "已装配"}[row.Place]}[o.Existing]
			if len(o.Warnings) > 0 {
				warns = append(warns, o.Warnings...)
				out += "。注意：" + strings.Join(o.Warnings, "；")
			}
			return out, nil
		})
		if err != nil {
			if row.Place && unitySaidNo(err) { // a plugin Unity will not take as it is: said, and the rest goes on
				gone[row.Object] = true
				warns = append(warns, "「"+row.Name+"」未放置："+err.Error())
				continue
			}
			return err
		}
		if object == "" {
			gone[row.Object] = true
			continue
		}
		names[row.Object] = object
		if existing {
			worn = append(worn, row.Name)
		} else {
			dressed = append(dressed, row.Name)
		}
	}

	// the avatar as it is now: what the plan may name, and the menus it has
	if now, err := sceneAvatars(ctx, p, 0); err == nil && len(now) == 1 {
		fresh := now[0]
		fresh.MaMenu = av.MaMenu // the menus as they were before this run decide what is a default
		av = fresh
	}
	menus, dropped, more := planFor(r, &av, names, gone, p)
	notes = append(notes, more...)
	bits, built := 0, 0
	var where []string
	for _, m := range menus {
		root, param := m.Root, m.Parameter
		var items []map[string]any
		for _, it := range m.Items {
			items = append(items, it.args())
		}
		items = localMenu("", av.MaMenu, root, items) // as the recipe names them, but by the names the menu has already
		title := fmt.Sprintf("生成菜单（%d 项）", len(items))
		if err := step("build_menu", title, func() (string, error) {
			args := map[string]any{"avatar": av.Path, "items": items}
			if param != "" {
				args["parameter"] = param
			}
			if root != "" && root != defaultMenuRoot {
				args["root"] = root
			}
			raw, err := unity.BridgeCall(ctx, p, "build_menu", args, 5*time.Minute)
			if err != nil {
				return "", oldPlugin(err)
			}
			var res struct {
				Created  []string `json:"created"`
				Warnings []string `json:"warnings"`
				Icons    int      `json:"icons"`
				Root     string   `json:"root"`
				Changed  *bool    `json:"changed"`
				Bits     int      `json:"parameterBits"`
			}
			_ = json.Unmarshal(raw, &res)
			changed := res.Changed == nil || *res.Changed
			if changed {
				s.changed(false)
				s.did(func(d *runDone) { d.menus++ })
			}
			s.recMenu(root, param, items, changed)
			warns = append(warns, res.Warnings...)
			where, bits, built = append(where, res.Root), res.Bits, built+len(items)
			out := fmt.Sprintf("新建 %d 项，图标 %d 张", len(res.Created), res.Icons)
			switch {
			case res.Changed != nil && !*res.Changed:
				out = "菜单已是最新，未作改动"
			case len(res.Created) == 0:
				out = "已更新现有菜单项，无新建项"
			}
			return out, nil
		}); err != nil {
			return err
		}
	}
	said := strings.Join(warns, "\n")
	if bones > budgetMax && !strings.Contains(said, "PhysBone") {
		warns = append(warns, fmt.Sprintf("模型上共有 %d 个 PhysBone，超过 %d 个的上限，上传时将校验失败（隐藏的物体同样计入）", bones, budgetMax))
	}
	if bits > budgetMax && !strings.Contains(said, "同步参数") {
		warns = append(warns, fmt.Sprintf("同步参数预计 %d / %d 位，超出上限将导致上传失败，可减少部件开关的数量", bits, budgetMax))
	}
	text := "已应用方案「" + r.Name + "」。"
	if len(dressed) > 0 {
		text += fmt.Sprintf("新装配 %d 项：%s。", len(dressed), strings.Join(dressed, "、"))
	}
	if len(worn) > 0 {
		text += fmt.Sprintf("已在模型上 %d 项：%s。", len(worn), strings.Join(worn, "、"))
	}
	if len(missing) > 0 {
		text += fmt.Sprintf("未导入、未装配 %d 项：%s。", len(missing), strings.Join(missing, "、"))
	}
	if built > 0 {
		text += fmt.Sprintf("菜单按方案生成 %d 项，位于模型下的「%s」。", built, strings.Join(core.UniqStrings(where), "」「"))
	} else {
		text += "未生成菜单。"
	}
	if dropped > 0 {
		text += fmt.Sprintf("方案中有 %d 个菜单项因所需素材缺失而未生成，导入缺失的素材后再次应用即可补全。", dropped)
	}
	if len(notes) > 0 {
		text += "\n" + strings.Join(core.UniqStrings(notes), "；") + "。"
	}
	if len(dressed)+len(worn) > 0 {
		s.add(AIStep{Kind: "tool", Tool: "look", Text: "为模型截图", Busy: true})
		if imgs, _, err := takeShots(ctx, p, map[string]any{"avatar": av.Path, "views": []string{"front", "back"}}); err != nil {
			s.finishStep(false, err.Error())
		} else {
			s.showImgs(saveShots(imgs))
			s.finishStep(true, "正面和背面，点击图片可放大。截图为场景当前显示的状态，菜单开关的效果需在 Play 模式中查看")
			text += "\n上方为模型当前的正面和背面截图，请检查是否有明显穿模、错位或整块洋红色（材质丢失）。"
		}
	}
	text += "\n菜单效果需在 Play 模式中用 Gesture Manager 逐项测试；确认无误后按 Ctrl+S 保存场景，如需回退，每点击一次「撤销上一步」撤销一步。"
	if len(warns) > 0 {
		text += "\n\n注意：" + strings.Join(core.UniqStrings(warns), "；")
	}
	s.add(AIStep{Kind: "say", Text: text})
	return nil
}
