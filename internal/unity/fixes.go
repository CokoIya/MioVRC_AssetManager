package unity

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vrclib/internal/core"
)

// One-click fixes from the check-up (plugin command "fix", kinds textures / lights / missing) and the way back
// for the one that bypasses Undo ("fix_revert"), then the check-up again so the window shows where it stands.
// The report card the page draws is kept under the data folder's reports/.

// FixKinds: what the plugin can put right by itself; everything else the check-up says stays advice.
var FixKinds = map[string]string{"textures": "贴图降到 2048", "lights": "移除灯光", "missing": "移除丢失的脚本"}

// What the player is told before a fix that reaches further than it looks (the panel's confirmation says the
// same): a texture's import settings belong to the project, and a "missing" script may only be a plugin that
// is not installed.
const (
	TextureFixWarn = "贴图的导入设置对整个工程生效，使用这些贴图的其他模型和场景也会随之改变"
	MissingFixWarn = "如果脚本丢失是因为插件（Modular Avatar、VRCFury 等）没有安装或编译出错，请不要移除，应先安装或修复该插件：移除后，这些组件上的设置（衣服的装配、菜单项等）在场景保存后无法找回，重新安装插件也不会恢复"
)

// IsFixUndo: the name of the undo step a one-click fix made (as Fixes.cs names them: 「MioVRCA 移除灯光」,
// 「MioVRCA 关闭灯光」, 「MioVRCA 移除丢失的脚本」), not one of the pipeline's own steps.
func IsFixUndo(name string) bool {
	return strings.HasPrefix(name, "MioVRCA 移除") || strings.HasPrefix(name, "MioVRCA 关闭")
}

// FixLine is one thing a fix touched: a path (a texture's in the project, an object's below the avatar) and
// what happened to it.
type FixLine struct {
	T         string   `json:"t,omitempty"`
	N         string   `json:"n,omitempty"`
	V         *float64 `json:"v,omitempty"`
	U         string   `json:"u,omitempty"`
	Path      string   `json:"path,omitempty"`
	Name      string   `json:"name,omitempty"`
	From      int      `json:"from,omitempty"`
	To        int      `json:"to,omitempty"`
	Platforms []string `json:"platforms,omitempty"` // whose larger overrides were lowered too
}

// FixResult is what the plugin answered.
type FixResult struct {
	Kind         string    `json:"kind"`
	Changed      int       `json:"changed"`
	Max          int       `json:"max,omitempty"`
	Items        []FixLine `json:"items"`
	Skipped      []FixLine `json:"skipped,omitempty"`
	Record       string    `json:"record,omitempty"` // the revert record, for textures
	MemoryBefore float64   `json:"memoryBefore,omitempty"`
	MemoryAfter  float64   `json:"memoryAfter,omitempty"`
	Undo         string    `json:"undo,omitempty"` // the undo step's name (lights, missing)
	At           int64     `json:"at"`
	// read from the record a texture fix left in the project, not from the fix's own answer (which never
	// arrived): what was changed is what the record names at most
	Adopted bool `json:"adopted,omitempty"`
}

// RevertResult is what fix_revert answered.
type RevertResult struct {
	Kind     string    `json:"kind"`
	Reverted int       `json:"reverted"`
	Items    []FixLine `json:"items"`
	Missing  []string  `json:"missing,omitempty"` // textures no longer at their path (moved, renamed, deleted): the record is kept
}

// RunFix has the plugin carry one fix out on the avatar the check-up measured, then measures again. The
// check-up record comes back with the fix; a check-up that fails after a fix that worked is said in recheck.
func RunFix(ctx context.Context, project, kind string, args map[string]any) (fix *FixResult, rec *CheckupRecord, recheck error, err error) {
	if _, ok := FixKinds[kind]; !ok {
		return nil, nil, nil, errors.New("没有这项一键处理：" + kind)
	}
	if err := CheckupReady(project); err != nil {
		return nil, nil, nil, err
	}
	a := map[string]any{"kind": kind}
	for k, v := range args {
		a[k] = v
	}
	raw, err := BridgeCall(ctx, project, "fix", a, 10*time.Minute)
	if err != nil {
		if kind == "textures" {
			// stopped, timed out or broken off half way: Unity may have done (part of) it all the same, and
			// its record is in the project by then
			keepTexFix(project, newestTexRecord(project))
		}
		return nil, nil, nil, fixPluginErr(err)
	}
	var f FixResult
	if json.Unmarshal(raw, &f) != nil || f.Kind == "" {
		return nil, nil, nil, errors.New("无法解析 Unity 返回的处理结果")
	}
	if f.Items == nil {
		f.Items = []FixLine{}
	}
	f.At = time.Now().Unix()
	rec, recheck = RunCheckup(ctx, project, argString(args, "avatar"))
	if f.Kind == "textures" && f.Record != "" {
		keepTexFix(project, &f)
		if rec != nil {
			rec = LastCheckup(project)
		}
	}
	return &f, rec, recheck, nil
}

// The texture fixes a project's 「恢复」 can take back are kept with its last check-up, the newest first:
// TexFix, then TexFixOlder.

const (
	texFixDir  = "UserSettings/MioVRCA/fixes" // where the plugin writes a texture fix's revert record
	texFixKeep = 20
)

func (r *CheckupRecord) texFixes() []*FixResult {
	var list []*FixResult
	for _, f := range append([]*FixResult{r.TexFix}, r.TexFixOlder...) {
		if f != nil && f.Record != "" {
			list = append(list, f)
		}
	}
	return list
}

func (r *CheckupRecord) setTexFixes(list []*FixResult) {
	r.TexFix, r.TexFixOlder = nil, nil
	if len(list) > texFixKeep {
		list = list[:texFixKeep]
	}
	if len(list) > 0 {
		r.TexFix = list[0]
	}
	if len(list) > 1 {
		r.TexFixOlder = list[1:]
	}
}

func (r *CheckupRecord) hasTexFix(record string) bool {
	for _, f := range r.texFixes() {
		if f.Record == record {
			return true
		}
	}
	return false
}

// changeTexFixes: the project's texture fixes without the one of this record and, with f, f in front.
func changeTexFixes(project, record string, f *FixResult) {
	checkMu.Lock()
	defer checkMu.Unlock()
	m := checkupsLocked()
	old := m[core.PathKey(project)]
	if old == nil || (f == nil && !old.hasTexFix(record)) {
		return // no check-up to keep it with (the fix's own answer still names its record), or nothing to drop
	}
	var list []*FixResult
	if f != nil {
		list = append(list, f)
	}
	for _, x := range old.texFixes() {
		if x.Record != record {
			list = append(list, x)
		}
	}
	// a copy: the record others hold is not changed under them
	rec := *old
	rec.setTexFixes(list)
	m[core.PathKey(project)] = &rec
	if err := saveCheckupsLocked(); err != nil {
		core.Logf("体检结果未能保存: %v", err)
	}
}

// keepTexFix: a texture fix that can be taken back becomes the one 「恢复」 is offered for; the ones before
// it wait behind it. (One read from the project's record gives way to the fix's own answer.)
func keepTexFix(project string, f *FixResult) {
	if f == nil || f.Record == "" {
		return
	}
	if f.Adopted {
		if last := LastCheckup(project); last == nil || last.hasTexFix(f.Record) {
			return
		}
	}
	changeTexFixes(project, f.Record, f)
}

// AdoptTexFix: a project whose last check-up has no texture fix to take back is looked at for a record one
// left behind (see RunCheckup), so the page offers 「恢复」 without another check-up. Cheap: a folder listing.
func AdoptTexFix(project string) {
	if last := LastCheckup(project); last != nil && last.TexFix == nil {
		keepTexFix(project, newestTexRecord(project))
	}
}

// dropTexFix: the record was spent (or is gone); the fix before it, if any, is offered next.
func dropTexFix(project, record string) { changeTexFixes(project, record, nil) }

// newestTexRecord: the newest revert record a texture fix left in the project (the plugin names them by the
// time), as far as the record says what the fix did. nil when there is none.
func newestTexRecord(project string) *FixResult {
	dir := filepath.Join(project, filepath.FromSlash(texFixDir))
	ents, err := os.ReadDir(dir) // (sorted by name)
	if err != nil {
		return nil
	}
	for i := len(ents) - 1; i >= 0; i-- {
		name := ents[i].Name()
		if ents[i].IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		if info, err := ents[i].Info(); err != nil || info.Size() > 8<<20 {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var d struct {
			Kind     string `json:"kind"`
			At       int64  `json:"at"`
			Max      int    `json:"max"`
			Textures []struct {
				Path string `json:"path"`
				Max  int    `json:"max"`
			} `json:"textures"`
		}
		if json.Unmarshal(b, &d) != nil || d.Kind != "textures" || len(d.Textures) == 0 {
			continue
		}
		f := &FixResult{Kind: "textures", Changed: len(d.Textures), Max: d.Max, Items: []FixLine{}, Record: texFixDir + "/" + name, At: d.At, Adopted: true}
		if f.At == 0 {
			if info, err := ents[i].Info(); err == nil {
				f.At = info.ModTime().Unix()
			}
		}
		for _, t := range d.Textures {
			if len(f.Items) == 60 {
				break
			}
			l := FixLine{Path: t.Path}
			if t.Max > d.Max && d.Max > 0 { // (a texture lowered only for a platform keeps its own Max Size)
				l.From, l.To = t.Max, d.Max
			}
			f.Items = append(f.Items, l)
		}
		return f
	}
	return nil
}

// RevertFix puts a texture fix's import settings back from its record, then measures again.
func RevertFix(ctx context.Context, project, record, avatar string) (rev *RevertResult, rec *CheckupRecord, recheck error, err error) {
	if err := CheckupReady(project); err != nil {
		return nil, nil, nil, err
	}
	raw, err := BridgeCall(ctx, project, "fix_revert", map[string]any{"record": record}, 10*time.Minute)
	if err != nil {
		if strings.Contains(err.Error(), "恢复记录已不存在") {
			dropTexFix(project, record)
		}
		return nil, nil, nil, fixPluginErr(err)
	}
	var r RevertResult
	if json.Unmarshal(raw, &r) != nil || r.Kind == "" {
		return nil, nil, nil, errors.New("无法解析 Unity 返回的恢复结果")
	}
	if len(r.Missing) == 0 { // (with a texture not found the plugin keeps the record: 「恢复」 stays for another go)
		dropTexFix(project, record)
	}
	if r.Items == nil {
		r.Items = []FixLine{}
	}
	rec, recheck = RunCheckup(ctx, project, avatar)
	return &r, rec, recheck, nil
}

func fixPluginErr(err error) error {
	if strings.Contains(err.Error(), "不认识的操作") {
		return errors.New("该工程的 Unity 插件为旧版，不支持一键处理：请在流水线页点击「更新」，待 Unity 编译完成后重试")
	}
	return err
}

func argString(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

// Text: the fix in words, for the AI and for a toast.
func (f *FixResult) Text() string {
	var b strings.Builder
	switch f.Kind {
	case "textures":
		fmt.Fprintf(&b, "已将 %d 张贴图的 Max Size 降到 %d", f.Changed, f.Max)
		if f.Changed > 0 && f.MemoryBefore > 0 {
			fmt.Fprintf(&b, "，所处理贴图的显存估计由 %s MB 降到 %s MB", trimNum(f.MemoryBefore), trimNum(f.MemoryAfter))
		}
		b.WriteString("。")
		for _, it := range f.Items {
			fmt.Fprintf(&b, "\n- %s：%d → %d", it.Path, it.From, it.To)
		}
		for _, it := range f.Skipped {
			fmt.Fprintf(&b, "\n- 未处理 %s：%s", it.T, it.N)
		}
		if f.Record != "" {
			b.WriteString("\n导入设置不在 Unity 的撤销记录中；玩家可在体检面板中点击「恢复」改回原设置（恢复记录：" + f.Record + "）")
		}
	case "lights":
		fmt.Fprintf(&b, "已移除 %d 个灯光组件。", f.Changed)
		for _, it := range f.Items {
			b.WriteString("\n- " + it.T)
		}
		if f.Undo != "" {
			b.WriteString("\n可用 Ctrl+Z 或「撤销上一步」撤销（" + f.Undo + "）")
		}
	case "missing":
		fmt.Fprintf(&b, "已移除 %d 个丢失脚本的组件。", f.Changed)
		for _, it := range f.Items {
			b.WriteString("\n- " + it.T)
			if it.V != nil {
				fmt.Fprintf(&b, "：%s %s", trimNum(*it.V), it.U)
			}
		}
		if f.Undo != "" {
			b.WriteString("\n可用 Ctrl+Z 或「撤销上一步」撤销（" + f.Undo + "）")
		}
	}
	return b.String()
}

// ---------- the report card ----------

const reportMax = 3 << 20 // a 2400×1260 PNG of the card is a few hundred KB; the request body allows 4 MB

// SaveReport writes the card the page drew (a PNG, base64) under the data folder's reports/, named after the
// avatar and the moment. The path is what the window offers to open.
func SaveReport(avatar string, png string) (string, error) {
	if len(png) > reportMax*4/3+64 {
		return "", errors.New("图片过大，未保存")
	}
	if i := strings.Index(png, ","); i >= 0 && strings.HasPrefix(png, "data:") {
		png = png[i+1:] // a data URL, as toDataURL gives it
	}
	b, err := base64.StdEncoding.DecodeString(png)
	if err != nil || !bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")) {
		return "", errors.New("图片数据无效，未保存")
	}
	if len(b) > reportMax {
		return "", errors.New("图片过大，未保存")
	}
	dir := filepath.Join(core.DataDir, "reports")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("无法创建 reports 文件夹：%v", err)
	}
	name := core.SafeName(avatar, 40)
	if strings.TrimSpace(name) == "" || name == "_" {
		name = "avatar"
	}
	stem := "checkup_" + name + "_" + time.Now().Format("20060102_1504")
	p := filepath.Join(dir, stem+".png")
	for n := 2; core.StatOK(p); n++ {
		p = filepath.Join(dir, fmt.Sprintf("%s_%d.png", stem, n))
	}
	if err := os.WriteFile(p, b, 0644); err != nil {
		return "", fmt.Errorf("无法保存图片：%v", err)
	}
	return p, nil
}
