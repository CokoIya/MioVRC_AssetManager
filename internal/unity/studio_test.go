package unity

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"

	"vrclib"
	"vrclib/internal/core"
)

// noEditors: no Unity on this computer, as far as the program can tell (OpenInUnity then fails instead of starting one).
func noEditors(t *testing.T) {
	t.Setenv("VRCLIB_UNITY_DIRS", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("ProgramFiles", "")
	t.Setenv("ProgramW6432", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	reset := func() {
		EdMu.Lock()
		EdCache = nil
		EdMu.Unlock()
	}
	reset()
	t.Cleanup(reset)
}

func readStudioRequest(t *testing.T, p string) (at int64, lang string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(StudioDataDir(p), "open.json"))
	if err != nil {
		t.Fatalf("no request: %v", err)
	}
	var m struct {
		At   int64  `json:"at"`
		Lang string `json:"lang"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("request %s: %v", b, err)
	}
	return m.At, m.Lang
}

func TestStudio(t *testing.T) {
	noEditors(t)
	p := fakeProject(t, "2022.3.22f1")
	dir := filepath.Join(p, "Packages", StudioPkg)
	want := EmbeddedStudioVersion()
	if want == "" || StudioInstalled(p) {
		t.Fatalf("embedded version %q, installed %v", want, StudioInstalled(p))
	}

	// first time: the package goes in, the request is left, Unity cannot be opened here (that is a note, not an error)
	before := time.Now().Unix()
	note, err := OpenStudio(p, "ja")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(note, "无法打开 Unity") || !strings.HasPrefix(note, "摄影棚插件已安装") {
		t.Errorf("note %q", note)
	}
	for _, f := range []string{"package.json", "package.json.meta", "Editor.meta", "Runtime.meta",
		"Editor/MioVRCA.Studio.Editor.asmdef", "Editor/MioVRCA.Studio.Editor.asmdef.meta",
		"Runtime/MioVRCA.Studio.asmdef", "Runtime/MioVRCA.Studio.asmdef.meta",
		"Editor/StudioLauncher.cs", "Editor/StudioLauncher.cs.meta", "Runtime/Studio.cs", "Runtime/Studio.cs.meta"} {
		if !core.StatOK(filepath.Join(dir, filepath.FromSlash(f))) {
			t.Errorf("missing %s", f)
		}
	}
	// every file and folder has its .meta (Unity would give it a new GUID otherwise, in a read-only package); names
	// that begin with a dot Unity skips (the fingerprint)
	_ = filepath.Walk(dir, func(fp string, fi os.FileInfo, err error) error {
		if err == nil && fp != dir && !strings.HasPrefix(fi.Name(), ".") && !strings.HasSuffix(fp, ".meta") && !core.StatOK(fp+".meta") {
			t.Errorf("no .meta for %s", strings.TrimPrefix(fp, dir))
		}
		return nil
	})
	if core.StatOK(dir+"~") || core.StatOK(dir+".old~") {
		t.Error("temp folder left")
	}
	hashFile := filepath.Join(dir, studioHashFile)
	if b, _ := os.ReadFile(hashFile); strings.TrimSpace(string(b)) != embeddedStudioHash() || len(embeddedStudioHash()) != 64 {
		t.Errorf("fingerprint %q, want %q", b, embeddedStudioHash())
	}
	if !StudioInstalled(p) {
		t.Error("not installed")
	}
	if _, ver, _ := pkgVersion(dir); ver != want {
		t.Errorf("installed %q, want %q", ver, want)
	}
	if at, lang := readStudioRequest(t, p); lang != "ja" || at < before || at > time.Now().Unix()+1 {
		t.Errorf("request at %d lang %q", at, lang)
	}
	if ms, _ := filepath.Glob(filepath.Join(StudioDataDir(p), "*.tmp")); len(ms) > 0 {
		t.Errorf("temp request left: %v", ms)
	}
	st := &core.Store{Projects: []core.ProjectInfo{{Name: "Plum_FT", Path: p}}}
	if cs := ProjectCards(st); len(cs) != 1 || !cs[0].Studio {
		t.Errorf("card %+v", cs)
	}

	// again, the same content: the package stays as it is, the request is new; a language the studio has not is Chinese
	marker := filepath.Join(dir, "marker.txt")
	_ = os.WriteFile(marker, []byte("x"), 0644)
	_ = os.Remove(filepath.Join(StudioDataDir(p), "open.json"))
	if note, err = OpenStudio(p, "fr"); err != nil || !strings.Contains(note, "无法打开 Unity") {
		t.Fatalf("again: %q %v", note, err)
	}
	if !core.StatOK(marker) {
		t.Error("the same content was put in again")
	}
	if _, lang := readStudioRequest(t, p); lang != "zh" {
		t.Errorf("lang %q", lang)
	}

	// the same version with other content (a later release changed a file without a new version, or a copy from
	// before the fingerprint), or with a file gone: put in again
	for _, change := range []func(){
		func() { _ = os.WriteFile(hashFile, []byte("0123\n"), 0644) },
		func() { _ = os.Remove(hashFile) },
		func() { _ = os.RemoveAll(filepath.Join(dir, "Runtime")) },
	} {
		_ = os.WriteFile(marker, []byte("x"), 0644)
		change()
		if _, err = OpenStudio(p, "zh"); err != nil {
			t.Fatal(err)
		}
		if core.StatOK(marker) || !core.StatOK(filepath.Join(dir, "Runtime", "Studio.cs")) || !studioCurrent(dir, want) {
			t.Errorf("changed content not put in again: marker %v", core.StatOK(marker))
		}
	}

	// another version in the project: replaced by ours
	pj := filepath.Join(dir, "package.json")
	b, _ := os.ReadFile(pj)
	_ = os.WriteFile(pj, []byte(strings.Replace(string(b), `"version": "`+want+`"`, `"version": "0.0.1"`, 1)), 0644)
	if _, ver, _ := pkgVersion(dir); ver != "0.0.1" {
		t.Fatalf("could not make it old: %q", ver)
	}
	if _, err = OpenStudio(p, "en"); err != nil {
		t.Fatal(err)
	}
	if _, ver, _ := pkgVersion(dir); ver != want || core.StatOK(marker) {
		t.Errorf("an old version was not replaced: %q, marker %v", ver, core.StatOK(marker))
	}

	// Unity has the project open (a lock file means that only off Windows): nothing is opened, the note says where to look
	if runtime.GOOS != "windows" {
		lock := filepath.Join(p, "Temp", "UnityLockfile")
		_ = os.MkdirAll(filepath.Dir(lock), 0755)
		_ = os.WriteFile(lock, nil, 0644)
		if note, err = OpenStudio(p, "zh"); err != nil || note != "切换到 Unity 窗口即可进入摄影棚" {
			t.Errorf("open, installed: %q %v", note, err)
		}
		_ = os.RemoveAll(dir)
		if note, err = OpenStudio(p, "zh"); err != nil || !strings.HasPrefix(note, "摄影棚插件已放进工程。") {
			t.Errorf("open, new: %q %v", note, err)
		}
		_ = os.Remove(lock)
	}

	// not while the studio is open in Unity (its running file is fresh): nothing goes
	_ = os.WriteFile(filepath.Join(StudioDataDir(p), "prefs.json"), []byte("{}"), 0644)
	_ = os.MkdirAll(filepath.Join(StudioDataDir(p), "poses"), 0755)
	_ = os.WriteFile(filepath.Join(StudioDataDir(p), "poses", "1.json"), []byte("{}"), 0644)
	_ = os.WriteFile(filepath.Join(p, "UserSettings", "EditorUserSettings.asset"), []byte("keep"), 0644)
	running := filepath.Join(StudioDataDir(p), "running")
	_ = os.WriteFile(running, []byte("1700000000"), 0644)
	if err := RemoveStudio(p); err == nil || err.Error() != "摄影棚正在 Unity 中打开，请先退出 Play 模式再移除" {
		t.Errorf("removed while open: %v", err)
	}
	if !StudioInstalled(p) || !core.StatOK(running) || !core.StatOK(filepath.Join(StudioDataDir(p), "prefs.json")) {
		t.Error("a refused removal took something")
	}
	old := time.Now().Add(-time.Minute)
	_ = os.Chtimes(running, old, old) // the studio stopped writing it (Unity closed or crashed)

	// removal: the package, its settings and poses, and the shared folder once empty; the rest of UserSettings stays.
	// What an earlier removal could not delete goes too
	gone := dir + studioDelSuffix
	_ = os.MkdirAll(filepath.Join(gone, "Runtime"), 0755)
	_ = os.WriteFile(filepath.Join(gone, "Runtime", "Studio.cs"), []byte("x"), 0644)
	if err := RemoveStudio(p); err != nil {
		t.Fatal(err)
	}
	if core.StatOK(dir) || core.StatOK(gone) || core.StatOK(StudioDataDir(p)) || core.StatOK(filepath.Join(p, "UserSettings", "MioVRCA")) ||
		!core.StatOK(filepath.Join(p, "UserSettings", "EditorUserSettings.asset")) {
		t.Error("removal: only the studio's own files go")
	}
	if cs := ProjectCards(st); len(cs) != 1 || cs[0].Studio {
		t.Errorf("card after removal %+v", cs)
	}
	// … and what other packages keep in the shared folder stays, as do pictures a studio without a Pictures folder took.
	// An install also clears what a removal left
	_ = os.MkdirAll(gone, 0755)
	if _, err = OpenStudio(p, "zh"); err != nil || core.StatOK(gone) {
		t.Fatalf("%v, removal left %v", err, core.StatOK(gone))
	}
	cover := filepath.Join(p, "UserSettings", "MioVRCA", "cover_1.png")
	photo := filepath.Join(StudioDataDir(p), "photos", "MioVRCA_20260101_120000.png")
	_ = os.WriteFile(cover, []byte("x"), 0644)
	_ = os.MkdirAll(filepath.Dir(photo), 0755)
	_ = os.WriteFile(photo, []byte("x"), 0644)
	if err := RemoveStudio(p); err != nil {
		t.Fatal(err)
	}
	if core.StatOK(dir) || core.StatOK(filepath.Join(StudioDataDir(p), "open.json")) || !core.StatOK(cover) || !core.StatOK(photo) {
		t.Error("removal took what is not the studio's settings")
	}
	if err := RemoveStudio(p); err != nil { // nothing there: fine
		t.Error(err)
	}

	// somebody else's package under that name is neither replaced nor removed
	_ = os.MkdirAll(dir, 0755)
	foreign := []byte(`{"name":"com.miovrc.studio","version":"9.0.0","author":{"name":"someone"}}`)
	_ = os.WriteFile(pj, foreign, 0644)
	if _, err = OpenStudio(p, "zh"); err == nil || !strings.Contains(err.Error(), "不是由本软件安装") {
		t.Errorf("foreign package replaced: %v", err)
	}
	if err = RemoveStudio(p); err == nil {
		t.Error("foreign package removed")
	}
	if b, _ := os.ReadFile(pj); string(b) != string(foreign) || StudioInstalled(p) {
		t.Errorf("foreign package changed: %s", b)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 {
		t.Errorf("foreign package folder: %d entries", len(ents))
	}
}

// Every .meta the program carries has a GUID of its own: Unity keys assets by GUID, across all packages of a project.
func TestUnityHelperGUIDs(t *testing.T) {
	re := regexp.MustCompile(`(?m)^guid: ([0-9a-f]{32})\s*$`)
	seen := map[string]string{}
	n := 0
	_ = fs.WalkDir(vrclib.UnityHelper, "unityhelper", func(fp string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Ext(fp) != ".meta" {
			return nil
		}
		b, _ := vrclib.UnityHelper.ReadFile(fp)
		m := re.FindSubmatch(b)
		if m == nil {
			t.Errorf("%s: no guid", fp)
			return nil
		}
		n++
		if o, dup := seen[string(m[1])]; dup {
			t.Errorf("%s: guid %s is %s's too", fp, m[1], o)
		}
		seen[string(m[1])] = fp
		return nil
	})
	if n < 20 {
		t.Errorf("only %d .meta files", n)
	}
}

// A second 摄影棚 while the Unity started for the first is still opening starts no other Unity (it would only report
// the project as in use); the request is left all the same.
func TestStudioLaunchOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in editor is a shell script")
	}
	noEditors(t)
	starts := filepath.Join(t.TempDir(), "starts")
	hub := t.TempDir()
	exe := filepath.Join(hub, "2022.3.22f1", "Editor", "Unity")
	_ = os.MkdirAll(filepath.Dir(exe), 0755)
	if err := os.WriteFile(exe, []byte("#!/bin/sh\necho \"$2\" >> '"+starts+"'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VRCLIB_UNITY_DIRS", hub)
	p := fakeProject(t, "2022.3.22f1")
	t.Cleanup(func() {
		startMu.Lock()
		delete(startedAt, core.PathKey(p))
		startMu.Unlock()
	})
	// the stand-in runs on its own: wait for its line(s), then a moment for any second one
	read := func() []string {
		b, _ := os.ReadFile(starts)
		return strings.FieldsFunc(string(b), func(r rune) bool { return r == '\n' })
	}
	count := func(n int) int {
		t.Helper()
		for end := time.Now().Add(5 * time.Second); time.Now().Before(end) && len(read()) < n; {
			time.Sleep(20 * time.Millisecond)
		}
		time.Sleep(200 * time.Millisecond)
		lines := read()
		for _, l := range lines {
			if l != p {
				t.Errorf("started for %q", l)
			}
		}
		return len(lines)
	}

	note, err := OpenStudio(p, "zh")
	if err != nil || note != "正在打开 Unity，首次需导入插件并编译（约一至两分钟），之后自动进入摄影棚" {
		t.Fatalf("first: %q %v", note, err)
	}
	if n := count(1); n != 1 {
		t.Fatalf("%d starts", n)
	}
	_ = os.Remove(filepath.Join(StudioDataDir(p), "open.json"))
	if note, err = OpenStudio(p, "en"); err != nil || note != "正在打开 Unity，打开后自动进入摄影棚" {
		t.Errorf("again: %q %v", note, err)
	}
	if _, lang := readStudioRequest(t, p); lang != "en" {
		t.Errorf("lang %q", lang)
	}
	if n := count(1); n != 1 {
		t.Errorf("a second Unity was started: %d starts", n)
	}
	// that Unity opened the project since and is gone again (closed, or crashed): started again at once
	startMu.Lock()
	startedAt[core.PathKey(p)] = time.Now().Add(-10 * time.Second)
	startMu.Unlock()
	_ = os.WriteFile(filepath.Join(p, "Library", "LastSceneManagerSetup.txt"), []byte("x"), 0644)
	if note, err = OpenStudio(p, "zh"); err != nil || note != "正在打开 Unity，打开后自动进入摄影棚" {
		t.Errorf("after a close: %q %v", note, err)
	}
	if n := count(2); n != 2 {
		t.Errorf("not started again after Unity closed: %d starts", n)
	}
	// long enough ago (that Unity never opened the project): started again
	startMu.Lock()
	startedAt[core.PathKey(p)] = time.Now().Add(-unityStartWait - time.Second)
	startMu.Unlock()
	if note, err = OpenStudio(p, "zh"); err != nil || note != "正在打开 Unity，打开后自动进入摄影棚" {
		t.Errorf("later: %q %v", note, err)
	}
	if n := count(3); n != 3 {
		t.Errorf("not started again: %d starts", n)
	}
}

// Every text the studio shows through L.T / L.F has its English and Japanese in Runtime/Lang.cs. The i18n harvest
// skips the package's files, so nothing else notices a missing one (the studio would show it in Chinese). The texts:
// the literals passed to L.T / L.F, and the name arrays the UI passes by index; a call passing anything else would
// hide its texts from this test, so it fails too.
func TestStudioLang(t *testing.T) {
	const root = "unityhelper/" + StudioPkg
	const lit = `"((?:[^"\\]|\\.)*)"`
	call := regexp.MustCompile(`\bL\.([TF])\(\s*`)
	litAt := regexp.MustCompile(`^` + lit)
	// the arrays the UI passes by index: where the call is, how it passes the array, and where the array is
	arrays := []struct{ at, arg, file, name string }{
		{"Runtime/StudioUi.cs", "names[", "Runtime/HandPresets.cs", "Names"}, // names = HandPresets.Names
		{"Runtime/StudioUi.cs", "Backdrop.GradientNames[", "Runtime/Backdrop.cs", "GradientNames"},
	}
	need := map[string]string{} // text → where it is used
	format := map[string]bool{} // passed to L.F (string.Format)
	var lang string
	_ = fs.WalkDir(vrclib.UnityHelper, root, func(fp string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Ext(fp) != ".cs" {
			return err
		}
		b, _ := vrclib.UnityHelper.ReadFile(fp)
		rel, src := strings.TrimPrefix(fp, root+"/"), string(b)
		if rel == "Runtime/Lang.cs" {
			lang = src
			return nil
		}
		for _, m := range call.FindAllStringSubmatchIndex(src, -1) {
			rest, line, fn := src[m[1]:], 1+strings.Count(src[:m[0]], "\n"), src[m[2]:m[3]]
			if l := litAt.FindStringSubmatch(rest); l != nil {
				s := csUnquote(l[1])
				need[s] = fmt.Sprintf("%s:%d", rel, line)
				format[s] = format[s] || fn == "F"
				continue
			}
			known := false
			for _, a := range arrays {
				known = known || rel == a.at && strings.HasPrefix(rest, a.arg)
			}
			if !known {
				t.Errorf("%s:%d: L.%s(%.30s…) passes no literal: add where its texts come from to TestStudioLang", rel, line, fn, rest)
			}
		}
		return nil
	})
	for _, a := range arrays {
		b, _ := vrclib.UnityHelper.ReadFile(root + "/" + a.file)
		m := regexp.MustCompile(`\b` + a.name + `\s*=\s*(?:new\s*(?:string\s*)?\[\s*\]\s*)?\{([^}]*)\}`).FindSubmatch(b)
		if m == nil {
			t.Errorf("%s: no %s array", a.file, a.name)
			continue
		}
		for _, l := range regexp.MustCompile(lit).FindAllSubmatch(m[1], -1) {
			need[csUnquote(string(l[1]))] = a.file + " " + a.name
		}
	}
	if len(need) < 100 || lang == "" {
		t.Fatalf("%d texts found, Lang.cs %d bytes: the package's layout changed", len(need), len(lang))
	}

	placeholders := func(s string) string {
		ms := regexp.MustCompile(`\{(\d+)[,:}]`).FindAllStringSubmatch(s, -1)
		set := map[string]bool{}
		for _, m := range ms {
			set[m[1]] = true
		}
		var out []string
		for k := range set {
			out = append(out, k)
		}
		sort.Strings(out)
		return strings.Join(out, ",")
	}
	var texts []string
	for s := range need {
		if needsTranslation(s) {
			texts = append(texts, s)
		}
	}
	sort.Strings(texts)
	for _, name := range []string{"En", "Ja"} {
		table, dups, ok := csDictionary(lang, name)
		if !ok {
			t.Errorf("Lang.cs: no %s dictionary", name)
			continue
		}
		for _, k := range dups { // a collection initializer adds: a key twice throws, and with it every L.T
			t.Errorf("Lang.cs %s: %q twice", name, k)
		}
		var missing []string
		for _, s := range texts {
			v, has := table[s]
			switch {
			case !has || strings.TrimSpace(v) == "":
				missing = append(missing, fmt.Sprintf("  %q  (%s)", s, need[s]))
			case format[s] && placeholders(v) != placeholders(s): // string.Format throws on an index it has no argument for
				t.Errorf("Lang.cs %s: %q has the placeholders {%s}, its text {%s}", name, v, placeholders(v), placeholders(s))
			}
		}
		if len(missing) > 0 {
			t.Errorf("Runtime/Lang.cs %s lacks %d of the studio's %d texts:\n%s", name, len(missing), len(texts), strings.Join(missing, "\n"))
		}
	}
}

// needsTranslation: the text has Chinese (or Japanese) in it; "OK" or "1:1" read the same in every language.
func needsTranslation(s string) bool {
	for _, r := range s {
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana) || r >= 0x3000 && r <= 0x303f || r >= 0xff00 && r <= 0xffef {
			return true
		}
	}
	return false
}

// csUnquote: the text of a C# regular string literal (without its quotes).
func csUnquote(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch c := s[i]; c {
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case '0':
			b.WriteByte(0)
		case 'u':
			if v, err := strconv.ParseUint(s[i+1:min(i+5, len(s))], 16, 32); err == nil && i+5 <= len(s) {
				b.WriteRune(rune(v))
				i += 4
			} else {
				b.WriteString(`\u`)
			}
		default: // \" \' \\
			b.WriteByte(c)
		}
	}
	return b.String()
}

// csDictionary reads `<name> = new Dictionary<string, string> { { "k", "v" }, … }` (or `["k"] = "v"` entries) from C#
// source: the entries, and the keys given twice in the {k, v} form.
func csDictionary(src, name string) (table map[string]string, dups []string, ok bool) {
	at := regexp.MustCompile(`\b` + name + `\s*=\s*new\b`).FindStringIndex(src)
	if at == nil {
		return nil, nil, false
	}
	open := strings.IndexByte(src[at[1]:], '{')
	if open < 0 {
		return nil, nil, false
	}
	// the body up to its closing brace, past string literals, without comments (an entry commented out is none)
	var body strings.Builder
	depth, end := 0, false
	for i := at[1] + open; i < len(src) && !end; i++ {
		switch {
		case src[i] == '"':
			j := i + 1
			for ; j < len(src) && src[j] != '"'; j++ {
				if src[j] == '\\' {
					j++
				}
			}
			body.WriteString(src[i:min(j+1, len(src))])
			i = j
			continue
		case strings.HasPrefix(src[i:], "//"):
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case strings.HasPrefix(src[i:], "/*"):
			if j := strings.Index(src[i+2:], "*/"); j >= 0 {
				i += j + 3
			} else {
				i = len(src)
			}
			continue
		case src[i] == '{':
			depth++
		case src[i] == '}':
			depth--
			end = depth == 0
		}
		if i < len(src) {
			body.WriteByte(src[i])
		}
	}
	if !end {
		return nil, nil, false
	}
	const lit = `"((?:[^"\\]|\\.)*)"`
	table = map[string]string{}
	for _, m := range regexp.MustCompile(`\{\s*`+lit+`\s*,\s*`+lit+`\s*\}`).FindAllStringSubmatch(body.String(), -1) {
		k := csUnquote(m[1])
		if _, dup := table[k]; dup {
			dups = append(dups, k)
		}
		table[k] = csUnquote(m[2])
	}
	for _, m := range regexp.MustCompile(`\[\s*`+lit+`\s*\]\s*=\s*`+lit).FindAllStringSubmatch(body.String(), -1) {
		table[csUnquote(m[1])] = csUnquote(m[2])
	}
	return table, dups, true
}
