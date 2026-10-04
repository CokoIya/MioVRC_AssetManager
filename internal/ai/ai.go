package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/unity"
)

type aiProviderInfo struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Base  string `json:"base"`  // default address
	Model string `json:"model"` // default model ("" = pick one from the list)
	Wire  string `json:"wire"`  // openai or claude
	Note  string `json:"note"`
}

var aiProviders = []aiProviderInfo{
	{ID: "deepseek", Label: "DeepSeek", Base: "https://api.deepseek.com", Model: "deepseek-flash", Wire: "openai",
		Note: "在 platform.deepseek.com 申请 API Key。模型：deepseek-flash（支持识图）、deepseek-v4-pro（能力更强，不支持识图，截图时自动由 deepseek-flash 代为描述）。旧名称 deepseek-chat / deepseek-reasoner 已停用。"},
	{ID: "qwen", Label: "通义千问", Base: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen-plus", Wire: "openai",
		Note: "在阿里云百炼（bailian.console.aliyun.com）申请 API Key。控制台提供的是带工作空间的地址（https://…maas.aliyuncs.com/compatible-mode/v1）时，请填写该地址。常用模型：qwen-plus、qwen-max、qwen3.8-max。"},
	{ID: "glm", Label: "智谱 GLM", Base: "https://open.bigmodel.cn/api/paas/v4", Model: "glm-5.3", Wire: "openai",
		Note: "在智谱开放平台（bigmodel.cn）申请 API Key。常用模型：glm-5.3、glm-4.7；免费模型：glm-4.7-flash。识图使用其视觉模型（glm-4.6v-flash 免费）。"},
	{ID: "kimi", Label: "Kimi", Base: "https://api.moonshot.cn/v1", Model: "kimi-k3", Wire: "openai",
		Note: "在 Kimi 开放平台（platform.kimi.com）申请 API Key。常用模型：kimi-k3、kimi-k2.6，均支持识图。"},
	{ID: "openai", Label: "ChatGPT 兼容", Base: "https://api.openai.com/v1", Model: "", Wire: "openai",
		Note: "OpenAI 官方，或任何兼容 OpenAI 接口的服务（中转站、本地的 Ollama / LM Studio 等）：填写该服务提供的接口地址和 Key。"},
	{ID: "claude", Label: "Claude 兼容", Base: "https://api.anthropic.com", Model: "claude-sonnet-5-5", Wire: "claude",
		Note: "Anthropic 官方，或兼容 Anthropic 接口的服务：填写该服务提供的接口地址和 Key。"},
}

// providerEyes: the model of each service that reads pictures. When the model doing the work cannot see and no
// vision model was set up, this one — same address, same key — describes the pictures for it.
var providerEyes = map[string]string{"deepseek": "deepseek-flash", "glm": "glm-4.6v-flash", "qwen": "qwen-vl-plus", "kimi": "kimi-k3"}

// names a service has retired, and what answers in their place
var retiredModels = map[string]map[string]string{"deepseek": {"deepseek-chat": "deepseek-flash", "deepseek-reasoner": "deepseek-flash"}}

// visionPresets: services with a model that reads pictures, offered in the 看图 part of the form (address and
// model are filled in; the key is the player's own).
type visionPreset struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Wire    string `json:"wire"`
	BaseURL string `json:"baseUrl"`
	Model   string `json:"model"`
	Note    string `json:"note"`
}

var visionPresets = []visionPreset{
	{ID: "deepseek", Label: "DeepSeek", Wire: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-flash", Note: "DeepSeek 的 deepseek-flash 支持识图"},
	{ID: "glm", Label: "智谱 GLM", Wire: "openai", BaseURL: "https://open.bigmodel.cn/api/paas/v4", Model: "glm-4.6v-flash", Note: "glm-4.6v-flash 免费；能力更强的有 glm-4.6v、glm-5v-turbo"},
	{ID: "qwen", Label: "通义千问", Wire: "openai", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen-vl-plus", Note: "也可使用 qwen3.8-max 等自身支持识图的模型"},
	{ID: "kimi", Label: "Kimi", Wire: "openai", BaseURL: "https://api.moonshot.cn/v1", Model: "kimi-k3", Note: "kimi-k3、kimi-k2.6 均支持识图"},
}

func aiProvider(id string) *aiProviderInfo {
	for i := range aiProviders {
		if aiProviders[i].ID == id {
			return &aiProviders[i]
		}
	}
	return nil
}

const defaultHierarchy = "主菜单 > {分类} > {素材} > {开关}"

// what 1.7.0 wrote by default; a player who kept it gets the new default (hair and props then go under their own menus)
const oldHierarchy = "主菜单 > 衣服 > {衣服} > {开关}"

type AIProfile struct {
	BaseURL string `json:"baseUrl"`
	Model   string `json:"model"`
}

type AIConfig struct {
	Provider  string                       `json:"provider"`
	Profiles  map[string]*AIProfile        `json:"profiles"`
	Hierarchy string                       `json:"hierarchy"`          // how generated menus are laid out
	Recent    map[string][]string          `json:"recent,omitempty"`   // project → the folders the last import put there
	Imported  map[string]map[string]string `json:"imported,omitempty"` // project → folder → the library asset it came from
	Vision    AIVision                     `json:"vision"`             // how pictures reach the AI
	Sees      map[string]string            `json:"sees,omitempty"`     // address and model → yes / no: does it read pictures (asked once)
}

var aiMu sync.Mutex

func aiConfigFile() string { return filepath.Join(core.DataDir, "ai.json") }
func aiKeyFile() string    { return filepath.Join(core.DataDir, "ai-key.dat") }

func loadAIConfig() AIConfig {
	aiMu.Lock()
	defer aiMu.Unlock()
	return loadAIConfigLocked()
}

func loadAIConfigLocked() AIConfig {
	c := AIConfig{}
	if b, err := os.ReadFile(aiConfigFile()); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	if c.Profiles == nil {
		c.Profiles = map[string]*AIProfile{}
	}
	if c.Recent == nil {
		c.Recent = map[string][]string{}
	}
	if c.Imported == nil {
		c.Imported = map[string]map[string]string{}
	}
	if h := strings.TrimSpace(c.Hierarchy); h == "" || h == oldHierarchy {
		c.Hierarchy = defaultHierarchy
	}
	return c
}

func saveAIConfigLocked(c AIConfig) error {
	b, _ := json.MarshalIndent(c, "", "  ")
	tmp := aiConfigFile() + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, aiConfigFile())
}

// profile: the address and model in use for a provider (the provider's defaults where nothing was entered).
func (c AIConfig) profile(id string) AIProfile {
	p := AIProfile{}
	if v := c.Profiles[id]; v != nil {
		p = *v
	}
	if info := aiProvider(id); info != nil {
		if strings.TrimSpace(p.BaseURL) == "" {
			p.BaseURL = info.Base
		}
		if strings.TrimSpace(p.Model) == "" {
			p.Model = info.Model
		}
		if now := retiredModels[id][strings.TrimSpace(p.Model)]; now != "" && sameService(p.BaseURL, info.Base) {
			p.Model = now // the service no longer answers to the old name
		}
	}
	return p
}

func loadAIKeysLocked() map[string]string {
	keys := map[string]string{}
	b, err := os.ReadFile(aiKeyFile())
	if err != nil {
		return keys
	}
	dec, err := core.UnprotectData(b)
	if err != nil {
		return keys // copied from another computer or Windows user: the keys have to be entered again
	}
	_ = json.Unmarshal(dec, &keys)
	return keys
}

func saveAIKeysLocked(keys map[string]string) error {
	b, _ := json.Marshal(keys)
	enc, err := core.ProtectData(b)
	if err != nil {
		return err
	}
	tmp := aiKeyFile() + ".tmp"
	if err := os.WriteFile(tmp, enc, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, aiKeyFile())
}

func aiKey(id string) string {
	aiMu.Lock()
	defer aiMu.Unlock()
	return loadAIKeysLocked()[id]
}

// AIView is what the window gets: no key, only whether there is one.
type AIView struct {
	Provider  string               `json:"provider"`
	Providers []aiProviderInfo     `json:"providers"`
	Profiles  map[string]AIProfile `json:"profiles"`
	Keys      map[string]string    `json:"keys"` // provider → "…abcd" when a key is saved
	Hierarchy string               `json:"hierarchy"`
	DefaultHi string               `json:"defaultHierarchy"`
	Ready     bool                 `json:"ready"` // a provider is picked and has what it needs
	Recent    map[string][]string  `json:"recent"`
	Vision    AIVision             `json:"vision"`
	VisionKey string               `json:"visionKey"` // "…abcd" when the second service has a key
	EyeModel  string               `json:"eyeModel"`  // the model that describes pictures for one that cannot see ("" = none)
	EyeOwn    bool                 `json:"eyeOwn"`    // … and it is the provider's own, not one the player set up
	Looks     string               `json:"looks"`     // who looks at pictures now: main, other, none, off, or unknown (not asked yet)
	Presets   []visionPreset       `json:"visionPresets"`
}

func keyTail(k string) string {
	k = strings.TrimSpace(k)
	if k == "" {
		return ""
	}
	if len(k) <= 8 {
		return "…"
	}
	return "…" + k[len(k)-4:]
}

func aiView() AIView {
	aiMu.Lock()
	defer aiMu.Unlock()
	c := loadAIConfigLocked()
	keys := loadAIKeysLocked()
	v := AIView{Provider: c.Provider, Providers: aiProviders, Profiles: map[string]AIProfile{}, Keys: map[string]string{},
		Hierarchy: localHierarchy(menuLang(), c.Hierarchy), DefaultHi: hierarchyIn(menuLang()), Recent: c.Recent}
	for _, p := range aiProviders {
		v.Profiles[p.ID] = c.profile(p.ID)
		if t := keyTail(keys[p.ID]); t != "" {
			v.Keys[p.ID] = t
		}
	}
	if aiProvider(c.Provider) != nil {
		p := c.profile(c.Provider)
		v.Ready = p.Model != "" && p.BaseURL != "" && (keys[c.Provider] != "" || isLocalHost(p.BaseURL))
	}
	v.Vision, v.VisionKey = c.Vision, keyTail(keys[visionKeyID])
	v.Looks, v.Presets = looksNow(c, keys), visionPresets
	if eye, _, own, ok := eyeFor(c, keys); ok {
		v.EyeModel, v.EyeOwn = eye.Model, own
	}
	return v
}

// aiSave: the provider, its address and model; key == nil leaves the saved key alone, "" removes it.
func aiSave(provider, base, model string, key *string, hierarchy *string) error {
	if provider != "" && aiProvider(provider) == nil {
		return errors.New("未知的 AI 服务商：" + provider)
	}
	aiMu.Lock()
	defer aiMu.Unlock()
	c := loadAIConfigLocked()
	if provider != "" {
		base = strings.TrimSpace(base)
		if base != "" {
			u, err := url.Parse(base)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return errors.New("接口地址须以 http:// 或 https:// 开头，例如 https://api.deepseek.com")
			}
		}
		was := c.profile(provider).BaseURL
		c.Provider = provider
		c.Profiles[provider] = &AIProfile{BaseURL: base, Model: strings.TrimSpace(model)}
		if key == nil && loadAIKeysLocked()[provider] != "" && !sameService(was, c.profile(provider).BaseURL) {
			return errKeyForOtherHost // a saved key is never sent to an address it was not entered for
		}
		if key != nil {
			keys := loadAIKeysLocked()
			if k := strings.TrimSpace(*key); k == "" {
				delete(keys, provider)
			} else {
				keys[provider] = k
			}
			if err := saveAIKeysLocked(keys); err != nil {
				return fmt.Errorf("API Key 保存失败：%v", err)
			}
		}
	}
	if hierarchy != nil {
		c.Hierarchy = strings.TrimSpace(*hierarchy)
	}
	return saveAIConfigLocked(c)
}

func init() { unity.AfterImport = aiRemember }

func aiRemember(project, key string, tops []string) {
	if project == "" || len(tops) == 0 {
		return
	}
	aiMu.Lock()
	defer aiMu.Unlock()
	c := loadAIConfigLocked()
	c.Recent[core.PathKey(project)] = tops
	if key != "" {
		m := c.Imported[core.PathKey(project)]
		if m == nil {
			m = map[string]string{}
			c.Imported[core.PathKey(project)] = m
		}
		for _, t := range tops {
			m[t] = key
		}
	}
	if len(c.Recent) > 40 { // old projects fall out
		ks := make([]string, 0, len(c.Recent))
		for k := range c.Recent {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		for _, k := range ks[:len(ks)-40] {
			if k != core.PathKey(project) {
				delete(c.Recent, k)
				delete(c.Imported, k)
			}
		}
	}
	_ = saveAIConfigLocked(c)
}

func isLocalHost(base string) bool {
	u, err := url.Parse(base)
	if err != nil {
		return false
	}
	h := u.Hostname()
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

// ---------- talking to the service ----------

type aiTool struct {
	Name   string
	Desc   string
	Params map[string]any // JSON schema of the arguments
}

type aiCall struct {
	ID   string
	Name string
	Args json.RawMessage
}

type aiResult struct {
	ID      string
	Name    string
	Content string
	IsErr   bool
	Images  []aiImage // pictures that came with the answer, for a model that looks itself
}

// aiTurn is one message of the conversation, whatever the service calls it.
type aiTurn struct {
	Role    string // user, assistant, tool
	Text    string
	Images  []aiImage  // user: pictures shown with the text
	Calls   []aiCall   // assistant: what it wants done
	Results []aiResult // tool: the answers to the calls of the turn before
	think   string     // openai wire: reasoning_content, handed back as it came
	blocks  json.RawMessage
	cut     bool // assistant: the output limit ended the turn in the middle of its last call
}

type aiClient struct {
	info   aiProviderInfo
	base   string
	model  string
	key    string
	hc     *http.Client
	outKey string                        // what this service calls the output limit ("" = max_tokens)
	outMax int                           // the limit asked for (0 = aiOutputMax, -1 = none sent)
	retry  func(n, of int) func(ok bool) // told before a request is sent again (nil = sent once only)
}

// aiOutputMax: how long one answer may get. Without a limit some services stop at a few hundred tokens and
// others at 4096; a menu plan needs more room than that.
const aiOutputMax = 8192

func aiHTTP(st *core.Store, base string) *http.Client {
	tr := &http.Transport{ResponseHeaderTimeout: 180 * time.Second}
	if !isLocalHost(base) {
		tr.Proxy = core.HTTPClient(st).Transport.(*http.Transport).Proxy
	}
	return &http.Client{Transport: tr, Timeout: 240 * time.Second,
		// the key goes to the address the player entered and nowhere else
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 0 && !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
				return errors.New("接口地址将请求重定向至其他网站（" + req.URL.Host + "），请求已中止，请填写服务商提供的最终地址")
			}
			if len(via) >= 5 {
				return errors.New("接口地址重定向次数过多")
			}
			return nil
		}}
}

// sameService: two addresses that are the same place as far as a key is concerned.
func sameService(a, b string) bool {
	ua, err1 := url.Parse(strings.TrimSpace(a))
	ub, err2 := url.Parse(strings.TrimSpace(b))
	return err1 == nil && err2 == nil && ua.Host != "" && strings.EqualFold(ua.Host, ub.Host)
}

var errKeyForOtherHost = errors.New("接口地址已更改：已保存的 API Key 仅用于原地址，请重新填写新地址的 Key")

// newAIClient: the service as it is set up now.
func newAIClient(st *core.Store) (*aiClient, error) {
	c := loadAIConfig()
	info := aiProvider(c.Provider)
	if info == nil {
		return nil, errors.New("尚未配置 AI 服务，请在「AI 服务」中选择服务商并填写 API Key")
	}
	p := c.profile(c.Provider)
	key := aiKey(c.Provider)
	return aiClientWith(st, *info, p.BaseURL, p.Model, key)
}

func aiClientWith(st *core.Store, info aiProviderInfo, base, model, key string) (*aiClient, error) {
	base = normBase(info.Wire, base)
	if base == "" {
		return nil, errors.New("未填写接口地址")
	}
	if key == "" && !isLocalHost(base) {
		return nil, errors.New(info.Label + "：未填写 API Key")
	}
	return &aiClient{info: info, base: base, model: strings.TrimSpace(model), key: key, hc: aiHTTP(st, base)}, nil
}

// normBase: what the player pasted, down to the part the endpoints are added to.
//
//	openai wire: https://host → https://host/v1; …/v1/chat/completions → …/v1
//	claude wire: https://host/v1/messages, https://host/v1 → https://host
func normBase(wire, base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return ""
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return ""
	}
	p := strings.TrimRight(u.Path, "/")
	if wire == "claude" {
		p = strings.TrimSuffix(p, "/messages")
		p = strings.TrimSuffix(p, "/v1")
	} else {
		p = strings.TrimSuffix(p, "/chat/completions")
		if p == "" {
			p = "/v1"
		}
	}
	u.Path, u.RawQuery, u.Fragment = p, "", ""
	return strings.TrimRight(u.String(), "/")
}

func (c *aiClient) headers(r *http.Request) {
	r.Header.Set("Content-Type", "application/json")
	if c.info.Wire == "claude" {
		r.Header.Set("anthropic-version", "2023-06-01")
		if c.key != "" {
			r.Header.Set("x-api-key", c.key)
			if u, _ := url.Parse(c.base); u == nil || !strings.HasSuffix(u.Hostname(), "anthropic.com") {
				r.Header.Set("Authorization", "Bearer "+c.key) // what most relays look at
			}
		}
		return
	}
	if c.key != "" {
		r.Header.Set("Authorization", "Bearer "+c.key)
	}
}

type aiHTTPError struct {
	Status int
	Msg    string
	Said   string        // the service's own words, lower case
	After  time.Duration // Retry-After, -1 when the service names none
}

func (e *aiHTTPError) Error() string { return e.Msg }

func hasAny(s string, words ...string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// overflow: the conversation no longer fits the model's context.
func (e *aiHTTPError) overflow() bool {
	return (e.Status == 400 || e.Status == 413 || e.Status == 422) && hasAny(e.Said, "maximum context length", "context_length_exceeded", "context length",
		"context window", "context limit", "range of input length", "prompt is too long", "input is too long", "input too long", "too many tokens", "token limit", "tokens exceed",
		"reduce the length", "上下文长度", "超出最大长度", "超过最大长度", "输入过长", "长度超过")
}

// noPictures: the service (or the model) does not take pictures.
func (e *aiHTTPError) noPictures() bool {
	return (e.Status == 400 || e.Status == 415 || e.Status == 422) && !e.overflow() && hasAny(e.Said, "image", "vision", "multimodal", "multi-modal",
		"picture", "unknown variant", "expected a string", "must be a string", "invalid type: sequence", "content type", "图片", "图像", "视觉", "多模态")
}

// busy: worth sending again in a moment (too many requests, or the service's own trouble). A 429 that is about
// money is not.
func (e *aiHTTPError) busy() bool {
	if e.Status == 429 {
		return !hasAny(e.Said, "insufficient", "balance", "quota", "余额", "额度", "欠费")
	}
	return e.Status == 408 || e.Status >= 500
}

// how long to wait before a request is sent again, when the service does not say (a var: tests wait less)
var aiRetryWait = []time.Duration{2 * time.Second, 6 * time.Second}

// do sends one request. A service that is busy (429, 5xx) or does not answer in time is asked again after a
// pause — its own Retry-After when it gives one — when the caller set c.retry to be told about it.
func (c *aiClient) do(ctx context.Context, method, path string, body any) ([]byte, error) {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return nil, err
		}
	}
	var done func(ok bool)
	for try := 0; ; try++ {
		b, timeout, err := c.once(ctx, method, path, payload)
		if done != nil {
			done(err == nil)
		}
		var he *aiHTTPError
		again := timeout && try == 0 || errors.As(err, &he) && he.busy() // a second timeout is the end of it
		if err == nil || !again || c.retry == nil || try >= len(aiRetryWait) || ctx.Err() != nil {
			return b, err
		}
		wait := aiRetryWait[try]
		if he != nil && he.After >= 0 {
			wait = min(he.After, time.Minute)
		}
		done = c.retry(try+2, len(aiRetryWait)+1)
		select {
		case <-ctx.Done():
			done(false)
			return nil, errors.New("已停止")
		case <-time.After(wait):
		}
	}
}

func (c *aiClient) once(ctx context.Context, method, path string, payload []byte) (b []byte, timeout bool, err error) {
	var rd io.Reader
	if payload != nil {
		rd = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return nil, false, err
	}
	c.headers(req)
	resp, err := c.hc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, false, errors.New("已停止")
		}
		host := c.base
		if u, e := url.Parse(c.base); e == nil {
			host = u.Host
		}
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return nil, true, fmt.Errorf("%s 响应超时，请稍后重试或更换模型", host)
		}
		return nil, false, fmt.Errorf("无法连接 %s，请检查接口地址和网络（如需代理，可在设置中填写）。%v", host, core.TrimErr(err))
	}
	defer resp.Body.Close()
	b, _ = io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode/100 != 2 {
		return nil, false, &aiHTTPError{Status: resp.StatusCode, Msg: aiErrText(c.info.Label, resp.StatusCode, b), Said: strings.ToLower(aiSaid(b)),
			After: retryAfter(resp.Header.Get("Retry-After"))}
	}
	return b, false, nil
}

// retryAfter: the header as a duration (seconds, or a date), -1 when it says nothing usable.
func retryAfter(h string) time.Duration {
	h = strings.TrimSpace(h)
	if n, err := strconv.Atoi(h); err == nil && n >= 0 {
		return time.Duration(n) * time.Second
	}
	if t, err := http.ParseTime(h); err == nil {
		return max(time.Until(t), 0)
	}
	return -1
}

// aiSaid: what the service itself said about a refusal ("" when its answer says nothing a player can read).
func aiSaid(body []byte) string {
	said := ""
	var e struct {
		Error json.RawMessage `json:"error"`
		Msg   string          `json:"message"`
	}
	if json.Unmarshal(body, &e) == nil {
		var o struct {
			Message string `json:"message"`
		}
		var s string
		switch {
		case json.Unmarshal(e.Error, &o) == nil && o.Message != "":
			said = o.Message
		case json.Unmarshal(e.Error, &s) == nil && s != "":
			said = s
		case e.Msg != "":
			said = e.Msg
		}
	}
	if said == "" {
		said = strings.TrimSpace(string(body))
		if strings.HasPrefix(said, "<") {
			said = "" // an HTML error page says nothing useful
		}
	}
	return said
}

// aiErrText: the service's refusal in words a player can act on, with what the service said.
func aiErrText(label string, status int, body []byte) string {
	said := aiSaid(body)
	low := strings.ToLower(said)
	if r := []rune(said); len(r) > 240 {
		said = string(r[:240]) + "…"
	}
	var what string
	switch {
	case (&aiHTTPError{Status: status, Said: low}).overflow():
		what = "对话内容已超出模型的上下文长度"
	case status == 401 || status == 403:
		what = "API Key 不正确或已失效，或无权使用该模型"
	case status == 402 || strings.Contains(low, "insufficient") || strings.Contains(low, "balance") || strings.Contains(low, "quota"):
		what = "账户余额或额度不足"
	case status == 404:
		what = "接口地址或模型名称不正确"
	case status == 429:
		what = "请求过于频繁，或额度已用尽"
	case status == 400 || status == 422:
		what = "请求未被接受（模型名称不正确，或该模型不支持工具调用）"
	case status >= 500:
		what = "服务端出错，请稍后重试"
	default:
		what = "服务拒绝了请求"
	}
	msg := fmt.Sprintf("%s：%s（HTTP %d）", label, what, status)
	if said != "" {
		msg += "。服务返回：" + said
	}
	return msg
}

// models: what the service offers, for the model list in the settings.
func (c *aiClient) models(ctx context.Context) ([]string, error) {
	path := "/models"
	if c.info.Wire == "claude" {
		path = "/v1/models?limit=100"
	}
	b, err := c.do(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	var r struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, errors.New("该服务未提供模型列表，请直接填写模型名称")
	}
	var out []string
	for _, m := range r.Data {
		if m.ID != "" {
			out = append(out, m.ID)
		}
	}
	for _, m := range r.Models {
		if m.ID != "" {
			out = append(out, m.ID)
		} else if m.Name != "" {
			out = append(out, m.Name)
		}
	}
	out = core.UniqStrings(out)
	sort.Strings(out)
	if len(out) == 0 {
		return nil, errors.New("该服务未提供模型列表，请直接填写模型名称")
	}
	return out, nil
}

// chat sends the conversation and returns the assistant's next turn: text, tool calls, or both.
func (c *aiClient) chat(ctx context.Context, system string, turns []aiTurn, tools []aiTool) (aiTurn, error) {
	if c.model == "" {
		return aiTurn{}, errors.New("未选择模型，请在「AI 服务」中填写模型名称，或点击「选择模型」选取")
	}
	if c.info.Wire == "claude" {
		return c.chatClaude(ctx, system, turns, tools)
	}
	return c.chatOpenAI(ctx, system, turns, tools)
}

func (c *aiClient) chatOpenAI(ctx context.Context, system string, turns []aiTurn, tools []aiTool) (aiTurn, error) {
	type fn struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	type call struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function fn     `json:"function"`
	}
	msgs := []map[string]any{}
	if system != "" {
		msgs = append(msgs, map[string]any{"role": "system", "content": system})
	}
	for _, t := range turns {
		switch t.Role {
		case "user":
			if len(t.Images) > 0 {
				msgs = append(msgs, map[string]any{"role": "user", "content": openAIParts(t.Text, t.Images)})
				continue
			}
			msgs = append(msgs, map[string]any{"role": "user", "content": t.Text})
		case "assistant":
			m := map[string]any{"role": "assistant", "content": t.Text}
			if len(t.Calls) == 0 {
				m["content"] = nonEmpty(t.Text)
			}
			if len(t.Calls) > 0 {
				cs := make([]call, 0, len(t.Calls))
				for _, k := range t.Calls {
					args := string(k.Args)
					if args == "" {
						args = "{}"
					}
					cs = append(cs, call{ID: k.ID, Type: "function", Function: fn{Name: k.Name, Arguments: args}})
				}
				m["tool_calls"] = cs
				if t.Text == "" {
					m["content"] = nil
				}
			}
			if t.think != "" {
				m["reasoning_content"] = t.think
			}
			msgs = append(msgs, m)
		case "tool":
			// a tool's answer is text on this wire: its pictures follow in a message of their own
			var imgs []aiImage
			for _, r := range t.Results {
				msgs = append(msgs, map[string]any{"role": "tool", "tool_call_id": r.ID, "content": r.Content})
				imgs = append(imgs, r.Images...)
			}
			if len(imgs) > 0 {
				msgs = append(msgs, map[string]any{"role": "user", "content": openAIParts("（上面工具结果里说的截图，按顺序附在这里。这条消息是软件自动加的，不是玩家说的话。）", imgs)})
			}
		}
	}
	body := map[string]any{"model": c.model, "messages": msgs, "stream": false}
	if len(tools) > 0 {
		ts := make([]map[string]any, 0, len(tools))
		for _, t := range tools {
			ts = append(ts, map[string]any{"type": "function", "function": map[string]any{"name": t.Name, "description": t.Desc, "parameters": t.Params}})
		}
		body["tools"] = ts
	}
	c.limitOutput(body)
	var b []byte
	var err error
	for {
		b, err = c.do(ctx, "POST", "/chat/completions", body)
		var he *aiHTTPError
		if !errors.As(err, &he) || (he.Status != 400 && he.Status != 422) {
			break
		}
		if _, off := body["enable_thinking"]; !off && strings.Contains(he.Msg, "enable_thinking") {
			body["enable_thinking"] = false // Qwen's thinking models answer in one piece only with thinking off
			continue
		}
		if !c.lowerOutput(he) {
			break
		}
		c.limitOutput(body)
	}
	if err != nil {
		return aiTurn{}, err
	}
	var r struct {
		Choices []struct {
			Message struct {
				Content   any    `json:"content"`
				Reasoning string `json:"reasoning_content"`
				ToolCalls []call `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(b, &r); err != nil || len(r.Choices) == 0 {
		return aiTurn{}, errors.New(c.info.Label + "：无法解析响应内容，该地址可能不是 OpenAI 兼容接口")
	}
	m := r.Choices[0].Message
	out := aiTurn{Role: "assistant", think: m.Reasoning}
	switch v := m.Content.(type) {
	case string:
		out.Text = v
	case []any: // some relays answer in parts
		for _, p := range v {
			if o, ok := p.(map[string]any); ok {
				if s, ok := o["text"].(string); ok {
					out.Text += s
				}
			}
		}
	}
	for i, k := range m.ToolCalls {
		id := k.ID
		if id == "" {
			id = fmt.Sprintf("call_%d_%d", time.Now().UnixNano(), i)
		}
		out.Calls = append(out.Calls, aiCall{ID: id, Name: k.Function.Name, Args: json.RawMessage(k.Function.Arguments)})
	}
	if out.Text == "" && len(out.Calls) == 0 && r.Choices[0].FinishReason == "length" {
		return aiTurn{}, errors.New("回答过长被截断：请更换输出上限更高的模型，或拆分要求后重试")
	}
	out.cut = r.Choices[0].FinishReason == "length" && len(out.Calls) > 0
	return out, nil
}

// limitOutput writes the output limit into a request the way this service takes it.
func (c *aiClient) limitOutput(body map[string]any) {
	delete(body, "max_tokens")
	delete(body, "max_completion_tokens")
	n, key := c.outMax, c.outKey
	if n == 0 {
		n = aiOutputMax
	}
	if key == "" {
		key = "max_tokens"
	}
	if n > 0 {
		body[key] = n
	}
}

// lowerOutput: the service refused the output limit itself — it wants it under another name (newer OpenAI
// models), or smaller, or (in the end) not at all. false: the refusal was about something else, or nothing
// is left to try. What worked is kept for the rest of the run.
func (c *aiClient) lowerOutput(he *aiHTTPError) bool {
	switch {
	case he.overflow() || !hasAny(he.Said, "max_tokens", "max_completion_tokens"): // too long as a whole is not about the limit
		return false
	case strings.Contains(he.Said, "max_completion_tokens") && c.outKey == "" && c.info.Wire != "claude":
		c.outKey = "max_completion_tokens"
	case c.outMax == 0:
		c.outMax = aiOutputMax / 2
	case c.outMax > 0 && c.info.Wire != "claude": // the Anthropic wire has no request without a limit
		c.outMax = -1
	default:
		return false
	}
	return true
}

func (c *aiClient) chatClaude(ctx context.Context, system string, turns []aiTurn, tools []aiTool) (aiTurn, error) {
	msgs := []map[string]any{}
	for _, t := range turns {
		switch t.Role {
		case "user":
			msgs = append(msgs, map[string]any{"role": "user", "content": append(claudeImages(t.Images), map[string]any{"type": "text", "text": t.Text})})
		case "assistant":
			if len(t.blocks) > 2 { // as the service sent it (thinking blocks included); "[]" is not a turn
				msgs = append(msgs, map[string]any{"role": "assistant", "content": t.blocks})
				continue
			}
			var bl []map[string]any
			if t.Text != "" || len(t.Calls) == 0 {
				bl = append(bl, map[string]any{"type": "text", "text": nonEmpty(t.Text)})
			}
			for _, k := range t.Calls {
				var in any = map[string]any{}
				if len(k.Args) > 0 {
					_ = json.Unmarshal(k.Args, &in)
				}
				bl = append(bl, map[string]any{"type": "tool_use", "id": k.ID, "name": k.Name, "input": in})
			}
			msgs = append(msgs, map[string]any{"role": "assistant", "content": bl})
		case "tool":
			var bl []map[string]any
			for _, r := range t.Results {
				o := map[string]any{"type": "tool_result", "tool_use_id": r.ID, "content": r.Content}
				if len(r.Images) > 0 {
					o["content"] = append([]map[string]any{{"type": "text", "text": r.Content}}, claudeImages(r.Images)...)
				}
				if r.IsErr {
					o["is_error"] = true
				}
				bl = append(bl, o)
			}
			msgs = append(msgs, map[string]any{"role": "user", "content": bl})
		}
	}
	body := map[string]any{"model": c.model, "messages": msgs}
	c.limitOutput(body)
	if system != "" {
		body["system"] = system
	}
	if len(tools) > 0 {
		ts := make([]map[string]any, 0, len(tools))
		for _, t := range tools {
			ts = append(ts, map[string]any{"name": t.Name, "description": t.Desc, "input_schema": t.Params})
		}
		body["tools"] = ts
	}
	b, err := c.do(ctx, "POST", "/v1/messages", body)
	var he *aiHTTPError
	if errors.As(err, &he) && he.Status == 400 && c.lowerOutput(he) { // an older model with a smaller limit
		c.limitOutput(body)
		b, err = c.do(ctx, "POST", "/v1/messages", body)
	}
	if err != nil {
		return aiTurn{}, err
	}
	var r struct {
		Content    json.RawMessage `json:"content"`
		StopReason string          `json:"stop_reason"`
	}
	var blocks []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(b, &r); err != nil || json.Unmarshal(r.Content, &blocks) != nil {
		return aiTurn{}, errors.New(c.info.Label + "：无法解析响应内容，该地址可能不是 Anthropic 兼容接口")
	}
	out := aiTurn{Role: "assistant", blocks: r.Content}
	for _, bl := range blocks {
		switch bl.Type {
		case "text":
			out.Text += bl.Text
		case "tool_use":
			out.Calls = append(out.Calls, aiCall{ID: bl.ID, Name: bl.Name, Args: bl.Input})
		}
	}
	if out.Text == "" && len(out.Calls) == 0 && r.StopReason == "max_tokens" {
		return aiTurn{}, errors.New("回答过长被截断：请更换输出上限更高的模型，或拆分要求后重试")
	}
	out.cut = r.StopReason == "max_tokens" && len(out.Calls) > 0
	return out, nil
}

// openAIParts: text and pictures as the parts of one message.
func openAIParts(text string, imgs []aiImage) []map[string]any {
	parts := []map[string]any{{"type": "text", "text": text}}
	for _, im := range imgs {
		parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:" + im.Mime + ";base64," + base64.StdEncoding.EncodeToString(im.Data)}})
	}
	return parts
}

func claudeImages(imgs []aiImage) []map[string]any {
	out := []map[string]any{}
	for _, im := range imgs {
		out = append(out, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": im.Mime, "data": base64.StdEncoding.EncodeToString(im.Data)}})
	}
	return out
}

func nonEmpty(s string) string {
	if strings.TrimSpace(s) == "" {
		return "（没有内容）"
	}
	return s
}

// aiTest: one short exchange, to tell the player whether the address, key and model work.
func aiTest(ctx context.Context, c *aiClient) (string, error) {
	t0 := time.Now()
	out, err := c.chat(ctx, "", []aiTurn{{Role: "user", Text: "只回复两个字：收到"}}, nil)
	if err != nil {
		return "", err
	}
	reply := strings.TrimSpace(out.Text)
	if r := []rune(reply); len(r) > 40 {
		reply = string(r[:40]) + "…"
	}
	return fmt.Sprintf("连接正常：%s 回复「%s」（%.1f 秒）", c.model, reply, time.Since(t0).Seconds()), nil
}
