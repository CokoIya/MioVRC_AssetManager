package library

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"sync"

	"vrclib/internal/core"
)

var thumbMu sync.Mutex

// Thumbnail returns a cached JPEG thumbnail path for an image, or "" if it cannot be decoded.
func Thumbnail(src string, w int) string {
	fi, err := os.Stat(src)
	if err != nil {
		return ""
	}
	h := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d|%d", src, fi.Size(), fi.ModTime().Unix(), w)))
	dir := filepath.Join(core.CoversDir(), "thumbs")
	dst := filepath.Join(dir, hex.EncodeToString(h[:10])+".jpg")
	if _, err := os.Stat(dst); err == nil {
		return dst
	}
	thumbMu.Lock()
	defer thumbMu.Unlock()
	if _, err := os.Stat(dst); err == nil {
		return dst
	}
	f, err := os.Open(src)
	if err != nil {
		return ""
	}
	img, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		return ""
	}
	out := resizeFit(img, w)
	_ = os.MkdirAll(dir, 0755)
	of, err := os.Create(dst + ".part")
	if err != nil {
		return ""
	}
	err = jpeg.Encode(of, out, &jpeg.Options{Quality: 86})
	of.Close()
	if err != nil {
		os.Remove(dst + ".part")
		return ""
	}
	_ = os.Rename(dst+".part", dst)
	return dst
}

// resizeFit scales the image so its shorter side is w (cover crop happens in CSS), with 3x3 supersampling.
func resizeFit(img image.Image, w int) image.Image {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw == 0 || sh == 0 {
		return img
	}
	short := sw
	if sh < short {
		short = sh
	}
	if short <= w {
		return flatten(img)
	}
	scale := float64(w) / float64(short)
	tw, th := int(float64(sw)*scale+0.5), int(float64(sh)*scale+0.5)
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	const ss = 3
	for y := 0; y < th; y++ {
		for x := 0; x < tw; x++ {
			var r, g, bl, a uint32
			for j := 0; j < ss; j++ {
				for i := 0; i < ss; i++ {
					sx := b.Min.X + int((float64(x)+(float64(i)+0.5)/ss)/scale)
					sy := b.Min.Y + int((float64(y)+(float64(j)+0.5)/ss)/scale)
					if sx >= b.Max.X {
						sx = b.Max.X - 1
					}
					if sy >= b.Max.Y {
						sy = b.Max.Y - 1
					}
					cr, cg, cb, ca := img.At(sx, sy).RGBA()
					r += cr
					g += cg
					bl += cb
					a += ca
				}
			}
			n := uint32(ss * ss)
			// composite over dark background for transparent images
			al := a / n
			rr := (r/n)>>8 + (255-al>>8)*0x1a/255
			gg := (g/n)>>8 + (255-al>>8)*0x1d/255
			bb := (bl/n)>>8 + (255-al>>8)*0x24/255
			dst.Set(x, y, color.RGBA{uint8(min(rr, 255)), uint8(min(gg, 255)), uint8(min(bb, 255)), 255})
		}
	}
	return dst
}

func flatten(img image.Image) image.Image {
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			cr, cg, cb, ca := img.At(x, y).RGBA()
			al := ca >> 8
			dst.Set(x-b.Min.X, y-b.Min.Y, color.RGBA{
				uint8(min(cr>>8+(255-al)*0x1a/255, 255)),
				uint8(min(cg>>8+(255-al)*0x1d/255, 255)),
				uint8(min(cb>>8+(255-al)*0x24/255, 255)), 255})
		}
	}
	return dst
}
