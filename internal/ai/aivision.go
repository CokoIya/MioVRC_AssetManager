package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/unity"
)

type aiImage struct {
	Mime string
	Data []byte
	Note string // what it shows ("正面")
}

// AIVision: how pictures reach the AI.
type AIVision struct {
	Mode    string `json:"mode"`    // "" = automatic, main = the main model looks, other = a second service describes, off
	Wire    string `json:"wire"`    // the second service: openai or claude
	BaseURL string `json:"baseUrl"` //
	Model   string `json:"model"`   //
}

const visionKeyID = "vision" // the second service's key, next to the providers' keys

func (v AIVision) otherReady(c AIConfig, keys map[string]string) bool {
	return strings.TrimSpace(v.BaseURL) != "" && strings.TrimSpace(v.Model) != "" && (visionKey(c, keys) != "" || isLocalHost(v.BaseURL))
}

// eyeFor: who describes pictures for a model that cannot see — the vision model the player set up, or else the
// provider's own (same address, same key: nothing to set up).
func eyeFor(c AIConfig, keys map[string]string) (v AIVision, key string, own, ok bool) {
	if c.Vision.otherReady(c, keys) {
		return c.Vision, visionKey(c, keys), false, true
	}
	info := aiProvider(c.Provider)
	if info == nil {
		return v, "", false, false
	}
	p := c.profile(info.ID)
	eye := providerEyes[info.ID]
	if eye == "" || eye == strings.TrimSpace(p.Model) || (keys[info.ID] == "" && !isLocalHost(p.BaseURL)) {
		return v, "", false, false
	}
	return AIVision{Wire: info.Wire, BaseURL: p.BaseURL, Model: eye}, keys[info.ID], true, true
}

// visionKey: the key entered for the second service; without one, the key of a provider at the same address
// (GLM for the work and GLM's vision model for the pictures need one key, not two).
func visionKey(c AIConfig, keys map[string]string) string {
	if k := keys[visionKeyID]; k != "" {
		return k
	}
	for _, p := range aiProviders {
		if keys[p.ID] != "" && sameService(c.profile(p.ID).BaseURL, c.Vision.BaseURL) {
			return keys[p.ID]
		}
	}
	return ""
}

// ---------- the pictures themselves ----------

// normImage: any PNG or JPEG as a JPEG no larger than max on its longer side.
func normImage(data []byte, max int, note string) (aiImage, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return aiImage{}, errors.New("截图读不出来：" + err.Error())
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return aiImage{}, errors.New("截图是空的")
	}
	if w > max || h > max {
		nw, nh := max, h*max/w
		if h > w {
			nw, nh = w*max/h, max
		}
		src = shrink(src, nw, nh)
	} else if _, ok := src.(*image.YCbCr); !ok { // flatten transparency onto a dark ground
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.Draw(dst, dst.Bounds(), &image.Uniform{color.RGBA{0x33, 0x36, 0x40, 0xff}}, image.Point{}, draw.Src)
		draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Over)
		src = dst
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, src, &jpeg.Options{Quality: 85}); err != nil {
		return aiImage{}, err
	}
	return aiImage{Mime: "image/jpeg", Data: out.Bytes(), Note: note}, nil
}

// shrink: an area average, good enough for making a picture smaller.
func shrink(src image.Image, nw, nh int) image.Image {
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		y0, y1 := y*h/nh, (y+1)*h/nh
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < nw; x++ {
			x0, x1 := x*w/nw, (x+1)*w/nw
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var r, g, bl, a, n uint64
			for yy := y0; yy < y1; yy++ {
				for xx := x0; xx < x1; xx++ {
					cr, cg, cb, ca := src.At(b.Min.X+xx, b.Min.Y+yy).RGBA()
					r, g, bl, a, n = r+uint64(cr), g+uint64(cg), bl+uint64(cb), a+uint64(ca), n+1
				}
			}
			// premultiplied already; what is transparent shows the dark ground
			inv := 0xffff - a/n
			dst.SetRGBA(x, y, color.RGBA{uint8((r/n + inv*0x33/0xff) >> 8), uint8((g/n + inv*0x36/0xff) >> 8), uint8((bl/n + inv*0x40/0xff) >> 8), 0xff})
		}
	}
	return dst
}

// 5×7 digits for the picture a model is asked to read
var digitRows = [10][7]uint8{
	{0x0E, 0x11, 0x13, 0x15, 0x19, 0x11, 0x0E}, {0x04, 0x0C, 0x04, 0x04, 0x04, 0x04, 0x0E}, {0x0E, 0x11, 0x01, 0x02, 0x04, 0x08, 0x1F},
	{0x1F, 0x02, 0x04, 0x02, 0x01, 0x11, 0x0E}, {0x02, 0x06, 0x0A, 0x12, 0x1F, 0x02, 0x02}, {0x1F, 0x10, 0x1E, 0x01, 0x01, 0x11, 0x0E},
	{0x06, 0x08, 0x10, 0x1E, 0x11, 0x11, 0x0E}, {0x1F, 0x01, 0x02, 0x04, 0x08, 0x08, 0x08}, {0x0E, 0x11, 0x11, 0x0E, 0x11, 0x11, 0x0E},
	{0x0E, 0x11, 0x11, 0x0F, 0x01, 0x02, 0x0C},
}

// probeImage: a picture of a three digit number (digits 2 to 9: nothing a model could take for a letter).
func probeImage() (aiImage, string) {
	digits := ""
	for i := 0; i < 3; i++ {
		digits += string(rune('2' + rand.Intn(8)))
	}
	const cell, pad = 14, 22
	w, h := pad*2+len(digits)*6*cell-cell, pad*2+7*cell
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)
	for i, d := range digits {
		rows := digitRows[d-'0']
		for y := 0; y < 7; y++ {
			for x := 0; x < 5; x++ {
				if rows[y]&(0x10>>x) != 0 {
					x0, y0 := pad+i*6*cell+x*cell, pad+y*cell
					draw.Draw(img, image.Rect(x0, y0, x0+cell, y0+cell), &image.Uniform{color.RGBA{0x15, 0x18, 0x22, 0xff}}, image.Point{}, draw.Src)
				}
			}
		}
	}
	var out bytes.Buffer
	_ = jpeg.Encode(&out, img, &jpeg.Options{Quality: 90})
	return aiImage{Mime: "image/jpeg", Data: out.Bytes(), Note: "测试图"}, digits
}

// ---------- can this model see? ----------

func seesKey(c *aiClient) string { return c.info.Wire + "|" + unity.RepoKey(c.base) + "|" + c.model }

// aiSeesTest shows the model a number and reports whether it read it. An error means the service refused the
// picture or could not be reached; refused is true when it said so itself (a 4xx about the request).
func aiSeesTest(ctx context.Context, c *aiClient) (sees bool, reply string, refused bool, err error) {
	img, digits := probeImage()
	out, err := c.chat(ctx, "", []aiTurn{{Role: "user", Text: "图片里写着一个三位数。只回答这个数字，不要说别的。看不到图片就回答：看不到。", Images: []aiImage{img}}}, nil)
	if err != nil {
		var he *aiHTTPError
		if errors.As(err, &he) && he.Status >= 400 && he.Status < 500 && he.Status != 401 && he.Status != 403 && he.Status != 429 {
			return false, err.Error(), true, nil
		}
		return false, "", false, err
	}
	reply = strings.TrimSpace(out.Text)
	if r := []rune(reply); len(r) > 60 {
		reply = string(r[:60]) + "…"
	}
	return strings.Contains(strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, out.Text), digits), reply, false, nil
}

// mainSees: does the main model see pictures? Asked once per address and model, then remembered.
func mainSees(ctx context.Context, c *aiClient) bool {
	key := seesKey(c)
	aiMu.Lock()
	known := loadAIConfigLocked().Sees[key]
	aiMu.Unlock()
	if known != "" {
		return known == "yes"
	}
	tctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	sees, reply, refused, err := aiSeesTest(tctx, c)
	if err != nil { // could not be asked: not remembered, not relied on
		core.Logf("看图测试没做成：%v", err)
		return false
	}
	core.Logf("看图测试：%s %s（%s）", c.model, map[bool]string{true: "看得到", false: "看不到"}[sees], reply)
	_ = refused
	rememberSees(key, sees)
	return sees
}

func rememberSees(key string, sees bool) {
	aiMu.Lock()
	defer aiMu.Unlock()
	c := loadAIConfigLocked()
	if c.Sees == nil {
		c.Sees = map[string]string{}
	}
	c.Sees[key] = map[bool]string{true: "yes", false: "no"}[sees]
	_ = saveAIConfigLocked(c)
}

// visionClient: the second service that describes pictures, when one is set up.
func visionClient(st *core.Store) (*aiClient, error) {
	aiMu.Lock()
	c := loadAIConfigLocked()
	key := visionKey(c, loadAIKeysLocked())
	aiMu.Unlock()
	return visionClientWith(st, c.Vision, key)
}

func visionClientWith(st *core.Store, v AIVision, key string) (*aiClient, error) {
	if strings.TrimSpace(v.BaseURL) == "" || strings.TrimSpace(v.Model) == "" {
		return nil, errors.New("还没有设置看图模型：填它的接口地址和模型名")
	}
	wire := v.Wire
	if wire != "claude" {
		wire = "openai"
	}
	info := aiProviderInfo{ID: "vision", Label: "看图模型", Wire: wire, Base: v.BaseURL}
	return aiClientWith(st, info, v.BaseURL, v.Model, key)
}

const describePrompt = `这是 Unity 里一个 VRChat 头像的截图%s。请只描述画面里看得到的东西，供改模的人检查：
- 头像整体：能不能看到完整的人形，姿势是否正常（T 字或 A 字站姿算正常）。
- 衣服、头发、配饰、道具：各自在不在该在的位置，有没有明显错位、悬空、穿到身体里面，或者身体从衣服里穿出来（穿模）。
- 材质：有没有整块洋红色 / 粉紫色（材质丢失）、纯白或纯灰（贴图没挂上）、全黑的部分。
- 其他明显不对的地方。
看不清或被挡住的就说看不清，不要猜。用中文，分条写，简短。%s`

// describeImages: the second service looks and says what it sees.
func describeImages(ctx context.Context, vc *aiClient, imgs []aiImage, question string) (string, error) {
	var notes []string
	for _, im := range imgs {
		if im.Note != "" {
			notes = append(notes, im.Note)
		}
	}
	views := ""
	if len(notes) > 0 {
		views = "（按顺序：" + strings.Join(notes, "、") + "）"
	}
	q := ""
	if q = strings.TrimSpace(question); q != "" {
		q = "\n特别要回答的问题：" + q
	}
	tctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	out, err := vc.chat(tctx, "", []aiTurn{{Role: "user", Text: fmt.Sprintf(describePrompt, views, q), Images: imgs}}, nil)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(out.Text) == "" {
		return "", errors.New("看图模型什么也没说")
	}
	return strings.TrimSpace(out.Text), nil
}

// present decides how the AI gets the pictures: attached (the main model looks), as a description, or not at
// all — then with the reason, which the AI passes on to the player. how is what the step list says.
func (s *aiSession) present(ctx context.Context, imgs []aiImage, question string) (text string, attach []aiImage, how string) {
	if len(imgs) == 0 {
		return "", nil, ""
	}
	aiMu.Lock()
	cfg := loadAIConfigLocked()
	keys := loadAIKeysLocked()
	aiMu.Unlock()
	v := cfg.Vision
	s.mu.Lock()
	main, st := s.client, s.st
	s.mu.Unlock()
	const unseen = "你没有看到画面：不要描述画面里的内容，也不要说看起来没问题。"
	if main == nil {
		return "", nil, ""
	}
	switch v.Mode {
	case "off":
		return "（玩家关掉了看图。截图已经显示给玩家，" + unseen + "请玩家自己看截图。）", nil, "只给你看（看图已关闭）"
	case "main":
		return "截图附在后面，按上面 views 的顺序。", imgs, "AI 直接看图"
	case "other":
	default:
		if mainSees(ctx, main) {
			return "截图附在后面，按上面 views 的顺序。", imgs, "AI 直接看图"
		}
	}
	eye, eyeKey, own, ok := eyeFor(cfg, keys)
	if !ok {
		return "（你现在用的模型（" + main.model + "）看不了图，玩家也没有设置看图模型。截图已经显示给玩家，" + unseen +
			"告诉玩家：截图在上面的记录里，请他自己看一下；想让你也能检查画面，可以在「AI 服务」设置的「看图」里填一个能看图的模型。）", nil, "只给你看（这个模型看不了图，还没有设置看图模型）"
	}
	vc, err := visionClientWith(st, eye, eyeKey)
	if err == nil {
		var desc string
		if desc, err = describeImages(ctx, vc, imgs, question); err == nil {
			return "你自己看不了图。下面是看图模型（" + vc.model + "）对这些截图的描述；它只描述画面，可能看漏或看错，拿不准的地方请玩家自己看截图确认：\n" + desc, nil, "由看图模型（" + vc.model + "）描述给 AI"
		}
	}
	if own {
		return "（请同一家的看图模型 " + eye.Model + " 来看图，但它没有回答：" + err.Error() + "。截图已经显示给玩家，" + unseen +
			"请玩家自己看截图；也可以在「AI 服务」的「看图」里另设一个看图模型。）", nil, "只给你看（" + eye.Model + " 没有回答）"
	}
	return "（看图模型没有回答：" + err.Error() + "。截图已经显示给玩家，" + unseen + "请玩家自己看截图。）", nil, "只给你看（看图模型没有回答）"
}

// looksNow: who looks at pictures as things are set up now.
func looksNow(c AIConfig, keys map[string]string) string {
	v := c.Vision
	_, _, _, hasEye := eyeFor(c, keys)
	other := map[bool]string{true: "other", false: "none"}[hasEye]
	switch v.Mode {
	case "off", "main":
		return v.Mode
	case "other":
		return other
	}
	info := aiProvider(c.Provider)
	if info == nil {
		return other
	}
	p := c.profile(c.Provider)
	switch c.Sees[info.Wire+"|"+unity.RepoKey(normBase(info.Wire, p.BaseURL))+"|"+strings.TrimSpace(p.Model)] {
	case "yes":
		return "main"
	case "no":
		return other
	}
	return "unknown"
}

// aiSaveVision: how pictures reach the AI; key == nil leaves the second service's key alone, "" removes it.
func aiSaveVision(v AIVision, key *string) error {
	switch v.Mode {
	case "", "main", "other", "off":
	default:
		return errors.New("不认识的看图方式：" + v.Mode)
	}
	if v.Wire != "claude" {
		v.Wire = "openai"
	}
	v.BaseURL, v.Model = strings.TrimSpace(v.BaseURL), strings.TrimSpace(v.Model)
	if v.BaseURL != "" {
		u, err := url.Parse(v.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errors.New("看图模型的接口地址要以 http:// 或 https:// 开头")
		}
	}
	aiMu.Lock()
	defer aiMu.Unlock()
	c := loadAIConfigLocked()
	keys := loadAIKeysLocked()
	if key == nil && keys[visionKeyID] != "" && v.BaseURL != "" && !sameService(c.Vision.BaseURL, v.BaseURL) {
		return errors.New("看图模型的接口地址换了：已保存的 Key 是给原来那个地址的，请重新填写这个地址的 Key")
	}
	if key != nil {
		if k := strings.TrimSpace(*key); k == "" {
			delete(keys, visionKeyID)
		} else {
			keys[visionKeyID] = k
		}
		if err := saveAIKeysLocked(keys); err != nil {
			return fmt.Errorf("看图模型的 Key 没能保存：%v", err)
		}
	}
	c.Vision = v
	return saveAIConfigLocked(c)
}

// ---------- pictures in the conversation ----------

func (s *aiSession) setOut(imgs []aiImage) {
	s.mu.Lock()
	s.outImgs = imgs
	s.mu.Unlock()
}

func (s *aiSession) takeOut() []aiImage {
	s.mu.Lock()
	defer s.mu.Unlock()
	imgs := s.outImgs
	s.outImgs = nil
	return imgs
}

// showImgs puts pictures on the step that is running, for the player.
func (s *aiSession) showImgs(names []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.steps) - 1; i >= 0; i-- {
		if s.steps[i].Kind == "tool" && s.steps[i].Busy {
			s.steps[i].Imgs = append(s.steps[i].Imgs, names...)
			return
		}
	}
}

func saveShots(imgs []aiImage) []string {
	var names []string
	for _, im := range imgs {
		if n := saveShot(im); n != "" {
			names = append(names, n)
		}
	}
	return names
}

// agePictures: only the pictures of the last two looks stay in the conversation; older ones are taken out
// (what was said about them stays).
func (s *aiSession) agePictures() {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := 0
	for i := len(s.turns) - 1; i >= 0; i-- {
		has := false
		for _, r := range s.turns[i].Results {
			has = has || len(r.Images) > 0
		}
		if !has {
			continue
		}
		if kept++; kept <= 2 {
			continue
		}
		for j := range s.turns[i].Results {
			if r := &s.turns[i].Results[j]; len(r.Images) > 0 {
				r.Images = nil
				r.Content += "\n（这次的截图已经从对话里撤下；还要看就再拍一次。）"
			}
		}
	}
}

// dropPictures: the service refused the pictures — the conversation goes on without them.
func (s *aiSession) dropPictures() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	any := false
	for i := range s.turns {
		for j := range s.turns[i].Results {
			if r := &s.turns[i].Results[j]; len(r.Images) > 0 {
				r.Images, any = nil, true
				r.Content += "\n（这个模型不接受图片，截图没有送到：" + "你没有看到画面，不要描述画面里的内容。截图已经显示给玩家。）"
			}
		}
	}
	return any
}

// ---------- look: pictures of the avatar ----------

var viewNames = map[string]string{"front": "正面", "back": "背面", "left": "左侧", "right": "右侧", "front_left": "左前方", "front_right": "右前方",
	"back_left": "左后方", "back_right": "右后方", "face": "脸部特写"}

type shotInfo struct {
	Avatar string `json:"avatar"`
	Shots  []struct {
		View string `json:"view"`
		File string `json:"file"`
	} `json:"shots"`
	Visible []string `json:"visible"`
	Hidden  []string `json:"hidden"`
	Playing bool     `json:"playing"`
	Note    string   `json:"note"`
}

// takeShots has the pipeline plugin photograph the avatar and reads the pictures in (the plugin's own files
// are removed again: nothing is left in the project).
func takeShots(ctx context.Context, project string, args map[string]any) ([]aiImage, shotInfo, error) {
	var o shotInfo
	raw, err := unity.BridgeCall(ctx, project, "snapshot", args, 2*time.Minute)
	if err != nil {
		if strings.Contains(err.Error(), "不认识的操作") {
			err = errors.New("这个工程里的 AI 插件是旧版，还不会拍照：在流水线页点「更新」把插件换成新版，等 Unity 编译完再试")
		}
		return nil, o, err
	}
	_ = json.Unmarshal(raw, &o)
	dir := filepath.Join(project, "UserSettings", "MioVRCA", "shots")
	var imgs []aiImage
	for _, sh := range o.Shots {
		f := filepath.Join(dir, filepath.Base(filepath.FromSlash(sh.File))) // only ever a file of that folder
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		_ = os.Remove(f)
		label := viewNames[sh.View]
		if label == "" {
			label = sh.View
		}
		if im, err := normImage(data, 1024, label); err == nil {
			imgs = append(imgs, im)
		}
	}
	if len(imgs) == 0 {
		return nil, o, errors.New("没有拍到图")
	}
	return imgs, o, nil
}

func (s *aiSession) look(ctx context.Context, a map[string]any) (content, short string, ok bool) {
	args := map[string]any{"avatar": argStr(a, "avatar"), "target": argStr(a, "target")}
	for _, k := range []string{"views", "show", "hide"} {
		if l := argStrs(a, k); len(l) > 0 {
			args[k] = l
		}
	}
	imgs, o, err := takeShots(ctx, s.project, args)
	if err != nil {
		return "错误：" + err.Error(), err.Error(), false
	}
	s.showImgs(saveShots(imgs))
	var views []string
	for _, im := range imgs {
		views = append(views, im.Note)
	}
	text, attach, how := s.present(ctx, imgs, argStr(a, "question"))
	s.setOut(attach)
	j, _ := json.Marshal(map[string]any{"avatar": o.Avatar, "views": views, "visible": o.Visible, "hidden": o.Hidden, "playing": o.Playing, "note": o.Note})
	short = fmt.Sprintf("拍了 %d 张（%s）", len(imgs), strings.Join(views, "、"))
	if how != "" {
		short += "，" + how
	}
	return string(j) + "\n" + text, short, true
}

// ---------- the pictures the window shows ----------

func shotsDir() string { return filepath.Join(core.DataDir, "shots") }

// saveShot keeps a picture for the step list; the oldest go when there are many.
func saveShot(im aiImage) string {
	dir := shotsDir()
	if os.MkdirAll(dir, 0755) != nil {
		return ""
	}
	name := fmt.Sprintf("%d_%04d.jpg", time.Now().UnixNano(), rand.Intn(10000))
	if os.WriteFile(filepath.Join(dir, name), im.Data, 0644) != nil {
		return ""
	}
	if ents, err := os.ReadDir(dir); err == nil && len(ents) > 120 {
		names := []string{}
		for _, e := range ents {
			if strings.HasSuffix(e.Name(), ".jpg") {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names) // names start with the time
		for _, n := range names[:len(names)-100] {
			_ = os.Remove(filepath.Join(dir, n))
		}
	}
	return name
}

func ServeShot(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("f")
	if name == "" || name != filepath.Base(name) || !strings.HasSuffix(name, ".jpg") || strings.ContainsAny(name, `/\:`) {
		http.NotFound(w, r)
		return
	}
	b, err := os.ReadFile(filepath.Join(shotsDir(), name))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	_, _ = w.Write(b)
}

// ---------- pictures in a UnitySkills answer ----------

var screenshotSkills = map[string]bool{"scene_screenshot": true, "camera_screenshot": true, "camera_sceneview_screenshot": true}

// screenshotArgs: the picture comes back inside the answer, and a camera's picture is kept where the other
// screenshots go (it cannot overwrite a file of the project).
func screenshotArgs(name string, args map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range args {
		out[k] = v
	}
	out["returnImage"] = true
	if _, ok := out["maxDimension"]; !ok {
		out["maxDimension"] = 1024
	}
	if name == "camera_screenshot" {
		base := "camera.png"
		if s, _ := out["savePath"].(string); s != "" {
			base = filepath.Base(strings.ReplaceAll(s, `\`, "/"))
		}
		if !strings.HasSuffix(strings.ToLower(base), ".png") {
			base += ".png"
		}
		out["savePath"] = "Assets/Screenshots/" + base
	}
	return out
}

// takeImages pulls the pictures out of a UnitySkills answer: what is left is the answer without the image
// data (which is no use as text and would only be cut off).
func takeImages(project string, raw []byte) ([]byte, []aiImage) {
	var o any
	if json.Unmarshal(raw, &o) != nil {
		return raw, nil
	}
	var imgs []aiImage
	var walk func(v any)
	walk = func(v any) {
		switch m := v.(type) {
		case map[string]any:
			if s, ok := m["imageBase64"].(string); ok && len(s) > 100 {
				if i := strings.Index(s, "base64,"); i >= 0 && i < 64 {
					s = s[i+7:]
				}
				if data, err := base64.StdEncoding.DecodeString(s); err == nil {
					if im, err := normImage(data, 1024, ""); err == nil {
						imgs = append(imgs, im)
						m["imageBase64"] = fmt.Sprintf("（图片 %d KB，已从结果里取出，见后面的说明）", len(data)/1024)
					}
				}
			}
			for _, c := range m {
				walk(c)
			}
		case []any:
			for _, c := range m {
				walk(c)
			}
		}
	}
	walk(o)
	if len(imgs) == 0 {
		// no picture in the answer: the file it names, when it is there
		if m, ok := o.(map[string]any); ok {
			r, _ := m["result"].(map[string]any)
			if r == nil {
				r = m
			}
			if p, _ := r["path"].(string); p != "" && strings.HasSuffix(strings.ToLower(p), ".png") {
				if !filepath.IsAbs(p) {
					p = filepath.Join(project, filepath.FromSlash(p))
				}
				if !strings.HasPrefix(core.PathKey(p), core.PathKey(project)+string(filepath.Separator)) { // only a file of this project
					return raw, nil
				}
				for i := 0; i < 8; i++ { // the Game view's file is written a frame later
					if data, err := os.ReadFile(p); err == nil && len(data) > 0 {
						if im, err := normImage(data, 1024, ""); err == nil {
							imgs = append(imgs, im)
							break
						}
					}
					time.Sleep(250 * time.Millisecond)
				}
			}
		}
	}
	if len(imgs) == 0 {
		return raw, nil
	}
	b, err := json.Marshal(o)
	if err != nil {
		return raw, imgs
	}
	return b, imgs
}
