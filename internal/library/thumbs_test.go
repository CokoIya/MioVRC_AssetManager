package library

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"vrclib/internal/testkit"
)

// A picture no larger than asked goes to the window as it is (a JPEG of it would only lose detail); a larger one
// is made smaller, once, and the copy is used again.
func TestThumbnailSmallAsIs(t *testing.T) {
	testkit.NewStore(t)
	dir := t.TempDir()
	pic := func(name string, w, h int) string {
		img := image.NewNRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				img.SetNRGBA(x, y, color.NRGBA{uint8(x), uint8(y), 90, 255})
			}
		}
		var b bytes.Buffer
		if filepath.Ext(name) == ".png" {
			_ = png.Encode(&b, img)
		} else {
			_ = jpeg.Encode(&b, img, nil)
		}
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b.Bytes(), 0644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	preview := pic("preview.png", 128, 128)
	if got := Thumbnail(preview, 420); got != "" {
		t.Errorf("Unity's 128 px preview made into %q", got)
	}
	if got := Thumbnail(pic("wide.jpg", 400, 225), 420); got != "" {
		t.Errorf("a small JPEG made again into %q", got)
	}
	// larger than asked: a smaller copy, its shorter side as asked
	big := pic("big.png", 900, 600)
	got := Thumbnail(big, 420)
	f, err := os.Open(got)
	if got == "" || err != nil {
		t.Fatalf("no copy of a large picture: %q %v", got, err)
	}
	cfg, err := jpeg.DecodeConfig(f)
	f.Close()
	if err != nil || cfg.Width != 630 || cfg.Height != 420 {
		t.Errorf("the copy: %+v %v", cfg, err)
	}
	if again := Thumbnail(big, 420); again != got {
		t.Errorf("made again: %q, %q", again, got)
	}
	// the preview where a smaller one is asked for
	if got := Thumbnail(preview, 64); got == "" {
		t.Error("the preview not made smaller for 64 px")
	}
	// what is not a picture, or is gone
	if got := Thumbnail(filepath.Join(dir, "none.png"), 420); got != "" {
		t.Errorf("a missing picture: %q", got)
	}
	_ = os.WriteFile(filepath.Join(dir, "bad.png"), []byte("not a png"), 0644)
	if got := Thumbnail(filepath.Join(dir, "bad.png"), 420); got != "" {
		t.Errorf("not a picture: %q", got)
	}
}
