package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vrclib/internal/core"
)

// The log lines sent with feedback carry no address, signed link, login data or Windows account name —
// also when an earlier version wrote them into the log as they were.
func TestFeedbackLogIsMasked(t *testing.T) {
	core.DataDir = t.TempDir()
	st := core.LoadStore(filepath.Join(core.DataDir, "library.json"))
	log := filepath.Join(core.DataDir, "library.log")
	_ = os.WriteFile(log+".1", []byte("2026/10/01 10:00:00 the file before\n"), 0644)
	_ = os.WriteFile(log, []byte(strings.Join([]string{
		`2026/10/02 10:00:00 Gumroad 已登录：mio@example.com`,
		`2026/10/02 10:00:01 下载失败 55: Get "https://booth.pximg.net/d/file.zip?sig=SECRETSIG&exp=1": EOF`,
		`2026/10/02 10:00:02 请求 Cookie: _plaza_session_nktz7u=SECRETCOOKIE`,
		`2026/10/02 10:00:03 启动 v1.7.5 data=C:\Users\天川澪\AppData\Local\MioVRC_AssetManager`,
		`2026/10/02 10:00:04 已下载 Dress.zip → D:\VRChat素材\Dress`,
	}, "\n")+"\n"), 0644)
	info := feedbackInfo(st)
	for _, secret := range []string{"mio@", "SECRETSIG", "exp=1", "SECRETCOOKIE", "天川澪"} {
		if strings.Contains(info, secret) {
			t.Errorf("%q is sent along:\n%s", secret, info)
		}
	}
	for _, kept := range []string{"***@example.com", "https://booth.pximg.net/d/file.zip?…", `data=C:\Users\***\AppData\Local\MioVRC_AssetManager`, `D:\VRChat素材\Dress`, "the file before"} {
		if !strings.Contains(info, kept) {
			t.Errorf("%q is missing:\n%s", kept, info)
		}
	}
}
