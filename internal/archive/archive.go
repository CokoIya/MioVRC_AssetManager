package archive

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"vrclib/internal/core"
)

// archiveSet is one archive: the file to open and every volume that belongs to it.
type archiveSet struct {
	Main  string   `json:"main"`
	Parts []string `json:"parts"`
	Name  string   `json:"name"` // without the archive extensions: the folder it unpacks to
	Size  int64    `json:"size"`
}

var (
	reVolNum   = regexp.MustCompile(`(?i)^(.+)\.(zip|7z|rar|tar)\.(\d{3})$`) // x.zip.001
	reVolPart  = regexp.MustCompile(`(?i)^(.+)\.part0*(\d+)\.rar$`)          // x.part1.rar
	reVolZnn   = regexp.MustCompile(`(?i)^(.+)\.z(\d{2})$`)                  // x.z01 (with x.zip)
	reVolRnn   = regexp.MustCompile(`(?i)^(.+)\.r(\d{2})$`)                  // x.r00 (with x.rar)
	reArchive  = regexp.MustCompile(`(?i)\.(zip|7z|rar)$`)
	ErrArcPwd  = errors.New("压缩包有密码")
	errNoTool  = errors.New("电脑上没找到解压软件（7-Zip、Bandizip、WinRAR 等），rar、7z、分卷和带密码的压缩包解不开")
	reWrongPwd = regexp.MustCompile(`(?i)wrong password|password is incorrect|密码错误|incorrect password|can not open encrypted`)
)

// archiveName: the name an archive unpacks to ("Dress.part1.rar" → "Dress").
func archiveName(file string) string {
	b := filepath.Base(file)
	for _, re := range []*regexp.Regexp{reVolNum, reVolPart} {
		if m := re.FindStringSubmatch(b); m != nil {
			return m[1]
		}
	}
	return strings.TrimSuffix(b, filepath.Ext(b))
}

// groupArchives sorts the archive files in one folder into archives (volumes together).
func groupArchives(files []string) []archiveSet {
	type key struct{ dir, name string }
	sets := map[key]*archiveSet{}
	var order []key
	get := func(dir, name string) *archiveSet {
		k := key{dir, strings.ToLower(name)}
		if s := sets[k]; s != nil {
			return s
		}
		s := &archiveSet{Name: name}
		sets[k] = s
		order = append(order, k)
		return s
	}
	sort.Strings(files)
	for _, f := range files {
		dir, b := filepath.Dir(f), filepath.Base(f)
		switch {
		case reVolNum.MatchString(b):
			m := reVolNum.FindStringSubmatch(b)
			s := get(dir, m[1])
			s.Parts = append(s.Parts, f)
			if m[3] == "001" {
				s.Main = f
			}
		case reVolPart.MatchString(b):
			m := reVolPart.FindStringSubmatch(b)
			s := get(dir, m[1])
			s.Parts = append(s.Parts, f)
			if strings.TrimLeft(m[2], "0") == "1" {
				s.Main = f
			}
		case reVolZnn.MatchString(b), reVolRnn.MatchString(b):
			var m []string
			if m = reVolZnn.FindStringSubmatch(b); m == nil {
				m = reVolRnn.FindStringSubmatch(b)
			}
			s := get(dir, m[1])
			s.Parts = append(s.Parts, f)
		case reArchive.MatchString(b):
			s := get(dir, strings.TrimSuffix(b, filepath.Ext(b)))
			s.Parts = append(s.Parts, f)
			if s.Main == "" || !reVolNum.MatchString(filepath.Base(s.Main)) {
				s.Main = f // x.zip with x.z01…, x.rar with x.r00…: the archive itself opens the set
			}
		}
	}
	var out []archiveSet
	for _, k := range order {
		s := sets[k]
		if s.Main == "" {
			continue // only some later volumes: the first one is missing
		}
		for _, p := range s.Parts {
			if st, err := os.Stat(p); err == nil {
				s.Size += st.Size()
			}
		}
		out = append(out, *s)
	}
	return out
}

func IsArchiveFile(name string) bool {
	b := filepath.Base(name)
	return reArchive.MatchString(b) || reVolNum.MatchString(b) || reVolZnn.MatchString(b) || reVolRnn.MatchString(b)
}

// findArchives: the archives under dir (or dir itself when it is an archive file).
func findArchives(dir string) []archiveSet {
	st, err := os.Stat(dir)
	if err != nil {
		return nil
	}
	if !st.IsDir() {
		if !IsArchiveFile(dir) {
			return nil
		}
		// a single archive file: with its sibling volumes
		ents, _ := os.ReadDir(filepath.Dir(dir))
		var files []string
		name := strings.ToLower(archiveName(dir))
		for _, e := range ents {
			p := filepath.Join(filepath.Dir(dir), e.Name())
			if !e.IsDir() && IsArchiveFile(p) && strings.ToLower(archiveName(p)) == name {
				files = append(files, p)
			}
		}
		return groupArchives(files)
	}
	var files []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && strings.HasSuffix(d.Name(), ".extracting") {
			return filepath.SkipDir
		}
		if !d.IsDir() && IsArchiveFile(p) {
			files = append(files, p)
		}
		return nil
	})
	return groupArchives(files)
}

// ---------- unpacking ----------

type arcTool struct {
	Kind string // 7z, bz, winrar, unrar, haozip, tar: how its command line works
	exe  string
	Name string // shown to the player
}

func firstExisting(paths ...string) string {
	for _, p := range paths {
		if p == "" {
			continue
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// toolFromExe: the command-line program of an archive manager, from the program Windows opens archives
// with (7zFM.exe → 7z.exe next to it, Bandizip.exe → bz.exe …). nil: no command line to use.
func toolFromExe(exe string) *arcTool {
	if exe == "" {
		return nil
	}
	dir, base := filepath.Dir(exe), strings.ToLower(filepath.Base(exe))
	in := func(names ...string) string {
		for _, n := range names {
			if p := firstExisting(filepath.Join(dir, n)); p != "" {
				return p
			}
		}
		return ""
	}
	switch {
	case strings.HasPrefix(base, "7z"):
		if p := in("7z.exe", "7za.exe"); p != "" {
			return &arcTool{"7z", p, "7-Zip"}
		}
	case strings.HasPrefix(base, "nanazip"):
		if p := in("NanaZipC.exe"); p != "" {
			return &arcTool{"7z", p, "NanaZip"}
		}
		if p, err := exec.LookPath("NanaZipC.exe"); err == nil {
			return &arcTool{"7z", p, "NanaZip"}
		}
	case strings.HasPrefix(base, "bandizip") || base == "bz.exe":
		if p := in("bz.exe"); p != "" {
			return &arcTool{"bz", p, "Bandizip"}
		}
	case strings.HasPrefix(base, "winrar") || base == "rar.exe" || base == "unrar.exe":
		if p := in("WinRAR.exe"); p != "" {
			return &arcTool{"winrar", p, "WinRAR"}
		}
		if p := in("UnRAR.exe"); p != "" {
			return &arcTool{"unrar", p, "WinRAR"}
		}
	case strings.HasPrefix(base, "haozip"):
		if p := in("HaoZipC.exe"); p != "" {
			return &arcTool{"haozip", p, "好压"}
		}
	case strings.HasPrefix(base, "peazip"):
		if p := in(`res\bin\7z\7z.exe`, `res\7z\7z.exe`); p != "" {
			return &arcTool{"7z", p, "PeaZip"}
		}
	}
	return nil
}

// defaultArcTool: the archive program the player chose in Windows for this kind of file.
func defaultArcTool(archive string) *arcTool {
	ext := strings.ToLower(filepath.Ext(archive))
	if m := reVolNum.FindStringSubmatch(filepath.Base(archive)); m != nil {
		ext = "." + strings.ToLower(m[2]) // x.7z.001: the program for .7z (or the one for .001)
		if t := toolFromExe(core.DefaultArcExe(".001")); t != nil {
			return t
		}
	}
	return toolFromExe(core.DefaultArcExe(ext))
}

// ArchiveTools: every archive program found, the usual ones first.
func ArchiveTools() []arcTool {
	pf, pf86 := os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")
	j := func(base, rel string) string {
		if base == "" {
			return ""
		}
		return filepath.Join(base, rel)
	}
	var out []arcTool
	for _, ext := range []string{".7z", ".rar", ".zip"} { // the defaults, wherever they are installed
		if t := toolFromExe(core.DefaultArcExe(ext)); t != nil {
			out = append(out, *t)
		}
	}
	if p := firstExisting(j(pf, `7-Zip\7z.exe`), j(pf86, `7-Zip\7z.exe`)); p != "" {
		out = append(out, arcTool{"7z", p, "7-Zip"})
	} else {
		for _, n := range []string{"7z", "7zz", "7za", "NanaZipC"} {
			if p, err := exec.LookPath(n); err == nil {
				out = append(out, arcTool{"7z", p, "7-Zip"})
				break
			}
		}
	}
	if p := firstExisting(j(pf, `Bandizip\bz.exe`), j(pf86, `Bandizip\bz.exe`)); p != "" {
		out = append(out, arcTool{"bz", p, "Bandizip"})
	}
	if p := firstExisting(j(pf, `WinRAR\WinRAR.exe`), j(pf86, `WinRAR\WinRAR.exe`)); p != "" {
		out = append(out, arcTool{"winrar", p, "WinRAR"})
	} else if p := firstExisting(j(pf, `WinRAR\UnRAR.exe`), j(pf86, `WinRAR\UnRAR.exe`)); p != "" {
		out = append(out, arcTool{"unrar", p, "WinRAR"})
	}
	if p := firstExisting(j(pf, `2345Soft\HaoZip\HaoZipC.exe`), j(pf86, `2345Soft\HaoZip\HaoZipC.exe`), j(pf, `HaoZip\HaoZipC.exe`)); p != "" {
		out = append(out, arcTool{"haozip", p, "好压"})
	}
	if p := firstExisting(filepath.Join(os.Getenv("SystemRoot"), `System32\tar.exe`)); p != "" {
		out = append(out, arcTool{"tar", p, "Windows"})
	}
	// each program once
	seen := map[string]bool{}
	kept := out[:0]
	for _, t := range out {
		if k := strings.ToLower(t.exe); !seen[k] {
			seen[k] = true
			kept = append(kept, t)
		}
	}
	return kept
}

// toolsFor: the programs to try for one archive — the player's default for its kind first.
func toolsFor(archive string) []arcTool {
	all := ArchiveTools()
	d := defaultArcTool(archive)
	if d == nil {
		return all
	}
	out := []arcTool{*d}
	for _, t := range all {
		if !strings.EqualFold(t.exe, d.exe) {
			out = append(out, t)
		}
	}
	return out
}

// zipNeedsTool: zips the built-in reader cannot do (encrypted entries, other compression methods).
func zipNeedsTool(p string) (needs bool, sjis bool) {
	zr, err := zip.OpenReader(p)
	if err != nil {
		return true, false
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Flags&0x1 != 0 || (f.Method != zip.Store && f.Method != zip.Deflate) {
			needs = true
		}
		if f.NonUTF8 && !utf8.ValidString(f.Name) {
			if s, ok := core.DecodeCP932([]byte(f.Name)); ok && hasKana(s) {
				sjis = true
			}
		}
	}
	return needs, sjis
}

func hasKana(s string) bool {
	for _, r := range s {
		if (r >= 0x3040 && r <= 0x30ff) || (r >= 0xff66 && r <= 0xff9d) {
			return true
		}
	}
	return false
}

// extractArchive unpacks a into its folder (a single folder inside is kept as it is, anything else
// goes into a folder named after the archive) and returns the folder that was made.
func extractArchive(a archiveSet, pwd string) (string, error) {
	parent := filepath.Dir(a.Main)
	single := len(a.Parts) == 1 && strings.EqualFold(filepath.Ext(a.Main), ".zip")
	sjis := false
	if single {
		needs, sj := zipNeedsTool(a.Main)
		sjis = sj
		if !needs {
			return extractZip(a.Main, parent)
		}
	}
	tools := toolsFor(a.Main)
	tmp := filepath.Join(parent, "."+core.SafeName(a.Name, 100)+".extracting")
	var lastErr error
	tried := 0
	for _, t := range tools {
		if t.Kind == "unrar" && !strings.Contains(strings.ToLower(a.Main), ".rar") {
			continue
		}
		if t.Kind == "tar" && (pwd != "" || len(a.Parts) > 1) {
			continue
		}
		tried++
		_ = os.RemoveAll(tmp)
		if err := os.MkdirAll(tmp, 0755); err != nil {
			return "", err
		}
		err := runArcTool(t, a.Main, tmp, pwd, sjis)
		if err == nil {
			return placeExtracted(tmp, parent, a.Name)
		}
		_ = os.RemoveAll(tmp)
		lastErr = err
		if errors.Is(err, ErrArcPwd) {
			return "", err // another program will not know the password either
		}
	}
	if lastErr == nil || tried == 0 {
		lastErr = errNoTool
	}
	return "", lastErr
}

func runArcTool(t arcTool, archive, out, pwd string, sjis bool) error {
	var args []string
	switch t.Kind {
	case "7z":
		args = []string{"x", "-y", "-bd", "-o" + out, "-p" + pwd} // -p always: never wait for a typed password
		if sjis {
			args = append(args, "-mcp=932")
		}
		args = append(args, "--", archive)
	case "bz":
		args = []string{"x", "-y", "-o:" + out}
		if pwd != "" {
			args = append(args, "-p:"+pwd)
		}
		if sjis {
			args = append(args, "-cp:932")
		}
		args = append(args, archive)
	case "unrar", "winrar":
		pw := "-p" + pwd
		if pwd == "" {
			pw = "-p-" // never ask
		}
		args = []string{"x", "-y", "-idq", pw, archive, out + string(os.PathSeparator)}
		if t.Kind == "winrar" {
			args = []string{"x", "-y", "-ibck", "-inul", pw, archive, out + string(os.PathSeparator)}
		}
	case "haozip":
		args = []string{"x", archive, "-o" + out, "-y", "-p" + pwd}
	case "tar":
		args = []string{"-xf", archive, "-C", out}
	}
	// a program that stops to ask something would wait forever in its hidden window
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()
	cmd := core.Hidden(exec.CommandContext(ctx, t.exe, args...))
	b, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		core.Logf("解压超时（%s）%s", t.Name, archive)
		return fmt.Errorf("%s 解压没有完成（超时）", t.Name)
	}
	if err == nil {
		if ents, _ := os.ReadDir(out); len(ents) == 0 {
			return errors.New("解压后什么也没有")
		}
		return nil
	}
	msg := string(b)
	var ee *exec.ExitError
	if reWrongPwd.MatchString(msg) || ((t.Kind == "winrar" || t.Kind == "unrar") && errors.As(err, &ee) && ee.ExitCode() == 11) {
		return ErrArcPwd // (RAR's exit code 11: wrong password)
	}
	core.Logf("解压失败（%s）%s: %v %s", t.Name, archive, err, core.Truncate(strings.TrimSpace(msg), 300))
	return fmt.Errorf("%s 解压失败", t.Name)
}

// placeExtracted moves what was unpacked into tmp next to the archive.
func placeExtracted(tmp, parent, name string) (string, error) {
	ents, err := os.ReadDir(tmp)
	if err != nil {
		return "", err
	}
	src, target := tmp, filepath.Join(parent, core.SafeName(name, 120))
	if len(ents) == 1 && ents[0].IsDir() {
		src = filepath.Join(tmp, ents[0].Name())
		target = filepath.Join(parent, core.SafeName(ents[0].Name(), 120))
	}
	target = UniquePath(target)
	if err := os.Rename(src, target); err != nil {
		_ = os.RemoveAll(tmp)
		return "", err
	}
	_ = os.RemoveAll(tmp)
	return target, nil
}

// UniquePath: p, or "p (2)", "p (3)" … when p is taken.
func UniquePath(p string) string {
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return p
	}
	ext := ""
	base := p
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		ext = filepath.Ext(p)
		base = strings.TrimSuffix(p, ext)
	}
	for i := 2; ; i++ {
		q := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if _, err := os.Stat(q); os.IsNotExist(err) {
			return q
		}
	}
}

// unpackAll unpacks every archive under the given places, then what those contained (an archive in
// an archive, PSD packs inside the main zip …), up to three levels. Archives that unpacked fine are
// handed to remove: the Recycle Bin for the player's own files, deletion for this program's downloads,
// nil keeps them. report is called before each archive.
type unpackResult struct {
	Done    []string // folders made
	Failed  map[string]string
	Removed int
}

func UnpackAll(places []string, pwd string, remove func([]string) error, report func(name string, i, n int)) unpackResult {
	res := unpackResult{Failed: map[string]string{}}
	seen := map[string]bool{}
	queue := places
	for level := 0; level < 3 && len(queue) > 0; level++ {
		var sets []archiveSet
		for _, p := range queue {
			for _, a := range findArchives(p) {
				if !seen[strings.ToLower(a.Main)] {
					seen[strings.ToLower(a.Main)] = true
					sets = append(sets, a)
				}
			}
		}
		queue = nil
		for i, a := range sets {
			if report != nil {
				report(filepath.Base(a.Main), i, len(sets))
			}
			if dir := alreadyUnpacked(a); dir != "" {
				queue = append(queue, dir) // unpacked before (by hand): leave the archive alone
				continue
			}
			out, err := extractArchive(a, pwd)
			if err != nil {
				res.Failed[a.Main] = err.Error()
				continue
			}
			res.Done = append(res.Done, out)
			queue = append(queue, out)
			if remove != nil {
				if err := remove(a.Parts); err != nil {
					core.Logf("压缩包没能删除 %s: %v", a.Main, err)
				} else {
					res.Removed += len(a.Parts)
				}
			}
		}
	}
	return res
}

// alreadyUnpacked: the folder an earlier unpack of a made, if it is there.
func alreadyUnpacked(a archiveSet) string {
	parent := filepath.Dir(a.Main)
	names := []string{a.Name}
	if strings.EqualFold(filepath.Ext(a.Main), ".zip") && len(a.Parts) == 1 {
		if zr, err := zip.OpenReader(a.Main); err == nil {
			tops := map[string]bool{}
			for _, f := range zr.File {
				n := strings.SplitN(zipEntryName(f), "/", 2)[0]
				if n != "" && !strings.HasPrefix(n, "__MACOSX") {
					tops[n] = true
				}
			}
			zr.Close()
			if len(tops) == 1 {
				for t := range tops {
					names = append(names, t)
				}
			}
		}
	}
	for _, n := range names {
		p := filepath.Join(parent, core.SafeName(n, 120))
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p
		}
	}
	return ""
}

// RemoveFiles deletes archives this program downloaded itself (they can be downloaded again).
func RemoveFiles(paths []string) error {
	var first error
	for _, p := range paths {
		if err := os.Remove(p); err != nil && first == nil {
			first = err
		}
	}
	return first
}
