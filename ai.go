package main

// The AI service the player picked for the AI assistant: DeepSeek, anything that speaks OpenAI's chat
// completions ("ChatGPT 兼容"), or anything that speaks Anthropic's messages ("Claude 兼容").
//
// The choice (address, model, menu layout) is in ai.json; the API keys are in ai-key.dat, encrypted for this
// Windows user. A key is never sent back to the window: it only learns whether one is saved and how it ends.

import (
	"bytes"
	"context"
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
	"strings"
	"sync"
	"time"
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
	{ID: "deepseek", Label: "DeepSeek", Base: "https://api.deepseek.com", Model: "deepseek-chat", Wire: "openai",
		Note: "在 platform.deepseek.com 申请 API Key。"},
	{ID: "openai", Label: "ChatGPT 兼容", Base: "https://api.openai.com/v1", Model: "", Wire: "openai",
		Note: "OpenAI 官方，或任何兼容 OpenAI 接口的服务（中转站、通义、Kimi、智谱、本地的 Ollama / LM Studio 等）：填它给的接口地址和 Key。"},
	{ID: "claude", Label: "Claude 兼容", Base: "https://api.anthropic.com", Model: "claude-sonnet-5-5", Wire: "claude",
		Note: "Anthropic 官方，或兼容 Anthropic 接口的服务：填它给的接口地址和 Key。"},
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
}

var aiMu sync.Mutex

func aiConfigFile() string { return filepath.Join(dataDir, "ai.json") }
func aiKeyFile() string    { return filepath.Join(dataDir, "ai-key.dat") }

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
	}
	return p
}

func loadAIKeysLocked() map[string]string {
	keys := map[string]string{}
	b, err := os.ReadFile(aiKeyFile())
	if err != nil {
		return keys
	}
	dec, err := unprotectData(b)
	if err != nil {
		return keys // copied from another computer or Windows user: the keys have to be entered again
	}
	_ = json.Unmarshal(dec, &keys)
	return keys
}

func saveAIKeysLocked(keys map[string]string) error {
	b, _ := json.Marshal(keys)
	enc, err := protectData(b)
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
		Hierarchy: c.Hierarchy, DefaultHi: defaultHierarchy, Recent: c.Recent}
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
	return v
}

// aiSave: the provider, its address and model; key == nil leaves the saved key alone, "" removes it.
func aiSave(provider, base, model string, key *string, hierarchy *string) error {
	if provider != "" && aiProvider(provider) == nil {
		return errors.New("不认识的 AI 服务商：" + provider)
	}
	aiMu.Lock()
	defer aiMu.Unlock()
	c := loadAIConfigLocked()
	if provider != "" {
		base = strings.TrimSpace(base)
		if base != "" {
			u, err := url.Parse(base)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return errors.New("接口地址要以 http:// 或 https:// 开头，例如 https://api.deepseek.com")
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
				return fmt.Errorf("API Key 没能保存：%v", err)
			}
		}
	}
	if hierarchy != nil {
		c.Hierarchy = strings.TrimSpace(*hierarchy)
	}
	return saveAIConfigLocked(c)
}

func aiRemember(project, key string, tops []string) {
	if project == "" || len(tops) == 0 {
		return
	}
	aiMu.Lock()
	defer aiMu.Unlock()
	c := loadAIConfigLocked()
	c.Recent[pathKey(project)] = tops
	if key != "" {
		m := c.Imported[pathKey(project)]
		if m == nil {
			m = map[string]string{}
			c.Imported[pathKey(project)] = m
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
			if k != pathKey(project) {
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
}

// aiTurn is one message of the conversation, whatever the service calls it.
type aiTurn struct {
	Role    string // user, assistant, tool
	Text    string
	Calls   []aiCall   // assistant: what it wants done
	Results []aiResult // tool: the answers to the calls of the turn before
	think   string     // openai wire: reasoning_content, handed back as it came
	blocks  json.RawMessage
}

type aiClient struct {
	info  aiProviderInfo
	base  string
	model string
	key   string
	hc    *http.Client
}

func aiHTTP(st *Store, base string) *http.Client {
	tr := &http.Transport{ResponseHeaderTimeout: 180 * time.Second}
	if !isLocalHost(base) {
		tr.Proxy = httpClient(st).Transport.(*http.Transport).Proxy
	}
	return &http.Client{Transport: tr, Timeout: 240 * time.Second,
		// the key goes to the address the player entered and nowhere else
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 0 && !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
				return errors.New("接口地址把请求转到了另一个网站（" + req.URL.Host + "），没有跟过去：填服务商给的最终地址")
			}
			if len(via) >= 5 {
				return errors.New("接口地址转来转去")
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

var errKeyForOtherHost = errors.New("接口地址换了：已保存的 API Key 是给原来那个地址的，请重新填写这个地址的 Key")

// newAIClient: the service as it is set up now.
func newAIClient(st *Store) (*aiClient, error) {
	c := loadAIConfig()
	info := aiProvider(c.Provider)
	if info == nil {
		return nil, errors.New("还没有设置 AI 服务：先在「AI 服务」里选服务商、填 API Key")
	}
	p := c.profile(c.Provider)
	key := aiKey(c.Provider)
	return aiClientWith(st, *info, p.BaseURL, p.Model, key)
}

func aiClientWith(st *Store, info aiProviderInfo, base, model, key string) (*aiClient, error) {
	base = normBase(info.Wire, base)
	if base == "" {
		return nil, errors.New("还没有填接口地址")
	}
	if key == "" && !isLocalHost(base) {
		return nil, errors.New("还没有填 " + info.Label + " 的 API Key")
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
}

func (e *aiHTTPError) Error() string { return e.Msg }

func (c *aiClient) do(ctx context.Context, method, path string, body any) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return nil, err
	}
	c.headers(req)
	resp, err := c.hc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, errors.New("已停止")
		}
		host := c.base
		if u, e := url.Parse(c.base); e == nil {
			host = u.Host
		}
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return nil, fmt.Errorf("%s 太久没有回应：稍后再试，或换一个模型", host)
		}
		return nil, fmt.Errorf("连不上 %s：检查接口地址和网络（需要代理的话在设置里填上）。%v", host, trimErr(err))
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode/100 != 2 {
		return nil, &aiHTTPError{Status: resp.StatusCode, Msg: aiErrText(c.info.Label, resp.StatusCode, b)}
	}
	return b, nil
}

func trimErr(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 && i+2 < len(s) {
		s = s[i+2:]
	}
	return s
}

// aiErrText: the service's refusal in words a player can act on, with what the service said.
func aiErrText(label string, status int, body []byte) string {
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
	if r := []rune(said); len(r) > 240 {
		said = string(r[:240]) + "…"
	}
	low := strings.ToLower(said)
	var what string
	switch {
	case status == 401 || status == 403:
		what = "API Key 不对、已失效，或没有权限用这个模型"
	case status == 402 || strings.Contains(low, "insufficient") || strings.Contains(low, "balance") || strings.Contains(low, "quota"):
		what = "账户余额或额度不够"
	case status == 404:
		what = "接口地址或模型名不对"
	case status == 429:
		what = "请求太频繁，或额度用完了"
	case status == 400 || status == 422:
		what = "服务不接受这次请求（模型名不对，或这个模型不支持工具调用）"
	case status >= 500:
		what = "服务那边出错了，稍后再试"
	default:
		what = "服务拒绝了请求"
	}
	msg := fmt.Sprintf("%s：%s（HTTP %d）", label, what, status)
	if said != "" {
		msg += "。原话：" + said
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
		return nil, errors.New("这个服务没有给出模型列表，直接填模型名就行")
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
	out = uniqStrings(out)
	sort.Strings(out)
	if len(out) == 0 {
		return nil, errors.New("这个服务没有给出模型列表，直接填模型名就行")
	}
	return out, nil
}

// chat sends the conversation and returns the assistant's next turn: text, tool calls, or both.
func (c *aiClient) chat(ctx context.Context, system string, turns []aiTurn, tools []aiTool) (aiTurn, error) {
	if c.model == "" {
		return aiTurn{}, errors.New("还没有选模型：在「AI 服务」里填模型名，或点「选择模型」挑一个")
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
			for _, r := range t.Results {
				msgs = append(msgs, map[string]any{"role": "tool", "tool_call_id": r.ID, "content": r.Content})
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
	if c.info.ID == "deepseek" {
		body["max_tokens"] = 8192
	}
	b, err := c.do(ctx, "POST", "/chat/completions", body)
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
		return aiTurn{}, errors.New(c.info.Label + " 的回答看不懂：这个地址可能不是 OpenAI 兼容接口")
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
		return aiTurn{}, errors.New("回答太长被截断了：换一个输出上限更大的模型，或把要求拆小一点")
	}
	return out, nil
}

func (c *aiClient) chatClaude(ctx context.Context, system string, turns []aiTurn, tools []aiTool) (aiTurn, error) {
	msgs := []map[string]any{}
	for _, t := range turns {
		switch t.Role {
		case "user":
			msgs = append(msgs, map[string]any{"role": "user", "content": []map[string]any{{"type": "text", "text": t.Text}}})
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
				if r.IsErr {
					o["is_error"] = true
				}
				bl = append(bl, o)
			}
			msgs = append(msgs, map[string]any{"role": "user", "content": bl})
		}
	}
	body := map[string]any{"model": c.model, "max_tokens": 8192, "messages": msgs}
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
	if errors.As(err, &he) && he.Status == 400 && strings.Contains(he.Msg, "max_tokens") {
		body["max_tokens"] = 4096 // an older model with a smaller limit
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
		return aiTurn{}, errors.New(c.info.Label + " 的回答看不懂：这个地址可能不是 Anthropic 兼容接口")
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
		return aiTurn{}, errors.New("回答太长被截断了：换一个输出上限更大的模型，或把要求拆小一点")
	}
	return out, nil
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
	return fmt.Sprintf("连接正常：%s 回答了「%s」（%.1f 秒）", c.model, reply, time.Since(t0).Seconds()), nil
}
