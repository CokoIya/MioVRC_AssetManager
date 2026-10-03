package archive

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"vrclib/internal/core"
)

// zipEntryName: the name of an entry as the author saw it. Zips from Japanese Windows store names
// in Shift-JIS without saying so; a name that is not valid UTF-8 is read that way.
func zipEntryName(f *zip.File) string {
	n := f.Name
	if f.NonUTF8 && !utf8.ValidString(n) {
		if s, ok := core.DecodeCP932([]byte(n)); ok {
			n = s
		}
	}
	return strings.ReplaceAll(n, "\\", "/")
}

// extractZip unpacks zipPath into parent: a zip holding a single folder keeps that folder, anything
// else goes into a folder named after the zip. Returns the folder that was made.
func extractZip(zipPath, parent string) (string, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", fmt.Errorf("无法打开压缩包：%v", err)
	}
	defer zr.Close()
	type ent struct {
		f   *zip.File
		rel string
	}
	var ents []ent
	tops := map[string]bool{}
	nested := true
	for _, f := range zr.File {
		rel := path.Clean("/" + zipEntryName(f))[1:] // no "..", no absolute paths
		if rel == "" || rel == "." || strings.HasPrefix(rel, "__MACOSX") || strings.HasSuffix(rel, ".DS_Store") {
			continue
		}
		parts := strings.Split(rel, "/")
		tops[parts[0]] = true
		if len(parts) == 1 && !f.FileInfo().IsDir() {
			nested = false
		}
		ents = append(ents, ent{f, rel})
	}
	if len(ents) == 0 {
		return "", errors.New("压缩包为空")
	}
	var target, strip string
	if len(tops) == 1 && nested {
		for t := range tops {
			strip = t + "/"
			target = filepath.Join(parent, core.SafeName(t, 120))
		}
	} else {
		target = filepath.Join(parent, core.SafeName(core.StripArchiveExt(filepath.Base(zipPath)), 120))
	}
	for i := 2; ; i++ { // never write into something that is already there
		if _, err := os.Stat(target); os.IsNotExist(err) {
			break
		}
		base := strings.TrimSuffix(target, fmt.Sprintf(" (%d)", i-1))
		target = fmt.Sprintf("%s (%d)", base, i)
	}
	tmp := filepath.Join(parent, "."+filepath.Base(target)+".extracting")
	_ = os.RemoveAll(tmp)
	for _, e := range ents {
		rel := strings.TrimPrefix(e.rel, strip)
		if rel == "" || rel == strings.TrimSuffix(strip, "/") {
			continue
		}
		segs := strings.Split(rel, "/")
		for i := range segs {
			segs[i] = core.SafeName(segs[i], 200)
		}
		out := filepath.Join(append([]string{tmp}, segs...)...)
		if e.f.FileInfo().IsDir() {
			if err := os.MkdirAll(out, 0755); err != nil {
				return "", err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(out), 0755); err != nil {
			return "", err
		}
		if err := writeZipEntry(e.f, out); err != nil {
			_ = os.RemoveAll(tmp)
			return "", fmt.Errorf("解压出错：%v", err)
		}
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.RemoveAll(tmp)
		return "", err
	}
	return target, nil
}

func writeZipEntry(f *zip.File, out string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	w, err := os.Create(out)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, rc); err != nil {
		w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	if !f.Modified.IsZero() {
		_ = os.Chtimes(out, f.Modified, f.Modified)
	}
	return nil
}
