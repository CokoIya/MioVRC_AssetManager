package main

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The zip of the portable copy: the folder's name on top, the Chinese name marked as UTF-8, the plain ones not
// (as before), nothing read-only, the time of day of this computer in the old date field and the exact time
// beside it, and what went in comes out.
func TestPack(t *testing.T) {
	was := time.Local
	time.Local = time.FixedZone("here", 9*3600)
	defer func() { time.Local = was }()
	when := time.Date(2026, 10, 6, 17, 1, 16, 0, time.Local)
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "port", "App")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"App.exe": "MZ" + string(make([]byte, 70000)), "使用说明.txt": "\xef\xbb\xbf说明\n"}
	for n, body := range files {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(body), 0444); err != nil { // (cannot be written to)
			t.Fatal(err)
		}
		if err := os.Chtimes(filepath.Join(dir, n), when, when); err != nil {
			t.Fatal(err)
		}
	}
	dst := filepath.Join(tmp, "out.zip")
	if err := os.WriteFile(dst, []byte("an older zip"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := pack(dst, dir+string(filepath.Separator)); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	seen := map[string]bool{}
	for _, f := range zr.File {
		seen[f.Name] = true
		if f.ExternalAttrs&1 != 0 {
			t.Errorf("%s: read-only", f.Name)
		}
		utf8 := f.Flags&0x800 != 0
		switch f.Name {
		case "App/":
			if utf8 || !f.FileInfo().IsDir() {
				t.Errorf("the folder: flags %#x, mode %v", f.Flags, f.Mode())
			}
			continue
		case "App/App.exe":
			if utf8 {
				t.Errorf("%s: marked as UTF-8", f.Name)
			}
		case "App/使用说明.txt":
			if !utf8 || f.NonUTF8 {
				t.Errorf("%s: not marked as UTF-8 (flags %#x)", f.Name, f.Flags)
			}
		default:
			t.Errorf("not expected in the zip: %q", f.Name)
			continue
		}
		if f.Method != zip.Deflate {
			t.Errorf("%s: method %d", f.Name, f.Method)
		}
		if old := f.ModTime(); old.Hour() != 17 || old.Minute() != 1 || old.Day() != 6 { // (the old field, read as it stands)
			t.Errorf("%s: the old date field says %v", f.Name, old)
		}
		if !f.Modified.Equal(when) {
			t.Errorf("%s: the time is %v, want %v", f.Name, f.Modified, when)
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if want := files[filepath.Base(f.Name)]; err != nil || string(b) != want {
			t.Errorf("%s: %d bytes (%v), want %d", f.Name, len(b), err, len(want))
		}
	}
	if len(seen) != 3 {
		t.Errorf("in the zip: %v", seen)
	}

	// not a folder; a folder that is not there: an error, and no zip left behind
	for _, bad := range []string{filepath.Join(dir, "App.exe"), filepath.Join(tmp, "nothing")} {
		if err := pack(filepath.Join(tmp, "bad.zip"), bad); err == nil {
			t.Errorf("%s: packed", bad)
		}
	}
}
