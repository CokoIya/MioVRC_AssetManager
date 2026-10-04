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
	changes  []bool    // scene changes made in this session, oldest first (for 「撤销上一步」); true: through UnitySkills
	fixes    int       // the check-up's one-click fixes that are an undo step in Unity (the AI's and the panel's): 「撤销上一步」 has them to take back too, 「保存为方案」 knows nothing of them
	done     runDone   // what the run in progress has put on the avatar so far
	soft     int       // the service refused a conversation of this size as too long: older answers give way earlier (0 = not yet)
	askCh    chan bool // the player's answer to the question that is open
	allowAll bool      // the player said yes to every risky operation of this conversation
	client   *aiClient // the service of the run in progress (nil when the pipeline runs without an AI)
	st       *core.Store
	outImgs  []aiImage // pictures the tool call in progress hands to the AI
	rec      runRecord // what the line put on the avatar and built since it was last run, for 「保存为方案」

	// the menus of the avatars inspect_avatar last showed, by avatar name (localMenu)
	menus map[string][]menuNode
}

// runDone: what one run changed in the scene, for the message of a run that ends half way.
type runDone struct {
	dressed []string // objects newly put on the avatar
	menus   int      // build_menu calls that changed something
	skills  int      // changes made through UnitySkills
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
			s.steps[i].Out = map[bool]string{true: "已允许", false: "未执行"}[ok]
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
	MutatesScene  bool     `json:"mutatesScene"`
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
		return "无法读取 UnitySkills 的操作清单，不能判断该操作是否安全"
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
			why = append(why, "会删除内容")
		case "Execute":
			why = append(why, "会执行编辑器命令")
		}
	}
	if k.MutatesAssets {
		why = append(why, "会修改工程中的文件，无法通过 Ctrl+Z 撤销")
	}
	if k.MayPlay {
		why = append(why, "会进入或退出 Play 模式")
	}
	if k.MayReload {
		why = append(why, "会触发 Unity 重新编译")
	}
	switch {
	case k.Risk == "": // an older UnitySkills without risk notes
		if riskySkill(name) || len(k.Operation) == 0 {
			why = append(why, "当前版本的 UnitySkills 未提供该操作的风险说明")
		}
	case k.Risk != "low":
		why = append(why, "UnitySkills 标注的风险等级为 "+k.Risk)
	}
	if len(why) == 0 && riskySkill(name) {
		why = append(why, "从名称判断，可能删除、保存或覆盖内容")
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
	return s.busy, append([]AIStep{}, s.steps...), len(s.changes) + s.fixes
}

func (s *aiSession) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy {
		return
	}
	s.steps, s.turns, s.changes, s.fixes, s.allowAll, s.soft = nil, nil, nil, 0, false, 0
	s.rec = runRecord{}
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
	return strings.NewReplacer("{project}", path.Base(strings.ReplaceAll(project, `\`, "/")), "{hierarchy}", zhHierarchy(hierarchy)).Replace(aiPromptText) + langNote(menuLang())
}

const aiPromptText = `你是「MioVRCA」里的 AI 改模助手，帮玩家在 Unity 里给 VRChat 头像装素材——衣服、头发、皮肤、配饰、道具，以及表情、光影、互动、面捕这类自带安装组件的插件，必要时先把素体放进场景——并做好菜单开关。玩家多半不熟悉 Unity：你用工具把事情做完，再用简短的中文说明结果。

# 工作方式
- 你通过工具操作玩家电脑上开着的 Unity 工程「{project}」。动手之前先用 inspect_avatar 看清现状。
- 所有改动都能在 Unity 里按 Ctrl+Z 撤销。你不保存场景，也不要调用任何保存场景的操作（place_avatar 新建并保存一个新场景是唯一的例外）；玩家检查满意后自己按 Ctrl+S。
- 只做玩家要求的事。不删除、不改名玩家已有的物体、菜单和资产，不改厂商原包里的文件。
- 道具、插件自带功能的根物体不改名、不挪层级：它们的动画和音效按物体路径找东西，改了会悄悄失效（没有报错，只是不工作）。菜单里想换个叫法，只改菜单项显示的名字。
- 物体路径、prefab 路径必须来自工具返回的结果，不要自己编。
- 工具报错时先读懂错误，改好参数再试；同一个错误不要原样重试两次以上。确实做不到就如实说明卡在哪一步、玩家可以怎么办。
- 一次工具调用的参数要简短：超过模型的输出上限会被截断，这次调用就不会执行。菜单项很多时把 build_menu 分成几次提交，每次十几项。
- 需要玩家做决定时（例如找不到给这个素体的版本、场景里有好几个头像），停下来问，不要猜。

# 素材的类别决定做法
- 素体：整只头像。场景里没有头像时，用 list_prefabs 在素体文件夹里找 wholeAvatar=true 的 prefab，用 place_avatar 放进新场景，再 inspect_avatar。场景里已经有头像就不要再放，告诉玩家。
- 衣服：dress 穿上；菜单里做互斥的 outfit 项（参数 Clothtoggle），可以按部件做 part 开关。
  - 素体自带的衣服（头像原本就有、不是 dress 穿上去的）如果由原厂菜单的参数控制（inspect_avatar 的 descriptorMenu 或素体自己的菜单里有它的开关），不要把它的物体写进 outfit / part 项：原厂参数通常同时带着身体收缩和别的网格的变形，只关物体会留下收缩或变形。新衣服照常装，并告诉玩家：自带的衣服要在原厂菜单里脱，工具做不到「选新衣服时自动脱掉原装」。
- 头发：dress 戴上；菜单里也是互斥的 outfit 项，但 parameter 写 Hair_Choose（和衣服分开互斥）。素体自带的头发还在头像上时（inspect_avatar 的 children 里 kind=mesh、名字带 Hair 的那个），也给它一个 outfit 项（objects 写它、不用 dress），这样换外装头发时能把它关掉。单色头发直接在「头发」这一层做一个 outfit 项；多配色的做一层子菜单，里面放配色项和发饰的 part 开关。
- 配饰：dress 戴上；做 toggle 项，default=true（默认显示；选中=显示，取消=隐藏）。
- 道具：dress 装上（active=false，不要写 name，保持 prefab 原名）；做 toggle 项，default=false（默认隐藏，选中才显示）；每个道具一层子菜单，里面的开关叫「显示」。
- 表情、光影、互动、面捕是插件：它们的 prefab 自带安装组件（Modular Avatar 的菜单、动画器、参数，或 VRCFury），构建时自己装好。做法见下面「插件：place_prefab」。不要对它们用 dress，不要给它们做 toggle 开关，不要改名。
- 皮肤：给素体的身体、脸换一套材质，做法见下面「皮肤」。

# 穿戴：dress
- 先用 list_prefabs 看文件夹里有哪些 prefab，再挑要穿的：
  - wholeAvatar=true 是带素体的整只模型，不能当衣服穿。
  - 一件衣服常有给不同素体的版本。头像的名字和 prefab 路径里能看出素体名，选路径或名字里带这个素体名的版本；找不到对应版本时告诉玩家这件衣服可能不是给这个素体做的，问清楚再继续。
  - modularAvatarReady=true 的已经配好 Modular Avatar；没有配好的也能穿，工具会自动对骨架。
  - 同一件衣服的不同配色常常是几个 prefab（网格名相同、材质不同）。只穿其中一个，其余配色在菜单里用 materialsFrom 做成换色项，不要把每个配色都穿上去：每多穿一份，它的骨骼物理就多算一份，隐藏着也算。
- 文件夹和 prefab 都写工程里的路径，以 Assets/ 开头。
- dress 的结果里已经带着这件衣服的网格列表（outfit.meshes），可以直接拿来分部件，不用再查一次。
- 结果里 setUp=false 或有 warnings 时，如实转告玩家。
- 头像下已经有同名物体（例如两个都叫 Dress 的 prefab）时，新穿上的会自动换一个名字，warnings 里会说明。做菜单、拍照一律用结果里 outfit.object 给出的路径，不要用 prefab 的文件名去猜。
- active 只对这次新放上去的物体生效；已经在头像上的（existing=true）保持原来的显示状态。
- PhysBone（骨骼物理组件）整只头像最多 256 个，隐藏的物体也计入，超了上传会失败。inspect_avatar 的 physBones 是现在的总数，children 里每个物体的 physBones 是它占多少；dress 的结果里也有新的总数。头发、尾巴、带飘带的衣服占得多，穿之前先看余量。超过 256 时告诉玩家是哪几件占得多，建议把多穿的配色改成换色项，或把不用的素材从模型上拿掉；不要自己删玩家的东西。

# 插件：place_prefab
- 用 list_prefabs 看文件夹里的 prefab：selfInstalling=true 的自带安装组件；missingScripts 大于 0 说明它依赖的插件没装进工程（先让玩家装好，不要放）；faceTracking=true 是面捕；menuInstallers 是它自带的菜单安装器个数。
- 只在能确定放哪一个时才放：文件夹里只有一个可放的 prefab，或者有给各个素体的版本、其中一个的路径带着头像的素体名。有好几个而看不出该放哪个时（例如同时有 MA 版和 VRCFury 版、有好几个预设），列出来问玩家，不要猜。
- 文件夹里没有 prefab（只有材质、贴图、动画或脚本）时，告诉玩家里面有什么、要按素材的说明手动安装，不要自己拼。
- place_prefab 把 prefab 原样放到头像下：保持原名和内部结构，不对骨架，不加开关。结果里的 object 是它的路径，menuInstallers 是它自带菜单安装器所在的物体。
- 菜单：表情放「表情」、光影放「光影」、互动放「互动」。用 build_menu 的 install 项把插件自带的菜单挪进去（objects 写 place_prefab 返回的 object，path 写类别那一层，label 写素材的简短中文名）。它只是换个位置，菜单里的文字保持插件原样；玩家要汉化时，告诉他在 Unity 里改那些菜单项的 Label。结果里提示没有 MA Menu Installer 时，菜单是 VRCFury 或插件自己的脚本生成的，位置要在它自己的组件里改，如实转告。
- 面捕：
  - 面捕依赖素体的脸部网格和形态键，只能用给这个素体（并且版本对得上）做的面捕。素材清单里写着适配素体；和头像的素体对不上，或者看不出来时，先问玩家，不要装。
  - 素材清单里写着文件夹里有什么。带着编辑器脚本的面捕（例如 Triturbo 的）必须用它自己的安装窗口装（它要给脸部网格补形态键），直接放 prefab 不会生效：不要 place_prefab，告诉玩家在 Unity 菜单栏打开它的安装窗口（Triturbo 是 TriturboFT > 素体名 FT），把头像拖进 Avatar 栏，选精度，点 Apply Face Tracking Addon。
  - 能直接放的面捕 prefab 用 place_prefab 放好即可。它的菜单留在主菜单上、保留原名（Face Tracking），不要用 install 挪。
  - inspect_avatar 的 faceTracking=true 表示头像已经有面捕：不要再装第二套，也不要再做「表情」（面捕取代手势表情）。
  - 面捕很占同步参数（高精度的眼加嘴约 160 位）。放之前看 parameterBits 的余量，放之后把结果里的 parameterBits 告诉玩家。
- 表情：只放给这个素体做的（表情动画写的是这个素体的形态键）。头像已经有面捕时不做。只有动画文件、没有可放的 prefab 的表情包，需要把动画接进 FX 的手势层或表情轮盘，这要改动画控制器，告诉玩家需手动完成，不要用 UnitySkills 去改原厂控制器。
- 光影、互动：放好并挪菜单即可。给别的素体做的互动插件位置会对不上，放之前告诉玩家。
- 头像上有 PCSS4VRC（children 里名字或组件带 PCSS 的物体）时，它在构建时只把网格上现有的材质换成带阴影的 _pcss 版本：换色项和皮肤项替换上去的材质不会被转换。做了这类菜单项就提醒玩家，可以在 Unity 里把对应 MA Material Setter 的目标材质改成同目录下的 _pcss 版本。

# 皮肤
- 皮肤是给素体的身体和脸换材质，不往头像上放东西。菜单里做 skin 项：同一个参数（Skin_Choose）上互斥，放在「皮肤」这一层，不分素材子菜单。
- 先做一项「原版皮肤」（kind=skin，default=true，不写 objects 和材质）：它就是场景里现在的样子，选别的皮肤后能换回来。头像已经有 Skin_Choose 的菜单项时不用再做。
- 皮肤素材带着 prefab（身体或整只头像，网格名和头像的一样）时：skin 项写 objects=头像自己的身体网格（inspect_avatar 的 children 里 kind=mesh 的，例如 Body、Body_base），materialsFrom=那个 prefab 的路径，工具会逐个材质槽比对出要换的材质。
- 皮肤素材只有材质文件时：能从材质名明确对上是哪个网格的哪个槽（先用 inspect_object 看网格现在的材质名）才做，用 materials 逐个写 {object, slot, material}（材质文件的路径可以用 UnitySkills 的资产查找操作查到）；对不上就问玩家。只有贴图或 lilToon 预设、没有现成材质的皮肤，需要先在 Unity 里复制素体的身体材质再套用，告诉玩家需手动完成。
- 只换给这个素体做的皮肤：贴图是按素体的 UV 画的，别的素体的用不了。

# 做菜单：build_menu
菜单用 Modular Avatar 的菜单项搭出来。items 里每一项是一个开关：
- kind="outfit"：穿这件。同一个 parameter 上的 outfit 互斥——选一件，其余自动脱下。objects 写这件的根物体。default=true 表示进游戏默认穿它，每个参数最多一个。
  - 换色项也是 outfit：objects 仍写已经穿上的那件的根物体，再加 materialsFrom=另一个配色的 prefab 路径。
- kind="part"：衣服上的一组部件（外套、鞋子、帽子…），选中=隐藏。objects 写这一组网格物体的路径，来自 inspect_object。一个开关管一组会一起出现的网格，例如「外套」包括外套本体和它的袖章、里衬。
- kind="toggle"：独立的开关（配饰、道具），选中=显示。objects 写它的根物体；default 写进游戏时显不显示。
- kind="strip"：一键脱光。它用自己的开关临时关掉这个参数上的所有衣服，取消后原来选的那件和各部件开关原样回来（它不占互斥参数的编号）。做衣服菜单时，在衣服那一层的最后加一项（label 写「一键脱光」），玩家说不要就不加；头像已经有时（inspect_avatar 的 maMenu 里带 strip 的项）不要再加。工具会把它排到所有衣服项之后，结果里提示它排在某些衣服之前时，转告玩家按提示把它拖到最后。已有的一键脱光项在之后每次 build_menu 时会自动补上新加的衣服，不用重复写；这个参数上还没有衣服时它什么都不控制，结果里会提示。
  - 它只脱菜单里的衣服。素体自带的内衣想一起脱时，可以把内衣网格写进 strip 的 objects——但只在内衣不由原厂菜单的参数控制时才这样做（原因同上面「素体自带的衣服」）；否则告诉玩家内衣要在原厂菜单里脱。
- kind="skin"：换皮肤，见上面「皮肤」。
- kind="install"：把插件自带的菜单挪到 path 这一层，见上面「插件」。不是开关，不占参数。
- path 是这一项所在的子菜单，从主菜单往下写，不含「主菜单」；label 是这一项显示的名字。

菜单层级按玩家给的模板排：{hierarchy}
- 「{分类}」是素材类别那一层，名字就是类别：表情、衣服、头发、皮肤、配饰、道具、光影、互动（新建菜单时按这个顺序排；面捕的菜单留在主菜单上，不进分类）。模板里直接写着「衣服」这样的名字时，别的类别换成自己的名字（头发放「头发」里，道具放「道具」里）。
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
- 已经做好的开关（part、toggle、strip）不改名、不挪到别的子菜单：它的参数是按菜单物体的位置生成的，改了玩家存的开关状态会丢。玩家想换显示的名字时，告诉他在 Unity 里改那个菜单项的 Label。给已有的衣服加配色时沿用它现有的部件开关，不另做一组。
- 互斥项的编号由工具分配，用过的编号不会再分给别的衣服。不要用 UnitySkills 去改现有菜单项的参数名和取值。
- 同步参数一共 256 位：每个 part / toggle / strip 开关占 1 位，一个互斥参数占 8 位。inspect_avatar 的 parameterBits 是预估的总位数：参数资产里已有的（parameterBitsAsset），加上头像上现有的 Modular Avatar 菜单项和 MA Parameters 在构建时会生成的同步参数。parameterBitsAtLeast=true 表示头像上有 VRCFury 组件，它的参数估不出来，实际只多不少。面捕模型常常已经接近上限，加开关之前先看余量。build_menu 结果里的 parameterBits 是做完之后的预估值，超过 256 时每次都会写进 warnings。结果里有 warnings 就转告玩家。

# 看一眼：look
- look 给头像拍照，截图同时显示给玩家。装完素材、做完菜单之后拍一次正面和背面（views 写 front、back），确认：东西在该在的位置，没有明显穿模，没有整块洋红色（材质丢失）或纯白（贴图没挂上）。要看细节时用 target 只拍那一件，或者加 face、left、right 这些角度。
- 拍照是为了确认结果，不用每一步都拍；同一处最多拍两三次。
- 编辑模式下拍到的是场景里现在显示着的东西，菜单开关的效果没有算进去。头像上同时放着几件互斥的衣服或头发时，用 hide 把别的临时藏起来再看这一件；默认隐藏的衣服、道具用 show 临时显示。show / hide 只在拍照时生效，不改场景。
- 正面图里，头像的左手在画面右边。
- 只说你确实看到的东西（截图，或视觉模型的描述）。结果里写着你没有看到画面时，不要描述画面，也不要说「看起来没问题」：如实告诉玩家这次你看不了图，请他自己看记录里的截图，并转告结果里说的设置办法。
- 看出问题（穿模、错位、材质丢失）时，告诉玩家是哪一件、在什么位置；工具能修的再修，修不了的不要硬改。骨骼物理和菜单开关的动态效果截图里看不出来，仍然要玩家在 Play 模式里确认。
- 要看的是 Game 视图或 Scene 视图本身（而不是头像）时，可以用 UnitySkills 的 scene_screenshot、camera_sceneview_screenshot、camera_screenshot，它们的图同样会交给你看；它们会在工程的 Assets/Screenshots 里留下图片文件，所以看头像优先用 look。

# 其他改模操作：UnitySkills
- 玩家要的事情超出上面几个工具时，用 unity_find_skills 按意图找操作，看清参数再用 unity_skill 调用。
- 先查后改。会删除东西、保存场景、改动工程里的文件、进入 Play 模式或批量改导入设置的操作，先向玩家说明要做什么、为什么。这类操作执行前软件还会让玩家点一次确认；玩家没同意时不要换办法绕过去。
- 返回 MODE_FORBIDDEN 表示玩家的 UnitySkills 权限模式不允许这个操作：告诉玩家可以在 Unity 的 Window > UnitySkills 面板里调整权限模式，不要换别的办法绕过。
- 不要对头像做采样动画（SampleAnimation）一类的操作，会把人形骨架压坏。

# 收尾
装完素材、做完菜单之后，用 checkup 做一次上传前体检（不改场景）。「需处理」的每一项都用玩家听得懂的话说清楚：是什么问题、不处理会怎样、该怎么办；「建议优化」的挑要紧的说。体检给的是构建前的数值，转述时说明上传时 Modular Avatar、AAO、VRCFury 还会改动它们；整体评级照结果里的写法说（例如「较差（Poor）」）。

做完后用几句话说明：装了什么、菜单是什么样、从截图里看到了什么（没看图就说没看）、有哪些警告要注意。有没做的事（要用素材自带工具安装的面捕、没有 prefab 的皮肤或表情、对不上素体的素材）逐条说清楚还需要玩家做什么。确认过的和没确认的分开说：工具结果里写明的、截图里看到的算确认过；菜单开关的动态效果、骨骼物理、能不能上传都还没确认，不要用一句「没问题」带过。同步参数或 PhysBone 接近、超过 256 时写出具体数字（同步参数是预估值，照「预计」「至少」的说法转述）。最后提醒玩家：菜单效果要在 Unity 的 Play 模式里用 Gesture Manager 点一遍；满意就按 Ctrl+S 保存场景，不满意按 Ctrl+Z 或点「撤销上一步」。`

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
	{Name: "inspect_avatar", Desc: "看 Unity 里打开的场景：有哪些头像，头像下面有哪些物体（衣服、网格、菜单），现有的菜单和参数是什么样，同步参数位数（parameterBits，预估值，已计入现有 Modular Avatar 菜单项在构建时生成的参数）和 PhysBone 数量用了多少。不改任何东西。",
		Params: obj(map[string]any{"avatar": prop("string", avatarArg)})},
	{Name: "inspect_object", Desc: "看头像下面某个物体（一般是一件衣服）里的每个网格：路径、是否显示、材质名、离地高度范围、面数。用它来决定部件怎么分组。不改任何东西。",
		Params: obj(map[string]any{"path": prop("string", "物体在头像下面的路径，例如 Sailor 或 Sailor/Jacket"), "avatar": prop("string", avatarArg)}, "path")},
	{Name: "list_prefabs", Desc: "列出工程里某些文件夹下的 prefab：路径、网格名、是不是整只模型、有没有配好 Modular Avatar，以及它带的脚本（scripts）、是不是自带安装组件的插件（selfInstalling）、有没有缺失的脚本（missingScripts）、是不是面捕（faceTracking）。",
		Params: obj(map[string]any{"folders": strList("文件夹，例如 Assets/店铺名/衣服名"), "limit": prop("integer", "最多列多少个，默认 60")}, "folders")},
	{Name: "dress", Desc: "把一个衣服 prefab 穿到头像上：放到头像下面，用 Modular Avatar 配好骨架。同一个 prefab 已经在头像上时直接用现有的；头像下已有别的同名物体时新物体会换名，以结果里的 outfit.object 为准。可以用 Ctrl+Z 撤销。",
		Params: obj(map[string]any{"prefab": prop("string", "prefab 的路径，来自 list_prefabs"), "name": prop("string", "穿上后物体叫什么，不写就用 prefab 的名字。道具、插件不要写（它们的动画按原名找物体）"),
			"active": prop("boolean", "穿上后在场景里显示还是隐藏。不写就是显示。只对这次新放上去的物体生效，已经在头像上的不改"), "avatar": prop("string", avatarArg)}, "prefab")},
	{Name: "place_prefab", Desc: "把一个插件 prefab（面捕、表情、光影、互动这类自带安装组件的）原样放到头像下：保持原名和内部结构，不对骨架，不加开关。同一个 prefab 已经在头像上时直接用现有的。有缺失脚本或头像下已有同名物体时会拒绝并说明原因。可以用 Ctrl+Z 撤销。",
		Params: obj(map[string]any{"prefab": prop("string", "prefab 的路径，来自 list_prefabs"), "avatar": prop("string", avatarArg)}, "prefab")},
	{Name: "build_menu", Desc: "按计划生成或更新头像的菜单开关（Modular Avatar），并给每一项拍图标。可以用 Ctrl+Z 撤销。",
		Params: obj(map[string]any{
			"items": map[string]any{"type": "array", "description": "菜单项，按显示顺序写", "items": obj(map[string]any{
				"kind":          map[string]any{"type": "string", "enum": []string{"outfit", "part", "toggle", "strip", "skin", "install"}, "description": "outfit=穿这件（同一参数上互斥：衣服用 Clothtoggle，头发用 Hair_Choose）；part=隐藏一组部件；toggle=独立开关（配饰、道具，选中=显示）；strip=一键脱光；skin=换皮肤（Skin_Choose 上互斥，只换材质）；install=把插件自带的菜单挪到这一层"},
				"path":          strList("这一项所在的子菜单，从主菜单往下，例如 [\"衣服\", \"水手服\"]"),
				"label":         prop("string", "这一项显示的名字"),
				"objects":       strList("它管的物体在头像下面的路径。outfit 写衣服的根物体，part 写一组网格，toggle 写配饰或道具的根物体，strip 一般不用写（写了就是一并关掉的物体），skin 配合 materialsFrom 时写素体的身体网格，install 写 place_prefab 返回的插件根物体"),
				"default":       prop("boolean", "outfit、skin：进游戏默认选这一项（每个参数最多一个）；toggle：进游戏默认显示（配饰 true，道具 false）"),
				"show":          prop("boolean", "只用于 part：true 表示选中时显示（默认是选中时隐藏）"),
				"parameter":     prop("string", "只用于 outfit/strip/skin：互斥用的参数名。不写就用整个计划的 parameter（skin 不写就是 Skin_Choose）；头发写 Hair_Choose"),
				"materialsFrom": prop("string", "outfit 的换色项：另一个配色的 prefab 路径；skin：带着皮肤材质的 prefab 路径"),
				"materials": map[string]any{"type": "array", "description": "只用于 skin：逐个写明要换的材质槽", "items": obj(map[string]any{
					"object": prop("string", "网格物体在头像下面的路径，例如 Body"), "slot": prop("integer", "第几个材质槽，从 0 起算"), "material": prop("string", "材质文件在工程里的路径，以 Assets/ 开头"),
				}, "object", "slot", "material")},
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
	{Name: "checkup", Desc: "上传前体检：头像现在会不会被拒绝上传（同步参数、PhysBone、丢失的脚本、菜单单页的控件数），有没有粉色或缺失的材质，以及各项性能数值和 VRChat SDK 给的评级。结果同时显示给玩家。不改任何东西，也不触发构建，数值是构建前的。",
		Params: obj(map[string]any{"avatar": prop("string", avatarArg)})},
	{Name: "checkup_fix", Desc: "体检的一键处理。kind=textures：把头像用到的 4096 像素贴图的 Max Size 降到 2048（改的是整个工程共用的导入设置，用到同一张贴图的其他头像和场景也跟着变；不在 Ctrl+Z 的记录里，玩家可以在体检面板点「恢复」改回去）；lights：移除头像下的所有灯光组件（物体保留）；missing：移除丢失脚本的组件——脚本丢失多半是 Modular Avatar、VRCFury 这类插件没装或编译出错，那种情况不要用它，让玩家先装好或修好插件（移除后保存了场景，组件上的设置就找不回来了）；只有玩家确认这些组件不要了才用（lights 和 missing 在保存场景前可以用 Ctrl+Z 撤销）。执行前软件会让玩家点一次确认；做完自动重新体检，结果同时显示给玩家。粉色材质、同步参数和 PhysBone 超限、菜单页数、包围盒没有一键处理，只能告诉玩家怎么办。",
		Params: obj(map[string]any{"kind": map[string]any{"type": "string", "enum": []string{"textures", "lights", "missing"}, "description": "处理哪一项"},
			"paths": strList("textures：只处理这些贴图（工程里的路径，来自 checkup 结果的 p）；不写就处理头像用到的全部 4096 像素贴图"), "avatar": prop("string", avatarArg)}, "kind")},
	{Name: "undo", Desc: "撤销你在 Unity 里做的上一步改动（dress、build_menu 或 UnitySkills 操作）。撤销记录里最近的一步不是你做的（玩家自己改过东西）时不会执行，并说明原因。", Params: obj(map[string]any{})},
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

// knownNames: a plan the AI wrote, with the names the avatar's menu already has for the same things in
// another language (localMenu). Only when inspect_avatar has shown that menu; otherwise the plan as it is.
func (s *aiSession) knownNames(avatar, root string, items any) any {
	list, ok := items.([]any)
	s.mu.Lock()
	tree, seen := s.menus[avatar]
	if avatar == "" && len(s.menus) == 1 {
		for _, t := range s.menus {
			tree, seen = t, true
		}
	}
	s.mu.Unlock()
	if !ok || !seen {
		return items
	}
	maps := make([]map[string]any, 0, len(list))
	for _, it := range list {
		m, ok := it.(map[string]any)
		if !ok {
			return items
		}
		maps = append(maps, m)
	}
	out := make([]any, 0, len(maps))
	for _, m := range localMenu(menuLang(), tree, root, maps) {
		out = append(out, m)
	}
	return out
}

// toolTitle: what the step list says a call is doing.
func toolTitle(name string, a map[string]any) string {
	switch name {
	case "inspect_avatar":
		return "查看模型和现有菜单"
	case "inspect_object":
		return "查看「" + argStr(a, "path") + "」中的网格"
	case "list_prefabs":
		return "查找 prefab：" + strings.Join(argStrs(a, "folders"), "、")
	case "dress":
		return "装配「" + strings.TrimSuffix(baseName(argStr(a, "prefab")), ".prefab") + "」"
	case "place_prefab":
		return "放置「" + strings.TrimSuffix(baseName(argStr(a, "prefab")), ".prefab") + "」"
	case "build_menu":
		n := 0
		if l, ok := a["items"].([]any); ok {
			n = len(l)
		}
		return fmt.Sprintf("生成菜单（%d 项）", n)
	case "place_avatar":
		return "将素体「" + strings.TrimSuffix(baseName(argStr(a, "prefab")), ".prefab") + "」放入新场景"
	case "look":
		t := "截图检查模型"
		if x := argStr(a, "target"); x != "" {
			t = "截图检查「" + baseName(x) + "」"
		}
		return t
	case "undo":
		return "撤销上一步"
	case "checkup":
		return "上传前体检"
	case "checkup_fix":
		return "一键处理：" + unity.FixKinds[argStr(a, "kind")]
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
	case "checkup":
		rec, err := unity.RunCheckup(ctx, p, argStr(a, "avatar"))
		if err != nil {
			return fail(err)
		}
		short = "全部通过"
		if f, w := rec.Result.Counts(); f+w > 0 {
			short = fmt.Sprintf("%d 项需处理，%d 项建议优化", f, w)
		}
		return rec.TextForAI(6000), short, true
	case "checkup_fix":
		kind := argStr(a, "kind")
		label, known := unity.FixKinds[kind]
		if !known {
			return fail(errors.New("没有这项一键处理：" + kind))
		}
		s.mu.Lock()
		allowed := s.allowAll
		s.mu.Unlock()
		// the same gate as a risky UnitySkills operation: the player says yes first. Removing components whose
		// script is missing is asked every time, 「本次对话始终允许」 or not: what it removes cannot be told from
		// a plugin that is only not installed, and is lost for good once the scene is saved
		if !allowed || kind == "missing" {
			if !s.confirm(ctx, "AI 请求执行体检的一键处理「"+label+"」\n"+fixAsk(p, kind, a)) {
				return "玩家没有同意执行这个操作。不要换别的办法去做同一件事；先问玩家想怎么办。", "未获允许，未执行", false
			}
		}
		args := map[string]any{"avatar": argStr(a, "avatar")}
		if ps := argStrs(a, "paths"); len(ps) > 0 {
			args["paths"] = ps
		} else {
			args["all"] = true
		}
		fix, rec, recheck, err := unity.RunFix(ctx, p, kind, args)
		if err != nil {
			return fail(err)
		}
		NoteFix(p, fix) // one "MioVRCA …" step for 「撤销上一步」
		text, short := fix.Text(), strings.SplitN(fix.Text(), "\n", 2)[0]
		if recheck != nil {
			text += "\n重新体检失败：" + recheck.Error()
		} else if rec != nil {
			text += "\n\n重新体检：" + rec.TextForAI(4000)
		}
		return text, short, true
	case "inspect_avatar":
		raw, err := bridge("inspect", map[string]any{"avatar": argStr(a, "avatar")}, 0)
		if err != nil {
			return fail(err)
		}
		var o struct {
			Avatars []struct {
				Name     string `json:"name"`
				Prefab   string `json:"prefab"`
				Children []any  `json:"children"`
			} `json:"avatars"`
		}
		_ = json.Unmarshal(raw, &o)
		short = fmt.Sprintf("场景中有 %d 个模型", len(o.Avatars))
		var seen struct {
			Avatars []avatarInfo `json:"avatars"`
		}
		_ = json.Unmarshal(raw, &seen)
		s.mu.Lock()
		s.menus = map[string][]menuNode{}
		for _, av := range seen.Avatars {
			s.menus[av.Name] = av.MaMenu
		}
		s.mu.Unlock()
		if len(o.Avatars) == 1 {
			s.recAvatar(o.Avatars[0].Name, o.Avatars[0].Prefab)
			short = fmt.Sprintf("模型「%s」，含 %d 个子物体", o.Avatars[0].Name, len(o.Avatars[0].Children))
		}
		return inspectForAI(raw, 40000), short, true
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
			short += "；未找到文件夹 " + strings.Join(o.NotFound, "、")
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
			Changed  *bool    `json:"changed"`
			Warnings []string `json:"warnings"`
			Outfit   struct {
				Object string `json:"object"`
				Meshes []any  `json:"meshes"`
			} `json:"outfit"`
		}
		_ = json.Unmarshal(raw, &o)
		changed := o.Changed != nil && *o.Changed || o.Changed == nil && !o.Existing // an older plugin does not say
		if changed {
			s.changed(false)
		}
		if !o.Existing {
			s.did(func(d *runDone) { d.dressed = append(d.dressed, o.Outfit.Object) })
		}
		step := recStep{Prefab: argStr(a, "prefab"), Object: o.Outfit.Object}
		if v, ok := a["active"].(bool); ok {
			step.Active = &v
		}
		s.recStep(step, changed)
		short = fmt.Sprintf("「%s」%s，%d 个网格", o.Outfit.Object, map[bool]string{true: "已在模型上", false: "已装配"}[o.Existing], len(o.Outfit.Meshes))
		if len(o.Warnings) > 0 {
			short += "。注意：" + strings.Join(o.Warnings, "；")
		}
		return string(unity.Clip(core.CompactJSON(raw), 40000)), short, true
	case "build_menu":
		a["items"] = s.knownNames(argStr(a, "avatar"), argStr(a, "root"), a["items"])
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
		changed, said := o["changed"].(bool)
		if changed || !said { // an older plugin does not say: counted, as before
			s.changed(false)
			s.did(func(d *runDone) { d.menus++ })
		}
		short = fmt.Sprintf("新建 %d 项，图标 %d 张", len(created), int(icons))
		switch {
		case said && !changed:
			short = "菜单已是最新，未作改动"
		case len(created) == 0 && said:
			short = "已更新现有菜单项，无新建项"
		case len(created) == 0:
			short = "菜单已是最新，无新建项"
		}
		for _, w := range warns {
			short += "。注意：" + fmt.Sprint(w)
		}
		s.recMenu(argStr(a, "root"), argStr(a, "parameter"), a["items"], changed || !said)
		b, _ := json.Marshal(o)
		return string(unity.Clip(b, 24000)), short, true
	case "place_prefab":
		raw, err := bridge("place", map[string]any{"avatar": argStr(a, "avatar"), "prefab": argStr(a, "prefab")}, 3*time.Minute)
		if err != nil {
			return fail(oldPlugin(err))
		}
		var o placed
		_ = json.Unmarshal(raw, &o)
		if o.Changed {
			s.changed(false)
			s.did(func(d *runDone) { d.dressed = append(d.dressed, o.Object) })
		}
		s.recStep(recStep{Prefab: argStr(a, "prefab"), Object: o.Object, Place: true}, o.Changed)
		short = fmt.Sprintf("「%s」%s", o.Object, map[bool]string{true: "已在模型上", false: "已放置"}[o.Existing])
		if len(o.Warnings) > 0 {
			short += "。注意：" + strings.Join(o.Warnings, "；")
		}
		return string(unity.Clip(core.CompactJSON(raw), 40000)), short, true
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
		return string(raw), "新场景 " + o.Scene + "，模型「" + o.Avatar + "」", true
	case "look":
		return s.look(ctx, a)
	case "undo":
		what, err := s.undo(ctx, 0)
		if err != nil {
			return fail(err)
		}
		j, _ := json.Marshal(map[string]any{"undone": what})
		return string(j), "已撤销：" + what, true
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
		man := skillsManifest(ctx, p)
		if why := consentReason(man, skill); why != "" && !allowed && !shot {
			j, _ := json.Marshal(args)
			if r := []rune(string(j)); len(r) > 300 {
				j = []byte(string(r[:300]) + "…")
			}
			q := "AI 请求在 Unity 中执行「" + skill + "」"
			if len(args) > 0 {
				q += "：" + string(j)
			}
			if !s.confirm(ctx, q+"\n"+why+"。") {
				return "玩家没有同意执行这个操作。不要换别的办法去做同一件事；先问玩家想怎么办。", "未获允许，未执行", false
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
			return out + "\n（没有拿到截图的图片数据。）", "未获取到截图", true
		}
		// only what Ctrl+Z takes back is counted: not a look, not a change to the project's files alone
		if k, known := man[skill]; !known || (!k.ReadOnly && (k.MutatesScene || !k.MutatesAssets)) {
			s.changed(true)
			s.did(func(d *runDone) { d.skills++ })
		}
		return out, "", true
	}
	return fail(errors.New("未知工具：" + name))
}

// inspectForAI: the overview of the scene as the AI gets it. What it decides by comes first (the avatar's
// numbers, its Modular Avatar menu), the long list of objects last; when it is too long, whole entries of the
// long lists are left out and counted, so what goes back is always JSON that can be read.
func inspectForAI(raw json.RawMessage, max int) string {
	var top map[string]json.RawMessage
	var avatars []map[string]json.RawMessage
	if json.Unmarshal(raw, &top) != nil || (len(top["avatars"]) > 0 && json.Unmarshal(top["avatars"], &avatars) != nil) {
		return string(unity.Clip(core.CompactJSON(raw), max))
	}
	first := []string{"name", "path", "active", "scene", "prefab", "eyeHeight", "parameterBits", "parameterBitsAsset", "parameterBitsAtLeast", "physBones",
		"menuAsset", "parametersAsset", "maMenu"}
	last := []string{"descriptorMenu", "children"}
	ordered := func(m map[string]json.RawMessage, first, last []string) []byte {
		var b bytes.Buffer
		b.WriteByte('{')
		put := func(k string) {
			v, ok := m[k]
			if !ok {
				return
			}
			if b.Len() > 1 {
				b.WriteByte(',')
			}
			kb, _ := json.Marshal(k)
			b.Write(kb)
			b.WriteByte(':')
			b.Write(core.CompactJSON(v))
		}
		for _, k := range first {
			put(k)
		}
		var rest []string
		for k := range m {
			if !core.ContainsStr(first, k) && !core.ContainsStr(last, k) {
				rest = append(rest, k)
			}
		}
		sort.Strings(rest)
		for _, k := range append(rest, last...) {
			put(k)
		}
		b.WriteByte('}')
		return b.Bytes()
	}
	build := func() []byte {
		list := make([]json.RawMessage, 0, len(avatars))
		for _, a := range avatars {
			list = append(list, ordered(a, first, last))
		}
		t := map[string]json.RawMessage{}
		for k, v := range top {
			t[k] = v
		}
		t["avatars"], _ = json.Marshal(list)
		return ordered(t, []string{"modularAvatar", "vrcSdk", "playing"}, []string{"avatars"})
	}
	out := build()
	// shorten: the objects list from its end, then the descriptor's own menu, then the menu tree itself
	trim := func(a map[string]json.RawMessage, key, note string) bool {
		var list []json.RawMessage
		if json.Unmarshal(a[key], &list) != nil || len(list) == 0 {
			return false
		}
		total := len(list)
		var before struct {
			N int `json:"total"`
		}
		if json.Unmarshal(a[key+"Omitted"], &before) == nil && before.N > total {
			total = before.N
		}
		list = list[:len(list)*3/4]
		a[key], _ = json.Marshal(list)
		a[key+"Omitted"], _ = json.Marshal(map[string]any{"total": total, "listed": len(list), "note": note})
		return true
	}
	for _, step := range [][2]string{{"children", "物体太多，只列出前面一部分；要看某个物体用 inspect_object"}, {"descriptorMenu", "原厂菜单较长，只列出前面一部分"},
		{"maMenu", "菜单较长，只列出前面一部分；用 inspect_avatar 的 avatar 参数只看一个头像，或在 Unity 里查看"}} {
		for len(out) > max {
			cut := false
			for i := len(avatars) - 1; i >= 0 && !cut; i-- {
				cut = trim(avatars[i], step[0], step[1])
			}
			if !cut {
				break
			}
			out = build()
		}
	}
	if len(out) > max { // still too long (many avatars, or one huge value): only what each avatar is
		for _, a := range avatars {
			for k := range a {
				if !core.ContainsStr(first[:10], k) {
					delete(a, k)
				}
			}
			a["omitted"], _ = json.Marshal("内容较长，只给出概要；用 avatar 参数指定一个头像再看")
		}
		out = build()
	}
	return string(out)
}

// changed: a step that changed the scene, so 「撤销上一步」 has something of ours to take back. Steps that only
// looked, or found everything in place already, are not counted: the button would undo something else.
func (s *aiSession) changed(skill bool) {
	s.mu.Lock()
	s.changes = append(s.changes, skill)
	s.mu.Unlock()
}

func (s *aiSession) undone() {
	s.mu.Lock()
	if n := len(s.changes); n > 0 {
		s.changes = s.changes[:n-1]
	}
	s.mu.Unlock()
}

// fixAsk: what the player is asked before the AI's one-click fix: what it will change, what cannot be taken
// back, and what to do instead when the fix is the wrong answer.
func fixAsk(project, kind string, a map[string]any) string {
	switch kind {
	case "lights":
		return "会移除模型下的全部灯光组件，可用 Ctrl+Z 撤销。"
	case "missing":
		return "会移除模型下丢失脚本的组件。" + unity.MissingFixWarn + "。保存场景前可用 Ctrl+Z 撤销。"
	}
	// textures: which avatar and how many, as the last check-up of that avatar counted them
	name, n := argStr(a, "avatar"), len(argStrs(a, "paths"))
	if rec := unity.LastCheckup(project); rec != nil && (name == "" || name == rec.Avatar || name == rec.Result.Path) {
		name = rec.Avatar
		for _, it := range rec.Result.Items {
			if it.ID != "textures" || n > 0 {
				continue
			}
			if it.Fixable != nil {
				n = *it.Fixable
			} else if it.Value != nil {
				n = int(*it.Value)
			}
		}
	}
	who := "模型"
	if name != "" {
		who = "模型「" + name + "」"
	}
	what := "将把" + who + "用到的全部 4096 像素贴图的 Max Size 改为 2048 并重新导入。"
	if len(argStrs(a, "paths")) > 0 {
		what = fmt.Sprintf("将把%s的 %d 张指定贴图的 Max Size 改为 2048 并重新导入。", who, n)
	} else if n > 0 {
		what = fmt.Sprintf("将把%s用到的 %d 张 4096 像素贴图的 Max Size 改为 2048 并重新导入。", who, n)
	}
	return what + unity.TextureFixWarn + "。导入设置不在 Unity 的撤销记录中，处理后可在体检面板中点击「恢复」改回原设置。"
}

// NoteFix: a one-click fix of the check-up was carried out (by the AI, or from the panel's button). One that
// is a step in Unity's undo history can be taken back with 「撤销上一步」 like the line's own steps.
func NoteFix(project string, fix *unity.FixResult) {
	if fix == nil || fix.Undo == "" {
		return
	}
	s := aiSessionFor(project)
	s.mu.Lock()
	s.fixes++
	s.mu.Unlock()
}

// undo takes the last step of ours back. The plugin only undoes a step of its own (or, when the last
// change was made through UnitySkills, one of UnitySkills') and says so when something else is on top. What
// the session counts follows what the plugin says it undid: a one-click fix is not one of the counted
// changes (nothing noted for 「保存为方案」 goes with it), anything else is the last of them.
func (s *aiSession) undo(ctx context.Context, wait time.Duration) (string, error) {
	s.mu.Lock()
	skill := len(s.changes) > 0 && s.changes[len(s.changes)-1]
	s.mu.Unlock()
	raw, err := unity.BridgeCall(ctx, s.project, "undo", map[string]any{"skills": skill}, wait)
	if err != nil {
		return "", err
	}
	var o struct {
		Undone string `json:"undone"`
	}
	_ = json.Unmarshal(raw, &o)
	if unity.IsFixUndo(o.Undone) {
		s.mu.Lock()
		if s.fixes > 0 {
			s.fixes--
		}
		s.mu.Unlock()
		return o.Undone, nil
	}
	s.undone()
	s.recUndone()
	return o.Undone, nil
}

func (s *aiSession) did(f func(d *runDone)) {
	s.mu.Lock()
	f(&s.done)
	s.mu.Unlock()
}

// abortText: what a run that ended half way leaves on the avatar and how to take it back, then why it ended.
func (s *aiSession) abortText(err error) string {
	s.mu.Lock()
	d := s.done
	s.mu.Unlock()
	if len(d.dressed) == 0 && d.menus == 0 && d.skills == 0 {
		return err.Error()
	}
	var parts []string
	if n := len(d.dressed); n > 0 {
		parts = append(parts, fmt.Sprintf("已装配 %d 件（%s）", n, strings.Join(d.dressed, "、")))
	}
	if d.menus > 0 {
		parts = append(parts, "菜单已生成或更新")
	} else {
		parts = append(parts, "菜单尚未生成")
	}
	if d.skills > 0 {
		parts = append(parts, fmt.Sprintf("另有 %d 项通过 UnitySkills 进行的改动", d.skills))
	}
	return "操作中途中断。已完成的改动仍保留在场景中（尚未保存）：" + strings.Join(parts, "，") +
		"。如需回退，每点击一次「撤销上一步」或在 Unity 中按一次 Ctrl+Z 撤销一步。\n中断原因：" + err.Error()
}

// retryStep: a line in the step list while a request to the AI service is sent again.
func (s *aiSession) retryStep(n, of int) func(ok bool) {
	s.add(AIStep{Kind: "tool", Tool: "retry", Text: fmt.Sprintf("服务繁忙，正在重试（%d/%d）", n, of), Busy: true})
	return func(ok bool) { s.finishStep(ok, map[bool]string{true: "已恢复", false: "仍未成功"}[ok]) }
}

// ---------- a run ----------

// one row of the pipeline's asset list: a folder (or prefab) of the project and what it is
type aiAsset struct {
	Folder string `json:"folder"`
	Kind   string `json:"kind"` // one of pipelineKinds
	Name   string `json:"name,omitempty"`
	// what the library knows about it (libraryBases): its record, and the base bodies it is made for; and, of
	// a plugin or a skin, what its folder holds
	key   string
	bases []string
	holds string
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
	b.WriteString("将所选素材装配到模型上，并生成菜单开关。\n")
	if len(assets) > 0 {
		b.WriteString("素材清单（类别：工程中的文件夹或 prefab）：\n")
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
					line += "；场景中没有模型时先将其放入场景"
				}
				if len(a.bases) > 0 {
					line += "；适配素体：" + strings.Join(a.bases, "、")
				}
				if a.holds != "" {
					line += "；" + a.holds
				}
				b.WriteString(line + "\n")
			}
		}
	} else {
		b.WriteString("未指定素材：请先询问素材位于工程的哪个文件夹、属于什么类别。\n")
	}
	b.WriteString("菜单层级：" + zhHierarchy(hierarchy) + "\n")
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
		return errors.New("上一个请求仍在进行，请等待其完成，或先点击「停止」")
	}
	hierarchy := strings.TrimSpace(req.Hierarchy)
	if hierarchy == "" {
		hierarchy = loadAIConfig().Hierarchy
	}
	text := strings.TrimSpace(req.Text)
	if req.Mode == "dress" {
		tidyAssets(&req)
		libraryBases(st, req.Project, req.Assets)
		text = pipelineTask(req.Assets, hierarchy, req.Text)
	}
	if text == "" {
		s.mu.Unlock()
		return errors.New("未填写要执行的内容")
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
	s.client, s.st, s.outImgs, s.done = client, st, nil, runDone{}
	if req.Mode == "dress" { // the line starts over: what is saved as a recipe is this run and what follows it
		s.rec = runRecord{assets: req.Assets, hierarchy: hierarchy}
	}
	s.mu.Unlock()
	if client != nil {
		client.retry = s.retryStep
	}
	if req.Mode == "dress" {
		h := hierarchy
		_ = aiSave("", "", "", nil, &h)
	}
	s.add(AIStep{Kind: "user", Text: text})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.add(AIStep{Kind: "error", Text: fmt.Sprint("程序内部错误：", r)})
			}
			cancel()
			s.mu.Lock()
			s.busy, s.cancel = false, nil
			s.mu.Unlock()
		}()
		var err error
		if req.Mode == "dress" { // what was just imported is on disk; Unity reads it in now
			s.add(AIStep{Kind: "tool", Tool: "refresh", Text: "刷新 Unity 资源以加载新导入的文件", Busy: true})
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
			s.add(AIStep{Kind: "error", Text: s.abortText(err)})
		}
	}()
	return nil
}

// How much of the conversation is sent as it is. Past aiHistorySoft the older tool answers are left out (the
// newest stay whole); past aiHistoryMax the conversation has to be started again.
var (
	aiHistorySoft = 120000
	aiHistoryMax  = 600000
)

const (
	agedMark  = "（较早的工具结果已省略"
	cutAnswer = "错误：这次调用的参数在模型的输出上限处被截断，没有执行。请缩短后重新调用：build_menu 可以分几次提交（每次只写一部分菜单项，同一位置的同名项会更新，不会重复创建），其他工具只写必要的参数。"
	badAnswer = "错误：参数不是合法的 JSON，重新写一次"
)

func historySize(turns []aiTurn) int {
	size := 0
	for _, t := range turns {
		size += len(t.Text) + len(t.blocks) + len(t.think)
		for _, k := range t.Calls {
			size += len(k.Args)
		}
		for _, r := range t.Results {
			size += len(r.Content)
		}
	}
	return size
}

// ageResults keeps a long conversation within the model's context: the answers of the last few tool turns stay
// as they are, older long ones (and their pictures) give way to a line saying so — the AI can ask again.
// hard: the service has refused the conversation as too long, so only the newest turn stays, cut to size.
func (s *aiSession) ageResults(hard bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	size, soft := historySize(s.turns), aiHistorySoft
	if s.soft > 0 && s.soft < soft {
		soft = s.soft
	}
	if hard { // this model takes less than was assumed: remembered, so the next rounds are not refused again
		s.soft = size * 4 / 5
	} else if size <= soft {
		return false
	}
	age := func(keep, long int) (any bool) {
		for i := len(s.turns) - 1; i >= 0; i-- {
			t := &s.turns[i]
			if t.Role != "tool" {
				continue
			}
			if keep > 0 {
				keep--
				for j := range t.Results {
					if r := &t.Results[j]; hard && len(r.Content) > 12000 {
						r.Content, any = string(unity.Clip([]byte(r.Content), 12000)), true
					}
				}
				continue
			}
			for j := range t.Results {
				r := &t.Results[j]
				if len(r.Images) > 0 {
					r.Images, any = nil, true
				}
				if len(r.Content) > long && !strings.HasPrefix(r.Content, agedMark) {
					r.Content, any = agedMark+"："+r.Name+"。需要时请重新调用该工具查看最新状态）", true
				}
			}
		}
		return any
	}
	if hard {
		return age(1, 300)
	}
	any := false
	for keep := 3; keep >= 1 && historySize(s.turns) > soft; keep-- { // three whole, fewer when even that is too much
		any = age(keep, 800) || any
	}
	return any
}

func (s *aiSession) copyTurns() []aiTurn {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]aiTurn{}, s.turns...)
}

// converse sends the player's text and lets the AI work until it has nothing more to call.
func (s *aiSession) converse(ctx context.Context, c *aiClient, text, hierarchy string) error {
	s.mu.Lock()
	keep := len(s.turns)
	s.turns = append(s.turns, aiTurn{Role: "user", Text: text})
	s.mu.Unlock()
	system := aiSystemPrompt(s.project, hierarchy)
	said := false
	lastBad := "" // the call that could not be read in the round before
	for round := 0; round < aiMaxRounds; round++ {
		s.agePictures()
		s.ageResults(false)
		turns := s.copyTurns()
		if historySize(turns) > aiHistoryMax {
			return errors.New("对话过长，请点击「新对话」重新开始（已完成的改动仍保留在 Unity 中）")
		}
		out, err := c.chat(ctx, system, turns, aiTools)
		var he *aiHTTPError
		switch {
		case errors.As(err, &he) && he.overflow():
			// too long for this model: once more with the older tool answers left out
			if s.ageResults(true) {
				out, err = c.chat(ctx, system, s.copyTurns(), aiTools)
			}
			if errors.As(err, &he) && he.overflow() {
				err = errors.New(he.Msg + "。较早的工具结果已自动省略，但仍超出上限：请点击「新对话」重新开始（已完成的改动仍保留在 Unity 中），或更换上下文更长的模型")
			}
		case errors.As(err, &he) && he.noPictures() && s.dropPictures():
			// the service does not take pictures after all: once more without them
			if out, err = c.chat(ctx, system, s.copyTurns(), aiTools); err == nil { // it was the pictures: remembered
				rememberSees(seesKey(c), false)
				s.add(AIStep{Kind: "error", Text: "当前 AI 模型不接受图片，截图未发送给 AI（截图仍显示在上方记录中）。如需 AI 检查画面，请在「AI 服务」的「识图」中配置视觉模型。"})
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
			out.Text, out.blocks = "（AI 未返回内容）", nil
		}
		// A call the output limit cut off (the last one of such a turn), or one whose arguments are not JSON, is
		// not carried out. In the history its arguments become "{}": sent back as they came, a strict service
		// refuses every request from then on.
		unread := map[int]string{}
		for i := range out.Calls {
			k := &out.Calls[i]
			var args map[string]any
			valid := len(k.Args) == 0 || json.Unmarshal(k.Args, &args) == nil
			switch {
			case out.cut && i == len(out.Calls)-1:
				unread[i] = "cut"
			case !valid:
				unread[i] = "bad"
			}
			if !valid {
				k.Args = json.RawMessage("{}")
			}
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
		bad := ""
		for i, k := range out.Calls {
			if ctx.Err() != nil {
				res.Results = append(res.Results, aiResult{ID: k.ID, Name: k.Name, Content: "玩家停止了这次操作", IsErr: true})
				continue
			}
			if why := unread[i]; why != "" {
				answer, short := badAnswer, "AI 提供的参数格式有误，已要求重新生成"
				if why == "cut" {
					answer, short = cutAnswer, "参数在模型的输出上限处被截断，未执行，已要求 AI 缩短后重试"
				}
				res.Results = append(res.Results, aiResult{ID: k.ID, Name: k.Name, Content: answer, IsErr: true})
				s.add(AIStep{Kind: "tool", Tool: k.Name, Text: toolTitle(k.Name, nil), Out: short})
				bad = why + " " + k.Name
				continue
			}
			var args map[string]any
			_ = json.Unmarshal(k.Args, &args)
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
		if bad != "" && bad == lastBad { // the same thing twice: a third try would end the same way
			if strings.HasPrefix(bad, "cut") {
				return errors.New("AI 的工具调用连续两次在输出上限处被截断，已停止。请更换输出上限更高的模型，或将要求拆分后重试（例如一次只装配一件、菜单分几次生成）")
			}
			return errors.New("AI 连续两次给出格式有误的工具参数，已停止。请重试，或更换模型")
		}
		lastBad = bad
	}
	return fmt.Errorf("AI 已连续执行 %d 步仍未完成，已暂停：请检查 Unity 中的结果，再告知 AI 后续操作", aiMaxRounds)
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
	// what its components say (plugin 1.2.4 and later): a plugin that installs itself, face tracking, scripts
	// the project lacks, MA Menu Installers of its own
	Scripts        []string `json:"scripts,omitempty"`
	SelfInstalling bool     `json:"selfInstalling,omitempty"`
	FaceTracking   bool     `json:"faceTracking,omitempty"`
	VRCFury        bool     `json:"vrcFury,omitempty"`
	MissingScripts int      `json:"missingScripts,omitempty"`
	MenuInstallers int      `json:"menuInstallers,omitempty"`
}

type menuNode struct {
	Object    string     `json:"object"`
	Label     string     `json:"label"`
	Type      string     `json:"type"`
	Parameter string     `json:"parameter"`
	Auto      bool       `json:"auto"`
	Default   bool       `json:"default"`
	Toggles   []string   `json:"toggles"`
	Strip     string     `json:"strip"` // "take everything off": the parameter whose outfits it takes off
	Children  []menuNode `json:"children"`
}

type avatarInfo struct {
	Name     string        `json:"name"`
	Path     string        `json:"path"`
	Active   bool          `json:"active"`
	Prefab   string        `json:"prefab"`
	FT       bool          `json:"faceTracking"` // face tracking is on it already
	MaMenu   []menuNode    `json:"maMenu"`
	Children []avatarChild `json:"children"`
}

// avatarChild: a direct child of the avatar, as inspect tells it.
type avatarChild struct {
	Name       string   `json:"name"`
	Kind       string   `json:"kind"`
	Active     bool     `json:"active"`
	Prefab     string   `json:"prefab"`
	Components []string `json:"components"`
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
// category slot ({分类}: 衣服, 头发, 皮肤, 表情, 配饰, 道具, 光影, 互动), the per-asset slot ({素材}, the older
// {衣服}) and the switches.
type hierLevel struct {
	lit  string // a sub menu's name
	slot string // "cat", "asset", "items", or "" for a name
}

// the menu a kind of asset goes under, and the names a player may write for that level
var kindCategory = map[string]string{"衣服": "衣服", "头发": "头发", "皮肤": "皮肤", "表情": "表情", "配饰": "配饰", "道具": "道具", "光影": "光影", "互动": "互动"}
var categoryWords = map[string]bool{"衣服": true, "服装": true, "换装": true, "头发": true, "发型": true, "配饰": true, "饰品": true, "道具": true,
	"皮肤": true, "表情": true, "光影": true, "互动": true}

// isCategoryWord: a level a player wrote stands for the category, in any of the interface's languages.
func isCategoryWord(s string) bool {
	if categoryWords[s] {
		return true
	}
	for zh := range kindCategory {
		if s != zh && sameMenuWord(s, zh) {
			return true
		}
	}
	return false
}

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
		items := strings.Contains(inner, "开关") || strings.Contains(inner, "部件") || strings.Contains(low, "toggle") || strings.Contains(low, "switch") || strings.Contains(inner, "トグル") || strings.Contains(inner, "スイッチ")
		cat := slot && (strings.Contains(inner, "分类") || strings.Contains(inner, "类别") || strings.Contains(inner, "类型") || low == "category" || low == "kind" || strings.Contains(inner, "カテゴリ") || strings.Contains(inner, "種類"))
		asset := each || (slot && (strings.Contains(inner, "素材") || strings.Contains(inner, "衣服") || strings.Contains(inner, "服装") || low == "outfit" || low == "asset" || low == "item" || strings.Contains(inner, "アセット") || strings.Contains(inner, "衣装")))
		switch {
		case seg == "" || inner == "主菜单" || inner == "根菜单" || low == "main menu" || low == "root" || inner == "メインメニュー" || inner == "ルート":
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
			if l.slot == "" && isCategoryWord(l.lit) {
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
	version string       // the folder that sets it apart from the same outfit's versions for other base bodies…
	others  []string     // …and theirs: left out, because only one version of an outfit is put on
}

// pickOutfits: which prefabs of the folders are outfits to put on, colours of one outfit together.
func pickOutfits(list []prefabInfo, aliases []string) (out []quickOutfit, skipped []string) {
	var cands []prefabInfo
	for _, p := range list {
		if p.WholeAvatar {
			skipped = append(skipped, p.Name+"（完整模型）")
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
	// The base body is known, yet several versions of one outfit are left (the same prefabs in sibling folders
	// whose names say nothing the settings know): only the first is put on, the others are named.
	if len(aliases) > 0 {
		var one []quickOutfit
		at := map[string]int{}
		for _, o := range out {
			names := []string{strings.ToLower(o.base.Name)}
			for _, c := range o.colours {
				names = append(names, strings.ToLower(c.Name))
			}
			sort.Strings(names)
			meshes := append([]string{}, o.base.MeshNames...)
			sort.Strings(meshes)
			k := fmt.Sprint(o.base.Renderers, "|", strings.Join(names, "|"), "||", strings.Join(meshes, "|"))
			i, seen := at[k]
			if !seen {
				at[k] = len(one)
				one = append(one, o)
				continue
			}
			f := &one[i]
			if f.version == "" {
				f.version = versionFolder(f.base.Path, o.base.Path)
			}
			other := versionFolder(o.base.Path, f.base.Path)
			f.others = append(f.others, other)
			skipped = append(skipped, o.base.Name+"（"+other+" 版本）")
		}
		out = one
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

// versionFolder: the nearest folder of a prefab's path that another version of the same prefab does not share
// ("Assets/Shop/BodyB/Prefab/Dress.prefab" beside ".../BodyA/Prefab/Dress.prefab" → "BodyB").
func versionFolder(p, other string) string {
	a, b := strings.Split(path.Dir(p), "/"), strings.Split(path.Dir(other), "/")
	for i, j := len(a)-1, len(b)-1; i >= 0 && j >= 0; i, j = i-1, j-1 {
		if !strings.EqualFold(a[i], b[j]) {
			return a[i]
		}
	}
	return path.Base(path.Dir(p))
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
	menuLang = func() string { return storeLang(st) }
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
			fail(w, errors.New("该工程不在工程列表中"))
		}
		return p, ok
	}
	// the service as the form has it now (saved or not): a key left empty means the saved one
	formClient := func(b map[string]json.RawMessage) (*aiClient, error) {
		info := aiProvider(str(b, "provider"))
		if info == nil {
			return nil, errors.New("请先选择 AI 服务商")
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
				fail(w, errors.New("识图设置格式有误"))
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
			fail(w, errors.New("未填写模型名称"))
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
		note := fmt.Sprintf("%s 支持识图：已正确读出测试图中的数字（%.1f 秒）", c.model, time.Since(t0).Seconds())
		switch {
		case refused:
			note = c.model + " 不接受图片：带图片的请求被服务拒绝"
			if i := strings.Index(reply, "服务返回："); i >= 0 {
				note += "。" + reply[i:]
			}
		case !sees:
			note = c.model + " 不支持识图：其回复为「" + reply + "」"
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
				fail(w, errors.New("AI 正在该工程中执行操作，请先停止再移除"))
				return
			}
			if err := unity.RemoveAIKit(p); err != nil {
				fail(w, err)
				return
			}
			core.WriteJSON(w, map[string]any{"ok": true, "kit": unity.AIKitStatus(p), "note": "已移除 Unity 插件"})
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
			fail(w, errors.New("AI 正在执行操作，请等待其完成，或先点击「停止」"))
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		what, err := s.undo(ctx, 30*time.Second)
		if err != nil {
			fail(w, err)
			return
		}
		s.add(AIStep{Kind: "tool", Tool: "undo", Text: "撤销上一步", OK: true, Out: "已撤销：" + what})
		core.WriteJSON(w, map[string]any{"ok": true, "undone": what})
	})
}
