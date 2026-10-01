package main

// Texture-source packs: outfits often come with a separate "PSD" download for recolouring.
// The scanner counts those files so such packs can be told apart and filed under their outfit.

import (
	"archive/zip"
	"encoding/binary"
	"io"
	"os"
	"regexp"
	"strings"
)

// psdDims reads width and height from a Photoshop file header ("8BPS", version, 6 reserved bytes,
// channels, height, width).
func psdDims(p string) (int, int) {
	f, err := os.Open(p)
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	var h [26]byte
	if _, err := io.ReadFull(f, h[:]); err != nil || string(h[:4]) != "8BPS" {
		return 0, 0
	}
	return int(binary.BigEndian.Uint32(h[18:22])), int(binary.BigEndian.Uint32(h[14:18]))
}

type zipInfo struct{ psds, models, packages int }

// zipListing counts what a zip holds without unpacking it (only the central directory is read).
func zipListing(p string) zipInfo {
	var zi zipInfo
	zr, err := zip.OpenReader(p)
	if err != nil {
		return zi
	}
	defer zr.Close()
	for _, f := range zr.File {
		ext := lowerExt(f.Name)
		switch {
		case psdExt[ext]:
			zi.psds++
		case modelExt[ext]:
			zi.models++
		case ext == ".unitypackage":
			zi.packages++
		}
	}
	return zi
}

var rePSDName = regexp.MustCompile(`(^|[^a-z])(psd|psb|clip)([^a-z]|$)|テクスチャ素材|改変用素材|texture ?source`)

// isPSDOnly: a pack of texture sources with no Unity content (a "PSD" download next to an outfit).
func isPSDOnly(a *Asset) bool {
	if len(a.Packages) > 0 || a.ZipPackages > 0 || a.ModelFiles > 0 {
		return false
	}
	if a.PSDCount > 0 {
		return true
	}
	// an archive we cannot look into (rar / 7z): go by its name
	return !a.HasDir && rePSDName.MatchString(strings.ToLower(a.Name+" "+a.RawName))
}
