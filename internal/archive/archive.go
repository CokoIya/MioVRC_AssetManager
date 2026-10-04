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
	"strconv"
	"strings"
	"time"

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
	errNoTool  = errors.New("本机未检测到解压软件（7-Zip、Bandizip、WinRAR 等），无法解压 rar、7z、分卷和带密码的压缩包")
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

// volKey: which archive of its folder a file is part of, by its name alone — the name the set goes by, the
// scheme its volumes follow, and whether this file is the one that opens the set. Only volumes of one scheme
// belong together: "x.zip" with "x.z01"…, "x.rar" with "x.r00"…, "x.part1.rar" with "x.part2.rar"…,
// "x.7z.001" with "x.7z.002"…. "x.zip" and "x.7z" are two archives that happen to share a name. kind "": no
// archive file.
func volKey(base string) (stem, kind string, first bool) {
	if m := reVolNum.FindStringSubmatch(base); m != nil {
		return m[1], "num." + strings.ToLower(m[2]), m[3] == "001"
	}
	if m := reVolPart.FindStringSubmatch(base); m != nil {
		return m[1], "part", strings.TrimLeft(m[2], "0") == "1"
	}
	if m := reVolZnn.FindStringSubmatch(base); m != nil {
		return m[1], "zip", false
	}
	if m := reVolRnn.FindStringSubmatch(base); m != nil {
		return m[1], "rar", false
	}
	if reArchive.MatchString(base) {
		ext := filepath.Ext(base)
		return strings.TrimSuffix(base, ext), strings.ToLower(ext[1:]), true
	}
	return "", "", false
}

// VolumeName splits a file's name where a set's name ends and what makes the file a volume of it begins:
// "Dress.part1.rar" → "Dress", ".part1.rar"; "Dress.zip.001" → "Dress", ".zip.001"; any other file at its
// extension. key is the same for all volumes of one set in a folder, and for nothing else there.
func VolumeName(name string) (stem, rest, key string) {
	stem, kind, _ := volKey(name)
	if kind == "" {
		stem = strings.TrimSuffix(name, filepath.Ext(name))
		return stem, name[len(stem):], strings.ToLower(name) + "|"
	}
	return stem, name[len(stem):], strings.ToLower(stem) + "|" + kind
}

// groupArchives sorts the archive files in one folder into archives (volumes together).
func groupArchives(files []string) []archiveSet {
	type key struct{ dir, name, kind string }
	sets := map[key]*archiveSet{}
	var order []key
	sort.Strings(files)
	for _, f := range files {
		stem, kind, first := volKey(filepath.Base(f))
		if kind == "" {
			continue
		}
		k := key{filepath.Dir(f), strings.ToLower(stem), kind}
		if s := sets[k]; s != nil && first && s.Main != "" {
			k.name = f // a second archive under that name (the two differ in case only): an archive of its own
		}
		s := sets[k]
		if s == nil {
			s = &archiveSet{Name: stem}
			sets[k] = s
			order = append(order, k)
		}
		s.Parts = append(s.Parts, f)
		if first {
			s.Main = f
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

// readParts: the files of a that unpacking a.Main reads — a.Main itself and the volumes that follow it under
// its own scheme, in its folder. Whatever else a set was handed is left out: only these may be removed once
// the unpack went well.
func readParts(a archiveSet) []string {
	out := []string{a.Main}
	stem, kind, _ := volKey(filepath.Base(a.Main))
	if kind == "" {
		return out
	}
	dir := core.PathKey(filepath.Dir(a.Main))
	for _, p := range a.Parts {
		s, k, first := volKey(filepath.Base(p))
		if p != a.Main && !first && k == kind && strings.EqualFold(s, stem) && core.PathKey(filepath.Dir(p)) == dir {
			out = append(out, p)
		}
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
		// a single archive file: with its sibling volumes, and nothing else that shares its name
		ents, _ := os.ReadDir(filepath.Dir(dir))
		var files []string
		name := strings.ToLower(archiveName(dir))
		for _, e := range ents {
			p := filepath.Join(filepath.Dir(dir), e.Name())
			if !e.IsDir() && IsArchiveFile(p) && strings.ToLower(archiveName(p)) == name {
				files = append(files, p)
			}
		}
		for _, s := range groupArchives(files) {
			for _, p := range s.Parts {
				if core.PathKey(p) == core.PathKey(dir) {
					return []archiveSet{s}
				}
			}
		}
		return nil
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

// zipNeedsTool: zips the built-in reader cannot do (encrypted entries, other compression methods), and
// the code page their names are in, for the program that unpacks them (0: nothing to tell it).
func zipNeedsTool(p string) (needs bool, cp uint32) {
	zr, err := zip.OpenReader(p)
	if err != nil {
		return true, 0
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Flags&0x1 != 0 || (f.Method != zip.Store && f.Method != zip.Deflate) {
			needs = true
		}
	}
	cp, _ = zipCodePage(zr.File)
	return needs, cp
}

// extractArchive unpacks a into its folder (a single folder inside is kept as it is, anything else
// goes into a folder named after the archive) and returns the folder that was made.
func extractArchive(a archiveSet, pwd string) (string, error) {
	parent := filepath.Dir(a.Main)
	single := len(a.Parts) == 1 && strings.EqualFold(filepath.Ext(a.Main), ".zip")
	cp := uint32(0)
	if single {
		var needs bool
		if needs, cp = zipNeedsTool(a.Main); !needs {
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
			return "", fmt.Errorf("解压出错：无法创建文件夹（%v）", err)
		}
		err := runArcTool(t, a.Main, tmp, pwd, cp)
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

// arcToolArgs: the command line that unpacks archive into out. cp: the code page of a zip's file names
// when they do not say it themselves (0 = nothing to tell the program).
func arcToolArgs(t arcTool, archive, out, pwd string, cp uint32) []string {
	var args []string
	switch t.Kind {
	case "7z":
		// -p always: never wait for a typed password. -aou: two entries under one name both stay (the
		// second gets a number), instead of the second replacing the first
		args = []string{"x", "-y", "-aou", "-bd", "-o" + out, "-p" + pwd}
		if cp != 0 {
			args = append(args, "-mcp="+strconv.FormatUint(uint64(cp), 10))
		}
		args = append(args, "--", archive)
	case "bz":
		args = []string{"x", "-y", "-o:" + out}
		if pwd != "" {
			args = append(args, "-p:"+pwd)
		}
		if cp != 0 {
			args = append(args, "-cp:"+strconv.FormatUint(uint64(cp), 10))
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
	return args
}

func runArcTool(t arcTool, archive, out, pwd string, cp uint32) error {
	args := arcToolArgs(t, archive, out, pwd, cp)
	// a program that stops to ask something would wait forever in its hidden window
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()
	cmd := core.Hidden(exec.CommandContext(ctx, t.exe, args...))
	b, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		core.Logf("解压超时（%s）%s", t.Name, archive)
		return fmt.Errorf("%s 解压超时", t.Name)
	}
	if err == nil {
		if ents, _ := os.ReadDir(out); len(ents) == 0 {
			return errors.New("解压结果为空")
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
		return "", fmt.Errorf("解压出错：无法读取解压出的文件（%v）", err)
	}
	src, target := tmp, filepath.Join(parent, core.SafeName(name, 120))
	if len(ents) == 1 && ents[0].IsDir() {
		src = filepath.Join(tmp, ents[0].Name())
		target = filepath.Join(parent, core.SafeName(ents[0].Name(), 120))
	}
	target = UniquePath(target)
	if err := os.Rename(src, target); err != nil {
		_ = os.RemoveAll(tmp)
		return "", fmt.Errorf("解压出错：无法将解压出的文件夹移至 %s（%v）", target, err)
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

type unpackResult struct {
	Done    []string // folders made
	Failed  map[string]string
	Removed int
}

// UnpackAll unpacks every archive under the given places, then what those contained (an archive in
// an archive, PSD packs inside the main zip …), up to three levels. Archives that unpacked fine are
// handed to remove: the Recycle Bin for the player's own files, nil keeps them. report is called before
// each archive. Every archive under a folder given here is taken: for what a download brought into a
// folder that may hold other things, UnpackFiles is the one to use.
func UnpackAll(places []string, pwd string, remove func([]string) error, report func(name string, i, n int)) unpackResult {
	return unpack(places, pwd, remove, report, false)
}

// UnpackFiles unpacks the given archive files — each with the volumes of its own set — and then what they
// contained, as UnpackAll does. Nothing else is touched: no other archive in their folders, and no folder
// that was there before. For the files a download just brought: only they are unpacked, and only they are
// handed to remove (deletion, for what this program downloaded itself and can download again).
func UnpackFiles(files []string, pwd string, remove func([]string) error, report func(name string, i, n int)) unpackResult {
	return unpack(files, pwd, remove, report, true)
}

func unpack(places []string, pwd string, remove func([]string) error, report func(name string, i, n int), own bool) unpackResult {
	res := unpackResult{Failed: map[string]string{}}
	seen := map[string]bool{}
	made := map[string]bool{} // the folders this run unpacked into
	fresh := func(dir string) bool {
		for m := range made {
			if core.UnderDir(dir, m) {
				return true
			}
		}
		return false
	}
	queue := places
	for level := 0; level < 3 && len(queue) > 0; level++ {
		var sets []archiveSet
		for _, p := range queue {
			if own && level == 0 {
				if st, err := os.Stat(p); err != nil || st.IsDir() {
					continue // files only: a folder is never looked through for what else it holds
				}
			}
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
			// unpacked before (by hand): leave the archive alone. A folder this run made is another archive's
			// ("Dress.7z" next to "Dress.zip"): this one is unpacked next to it
			if dir := alreadyUnpacked(a); dir != "" && !made[core.PathKey(dir)] {
				if !own || fresh(dir) {
					queue = append(queue, dir)
				}
				continue
			}
			out, err := extractArchive(a, pwd)
			if err != nil {
				res.Failed[a.Main] = err.Error()
				continue
			}
			res.Done = append(res.Done, out)
			made[core.PathKey(out)] = true
			queue = append(queue, out)
			if remove != nil {
				parts := readParts(a) // never a file the unpack did not read
				if err := remove(parts); err != nil {
					core.Logf("压缩包没能删除 %s: %v", a.Main, err)
				} else {
					res.Removed += len(parts)
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
			entries, _ := zipNames(zr.File) // names that cannot be read: only the archive's own name to go by
			for _, e := range entries {
				n := strings.SplitN(e, "/", 2)[0]
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

// RemoveFiles deletes archives this program downloaded itself (they can be downloaded again). Only for
// UnpackFiles: with UnpackAll it would delete whatever else lies in the folder.
func RemoveFiles(paths []string) error {
	var first error
	for _, p := range paths {
		if err := os.Remove(p); err != nil && first == nil {
			first = err
		}
	}
	return first
}
