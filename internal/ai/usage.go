package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
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
)

// What the AI services were asked for: the tokens of every answer, by day, service and model (ai-usage.json) —
// for the player to see what the pipeline uses, and about what it costs at the prices of the price list. The
// services' own bills are what counts: DeepSeek and Kimi also say what is left on the account.

// aiUsage: the tokens of one answer, or of many.
type aiUsage struct {
	In    int64 `json:"in"`              // input not read from the service's cache
	Hit   int64 `json:"hit"`             // input read from the cache (cheaper)
	Write int64 `json:"write,omitempty"` // input written into the cache (Anthropic: dearer than the rest)
	Out   int64 `json:"out"`             // output, thinking included
}

func (u aiUsage) total() int64 { return u.In + u.Hit + u.Write + u.Out }

func (u *aiUsage) add(o aiUsage) {
	u.In += o.In
	u.Hit += o.Hit
	u.Write += o.Write
	u.Out += o.Out
}

// openAIUsage: the usage of an answer on the OpenAI wire. The cached part of the input is called differently
// by each service (DeepSeek: prompt_cache_hit_tokens; OpenAI, GLM, Qwen: prompt_tokens_details.cached_tokens;
// Kimi: cached_tokens).
func openAIUsage(body []byte) aiUsage {
	var r struct {
		Usage *struct {
			Prompt     int64 `json:"prompt_tokens"`
			Completion int64 `json:"completion_tokens"`
			HitDS      int64 `json:"prompt_cache_hit_tokens"`
			Cached     int64 `json:"cached_tokens"`
			Details    *struct {
				Cached int64 `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	if json.Unmarshal(body, &r) != nil || r.Usage == nil {
		return aiUsage{}
	}
	u := r.Usage
	hit := u.HitDS
	if hit == 0 && u.Details != nil {
		hit = u.Details.Cached
	}
	if hit == 0 {
		hit = u.Cached
	}
	hit = min(max(hit, 0), max(u.Prompt, 0))
	return aiUsage{In: max(u.Prompt-hit, 0), Hit: hit, Out: max(u.Completion, 0)}
}

// claudeUsage: the usage of an answer on the Anthropic wire (input_tokens leaves out what the cache read and wrote).
func claudeUsage(body []byte) aiUsage {
	var r struct {
		Usage *struct {
			In    int64 `json:"input_tokens"`
			Out   int64 `json:"output_tokens"`
			Read  int64 `json:"cache_read_input_tokens"`
			Write int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(body, &r) != nil || r.Usage == nil {
		return aiUsage{}
	}
	u := r.Usage
	return aiUsage{In: max(u.In, 0), Hit: max(u.Read, 0), Write: max(u.Write, 0), Out: max(u.Out, 0)}
}

// aiPrice: what a model costs per million tokens, in Currency (CNY or USD). Hit: input read from the cache;
// Write: input written into it (0: as In). Peak: the factor of DeepSeek's busy hours (0: none).
type aiPrice struct {
	Currency string  `json:"currency"`
	In       float64 `json:"in"`
	Hit      float64 `json:"hit"`
	Write    float64 `json:"write,omitempty"`
	Out      float64 `json:"out"`
	Peak     float64 `json:"peak,omitempty"`
}

func (p aiPrice) ok() bool {
	return (p.Currency == "CNY" || p.Currency == "USD") && (p.In > 0 || p.Out > 0)
}

// cost: what u costs at p (peak: in DeepSeek's busy hours). Input written into the cache (only the Anthropic
// wire says so) is 1.25 times the input price there, when no price of its own is given.
func (p aiPrice) cost(u aiUsage, peak bool) float64 {
	w := p.Write
	if w == 0 {
		w = p.In * 1.25
	}
	c := (float64(u.In)*p.In + float64(u.Hit)*p.Hit + float64(u.Write)*w + float64(u.Out)*p.Out) / 1e6
	if peak && p.Peak > 0 {
		c *= p.Peak
	}
	return c
}

// builtinPrices: the services' own price lists, by "service/model". DeepSeek (api-docs.deepseek.com, October
// 2026, yuan): the busy hours (Beijing time, working days 9–12 and 14–18) cost twice as much.
var builtinPrices = map[string]aiPrice{
	"deepseek/deepseek-flash":  {Currency: "CNY", In: 1, Hit: 0.02, Out: 4, Peak: 2},
	"deepseek/deepseek-v4-pro": {Currency: "CNY", In: 4.5, Hit: 0.15, Out: 13.5, Peak: 2},
}

// PricesNote: where the built-in prices come from, for the window.
const pricesNote = "DeepSeek 价格取自其官方价格页（2026 年 10 月）"

// deepseekPeak: DeepSeek's busy hours — Monday to Friday, 9:00–12:00 and 14:00–18:00 Beijing time. Public
// holidays are off-peak there; they are not known here, so they count as busy (the estimate is on the high side).
func deepseekPeak(t time.Time) bool {
	bj := t.In(time.FixedZone("CST", 8*3600))
	if wd := bj.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return false
	}
	h := bj.Hour()
	return (h >= 9 && h < 12) || (h >= 14 && h < 18)
}

// usageRow: one day's answers of one model (busy and quiet hours apart, where they are priced apart).
type usageRow struct {
	Day      string `json:"day"` // this computer's date
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Peak     bool   `json:"peak,omitempty"`
	Calls    int    `json:"calls"`
	aiUsage
}

type usageFile struct {
	Since  int64              `json:"since,omitempty"` // when counting began (or began again)
	Rows   []usageRow         `json:"rows"`
	Prices map[string]aiPrice `json:"prices,omitempty"` // the player's own, by "service/model"
	Runs   []runRec           `json:"runs,omitempty"`   // the last runs of the pipeline, for what the next one may take
	Budget int64              `json:"budget,omitempty"` // tokens one run may take before the player is asked (0: defaultBudget, -1: no limit)
}

// runRec: what one run of the pipeline with an AI took.
type runRec struct {
	At       int64              `json:"at"`
	Provider string             `json:"provider"`
	Model    string             `json:"model"`
	Mode     string             `json:"mode"` // dress (the pipeline) or chat (what the player asked for afterwards)
	Calls    int                `json:"calls"`
	Tokens   int64              `json:"tokens"`
	Cost     map[string]float64 `json:"cost,omitempty"`
	OK       bool               `json:"ok"` // it ran to its end (not stopped, not failed)
}

// defaultBudget: how many tokens one run may take before the player is asked whether to go on — far more than
// a run of the pipeline needs, short of what a run going round in circles would take.
const defaultBudget = 1000000

// runBudget: the tokens one run may take before the player is asked (0: no limit).
func runBudget() int64 {
	usageMu.Lock()
	defer usageMu.Unlock()
	switch b := usageLocked().Budget; {
	case b < 0:
		return 0
	case b == 0:
		return defaultBudget
	default:
		return b
	}
}

// setBudget: the player's limit for one run, in tokens (0: no limit).
func setBudget(tokens int64) error {
	if tokens < 0 || tokens > 1e10 {
		return errors.New("上限须为 0 到 100 亿之间的数字")
	}
	usageMu.Lock()
	defer usageMu.Unlock()
	f := usageLocked()
	if tokens == 0 {
		f.Budget = -1
	} else {
		f.Budget = tokens
	}
	return saveUsageLocked()
}

// recordRun keeps what a run took; the last 60 are kept.
func recordRun(r runRec) {
	if r.Calls == 0 {
		return
	}
	usageMu.Lock()
	defer usageMu.Unlock()
	f := usageLocked()
	f.Runs = append(f.Runs, r)
	if len(f.Runs) > 60 {
		f.Runs = f.Runs[len(f.Runs)-60:]
	}
	if err := saveUsageLocked(); err != nil {
		core.Logf("AI 用量记录保存失败：%v", err)
	}
}

// runEstimate: what the next run is likely to take — the average of the last few runs of this kind with this
// model that ran to their end. nil when there are none.
func runEstimate(provider, model, mode string) map[string]any {
	usageMu.Lock()
	defer usageMu.Unlock()
	var tokens int64
	cost := map[string]float64{}
	n := 0
	runs := usageLocked().Runs
	for i := len(runs) - 1; i >= 0 && n < 5; i-- {
		r := runs[i]
		if !r.OK || r.Provider != provider || !strings.EqualFold(r.Model, model) || r.Mode != mode {
			continue
		}
		n++
		tokens += r.Tokens
		for k, v := range r.Cost {
			cost[k] += v
		}
	}
	if n == 0 {
		return nil
	}
	for k := range cost {
		cost[k] /= float64(n)
	}
	out := map[string]any{"tokens": tokens / int64(n), "runs": n}
	if len(cost) > 0 {
		out["cost"] = cost
	}
	return out
}

// minus: what was used between two looks at the same sum.
func (v UsageView) minus(o UsageView) UsageView {
	d := UsageView{Calls: v.Calls - o.Calls, Tokens: v.Tokens - o.Tokens, In: v.In - o.In, Hit: v.Hit - o.Hit, Write: v.Write - o.Write, Out: v.Out - o.Out, Unpriced: v.Unpriced - o.Unpriced}
	for k, c := range v.Cost {
		if c -= o.Cost[k]; c > 0 {
			if d.Cost == nil {
				d.Cost = map[string]float64{}
			}
			d.Cost[k] = c
		}
	}
	return d
}

// peakNow: DeepSeek's busy hours are on (its prices are twice as high), and until when (Beijing time).
func peakNow(provider string) map[string]any {
	now := usageNow()
	if provider != "deepseek" || !deepseekPeak(now) {
		return nil
	}
	until := "12:00"
	if h := now.In(time.FixedZone("CST", 8*3600)).Hour(); h >= 14 {
		until = "18:00"
	}
	return map[string]any{"until": until}
}

var (
	usageMu   sync.Mutex
	usageFrom string // the file usageData was read from ("": not read yet)
	usageData usageFile
)

func usagePath() string { return filepath.Join(core.DataDir, "ai-usage.json") }

// usageLocked: the file, read once (again when the data folder is another one: tests). Caller holds usageMu.
func usageLocked() *usageFile {
	if p := usagePath(); usageFrom != p {
		usageData = usageFile{}
		if b, err := os.ReadFile(p); err == nil {
			_ = json.Unmarshal(b, &usageData)
		}
		if usageData.Prices == nil {
			usageData.Prices = map[string]aiPrice{}
		}
		usageFrom = p
	}
	return &usageData
}

func saveUsageLocked() error {
	b, _ := json.MarshalIndent(&usageData, "", " ")
	tmp := usagePath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, usagePath())
}

func priceKey(provider, model string) string {
	return provider + "/" + strings.ToLower(strings.TrimSpace(model))
}

// priceLocked: the player's price for the model, else the service's own list. Caller holds usageMu.
func priceLocked(provider, model string) (aiPrice, bool) {
	k := priceKey(provider, model)
	if p, ok := usageLocked().Prices[k]; ok && p.ok() {
		return p, true
	}
	p, ok := builtinPrices[k]
	return p, ok
}

// usageSum: what the answers of one conversation of the pipeline took, and cost.
type usageSum struct {
	mu       sync.Mutex
	calls    int
	used     aiUsage
	cost     map[string]float64 // currency → amount, of the answers whose model has a price
	unpriced int64              // tokens of the answers whose model has none
}

func (s *usageSum) note(u aiUsage, p aiPrice, priced, peak bool) { s.noteN(1, u, p, priced, peak) }

// noteN: calls answers that took u together.
func (s *usageSum) noteN(calls int, u aiUsage, p aiPrice, priced, peak bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls += calls
	s.used.add(u)
	if !priced {
		s.unpriced += u.total()
		return
	}
	if s.cost == nil {
		s.cost = map[string]float64{}
	}
	s.cost[p.Currency] += p.cost(u, peak)
}

func (s *usageSum) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls, s.used, s.cost, s.unpriced = 0, aiUsage{}, nil, 0
}

// UsageView: tokens and what they cost, as the window shows them.
type UsageView struct {
	Calls    int                `json:"calls"`
	Tokens   int64              `json:"tokens"`
	In       int64              `json:"in"`
	Hit      int64              `json:"hit"`
	Write    int64              `json:"write,omitempty"`
	Out      int64              `json:"out"`
	Cost     map[string]float64 `json:"cost,omitempty"`     // currency → amount
	Unpriced int64              `json:"unpriced,omitempty"` // tokens without a price to count them at
}

func (s *usageSum) view() UsageView {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := UsageView{Calls: s.calls, Tokens: s.used.total(), In: s.used.In, Hit: s.used.Hit, Write: s.used.Write, Out: s.used.Out, Unpriced: s.unpriced}
	if len(s.cost) > 0 {
		v.Cost = map[string]float64{}
		for k, c := range s.cost {
			v.Cost[k] = c
		}
	}
	return v
}

type usageSinkKey struct{}

// withUsageSink: the answers asked for under ctx are counted into s too.
func withUsageSink(ctx context.Context, s *usageSum) context.Context {
	return context.WithValue(ctx, usageSinkKey{}, s)
}

// usageNow: the clock of the records (a var: tests set the time).
var usageNow = time.Now

// noteUsage records the tokens of one answer of c's service.
func (c *aiClient) noteUsage(ctx context.Context, u aiUsage) {
	if u.total() <= 0 { // the service said nothing about it
		return
	}
	now := usageNow()
	model := strings.TrimSpace(c.model)
	peak := c.info.ID == "deepseek" && deepseekPeak(now)
	usageMu.Lock()
	f := usageLocked()
	if f.Since == 0 {
		f.Since = now.Unix()
	}
	day := now.Format("2006-01-02")
	var row *usageRow
	for i := len(f.Rows) - 1; i >= 0; i-- {
		r := &f.Rows[i]
		if r.Day == day && r.Provider == c.info.ID && r.Model == model && r.Peak == peak {
			row = r
			break
		}
	}
	if row == nil {
		f.Rows = append(f.Rows, usageRow{Day: day, Provider: c.info.ID, Model: model, Peak: peak})
		row = &f.Rows[len(f.Rows)-1]
	}
	row.Calls++
	row.add(u)
	// a little over a year is kept
	cut := now.AddDate(0, 0, -400).Format("2006-01-02")
	if len(f.Rows) > 0 && f.Rows[0].Day < cut {
		keep := f.Rows[:0]
		for _, r := range f.Rows {
			if r.Day >= cut {
				keep = append(keep, r)
			}
		}
		f.Rows = keep
	}
	p, priced := priceLocked(c.info.ID, model)
	err := saveUsageLocked()
	usageMu.Unlock()
	if err != nil {
		core.Logf("AI 用量记录保存失败：%v", err)
	}
	if s, _ := ctx.Value(usageSinkKey{}).(*usageSum); s != nil {
		s.note(u, p, priced, peak)
	}
}

// usageModel: one model's tokens and cost today, this month and since counting began, with its price.
type usageModel struct {
	Provider string    `json:"provider"`
	Label    string    `json:"label"` // the service's name
	Model    string    `json:"model"`
	Today    UsageView `json:"today"`
	Month    UsageView `json:"month"`
	All      UsageView `json:"all"`
	Price    *aiPrice  `json:"price,omitempty"`
	Builtin  bool      `json:"builtin,omitempty"` // the price is the service's own list (not the player's)
}

// usageReport: what the window shows — every model used, and the one set up now (even when not used yet).
func usageReport() map[string]any {
	now := usageNow()
	today, month := now.Format("2006-01-02"), now.Format("2006-01")
	cfg := loadAIConfig()
	curModel := ""
	if aiProvider(cfg.Provider) != nil {
		curModel = strings.TrimSpace(cfg.profile(cfg.Provider).Model)
	}
	usageMu.Lock()
	defer usageMu.Unlock()
	f := usageLocked()
	byKey := map[string]*usageModel{}
	var order []string
	sums := map[string]map[string]*usageSum{} // model key → period → sum
	get := func(provider, model string) *usageModel {
		k := priceKey(provider, model)
		if m := byKey[k]; m != nil {
			return m
		}
		label := provider
		if info := aiProvider(provider); info != nil {
			label = info.Label
		}
		m := &usageModel{Provider: provider, Label: label, Model: model}
		if p, ok := priceLocked(provider, model); ok {
			pp := p
			m.Price = &pp
			_, own := f.Prices[k]
			m.Builtin = !own
		}
		byKey[k] = m
		order = append(order, k)
		sums[k] = map[string]*usageSum{"today": {}, "month": {}, "all": {}}
		return m
	}
	for _, r := range f.Rows {
		m := get(r.Provider, r.Model)
		var p aiPrice
		if m.Price != nil {
			p = *m.Price
		}
		periods := []string{"all"}
		if strings.HasPrefix(r.Day, month) {
			periods = append(periods, "month")
		}
		if r.Day == today {
			periods = append(periods, "today")
		}
		for _, per := range periods {
			sums[priceKey(r.Provider, r.Model)][per].noteN(r.Calls, r.aiUsage, p, m.Price != nil, r.Peak)
		}
	}
	if curModel != "" {
		get(cfg.Provider, curModel)
	}
	out := []*usageModel{}
	for _, k := range order {
		m := byKey[k]
		m.Today, m.Month, m.All = sums[k]["today"].view(), sums[k]["month"].view(), sums[k]["all"].view()
		out = append(out, m)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].All.Tokens > out[j].All.Tokens })
	budget := f.Budget
	if budget == 0 {
		budget = defaultBudget
	}
	return map[string]any{"models": out, "since": f.Since, "provider": cfg.Provider, "model": curModel,
		"balance": balanceKind(cfg) != "", "pricesNote": pricesNote, "budget": max(budget, 0)}
}

// usageToday: all of today's answers, of every model, with what they cost.
func usageToday() UsageView {
	today := usageNow().Format("2006-01-02")
	usageMu.Lock()
	defer usageMu.Unlock()
	var s usageSum
	for _, r := range usageLocked().Rows {
		if r.Day == today {
			p, ok := priceLocked(r.Provider, r.Model)
			s.noteN(r.Calls, r.aiUsage, p, ok, r.Peak)
		}
	}
	return s.view()
}

// setPrice: the player's price for a model (nil: back to the service's own list, or none).
func setPrice(provider, model string, p *aiPrice) error {
	if strings.TrimSpace(model) == "" || provider == "" {
		return errors.New("未指定模型")
	}
	usageMu.Lock()
	defer usageMu.Unlock()
	f := usageLocked()
	k := priceKey(provider, model)
	if p == nil {
		delete(f.Prices, k)
		return saveUsageLocked()
	}
	if p.Currency != "CNY" && p.Currency != "USD" {
		return errors.New("币种只能是人民币或美元")
	}
	for _, v := range []float64{p.In, p.Hit, p.Write, p.Out} {
		if v < 0 || v > 10000 || math.IsNaN(v) {
			return errors.New("价格须为 0 到 10000 之间的数字（每百万 tokens）")
		}
	}
	if p.In == 0 && p.Out == 0 {
		return errors.New("请至少填写输入或输出的价格")
	}
	q := *p
	if b, ok := builtinPrices[k]; ok {
		q.Peak = b.Peak // DeepSeek's busy hours stay twice the price
	} else {
		q.Peak = 0
	}
	f.Prices[k] = q
	return saveUsageLocked()
}

// clearUsage: counting begins again (the prices stay).
func clearUsage() error {
	usageMu.Lock()
	defer usageMu.Unlock()
	f := usageLocked()
	f.Rows, f.Since = nil, usageNow().Unix()
	return saveUsageLocked()
}

// ---------- what is left on the account ----------

// balanceKind: whose balance the service set up now can tell — DeepSeek's and Kimi's own addresses only (a
// relay's key is not theirs).
func balanceKind(c AIConfig) string {
	p := c.profile(c.Provider)
	u, err := url.Parse(strings.TrimSpace(p.BaseURL))
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	switch {
	case c.Provider == "deepseek" && host == "api.deepseek.com":
		return "deepseek"
	case c.Provider == "kimi" && (host == "api.moonshot.cn" || host == "api.moonshot.ai"):
		return "kimi"
	}
	return ""
}

// balanceAddr: where the service tells the balance (a var: tests answer it themselves).
var balanceAddr = func(kind string, base *url.URL) string {
	if kind == "kimi" {
		return base.Scheme + "://" + base.Host + "/v1/users/me/balance"
	}
	return base.Scheme + "://" + base.Host + "/user/balance"
}

// aiBalance: what is left on the account of the service set up now.
type aiBalance struct {
	Currency string  `json:"currency"`
	Total    float64 `json:"total"`
	Granted  float64 `json:"granted"` // gift money (DeepSeek) / vouchers (Kimi)
	Paid     float64 `json:"paid"`    // topped up (DeepSeek) / cash (Kimi; below 0: owed)
	Usable   bool    `json:"usable"`
}

func queryBalance(ctx context.Context, st *core.Store) (*aiBalance, error) {
	cfg := loadAIConfig()
	kind := balanceKind(cfg)
	if kind == "" {
		return nil, errors.New("当前的 AI 服务不支持查询余额（仅 DeepSeek 和 Kimi 的官方接口可以查询）")
	}
	key := aiKey(cfg.Provider)
	if key == "" {
		return nil, errors.New("未填写 API Key")
	}
	base, _ := url.Parse(strings.TrimSpace(cfg.profile(cfg.Provider).BaseURL))
	addr := balanceAddr(kind, base)
	req, _ := http.NewRequestWithContext(ctx, "GET", addr, nil)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	resp, err := aiHTTP(st, addr).Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法连接 %s：%s", base.Host, core.TrimErr(err))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		return nil, errors.New(aiErrText("查询余额", resp.StatusCode, body))
	}
	num := func(s json.RawMessage) float64 {
		v, _ := strconv.ParseFloat(strings.Trim(string(s), `"`), 64)
		return v
	}
	if kind == "deepseek" {
		var r struct {
			Available bool `json:"is_available"`
			Infos     []struct {
				Currency string          `json:"currency"`
				Total    json.RawMessage `json:"total_balance"`
				Granted  json.RawMessage `json:"granted_balance"`
				TopUp    json.RawMessage `json:"topped_up_balance"`
			} `json:"balance_infos"`
		}
		if json.Unmarshal(body, &r) != nil || len(r.Infos) == 0 {
			return nil, errors.New("DeepSeek 返回的余额无法识别")
		}
		in := r.Infos[0]
		for _, x := range r.Infos { // the yuan one first, when there are both
			if x.Currency == "CNY" {
				in = x
			}
		}
		return &aiBalance{Currency: in.Currency, Total: num(in.Total), Granted: num(in.Granted), Paid: num(in.TopUp), Usable: r.Available}, nil
	}
	var r struct {
		Code int `json:"code"`
		Data *struct {
			Available json.RawMessage `json:"available_balance"`
			Voucher   json.RawMessage `json:"voucher_balance"`
			Cash      json.RawMessage `json:"cash_balance"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &r) != nil || r.Data == nil || r.Code != 0 {
		return nil, errors.New("Kimi 返回的余额无法识别")
	}
	total := num(r.Data.Available)
	return &aiBalance{Currency: "CNY", Total: total, Granted: num(r.Data.Voucher), Paid: num(r.Data.Cash), Usable: total > 0}, nil
}

// tokText: a number of tokens as the window writes it (fmtTok in aiusage.js): "950", "12.3k", "1.25M".
func tokText(n int64) string {
	short := func(v float64, prec int) string {
		t := strconv.FormatFloat(v, 'f', prec, 64)
		if strings.Contains(t, ".") {
			t = strings.TrimRight(strings.TrimRight(t, "0"), ".")
		}
		return t
	}
	switch {
	case n >= 1000000:
		return short(float64(n)/1e6, 2) + "M"
	case n >= 1000:
		return short(float64(n)/1e3, 1) + "k"
	}
	return strconv.FormatInt(n, 10)
}

// moneyText: amounts by currency, as the player reads them ("¥0.012", "¥0.50 + $0.10").
func moneyText(cost map[string]float64) string {
	var keys []string
	for k := range cost {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		sym := "¥"
		if k == "USD" {
			sym = "$"
		}
		v := cost[k]
		prec := 4
		switch {
		case v >= 1:
			prec = 2
		case v >= 0.01:
			prec = 3
		}
		parts = append(parts, sym+strconv.FormatFloat(v, 'f', prec, 64))
	}
	return strings.Join(parts, " + ")
}
