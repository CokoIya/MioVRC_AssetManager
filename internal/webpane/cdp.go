package webpane

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"vrclib/internal/core"
)

type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// cdpMsg covers both Chrome DevTools replies ({"id","result"|"error":{code,message}}) and
// WebDriver BiDi replies ({"type":"success"|"error","id","result","error":"…","message":"…"}).
type cdpMsg struct {
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Type    string          `json:"type"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error"`
	Message string          `json:"message"`
	Params  json.RawMessage `json:"params"`
}

func (m cdpMsg) err() error {
	if len(m.Error) == 0 || string(m.Error) == "null" {
		return nil
	}
	var ce cdpError
	if json.Unmarshal(m.Error, &ce) == nil && ce.Message != "" {
		return errors.New(ce.Message)
	}
	var code string
	_ = json.Unmarshal(m.Error, &code)
	if m.Message != "" {
		return errors.New(code + ": " + m.Message)
	}
	return errors.New(code)
}

type cdpConn struct {
	conn    net.Conn
	br      *bufio.Reader
	wmu     sync.Mutex
	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan cdpMsg
	done    chan struct{}
	once    sync.Once
	err     error
	onEvent func(method string, params json.RawMessage) // optional; most events are not needed
}

var ErrCDPClosed = errors.New("浏览器窗口已关闭")

func cdpDial(wsURL string, timeout time.Duration) (*cdpConn, error) {
	u, err := url.Parse(wsURL)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("tcp", u.Host, timeout)
	if err != nil {
		return nil, err
	}
	kb := make([]byte, 16)
	_, _ = rand.Read(kb)
	key := base64.StdEncoding.EncodeToString(kb)
	_ = conn.SetDeadline(time.Now().Add(timeout))
	req := "GET " + u.RequestURI() + " HTTP/1.1\r\nHost: " + u.Host +
		"\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: " + key +
		"\r\nSec-WebSocket-Version: 13\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		return nil, err
	}
	br := bufio.NewReaderSize(conn, 1<<16)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if resp.StatusCode != 101 {
		conn.Close()
		return nil, fmt.Errorf("websocket handshake: %s", resp.Status)
	}
	_ = conn.SetDeadline(time.Time{})
	c := &cdpConn{conn: conn, br: br, pending: map[int64]chan cdpMsg{}, done: make(chan struct{})}
	go c.readLoop()
	return c, nil
}

func (c *cdpConn) fail(err error) {
	c.once.Do(func() {
		c.err = err
		close(c.done)
		c.conn.Close()
	})
}

func (c *cdpConn) Close() {
	_ = c.writeFrame(0x8, nil)
	c.fail(ErrCDPClosed)
}

func (c *cdpConn) writeFrame(op byte, payload []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	var hdr [14]byte
	hdr[0] = 0x80 | op
	n := 2
	l := len(payload)
	switch {
	case l < 126:
		hdr[1] = 0x80 | byte(l)
	case l < 65536:
		hdr[1] = 0x80 | 126
		binary.BigEndian.PutUint16(hdr[2:], uint16(l))
		n = 4
	default:
		hdr[1] = 0x80 | 127
		binary.BigEndian.PutUint64(hdr[2:], uint64(l))
		n = 10
	}
	var mask [4]byte
	_, _ = rand.Read(mask[:])
	copy(hdr[n:], mask[:])
	n += 4
	buf := make([]byte, n+l)
	copy(buf, hdr[:n])
	for i, b := range payload {
		buf[n+i] = b ^ mask[i%4]
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
	_, err := c.conn.Write(buf)
	return err
}

func (c *cdpConn) readFrame() (bool, byte, []byte, error) {
	var h [2]byte
	if _, err := io.ReadFull(c.br, h[:]); err != nil {
		return false, 0, nil, err
	}
	fin := h[0]&0x80 != 0
	op := h[0] & 0x0f
	masked := h[1]&0x80 != 0
	l := uint64(h[1] & 0x7f)
	switch l {
	case 126:
		var b [2]byte
		if _, err := io.ReadFull(c.br, b[:]); err != nil {
			return false, 0, nil, err
		}
		l = uint64(binary.BigEndian.Uint16(b[:]))
	case 127:
		var b [8]byte
		if _, err := io.ReadFull(c.br, b[:]); err != nil {
			return false, 0, nil, err
		}
		l = binary.BigEndian.Uint64(b[:])
	}
	if l > 512<<20 {
		return false, 0, nil, errors.New("websocket frame too large")
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(c.br, mask[:]); err != nil {
			return false, 0, nil, err
		}
	}
	p := make([]byte, l)
	if _, err := io.ReadFull(c.br, p); err != nil {
		return false, 0, nil, err
	}
	if masked {
		for i := range p {
			p[i] ^= mask[i%4]
		}
	}
	return fin, op, p, nil
}

func (c *cdpConn) readLoop() {
	var msg []byte
	for {
		fin, op, p, err := c.readFrame()
		if err != nil {
			c.fail(ErrCDPClosed)
			return
		}
		switch op {
		case 0x9: // ping
			_ = c.writeFrame(0xA, p)
		case 0xA:
		case 0x8:
			c.fail(ErrCDPClosed)
			return
		case 0x1, 0x2:
			if fin {
				c.dispatch(p)
			} else {
				msg = append([]byte{}, p...)
			}
		case 0x0:
			msg = append(msg, p...)
			if fin {
				c.dispatch(msg)
				msg = nil
			}
		}
	}
}

func (c *cdpConn) dispatch(b []byte) {
	var m cdpMsg
	if json.Unmarshal(b, &m) != nil {
		return
	}
	if m.ID == 0 {
		if m.Method != "" && c.onEvent != nil {
			go c.onEvent(m.Method, m.Params)
		}
		return
	}
	c.mu.Lock()
	ch := c.pending[m.ID]
	delete(c.pending, m.ID)
	c.mu.Unlock()
	if ch != nil {
		ch <- m
	}
}

func (c *cdpConn) call(method string, params any, timeout time.Duration) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan cdpMsg, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	if params == nil {
		params = map[string]any{}
	}
	b, _ := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err := c.writeFrame(0x1, b); err != nil {
		c.fail(ErrCDPClosed)
		return nil, ErrCDPClosed
	}
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case m := <-ch:
		if err := m.err(); err != nil {
			return nil, err
		}
		return m.Result, nil
	case <-c.done:
		return nil, c.err
	case <-t.C:
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("%s 超时", method)
	}
}

// eval runs JS in the page and returns the JSON value of the result.
func (c *cdpConn) eval(expr string, await bool, timeout time.Duration) (json.RawMessage, error) {
	res, err := c.call("Runtime.evaluate", map[string]any{
		"expression": expr, "awaitPromise": await, "returnByValue": true, "userGesture": false,
	}, timeout)
	if err != nil {
		return nil, err
	}
	var r struct {
		Result struct {
			Type  string          `json:"type"`
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception *struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return nil, err
	}
	if r.ExceptionDetails != nil {
		msg := r.ExceptionDetails.Text
		if r.ExceptionDetails.Exception != nil && r.ExceptionDetails.Exception.Description != "" {
			msg = r.ExceptionDetails.Exception.Description
		}
		return nil, errors.New("页面脚本出错：" + firstLine(msg))
	}
	return r.Result.Value, nil
}

func (c *cdpConn) evalString(expr string, timeout time.Duration) (string, error) {
	v, err := c.eval(expr, false, timeout)
	if err != nil {
		return "", err
	}
	var s string
	_ = json.Unmarshal(v, &s)
	return s, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// ---------- launching a debuggable browser on its own profile ----------

func ReadDevToolsPort(profile string) (int, string) {
	b, err := os.ReadFile(filepath.Join(profile, "DevToolsActivePort"))
	if err != nil {
		return 0, ""
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	port, _ := strconv.Atoi(strings.TrimSpace(lines[0]))
	path := ""
	if len(lines) > 1 {
		path = strings.TrimSpace(lines[1])
	}
	return port, path
}

type cdpTarget struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	URL   string `json:"url"`
	WSURL string `json:"webSocketDebuggerUrl"`
}

var LocalHTTP = &http.Client{Timeout: 4 * time.Second, Transport: &http.Transport{Proxy: nil}}

func cdpTargets(port int) ([]cdpTarget, error) {
	resp, err := LocalHTTP.Get(fmt.Sprintf("http://127.0.0.1:%d/json/list", port))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var ts []cdpTarget
	err = json.NewDecoder(resp.Body).Decode(&ts)
	return ts, err
}

func cdpNewTab(port int, u string) {
	req, _ := http.NewRequest("PUT", fmt.Sprintf("http://127.0.0.1:%d/json/new?%s", port, u), nil)
	if resp, err := LocalHTTP.Do(req); err == nil {
		resp.Body.Close()
	}
}

// launchDebugBrowser opens startURL in a browser that runs on its own profile with a random
// DevTools port (only on 127.0.0.1). Reuses the window if it is still open from last time.
func launchDebugBrowser(browser, profile, startURL string, extra []string) (int, error) {
	_ = os.MkdirAll(profile, 0755)
	if port, _ := ReadDevToolsPort(profile); port > 0 {
		if _, err := cdpTargets(port); err == nil {
			cdpNewTab(port, startURL)
			return port, nil
		}
	}
	_ = os.Remove(filepath.Join(profile, "DevToolsActivePort"))
	args := []string{"--user-data-dir=" + profile, "--remote-debugging-port=0", "--remote-debugging-address=127.0.0.1",
		"--no-first-run", "--no-default-browser-check", "--disable-features=Translate", "--lang=zh-CN",
		"--window-size=1180,880", "--new-window"}
	args = append(args, extra...)
	args = append(args, startURL)
	cmd := core.BrowserCmd(browser, args)
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	go func() { _ = cmd.Wait() }()
	for i := 0; i < 150; i++ {
		time.Sleep(150 * time.Millisecond)
		if port, _ := ReadDevToolsPort(profile); port > 0 {
			if _, err := cdpTargets(port); err == nil {
				return port, nil
			}
		}
	}
	return 0, errors.New("浏览器没有打开调试端口")
}

func CloseDebugBrowser(port int) {
	resp, err := LocalHTTP.Get(fmt.Sprintf("http://127.0.0.1:%d/json/version", port))
	if err != nil {
		return
	}
	var v struct {
		WS string `json:"webSocketDebuggerUrl"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&v)
	resp.Body.Close()
	if v.WS == "" {
		return
	}
	if c, err := cdpDial(v.WS, 4*time.Second); err == nil {
		_, _ = c.call("Browser.close", nil, 4*time.Second)
		c.Close()
	}
}
