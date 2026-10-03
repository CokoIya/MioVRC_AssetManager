package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/unity"
)

const aiMaxRounds = 40

type AIStep struct {
	Kind string   `json:"kind"` // user, say, tool, ask, error
	Text string   `json:"text"`
	Tool string   `json:"tool,omitempty"`
	OK   bool     `json:"ok,omitempty"`
	Out  string   `json:"out,omitempty"`  // what came back, in short
	Busy bool     `json:"busy,omitempty"` // the tool is still running
	Imgs []string `json:"imgs,omitempty"` // pictures taken in this step (names for /shot)
	At   int64    `json:"at"`
}

type aiSession struct {
	mu       sync.Mutex
	project  string
	busy     bool
	steps    []AIStep
	turns    []aiTurn
	cancel   context.CancelFunc
	changes  int       // scene changes made in this session (for 「撤销上一步」)
	askCh    chan bool // the player's answer to the question that is open
	allowAll bool      // the player said yes to every risky operation of this conversation
	client   *aiClient // the service of the run in progress (nil when the pipeline runs without an AI)
	st       *core.Store
	outImgs  []aiImage // pictures the tool call in progress hands to the AI
}

var (
	aiSessMu sync.Mutex
	aiSess   = map[string]*aiSession{}
)

func aiSessionFor(p string) *aiSession {
	aiSessMu.Lock()
	defer aiSessMu.Unlock()
	s := aiSess[core.PathKey(p)]
	if s == nil {
		s = &aiSession{project: p}
		aiSess[core.PathKey(p)] = s
	}
	return s
}

func (s *aiSession) add(st AIStep) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	st.At = time.Now().Unix()
	s.steps = append(s.steps, st)
	if len(s.steps) > 400 {
		s.steps = s.steps[len(s.steps)-400:]
	}
	return len(s.steps) - 1
}

func (s *aiSession) finishStep(ok bool, out string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.steps) - 1; i >= 0; i-- {
		if s.steps[i].Kind == "tool" && s.steps[i].Busy {
			s.steps[i].Busy, s.steps[i].OK, s.steps[i].Out = false, ok, out
			return
		}
	}
}

// confirm puts a question to the player ("the AI wants to delete …") and waits for the answer.
func (s *aiSession) confirm(ctx context.Context, text string) bool {
	ch := make(chan bool, 1)
	s.mu.Lock()
	s.askCh = ch
	s.mu.Unlock()
	s.add(AIStep{Kind: "ask", Text: text, Busy: true})
	ok := false
	select {
	case ok = <-ch:
	case <-ctx.Done():
	case <-time.After(15 * time.Minute):
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.askCh = nil
	for i := len(s.steps) - 1; i >= 0; i-- {
		if s.steps[i].Kind == "ask" && s.steps[i].Busy {
			s.steps[i].Busy, s.steps[i].OK = false, ok
			s.steps[i].Out = map[bool]string{true: "你同意了", false: "没有执行"}[ok]
			break
		}
	}
	return ok
}

func (s *aiSession) answer(ok, all bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.askCh == nil {
		return false
	}
	if ok && all {
		s.allowAll = true
	}
	select {
	case s.askCh <- ok:
	default:
	}
	return true
}

// skillInfo: what UnitySkills says about one of its skills (GET /skills?full=1).
type skillInfo struct {
	Name          string   `json:"name"`
	Operation     []string `json:"operation"`
	ReadOnly      bool     `json:"readOnly"`
	MutatesAssets bool     `json:"mutatesAssets"`
	MayReload     bool     `json:"mayTriggerReload"`
	MayPlay       bool     `json:"mayEnterPlayMode"`
	Risk          string   `json:"riskLevel"`
}

var (
	skillsMu    sync.Mutex
	skillsCache = map[string]map[string]skillInfo{} // project and port → skills by name
)

// skillsManifest: every skill of the project's UnitySkills with its risk notes, read once per server.
func skillsManifest(ctx context.Context, p string) map[string]skillInfo {
	key := fmt.Sprint(core.PathKey(p), "|", unity.SkillsPort(p))
	skillsMu.Lock()
	m, ok := skillsCache[key]
	skillsMu.Unlock()
	if ok {
		return m
	}
	b, status, err := unity.SkillsRequest(ctx, p, "GET", "/skills?full=1", nil)
	var r struct {
		Skills []skillInfo `json:"skills"`
	}
	if err != nil || status != 200 || json.Unmarshal(b, &r) != nil || len(r.Skills) == 0 {
		return nil
	}
	m = map[string]skillInfo{}
	for _, k := range r.Skills {
		m[k.Name] = k
	}
	skillsMu.Lock()
	skillsCache[key] = m
	skillsMu.Unlock()
	return m
}

// consentReason: why the player is asked before a UnitySkills operation runs ("" = it just runs). What only
// looks, and what changes the scene in a way Ctrl+Z takes back, runs; what deletes, writes files of the
// project, runs commands or code, reloads scripts or enters Play mode is asked about first — as is anything
// UnitySkills gives no risk notes for.
func consentReason(m map[string]skillInfo, name string) string {
	if m == nil {
		return "读不到 UnitySkills 的操作清单，没法判断它安不安全"
	}
	k, ok := m[name]
	if !ok {
		return "" // not a skill: UnitySkills will say so
	}
	if k.ReadOnly {
		return ""
	}
	var why []string
	for _, op := range k.Operation {
		switch op {
		case "Delete":
			why = append(why, "会删除东西")
		case "Execute":
			why = append(why, "会执行编辑器命令")
		}
	}
	if k.MutatesAssets {
		why = append(why, "会改动工程里的文件，Ctrl+Z 撤销不了")
	}
	if k.MayPlay {
		why = append(why, "会进入或退出 Play 模式")
	}
	if k.MayReload {
		why = append(why, "会让 Unity 重新编译")
	}
	switch {
	case k.Risk == "": // an older UnitySkills without risk notes
		if riskySkill(name) || len(k.Operation) == 0 {
			why = append(why, "这个版本的 UnitySkills 没有说明它的风险")
		}
	case k.Risk != "low":
		why = append(why, "UnitySkills 标的风险等级是 "+k.Risk)
	}
	if len(why) == 0 && riskySkill(name) {
		why = append(why, "从名字看可能删除、保存或覆盖东西")
	}
	return strings.Join(core.UniqStrings(why), "；")
}

// riskySkill: names that delete, save, load another scene (dropping unsaved changes), run code or menu
// commands, change packages or enter Play mode.
func riskySkill(name string) bool {
	n := strings.ToLower(name)
	for _, w := range []string{"delete", "remove", "destroy", "clear", "clean", "save", "scene_load", "scene_new", "scene_create", "scene_unload",
		"script", "package", "execute", "play", "build", "revert", "reset", "overwrite", "replace", "import_settings", "reimport", "move", "rename", "apply", "invoke", "define"} {
		if strings.Contains(n, w) {
			return true
		}
	}
	return false
}

func (s *aiSession) snapshot() (bool, []AIStep, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.busy, append([]AIStep{}, s.steps...), s.changes
}

func (s *aiSession) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy {
		return
	}
	s.steps, s.turns, s.changes, s.allowAll = nil, nil, 0, false
}

func (s *aiSession) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

// ---------- what the AI is told ----------

func aiSystemPrompt(project, hierarchy string) string {
	if strings.TrimSpace(hierarchy) == "" {
		hierarchy = defaultHierarchy
	}
	return strings.NewReplacer("{project}", path.Base(strings.ReplaceAll(project, `\`, "/")), "{hierarchy}", hierarchy).Replace(aiPromptText)
}

const aiPromptText = `你是「MioVRCA」里的 AI 改模助手，帮玩家在 Unity 里给 VRChat 头像装素材——衣服、头发、配饰、道具，必要时先把素体放进场景——并做好菜单开关。玩家多半不熟悉 Unity：你用工具把事情做完，再用简短的中文说明结果。

# 工作方式
- 你通过工具操作玩家电脑上开着的 Unity 工程「{project}」。动手之前先用 inspect_avatar 看清现状。
- 所有改动都能在 Unity 里按 Ctrl+Z 撤销。你不保存场景，也不要调用任何保存场景的操作（place_avatar 新建并保存一个新场景是唯一的例外）；玩家检查满意后自己按 Ctrl+S。
- 只做玩家要求的事。不删除、不改名玩家已有的物体、菜单和资产，不改厂商原包里的文件。
- 物体路径、prefab 路径必须来自工具返回的结果，不要自己编。
- 工具报错时先读懂错误，改好参数再试；同一个错误不要原样重试两次以上。确实做不到就如实说明卡在哪一步、玩家可以怎么办。
- 需要玩家做决定时（例如找不到给这个素体的版本、场景里有好几个头像），停下来问，不要猜。

# 素材的类别决定做法
- 素体：整只头像。场景里没有头像时，用 list_prefabs 在素体文件夹里找 wholeAvatar=true 的 prefab，用 place_avatar 放进新场景，再 inspect_avatar。场景里已经有头像就不要再放，告诉玩家。
- 衣服：dress 穿上；菜单里做互斥的 outfit 项（参数 Clothtoggle），可以按部件做 part 开关。
- 头发：dress 戴上；菜单里也是互斥的 outfit 项，但 parameter 写 Hair_Choose（和衣服分开互斥）。素体自带的头发还在头像上时（inspect_avatar 的 children 里 kind=mesh、名字带 Hair 的那个），也给它一个 outfit 项（objects 写它、不用 dress），这样换外装头发时能把它关掉。单色头发直接在「头发」这一层做一个 outfit 项；多配色的做一层子菜单，里面放配色项和发饰的 part 开关。
- 配饰：dress 戴上；做 toggle 项，default=true（默认显示；选中=显示，取消=隐藏）。
- 道具：dress 装上（active=false）；做 toggle 项，default=false（默认隐藏，选中才显示）；每个道具一层子菜单，里面的开关叫「显示」。

# 穿戴：dress
- 先用 list_prefabs 看文件夹里有哪些 prefab，再挑要穿的：
  - wholeAvatar=true 是带素体的整只模型，不能当衣服穿。
  - 一件衣服常有给不同素体的版本。头像的名字和 prefab 路径里能看出素体名，选路径或名字里带这个素体名的版本；找不到对应版本时告诉玩家这件衣服可能不是给这个素体做的，问清楚再继续。
  - modularAvatarReady=true 的已经配好 Modular Avatar；没有配好的也能穿，工具会自动对骨架。
  - 同一件衣服的不同配色常常是几个 prefab（网格名相同、材质不同）。只穿其中一个，其余配色在菜单里用 materialsFrom 做成换色项，不要把每个配色都穿上去。
- 文件夹和 prefab 都写工程里的路径，以 Assets/ 开头。
- dress 的结果里已经带着这件衣服的网格列表（outfit.meshes），可以直接拿来分部件，不用再查一次。
- 结果里 setUp=false 或有 warnings 时，如实转告玩家。

# 做菜单：build_menu
菜单用 Modular Avatar 的菜单项搭出来。items 里每一项是一个开关：
- kind="outfit"：穿这件。同一个 parameter 上的 outfit 互斥——选一件，其余自动脱下。objects 写这件的根物体。default=true 表示进游戏默认穿它，每个参数最多一个。
  - 换色项也是 outfit：objects 仍写已经穿上的那件的根物体，再加 materialsFrom=另一个配色的 prefab 路径。
- kind="part"：衣服上的一组部件（外套、鞋子、帽子…），选中=隐藏。objects 写这一组网格物体的路径，来自 inspect_object。一个开关管一组会一起出现的网格，例如「外套」包括外套本体和它的袖章、里衬。
- kind="toggle"：独立的开关（配饰、道具），选中=显示。objects 写它的根物体；default 写进游戏时显不显示。
- kind="strip"：一键脱光，关掉这个参数上的所有衣服。玩家要求时才加，放在衣服那一层的最后。
- path 是这一项所在的子菜单，从主菜单往下写，不含「主菜单」；label 是这一项显示的名字。

菜单层级按玩家给的模板排：{hierarchy}
- 「{分类}」是素材类别那一层，名字就是类别：衣服、头发、配饰、道具。模板里直接写着「衣服」这样的名字时，别的类别换成自己的名字（头发放「头发」里，道具放「道具」里）。
- 「{素材}」（也写作「{衣服}」）表示每个素材自己的一层，名字用它的简短中文名（例如「水手服」）。这一层里先放 outfit 项（只有一个配色时 label 写「穿上」，有几个配色时每个配色一项、label 写配色名），再放它的 part 项。
- 「{开关}」指的就是这些开关项本身，不是一层菜单。
- 模板里没有「{素材}」这一层时，每个素材只在类别那一层做一个项，label 用素材名，不做部件开关。
- 玩家用自己的话描述层级时，按他的意思排。

其他规矩：
- 每个菜单最多 8 项。部件太多就合并成更大的组（各种小饰品合成「饰品」），配色太多就把配色单独收进一层「配色」。
- label 用简短中文：外套、上衣、裙子、裤子、袜子、鞋子、帽子、头饰、饰品、尾巴、翅膀、包。不要把物体的内部名字直接当菜单名。
- 分部件之前先用 inspect_object 看这件衣服有哪些网格，按名字、材质名和高度判断是什么（height=[最低, 最高]，单位米，脚在 0 附近）。大件不要漏；拿不准的小零件归到「饰品」；衣服的主体（没有它就不成一件衣服的那部分）不用做开关。
- 头像已经有衣服菜单时（inspect_avatar 的 maMenu 里有几个 Toggle 共用同一个参数并且带 toggles），沿用它：root 填那个菜单的根物体，parameter 填它的参数名，把新衣服加进去，不要另起一套。头发菜单同理（参数名里带 hair 的那个）。
- 头像上原本没有衣服菜单时，把这次穿的第一件设成 default。头像原本有默认衣服时不要抢它的默认，新衣服用 dress 的 active=false 穿上。头发也一样，按 Hair_Choose 算。
- build_menu 可以重复调用：同一位置同名的项会更新，不会重复创建。计划里有一处写错，整个计划都不会执行，改好再调一次。
- 同步参数一共 256 位：每个 part / toggle 开关占 1 位，一个互斥参数占 8 位。结果里有 warnings 就转告玩家。

# 看一眼：look
- look 给头像拍照，截图同时显示给玩家。装完素材、做完菜单之后拍一次正面和背面（views 写 front、back），确认：东西在该在的位置，没有明显穿模，没有整块洋红色（材质丢失）或纯白（贴图没挂上）。要看细节时用 target 只拍那一件，或者加 face、left、right 这些角度。
- 拍照是为了确认结果，不用每一步都拍；同一处最多拍两三次。
- 编辑模式下拍到的是场景里现在显示着的东西，菜单开关的效果没有算进去。头像上同时放着几件互斥的衣服或头发时，用 hide 把别的临时藏起来再看这一件；默认隐藏的衣服、道具用 show 临时显示。show / hide 只在拍照时生效，不改场景。
- 正面图里，头像的左手在画面右边。
- 只说你确实看到的东西（截图，或看图模型的描述）。结果里写着你没有看到画面时，不要描述画面，也不要说「看起来没问题」：如实告诉玩家这次你看不了图，请他自己看记录里的截图，并转告结果里说的设置办法。
- 看出问题（穿模、错位、材质丢失）时，告诉玩家是哪一件、在什么位置；工具能修的再修，修不了的不要硬改。骨骼物理和菜单开关的动态效果截图里看不出来，仍然要玩家在 Play 模式里确认。
- 要看的是 Game 视图或 Scene 视图本身（而不是头像）时，可以用 UnitySkills 的 scene_screenshot、camera_sceneview_screenshot、camera_screenshot，它们的图同样会交给你看；它们会在工程的 Assets/Screenshots 里留下图片文件，所以看头像优先用 look。

# 其他改模操作：UnitySkills
- 玩家要的事情超出上面几个工具时，用 unity_find_skills 按意图找操作，看清参数再用 unity_skill 调用。
- 先查后改。会删除东西、保存场景、改动工程里的文件、进入 Play 模式或批量改导入设置的操作，先向玩家说明要做什么、为什么。这类操作执行前软件还会让玩家点一次确认；玩家没同意时不要换办法绕过去。
- 返回 MODE_FORBIDDEN 表示玩家的 UnitySkills 权限模式不允许这个操作：告诉玩家可以在 Unity 的 Window > UnitySkills 面板里调整权限模式，不要换别的办法绕过。
- 不要对头像做采样动画（SampleAnimation）一类的操作，会把人形骨架压坏。

# 收尾
做完后用几句话说明：装了什么、菜单是什么样、从截图里看到了什么（没看图就说没看）、有哪些警告要注意。最后提醒玩家：菜单效果要在 Unity 的 Play 模式里用 Gesture Manager 点一遍；满意就按 Ctrl+S 保存场景，不满意按 Ctrl+Z 或点「撤销上一步」。`

func obj(props map[string]any, required ...string) map[string]any {
	o := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		o["required"] = required
	}
	return o
}

func prop(typ, desc string) map[string]any { return map[string]any{"type": typ, "description": desc} }

func strList(desc string) map[string]any {
	return map[string]any{"type": "array", "description": desc, "items": map[string]any{"type": "string"}}
}

const avatarArg = "头像的名字。场景里只有一个头像时不用写"

var aiTools = []aiTool{
	{Name: "inspect_avatar", Desc: "看 Unity 里打开的场景：有哪些头像，头像下面有哪些物体（衣服、网格、菜单），现有的菜单和参数是什么样。不改任何东西。",
		Params: obj(map[string]any{"avatar": prop("string", avatarArg)})},
	{Name: "inspect_object", Desc: "看头像下面某个物体（一般是一件衣服）里的每个网格：路径、是否显示、材质名、离地高度范围、面数。用它来决定部件怎么分组。不改任何东西。",
		Params: obj(map[string]any{"path": prop("string", "物体在头像下面的路径，例如 Sailor 或 Sailor/Jacket"), "avatar": prop("string", avatarArg)}, "path")},
	{Name: "list_prefabs", Desc: "列出工程里某些文件夹下的 prefab：路径、网格名、是不是整只模型、有没有配好 Modular Avatar。",
		Params: obj(map[string]any{"folders": strList("文件夹，例如 Assets/店铺名/衣服名"), "limit": prop("integer", "最多列多少个，默认 60")}, "folders")},
	{Name: "dress", Desc: "把一个衣服 prefab 穿到头像上：放到头像下面，用 Modular Avatar 配好骨架。同一个 prefab 已经在头像上时直接用现有的。可以用 Ctrl+Z 撤销。",
		Params: obj(map[string]any{"prefab": prop("string", "prefab 的路径，来自 list_prefabs"), "name": prop("string", "穿上后物体叫什么，不写就用 prefab 的名字"),
			"active": prop("boolean", "穿上后在场景里显示还是隐藏。不写就是显示"), "avatar": prop("string", avatarArg)}, "prefab")},
	{Name: "build_menu", Desc: "按计划生成或更新头像的菜单开关（Modular Avatar），并给每一项拍图标。可以用 Ctrl+Z 撤销。",
		Params: obj(map[string]any{
			"items": map[string]any{"type": "array", "description": "菜单项，按显示顺序写", "items": obj(map[string]any{
				"kind":          map[string]any{"type": "string", "enum": []string{"outfit", "part", "toggle", "strip"}, "description": "outfit=穿这件（同一参数上互斥：衣服用 Clothtoggle，头发用 Hair_Choose）；part=隐藏一组部件；toggle=独立开关（配饰、道具，选中=显示）；strip=一键脱光"},
				"path":          strList("这一项所在的子菜单，从主菜单往下，例如 [\"衣服\", \"水手服\"]"),
				"label":         prop("string", "这一项显示的名字"),
				"objects":       strList("它管的物体在头像下面的路径。outfit 写衣服的根物体，part 写一组网格，toggle 写配饰或道具的根物体，strip 不用写"),
				"default":       prop("boolean", "outfit：进游戏默认穿这件（每个参数最多一个）；toggle：进游戏默认显示（配饰 true，道具 false）"),
				"show":          prop("boolean", "只用于 part：true 表示选中时显示（默认是选中时隐藏）"),
				"parameter":     prop("string", "只用于 outfit/strip：互斥用的参数名。不写就用整个计划的 parameter；头发写 Hair_Choose"),
				"materialsFrom": prop("string", "只用于 outfit 的换色项：另一个配色的 prefab 路径"),
			}, "kind", "path", "label")},
			"root":      prop("string", "菜单的根物体名。不写就用（或新建）Avatar Menu；头像已有衣服菜单时写它的根物体"),
			"parameter": prop("string", "outfit 默认共用的参数名。不写就用 Clothtoggle；头像已有衣服菜单时写它的参数名"),
			"icons":     prop("boolean", "要不要拍图标，默认要"),
			"avatar":    prop("string", avatarArg),
		}, "items")},
	{Name: "place_avatar", Desc: "新建一个场景，把一个素体（整只头像的 prefab）放进去并保存为 Assets/<工程名>/<工程名>.unity。只在场景里还没有头像时用。当前场景有没保存的改动时会拒绝。",
		Params: obj(map[string]any{"prefab": prop("string", "素体的 prefab 路径，来自 list_prefabs（wholeAvatar=true 的那个）"), "name": prop("string", "场景文件名，不写就用工程名")}, "prefab")},
	{Name: "look", Desc: "给头像拍照并看一眼：衣服穿得对不对、有没有穿模、材质有没有丢（整块洋红色）、东西在不在该在的位置。相机自动对准头像，只拍头像本身，截图同时显示给玩家。不改场景，也不在工程里留文件。",
		Params: obj(map[string]any{
			"views": map[string]any{"type": "array", "description": "拍哪几个角度，最多 4 个；不写就只拍正面", "items": map[string]any{"type": "string",
				"enum": []string{"front", "back", "left", "right", "front_left", "front_right", "back_left", "back_right", "face"}}},
			"target":   prop("string", "只拍头像下面的某个物体（路径），例如刚穿上的衣服的根物体。不写就拍整只头像"),
			"show":     strList("拍照时临时显示的物体路径（拍完恢复原状）：用来看默认隐藏的衣服、道具"),
			"hide":     strList("拍照时临时隐藏的物体路径（拍完恢复原状）：例如看新衣服时把原来那件藏起来"),
			"question": prop("string", "这次想确认什么，例如「外套有没有穿模」"),
			"avatar":   prop("string", avatarArg),
		})},
	{Name: "undo", Desc: "撤销 Unity 里的上一步改动（相当于按一次 Ctrl+Z）。", Params: obj(map[string]any{})},
	{Name: "unity_find_skills", Desc: "按意图查找 UnitySkills 提供的 Unity 编辑器操作，返回名字、说明和参数。意图用英文短语写更准，例如 \"set material color\"。",
		Params: obj(map[string]any{"intent": prop("string", "想做什么"), "top": prop("integer", "返回几个，默认 6")}, "intent")},
	{Name: "unity_skill", Desc: "调用一个 UnitySkills 操作。名字和参数先用 unity_find_skills 查清楚。",
		Params: obj(map[string]any{"name": prop("string", "操作名，例如 gameobject_find"), "args": map[string]any{"type": "object", "description": "操作的参数"}}, "name")},
}

// ---------- carrying a tool call out ----------

func argStr(m map[string]any, k string) string { s, _ := m[k].(string); return s }

func argStrs(m map[string]any, k string) []string {
	var out []string
	switch v := m[k].(type) {
	case string:
		if v != "" {
			out = append(out, v)
		}
	case []any:
		for _, e := range v {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func baseName(p string) string { return path.Base(strings.ReplaceAll(p, `\`, "/")) }

// toolTitle: what the step list says a call is doing.
func toolTitle(name string, a map[string]any) string {
	switch name {
	case "inspect_avatar":
		return "查看头像和现有菜单"
	case "inspect_object":
		return "查看「" + argStr(a, "path") + "」里的网格"
	case "list_prefabs":
		return "查找 prefab：" + strings.Join(argStrs(a, "folders"), "、")
	case "dress":
		return "穿上「" + strings.TrimSuffix(baseName(argStr(a, "prefab")), ".prefab") + "」"
	case "build_menu":
		n := 0
		if l, ok := a["items"].([]any); ok {
			n = len(l)
		}
		return fmt.Sprintf("生成菜单（%d 项）", n)
	case "place_avatar":
		return "把素体「" + strings.TrimSuffix(baseName(argStr(a, "prefab")), ".prefab") + "」放进新场景"
	case "look":
		t := "看一眼头像"
		if x := argStr(a, "target"); x != "" {
			t = "看一眼「" + baseName(x) + "」"
		}
		return t
	case "undo":
		return "撤销上一步"
	case "unity_find_skills":
		return "查找 Unity 操作：" + argStr(a, "intent")
	case "unity_skill":
		return "Unity 操作 " + argStr(a, "name")
	}
	return name
}

// runTool carries one call out. content goes back to the AI; short is what the player reads.
func (s *aiSession) runTool(ctx context.Context, name string, a map[string]any) (content, short string, ok bool) {
	p := s.project
	fail := func(err error) (string, string, bool) {
		return "错误：" + err.Error(), err.Error(), false
	}
	bridge := func(cmd string, args map[string]any, wait time.Duration) (json.RawMessage, error) {
		return unity.BridgeCall(ctx, p, cmd, args, wait)
	}
	switch name {
	case "inspect_avatar":
		raw, err := bridge("inspect", map[string]any{"avatar": argStr(a, "avatar")}, 0)
		if err != nil {
			return fail(err)
		}
		var o struct {
			Avatars []struct {
				Name     string `json:"name"`
				Children []any  `json:"children"`
			} `json:"avatars"`
		}
		_ = json.Unmarshal(raw, &o)
		short = fmt.Sprintf("场景里有 %d 个头像", len(o.Avatars))
		if len(o.Avatars) == 1 {
			short = fmt.Sprintf("头像「%s」，下面有 %d 个物体", o.Avatars[0].Name, len(o.Avatars[0].Children))
		}
		return string(unity.Clip(core.CompactJSON(raw), 40000)), short, true
	case "inspect_object":
		raw, err := bridge("inspect_object", map[string]any{"avatar": argStr(a, "avatar"), "path": argStr(a, "path")}, 0)
		if err != nil {
			return fail(err)
		}
		var o struct {
			Meshes []any `json:"meshes"`
		}
		_ = json.Unmarshal(raw, &o)
		return string(unity.Clip(core.CompactJSON(raw), 40000)), fmt.Sprintf("%d 个网格", len(o.Meshes)), true
	case "list_prefabs":
		args := map[string]any{"folders": argStrs(a, "folders")}
		if n, ok := a["limit"].(float64); ok && n > 0 {
			args["limit"] = n
		}
		var o struct {
			Prefabs  []any    `json:"prefabs"`
			NotFound []string `json:"notFound"`
			Total    int      `json:"total"`
		}
		raw, err := bridge("prefabs", args, 0)
		if err == nil {
			_ = json.Unmarshal(raw, &o)
		}
		if err == nil && len(o.NotFound) > 0 { // just imported: Unity has not read the files in yet
			if _, err = bridge("refresh", map[string]any{}, 10*time.Minute); err == nil {
				if raw, err = bridge("prefabs", args, 0); err == nil {
					o.NotFound, o.Prefabs = nil, nil
					_ = json.Unmarshal(raw, &o)
				}
			}
		}
		if err != nil {
			return fail(err)
		}
		short = fmt.Sprintf("%d 个 prefab", len(o.Prefabs))
		if len(o.NotFound) > 0 {
			short += "；没找到文件夹 " + strings.Join(o.NotFound, "、")
		}
		return string(unity.Clip(core.CompactJSON(raw), 40000)), short, true
	case "dress":
		args := map[string]any{"avatar": argStr(a, "avatar"), "prefab": argStr(a, "prefab"), "name": argStr(a, "name")}
		if v, ok := a["active"].(bool); ok {
			args["active"] = v
		}
		raw, err := bridge("dress", args, 3*time.Minute)
		if err != nil {
			return fail(err)
		}
		var o struct {
			SetUp    bool     `json:"setUp"`
			Existing bool     `json:"existing"`
			Warnings []string `json:"warnings"`
			Outfit   struct {
				Object string `json:"object"`
				Meshes []any  `json:"meshes"`
			} `json:"outfit"`
		}
		_ = json.Unmarshal(raw, &o)
		if !o.Existing {
			s.changed()
		}
		short = fmt.Sprintf("「%s」%s，%d 个网格", o.Outfit.Object, map[bool]string{true: "已经在头像上", false: "已穿上"}[o.Existing], len(o.Outfit.Meshes))
		if len(o.Warnings) > 0 {
			short += "。注意：" + strings.Join(o.Warnings, "；")
		}
		return string(unity.Clip(core.CompactJSON(raw), 40000)), short, true
	case "build_menu":
		args := map[string]any{"avatar": argStr(a, "avatar"), "items": a["items"], "root": argStr(a, "root"), "parameter": argStr(a, "parameter")}
		if v, ok := a["icons"].(bool); ok {
			args["icons"] = v
		}
		raw, err := bridge("build_menu", args, 5*time.Minute)
		if err != nil {
			return fail(err)
		}
		var o map[string]any
		_ = json.Unmarshal(raw, &o)
		delete(o, "menu") // the whole scene again: the AI can ask for it
		created, _ := o["created"].([]any)
		warns, _ := o["warnings"].([]any)
		icons, _ := o["icons"].(float64)
		s.changed()
		short = fmt.Sprintf("新建 %d 项，图标 %d 张", len(created), int(icons))
		if len(created) == 0 {
			short = "菜单已是最新，没有新建的项"
		}
		for _, w := range warns {
			short += "。注意：" + fmt.Sprint(w)
		}
		b, _ := json.Marshal(o)
		return string(unity.Clip(b, 24000)), short, true
	case "place_avatar":
		raw, err := bridge("place_avatar", map[string]any{"prefab": argStr(a, "prefab"), "name": argStr(a, "name")}, 3*time.Minute)
		if err != nil {
			return fail(err)
		}
		var o struct {
			Scene  string `json:"scene"`
			Avatar string `json:"avatar"`
		}
		_ = json.Unmarshal(raw, &o)
		// a saved new scene: nothing to take back with Ctrl+Z, so the count of changes stays
		return string(raw), "新场景 " + o.Scene + "，头像「" + o.Avatar + "」", true
	case "look":
		return s.look(ctx, a)
	case "undo":
		raw, err := bridge("undo", map[string]any{}, 0)
		if err != nil {
			return fail(err)
		}
		var o struct {
			Undone string `json:"undone"`
		}
		_ = json.Unmarshal(raw, &o)
		s.undone()
		return string(raw), "已撤销：" + o.Undone, true
	case "unity_find_skills":
		top := 0
		if n, ok := a["top"].(float64); ok {
			top = int(n)
		}
		out, err := unity.SkillsFind(ctx, p, argStr(a, "intent"), top)
		if err != nil {
			return fail(err)
		}
		return out, "", true
	case "unity_skill":
		args, _ := a["args"].(map[string]any)
		skill := argStr(a, "name")
		shot := screenshotSkills[skill] // only looks; its picture goes to Assets/Screenshots
		if shot {
			args = screenshotArgs(skill, args)
		}
		s.mu.Lock()
		allowed := s.allowAll
		s.mu.Unlock()
		if why := consentReason(skillsManifest(ctx, p), skill); why != "" && !allowed && !shot {
			j, _ := json.Marshal(args)
			if r := []rune(string(j)); len(r) > 300 {
				j = []byte(string(r[:300]) + "…")
			}
			q := "AI 想在 Unity 里执行「" + skill + "」"
			if len(args) > 0 {
				q += "：" + string(j)
			}
			if !s.confirm(ctx, q+"\n"+why+"。") {
				return "玩家没有同意执行这个操作。不要换别的办法去做同一件事；先问玩家想怎么办。", "你没有同意，没有执行", false
			}
		}
		raw, good, err := unity.SkillsCallRaw(ctx, p, skill, args)
		if err != nil {
			return fail(err)
		}
		if !good {
			out := string(unity.Clip(core.CompactJSON(raw), unity.SkillAnswerMax))
			r := []rune(out)
			if len(r) > 200 {
				r = append(r[:200], '…')
			}
			return out, string(r), false
		}
		var imgs []aiImage
		if shot || bytes.Contains(raw, []byte(`"imageBase64"`)) {
			raw, imgs = takeImages(p, raw) // a picture is no use as text: it goes along as a picture
		}
		out := string(unity.Clip(core.CompactJSON(raw), unity.SkillAnswerMax))
		if len(imgs) > 0 {
			s.showImgs(saveShots(imgs))
			text, attach, how := s.present(ctx, imgs, "")
			s.setOut(attach)
			return out + "\n" + text, fmt.Sprintf("截图 %d 张，%s", len(imgs), how), true
		}
		if shot {
			return out + "\n（没有拿到截图的图片数据。）", "没有拿到截图", true
		}
		s.changed()
		return out, "", true
	}
	return fail(errors.New("没有这个工具：" + name))
}

func (s *aiSession) changed() {
	s.mu.Lock()
	s.changes++
	s.mu.Unlock()
}

func (s *aiSession) undone() {
	s.mu.Lock()
	if s.changes > 0 {
		s.changes--
	}
	s.mu.Unlock()
}

// ---------- a run ----------

// one row of the pipeline's asset list: a folder (or prefab) of the project and what it is
type aiAsset struct {
	Folder string `json:"folder"`
	Kind   string `json:"kind"` // 素体, 衣服, 头发, 配饰, 道具
	Name   string `json:"name,omitempty"`
}

type aiRunReq struct {
	Project   string    `json:"project"`
	Mode      string    `json:"mode"` // chat, dress
	Text      string    `json:"text"`
	Folders   []string  `json:"folders"` // the older form of Assets: outfits
	Assets    []aiAsset `json:"assets"`
	Hierarchy string    `json:"hierarchy"`
	NoAI      bool      `json:"noAI"`
}

// tidyAssets: the rows as the pipeline uses them (folders in the project's form, a kind each, no doubles).
func tidyAssets(req *aiRunReq) {
	if len(req.Assets) == 0 {
		for _, f := range req.Folders {
			req.Assets = append(req.Assets, aiAsset{Folder: f, Kind: "衣服"})
		}
	}
	var out []aiAsset
	seen := map[string]bool{}
	for _, a := range req.Assets {
		a.Folder = strings.Trim(strings.TrimSpace(strings.ReplaceAll(a.Folder, `\`, "/")), "/")
		a.Name = strings.TrimSpace(a.Name)
		if a.Folder == "" || seen[strings.ToLower(a.Folder)] {
			continue
		}
		if !core.ContainsStr(pipelineKinds, a.Kind) {
			a.Kind = "衣服"
		}
		seen[strings.ToLower(a.Folder)] = true
		out = append(out, a)
	}
	req.Assets = out
	req.Folders = nil
	for _, a := range out {
		req.Folders = append(req.Folders, a.Folder)
	}
}

func pipelineTask(assets []aiAsset, hierarchy, extra string) string {
	var b strings.Builder
	b.WriteString("把我选好的素材装到头像上，并生成菜单开关。\n")
	if len(assets) > 0 {
		b.WriteString("素材清单（类别：工程里的文件夹或 prefab）：\n")
		for _, k := range pipelineKinds {
			for _, a := range assets {
				if a.Kind != k {
					continue
				}
				line := "- " + k + "：" + a.Folder
				if a.Name != "" {
					line += "（" + a.Name + "）"
				}
				if k == "素体" {
					line += " —— 场景里没有头像时先把它放进场景"
				}
				b.WriteString(line + "\n")
			}
		}
	} else {
		b.WriteString("我没有指明素材：先问我素材在工程的哪个文件夹、是什么类别。\n")
	}
	b.WriteString("菜单层级：" + hierarchy + "\n")
	if strings.TrimSpace(extra) != "" {
		b.WriteString("补充要求：" + strings.TrimSpace(extra) + "\n")
	}
	return b.String()
}

func startAIRun(st *core.Store, req aiRunReq) error {
	s := aiSessionFor(req.Project)
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return errors.New("上一个请求还在进行：等它做完，或者先点「停止」")
	}
	hierarchy := strings.TrimSpace(req.Hierarchy)
	if hierarchy == "" {
		hierarchy = loadAIConfig().Hierarchy
	}
	text := strings.TrimSpace(req.Text)
	if req.Mode == "dress" {
		tidyAssets(&req)
		text = pipelineTask(req.Assets, hierarchy, req.Text)
	}
	if text == "" {
		s.mu.Unlock()
		return errors.New("还没有写要做什么")
	}
	var client *aiClient
	if !(req.Mode == "dress" && req.NoAI) {
		c, err := newAIClient(st)
		if err != nil {
			s.mu.Unlock()
			return err
		}
		client = c
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.busy, s.cancel = true, cancel
	s.client, s.st, s.outImgs = client, st, nil
	s.mu.Unlock()
	if req.Mode == "dress" {
		h := hierarchy
		_ = aiSave("", "", "", nil, &h)
	}
	s.add(AIStep{Kind: "user", Text: text})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.add(AIStep{Kind: "error", Text: fmt.Sprint("出错了：", r)})
			}
			cancel()
			s.mu.Lock()
			s.busy, s.cancel = false, nil
			s.mu.Unlock()
		}()
		var err error
		if req.Mode == "dress" { // what was just imported is on disk; Unity reads it in now
			s.add(AIStep{Kind: "tool", Tool: "refresh", Text: "让 Unity 读入新导入的文件", Busy: true})
			if _, err = unity.BridgeCall(ctx, s.project, "refresh", map[string]any{}, 10*time.Minute); err == nil {
				s.finishStep(true, "")
			}
		}
		switch {
		case err != nil:
		case client == nil:
			err = quickPipeline(ctx, st, s, req.Assets, hierarchy)
		default:
			err = s.converse(ctx, client, text, hierarchy)
		}
		if err != nil {
			s.finishStep(false, "已中断")
			s.add(AIStep{Kind: "error", Text: err.Error()})
		}
	}()
	return nil
}

// converse sends the player's text and lets the AI work until it has nothing more to call.
func (s *aiSession) converse(ctx context.Context, c *aiClient, text, hierarchy string) error {
	s.mu.Lock()
	keep := len(s.turns)
	s.turns = append(s.turns, aiTurn{Role: "user", Text: text})
	s.mu.Unlock()
	system := aiSystemPrompt(s.project, hierarchy)
	said := false
	for round := 0; round < aiMaxRounds; round++ {
		s.agePictures()
		s.mu.Lock()
		turns := append([]aiTurn{}, s.turns...)
		s.mu.Unlock()
		size := 0
		for _, t := range turns {
			size += len(t.Text) + len(t.blocks)
			for _, r := range t.Results {
				size += len(r.Content)
			}
		}
		if size > 600000 {
			return errors.New("这段对话太长了：点「新对话」重新开始（已经做的改动还在 Unity 里）")
		}
		out, err := c.chat(ctx, system, turns, aiTools)
		var he *aiHTTPError
		if errors.As(err, &he) && (he.Status == 400 || he.Status == 415 || he.Status == 422) && s.dropPictures() {
			// the service may not take pictures after all: once more without them
			s.mu.Lock()
			turns = append([]aiTurn{}, s.turns...)
			s.mu.Unlock()
			if out, err = c.chat(ctx, system, turns, aiTools); err == nil { // it was the pictures: remembered
				rememberSees(seesKey(c), false)
				s.add(AIStep{Kind: "error", Text: "这个模型不接受图片，截图没有送到 AI 那里（截图仍然显示在上面）。想让 AI 看图，在「AI 服务」的「看图」里另设一个看图模型。"})
			}
		}
		if err != nil {
			if !said { // nothing happened yet: the request can simply be sent again
				s.mu.Lock()
				s.turns = s.turns[:keep]
				s.mu.Unlock()
			} else {
				s.mendTurns()
			}
			return err
		}
		said = true
		if strings.TrimSpace(out.Text) == "" && len(out.Calls) == 0 { // nothing at all: not a turn a service takes back
			out.Text, out.blocks = "（AI 没有再说什么）", nil
		}
		s.mu.Lock()
		s.turns = append(s.turns, out)
		s.mu.Unlock()
		if t := strings.TrimSpace(out.Text); t != "" {
			s.add(AIStep{Kind: "say", Text: t})
		}
		if len(out.Calls) == 0 {
			return nil
		}
		res := aiTurn{Role: "tool"}
		for _, k := range out.Calls {
			if ctx.Err() != nil {
				res.Results = append(res.Results, aiResult{ID: k.ID, Name: k.Name, Content: "玩家停止了这次操作", IsErr: true})
				continue
			}
			var args map[string]any
			if len(k.Args) > 0 && json.Unmarshal(k.Args, &args) != nil {
				res.Results = append(res.Results, aiResult{ID: k.ID, Name: k.Name, Content: "错误：参数不是合法的 JSON，重新写一次", IsErr: true})
				s.add(AIStep{Kind: "tool", Tool: k.Name, Text: toolTitle(k.Name, nil), Out: "AI 给的参数格式不对，让它重写"})
				continue
			}
			if args == nil {
				args = map[string]any{}
			}
			s.add(AIStep{Kind: "tool", Tool: k.Name, Text: toolTitle(k.Name, args), Busy: true})
			content, short, ok := s.runTool(ctx, k.Name, args)
			s.finishStep(ok, short)
			res.Results = append(res.Results, aiResult{ID: k.ID, Name: k.Name, Content: content, IsErr: !ok, Images: s.takeOut()})
		}
		s.mu.Lock()
		s.turns = append(s.turns, res)
		s.mu.Unlock()
		if ctx.Err() != nil {
			return errors.New("已停止")
		}
	}
	return fmt.Errorf("AI 连续做了 %d 步还没有做完，先停在这里：看看 Unity 里的结果，再告诉它接着做什么", aiMaxRounds)
}

// mendTurns: a conversation must not end on an assistant turn whose calls were never answered.
func (s *aiSession) mendTurns() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n := len(s.turns); n > 0 && s.turns[n-1].Role == "assistant" && len(s.turns[n-1].Calls) > 0 {
		res := aiTurn{Role: "tool"}
		for _, k := range s.turns[n-1].Calls {
			res.Results = append(res.Results, aiResult{ID: k.ID, Name: k.Name, Content: "没有执行（请求中断了）", IsErr: true})
		}
		s.turns = append(s.turns, res)
	}
}

// ---------- without an AI: group the meshes by their names ----------

type prefabInfo struct {
	Path        string   `json:"path"`
	Name        string   `json:"name"`
	WholeAvatar bool     `json:"wholeAvatar"`
	MAReady     bool     `json:"modularAvatarReady"`
	Renderers   int      `json:"renderers"`
	MeshNames   []string `json:"meshNames"`
	HasArmature bool     `json:"hasArmature"`
}

type menuNode struct {
	Object    string     `json:"object"`
	Label     string     `json:"label"`
	Type      string     `json:"type"`
	Parameter string     `json:"parameter"`
	Auto      bool       `json:"auto"`
	Default   bool       `json:"default"`
	Toggles   []string   `json:"toggles"`
	Children  []menuNode `json:"children"`
}

type avatarInfo struct {
	Name     string     `json:"name"`
	Path     string     `json:"path"`
	Active   bool       `json:"active"`
	Prefab   string     `json:"prefab"`
	MaMenu   []menuNode `json:"maMenu"`
	Children []struct {
		Name   string `json:"name"`
		Kind   string `json:"kind"`
		Active bool   `json:"active"`
		Prefab string `json:"prefab"`
	} `json:"children"`
}

// partWords: what a mesh is, by words in its name. Earlier rows win ("Sandal jewel" is a shoe).
var partWords = []struct {
	label string
	words []string
}{
	{"尾巴", []string{"tail", "しっぽ", "尻尾", "尾巴"}},
	{"翅膀", []string{"wing", "ウィング", "翼", "翅"}},
	{"袜子", []string{"sock", "stocking", "tights", "kneehigh", "knee high", "legwear", "ソックス", "タイツ", "ニーハイ", "靴下", "袜"}},
	{"鞋子", []string{"shoe", "boot", "sandal", "heel", "sneaker", "loafer", "pumps", "slipper", "靴", "ブーツ", "サンダル", "シューズ", "鞋"}},
	{"手套", []string{"glove", "手袋", "グローブ", "手套"}},
	{"帽子", []string{"hat", "cap", "beret", "hood", "helmet", "帽", "ハット", "キャップ", "フード"}},
	{"头饰", []string{"horn", "headdress", "headband", "hairpin", "hair", "pin", "crown", "tiara", "halo", "ear", "ears", "flower", "ツノ", "ヘッド", "カチューシャ", "头饰", "发饰", "花"}},
	{"包", []string{"bag", "pouch", "backpack", "バッグ", "ポーチ", "背包", "挎包"}},
	{"外套", []string{"jacket", "coat", "outer", "cardigan", "hoodie", "parka", "blazer", "cape", "poncho", "haori", "ジャケット", "コート", "カーディガン", "パーカー", "アウター", "羽織", "外套"}},
	{"裙子", []string{"skirt", "スカート", "裙"}},
	{"裤子", []string{"pants", "shorts", "slacks", "trousers", "jeans", "denim", "ズボン", "ショートパンツ", "裤"}},
	{"内衣", []string{"bra", "panties", "panty", "underwear", "lingerie", "ブラジャー", "ショーツ", "内衣"}},
	{"上衣", []string{"shirt", "tops", "top", "blouse", "sailor", "tee", "sweater", "knit", "vest", "inner", "corset", "bustier", "シャツ", "トップス", "ブラウス", "セーター", "ニット", "インナー", "上衣"}},
	{"饰品", []string{"choker", "necklace", "belt", "charm", "earring", "pierce", "ring", "bracelet", "bangle", "accessor", "acc", "chouchou", "jewel", "harness", "garter", "ribbon", "badge", "tie", "necktie", "collar", "chain", "strap", "glasses", "mask", "ornament",
		"チョーカー", "ベルト", "アクセ", "リボン", "ネックレス", "ピアス", "飾り", "饰"}},
}

// partRank: the order of the parts in a menu; what comes late is also what is merged into 饰品 first.
var partRank = map[string]int{"外套": 0, "上衣": 1, "裙子": 2, "裤子": 3, "内衣": 4, "袜子": 5, "鞋子": 6, "帽子": 7, "尾巴": 8, "翅膀": 9, "手套": 10, "包": 11, "头饰": 12, "饰品": 13}

// partOf: the group a mesh name belongs to ("" = the outfit itself, no switch of its own).
func partOf(name string) string {
	// "DevilTail_L" → " devil tail l "
	var sb strings.Builder
	prev := rune(0)
	for _, c := range name {
		if c >= 'A' && c <= 'Z' && prev >= 'a' && prev <= 'z' {
			sb.WriteRune(' ')
		}
		sb.WriteRune(c)
		prev = c
	}
	n := " " + strings.ToLower(sb.String()) + " "
	n = strings.NewReplacer("_", " ", "-", " ", ".", " ").Replace(n)
	for _, row := range partWords {
		for _, w := range row.words {
			if core.IsASCII(w) && len(w) <= 4 { // short words only as whole words ("ear" is not in "wear")
				if strings.Contains(n, " "+w+" ") || strings.Contains(n, " "+w+"s ") {
					return row.label
				}
				continue
			}
			if strings.Contains(n, w) {
				return row.label
			}
		}
	}
	return ""
}

// A menu template: "主菜单 > {分类} > {素材} > {开关}". Levels are sub menus written out ("衣柜"), the
// category slot ({分类}: 衣服, 头发, 配饰, 道具), the per-asset slot ({素材}, the older {衣服}) and the switches.
type hierLevel struct {
	lit  string // a sub menu's name
	slot string // "cat", "asset", "items", or "" for a name
}

// the menu a kind of asset goes under, and the names a player may write for that level
var kindCategory = map[string]string{"衣服": "衣服", "头发": "头发", "配饰": "配饰", "道具": "道具"}
var categoryWords = map[string]bool{"衣服": true, "服装": true, "换装": true, "头发": true, "发型": true, "配饰": true, "饰品": true, "道具": true}

func parseHierarchy(h string) []hierLevel {
	h = strings.NewReplacer("->", ">", "→", ">", "＞", ">", "》", ">", "/", ">", "｛", "{", "｝", "}").Replace(h)
	var out []hierLevel
	for _, seg := range strings.Split(h, ">") {
		seg = strings.TrimSpace(strings.Trim(strings.TrimSpace(seg), "（）()"))
		inner := strings.TrimSpace(strings.Trim(seg, "{}"))
		slot := strings.HasPrefix(seg, "{") || strings.HasSuffix(seg, "}")
		low := strings.ToLower(inner)
		// written out in words instead of {…}: "衣服1层级", "每件衣服", "衣服相关开关"
		each := strings.Contains(inner, "每件") || strings.Contains(inner, "每个") || strings.Contains(inner, "衣服1") || strings.Contains(inner, "衣服名") || strings.Contains(inner, "各件") || strings.Contains(inner, "素材名")
		items := strings.Contains(inner, "开关") || strings.Contains(inner, "部件")
		cat := slot && (strings.Contains(inner, "分类") || strings.Contains(inner, "类别") || strings.Contains(inner, "类型") || low == "category" || low == "kind")
		asset := each || (slot && (strings.Contains(inner, "素材") || strings.Contains(inner, "衣服") || strings.Contains(inner, "服装") || low == "outfit" || low == "asset" || low == "item"))
		switch {
		case seg == "" || inner == "主菜单" || inner == "根菜单":
		case items:
			out = append(out, hierLevel{slot: "items"})
		case cat:
			out = append(out, hierLevel{slot: "cat"})
		case asset:
			out = append(out, hierLevel{slot: "asset"})
		case slot:
		default:
			if name := strings.TrimSuffix(strings.TrimPrefix(inner, "生成"), "层级"); name != "" {
				out = append(out, hierLevel{lit: name})
			}
		}
	}
	return out
}

// menuPath: the sub menus above a kind's switches, and whether each asset of that kind gets a level of its own.
// A level written as "衣服" stands for the category when the template has no {分类}: hair then goes under 头发.
func menuPath(levels []hierLevel, kind string) (base []string, perAsset bool) {
	hasCat := false
	for _, l := range levels {
		hasCat = hasCat || l.slot == "cat"
	}
	cat := kindCategory[kind]
	if cat == "" {
		cat = kind
	}
	// the level standing for the category: the last category word above the asset level (none with {分类}).
	// Outfits keep the names as written: the template was written with them in mind.
	catAt := -1
	if !hasCat && kind != "衣服" {
		for i, l := range levels {
			if l.slot == "asset" || l.slot == "items" {
				break
			}
			if l.slot == "" && categoryWords[l.lit] {
				catAt = i
			}
		}
	}
	for i, l := range levels {
		switch {
		case l.slot == "asset":
			perAsset = true
		case l.slot == "items", perAsset:
		case l.slot == "cat" || i == catAt:
			base = append(base, cat)
		default:
			base = append(base, l.lit)
		}
	}
	return base, perAsset
}

// hierarchyLevels: the outfits' view of a template.
func hierarchyLevels(h string) (base []string, perOutfit bool) {
	return menuPath(parseHierarchy(h), "衣服")
}

func tidyName(s string, drop []string) string {
	s = strings.TrimSuffix(s, ".prefab")
	low := strings.ToLower(s)
	for _, d := range drop {
		if d == "" {
			continue
		}
		if i := strings.Index(low, strings.ToLower(d)); i >= 0 {
			s = s[:i] + s[i+len(d):]
			low = strings.ToLower(s)
		}
	}
	s = strings.NewReplacer("_", " ", "prefab", "", "Prefab", "", "  ", " ").Replace(s)
	return strings.Trim(strings.TrimSpace(s), "-·() ")
}

func commonPrefix(names []string) string {
	if len(names) == 0 {
		return ""
	}
	p := []rune(names[0])
	for _, n := range names[1:] {
		r := []rune(n)
		i := 0
		for i < len(p) && i < len(r) && p[i] == r[i] {
			i++
		}
		p = p[:i]
	}
	return string(p)
}

// baseAliases: the names of the base body the avatar is built on (from the settings' table), by what its
// name and prefab path say.
func baseAliases(st *core.Store, av avatarInfo) []string {
	st.Mu.RLock()
	rows := append([]string{}, st.Settings.Bases...)
	st.Mu.RUnlock()
	hay := strings.ToLower(av.Name + " " + av.Prefab)
	for _, row := range rows {
		name, rest, _ := strings.Cut(row, "=")
		al := append([]string{name}, strings.Split(rest, "|")...)
		for _, a := range al {
			if a = strings.TrimSpace(a); a != "" && strings.Contains(hay, strings.ToLower(a)) {
				out := []string{}
				for _, x := range al {
					if x = strings.TrimSpace(x); x != "" {
						out = append(out, x)
					}
				}
				return out
			}
		}
	}
	return nil
}

// outfitMenu: the outfit menu the avatar has already — its root object and the parameter its outfits share — if
// there is one (two or more switches on one parameter, each turning objects on). Hair parameters are not it.
func outfitMenu(av avatarInfo) (root, param string) {
	return menuOn(av, func(p string) bool { return !isHairParam(p) })
}

// hairMenu: the same for hair (a parameter named after hair).
func hairMenu(av avatarInfo) (root, param string) {
	return menuOn(av, isHairParam)
}

func isHairParam(p string) bool { return strings.Contains(strings.ToLower(p), "hair") }

func menuOn(av avatarInfo, want func(param string) bool) (root, param string) {
	count := map[string]int{}
	rootOf := map[string]string{}
	var walk func(top string, n menuNode)
	walk = func(top string, n menuNode) {
		on := false
		for _, t := range n.Toggles {
			on = on || strings.HasSuffix(t, "=on")
		}
		if n.Type == "Toggle" && n.Parameter != "" && !n.Auto && on && want(n.Parameter) {
			count[n.Parameter]++
			if rootOf[n.Parameter] == "" {
				rootOf[n.Parameter] = top
			}
		}
		for _, c := range n.Children {
			walk(top, c)
		}
	}
	for _, n := range av.MaMenu {
		walk(n.Object, n)
	}
	best := ""
	for p, n := range count {
		if n >= 2 && (best == "" || n > count[best] || (n == count[best] && p < best)) {
			best = p
		}
	}
	if best == "" {
		return "", ""
	}
	return rootOf[best], best
}

// wearsByDefault: some switch on that parameter is the avatar's default already.
func wearsByDefault(av avatarInfo, param string) bool {
	found := false
	var walk func(n menuNode)
	walk = func(n menuNode) {
		if n.Parameter == param && !n.Auto && n.Default {
			found = true
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	for _, n := range av.MaMenu {
		walk(n)
	}
	return found
}

type quickOutfit struct {
	label   string
	base    prefabInfo
	colours []prefabInfo // other colours of the same meshes
}

// pickOutfits: which prefabs of the folders are outfits to put on, colours of one outfit together.
func pickOutfits(list []prefabInfo, aliases []string) (out []quickOutfit, skipped []string) {
	var cands []prefabInfo
	for _, p := range list {
		if p.WholeAvatar {
			skipped = append(skipped, p.Name+"（整只模型）")
			continue
		}
		if p.Renderers == 0 {
			continue
		}
		cands = append(cands, p)
	}
	// the versions made for this base body, when the package says which is which
	if len(aliases) > 0 {
		var mine []prefabInfo
		for _, p := range cands {
			low := strings.ToLower(p.Path)
			for _, a := range aliases {
				if strings.Contains(low, strings.ToLower(a)) {
					mine = append(mine, p)
					break
				}
			}
		}
		if len(mine) > 0 {
			cands = mine
		}
	}
	// set up for Modular Avatar already, when some are
	var ready []prefabInfo
	for _, p := range cands {
		if p.MAReady {
			ready = append(ready, p)
		}
	}
	if len(ready) > 0 {
		cands = ready
	}
	sig := func(p prefabInfo) string {
		n := append([]string{}, p.MeshNames...)
		sort.Strings(n)
		return fmt.Sprint(p.Renderers, "|", path.Dir(p.Path), "|", strings.Join(n, "|"))
	}
	groups := map[string]int{}
	for _, p := range cands {
		k := sig(p)
		if i, ok := groups[k]; ok {
			out[i].colours = append(out[i].colours, p)
			continue
		}
		groups[k] = len(out)
		out = append(out, quickOutfit{base: p})
	}
	for i := range out {
		o := &out[i]
		names := []string{o.base.Name}
		for _, c := range o.colours {
			names = append(names, c.Name)
		}
		o.label = tidyName(o.base.Name, aliases)
		if len(names) > 1 {
			// the colours' common name, else the nearest folder that is named after the outfit
			o.label = tidyName(commonPrefix(names), aliases)
			for d := path.Dir(o.base.Path); len([]rune(o.label)) < 3 && d != "." && d != "/" && d != "Assets"; d = path.Dir(d) {
				o.label = tidyName(path.Base(d), aliases)
			}
		}
		if o.label == "" {
			o.label = o.base.Name
		}
	}
	return out, skipped
}

func colourLabel(p prefabInfo, all []string, aliases []string) string {
	cp := commonPrefix(all)
	n := tidyName(strings.TrimPrefix(p.Name, cp), aliases)
	if n == "" {
		n = tidyName(p.Name, aliases)
	}
	return n
}

// ---------- the window's requests ----------

func RegisterAI(st *core.Store, post func(string, func(http.ResponseWriter, map[string]json.RawMessage))) {
	str := func(b map[string]json.RawMessage, k string) string {
		var s string
		_ = json.Unmarshal(b[k], &s)
		return s
	}
	fail := func(w http.ResponseWriter, err error) {
		core.WriteJSON(w, map[string]any{"ok": false, "err": err.Error()})
	}
	project := func(w http.ResponseWriter, b map[string]json.RawMessage) (string, bool) {
		p, ok := unity.KnownProject(st, str(b, "project"))
		if !ok {
			fail(w, errors.New("不在工程列表里"))
		}
		return p, ok
	}
	// the service as the form has it now (saved or not): a key left empty means the saved one
	formClient := func(b map[string]json.RawMessage) (*aiClient, error) {
		info := aiProvider(str(b, "provider"))
		if info == nil {
			return nil, errors.New("先选一个 AI 服务商")
		}
		c := loadAIConfig()
		c.Profiles[info.ID] = &AIProfile{BaseURL: strings.TrimSpace(str(b, "baseUrl")), Model: strings.TrimSpace(str(b, "model"))}
		p := c.profile(info.ID)
		key := strings.TrimSpace(str(b, "key"))
		if key == "" {
			if key = aiKey(info.ID); key != "" && !sameService(loadAIConfig().profile(info.ID).BaseURL, p.BaseURL) {
				return nil, errKeyForOtherHost // the saved key stays with the address it was entered for
			}
		}
		return aiClientWith(st, *info, p.BaseURL, p.Model, key)
	}

	post("/api/ai/config", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		core.WriteJSON(w, map[string]any{"ok": true, "ai": aiView()})
	})
	post("/api/ai/save", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		var key, hier *string
		if raw, ok := b["key"]; ok {
			var k string
			if json.Unmarshal(raw, &k) == nil {
				key = &k
			}
		}
		if raw, ok := b["hierarchy"]; ok {
			var h string
			if json.Unmarshal(raw, &h) == nil {
				hier = &h
			}
		}
		if err := aiSave(str(b, "provider"), str(b, "baseUrl"), str(b, "model"), key, hier); err != nil {
			fail(w, err)
			return
		}
		if raw, ok := b["vision"]; ok {
			var v AIVision
			var vkey *string
			if json.Unmarshal(raw, &v) != nil {
				fail(w, errors.New("看图设置的格式不对"))
				return
			}
			if kr, ok := b["visionKey"]; ok {
				var k string
				if json.Unmarshal(kr, &k) == nil {
					vkey = &k
				}
			}
			if err := aiSaveVision(v, vkey); err != nil {
				fail(w, err)
				return
			}
		}
		core.WriteJSON(w, map[string]any{"ok": true, "ai": aiView()})
	})
	// 「测试看图」: the model is shown a number and asked to read it. who = main (the service of the form) or
	// other (the second service of the form).
	post("/api/ai/visiontest", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		var c *aiClient
		var err error
		who := str(b, "who")
		if who == "other" {
			var v AIVision
			_ = json.Unmarshal(b["vision"], &v)
			key := strings.TrimSpace(str(b, "visionKey"))
			if key == "" {
				aiMu.Lock()
				cfg, keys := loadAIConfigLocked(), loadAIKeysLocked()
				aiMu.Unlock()
				was := cfg.Vision.BaseURL
				cfg.Vision = v // the form as it is now: a provider's key at the same address serves
				if key = keys[visionKeyID]; key != "" && !sameService(was, v.BaseURL) {
					key = ""
				}
				if key == "" {
					delete(keys, visionKeyID)
					key = visionKey(cfg, keys)
				}
			}
			c, err = visionClientWith(st, v, key)
		} else {
			c, err = formClient(b)
		}
		if err != nil {
			fail(w, err)
			return
		}
		if c.model == "" {
			fail(w, errors.New("还没有填模型名"))
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		t0 := time.Now()
		sees, reply, refused, err := aiSeesTest(ctx, c)
		if err != nil {
			fail(w, err)
			return
		}
		if who != "other" {
			rememberSees(seesKey(c), sees)
		}
		note := fmt.Sprintf("%s 看得到图片：它读出了测试图里的数字（%.1f 秒）", c.model, time.Since(t0).Seconds())
		switch {
		case refused:
			note = c.model + " 不接受图片：服务拒绝了带图片的请求"
			if i := strings.Index(reply, "原话："); i >= 0 {
				note += "。" + reply[i:]
			}
		case !sees:
			note = c.model + " 看不到图片：它回答的是「" + reply + "」"
		}
		core.WriteJSON(w, map[string]any{"ok": true, "sees": sees, "note": note, "ai": aiView()})
	})
	post("/api/ai/test", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		c, err := formClient(b)
		if err != nil {
			fail(w, err)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		note, err := aiTest(ctx, c)
		if err != nil {
			fail(w, err)
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "note": note})
	})
	post("/api/ai/models", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		c, err := formClient(b)
		if err != nil {
			fail(w, err)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		ms, err := c.models(ctx)
		if err != nil {
			fail(w, err)
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "models": ms})
	})
	post("/api/ai/setup", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		p, ok := project(w, b)
		if !ok {
			return
		}
		var on bool
		_ = json.Unmarshal(b["on"], &on)
		if !on {
			if busy, _, _ := aiSessionFor(p).snapshot(); busy {
				fail(w, errors.New("AI 还在这个工程里干活：先停止再移除"))
				return
			}
			if err := unity.RemoveAIKit(p); err != nil {
				fail(w, err)
				return
			}
			core.WriteJSON(w, map[string]any{"ok": true, "kit": unity.AIKitStatus(p), "note": "已移除 AI 插件"})
			return
		}
		note, err := unity.InstallAIKit(p)
		if err != nil {
			fail(w, err)
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true, "kit": unity.AIKitStatus(p), "note": note})
	})
	post("/api/ai/session", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		p, ok := project(w, b)
		if !ok {
			return
		}
		busy, steps, changes := aiSessionFor(p).snapshot()
		core.WriteJSON(w, map[string]any{"ok": true, "kit": unity.AIKitStatus(p), "busy": busy, "steps": steps, "changes": changes})
	})
	post("/api/ai/run", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		p, ok := project(w, b)
		if !ok {
			return
		}
		req := aiRunReq{Project: p, Mode: str(b, "mode"), Text: str(b, "text"), Hierarchy: str(b, "hierarchy")}
		_ = json.Unmarshal(b["folders"], &req.Folders)
		_ = json.Unmarshal(b["assets"], &req.Assets)
		_ = json.Unmarshal(b["noAI"], &req.NoAI)
		var clean []string
		for _, f := range req.Folders {
			if f = strings.Trim(strings.TrimSpace(strings.ReplaceAll(f, `\`, "/")), "/"); f != "" {
				clean = append(clean, f)
			}
		}
		req.Folders = core.UniqStrings(clean)
		if err := startAIRun(st, req); err != nil {
			fail(w, err)
			return
		}
		core.WriteJSON(w, map[string]any{"ok": true})
	})
	post("/api/ai/cancel", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		if p, ok := project(w, b); ok {
			aiSessionFor(p).stop()
			core.WriteJSON(w, map[string]any{"ok": true})
		}
	})
	post("/api/ai/answer", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		if p, ok := project(w, b); ok {
			var yes, all bool
			_ = json.Unmarshal(b["ok"], &yes)
			_ = json.Unmarshal(b["all"], &all)
			core.WriteJSON(w, map[string]any{"ok": aiSessionFor(p).answer(yes, all)})
		}
	})
	post("/api/ai/reset", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		if p, ok := project(w, b); ok {
			aiSessionFor(p).reset()
			core.WriteJSON(w, map[string]any{"ok": true})
		}
	})
	post("/api/ai/undo", func(w http.ResponseWriter, b map[string]json.RawMessage) {
		p, ok := project(w, b)
		if !ok {
			return
		}
		s := aiSessionFor(p)
		if busy, _, _ := s.snapshot(); busy {
			fail(w, errors.New("AI 还在干活：等它做完，或者先点「停止」"))
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		raw, err := unity.BridgeCall(ctx, p, "undo", map[string]any{}, 30*time.Second)
		if err != nil {
			fail(w, err)
			return
		}
		var o struct {
			Undone string `json:"undone"`
		}
		_ = json.Unmarshal(raw, &o)
		s.undone()
		s.add(AIStep{Kind: "tool", Tool: "undo", Text: "撤销上一步", OK: true, Out: "已撤销：" + o.Undone})
		core.WriteJSON(w, map[string]any{"ok": true, "undone": o.Undone})
	})
}
