package archive

import (
	"archive/zip"
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"vrclib/internal/core"
)

// Set is one archive as the player sees it: the file that opens it and every volume that belongs to it.
type Set struct {
	Main  string
	Parts []string
	Name  string // without the archive extensions: the folder it unpacks to
	Size  int64  // all volumes together
}

// Sets sorts archive files into archives, the volumes of one together ("x.part1.rar" + "x.part2.rar",
// "x.zip.001" …). A set whose first volume is missing is left out.
func Sets(files []string) []Set {
	var out []Set
	for _, s := range groupArchives(append([]string{}, files...)) {
		out = append(out, Set{Main: s.Main, Parts: s.Parts, Name: s.Name, Size: s.Size})
	}
	return out
}

// Entry is one file inside an archive ("/" between folders).
type Entry struct {
	Name string
	Size int64
}

// ErrNoListing: what is inside cannot be read here (rar, 7z and volumes need 7-Zip; encrypted names need the password).
var ErrNoListing = errors.New("无法读取压缩包内的文件列表")

// ListEntries: the files inside an archive, without unpacking it. A plain zip is read directly; anything
// else through 7-Zip when the computer has it.
func ListEntries(s Set) ([]Entry, error) {
	if len(s.Parts) == 1 && strings.EqualFold(core.LowerExt(s.Main), ".zip") {
		if out, err := ZipEntries(s.Main); err == nil {
			return out, nil
		}
	}
	for _, t := range toolsFor(s.Main) {
		if t.Kind != "7z" {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		b, err := core.Hidden(exec.CommandContext(ctx, t.exe, "l", "-slt", "-ba", "-sccUTF-8", "-p", "--", s.Main)).Output()
		cancel()
		if err != nil {
			return nil, ErrNoListing
		}
		return parse7zList(string(b)), nil
	}
	return nil, ErrNoListing
}

// ZipEntries: the files inside a plain zip, from its central directory alone — no other program is asked, so
// it is cheap enough for a scan. An error when it is no zip that can be read, or its names cannot be.
func ZipEntries(p string) ([]Entry, error) {
	zr, err := zip.OpenReader(p)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	names, err := zipNames(zr.File)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for i, f := range zr.File {
		if f.FileInfo().IsDir() || strings.HasSuffix(names[i], "/") {
			continue
		}
		out = append(out, Entry{Name: names[i], Size: int64(f.UncompressedSize64)})
	}
	return out, nil
}

// parse7zList reads "7z l -slt -ba": one block of "Key = value" lines per entry.
func parse7zList(s string) []Entry {
	var out []Entry
	var cur map[string]string
	flush := func() {
		if cur == nil {
			return
		}
		name, isDir := cur["Path"], cur["Folder"] == "+" || strings.HasPrefix(cur["Attributes"], "D")
		if _, arc := cur["Type"]; name != "" && !isDir && !arc { // (a block with a Type is the archive itself)
			n, _ := strconv.ParseInt(cur["Size"], 10, 64)
			out = append(out, Entry{Name: strings.ReplaceAll(name, "\\", "/"), Size: n})
		}
		cur = nil
	}
	for _, line := range strings.Split(strings.ReplaceAll(s, "\r", ""), "\n") {
		k, v, ok := strings.Cut(line, " = ")
		if !ok {
			if strings.TrimSpace(line) == "" {
				flush()
			}
			continue
		}
		if k == "Path" {
			flush()
			cur = map[string]string{}
		}
		if cur != nil {
			cur[k] = v
		}
	}
	flush()
	return out
}

// UnpackAgain unpacks an archive whose folder is there already — a newer file that came under the name of
// an earlier one — into a folder of its own ("Dress (2)"); the earlier folder stays as it is. again false:
// there is no earlier folder, and nothing was done (the archive is unpacked the usual way).
func UnpackAgain(s Set, pwd string) (dir string, again bool, err error) {
	as := archiveSet{Main: s.Main, Parts: s.Parts, Name: s.Name, Size: s.Size}
	if alreadyUnpacked(as) == "" {
		return "", false, nil
	}
	dir, err = extractArchive(as, pwd)
	return dir, true, err
}
