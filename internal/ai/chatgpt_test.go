package ai

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/testkit"
	"vrclib/internal/unity/unitytest"
)

// ---------- a stand-in for OpenAI: its sign-in, its signing keys, its API, and the player's browser ----------

var (
	gptTestKeysOnce sync.Once
	gptTestKey      *rsa.PrivateKey // OpenAI's signing key
	gptOtherKey     *rsa.PrivateKey // someone else's
)

func gptTestKeys() (*rsa.PrivateKey, *rsa.PrivateKey) {
	gptTestKeysOnce.Do(func() {
		gptTestKey, _ = rsa.GenerateKey(rand.Reader, 2048)
		gptOtherKey, _ = rsa.GenerateKey(rand.Reader, 2048)
	})
	return gptTestKey, gptOtherKey
}

func signJWT(key *rsa.PrivateKey, kid string, claims map[string]any) string {
	h, _ := json.Marshal(map[string]any{"alg": "RS256", "kid": kid, "typ": "JWT"})
	p, _ := json.Marshal(claims)
	in := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(p)
	sum := sha256.Sum256([]byte(in))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	return in + "." + base64.RawURLEncoding.EncodeToString(sig)
}

type fakeCode struct{ client, challenge, redirect, nonce string }

type fakeFail struct {
	status int
	body   string
	reqID  string
	stale  bool // the access token the request came with is no good any more
}

type fakeOpenAI struct {
	srv *httptest.Server
	mu  sync.Mutex

	// the account the browser signs in as, and what OpenAI's page does
	sub, email, scope string
	issued            string // the client ID a first registration is given
	deny              string // the page sends the player back with this error
	answerClient      string // a reauthorization's answer names this client ID
	noClient          bool   // a first registration's answer names none
	idTweak           func(map[string]any)
	badSig            bool // the ID token is signed with another key

	codes          map[string]fakeCode
	refresh        map[string]string // live refresh token → its client ID
	access         map[string]bool
	refuse         string // a refresh is refused with this OAuth error
	refreshDown    bool   // … or not answered (503)
	exchangeRefuse string // a code exchange is refused with this

	authz        []url.Values
	rawAuthz     string // the last authorize address as the program wrote it
	tokens       []url.Values
	revokes      []url.Values
	revokeStatus int
	pages        chan string

	answers []string // the streamed answers of /v1/responses, in turn (the last one again)
	reqs    []map[string]any
	hdrs    []http.Header
	fail    []fakeFail // /v1/responses refuses with these first
	models  string

	respDelay   time.Duration // /v1/responses takes this long to answer
	tokenDelay  time.Duration // … and the token endpoint
	arrived     int           // requests that reached /v1/responses (before its delay)
	redirectAPI bool          // /v1/responses sends the request elsewhere (307)
	elsewhere   int           // … and how many came there
	earliest    int64         // the token endpoint's earliest_refresh_at (0: none)
}

func newFakeOpenAI(t *testing.T) *fakeOpenAI {
	t.Helper()
	gptTestKeys()
	f := &fakeOpenAI{sub: "user-1", email: "player@example.com", scope: gptScopes, issued: "oaiapp_one",
		codes: map[string]fakeCode{}, refresh: map[string]string{}, access: map[string]bool{}, pages: make(chan string, 16),
		answers: []string{sseAnswer("收到", 10, 0, 2)},
		models: `{"models":[{"slug":"gpt-b","display_name":"GPT B","visibility":"list"},{"slug":"gpt-hidden","display_name":"Hidden","visibility":"hide"},` +
			`{"slug":"gpt-a","display_name":"GPT A","visibility":"list"}]}`}
	f.srv = httptest.NewServer(f)
	auth, api, open, ports, wait, revWait := gptAuthBase, gptAPIBase, gptOpen, gptPorts, gptWait, gptRevokeWait
	gptAuthBase, gptAPIBase = f.srv.URL, f.srv.URL+"/v1"
	gptOpen = func(u string) error { go f.browse(u); return nil }
	gptPorts, gptRevokeWait = nil, []time.Duration{5 * time.Millisecond, 5 * time.Millisecond}
	gptKeysMu.Lock()
	gptKeySet = nil
	gptKeysMu.Unlock()
	gptPendMu.Lock()
	gptErr = ""
	gptPendMu.Unlock()
	t.Cleanup(func() {
		gptCancelSignIn()
		for i := 0; i < 100; i++ { // the sign-in in the background ends
			gptPendMu.Lock()
			p := gptPend
			gptPendMu.Unlock()
			if p == nil {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		f.srv.Close()
		gptAuthBase, gptAPIBase, gptOpen, gptPorts, gptWait, gptRevokeWait = auth, api, open, ports, wait, revWait
		gptKeysMu.Lock()
		gptKeySet = nil
		gptKeysMu.Unlock()
	})
	return f
}

// fakeDPAPI: the tokens' file as Windows would keep it — not readable as it is.
func fakeDPAPI(t *testing.T) {
	p, u := protect, unprotect
	protect = func(b []byte) ([]byte, error) {
		return append([]byte("DPAPI:"), base64.StdEncoding.EncodeToString(b)...), nil
	}
	unprotect = func(b []byte) ([]byte, error) {
		if !bytes.HasPrefix(b, []byte("DPAPI:")) {
			return nil, errors.New("not this user's")
		}
		return base64.StdEncoding.DecodeString(string(b[6:]))
	}
	t.Cleanup(func() { protect, unprotect = p, u })
}

func oauthErr(w http.ResponseWriter, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(400)
	_, _ = fmt.Fprintf(w, `{"error":%q,"error_description":"refused: %s"}`, code, code)
}

func (f *fakeOpenAI) issue(w http.ResponseWriter, client, nonce string, id bool) {
	at, rt := "at-"+randURL(9), "rt-"+randURL(9)
	f.access[at], f.refresh[rt] = true, client
	out := map[string]any{"access_token": at, "refresh_token": rt, "token_type": "Bearer", "expires_in": 3600, "scope": f.scope}
	if f.earliest != 0 {
		out["earliest_refresh_at"] = f.earliest
	}
	if id {
		claims := map[string]any{"iss": f.srv.URL, "aud": client, "sub": f.sub, "email": f.email, "nonce": nonce,
			"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix()}
		if f.idTweak != nil {
			f.idTweak(claims)
		}
		k, other := gptTestKeys()
		if f.badSig {
			k = other
		}
		out["id_token"] = signJWT(k, "k1", claims)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (f *fakeOpenAI) authed(w http.ResponseWriter, r *http.Request) bool {
	if f.access[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")] {
		return true
	}
	w.WriteHeader(401)
	_, _ = io.WriteString(w, `{"detail":"Unauthorized"}`)
	return false
}

func (f *fakeOpenAI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	var d time.Duration
	switch r.URL.Path {
	case "/v1/responses":
		f.arrived++
		d = f.respDelay
	case "/api/accounts/oauth/token":
		d = f.tokenDelay
	}
	f.mu.Unlock()
	if d > 0 { // (taking its time, without holding up the other requests; the body read first, so that the server sees the client go)
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		select {
		case <-time.After(d):
		case <-r.Context().Done():
			return
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.URL.Path {
	case "/v1/elsewhere":
		f.elsewhere++
		_, _ = io.WriteString(w, sseAnswer("不该到这里", 1, 0, 1))
	case "/.well-known/jwks.json":
		k, _ := gptTestKeys()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]any{{"kty": "RSA", "kid": "k1", "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(k.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(k.E)).Bytes())}}})
	case "/api/accounts/oauth/token":
		_ = r.ParseForm()
		form := r.PostForm
		f.tokens = append(f.tokens, form)
		switch form.Get("grant_type") {
		case "authorization_code":
			c, ok := f.codes[form.Get("code")]
			delete(f.codes, form.Get("code"))
			sum := sha256.Sum256([]byte(form.Get("code_verifier")))
			if f.exchangeRefuse != "" {
				oauthErr(w, f.exchangeRefuse)
				return
			}
			if !ok || form.Get("client_id") != c.client || form.Get("redirect_uri") != c.redirect || form.Get("resource") != gptAPIBase ||
				base64.RawURLEncoding.EncodeToString(sum[:]) != c.challenge || form.Has("client_secret") {
				oauthErr(w, "invalid_grant")
				return
			}
			f.issue(w, c.client, c.nonce, true)
		case "refresh_token":
			if f.refreshDown {
				w.WriteHeader(503)
				return
			}
			if f.refuse != "" {
				oauthErr(w, f.refuse)
				return
			}
			client, ok := f.refresh[form.Get("refresh_token")]
			if !ok || client != form.Get("client_id") || form.Get("resource") != gptAPIBase || form.Has("scope") {
				oauthErr(w, "invalid_grant")
				return
			}
			delete(f.refresh, form.Get("refresh_token")) // turned over: the old one is spent
			f.issue(w, client, "", false)
		default:
			oauthErr(w, "unsupported_grant_type")
		}
	case "/api/accounts/oauth/revoke":
		_ = r.ParseForm()
		f.revokes = append(f.revokes, r.PostForm)
		if f.revokeStatus != 0 {
			w.WriteHeader(f.revokeStatus)
			return
		}
		delete(f.refresh, r.PostForm.Get("token"))
	case "/v1/models":
		if f.authed(w, r) {
			_, _ = io.WriteString(w, f.models)
		}
	case "/v1/responses":
		f.hdrs = append(f.hdrs, r.Header.Clone())
		if f.redirectAPI {
			http.Redirect(w, r, "/v1/elsewhere", http.StatusTemporaryRedirect)
			return
		}
		if len(f.fail) > 0 {
			x := f.fail[0]
			f.fail = f.fail[1:]
			if x.stale {
				delete(f.access, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
			}
			if x.reqID != "" {
				w.Header().Set("x-request-id", x.reqID)
			}
			w.WriteHeader(x.status)
			_, _ = io.WriteString(w, x.body)
			return
		}
		if !f.authed(w, r) {
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.reqs = append(f.reqs, body)
		ans := f.answers[0]
		if len(f.answers) > 1 {
			f.answers = f.answers[1:]
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, ans)
	default:
		http.NotFound(w, r)
	}
}

// browse: the player's browser on OpenAI's page — it signs in (or not) and is sent back to the program.
func (f *fakeOpenAI) browse(page string) {
	u, _ := url.Parse(page)
	q := u.Query()
	f.mu.Lock()
	f.authz, f.rawAuthz = append(f.authz, q), page
	cb := url.Values{"state": {q.Get("state")}}
	if f.deny != "" {
		cb.Set("error", f.deny)
	} else {
		client := q.Get("client_id")
		if client == gptDynamic {
			client = f.issued
			if !f.noClient {
				cb.Set("client_id", client)
			}
		} else if f.answerClient != "" {
			cb.Set("client_id", f.answerClient)
		}
		code := randURL(12)
		f.codes[code] = fakeCode{client: client, challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri"), nonce: q.Get("nonce")}
		cb.Set("code", code)
		cb.Set("scope", f.scope)
	}
	f.mu.Unlock()
	got := ""
	if resp, err := http.Get(q.Get("redirect_uri") + "?" + cb.Encode()); err == nil {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		got = string(b)
	}
	f.pages <- got
}

// signIn: a sign-in to its end, and the page the browser was left on ("" when the browser was not opened).
func (f *fakeOpenAI) signIn(t *testing.T, mode string) (string, error) {
	t.Helper()
	return f.signInAs(t, mode, "")
}

func (f *fakeOpenAI) signInAs(t *testing.T, mode, account string) (string, error) {
	t.Helper()
	f.mu.Lock()
	n := len(f.authz)
	f.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := gptSignIn(ctx, gptHTTP(nil, gptAuthBase), mode, account)
	f.mu.Lock()
	opened := len(f.authz) > n
	f.mu.Unlock()
	if !opened {
		return "", err
	}
	select {
	case p := <-f.pages:
		return p, err
	case <-time.After(10 * time.Second):
		t.Fatal("the browser was left without a page")
		return "", err
	}
}

func (f *fakeOpenAI) lastAuthz() url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.authz[len(f.authz)-1]
}

func sse(events ...map[string]any) string {
	var b strings.Builder
	for _, e := range events {
		j, _ := json.Marshal(e)
		fmt.Fprintf(&b, "event: %s\ndata: %s\n\n", e["type"], j)
	}
	return b.String()
}

func sseAnswer(text string, in, cached, out int) string {
	return sse(map[string]any{"type": "response.created", "response": map[string]any{"status": "in_progress"}},
		map[string]any{"type": "response.output_text.delta", "delta": text},
		map[string]any{"type": "response.completed", "response": map[string]any{"status": "completed",
			"output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": text}}}},
			"usage":  map[string]any{"input_tokens": in, "output_tokens": out, "input_tokens_details": map[string]any{"cached_tokens": cached}}}})
}

func gptTokensNow(t *testing.T) *gptTokens {
	t.Helper()
	aiMu.Lock()
	defer aiMu.Unlock()
	tk, err := loadGptTokensLocked()
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

func gptHostNow() gptHost {
	aiMu.Lock()
	defer aiMu.Unlock()
	h, _ := loadGptHostLocked()
	return h
}

// ---------- signing in ----------

// The first sign-in of a computer: OpenAI's page in the browser, with all a registration asks for; the code
// exchanged with PKCE and no secret; the ID token checked; the tokens kept encrypted, the registration not.
func TestGptFirstSignIn(t *testing.T) {
	core.DataDir = t.TempDir()
	fakeDPAPI(t)
	f := newFakeOpenAI(t)
	page, err := f.signIn(t, gptAgain)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(page, "登录完成") || !strings.Contains(page, "Signed in") {
		t.Errorf("the page the browser was left on: %s", page)
	}
	q := f.lastAuthz()
	f.mu.Lock()
	addr := f.rawAuthz
	f.mu.Unlock()
	if !strings.Contains(addr, "scope=openid%20profile%20email%20offline_access%20resource.invoke%20chatgpt.tokens.use.direct&") || strings.Contains(addr, "+") {
		t.Errorf("the scope's spaces: %s", addr)
	}
	want := map[string]string{"client_id": gptDynamic, "agent_name_hint": "MioVRCA", "response_type": "code", "scope": gptScopes,
		"resource": f.srv.URL + "/v1", "code_challenge_method": "S256"}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("authorize %s = %q, want %q", k, q.Get(k), v)
		}
	}
	if !regexp.MustCompile(`^urn:uuid:[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(q.Get("ext_agent_host_id")) {
		t.Errorf("host id %q", q.Get("ext_agent_host_id"))
	}
	if !regexp.MustCompile(`^http://127\.0\.0\.1:\d+/auth/callback$`).MatchString(q.Get("redirect_uri")) {
		t.Errorf("redirect %q", q.Get("redirect_uri"))
	}
	for _, k := range []string{"state", "nonce", "code_challenge"} {
		if len(q.Get(k)) < 40 {
			t.Errorf("%s too short: %q", k, q.Get(k))
		}
	}
	for _, k := range []string{"login_hint", "prompt", "id_token_hint"} {
		if q.Has(k) {
			t.Errorf("a first sign-in sends %s", k)
		}
	}
	f.mu.Lock()
	ex := f.tokens[0]
	f.mu.Unlock()
	if ex.Get("grant_type") != "authorization_code" || ex.Get("client_id") != "oaiapp_one" || ex.Get("redirect_uri") != q.Get("redirect_uri") || ex.Get("code_verifier") == "" || ex.Has("client_secret") {
		t.Errorf("exchange %v", ex)
	}
	h := gptHostNow()
	if h.HostID != q.Get("ext_agent_host_id") || len(h.Accounts) != 1 || h.Accounts[0] != (gptAccount{Sub: "user-1", ClientID: "oaiapp_one", Email: "player@example.com"}) ||
		h.Last != "user-1" || h.Pending != "" || h.Welcomed {
		t.Errorf("host %+v", h)
	}
	tk := gptTokensNow(t)
	if !tk.planOK() || tk.ClientID != "oaiapp_one" || tk.Sub != "user-1" || tk.Email != "player@example.com" || tk.IDToken == "" || tk.Expires < time.Now().Add(50*time.Minute).Unix() {
		t.Errorf("tokens %+v", tk)
	}
	raw, _ := os.ReadFile(gptAuthFile())
	hostRaw, _ := os.ReadFile(gptHostFile())
	if bytes.Contains(raw, []byte(tk.Access)) || bytes.Contains(raw, []byte(tk.Refresh)) || !bytes.HasPrefix(raw, []byte("DPAPI:")) {
		t.Error("the tokens are not kept encrypted")
	}
	if bytes.Contains(hostRaw, []byte(tk.Access)) || bytes.Contains(hostRaw, []byte(tk.Refresh)) || bytes.Contains(hostRaw, []byte("idToken")) {
		t.Error("a token in the registration's file")
	}
	v := aiView()
	if g := v.ChatGPT; !g.SignedIn || !g.Plan || g.Email != "player@example.com" || !g.Welcome || g.Pending || g.Err != "" || g.UsageURL != gptUsageURL {
		t.Errorf("view %+v", g)
	}
	if b, _ := json.Marshal(v); bytes.Contains(b, []byte(tk.Access)) || bytes.Contains(b, []byte(tk.Refresh)) {
		t.Error("a token in what the window is given")
	}
	if err := gptWelcomed(); err != nil || aiView().ChatGPT.Welcome {
		t.Errorf("the note is shown once only: %v", err)
	}
}

// Signing in again: the account's own client ID, its e-mail as a hint, no new registration; another account
// only through 「使用其他账号」, with a registration of its own; never while one is signed in.
func TestGptSignInAgainAndOther(t *testing.T) {
	core.DataDir = t.TempDir()
	f := newFakeOpenAI(t)
	if _, err := f.signIn(t, gptAgain); err != nil {
		t.Fatal(err)
	}
	host := f.lastAuthz().Get("ext_agent_host_id")
	if _, err := f.signIn(t, gptOther); err == nil || !strings.Contains(err.Error(), "请先退出登录") {
		t.Errorf("a second sign-in while signed in: %v", err)
	}
	ctx := context.Background()
	if revoked, err := gptSignOut(ctx, gptHTTP(nil, gptAuthBase)); err != nil || !revoked {
		t.Fatalf("sign-out %v %v", revoked, err)
	}
	if v := aiView().ChatGPT; v.SignedIn || len(v.Accounts) != 1 || v.Accounts[0] != (gptViewAcc{ID: "user-1", Email: "player@example.com"}) {
		t.Errorf("signed out %+v", v)
	}
	// the same account again
	if _, err := f.signIn(t, gptAgain); err != nil {
		t.Fatal(err)
	}
	q := f.lastAuthz()
	if q.Get("client_id") != "oaiapp_one" || q.Has("agent_name_hint") || q.Get("login_hint") != "player@example.com" || q.Get("ext_agent_host_id") != host {
		t.Errorf("reauthorization %v", q)
	}
	if tk := gptTokensNow(t); !tk.planOK() || tk.ClientID != "oaiapp_one" {
		t.Errorf("tokens %+v", tk)
	}
	// the page names another client ID than the one asked with: refused, nothing replaced
	_, _ = gptSignOut(ctx, gptHTTP(nil, gptAuthBase))
	f.answerClient = "oaiapp_evil"
	if _, err := f.signIn(t, gptAgain); err == nil || !strings.Contains(err.Error(), "不一致") {
		t.Errorf("another client ID: %v", err)
	}
	f.answerClient = ""
	// another account chosen on the page of the last one: refused
	f.sub, f.email = "user-2", "other@example.com"
	if _, err := f.signIn(t, gptAgain); err == nil || !strings.Contains(err.Error(), "使用其他账号") {
		t.Errorf("another account on the last one's registration: %v", err)
	}
	if tk := gptTokensNow(t); tk.signedIn() {
		t.Error("tokens kept from a refused sign-in")
	}
	// 「使用其他账号」: a registration of its own
	f.issued = "oaiapp_two"
	if _, err := f.signIn(t, gptOther); err != nil {
		t.Fatal(err)
	}
	q = f.lastAuthz()
	if q.Get("client_id") != gptDynamic || q.Get("agent_name_hint") != "MioVRCA" || q.Has("login_hint") || q.Get("ext_agent_host_id") != host {
		t.Errorf("new registration %v", q)
	}
	h := gptHostNow()
	if len(h.Accounts) != 2 || h.Last != "user-2" || h.account("user-2").ClientID != "oaiapp_two" || h.account("user-1").ClientID != "oaiapp_one" {
		t.Errorf("host %+v", h)
	}
	if tk := gptTokensNow(t); tk.ClientID != "oaiapp_two" || tk.Sub != "user-2" {
		t.Errorf("tokens %+v", tk)
	}
	// the revocation names the token and its own client ID
	if _, err := gptSignOut(ctx, gptHTTP(nil, gptAuthBase)); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	last := f.revokes[len(f.revokes)-1]
	live := len(f.refresh)
	f.mu.Unlock()
	if last.Get("token_type_hint") != "refresh_token" || last.Get("client_id") != "oaiapp_two" || !strings.HasPrefix(last.Get("token"), "rt-") {
		t.Errorf("revocation %v", last)
	}
	if live != 0 {
		t.Errorf("%d refresh tokens left alive at OpenAI", live)
	}
}

// What OpenAI's page or token endpoint can answer instead of a sign-in, and the ID tokens that are not this
// sign-in's: each one refused, nothing kept.
func TestGptSignInRefused(t *testing.T) {
	core.DataDir = t.TempDir()
	f := newFakeOpenAI(t)
	for _, c := range []struct {
		name string
		set  func()
		want string
	}{
		{"access_denied", func() { f.deny = "access_denied" }, "取消授权"},
		{"server_error", func() { f.deny = "server_error" }, "server_error"},
		{"no client ID", func() { f.noClient = true }, "未返回应用编号"},
		{"other key", func() { f.badSig = true }, "签名无效"},
		{"other nonce", func() { f.idTweak = func(c map[string]any) { c["nonce"] = "x" } }, "不符"},
		{"other audience", func() { f.idTweak = func(c map[string]any) { c["aud"] = "oaiapp_else" } }, "不符"},
		{"other issuer", func() { f.idTweak = func(c map[string]any) { c["iss"] = "https://auth.example.com" } }, "不符"},
		{"no subject", func() { f.idTweak = func(c map[string]any) { delete(c, "sub") } }, "不符"},
		{"run out", func() { f.idTweak = func(c map[string]any) { c["exp"] = time.Now().Add(-time.Hour).Unix() } }, "过期"},
	} {
		f.deny, f.noClient, f.badSig, f.idTweak = "", false, false, nil
		c.set()
		page, err := f.signIn(t, gptAgain)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
		if !strings.Contains(page, "未完成登录") || !strings.Contains(page, c.want) {
			t.Errorf("%s: page %q", c.name, page)
		}
		if tk := gptTokensNow(t); tk.signedIn() {
			t.Fatalf("%s: tokens kept", c.name)
		}
	}
	f.deny, f.noClient, f.badSig, f.idTweak = "", false, false, nil
	// the code is refused at the exchange: the client ID issued is kept for the next attempt, which goes on with it
	f.exchangeRefuse = "invalid_grant"
	if _, err := f.signIn(t, gptAgain); err == nil || !strings.Contains(err.Error(), "过期") {
		t.Errorf("refused exchange: %v", err)
	}
	if h := gptHostNow(); h.Pending != "oaiapp_one" || len(h.Accounts) != 0 {
		t.Errorf("host after a refused exchange %+v", h)
	}
	f.exchangeRefuse, f.issued = "", "oaiapp_never"
	if _, err := f.signIn(t, gptAgain); err != nil {
		t.Fatal(err)
	}
	if q := f.lastAuthz(); q.Get("client_id") != "oaiapp_one" || q.Has("agent_name_hint") {
		t.Errorf("the next attempt %v", q)
	}
	if h := gptHostNow(); h.Pending != "" || len(h.Accounts) != 1 || h.Accounts[0].ClientID != "oaiapp_one" {
		t.Errorf("host %+v", h)
	}
	// the window's view of a sign-in that did not complete
	_, _ = gptSignOut(context.Background(), gptHTTP(nil, gptAuthBase))
	f.deny = "access_denied"
	if err := gptStartSignIn(nil, gptAgain, ""); err != nil {
		t.Fatal(err)
	}
	<-f.pages
	for i := 0; i < 100 && aiView().ChatGPT.Pending; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if v := aiView().ChatGPT; v.Pending || !strings.Contains(v.Err, "取消授权") {
		t.Errorf("view %+v", v)
	}
	if err := gptStartSignIn(nil, "bogus", ""); err == nil {
		t.Error("an unknown way of signing in was taken")
	}
}

// A sign-in under way can be cancelled, and opened again; one at a time; and it does not wait forever.
func TestGptSignInPending(t *testing.T) {
	core.DataDir = t.TempDir()
	f := newFakeOpenAI(t)
	var opened []string
	var mu sync.Mutex
	gptOpen = func(u string) error { mu.Lock(); opened = append(opened, u); mu.Unlock(); return nil } // the player has not answered yet
	if err := gptStartSignIn(nil, gptAgain, ""); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		mu.Lock()
		n := len(opened)
		mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !aiView().ChatGPT.Pending {
		t.Fatal("not pending")
	}
	if err := gptStartSignIn(nil, gptAgain, ""); err == nil {
		t.Error("two sign-ins at once")
	}
	if err := gptReopen(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if len(opened) != 2 || opened[0] != opened[1] || !strings.HasPrefix(opened[0], f.srv.URL+"/api/accounts/authorize?") {
		t.Errorf("opened %v", opened)
	}
	mu.Unlock()
	gptCancelSignIn()
	for i := 0; i < 100 && aiView().ChatGPT.Pending; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if v := aiView().ChatGPT; v.Pending || v.Err != "" {
		t.Errorf("cancelled: %+v", v)
	}
	if err := gptReopen(); err == nil {
		t.Error("reopened with no sign-in under way")
	}
	gptWait = 100 * time.Millisecond
	if err := gptStartSignIn(nil, gptAgain, ""); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100 && aiView().ChatGPT.Pending; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if v := aiView().ChatGPT; v.Pending || !strings.Contains(v.Err, "超时") {
		t.Errorf("timed out: %+v", v)
	}
}

// A sign-in without the use of the plan stays a sign-in: shown, not ready, no client; asked again with
// prompt=consent for the same account, which then may use the plan.
func TestGptNoPlanThenConsent(t *testing.T) {
	st := testkit.NewStore(t)
	f := newFakeOpenAI(t)
	f.scope = "openid profile email offline_access"
	if _, err := f.signIn(t, gptAgain); err != nil {
		t.Fatal(err)
	}
	if err := aiSave("chatgpt", "", "gpt-a", nil, nil); err != nil {
		t.Fatal(err)
	}
	v := aiView()
	if !v.ChatGPT.SignedIn || v.ChatGPT.Plan || v.Ready || v.ChatGPT.Welcome {
		t.Errorf("view without the plan %+v ready=%v", v.ChatGPT, v.Ready)
	}
	if _, err := newAIClient(st); aiErrCode(err) != "plan" {
		t.Errorf("client without the plan: %v", err)
	}
	f.scope = gptScopes
	f.sub = "user-2" // another account picked on the page: refused
	if _, err := f.signIn(t, gptConsent); err == nil || !strings.Contains(err.Error(), "不同") {
		t.Errorf("consent from another account: %v", err)
	}
	f.sub = "user-1"
	if _, err := f.signIn(t, gptConsent); err != nil {
		t.Fatal(err)
	}
	q := f.lastAuthz()
	if q.Get("prompt") != "consent" || q.Get("client_id") != "oaiapp_one" || q.Has("id_token_hint") || q.Get("login_hint") != "player@example.com" || q.Get("scope") != gptScopes {
		t.Errorf("consent %v", q)
	}
	if v := aiView(); !v.Ready || !v.ChatGPT.Plan || !v.ChatGPT.Welcome {
		t.Errorf("after consent %+v ready=%v", v.ChatGPT, v.Ready)
	}
	_, _ = gptSignOut(context.Background(), gptHTTP(nil, gptAuthBase))
	if _, err := f.signIn(t, gptConsent); err == nil || aiErrCode(err) != "signin" {
		t.Errorf("consent while signed out: %v", err)
	}
}

// ---------- the tokens ----------

// The access token is refreshed before it runs out, once at a time (the refresh token turns over), and kept;
// a refresh refused for good ends the sign-in, one that does not get through leaves it.
func TestGptRefresh(t *testing.T) {
	core.DataDir = t.TempDir()
	f := newFakeOpenAI(t)
	if _, err := f.signIn(t, gptAgain); err != nil {
		t.Fatal(err)
	}
	hc := gptHTTP(nil, gptAuthBase)
	ctx := context.Background()
	refreshes := func() int {
		f.mu.Lock()
		defer f.mu.Unlock()
		n := 0
		for _, x := range f.tokens {
			if x.Get("grant_type") == "refresh_token" {
				n++
			}
		}
		return n
	}
	first := gptTokensNow(t)
	if at, err := gptAccess(ctx, hc, false, gptPin{}); err != nil || at != first.Access || refreshes() != 0 {
		t.Fatalf("fresh token: %v %v", at == first.Access, err)
	}
	soon := func() { // about to run out
		aiMu.Lock()
		tk, _ := loadGptTokensLocked()
		tk.Expires = time.Now().Add(time.Minute).Unix()
		_ = saveGptTokensLocked(tk)
		aiMu.Unlock()
	}
	soon()
	at, err := gptAccess(ctx, hc, false, gptPin{})
	tk := gptTokensNow(t)
	if err != nil || at == first.Access || tk.Access != at || tk.Refresh == first.Refresh || refreshes() != 1 || tk.IDToken != first.IDToken {
		t.Fatalf("refreshed: %v %+v", err, tk)
	}
	f.mu.Lock()
	x := f.tokens[len(f.tokens)-1]
	f.mu.Unlock()
	if x.Get("client_id") != "oaiapp_one" || x.Get("refresh_token") != first.Refresh || x.Get("resource") != gptAPIBase || x.Has("scope") {
		t.Errorf("refresh request %v", x)
	}
	// many requests at once, each wanting a fresh token: one refresh after another, each with the newest token
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := gptAccess(ctx, hc, true, gptPin{}); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("refreshes at once: %v", err)
		}
	}
	// OpenAI does not answer: the token in hand serves while it is good; asked for a new one, the error
	f.refreshDown = true
	soon()
	if at, err := gptAccess(ctx, hc, false, gptPin{}); err != nil || at != gptTokensNow(t).Access {
		t.Errorf("refresh not answered, token still good: %v", err)
	}
	if _, err := gptAccess(ctx, hc, true, gptPin{}); err == nil || aiErrCode(err) != "" {
		t.Errorf("refresh not answered, forced: %v", err)
	}
	if !gptTokensNow(t).signedIn() {
		t.Error("the sign-in was dropped for a refresh that did not get through")
	}
	f.refreshDown = false
	// refused for good: signed out, the registration stays
	for _, code := range []string{"invalid_grant", "refresh_token_reused", "refresh_token_expired", "invalid_refresh_token", "token_expired", "refresh_token_invalidated"} {
		if !gptTokensNow(t).signedIn() {
			if _, err := f.signIn(t, gptAgain); err != nil {
				t.Fatal(err)
			}
		}
		f.refuse = code
		if _, err := gptAccess(ctx, hc, true, gptPin{}); err != errGptExpired {
			t.Errorf("%s: %v", code, err)
		}
		if v := aiView().ChatGPT; gptTokensNow(t).signedIn() || len(gptHostNow().Accounts) != 1 || len(v.Accounts) != 1 || v.Accounts[0].Email != "player@example.com" {
			t.Errorf("%s: after %+v", code, gptHostNow())
		}
		f.refuse = ""
	}
	// the client ID is no longer known to OpenAI: the account's next sign-in registers anew
	if _, err := f.signIn(t, gptAgain); err != nil {
		t.Fatal(err)
	}
	f.refuse = "invalid_client"
	if _, err := gptAccess(ctx, hc, true, gptPin{}); err != errGptClientGone || aiErrCode(err) != "signin" {
		t.Errorf("invalid_client: %v", err)
	}
	f.refuse = ""
	if h := gptHostNow(); len(h.Accounts) != 0 {
		t.Errorf("registration kept after invalid_client: %+v", h)
	}
	f.issued = "oaiapp_new"
	if _, err := f.signIn(t, gptAgain); err != nil {
		t.Fatal(err)
	}
	if q := f.lastAuthz(); q.Get("client_id") != gptDynamic {
		t.Errorf("after invalid_client: %v", q)
	}
	// a sign-out OpenAI does not confirm: the tokens go all the same
	f.revokeStatus = 503
	if revoked, err := gptSignOut(ctx, hc); err != nil || revoked {
		t.Errorf("unconfirmed sign-out: %v %v", revoked, err)
	}
	f.mu.Lock()
	n := len(f.revokes)
	f.mu.Unlock()
	if n < 3 || gptTokensNow(t).signedIn() {
		t.Errorf("revocation tries %d", n)
	}
}

// ---------- the Responses API ----------

func gptReady(t *testing.T) (*core.Store, *fakeOpenAI) {
	t.Helper()
	st := testkit.NewStore(t)
	f := newFakeOpenAI(t)
	if _, err := f.signIn(t, gptAgain); err != nil {
		t.Fatal(err)
	}
	if err := aiSave("chatgpt", "", "gpt-a", nil, nil); err != nil {
		t.Fatal(err)
	}
	return st, f
}

// A turn as the plan takes it: store off, streamed, the system prompt as instructions, nothing it refuses; the
// pictures as input images, the tools and their answers as function calls; the answer read from the stream,
// what it used counted on the plan.
func TestGptChat(t *testing.T) {
	st, f := gptReady(t)
	usageNow = func() time.Time { return time.Date(2026, 10, 13, 12, 0, 0, 0, time.Local) }
	t.Cleanup(func() { usageNow = time.Now })
	if v := aiView(); !v.Ready || v.Provider != "chatgpt" {
		t.Fatalf("not ready %+v", v)
	}
	c, err := newAIClient(st)
	if err != nil {
		t.Fatal(err)
	}
	f.answers = []string{sse(
		map[string]any{"type": "response.output_text.delta", "delta": "先看"},
		map[string]any{"type": "response.output_text.delta", "delta": "一下。"},
		map[string]any{"type": "response.output_item.done", "item": map[string]any{"type": "function_call", "call_id": "call_9", "name": "inspect_avatar", "arguments": `{"deep":true}`}},
		map[string]any{"type": "response.completed", "response": map[string]any{"status": "completed",
			"output": []any{map[string]any{"type": "function_call", "call_id": "call_9", "name": "inspect_avatar", "arguments": `{"deep":true}`}},
			"usage":  map[string]any{"input_tokens": 1000, "output_tokens": 50, "input_tokens_details": map[string]any{"cached_tokens": 400}}}})}
	img := aiImage{Mime: "image/jpeg", Data: []byte("JPEG")}
	turns := []aiTurn{
		{Role: "user", Text: "装上衣服", Images: []aiImage{img}},
		{Role: "assistant", Text: "好的", Calls: []aiCall{{ID: "call_1", Name: "dress", Args: json.RawMessage(`{"a":1}`)}}},
		{Role: "tool", Results: []aiResult{{ID: "call_1", Name: "dress", Content: "已装上", Images: []aiImage{img}}}},
	}
	tools := []aiTool{{Name: "inspect_avatar", Desc: "看看", Params: map[string]any{"type": "object"}}}
	out, err := c.chat(context.Background(), "你是助手", turns, tools)
	if err != nil {
		t.Fatal(err)
	}
	if out.Text != "先看一下。" || len(out.Calls) != 1 || out.Calls[0].ID != "call_9" || string(out.Calls[0].Args) != `{"deep":true}` {
		t.Errorf("answer %+v", out)
	}
	f.mu.Lock()
	body, hdr := f.reqs[0], f.hdrs[0]
	f.mu.Unlock()
	if body["store"] != false || body["stream"] != true || body["model"] != "gpt-a" || body["instructions"] != "你是助手" {
		t.Errorf("body %v", body)
	}
	for _, k := range []string{"temperature", "max_output_tokens", "max_tokens", "metadata", "previous_response_id", "user", "background", "top_p", "truncation", "messages"} {
		if _, ok := body[k]; ok {
			t.Errorf("sent %s, which the plan does not take", k)
		}
	}
	j, _ := json.Marshal(body["input"])
	in := string(j)
	for _, want := range []string{`{"content":[{"text":"装上衣服","type":"input_text"},{"image_url":"data:image/jpeg;base64,SlBFRw==","type":"input_image"}],"role":"user"}`,
		`{"content":"好的","role":"assistant"}`, `{"arguments":"{\"a\":1}","call_id":"call_1","name":"dress","type":"function_call"}`,
		`{"call_id":"call_1","output":"已装上","type":"function_call_output"}`, `"role":"user"`} {
		if !strings.Contains(in, want) {
			t.Errorf("input lacks %s:\n%s", want, in)
		}
	}
	if strings.Contains(in, `"system"`) || strings.Count(in, "input_image") != 2 || strings.Contains(in, `"id":"`) {
		t.Errorf("input %s", in)
	}
	tj, _ := json.Marshal(body["tools"])
	if string(tj) != `[{"description":"看看","name":"inspect_avatar","parameters":{"type":"object"},"type":"function"}]` {
		t.Errorf("tools %s", tj)
	}
	if !strings.HasPrefix(hdr.Get("Authorization"), "Bearer at-") || hdr.Get("Accept") != "text/event-stream" || hdr.Get("x-api-key") != "" {
		t.Errorf("headers %v", hdr)
	}
	// counted on the plan: no price, nothing unpriced
	rep := usageReport()
	ms := rep["models"].([]*usageModel)
	if len(ms) != 1 || !ms[0].Plan || ms[0].Price != nil || ms[0].All.Plan != 1050 || ms[0].All.Unpriced != 0 || ms[0].All.Hit != 400 || ms[0].All.Cost != nil {
		t.Errorf("usage %+v", ms[0])
	}
	if u := usageToday(); u.Plan != 1050 || u.Unpriced != 0 || u.Cost != nil {
		t.Errorf("today %+v", u)
	}
	if err := setPrice("chatgpt", "gpt-a", &aiPrice{Currency: "USD", In: 1, Out: 1}); err == nil {
		t.Error("a price was set for the plan")
	}
	// the models of the account: the listed ones, in OpenAI's order, with their names
	ms2, err := c.models(context.Background())
	if err != nil || strings.Join(ms2, ",") != "gpt-b,gpt-a" || c.names["gpt-b"] != "GPT B" || c.names["gpt-hidden"] != "" {
		t.Errorf("models %v %v %v", ms2, c.names, err)
	}
}

// The plan's limit: 「管理用量」 is the way out, the request is not sent again, and nothing more is asked of
// the plan by this client — in the middle of an answer or before it.
func TestGptUsageLimit(t *testing.T) {
	st, f := gptReady(t)
	quickRetries(t)
	c, _ := newAIClient(st)
	retried := 0
	c.retry = func(n, of int) func(bool) { retried++; return func(bool) {} }
	f.answers = []string{sse(map[string]any{"type": "response.created"}, map[string]any{"type": "response.failed", "response": map[string]any{"status": "failed",
		"error": map[string]any{"code": "subscription_sharing_usage_limit_exceeded", "message": "limit"}}})}
	_, err := c.chat(context.Background(), "", []aiTurn{{Role: "user", Text: "hi"}}, nil)
	if aiErrCode(err) != "usage" || !strings.Contains(err.Error(), gptUsageURL) || !strings.Contains(err.Error(), "用量上限") {
		t.Fatalf("limit in the stream: %v", err)
	}
	f.mu.Lock()
	n := len(f.hdrs)
	f.mu.Unlock()
	if _, err2 := c.chat(context.Background(), "", []aiTurn{{Role: "user", Text: "again"}}, nil); err2 != err {
		t.Errorf("asked again after the limit: %v", err2)
	}
	f.mu.Lock()
	if len(f.hdrs) != n {
		t.Error("a request went out after the limit")
	}
	f.mu.Unlock()
	// before the answer: HTTP 429 with the plan's code is not sent again
	c, _ = newAIClient(st)
	c.retry = func(n, of int) func(bool) { retried++; return func(bool) {} }
	f.fail = []fakeFail{{status: 429, body: `{"error":{"code":"subscription_sharing_usage_limit_exceeded","message":"You have hit your usage limit."}}`, reqID: "req_123"}}
	_, err = c.chat(context.Background(), "", []aiTurn{{Role: "user", Text: "hi"}}, nil)
	if aiErrCode(err) != "usage" || retried != 0 || !strings.Contains(err.Error(), "req_123") {
		t.Errorf("limit before the answer: %v (retried %d)", err, retried)
	}
	// the plan unavailable for a moment: sent again, as a busy service is
	c, _ = newAIClient(st)
	c.retry = func(n, of int) func(bool) { retried++; return func(bool) {} }
	f.fail = []fakeFail{{status: 503, body: `{"error":{"code":"subscription_sharing_usage_unavailable","message":"try later"}}`}}
	f.answers = []string{sseAnswer("好", 5, 0, 1)}
	if out, err := c.chat(context.Background(), "", []aiTurn{{Role: "user", Text: "hi"}}, nil); err != nil || out.Text != "好" || retried != 1 {
		t.Errorf("unavailable, then answered: %v %q retried %d", err, out.Text, retried)
	}
	// not eligible: said so, not sent again, no more requests
	c, _ = newAIClient(st)
	f.fail = []fakeFail{{status: 403, body: `{"error":{"code":"subscription_sharing_user_not_eligible","message":"no"}}`}}
	_, err = c.chat(context.Background(), "", []aiTurn{{Role: "user", Text: "hi"}}, nil)
	if aiErrCode(err) != "denied" || !strings.Contains(err.Error(), "不能在本软件中使用") {
		t.Errorf("not eligible: %v", err)
	}
	// a capability the plan does not take: what it was is said, the request may be sent without it
	c, _ = newAIClient(st)
	f.fail = []fakeFail{{status: 400, body: `{"error":{"code":"subscription_sharing_unsupported_capability","param":"input[0].content[1].image_url","message":"unsupported"}}`}}
	_, err = c.chat(context.Background(), "", []aiTurn{{Role: "user", Text: "hi"}}, nil)
	var he *aiHTTPError
	if !errors.As(err, &he) || !he.noPictures() || aiErrCode(err) != "" || !strings.Contains(err.Error(), "image_url") {
		t.Errorf("unsupported capability: %v", err)
	}
	if out, err := c.chat(context.Background(), "", []aiTurn{{Role: "user", Text: "hi"}}, nil); err != nil || out.Text == "" {
		t.Errorf("the client stopped after an unsupported capability: %v", err)
	}
	// an answer the stream does not finish is no answer
	f.answers = []string{sse(map[string]any{"type": "response.output_text.delta", "delta": "半"})}
	if _, err := c.chat(context.Background(), "", []aiTurn{{Role: "user", Text: "hi"}}, nil); err == nil || !strings.Contains(err.Error(), "中断") {
		t.Errorf("cut stream: %v", err)
	}
	f.answers = []string{sse(map[string]any{"type": "response.incomplete", "response": map[string]any{"incomplete_details": map[string]any{"reason": "max_output_tokens"}}})}
	if _, err := c.chat(context.Background(), "", []aiTurn{{Role: "user", Text: "hi"}}, nil); err == nil || !strings.Contains(err.Error(), "截断") {
		t.Errorf("incomplete: %v", err)
	}
	f.answers = []string{sse(map[string]any{"type": "response.failed", "response": map[string]any{"error": map[string]any{"code": "context_length_exceeded", "message": "too long"}}})}
	_, err = c.chat(context.Background(), "", []aiTurn{{Role: "user", Text: "hi"}}, nil)
	if !errors.As(err, &he) || !he.overflow() {
		t.Errorf("too long: %v", err)
	}
}

// A refused access token is refreshed and the request sent once more; refused again, the player is told, the
// sign-in stays. A sign-in that is gone is said as such, before anything is sent.
func TestGptUnauthorized(t *testing.T) {
	st, f := gptReady(t)
	c, _ := newAIClient(st)
	before := gptTokensNow(t)
	f.fail = []fakeFail{{status: 401, body: `{"detail":"Unauthorized"}`, stale: true}}
	if out, err := c.chat(context.Background(), "", []aiTurn{{Role: "user", Text: "hi"}}, nil); err != nil || out.Text != "收到" {
		t.Fatalf("after a refresh: %v", err)
	}
	if now := gptTokensNow(t); now.Access == before.Access || now.Refresh == before.Refresh {
		t.Error("not refreshed")
	}
	f.fail = []fakeFail{{status: 401, body: `{"error":{"code":"subscription_sharing_invalid_user","message":"bad"}}`, reqID: "req_a"},
		{status: 401, body: `{"error":{"code":"subscription_sharing_invalid_user","message":"bad"}}`, reqID: "req_b"}}
	_, err := c.chat(context.Background(), "", []aiTurn{{Role: "user", Text: "hi"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "未接受当前登录") || !strings.Contains(err.Error(), "req_b") || aiErrCode(err) != "" {
		t.Errorf("refused twice: %v", err)
	}
	if !gptTokensNow(t).signedIn() {
		t.Error("the sign-in was dropped for a refusal")
	}
	f.mu.Lock()
	f.refuse = "invalid_grant"
	f.mu.Unlock()
	c2, _ := newAIClient(st)
	f.fail = []fakeFail{{status: 401, body: `{"detail":"Unauthorized"}`, stale: true}}
	_, err = c2.chat(context.Background(), "", []aiTurn{{Role: "user", Text: "hi"}}, nil)
	if aiErrCode(err) != "signin" || gptTokensNow(t).signedIn() {
		t.Errorf("refresh refused for good: %v", err)
	}
	if _, err := newAIClient(st); aiErrCode(err) != "signin" {
		t.Errorf("client while signed out: %v", err)
	}
	if v := aiView(); v.Ready {
		t.Error("ready while signed out")
	}
}

// The provider's form: no address and no key are kept for it, whatever is sent; the key of the address it
// shares with 「ChatGPT 兼容」 is never used for it.
func TestGptSaveNoKey(t *testing.T) {
	core.DataDir = t.TempDir()
	key := "sk-should-not-be-kept-1234"
	if err := aiSave("chatgpt", "https://relay.example.com/v1", "gpt-a", &key, nil); err != nil {
		t.Fatal(err)
	}
	v := aiView()
	if v.Provider != "chatgpt" || v.Profiles["chatgpt"].BaseURL != "https://api.openai.com/v1" || v.Keys["chatgpt"] != "" || v.Ready {
		t.Errorf("saved %+v", v)
	}
	k2 := "sk-openai-compatible-9876"
	if err := aiSave("openai", "https://api.openai.com/v1", "gpt-x", &k2, nil); err != nil {
		t.Fatal(err)
	}
	if err := aiSave("chatgpt", "", "gpt-a", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := newAIClient(nil); aiErrCode(err) != "signin" {
		t.Errorf("the API key stood in for the sign-in: %v", err)
	}
}

// The routes the window uses, end to end: sign in, the plan in use, models, sign out.
func TestGptRoutes(t *testing.T) {
	st := testkit.NewStore(t)
	f := newFakeOpenAI(t)
	old := menuLang
	t.Cleanup(func() { menuLang = old })
	routes := map[string]func(http.ResponseWriter, map[string]json.RawMessage){}
	RegisterAI(st, func(p string, h func(http.ResponseWriter, map[string]json.RawMessage)) { routes[p] = h })
	call := func(path string, body map[string]any) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(body)
		var b map[string]json.RawMessage
		_ = json.Unmarshal(raw, &b)
		rec := httptest.NewRecorder()
		h := routes[path]
		if h == nil {
			t.Fatalf("no route %s", path)
		}
		h(rec, b)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}
	r := call("/api/ai/chatgpt/signin", map[string]any{"mode": ""})
	if r["ok"] != true {
		t.Fatalf("signin %v", r)
	}
	<-f.pages
	var g map[string]any
	for i := 0; i < 200; i++ {
		g = call("/api/ai/config", nil)["ai"].(map[string]any)["chatgpt"].(map[string]any)
		if g["pending"] != true {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if g["signedIn"] != true || g["plan"] != true || g["welcome"] != true {
		t.Fatalf("after signing in %v", g)
	}
	if r := call("/api/ai/chatgpt/welcomed", nil); r["ok"] != true || r["ai"].(map[string]any)["chatgpt"].(map[string]any)["welcome"] == true {
		t.Errorf("welcomed %v", r)
	}
	r = call("/api/ai/models", map[string]any{"provider": "chatgpt", "baseUrl": "", "model": "", "key": ""})
	if r["ok"] != true || fmt.Sprint(r["models"]) != "[gpt-b gpt-a]" || r["names"].(map[string]any)["gpt-a"] != "GPT A" {
		t.Errorf("models %v", r)
	}
	r = call("/api/ai/test", map[string]any{"provider": "chatgpt", "model": "gpt-a"})
	if r["ok"] != true || !strings.Contains(fmt.Sprint(r["note"]), "收到") {
		t.Errorf("test %v", r)
	}
	r = call("/api/ai/save", map[string]any{"provider": "chatgpt", "baseUrl": "", "model": "gpt-a", "key": "x"})
	if r["ok"] != true || r["ai"].(map[string]any)["ready"] != true {
		t.Errorf("save %v", r)
	}
	r = call("/api/ai/chatgpt/signout", nil)
	if r["ok"] != true || r["revoked"] != true || r["ai"].(map[string]any)["ready"] == true {
		t.Errorf("signout %v", r)
	}
	if r := call("/api/ai/chatgpt/reopen", nil); r["ok"] == true {
		t.Error("reopen with nothing under way")
	}
}

// A run that meets the plan's limit ends with the step that offers 「管理用量」, and no further request.
func TestGptRunLimit(t *testing.T) {
	st := testkit.NewStore(t)
	proj := t.TempDir()
	u := &sceneUnity{}
	unitytest.FakeUnity(t, proj, u.answer)
	st.Projects = []core.ProjectInfo{{Name: "P", Path: proj}}
	f := newFakeOpenAI(t)
	if _, err := f.signIn(t, gptAgain); err != nil {
		t.Fatal(err)
	}
	if err := aiSave("chatgpt", "", "gpt-a", nil, nil); err != nil {
		t.Fatal(err)
	}
	f.answers = []string{sse(map[string]any{"type": "response.failed", "response": map[string]any{"error": map[string]any{"code": "subscription_sharing_usage_limit_exceeded", "message": "limit"}}})}
	if err := startAIRun(st, aiRunReq{Project: proj, Mode: "chat", Text: "看看模型"}); err != nil {
		t.Fatal(err)
	}
	steps := waitRun(t, aiSessionFor(proj))
	last := steps[len(steps)-1]
	if last.Kind != "error" || last.Code != "usage" || !strings.Contains(last.Text, "用量上限") {
		t.Errorf("last step %+v", last)
	}
	f.mu.Lock()
	n := len(f.hdrs)
	f.mu.Unlock()
	if n != 1 {
		t.Errorf("%d requests", n)
	}
}

// what the window is told about each kind of refusal
func TestGptErrCodes(t *testing.T) {
	if aiErrCode(nil) != "" || aiErrCode(errors.New("x")) != "" || aiErrCode(errGptExpired) != "signin" || aiErrCode(fmt.Errorf("wrapped: %w", errGptNoPlan)) != "plan" {
		t.Error("codes")
	}
	he := gptHTTPError(403, "", []byte(`{"detail":"Region not supported"}`))
	if !strings.Contains(he.Msg, "Region not supported") || !strings.Contains(he.Msg, "地区") || he.busy() {
		t.Errorf("403 %+v", he)
	}
	he = gptHTTPError(503, "", []byte(`{"detail":"Direct routing unavailable"}`))
	if !he.busy() {
		t.Error("503 not busy")
	}
	he = gptHTTPError(403, "", []byte(`{"error":{"code":"chatpass_v2_scope_not_authorized","message":"m"}}`))
	if he.code != "plan" || he.busy() {
		t.Errorf("scope %+v", he)
	}
}

// ---------- what the review asked for ----------

// The accounts signed in here before are offered by name; signing in again as one of them goes on with its own
// client ID (no new registration). One no longer known is refused.
func TestGptAccounts(t *testing.T) {
	core.DataDir = t.TempDir()
	f := newFakeOpenAI(t)
	ctx := context.Background()
	if _, err := f.signIn(t, gptAgain); err != nil {
		t.Fatal(err)
	}
	_, _ = gptSignOut(ctx, gptHTTP(nil, gptAuthBase))
	f.sub, f.email, f.issued = "user-2", "two@example.com", "oaiapp_two"
	if _, err := f.signIn(t, gptOther); err != nil {
		t.Fatal(err)
	}
	_, _ = gptSignOut(ctx, gptHTTP(nil, gptAuthBase))
	v := aiView().ChatGPT
	if len(v.Accounts) != 2 || v.Accounts[0] != (gptViewAcc{ID: "user-2", Email: "two@example.com"}) || v.Accounts[1] != (gptViewAcc{ID: "user-1", Email: "player@example.com"}) {
		t.Fatalf("accounts %+v", v.Accounts)
	}
	f.sub, f.email, f.issued = "user-1", "player@example.com", "oaiapp_never"
	if _, err := f.signInAs(t, gptAgain, "user-1"); err != nil {
		t.Fatal(err)
	}
	if q := f.lastAuthz(); q.Get("client_id") != "oaiapp_one" || q.Get("login_hint") != "player@example.com" || q.Has("agent_name_hint") {
		t.Errorf("signing in again as a saved account %v", q)
	}
	if h := gptHostNow(); len(h.Accounts) != 2 || h.Last != "user-1" {
		t.Errorf("host %+v", h)
	}
	if v := aiView().ChatGPT; len(v.Accounts) != 0 || !v.SignedIn {
		t.Errorf("signed in: no accounts offered %+v", v)
	}
	_, _ = gptSignOut(ctx, gptHTTP(nil, gptAuthBase))
	if _, err := f.signInAs(t, gptAgain, "nobody"); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Errorf("unknown account: %v", err)
	}
}

// A client ID left by a new registration that did not complete is tried again by the next attempt of the same
// kind only, for a while only, and once only; 「使用其他账号」 never goes on with it.
func TestGptPendingRules(t *testing.T) {
	core.DataDir = t.TempDir()
	f := newFakeOpenAI(t)
	f.exchangeRefuse = "invalid_grant"
	if _, err := f.signIn(t, gptAgain); err == nil {
		t.Fatal("refused exchange signed in")
	}
	if h := gptHostNow(); h.Pending != "oaiapp_one" || h.PendingAt == 0 {
		t.Fatalf("pending %+v", h)
	}
	f.exchangeRefuse, f.issued = "", "oaiapp_two"
	if _, err := f.signIn(t, gptOther); err != nil {
		t.Fatal(err)
	}
	if q := f.lastAuthz(); q.Get("client_id") != gptDynamic {
		t.Errorf("「使用其他账号」 went on with the pending client: %v", q)
	}
	if h := gptHostNow(); h.Pending != "" {
		t.Errorf("pending kept %+v", h)
	}
	// a fresh computer: one left long ago is not used
	core.DataDir = t.TempDir()
	f.exchangeRefuse, f.issued = "invalid_grant", "oaiapp_p"
	_, _ = f.signIn(t, gptAgain)
	aiMu.Lock()
	h, _ := loadGptHostLocked()
	h.PendingAt = time.Now().Add(-time.Hour).Unix()
	_ = saveGptHostLocked(h)
	aiMu.Unlock()
	f.exchangeRefuse, f.issued = "", "oaiapp_q"
	if _, err := f.signIn(t, gptAgain); err != nil {
		t.Fatal(err)
	}
	if q := f.lastAuthz(); q.Get("client_id") != gptDynamic {
		t.Errorf("an old pending client was used: %v", q)
	}
	// declined on the page: not tried again
	core.DataDir = t.TempDir()
	f.exchangeRefuse, f.issued = "invalid_grant", "oaiapp_r"
	_, _ = f.signIn(t, gptAgain)
	f.exchangeRefuse, f.deny = "", "access_denied"
	_, _ = f.signIn(t, gptAgain) // (goes on with oaiapp_r, and is declined)
	f.deny, f.issued = "", "oaiapp_s"
	if _, err := f.signIn(t, gptAgain); err != nil {
		t.Fatal(err)
	}
	if q := f.lastAuthz(); q.Get("client_id") != gptDynamic {
		t.Errorf("used twice: %v", q)
	}
}

// A client made for one account never sends another account's token: signed in again as someone else, the work
// in progress stops.
func TestGptPinned(t *testing.T) {
	st, f := gptReady(t)
	c, _ := newAIClient(st)
	_, _ = gptSignOut(context.Background(), gptHTTP(nil, gptAuthBase))
	f.sub, f.email, f.issued = "user-2", "two@example.com", "oaiapp_two"
	if _, err := f.signIn(t, gptOther); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	n := len(f.hdrs)
	f.mu.Unlock()
	if _, err := c.chat(context.Background(), "", []aiTurn{{Role: "user", Text: "hi"}}, nil); err != errGptSwitched || aiErrCode(err) != "signin" {
		t.Errorf("after another account signed in: %v", err)
	}
	f.mu.Lock()
	if len(f.hdrs) != n {
		t.Error("a request went out with the other account's token")
	}
	f.mu.Unlock()
}

// A refresh is not cut short by the end of the request that asked for it (the new refresh token would be lost);
// waiting for another refresh gives way to that end.
func TestGptRefreshNotCutShort(t *testing.T) {
	core.DataDir = t.TempDir()
	f := newFakeOpenAI(t)
	if _, err := f.signIn(t, gptAgain); err != nil {
		t.Fatal(err)
	}
	hc := gptHTTP(nil, gptAuthBase)
	f.mu.Lock()
	f.tokenDelay = 300 * time.Millisecond
	f.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	before := gptTokensNow(t)
	_, _ = gptAccess(ctx, hc, true, gptPin{})
	now := gptTokensNow(t)
	f.mu.Lock()
	_, live := f.refresh[now.Refresh]
	f.tokenDelay = 0
	f.mu.Unlock()
	if now.Refresh == before.Refresh || !live {
		t.Fatalf("the refresh was lost: %v %v", now.Refresh == before.Refresh, live)
	}
	if _, err := gptAccess(context.Background(), hc, true, gptPin{}); err != nil {
		t.Errorf("the next refresh: %v", err)
	}
	if err := gptLock(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel2()
	t0 := time.Now()
	_, err := gptAccess(ctx2, hc, false, gptPin{})
	gptUnlock()
	if err == nil || time.Since(t0) > time.Second {
		t.Errorf("waited past its end: %v %v", err, time.Since(t0))
	}
}

// Tokens a refresh brought that cannot be written are held, used, and written as soon as they can be.
func TestGptUnsavedTokens(t *testing.T) {
	core.DataDir = t.TempDir()
	f := newFakeOpenAI(t)
	if _, err := f.signIn(t, gptAgain); err != nil {
		t.Fatal(err)
	}
	w := aiReadWait
	aiReadWait = time.Millisecond
	p := protect
	t.Cleanup(func() { aiReadWait, protect = w, p })
	hc := gptHTTP(nil, gptAuthBase)
	fileTokens := func() gptTokens {
		var tk gptTokens
		b, _ := os.ReadFile(gptAuthFile())
		dec, _ := unprotect(b)
		_ = json.Unmarshal(dec, &tk)
		return tk
	}
	old := fileTokens()
	protect = func(b []byte) ([]byte, error) { return nil, errors.New("disk full") }
	at, err := gptAccess(context.Background(), hc, true, gptPin{})
	if err != nil || at == old.Access {
		t.Fatalf("refresh %v", err)
	}
	if fileTokens().Refresh != old.Refresh || gptTokensNow(t).Access != at || !aiView().ChatGPT.SignedIn {
		t.Fatal("the new tokens are not held while they cannot be written")
	}
	protect = p
	if at2, err := gptAccess(context.Background(), hc, false, gptPin{}); err != nil || at2 != at {
		t.Fatalf("next request %v", err)
	}
	if ft := fileTokens(); ft.Access != at || ft.Refresh == old.Refresh {
		t.Error("not written once it could be")
	}
	aiMu.Lock()
	held := gptUnsaved
	aiMu.Unlock()
	if held != nil {
		t.Error("still held after writing")
	}
}

// The browser's second answer (a reload of the page) is refused at once; only the first one counts.
func TestGptCallbackOnce(t *testing.T) {
	core.DataDir = t.TempDir()
	f := newFakeOpenAI(t)
	second := make(chan int, 1)
	gptOpen = func(page string) error {
		u, _ := url.Parse(page)
		q := u.Query()
		code := randURL(12)
		f.mu.Lock()
		f.authz = append(f.authz, q)
		f.codes[code] = fakeCode{client: f.issued, challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri"), nonce: q.Get("nonce")}
		f.mu.Unlock()
		back := q.Get("redirect_uri") + "?" + url.Values{"state": {q.Get("state")}, "code": {code}, "client_id": {f.issued}, "scope": {f.scope}}.Encode()
		f.mu.Lock()
		f.tokenDelay = 300 * time.Millisecond // (the first answer is still being taken when the second comes)
		f.mu.Unlock()
		go func() {
			if resp, err := http.Get(back); err == nil {
				resp.Body.Close()
			}
			f.pages <- ""
		}()
		go func() {
			time.Sleep(80 * time.Millisecond)
			t0 := time.Now()
			resp, err := http.Get(back)
			if err != nil {
				second <- -1
				return
			}
			resp.Body.Close()
			if time.Since(t0) > 200*time.Millisecond {
				second <- -2
				return
			}
			second <- resp.StatusCode
		}()
		return nil
	}
	if _, err := f.signIn(t, gptAgain); err != nil {
		t.Fatal(err)
	}
	if code := <-second; code != http.StatusConflict {
		t.Errorf("the second answer: %d", code)
	}
}

// A redirect from OpenAI's API is not followed: the token goes nowhere else.
func TestGptNoRedirects(t *testing.T) {
	st, f := gptReady(t)
	c, _ := newAIClient(st)
	f.mu.Lock()
	f.redirectAPI = true
	f.mu.Unlock()
	_, err := c.chat(context.Background(), "", []aiTurn{{Role: "user", Text: "hi"}}, nil)
	f.mu.Lock()
	n := f.elsewhere
	f.mu.Unlock()
	if err == nil || !strings.Contains(err.Error(), "重定向") || n != 0 {
		t.Errorf("redirect: %v (followed %d)", err, n)
	}
}

// OpenAI's earliest_refresh_at: no refresh before it while the token is good; asked for one, it is refreshed.
func TestGptEarliestRefresh(t *testing.T) {
	core.DataDir = t.TempDir()
	f := newFakeOpenAI(t)
	f.earliest = time.Now().Add(time.Hour).Unix()
	if _, err := f.signIn(t, gptAgain); err != nil {
		t.Fatal(err)
	}
	if tk := gptTokensNow(t); tk.RefreshAfter != f.earliest {
		t.Fatalf("refreshAfter %d", tk.RefreshAfter)
	}
	aiMu.Lock()
	tk, _ := loadGptTokensLocked()
	tk.Expires = time.Now().Add(time.Minute).Unix()
	_ = saveGptTokensLocked(tk)
	aiMu.Unlock()
	hc := gptHTTP(nil, gptAuthBase)
	n := len(f.tokens)
	if _, err := gptAccess(context.Background(), hc, false, gptPin{}); err != nil || len(f.tokens) != n {
		t.Errorf("refreshed before earliest_refresh_at: %v", err)
	}
	if _, err := gptAccess(context.Background(), hc, true, gptPin{}); err != nil || len(f.tokens) != n+1 {
		t.Errorf("forced: %v", err)
	}
	if gptWhen(json.RawMessage(`"2026-10-13T12:00:00Z"`)) != time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC).Unix() || gptWhen(json.RawMessage(`1760356800000`)) != 1760356800 || gptWhen(nil) != 0 {
		t.Error("gptWhen")
	}
}

// Signing out stops what uses the plan first: a run waiting on OpenAI ends at once.
func TestGptStopBeforeSignOut(t *testing.T) {
	st := testkit.NewStore(t)
	proj := t.TempDir()
	u := &sceneUnity{}
	unitytest.FakeUnity(t, proj, u.answer)
	st.Projects = []core.ProjectInfo{{Name: "P", Path: proj}}
	f := newFakeOpenAI(t)
	if _, err := f.signIn(t, gptAgain); err != nil {
		t.Fatal(err)
	}
	if err := aiSave("chatgpt", "", "gpt-a", nil, nil); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.respDelay = 5 * time.Second
	f.mu.Unlock()
	if err := startAIRun(st, aiRunReq{Project: proj, Mode: "chat", Text: "看看模型"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		f.mu.Lock()
		a := f.arrived
		f.mu.Unlock()
		if a > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	t0 := time.Now()
	stopPlanWork()
	if revoked, err := gptSignOut(context.Background(), gptHTTP(nil, gptAuthBase)); err != nil || !revoked {
		t.Fatalf("sign-out %v %v", revoked, err)
	}
	waitRun(t, aiSessionFor(proj))
	if d := time.Since(t0); d > 2*time.Second {
		t.Errorf("the run went on for %v after the sign-out began", d)
	}
}

// The registration's file that cannot be read is not written over: no new host ID, no lost accounts.
func TestGptHostUnread(t *testing.T) {
	core.DataDir = t.TempDir()
	f := newFakeOpenAI(t)
	if _, err := f.signIn(t, gptAgain); err != nil {
		t.Fatal(err)
	}
	_, _ = gptSignOut(context.Background(), gptHTTP(nil, gptAuthBase))
	before, _ := os.ReadFile(gptHostFile())
	w := aiReadWait
	aiReadWait = time.Millisecond
	t.Cleanup(func() { aiReadWait = w })
	give := testkit.Unreadable(t, gptHostFile())
	_, err := f.signIn(t, gptAgain)
	_ = gptWelcomed()
	give()
	if err != errGptHostUnread {
		t.Errorf("sign-in with the file unreadable: %v", err)
	}
	if after, _ := os.ReadFile(gptHostFile()); !bytes.Equal(before, after) {
		t.Error("the registration's file was written over")
	}
}
