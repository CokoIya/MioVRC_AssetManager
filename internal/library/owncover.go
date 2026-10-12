package library

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"vrclib/internal/core"
)

// OwnCoverDir: where the copies of the pictures the player picked as covers are kept — the covers folder itself
// ("own-…"): what is there travels with a library export, and the covers of an import are looked for there.
func OwnCoverDir() string { return core.CoversDir() }

const ownCoverMax = 20 << 20

// CopyOwnCover: a copy of a picture the player picked from anywhere on this computer, to be a card's cover —
// it stays when the original is moved or deleted, and the original is never changed. Only a picture: by its
// extension and by what the file begins with. The same picture picked again is the same copy.
func CopyOwnCover(src string) (string, error) {
	ext := core.LowerExt(src)
	if ext == ".jpeg" {
		ext = ".jpg"
	}
	if !ImageExt[ext] {
		return "", errors.New("只能选择 PNG、JPG、WebP 或 GIF 图片")
	}
	fi, err := os.Stat(src)
	if err != nil || fi.IsDir() {
		return "", errors.New("无法读取所选的图片")
	}
	if fi.Size() == 0 || fi.Size() >= ownCoverMax {
		return "", errors.New("图片太大（最大 20 MB）")
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return "", errors.New("无法读取所选的图片")
	}
	if !strings.HasPrefix(http.DetectContentType(b), "image/") {
		return "", errors.New("所选文件不是图片")
	}
	sum := sha1.Sum(b)
	dir := OwnCoverDir()
	dst := filepath.Join(dir, "own-"+hex.EncodeToString(sum[:10])+ext)
	if fi, err := os.Stat(dst); err == nil && fi.Size() == int64(len(b)) {
		return dst, nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", errors.New("无法保存封面图片：" + core.TrimErr(err))
	}
	tmp := dst + ".tmp" // (whole or not there at all)
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		_ = os.Remove(tmp)
		return "", errors.New("无法保存封面图片：" + core.TrimErr(err))
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return "", errors.New("无法保存封面图片：" + core.TrimErr(err))
	}
	return dst, nil
}
