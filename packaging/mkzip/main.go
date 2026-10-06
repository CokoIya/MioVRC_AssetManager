// mkzip packs a folder into the zip of the portable copy, the way `zip -r` did, with one difference: a name
// that is not plain ASCII (使用说明.txt) is marked as UTF-8 (general purpose bit 11). Without the mark a program
// that goes by the book — Windows Explorer — reads the name in the system's code page and shows it garbled;
// Info-ZIP's zip leaves the mark out when the build does not run in a UTF-8 locale.
//
//	go run ./packaging/mkzip <zip to write> <folder>
//
// The folder's own name is the top entry of the zip.
package main

import (
	"archive/zip"
	"compress/flate"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: mkzip <zip to write> <folder>")
		os.Exit(2)
	}
	if err := pack(os.Args[1], os.Args[2]); err != nil {
		_ = os.Remove(os.Args[1])
		fmt.Fprintln(os.Stderr, "mkzip:", err)
		os.Exit(1)
	}
}

func pack(dst, dir string) error {
	dir = filepath.Clean(dir)
	if st, err := os.Stat(dir); err != nil {
		return err
	} else if !st.IsDir() {
		return fmt.Errorf("%s is not a folder", dir)
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(out)
	zw.RegisterCompressor(zip.Deflate, func(w io.Writer) (io.WriteCloser, error) { // (the program downloads this to update itself)
		return flate.NewWriter(w, flate.BestCompression)
	})
	top := filepath.Dir(dir)
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("%s is neither a file nor a folder", p)
		}
		rel, err := filepath.Rel(top, p)
		if err != nil {
			return err
		}
		h, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		h.Name = filepath.ToSlash(rel) // (the writer marks it as UTF-8 when it is not plain ASCII)
		// as zip did: the old date field holds the time of day of the computer the zip is made on — a program
		// that reads only that field (Windows Explorer) takes it for the time of day where it runs. The exact
		// time goes into the zip beside it.
		h.Modified = info.ModTime()
		if info.IsDir() {
			h.Name += "/"
			h.SetMode(fs.ModeDir | 0755)
			_, err = zw.CreateHeader(h)
			return err
		}
		// the same in every zip, whatever the build's files are like: one that cannot be written to here would
		// come out read-only on Windows
		h.SetMode(0644)
		h.Method = zip.Deflate
		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		return err
	})
	if err == nil {
		err = zw.Close()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}
