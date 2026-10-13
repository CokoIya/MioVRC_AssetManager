package ai

import (
	"bufio"
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"vrclib/internal/core"
)

// Sign in with ChatGPT: the player's ChatGPT plan instead of an API key — OpenAI's own way for open-source programs
// that run on the player's computer (developers.openai.com/siwc). The player signs in in their own browser, on
// OpenAI's page (this program never sees the password), and lets the program use the plan. What the program gets
// is an OAuth token for OpenAI's public API (api.openai.com/v1, the Responses API), kept encrypted for this
// Windows user (DPAPI). Nothing of chatgpt.com itself is used: no web session, no cookies, no backend-api, no
// browser driven by the program. What the AI does here counts against the plan; the player can see and cap it in
// ChatGPT's settings (Usage), sign out here (the program's access is revoked at OpenAI) or disconnect the program
// there.

var (
	gptAuthBase   = "https://auth.openai.com"   // where the player signs in, and the tokens come from
	gptAPIBase    = "https://api.openai.com/v1" // the public API the tokens are for (the OAuth "resource")
	gptOpen       = core.OpenURL                // opens the sign-in page in the player's browser (tests answer it themselves)
	gptPorts      = []int{1455}                 // the loopback port tried first (OpenAI's example); any free one after it
	gptWait       = 10 * time.Minute            // how long the player may take on the sign-in page
	gptRevokeWait = []time.Duration{time.Second, 3 * time.Second}
	gptCallTime   = 60 * time.Second // one request to OpenAI's sign-in (an exchange, a refresh): not cut short by what asked for it
)

// OpenAI's sign-in, as its discovery document gives it (auth.openai.com/.well-known/openid-configuration).
const (
	gptAuthorizePath = "/api/accounts/authorize"
	gptTokenPath     = "/api/accounts/oauth/token"
	gptRevokePath    = "/api/accounts/oauth/revoke"
	gptKeysPath      = "/.well-known/jwks.json"
)

const (
	gptAppName    = "MioVRCA"
	gptDynamic    = "dynamic_agent_client" // a first sign-in: OpenAI issues the program a client ID of its own for the account
	gptScopes     = "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct"
	gptPlanScope  = "chatgpt.tokens.use.direct" // the use of the plan; without it the sign-in is no use to the AI
	gptUsageURL   = "https://chatgpt.com/settings/usage"
	gptMaxAccts   = 10               // registrations kept (one per ChatGPT account signed in on this computer)
	gptPendingFor = 15 * time.Minute // how long a registration whose sign-in did not complete is tried again
)

// what a sign-in is for
const (
	gptAgain   = ""        // a saved account (the one signed in last unless one is named); a new registration when there is none
	gptOther   = "other"   // another account: a registration of its own
	gptConsent = "consent" // the account signed in now, asked again for the use of the plan
)

// gptError: an error with what the window offers for it — code "usage": the plan's limit (「管理用量」 first);
// "signin": the sign-in is gone; "plan": the sign-in does not cover the use of the plan; "denied": the plan is not
// to be used here (nothing to offer). Any of them ends the requests of the client it came from (aiClient.halt).
type gptError struct {
	code string
	msg  string
}

func (e *gptError) Error() string { return e.msg }

var (
	errGptSignedOut  = &gptError{"signin", "尚未登录 ChatGPT 账号：请在「AI 服务」中选择「ChatGPT 账号」，点击「使用 ChatGPT 继续」"}
	errGptExpired    = &gptError{"signin", "ChatGPT 登录已失效，请在「AI 服务」中重新登录"}
	errGptClientGone = &gptError{"signin", "本软件在 OpenAI 的登记已失效，请在「AI 服务」中重新登录（将重新登记本软件）"}
	errGptSwitched   = &gptError{"signin", "ChatGPT 登录的账号已更改，本次操作已停止，请重新开始"}
	errGptNoPlan     = &gptError{"plan", "该 ChatGPT 登录未授权本软件使用套餐额度：请在「AI 服务」中点击「启用套餐额度」，并在 OpenAI 的页面中同意"}
	errGptUnread     = errors.New("ChatGPT 登录文件暂时无法读取，请稍后重试")
	errGptHostUnread = errors.New("ChatGPT 的本机登记文件暂时无法读取，请稍后重试")
	errGptCancelled  = errors.New("已取消登录")
)

// aiErrCode: what the window offers for an error the AI ran into ("" = nothing in particular).
func aiErrCode(err error) string {
	var ge *gptError
	if errors.As(err, &ge) {
		return ge.code
	}
	var he *aiHTTPError
	if errors.As(err, &he) {
		return he.code
	}
	return ""
}

func gptHostFile() string { return filepath.Join(core.DataDir, "chatgpt-host.json") }
func gptAuthFile() string { return filepath.Join(core.DataDir, "chatgpt-auth.dat") }

// gptHTTP: a client for OpenAI's sign-in or API. A redirect is never followed: a token goes to the address it is
// for and nowhere else, not even another page of the same host.
func gptHTTP(st *core.Store, base string) *http.Client {
	hc := aiHTTP(st, base)
	hc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return errors.New("OpenAI 的接口返回了重定向，请求已中止")
	}
	return hc
}

// gptHost: this computer's registrations with OpenAI. Not secret, and never part of an export: it belongs to this
// computer (library/transfer.go).
type gptHost struct {
	HostID    string       `json:"hostId"`              // chosen once, sent with every sign-in: opaque, nothing of the player
	Accounts  []gptAccount `json:"accounts,omitempty"`  // a client ID of its own for each account signed in here, kept through sign-outs
	Pending   string       `json:"pending,omitempty"`   // a client ID issued to a new registration whose sign-in did not complete …
	PendingAt int64        `json:"pendingAt,omitempty"` // … and when: the next attempt goes on with it, for a while (gptPendingFor)
	Last      string       `json:"last,omitempty"`      // the account signed in last (its sub): offered first at the next sign-in
	Welcomed  bool         `json:"welcomed,omitempty"`  // the note that the plan is in use was shown (once only)
}

type gptAccount struct {
	Sub      string `json:"sub"`
	ClientID string `json:"clientId"`
	Email    string `json:"email,omitempty"`
}

func (h *gptHost) account(sub string) *gptAccount {
	for i := range h.Accounts {
		if sub != "" && h.Accounts[i].Sub == sub {
			return &h.Accounts[i]
		}
	}
	return nil
}

// keep: the registration of an account, in place of the one it had (the newest last; the oldest go past gptMaxAccts).
func (h *gptHost) keep(a gptAccount) {
	out := []gptAccount{}
	for _, x := range h.Accounts {
		if x.Sub != a.Sub && x.ClientID != a.ClientID {
			out = append(out, x)
		}
	}
	out = append(out, a)
	if len(out) > gptMaxAccts {
		out = out[len(out)-gptMaxAccts:]
	}
	h.Accounts = out
}

// gptTokens: what the sign-in of the account in use brought, kept encrypted (chatgpt-auth.dat). The client ID is the
// one these tokens were issued to: they are never sent with another.
type gptTokens struct {
	ClientID     string `json:"clientId"`
	Sub          string `json:"sub"`
	Email        string `json:"email,omitempty"`
	Access       string `json:"access"`
	Refresh      string `json:"refresh"`
	IDToken      string `json:"idToken,omitempty"`
	Expires      int64  `json:"expires"`                // unix seconds
	RefreshAfter int64  `json:"refreshAfter,omitempty"` // OpenAI's earliest_refresh_at: not refreshed before, while the token is good
	Scope        string `json:"scope"`
	Saved        int64  `json:"saved"`
}

func (t *gptTokens) signedIn() bool {
	return t != nil && t.Access != "" && t.ClientID != "" && t.Sub != ""
}
func (t *gptTokens) planOK() bool { return t.signedIn() && hasScope(t.Scope, gptPlanScope) }

func hasScope(scopes, s string) bool {
	for _, x := range strings.Fields(strings.ReplaceAll(scopes, "+", " ")) {
		if x == s {
			return true
		}
	}
	return false
}

// Locks: gptSem is held through a refresh, a sign-out or the end of a sign-in (refresh tokens turn over: two
// refreshes at once would spend the same one), and takes aiMu for the files; aiMu never takes gptSem. Waiting for
// it gives way to the waiter's own end (gptLock). gptPendMu only guards the sign-in under way and takes nothing.
var (
	gptSem     = make(chan struct{}, 1)
	gptPendMu  sync.Mutex
	gptPend    context.CancelFunc // a sign-in under way
	gptPendURL string             // … and the page it opened (opened again on request)
	gptErr     string             // why the last sign-in did not complete
)

func gptLock(ctx context.Context) error {
	select {
	case gptSem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return errors.New("已停止")
	}
}

func gptUnlock() { <-gptSem }

// ---------- the two files (caller holds aiMu) ----------

// loadGptHostLocked: this computer's registrations; errGptHostUnread when the file is there and could not be read
// (it is not written over then). One that does not parse is kept aside before it is written over.
func loadGptHostLocked() (gptHost, error) {
	var h gptHost
	p := gptHostFile()
	b, ok := readAIFile(p)
	if !ok {
		return h, errGptHostUnread
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &h); err != nil {
			aiBroken[p] = aiBrokenFile{b: b, err: err}
			return gptHost{}, nil
		}
	}
	return h, nil
}

func saveGptHostLocked(h gptHost) error {
	b, _ := json.MarshalIndent(h, "", "  ")
	return writeAIFile(gptHostFile(), b, errGptHostUnread)
}

// gptUnsaved: tokens a refresh brought that could not be written yet (the file still holds the refresh token that
// was spent): this run of the program goes on with them, and writes them as soon as it can. Guarded by aiMu.
var gptUnsaved *gptTokens

// loadGptTokensLocked: the tokens, nil when there are none (or they are another Windows user's, or do not parse:
// kept aside when written over, as the key file is).
func loadGptTokensLocked() (*gptTokens, error) {
	if gptUnsaved != nil {
		t := *gptUnsaved
		return &t, nil
	}
	p := gptAuthFile()
	b, ok := readAIFile(p)
	if !ok {
		return nil, errGptUnread
	}
	if len(b) == 0 {
		return nil, nil
	}
	dec, err := unprotect(b)
	if err != nil {
		aiBroken[p] = aiBrokenFile{b: b, err: err, decrypt: true}
		return nil, nil
	}
	var t gptTokens
	if err := json.Unmarshal(dec, &t); err != nil {
		aiBroken[p] = aiBrokenFile{b: b, err: err}
		return nil, nil
	}
	return &t, nil
}

var protect = core.ProtectData

func saveGptTokensLocked(t *gptTokens) error {
	t.Saved = time.Now().Unix()
	b, _ := json.Marshal(t)
	enc, err := protect(b)
	if err != nil {
		return err
	}
	return writeAIFile(gptAuthFile(), enc, errGptUnread)
}

// keepGptTokensLocked writes what a refresh brought; when the file cannot be written, the tokens are held here
// (gptUnsaved) and written at the next request — never lost, since OpenAI has already spent the old refresh token.
func keepGptTokensLocked(t *gptTokens) {
	err := saveGptTokensLocked(t)
	for i := 0; err != nil && i < 2; i++ {
		time.Sleep(aiReadWait)
		_, _ = readAIFile(gptAuthFile()) // (whether it can be read again is looked at anew)
		err = saveGptTokensLocked(t)
	}
	if err != nil {
		cp := *t
		gptUnsaved = &cp
		core.Logf("ChatGPT 登录续期后暂时未能保存（%v），将在下次请求时再保存", err)
		return
	}
	gptUnsaved = nil
}

// gptForgetLocked: no tokens any more (signed out, or OpenAI ended the sign-in).
func gptForgetLocked() error {
	gptUnsaved = nil
	delete(aiBroken, gptAuthFile())
	if err := os.Remove(gptAuthFile()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ---------- signing in ----------

// randURL: n random bytes as base64url, for state, nonce and the PKCE verifier.
func randURL(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// newUUID: a random UUID (version 4).
func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// gptListen: the loopback address OpenAI's page sends the player back to (127.0.0.1, never "localhost"; only the
// port may differ from one sign-in to the next).
func gptListen() (net.Listener, int, error) {
	for _, p := range gptPorts {
		if ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p)); err == nil {
			return ln, p, nil
		}
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, 0, err
	}
	return ln, ln.Addr().(*net.TCPAddr).Port, nil
}

// gptDonePage: what the browser shows once OpenAI's page sent the player back, in the three languages (this page
// is not the program's window): signed in, or why not.
func gptDonePage(err error) string {
	say := "登录完成，请返回 MioVRCA。此页面可以关闭。<br>Signed in. Go back to MioVRCA; you can close this page.<br>サインインしました。MioVRCA に戻ってください。このページは閉じてかまいません。"
	if err != nil {
		say = "未完成登录：" + html.EscapeString(err.Error()) + "<br>请返回 MioVRCA。此页面可以关闭。<br><br>Not signed in. Go back to MioVRCA; you can close this page.<br>サインインは完了していません。MioVRCA に戻ってください。このページは閉じてかまいません。"
	}
	return `<!doctype html><html><head><meta charset="utf-8"><title>MioVRCA</title></head><body style="font-family:sans-serif;margin:3em;line-height:1.8">` + say + `</body></html>`
}

type gptCallback struct {
	v    url.Values
	done chan error // the end of the sign-in, for the page the browser is waiting on
}

// gptSignIn takes a sign-in to its end: OpenAI's page opened in the player's browser, its answer taken on a
// loopback address (authorization code with PKCE), the code exchanged for tokens, the ID token checked, and the
// tokens kept. account: the saved account to sign in again ("": the one signed in last).
func gptSignIn(ctx context.Context, hc *http.Client, mode, account string) error {
	aiMu.Lock()
	host, err := loadGptHostLocked()
	var cur *gptTokens
	if err == nil {
		cur, err = loadGptTokensLocked()
	}
	if err != nil {
		aiMu.Unlock()
		return err
	}
	if host.HostID == "" {
		host.HostID = "urn:uuid:" + newUUID()
	}
	// a client ID left by a new registration that did not complete: the next attempt of the same kind goes on
	// with it for a while (OpenAI: retry with the same issued client ID), and it is used once only
	pending := ""
	if host.Pending != "" && mode == gptAgain && time.Since(time.Unix(host.PendingAt, 0)) < gptPendingFor {
		pending = host.Pending
	}
	host.Pending, host.PendingAt = "", 0
	if err := saveGptHostLocked(host); err != nil {
		aiMu.Unlock()
		return fmt.Errorf("无法保存本机的登录信息：%v", err)
	}
	aiMu.Unlock()
	// the account this sign-in is for: nil for a registration of its own
	var want *gptAccount
	switch {
	case mode == gptConsent:
		if !cur.signedIn() {
			return errGptSignedOut
		}
		want = &gptAccount{Sub: cur.Sub, ClientID: cur.ClientID, Email: cur.Email}
	case cur.signedIn():
		return errors.New("已登录 ChatGPT 账号：如需更换账号，请先退出登录")
	case mode == gptAgain:
		pick := account
		if pick == "" {
			pick = host.Last
		}
		if a := host.account(pick); a != nil {
			cp := *a
			want = &cp
		} else if account != "" {
			return errors.New("该账号的登录记录已不存在，请点击「使用其他账号」")
		}
	}
	clientID := gptDynamic
	if want != nil {
		clientID = want.ClientID
	} else if pending != "" {
		clientID = pending
	}
	ln, port, err := gptListen()
	if err != nil {
		return fmt.Errorf("无法在本机接收登录结果：%v", err)
	}
	redirect := fmt.Sprintf("http://127.0.0.1:%d/auth/callback", port)
	state, nonce, verifier := randURL(32), randURL(32), randURL(48)
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{}
	q.Set("client_id", clientID)
	if clientID == gptDynamic {
		q.Set("agent_name_hint", gptAppName) // a first registration only
	}
	q.Set("ext_agent_host_id", host.HostID)
	if want != nil && want.Email != "" {
		q.Set("login_hint", want.Email)
	}
	if mode == gptConsent {
		q.Set("prompt", "consent")
	}
	q.Set("response_type", "code")
	q.Set("redirect_uri", redirect)
	q.Set("scope", gptScopes)
	q.Set("resource", gptAPIBase)
	q.Set("state", state)
	q.Set("nonce", nonce)
	q.Set("code_challenge_method", "S256")
	q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(sum[:]))
	got := make(chan gptCallback, 1)
	var taken atomic.Bool // the first answer of this attempt is the only one taken
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v := r.URL.Query()
		if r.Method != "GET" || r.URL.Path != "/auth/callback" || v.Get("state") != state { // not this attempt's answer: not taken
			http.NotFound(w, r)
			return
		}
		if !taken.CompareAndSwap(false, true) {
			http.Error(w, "already answered", http.StatusConflict)
			return
		}
		cb := gptCallback{v: v, done: make(chan error, 1)}
		got <- cb
		var end error
		select {
		case end = <-cb.done:
		case <-time.After(2 * time.Minute):
			end = errors.New("超时")
		case <-r.Context().Done():
			return
		}
		h := w.Header()
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Cache-Control", "no-store")
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		_, _ = io.WriteString(w, gptDonePage(end))
	})}
	go func() { _ = srv.Serve(ln) }()
	defer func() { // the page the browser waits on is written first
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	page := gptAuthBase + gptAuthorizePath + "?" + strings.ReplaceAll(q.Encode(), "+", "%20") // (a space as %20: what every server reads as one)
	gptPendMu.Lock()
	gptPendURL = page
	gptPendMu.Unlock()
	if err := gptOpen(page); err != nil {
		return fmt.Errorf("无法打开浏览器：%v", err)
	}
	var cb gptCallback
	select {
	case cb = <-got:
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return errors.New("登录超时：未在浏览器中完成 ChatGPT 登录")
		}
		return errGptCancelled
	}
	// the player has answered: what is left is the program's, and is not cut short by the time the player had
	// or by 「取消登录」 now
	fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gptCallTime)
	defer cancel()
	err = gptFinish(fctx, hc, cb.v, mode, want, clientID, redirect, verifier, nonce)
	cb.done <- err
	return err
}

// gptFinish: OpenAI's answer to a sign-in, taken to its end.
func gptFinish(ctx context.Context, hc *http.Client, v url.Values, mode string, want *gptAccount, clientID, redirect, verifier, nonce string) error {
	switch e := v.Get("error"); {
	case e == "access_denied":
		return errors.New("已在 OpenAI 的页面中取消授权")
	case e != "":
		return fmt.Errorf("ChatGPT 登录未完成（%s）", core.Truncate(e, 80))
	case v.Get("code") == "":
		return errors.New("ChatGPT 登录未返回授权码，请重试")
	}
	issued := v.Get("client_id")
	if clientID == gptDynamic {
		if issued == "" || issued == gptDynamic {
			return errors.New("ChatGPT 登录未完成本软件的登记（未返回应用编号），请重试")
		}
	} else {
		if issued != "" && issued != clientID {
			return errors.New("ChatGPT 登录返回的应用编号与本机登记的不一致，已中止")
		}
		issued = clientID
	}
	if err := gptLock(ctx); err != nil {
		return err
	}
	defer gptUnlock()
	if want == nil { // a new registration: kept before the exchange, so that the next attempt goes on with it
		aiMu.Lock()
		if host, err := loadGptHostLocked(); err == nil {
			host.Pending, host.PendingAt = issued, time.Now().Unix()
			_ = saveGptHostLocked(host)
		}
		aiMu.Unlock()
	}
	r, err := gptTokenRequest(ctx, hc, url.Values{"grant_type": {"authorization_code"}, "client_id": {issued}, "code": {v.Get("code")},
		"code_verifier": {verifier}, "redirect_uri": {redirect}, "resource": {gptAPIBase}})
	var oe *gptOAuthErr
	if errors.As(err, &oe) && oe.code == "invalid_grant" {
		return errors.New("ChatGPT 登录已过期，请重新登录")
	}
	if err != nil {
		return err
	}
	kept := false
	defer func() { // tokens this sign-in got and does not keep are given back
		if !kept && r.Refresh != "" {
			rctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := gptRevoke(rctx, hc, &gptTokens{ClientID: issued, Refresh: r.Refresh}); err != nil {
				core.Logf("ChatGPT：未采用的登录未能在 OpenAI 撤销（%v）", core.TrimErr(err))
			}
		}
	}()
	cl, err := gptIDClaims(ctx, hc, r.IDToken, issued, nonce)
	if err != nil {
		return err
	}
	if want != nil && cl.Sub != want.Sub {
		if mode == gptConsent {
			return errors.New("授权的 ChatGPT 账号与当前登录的账号不同，已中止。请选择当前登录的账号")
		}
		return errors.New("登录的 ChatGPT 账号与所选的账号不同，已中止。如需使用其他账号，请点击「使用其他账号」")
	}
	scope := r.Scope
	if scope == "" {
		scope = v.Get("scope")
	}
	email := cl.Email
	if email == "" && want != nil {
		email = want.Email
	}
	t := &gptTokens{ClientID: issued, Sub: cl.Sub, Email: email, Access: r.Access, Refresh: r.Refresh, IDToken: r.IDToken, Expires: gptExpiry(r.ExpiresIn),
		RefreshAfter: gptWhen(r.Earliest), Scope: scope}
	aiMu.Lock()
	defer aiMu.Unlock()
	gptUnsaved = nil
	if err := saveGptTokensLocked(t); err != nil {
		return fmt.Errorf("无法保存 ChatGPT 登录：%v", err)
	}
	kept = true
	if host, err := loadGptHostLocked(); err == nil {
		host.keep(gptAccount{Sub: cl.Sub, ClientID: issued, Email: email})
		host.Last = cl.Sub
		if host.Pending == issued {
			host.Pending, host.PendingAt = "", 0
		}
		if err := saveGptHostLocked(host); err != nil {
			core.Logf("ChatGPT：本机登记未能保存（%v）", err)
		}
	} else {
		core.Logf("ChatGPT：本机登记未能保存（%v）", err)
	}
	if t.planOK() {
		core.Logf("ChatGPT 账号已登录")
	} else {
		core.Logf("ChatGPT 账号已登录，但未授权使用套餐额度")
	}
	return nil
}

func gptExpiry(in int64) int64 {
	if in <= 0 {
		in = 3600
	}
	return time.Now().Add(time.Duration(in) * time.Second).Unix()
}

// gptWhen: a time OpenAI gives (unix seconds or milliseconds, or RFC 3339) as unix seconds; 0 when it gives none.
func gptWhen(raw json.RawMessage) int64 {
	var n float64
	if json.Unmarshal(raw, &n) == nil && n > 1e9 {
		if n > 1e12 {
			n /= 1000
		}
		return int64(n)
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.Unix()
		}
	}
	return 0
}

type gptTokenResp struct {
	Access    string          `json:"access_token"`
	Refresh   string          `json:"refresh_token"`
	IDToken   string          `json:"id_token"`
	ExpiresIn int64           `json:"expires_in"`
	Scope     string          `json:"scope"`
	Earliest  json.RawMessage `json:"earliest_refresh_at"`
}

// gptOAuthErr: OpenAI's sign-in refused a request (code: its OAuth error, "invalid_grant" …).
type gptOAuthErr struct {
	status int
	code   string
	desc   string
}

func (e *gptOAuthErr) Error() string {
	said := e.desc
	if said == "" {
		said = e.code
	}
	if said == "" {
		return fmt.Sprintf("OpenAI 未接受登录请求（HTTP %d）", e.status)
	}
	return fmt.Sprintf("OpenAI 未接受登录请求（HTTP %d）：%s", e.status, core.Truncate(said, 160))
}

// the refusals of a refresh after which the tokens are of no more use: the player signs in again
var gptDead = map[string]bool{"invalid_grant": true, "invalid_refresh_token": true, "token_expired": true, "refresh_token_expired": true,
	"refresh_token_invalidated": true, "refresh_token_reused": true}

// gptTokenRequest: one request to OpenAI's token endpoint (a code exchanged, or a refresh).
func gptTokenRequest(ctx context.Context, hc *http.Client, form url.Values) (*gptTokenResp, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", gptAuthBase+gptTokenPath, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, errGptCancelled
		}
		return nil, fmt.Errorf("无法连接 OpenAI，请检查网络（如需代理，可在设置中填写）。%v", core.TrimErr(err))
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var r struct {
		gptTokenResp
		Error json.RawMessage `json:"error"`
		Desc  string          `json:"error_description"`
	}
	_ = json.Unmarshal(b, &r)
	if resp.StatusCode/100 != 2 || r.Access == "" {
		oe := &gptOAuthErr{status: resp.StatusCode, desc: r.Desc}
		var o struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if json.Unmarshal(r.Error, &oe.code) != nil && json.Unmarshal(r.Error, &o) == nil { // {"error": "invalid_grant"} or {"error": {"code": …}}
			oe.code = o.Code
			if oe.desc == "" {
				oe.desc = o.Message
			}
		}
		return nil, oe
	}
	return &r.gptTokenResp, nil
}

// ---------- the ID token ----------

type gptClaims struct {
	Iss   string          `json:"iss"`
	Aud   json.RawMessage `json:"aud"` // one name, or a list of them
	Exp   int64           `json:"exp"`
	Nonce string          `json:"nonce"`
	Sub   string          `json:"sub"`
	Email string          `json:"email"`
}

// OpenAI's signing keys, by key ID, as last fetched
var (
	gptKeysMu sync.Mutex
	gptKeySet map[string]*rsa.PublicKey
)

func b64url(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
}

// gptFetchKeys: OpenAI's published signing keys (RS256).
func gptFetchKeys(ctx context.Context, hc *http.Client) (map[string]*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", gptAuthBase+gptKeysPath, nil)
	if err != nil {
		return nil, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法读取 OpenAI 的签名密钥：%v", core.TrimErr(err))
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var set struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			Use string `json:"use"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if resp.StatusCode != 200 || json.Unmarshal(b, &set) != nil {
		return nil, fmt.Errorf("无法读取 OpenAI 的签名密钥（HTTP %d）", resp.StatusCode)
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "RSA" || (k.Use != "" && k.Use != "sig") {
			continue
		}
		n, err1 := b64url(k.N)
		e, err2 := b64url(k.E)
		if err1 != nil || err2 != nil || len(n) < 256 || len(e) == 0 || len(e) > 4 {
			continue
		}
		ev := 0
		for _, x := range e {
			ev = ev<<8 | int(x)
		}
		keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: ev}
	}
	return keys, nil
}

// gptVerify: the payload of a JWT signed with one of OpenAI's published keys (RS256: what its discovery document
// names). The keys are fetched again when one of them is not known.
func gptVerify(ctx context.Context, hc *http.Client, tok string) ([]byte, error) {
	bad := errors.New("ChatGPT 登录返回的身份信息无效")
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return nil, bad
	}
	hb, err := b64url(parts[0])
	if err != nil {
		return nil, bad
	}
	var h struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if json.Unmarshal(hb, &h) != nil || h.Alg != "RS256" {
		return nil, bad
	}
	sig, err := b64url(parts[2])
	if err != nil {
		return nil, bad
	}
	gptKeysMu.Lock()
	key := gptKeySet[h.Kid]
	gptKeysMu.Unlock()
	if key == nil {
		keys, err := gptFetchKeys(ctx, hc)
		if err != nil {
			return nil, err
		}
		gptKeysMu.Lock()
		gptKeySet = keys
		gptKeysMu.Unlock()
		if key = keys[h.Kid]; key == nil {
			return nil, errors.New("ChatGPT 登录返回的身份信息无法验证（签名密钥不明）")
		}
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], sig) != nil {
		return nil, errors.New("ChatGPT 登录返回的身份信息签名无效，已中止")
	}
	payload, err := b64url(parts[1])
	if err != nil {
		return nil, bad
	}
	return payload, nil
}

// gptIDClaims: the ID token of a sign-in, checked: signed by OpenAI's sign-in, issued by it, for this program's
// client ID, for this attempt (nonce), not run out.
func gptIDClaims(ctx context.Context, hc *http.Client, idt, aud, nonce string) (gptClaims, error) {
	var c gptClaims
	payload, err := gptVerify(ctx, hc, idt)
	if err != nil {
		return c, err
	}
	if json.Unmarshal(payload, &c) != nil {
		return c, errors.New("ChatGPT 登录返回的身份信息无效")
	}
	var auds []string
	if json.Unmarshal(c.Aud, &auds) != nil {
		var one string
		_ = json.Unmarshal(c.Aud, &one)
		auds = []string{one}
	}
	ok := false
	for _, a := range auds {
		ok = ok || a == aud
	}
	switch {
	case strings.TrimRight(c.Iss, "/") != strings.TrimRight(gptAuthBase, "/"), !ok, c.Nonce != nonce, c.Sub == "":
		return c, errors.New("ChatGPT 登录返回的身份信息与本次登录不符，已中止")
	case c.Exp == 0 || time.Unix(c.Exp, 0).Before(time.Now().Add(-time.Minute)):
		return c, errors.New("ChatGPT 登录返回的身份信息已过期，请重试")
	}
	return c, nil
}

// ---------- in the background, for the window ----------

// gptStartSignIn begins a sign-in in the background; the window follows it in the AI view (pending, err).
func gptStartSignIn(st *core.Store, mode, account string) error {
	switch mode {
	case gptAgain, gptOther, gptConsent:
	default:
		return errors.New("未知的登录方式：" + mode)
	}
	gptPendMu.Lock()
	if gptPend != nil {
		gptPendMu.Unlock()
		return errors.New("正在等待浏览器中的登录，请完成或取消后再试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), gptWait)
	gptPend, gptPendURL, gptErr = cancel, "", ""
	gptPendMu.Unlock()
	hc := gptHTTP(st, gptAuthBase)
	go func() {
		defer cancel()
		err := gptSignIn(ctx, hc, mode, account)
		gptPendMu.Lock()
		gptPend, gptPendURL = nil, ""
		if err != nil && !errors.Is(err, errGptCancelled) {
			gptErr = err.Error()
			core.Logf("ChatGPT 登录未完成：%v", err)
		}
		gptPendMu.Unlock()
		core.BumpRev()
	}()
	return nil
}

func gptCancelSignIn() {
	gptPendMu.Lock()
	if gptPend != nil {
		gptPend()
	}
	gptPendMu.Unlock()
}

// gptReopen: the sign-in page of the sign-in under way, opened again (the browser was closed meanwhile).
func gptReopen() error {
	gptPendMu.Lock()
	page := gptPendURL
	gptPendMu.Unlock()
	if page == "" {
		return errors.New("没有进行中的登录")
	}
	return gptOpen(page)
}

// gptRevoke: the refresh token revoked at OpenAI. Sent again after a pause when there was no answer or OpenAI's
// own trouble (5xx); OpenAI answers 200 when the token is gone, also when it was gone already.
func gptRevoke(ctx context.Context, hc *http.Client, t *gptTokens) error {
	form := url.Values{"token": {t.Refresh}, "token_type_hint": {"refresh_token"}, "client_id": {t.ClientID}}
	for try := 0; ; try++ {
		req, err := http.NewRequestWithContext(ctx, "POST", gptAuthBase+gptRevokePath, strings.NewReader(form.Encode()))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := hc.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
			err = fmt.Errorf("HTTP %d", resp.StatusCode)
			if resp.StatusCode < 500 {
				return err
			}
		}
		if try >= len(gptRevokeWait) || ctx.Err() != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(gptRevokeWait[try]):
		}
	}
}

// gptSignOut: the program's access revoked at OpenAI, then the tokens forgotten here. revoked false: OpenAI did not
// confirm it (no connection …) — the tokens are forgotten all the same, and the player can disconnect the program
// in ChatGPT's settings. The registration of this computer stays, for a later sign-in. (What uses the plan is
// stopped before: aiagent.go, stopPlanWork.)
func gptSignOut(ctx context.Context, hc *http.Client) (revoked bool, err error) {
	if err := gptLock(ctx); err != nil {
		return false, err
	}
	defer gptUnlock()
	aiMu.Lock()
	t, err := loadGptTokensLocked()
	aiMu.Unlock()
	if err != nil {
		return false, err
	}
	revoked = true
	if t.signedIn() && t.Refresh != "" {
		if e := gptRevoke(ctx, hc, t); e != nil {
			revoked = false
			core.Logf("ChatGPT 退出登录：OpenAI 未确认撤销授权（%v）", core.TrimErr(e))
		}
	}
	aiMu.Lock()
	defer aiMu.Unlock()
	if err := gptForgetLocked(); err != nil {
		return revoked, err
	}
	core.Logf("ChatGPT 账号已退出登录")
	return revoked, nil
}

// gptWelcomed: the note that the plan is in use was shown, and is not shown again.
func gptWelcomed() error {
	aiMu.Lock()
	defer aiMu.Unlock()
	h, err := loadGptHostLocked()
	if err != nil {
		return err
	}
	h.Welcomed = true
	return saveGptHostLocked(h)
}

// gptView: the sign-in as the window shows it.
type gptView struct {
	SignedIn bool         `json:"signedIn"`
	Plan     bool         `json:"plan"` // the sign-in covers the use of the plan
	Email    string       `json:"email,omitempty"`
	Accounts []gptViewAcc `json:"accounts,omitempty"` // signed out: the accounts signed in here before, the last one first
	Pending  bool         `json:"pending,omitempty"`
	Err      string       `json:"err,omitempty"`
	Welcome  bool         `json:"welcome,omitempty"` // the note that the plan is in use is due (once, after the first sign-in)
	Unread   bool         `json:"unread,omitempty"`  // a file could not be read just now
	UsageURL string       `json:"usageUrl"`
}

type gptViewAcc struct {
	ID    string `json:"id"` // OpenAI's opaque name of the account (sub)
	Email string `json:"email"`
}

// gptViewLocked: caller holds aiMu.
func gptViewLocked() gptView {
	t, err := loadGptTokensLocked()
	h, herr := loadGptHostLocked()
	v := gptView{UsageURL: gptUsageURL, Unread: err != nil || herr != nil}
	if t.signedIn() {
		v.SignedIn, v.Plan, v.Email = true, t.planOK(), t.Email
		v.Welcome = v.Plan && !h.Welcomed
	} else {
		if a := h.account(h.Last); a != nil {
			v.Accounts = append(v.Accounts, gptViewAcc{ID: a.Sub, Email: a.Email})
		}
		for i := len(h.Accounts) - 1; i >= 0; i-- {
			if a := h.Accounts[i]; a.Sub != h.Last {
				v.Accounts = append(v.Accounts, gptViewAcc{ID: a.Sub, Email: a.Email})
			}
		}
	}
	gptPendMu.Lock()
	v.Pending, v.Err = gptPend != nil, gptErr
	gptPendMu.Unlock()
	return v
}

// gptPin: the account and client ID a client was made for. A request of that client never goes out with another
// account's token: signed in again as someone else meanwhile, the work in progress stops instead.
type gptPin struct{ sub, client string }

// gptAccess: the access token for a request — refreshed when it is about to run out, or when force (the API
// refused the one it had). OpenAI turns the refresh token over at each refresh: what comes back replaces all of
// it. A refresh OpenAI refuses for good ends the sign-in; one that does not get through leaves it as it is. The
// refresh itself is not cut short by the end of the request that asked for it (the new refresh token would be lost).
func gptAccess(ctx context.Context, hc *http.Client, force bool, pin gptPin) (string, error) {
	if err := gptLock(ctx); err != nil {
		return "", err
	}
	defer gptUnlock()
	aiMu.Lock()
	if gptUnsaved != nil { // what an earlier refresh could not write: written now, when it can be
		keepGptTokensLocked(gptUnsaved)
	}
	t, err := loadGptTokensLocked()
	aiMu.Unlock()
	switch {
	case err != nil:
		return "", err
	case !t.signedIn():
		return "", errGptSignedOut
	case pin.sub != "" && (t.Sub != pin.sub || t.ClientID != pin.client):
		return "", errGptSwitched
	case !t.planOK():
		return "", errGptNoPlan
	}
	left := time.Until(time.Unix(t.Expires, 0))
	if !force && (left > 2*time.Minute || left > 0 && time.Now().Unix() < t.RefreshAfter) {
		return t.Access, nil
	}
	if t.Refresh == "" {
		if !force && left > 0 {
			return t.Access, nil
		}
		gptDrop(t, false)
		return "", errGptExpired
	}
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gptCallTime)
	defer cancel()
	r, err := gptTokenRequest(rctx, hc, url.Values{"grant_type": {"refresh_token"}, "client_id": {t.ClientID}, "refresh_token": {t.Refresh}, "resource": {gptAPIBase}})
	var oe *gptOAuthErr
	switch {
	case errors.As(err, &oe) && oe.code == "invalid_client": // OpenAI no longer knows this client ID
		gptDrop(t, true)
		core.Logf("ChatGPT：本软件在 OpenAI 的登记已失效（续期被拒绝：invalid_client），需要重新登录")
		return "", errGptClientGone
	case errors.As(err, &oe) && gptDead[oe.code]:
		gptDrop(t, false)
		core.Logf("ChatGPT 登录已失效（续期被拒绝：%s），需要重新登录", oe.code)
		return "", errGptExpired
	case err != nil:
		if !force && left > 0 { // still good for a while: the refresh is tried again at the next request
			return t.Access, nil
		}
		return "", err
	}
	t.Access, t.Expires, t.RefreshAfter = r.Access, gptExpiry(r.ExpiresIn), gptWhen(r.Earliest)
	if r.Refresh != "" {
		t.Refresh = r.Refresh
	}
	if r.Scope != "" {
		t.Scope = r.Scope
	}
	if r.IDToken != "" {
		t.IDToken = r.IDToken
	}
	aiMu.Lock()
	keepGptTokensLocked(t)
	aiMu.Unlock()
	if !t.planOK() {
		return "", errGptNoPlan
	}
	return t.Access, nil
}

// gptDrop forgets the tokens of t; client too: OpenAI no longer knows its client ID, and the account's next
// sign-in registers anew. Caller holds gptSem.
func gptDrop(t *gptTokens, client bool) {
	aiMu.Lock()
	defer aiMu.Unlock()
	_ = gptForgetLocked()
	if !client {
		return
	}
	h, err := loadGptHostLocked()
	if err != nil {
		return
	}
	out := h.Accounts[:0]
	for _, a := range h.Accounts {
		if a.ClientID != t.ClientID {
			out = append(out, a)
		}
	}
	h.Accounts = out
	_ = saveGptHostLocked(h)
}

// ---------- the Responses API ----------

func responsesParts(text string, imgs []aiImage) []map[string]any {
	parts := []map[string]any{{"type": "input_text", "text": text}}
	for _, im := range imgs {
		parts = append(parts, map[string]any{"type": "input_image", "image_url": "data:" + im.Mime + ";base64," + base64.StdEncoding.EncodeToString(im.Data)})
	}
	return parts
}

// chatResponses: one turn over the Responses API, as the use of a ChatGPT plan asks it — the whole conversation
// sent each time (store false: nothing is kept at OpenAI), the answer streamed, and none of the fields it does
// not take (temperature, output limit, metadata …). The system prompt goes as instructions.
func (c *aiClient) chatResponses(ctx context.Context, system string, turns []aiTurn, tools []aiTool) (aiTurn, error) {
	input := []map[string]any{}
	for _, t := range turns {
		switch t.Role {
		case "user":
			input = append(input, map[string]any{"role": "user", "content": responsesParts(t.Text, t.Images)})
		case "assistant":
			if strings.TrimSpace(t.Text) != "" || len(t.Calls) == 0 {
				input = append(input, map[string]any{"role": "assistant", "content": nonEmpty(t.Text)})
			}
			for _, k := range t.Calls {
				args := string(k.Args)
				if args == "" {
					args = "{}"
				}
				input = append(input, map[string]any{"type": "function_call", "call_id": k.ID, "name": k.Name, "arguments": args})
			}
		case "tool":
			var imgs []aiImage
			for _, r := range t.Results {
				input = append(input, map[string]any{"type": "function_call_output", "call_id": r.ID, "output": r.Content})
				imgs = append(imgs, r.Images...)
			}
			if len(imgs) > 0 {
				input = append(input, map[string]any{"role": "user", "content": responsesParts("（上面工具结果里说的截图，按顺序附在这里。这条消息是软件自动加的，不是玩家说的话。）", imgs)})
			}
		}
	}
	body := map[string]any{"model": c.model, "input": input, "store": false, "stream": true}
	if system != "" {
		body["instructions"] = system
	}
	if len(tools) > 0 {
		ts := make([]map[string]any, 0, len(tools))
		for _, t := range tools {
			ts = append(ts, map[string]any{"type": "function", "name": t.Name, "description": t.Desc, "parameters": t.Params})
		}
		body["tools"] = ts
	}
	b, err := c.do(ctx, "POST", "/responses", body)
	if err != nil {
		return aiTurn{}, err
	}
	return c.responsesTurn(ctx, b)
}

// sseData: the data of each event of a streamed answer.
func sseData(b []byte) [][]byte {
	var out [][]byte
	var cur []byte
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 64<<10), 32<<20)
	for sc.Scan() {
		line := sc.Bytes()
		switch {
		case len(line) == 0:
			if len(cur) > 0 {
				out = append(out, cur)
				cur = nil
			}
		case bytes.HasPrefix(line, []byte("data:")):
			d := bytes.TrimSpace(line[5:])
			if string(d) == "[DONE]" {
				continue
			}
			if len(cur) > 0 {
				cur = append(cur, '\n')
			}
			cur = append(cur, d...)
		}
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

type responsesItem struct {
	Type    string `json:"type"`
	CallID  string `json:"call_id"`
	Name    string `json:"name"`
	Args    string `json:"arguments"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

type responsesErr struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Param   string `json:"param"`
}

type responsesBody struct {
	Status     string          `json:"status"`
	Output     []responsesItem `json:"output"`
	Error      *responsesErr   `json:"error"`
	Incomplete *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
	Usage *struct {
		In      int64 `json:"input_tokens"`
		Out     int64 `json:"output_tokens"`
		Details *struct {
			Cached int64 `json:"cached_tokens"`
		} `json:"input_tokens_details"`
	} `json:"usage"`
}

// responsesTurn reads a streamed answer to its end: the text as it came, the function calls, what it used. Only
// response.completed makes it an answer; a failure, an answer left incomplete or a stream cut short is an error.
func (c *aiClient) responsesTurn(ctx context.Context, b []byte) (aiTurn, error) {
	out := aiTurn{Role: "assistant"}
	var text strings.Builder
	seen := map[string]bool{}
	var done *responsesBody
	for _, d := range sseData(b) {
		var e struct {
			Type     string          `json:"type"`
			Delta    string          `json:"delta"`
			Item     *responsesItem  `json:"item"`
			Response *responsesBody  `json:"response"`
			Code     string          `json:"code"`
			Message  string          `json:"message"`
			Param    string          `json:"param"`
			Error    json.RawMessage `json:"error"`
		}
		if json.Unmarshal(d, &e) != nil {
			continue
		}
		switch e.Type {
		case "response.output_text.delta":
			text.WriteString(e.Delta)
		case "response.output_item.done":
			if e.Item != nil && e.Item.Type == "function_call" && !seen[e.Item.CallID] {
				seen[e.Item.CallID] = true
				out.Calls = append(out.Calls, aiCall{ID: e.Item.CallID, Name: e.Item.Name, Args: json.RawMessage(e.Item.Args)})
			}
		case "response.completed":
			done = e.Response
		case "response.incomplete":
			why := ""
			if e.Response != nil && e.Response.Incomplete != nil {
				why = e.Response.Incomplete.Reason
			}
			if why == "max_output_tokens" {
				return aiTurn{}, errors.New("回答过长被截断：请更换模型，或拆分要求后重试")
			}
			return aiTurn{}, fmt.Errorf("ChatGPT：回答未完成（%s）", nonEmpty(why))
		case "response.failed":
			var re responsesErr
			if e.Response != nil && e.Response.Error != nil {
				re = *e.Response.Error
			}
			return aiTurn{}, gptFailure(0, re, "")
		case "error":
			re := responsesErr{Code: e.Code, Message: e.Message, Param: e.Param}
			var o responsesErr
			if json.Unmarshal(e.Error, &o) == nil && (o.Code != "" || o.Message != "") {
				re = o
			}
			return aiTurn{}, gptFailure(0, re, "")
		}
	}
	if done == nil {
		if ctx.Err() != nil {
			return aiTurn{}, errors.New("已停止")
		}
		return aiTurn{}, errors.New("ChatGPT：回答在完成前中断，请重试")
	}
	if u := done.Usage; u != nil {
		hit := int64(0)
		if u.Details != nil {
			hit = min(max(u.Details.Cached, 0), max(u.In, 0))
		}
		c.noteUsage(ctx, aiUsage{In: max(u.In-hit, 0), Hit: hit, Out: max(u.Out, 0)})
	}
	out.Text = text.String()
	for _, it := range done.Output { // (what the events did not bring is in the final answer)
		switch {
		case it.Type == "message" && out.Text == "":
			for _, p := range it.Content {
				if p.Type == "output_text" {
					out.Text += p.Text
				}
			}
		case it.Type == "function_call" && !seen[it.CallID]:
			seen[it.CallID] = true
			out.Calls = append(out.Calls, aiCall{ID: it.CallID, Name: it.Name, Args: json.RawMessage(it.Args)})
		}
	}
	for i := range out.Calls {
		if out.Calls[i].ID == "" {
			out.Calls[i].ID = fmt.Sprintf("call_%d_%d", time.Now().UnixNano(), i)
		}
	}
	if out.Text == "" && len(out.Calls) == 0 {
		return aiTurn{}, errors.New("ChatGPT：回答为空，请重试或更换模型")
	}
	return out, nil
}

// gptFailure: a request OpenAI did not answer, in words for the player — before the answer began (status: its
// HTTP status) or in the middle of it (status 0). Above all the plan's limit, whose way out is ChatGPT's usage
// settings. The service's own words go in Said, for overflow, busy and noPictures to read.
func gptFailure(status int, re responsesErr, reqID string) *aiHTTPError {
	he := &aiHTTPError{Status: status, After: -1, Said: strings.ToLower(strings.TrimSpace(re.Code + " " + re.Param + " " + re.Message))}
	if status == 0 { // in the middle of the answer: as the HTTP status it would have had
		switch {
		case re.Code == "context_length_exceeded":
			he.Status = 400
		case re.Code == "rate_limit_exceeded":
			he.Status = 429
		case strings.HasSuffix(re.Code, "_unavailable"), re.Code == "server_error":
			he.Status = 503
		}
	}
	said := re.Message
	if said == "" {
		said = re.Code
	}
	var what string
	switch re.Code {
	case "subscription_sharing_usage_limit_exceeded":
		he.code = "usage"
		what = "已达到用量上限：ChatGPT 套餐的可用额度已用完，或已达到你为本软件设定的上限。请在 ChatGPT 的「设置 → 用量」中查看和管理：" + gptUsageURL
		said = ""
	case "subscription_sharing_user_not_eligible":
		he.code = "denied" // (not asked again: the answer would be the same)
		what = "该 ChatGPT 账号（或其所在的工作区）目前不能在本软件中使用套餐额度"
	case "subscription_sharing_usage_unavailable", "subscription_sharing_user_unavailable":
		what = "ChatGPT 套餐额度暂时不可用，请稍后重试"
	case "subscription_sharing_unsupported_capability":
		what = "所选模型或此次请求的部分内容不支持通过 ChatGPT 套餐使用，请在「AI 服务」中更换模型"
		if re.Param != "" {
			what += "（" + core.Truncate(re.Param, 80) + "）"
		}
	case "subscription_sharing_route_not_supported":
		he.code = "denied"
		what = "ChatGPT 套餐不支持此类请求"
	case "subscription_sharing_invalid_user":
		what = "ChatGPT 未接受当前登录。请稍后重试，如一直出现，请在「AI 服务」中退出并重新登录"
	case "chatpass_v2_scope_not_authorized", "chatpass_v2_invalid_authorization_context":
		he.code = "plan"
		what = "ChatGPT 套餐的授权无效：请在「AI 服务」中点击「重新授权」"
	default:
		switch {
		case he.overflow():
			what = "对话内容已超出模型的上下文长度"
		case he.Status == 401:
			what = "ChatGPT 未接受当前登录。请稍后重试，如一直出现，请在「AI 服务」中退出并重新登录"
		case he.Status == 403:
			what = "ChatGPT 拒绝了请求（所在地区可能不在服务范围内，或该账号无权使用）"
		case he.Status == 404:
			what = "模型不存在或当前账号不可用：请在「AI 服务」中重新选择模型"
		case he.Status == 429:
			what = "请求过于频繁，请稍后重试"
		case he.Status == 400 || he.Status == 422:
			what = "请求未被接受"
		case he.Status >= 500:
			what = "ChatGPT 服务暂时不可用，请稍后重试"
		default:
			what = "请求失败"
		}
	}
	msg := what
	if !strings.HasPrefix(what, "ChatGPT") {
		msg = "ChatGPT：" + what
	}
	if status > 0 {
		msg += fmt.Sprintf("（HTTP %d）", status)
	}
	if said != "" && !strings.Contains(what, said) {
		msg += "。服务返回：" + core.Truncate(said, 240)
	}
	if reqID != "" {
		msg += "。请求编号：" + core.Truncate(reqID, 80)
	}
	he.Msg = msg
	return he
}

// gptHTTPError: OpenAI's refusal of a request on the plan, before an answer began: {"error": {code, message,
// param}}, or {"detail": …} when it was not let in at all.
func gptHTTPError(status int, reqID string, b []byte) *aiHTTPError {
	var r struct {
		Error  json.RawMessage `json:"error"`
		Detail json.RawMessage `json:"detail"`
	}
	var re responsesErr
	if json.Unmarshal(b, &r) == nil {
		if json.Unmarshal(r.Error, &re) != nil {
			_ = json.Unmarshal(r.Error, &re.Message)
		}
		if re.Message == "" && len(r.Detail) > 0 {
			var s string
			if json.Unmarshal(r.Detail, &s) == nil {
				re.Message = s
			} else {
				re.Message = string(r.Detail)
			}
		}
	}
	if re.Message == "" && re.Code == "" {
		re.Message = aiSaid(b)
	}
	return gptFailure(status, re, reqID)
}

// gptModels: the models of the signed-in account's plan, as OpenAI orders them (the ones it lists only), with the
// names it gives them.
func (c *aiClient) gptModels(ctx context.Context) ([]string, error) {
	b, err := c.do(ctx, "GET", "/models", nil)
	if err != nil {
		return nil, err
	}
	var r struct {
		Models []struct {
			Slug       string `json:"slug"`
			Name       string `json:"display_name"`
			Visibility string `json:"visibility"`
		} `json:"models"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, errors.New("ChatGPT：无法读取模型列表")
	}
	var out []string
	c.names = map[string]string{}
	for _, m := range r.Models {
		if m.Slug == "" || m.Visibility != "list" || c.names[m.Slug] != "" {
			continue
		}
		out = append(out, m.Slug)
		c.names[m.Slug] = nonEmptyOr(m.Name, m.Slug)
	}
	if len(out) == 0 {
		return nil, errors.New("ChatGPT：当前账号没有可用的模型")
	}
	return out, nil
}

func nonEmptyOr(s, or string) string {
	if strings.TrimSpace(s) == "" {
		return or
	}
	return s
}
