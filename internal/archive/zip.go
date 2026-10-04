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

	"vrclib/internal/core"
)

// extractZip unpacks zipPath into parent: a zip holding a single folder keeps that folder, anything
// else goes into a folder named after the zip. Returns the folder that was made. Nothing is left behind
// when it fails, and the caller keeps the zip.
func extractZip(zipPath, parent string) (string, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", fmt.Errorf("无法打开压缩包：%v", err)
	}
	defer zr.Close()
	names, err := zipNames(zr.File)
	if err != nil {
		return "", err
	}
	type ent struct {
		f   *zip.File
		rel string
	}
	var ents []ent
	tops := map[string]bool{}
	nested := true
	for i, f := range zr.File {
		rel := path.Clean("/" + names[i])[1:] // no "..", no absolute paths
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
	fail := func(err error) (string, error) {
		_ = os.RemoveAll(tmp)
		return "", err
	}
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
				return fail(fmt.Errorf("解压出错：无法创建文件夹（%v）", err))
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(out), 0755); err != nil {
			return fail(fmt.Errorf("解压出错：无法创建文件夹（%v）", err))
		}
		if err := writeZipEntry(e.f, out); os.IsExist(err) {
			// two entries under one name (names that differ only in case, or in characters Windows does not
			// allow): the second would replace the first without a word
			return fail(fmt.Errorf("压缩包内有重名的文件（%s），为避免文件相互覆盖，已停止解压并保留压缩包，请使用解压软件手动解压", rel))
		} else if err != nil {
			return fail(fmt.Errorf("解压出错：%v", err))
		}
	}
	if err := os.Rename(tmp, target); err != nil {
		return fail(fmt.Errorf("解压出错：无法将解压出的文件夹移至 %s（%v）", target, err))
	}
	return target, nil
}

// writeZipEntry writes one file of the zip. The file must not be there yet: an entry never replaces what
// an earlier one of the same archive wrote.
func writeZipEntry(f *zip.File, out string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	w, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0666)
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
