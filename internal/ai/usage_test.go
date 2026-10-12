package ai

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
	"vrclib/internal/unity/unitytest"
)

func TestUsageParsing(t *testing.T) {
	for name, c := range map[string]struct {
		body string
		want aiUsage
	}{
		"deepseek": {`{"usage":{"prompt_tokens":1000,"completion_tokens":50,"prompt_cache_hit_tokens":300,"prompt_cache_miss_tokens":700,"prompt_tokens_details":{"cached_tokens":300}}}`, aiUsage{In: 700, Hit: 300, Out: 50}},
		"openai":   {`{"usage":{"prompt_tokens":200,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":128}}}`, aiUsage{In: 72, Hit: 128, Out: 20}},
		"kimi":     {`{"usage":{"prompt_tokens":90,"completion_tokens":9,"cached_tokens":40}}`, aiUsage{In: 50, Hit: 40, Out: 9}},
		"none":     {`{"choices":[]}`, aiUsage{}},
		"odd":      {`{"usage":{"prompt_tokens":10,"completion_tokens":1,"prompt_cache_hit_tokens":99}}`, aiUsage{In: 0, Hit: 10, Out: 1}},
	} {
		if got := openAIUsage([]byte(c.body)); got != c.want {
			t.Errorf("%s: %+v, want %+v", name, got, c.want)
		}
	}
	if got := claudeUsage([]byte(`{"usage":{"input_tokens":30,"output_tokens":7,"cache_read_input_tokens":500,"cache_creation_input_tokens":60}}`)); got != (aiUsage{In: 30, Hit: 500, Write: 60, Out: 7}) {
		t.Errorf("claude: %+v", got)
	}
}

func TestDeepSeekPeak(t *testing.T) {
	bj := time.FixedZone("CST", 8*3600)
	for _, c := range []struct {
		at   string
		peak bool
	}{
		{"2026-10-12 08:59", false}, {"2026-10-12 09:00", true}, {"2026-10-12 11:59", true}, {"2026-10-12 12:00", false},
		{"2026-10-12 13:59", false}, {"2026-10-12 14:00", true}, {"2026-10-16 17:59", true}, {"2026-10-16 18:00", false},
		{"2026-10-17 10:00", false}, {"2026-10-18 15:00", false},
	} {
		at, _ := time.ParseInLocation("2006-01-02 15:04", c.at, bj)
		if got := deepseekPeak(at.In(time.FixedZone("JST", 9*3600))); got != c.peak { // (whatever the computer's own zone)
			t.Errorf("%s: %v", c.at, got)
		}
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// Every answer's tokens are kept by day, service and model, and priced: DeepSeek by its own list (twice as
// much in its busy hours), another model at the price the player gives it.
func TestUsageRecordAndReport(t *testing.T) {
	testkit.NewStore(t)
	quiet := time.Date(2026, 10, 10, 10, 0, 0, 0, time.FixedZone("CST", 8*3600)) // a Saturday
	busy := time.Date(2026, 10, 12, 10, 0, 0, 0, time.FixedZone("CST", 8*3600))  // a Monday morning
	clock := quiet
	usageNow = func() time.Time { return clock }
	t.Cleanup(func() { usageNow = time.Now })
	ds := &aiClient{info: *aiProvider("deepseek"), model: "deepseek-flash"}
	var sum usageSum
	ctx := withUsageSink(context.Background(), &sum)
	ds.noteUsage(ctx, aiUsage{In: 900, Hit: 100, Out: 50})
	ds.noteUsage(ctx, aiUsage{}) // nothing said about it: nothing kept
	clock = busy
	ds.noteUsage(ctx, aiUsage{In: 900, Hit: 100, Out: 50})
	quietCost := (900*1 + 100*0.02 + 50*4) / 1e6
	v := sum.view()
	if v.Calls != 2 || v.Tokens != 2100 || !near(v.Cost["CNY"], quietCost*3) || v.Unpriced != 0 {
		t.Errorf("the conversation's: %+v (want ¥%f)", v, quietCost*3)
	}
	// another service's model: no price yet, then the player's
	gpt := &aiClient{info: *aiProvider("openai"), model: "gpt-x"}
	gpt.noteUsage(context.Background(), aiUsage{In: 1000, Out: 1000})
	rep := usageReport()
	models := rep["models"].([]*usageModel)
	if len(models) != 2 || models[0].Model != "deepseek-flash" || models[1].Model != "gpt-x" {
		t.Fatalf("models: %+v", models)
	}
	d, g := models[0], models[1]
	if d.Today.Tokens != 1050 || !near(d.Today.Cost["CNY"], quietCost*2) || d.All.Tokens != 2100 || !near(d.All.Cost["CNY"], quietCost*3) || d.Month.Calls != 2 || !d.Builtin || d.Price.Peak != 2 {
		t.Errorf("deepseek: %+v", d)
	}
	if g.Price != nil || g.All.Unpriced != 2000 || g.All.Cost != nil {
		t.Errorf("gpt without a price: %+v", g)
	}
	if err := setPrice("openai", "gpt-x", &aiPrice{Currency: "USD", In: 2, Hit: 0.5, Out: 8, Peak: 5}); err != nil {
		t.Fatal(err)
	}
	g = usageReport()["models"].([]*usageModel)[1]
	if g.Price == nil || g.Price.Peak != 0 || g.Builtin || !near(g.All.Cost["USD"], (1000*2+1000*8)/1e6) {
		t.Errorf("gpt with the player's price: %+v %+v", g, g.Price)
	}
	for _, bad := range []*aiPrice{{Currency: "EUR", In: 1}, {Currency: "CNY", In: -1}, {Currency: "CNY"}, {Currency: "CNY", In: 1e9}} {
		if setPrice("openai", "gpt-x", bad) == nil {
			t.Errorf("taken: %+v", bad)
		}
	}
	// the player's price for a DeepSeek model keeps its busy hours dearer; nil goes back to DeepSeek's list
	if err := setPrice("deepseek", "deepseek-flash", &aiPrice{Currency: "CNY", In: 2, Hit: 0.1, Out: 8}); err != nil {
		t.Fatal(err)
	}
	if d = usageReport()["models"].([]*usageModel)[0]; d.Builtin || d.Price.Peak != 2 || d.Price.In != 2 {
		t.Errorf("deepseek with the player's price: %+v", d.Price)
	}
	_ = setPrice("deepseek", "deepseek-flash", nil)
	if d = usageReport()["models"].([]*usageModel)[0]; !d.Builtin || d.Price.In != 1 {
		t.Errorf("back to the list: %+v", d.Price)
	}
	// today's, over every model
	if tv := usageToday(); tv.Tokens != 1050+2000 || !near(tv.Cost["CNY"], quietCost*2) || !near(tv.Cost["USD"], 0.01) {
		t.Errorf("today: %+v", tv)
	}
	// kept on disk: read again in a fresh start
	usageMu.Lock()
	usageFrom = ""
	usageMu.Unlock()
	if d = usageReport()["models"].([]*usageModel)[0]; d.All.Tokens != 2100 {
		t.Errorf("read again: %+v", d.All)
	}
	// counting begins again; the prices stay
	if err := clearUsage(); err != nil {
		t.Fatal(err)
	}
	rep = usageReport()
	if ms := rep["models"].([]*usageModel); len(ms) != 0 || rep["since"].(int64) != busy.Unix() {
		t.Errorf("cleared: %+v %v", ms, rep["since"])
	}
	usageMu.Lock()
	_, kept := usageLocked().Prices["openai/gpt-x"]
	usageMu.Unlock()
	if !kept {
		t.Error("the prices went with the counts")
	}
}

// A run of the pipeline counts its answers: shown with the conversation (and gone with it), and today's.
func TestUsageOfARun(t *testing.T) {
	st := testkit.NewStore(t)
	proj := t.TempDir()
	u := &sceneUnity{}
	unitytest.FakeUnity(t, proj, u.answer)
	st.Projects = []core.ProjectInfo{{Name: "P", Path: proj}}
	usageNow = func() time.Time { return time.Date(2026, 10, 11, 10, 0, 0, 0, time.FixedZone("CST", 8*3600)) } // a Sunday
	t.Cleanup(func() { usageNow = time.Now })
	llm := &mockLLM{wire: "openai", script: []mockTurn{
		{text: "看看。", calls: []aiCall{{ID: "c1", Name: "inspect_avatar", Args: json.RawMessage(`{}`)}},
			usage: map[string]any{"prompt_tokens": 3000, "completion_tokens": 100, "prompt_cache_hit_tokens": 1000}},
		{text: "好了。", usage: map[string]any{"prompt_tokens": 3500, "completion_tokens": 200, "prompt_cache_hit_tokens": 3000}},
	}}
	srv := httptest.NewServer(llm)
	defer srv.Close()
	key := "secret-key-12345678"
	if err := aiSave("deepseek", srv.URL, "deepseek-flash", &key, nil); err != nil {
		t.Fatal(err)
	}
	s := aiSessionFor(proj)
	s.reset()
	if err := startAIRun(st, aiRunReq{Project: proj, Mode: "chat", Text: "看看头像"}); err != nil {
		t.Fatal(err)
	}
	waitRun(t, s)
	v := s.used.view()
	want := (2000*1 + 1000*0.02 + 100*4 + 500*1 + 3000*0.02 + 200*4) / 1e6
	if v.Calls != 2 || v.Tokens != 6800 || v.In != 2500 || v.Hit != 4000 || v.Out != 300 || !near(v.Cost["CNY"], want) {
		t.Errorf("the run's: %+v (want ¥%f)", v, want)
	}
	if tv := usageToday(); tv.Tokens != 6800 {
		t.Errorf("today: %+v", tv)
	}
	s.reset()
	if v := s.used.view(); v.Calls != 0 || v.Tokens != 0 || v.Cost != nil {
		t.Errorf("after the log was cleared: %+v", v)
	}
}

// What is left on the account: DeepSeek's and Kimi's own addresses only, asked with the key.
func TestBalance(t *testing.T) {
	st := testkit.NewStore(t)
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path+" "+r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/user/balance":
			_, _ = w.Write([]byte(`{"is_available":true,"balance_infos":[{"currency":"USD","total_balance":"1.00","granted_balance":"0.00","topped_up_balance":"1.00"},{"currency":"CNY","total_balance":"12.34","granted_balance":"2.34","topped_up_balance":"10.00"}]}`))
		case "/v1/users/me/balance":
			_, _ = w.Write([]byte(`{"code":0,"data":{"available_balance":49.58894,"voucher_balance":46.58893,"cash_balance":3.00001},"scode":"0x0","status":true}`))
		default:
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":{"message":"Authentication Fails"}}`))
		}
	}))
	defer srv.Close()
	old := balanceAddr
	balanceAddr = func(kind string, base *url.URL) string {
		if !strings.HasPrefix(base.Host, "api.") {
			t.Errorf("asked of %s", base)
		}
		return srv.URL + strings.TrimPrefix(old(kind, base), base.Scheme+"://"+base.Host)
	}
	t.Cleanup(func() { balanceAddr = old })
	ctx := context.Background()
	key := "sk-test-0001"
	_ = aiSave("deepseek", "https://api.deepseek.com", "deepseek-flash", &key, nil)
	b, err := queryBalance(ctx, st)
	if err != nil || b.Currency != "CNY" || b.Total != 12.34 || b.Granted != 2.34 || b.Paid != 10 || !b.Usable {
		t.Errorf("deepseek: %+v %v", b, err)
	}
	if usageReport()["balance"] != true {
		t.Error("the window is not told that the balance can be asked for")
	}
	_ = aiSave("kimi", "https://api.moonshot.cn/v1", "kimi-k3", &key, nil)
	if b, err = queryBalance(ctx, st); err != nil || b.Total != 49.58894 || b.Granted != 46.58893 || b.Paid != 3.00001 {
		t.Errorf("kimi: %+v %v", b, err)
	}
	if len(asked) != 2 || asked[0] != "/user/balance Bearer sk-test-0001" || asked[1] != "/v1/users/me/balance Bearer sk-test-0001" {
		t.Errorf("asked: %v", asked)
	}
	// a relay's key is not DeepSeek's; GLM has no such question
	_ = aiSave("deepseek", "https://relay.example.com/v1", "deepseek-flash", &key, nil)
	if _, err = queryBalance(ctx, st); err == nil || !strings.Contains(err.Error(), "不支持查询余额") {
		t.Errorf("relay: %v", err)
	}
	_ = aiSave("glm", "", "glm-5.3", &key, nil)
	if _, err = queryBalance(ctx, st); err == nil || usageReport()["balance"] != false {
		t.Errorf("glm: %v", err)
	}
	if len(asked) != 2 {
		t.Errorf("asked again: %v", asked)
	}
}
