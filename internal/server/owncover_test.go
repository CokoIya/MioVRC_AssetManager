package server

import (
	"bytes"
	"image"
	"image/png"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vrclib/internal/core"
	"vrclib/internal/library"
	"vrclib/internal/testkit"
)

// A picture from anywhere on this computer as a card's cover: a copy is kept in the program's folder (the
// picture itself is left alone), the card shows it, and 「恢复默认封面」 takes it away again.
func TestOwnCoverAPI(t *testing.T) {
	st := testkit.NewStore(t)
	st.Assets = []*core.Asset{{Key: "name:dress", Name: "Dress", Category: "衣服"}}
	st.User["pan:1Share"] = &core.UserData{ShareURL: "https://pan.baidu.com/s/1Share"}
	srv := httptest.NewServer(NewMux(st))
	defer srv.Close()

	var img bytes.Buffer
	_ = png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 8, 8)))
	pic := filepath.Join(t.TempDir(), "我的封面.PNG")
	_ = os.WriteFile(pic, img.Bytes(), 0644)
	t.Setenv("VRCLIB_PICK_FILE", pic)

	r := postJSON(t, srv, "/api/cover/own", map[string]any{"key": "name:dress"})
	cover, _ := r["cover"].(string)
	if r["ok"] != true || filepath.Dir(cover) != library.OwnCoverDir() || !strings.HasSuffix(cover, ".png") {
		t.Fatalf("picked: %v", r)
	}
	if b, _ := os.ReadFile(cover); !bytes.Equal(b, img.Bytes()) {
		t.Error("the copy is not the picture")
	}
	if b, _ := os.ReadFile(pic); !bytes.Equal(b, img.Bytes()) {
		t.Error("the picture itself was changed")
	}
	st.Mu.RLock()
	got := st.User["name:dress"].Cover
	st.Mu.RUnlock()
	if got != cover {
		t.Errorf("card's cover: %q", got)
	}
	// the same picture for a netdisk card: the same copy
	if r := postJSON(t, srv, "/api/cover/own", map[string]any{"key": "pan:1Share"}); r["cover"] != cover {
		t.Errorf("again: %v", r)
	}
	// back to the card's own
	if r := postJSON(t, srv, "/api/cover/own", map[string]any{"key": "name:dress", "reset": true}); r["ok"] != true || r["cover"] != "" {
		t.Errorf("reset: %v", r)
	}
	st.Mu.RLock()
	got = st.User["name:dress"].Cover
	st.Mu.RUnlock()
	if got != "" {
		t.Errorf("after reset: %q", got)
	}
	// not a picture, whatever its name says; a card that is not there; the window closed without a pick
	txt := filepath.Join(t.TempDir(), "readme.png")
	_ = os.WriteFile(txt, []byte("just text, not a picture"), 0644)
	t.Setenv("VRCLIB_PICK_FILE", txt)
	if r := postJSON(t, srv, "/api/cover/own", map[string]any{"key": "name:dress"}); r["ok"] != false || r["err"] != "所选文件不是图片" {
		t.Errorf("text: %v", r)
	}
	t.Setenv("VRCLIB_PICK_FILE", pic)
	if r := postJSON(t, srv, "/api/cover/own", map[string]any{"key": "name:nothing"}); r["ok"] != false || r["err"] != "未找到该素材" {
		t.Errorf("unknown card: %v", r)
	}
	t.Setenv("VRCLIB_PICK_FILE", "")
	if r := postJSON(t, srv, "/api/cover/own", map[string]any{"key": "name:dress"}); r["ok"] != false || r["cancelled"] != true || r["err"] != nil {
		t.Errorf("cancelled: %v", r)
	}
}
