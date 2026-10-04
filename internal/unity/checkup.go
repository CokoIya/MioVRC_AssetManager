package unity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"vrclib/internal/core"
)

// The check-up before an upload: the plugin measures the avatar in the open scene (command "checkup"), and
// the last answer for each project is kept in checkups.json for the project's card.

// CheckLine is one line of an item's details: a name or path, and a note or a number about it.
type CheckLine struct {
	T string   `json:"t"`
	N string   `json:"n,omitempty"`
	V *float64 `json:"v,omitempty"`
	U string   `json:"u,omitempty"`
	M string   `json:"m,omitempty"` // the material
	F string   `json:"f,omitempty"` // the shader it was probably made for
	W bool     `json:"w,omitempty"` // worth a second look (a texture of 4096 px)
	P string   `json:"p,omitempty"` // the asset's path in the project (a texture: what a fix is asked for by)
}

// CheckItem: a limit that stops the upload, something that looks wrong in the game, or a figure.
type CheckItem struct {
	ID           string      `json:"id"`
	Group        string      `json:"group"` // limit, look, figure
	Label        string      `json:"label"`
	Level        string      `json:"level"` // fail, warn, ok, info
	Value        *float64    `json:"value,omitempty"`
	Text         string      `json:"text,omitempty"` // shown in place of the value (a size, on / off)
	Unit         string      `json:"unit,omitempty"`
	Limit        float64     `json:"limit,omitempty"`   // what the upload allows at most
	AtLeast      bool        `json:"atLeast,omitempty"` // the value is a lower bound (VRCFury adds to it at build time)
	Rating       string      `json:"rating,omitempty"`  // the SDK's rank on PC: Excellent … VeryPoor
	RatingQuest  string      `json:"ratingQuest,omitempty"`
	Tiers        []float64   `json:"tiers,omitempty"` // what Excellent, Good, Medium and Poor allow at most on PC
	Total        int         `json:"total,omitempty"`
	Fixable      *int        `json:"fixable,omitempty"` // of the 4096 px textures: how many the one-click fix can lower
	Details      []CheckLine `json:"details,omitempty"`
	DetailsTitle string      `json:"detailsTitle,omitempty"`
	Advice       string      `json:"advice,omitempty"`
}

type Checkup struct {
	Avatar   string            `json:"avatar"`
	Path     string            `json:"path,omitempty"`
	Prebuild bool              `json:"prebuild"`         // measured before the build: the upload's figures differ
	Note     string            `json:"note,omitempty"`   // …said in words
	Target   string            `json:"target,omitempty"` // the editor's build target: pc or quest
	SDKCalc  bool              `json:"sdkCalc"`          // the figures are the SDK's own
	Ranks    map[string]string `json:"ranks,omitempty"`  // overall rank: pc, quest
	Tools    map[string]any    `json:"tools,omitempty"`  // what will change the avatar at build time
	Items    []CheckItem       `json:"items"`
	Ms       int               `json:"ms,omitempty"`
}

// CheckupRecord: the last check-up of a project.
type CheckupRecord struct {
	Project string  `json:"project"`
	At      int64   `json:"at"`
	Avatar  string  `json:"avatar"`
	Plugin  string  `json:"plugin,omitempty"` // the plugin version that measured
	Result  Checkup `json:"result"`
	// the last texture fix that can still be taken back (its record is in the project): the page offers
	// 「恢复」 from it after a reload or a restart as well, and for a fix the AI made
	TexFix *FixResult `json:"texFix,omitempty"`
	// the texture fixes before it that can still be taken back, the newest first: each is offered once the
	// one after it has been taken back
	TexFixOlder []*FixResult `json:"texFixOlder,omitempty"`
}

// CheckupBrief is what a project's card shows of it.
type CheckupBrief struct {
	At     int64  `json:"at"`
	Avatar string `json:"avatar,omitempty"`
	PC     string `json:"pc,omitempty"`
	Quest  string `json:"quest,omitempty"`
	Fail   int    `json:"fail"`
	Warn   int    `json:"warn"`
}

// Counts: how many items need handling, and how many are worth improving.
func (c *Checkup) Counts() (fail, warn int) {
	for _, it := range c.Items {
		switch it.Level {
		case "fail":
			fail++
		case "warn":
			warn++
		}
	}
	return
}

func (r *CheckupRecord) Brief() *CheckupBrief {
	b := &CheckupBrief{At: r.At, Avatar: r.Avatar, PC: r.Result.Ranks["pc"], Quest: r.Result.Ranks["quest"]}
	b.Fail, b.Warn = r.Result.Counts()
	return b
}

// ---------- the file ----------

var (
	checkMu  sync.Mutex
	checkDir string // the data folder the records were read from
	checkAll map[string]*CheckupRecord
)

func checkupFile() string { return filepath.Join(core.DataDir, "checkups.json") }

// checkupsLocked: the records, read once per data folder. Caller holds checkMu.
func checkupsLocked() map[string]*CheckupRecord {
	if checkAll != nil && checkDir == core.DataDir {
		return checkAll
	}
	var f struct {
		Projects map[string]*CheckupRecord `json:"projects"`
	}
	if b, err := os.ReadFile(checkupFile()); err == nil {
		_ = json.Unmarshal(b, &f)
	}
	if f.Projects == nil {
		f.Projects = map[string]*CheckupRecord{}
	}
	for k, r := range f.Projects {
		if r == nil {
			delete(f.Projects, k)
		}
	}
	checkAll, checkDir = f.Projects, core.DataDir
	return checkAll
}

func saveCheckupsLocked() error {
	b, _ := json.MarshalIndent(map[string]any{"version": 1, "projects": checkAll}, "", " ")
	tmp := checkupFile() + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, checkupFile()) // whole at every moment
}

// LastCheckup: the project's last check-up, nil when it has had none.
func LastCheckup(project string) *CheckupRecord {
	checkMu.Lock()
	defer checkMu.Unlock()
	return checkupsLocked()[core.PathKey(project)]
}

func lastCheckupBrief(project string) *CheckupBrief {
	if r := LastCheckup(project); r != nil {
		return r.Brief()
	}
	return nil
}

// ---------- asking Unity ----------

// AllCheckups: every project's last check-up, by project path.
func AllCheckups() map[string]*CheckupRecord {
	checkMu.Lock()
	defer checkMu.Unlock()
	out := map[string]*CheckupRecord{}
	for _, r := range checkupsLocked() {
		out[r.Project] = r
	}
	return out
}

// CheckupReady says what stands in the way before Unity is asked, in words that fit wherever the button is.
func CheckupReady(project string) error {
	a, alive := ReadBridgeAlive(project)
	switch {
	case !alive && !core.StatOK(filepath.Join(project, "Packages", PipePkg, "package.json")):
		return errors.New("该工程尚未安装 Unity 插件，请先在「流水线」页点击「安装并打开 Unity」")
	case !alive && !ProjectRunning(project):
		return errors.New("该工程尚未在 Unity 中打开，请先打开工程，待插件连接后再体检")
	case alive && a.Compiling:
		return errors.New("Unity 正在编译或导入，请待其完成后再体检")
	case alive && a.Playing:
		return errors.New("Unity 处于 Play 模式，请先退出 Play 模式再体检")
	}
	return nil
}

// RunCheckup has the plugin measure the avatar in the open scene, and keeps the answer as the project's
// last check-up. avatar may be empty (the avatar in the scene).
func RunCheckup(ctx context.Context, project, avatar string) (*CheckupRecord, error) {
	if err := CheckupReady(project); err != nil {
		return nil, err
	}
	raw, err := BridgeCall(ctx, project, "checkup", map[string]any{"avatar": avatar}, 2*time.Minute)
	if err != nil {
		if strings.Contains(err.Error(), "不认识的操作") {
			err = errors.New("该工程的 Unity 插件为旧版，不支持体检：请在流水线页点击「更新」，待 Unity 编译完成后重试")
		}
		return nil, err
	}
	var c Checkup
	if json.Unmarshal(raw, &c) != nil || c.Items == nil {
		return nil, errors.New("无法解析 Unity 返回的体检结果")
	}
	rec := &CheckupRecord{Project: project, At: time.Now().Unix(), Avatar: c.Avatar, Result: c}
	if a, ok := ReadBridgeAlive(project); ok {
		rec.Plugin = a.Bridge
	}
	left := newestTexRecord(project)
	checkMu.Lock()
	if old := checkupsLocked()[core.PathKey(project)]; old != nil {
		rec.TexFix, rec.TexFixOlder = old.TexFix, old.TexFixOlder
	}
	// a texture fix whose answer never arrived (the run was stopped, the program closed, the reimport broke
	// off half way) left its record in the project: 「恢复」 is offered from it all the same
	if left != nil && !rec.hasTexFix(left.Record) {
		rec.setTexFixes(append([]*FixResult{left}, rec.texFixes()...))
	}
	checkupsLocked()[core.PathKey(project)] = rec
	err = saveCheckupsLocked()
	checkMu.Unlock()
	if err != nil {
		core.Logf("体检结果未能保存: %v", err)
	}
	return rec, nil
}

// ---------- in words, for the AI ----------

var rankZh = map[string]string{"Excellent": "极佳", "Good": "良好", "Medium": "中等", "Poor": "较差", "VeryPoor": "极差"}

// RankZh: "较差（Poor）", as the window says it.
func RankZh(r string) string {
	if zh := rankZh[r]; zh != "" {
		en := r
		if r == "VeryPoor" {
			en = "Very Poor"
		}
		return zh + "（" + en + "）"
	}
	return r
}

func (it *CheckItem) valueText() string {
	s := it.Text
	if s == "" && it.Value != nil {
		s = trimNum(*it.Value)
	}
	if it.AtLeast {
		s = "至少 " + s
	}
	if it.Limit > 0 {
		s += " / " + trimNum(it.Limit)
	}
	if it.Unit != "" {
		s += " " + it.Unit
	}
	return s
}

func trimNum(f float64) string {
	if f == float64(int64(f)) {
		return fmt.Sprint(int64(f))
	}
	return fmt.Sprintf("%.1f", f)
}

func (l CheckLine) text() string {
	s := l.T
	if l.M != "" {
		s += "（材质 " + l.M + "）"
	}
	if l.N != "" {
		s += "：" + l.N
	}
	if l.V != nil {
		s += "：" + trimNum(*l.V) + " " + l.U
	}
	if l.F != "" {
		s += "，可能是 " + l.F
	}
	if l.P != "" {
		s += "（" + l.P + "）" // what checkup_fix takes
	}
	return s
}

// TextForAI: the check-up as the AI reads it — what needs handling in full, the figures in one line each.
// max bounds its length in bytes (the figures give way first).
func (r *CheckupRecord) TextForAI(max int) string {
	c := &r.Result
	var b strings.Builder
	fail, warn := c.Counts()
	fmt.Fprintf(&b, "模型「%s」的上传前体检：%d 项需处理，%d 项建议优化。", c.Avatar, fail, warn)
	if pc := c.Ranks["pc"]; pc != "" {
		b.WriteString("整体性能评级 PC " + RankZh(pc))
		if q := c.Ranks["quest"]; q != "" {
			b.WriteString("，Quest " + RankZh(q))
		}
		b.WriteString("。")
	}
	b.WriteString("\n以下是构建前的数值：上传时 Modular Avatar、Avatar Optimizer（AAO）、VRCFury 还会改动模型（AAO 通常会减少网格、材质槽和骨骼）。\n")
	section := func(title, level string, details int) {
		first := true
		for _, it := range c.Items {
			if it.Level != level {
				continue
			}
			if first {
				b.WriteString(title + "\n")
				first = false
			}
			b.WriteString("- " + it.Label + "：" + it.valueText())
			if it.Rating != "" {
				b.WriteString("，" + RankZh(it.Rating))
			}
			b.WriteString("\n")
			for i, l := range it.Details {
				if i >= details {
					fmt.Fprintf(&b, "  · 另有 %d 处\n", len(it.Details)-i)
					break
				}
				b.WriteString("  · " + l.text() + "\n")
			}
			if it.Advice != "" {
				b.WriteString("  建议：" + it.Advice + "\n")
			}
		}
	}
	section("【需处理】", "fail", 8)
	section("【建议优化】", "warn", 4)
	if fail+warn == 0 {
		b.WriteString("没有需要处理的项目。\n")
	}
	head := b.Len()
	b.WriteString("【其余数值】")
	n := 0
	for _, it := range c.Items {
		if it.Level == "fail" || it.Level == "warn" || (it.Value != nil && *it.Value == 0 && it.Limit == 0) {
			continue // said above, or nothing to say
		}
		if n > 0 {
			b.WriteString("；")
		}
		b.WriteString(it.Label + " " + it.valueText())
		if it.Rating != "" {
			b.WriteString("（" + it.Rating + "）")
		}
		n++
	}
	out := b.String()
	if len(out) > max && head < max {
		out = out[:head] // the figures are the part that can go
	}
	return string(Clip([]byte(out), max))
}

// CheckupsReload: checkups.json was replaced from outside (a library import or its undo); read it again at the next use.
func CheckupsReload() {
	checkMu.Lock()
	checkAll = nil
	checkMu.Unlock()
}
