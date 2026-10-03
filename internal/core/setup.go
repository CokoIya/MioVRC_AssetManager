package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Candidate is a folder suggested on the first-run screen.
type Candidate struct {
	Path    string `json:"path"`
	Note    string `json:"note"`
	Checked bool   `json:"checked"`
}

func IsDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// ResolveDataDir: next to the exe for a portable copy (or an existing library), otherwise the
// per-user app-data folder (installed copies, read-only folders).
func ResolveDataDir(exeDir string) string {
	if FileExists(filepath.Join(exeDir, "library.json")) || FileExists(filepath.Join(exeDir, "portable.txt")) {
		return exeDir
	}
	if ud := userDataBase(); ud != "" && (IsInstalledDir(exeDir) || !DirWritable(exeDir)) {
		return AppDataFolder(ud)
	}
	return exeDir
}

const (
	AppName    = "MioVRCA"             // shown to people: window title, shortcuts
	AppID      = "MioVRC_AssetManager" // file names, folders, update downloads (unchanged by the renames)
	legacyName = "VRC素材库"              // before 1.6: data folder, window title, exe name
)

// FormerNames: what the window and the shortcuts were called before, newest first. A window of an older
// version is still found by them, and their shortcuts make way for the new one.
//
//	MioVRCA素材托管Tools  builds between 1.7.1 and 1.7.2
//	MioVRC素材托管工具     1.6 to 1.7.1
//	VRC素材库            before 1.6
var FormerNames = []string{"MioVRCA素材托管Tools", "MioVRC素材托管工具", legacyName}

// AppDataFolder: the per-user folder under base (%LOCALAPPDATA%); a library kept there by a version
// from before the rename stays where it is.
func AppDataFolder(base string) string {
	if old := filepath.Join(base, legacyName); FileExists(filepath.Join(old, "library.json")) && !FileExists(filepath.Join(base, AppID, "library.json")) {
		return old
	}
	return filepath.Join(base, AppID)
}

func DirWritable(d string) bool {
	f, err := os.CreateTemp(d, ".write-test-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	_ = os.Remove(name)
	return true
}

func UnderDir(p, dir string) bool {
	if dir == "" {
		return false
	}
	lp := strings.ToLower(filepath.Clean(p))
	ld := strings.ToLower(filepath.Clean(dir))
	return lp == ld || strings.HasPrefix(lp, ld+string(os.PathSeparator))
}

// DetectCandidates looks for the usual places VRChat creators keep downloads and Unity projects.
func DetectCandidates() (roots, projects []Candidate) {
	seen := map[string]bool{}
	add := func(list *[]Candidate, p, note string, checked bool) {
		p = filepath.Clean(p)
		k := strings.ToLower(p)
		if seen[k] || !IsDir(p) {
			return
		}
		seen[k] = true
		*list = append(*list, Candidate{Path: p, Note: note, Checked: checked})
	}
	for _, d := range driveRoots() {
		add(&roots, filepath.Join(d, "BaiduNetdiskDownload"), "百度网盘下载", true)
		for _, n := range []string{"VRChat素材", "VRC素材", "素材", "Booth", "BOOTH"} {
			add(&roots, filepath.Join(d, n), "", true)
		}
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		add(&roots, filepath.Join(home, "Downloads", "BaiduNetdiskDownload"), "百度网盘下载", true)
		add(&roots, filepath.Join(home, "Documents", "BaiduNetdiskDownload"), "百度网盘下载", true)
		add(&roots, filepath.Join(home, "Downloads"), "下载文件夹", false)
	}
	for _, p := range filepath.SplitList(os.Getenv("VRCLIB_DETECT_ROOTS")) { // tests
		add(&roots, p, "", true)
	}

	// Unity projects known to Unity Hub / VCC / ALCOM
	var projs []string
	for _, p := range knownProjectPaths() {
		if IsUnityProject(p) {
			projs = append(projs, filepath.Clean(p))
		}
	}
	for _, d := range defaultProjectDirs() {
		if IsDir(d) {
			if ents, err := os.ReadDir(d); err == nil {
				for _, e := range ents {
					if e.IsDir() && IsUnityProject(filepath.Join(d, e.Name())) {
						projs = append(projs, filepath.Join(d, e.Name()))
					}
				}
			}
		}
	}
	for _, p := range filepath.SplitList(os.Getenv("VRCLIB_DETECT_PROJECTS")) { // tests
		projs = append(projs, p)
	}
	// a folder holding several projects is suggested instead of each project (new projects there get picked up too)
	byParent := map[string][]string{}
	for _, p := range projs {
		byParent[filepath.Dir(p)] = append(byParent[filepath.Dir(p)], p)
	}
	parents := make([]string, 0, len(byParent))
	for k := range byParent {
		parents = append(parents, k)
	}
	sort.Strings(parents)
	for _, par := range parents {
		list := UniqStrings(byParent[par])
		if len(list) >= 2 {
			add(&projects, par, Itoa(len(list))+" 个工程", true)
			continue
		}
		for _, p := range list {
			add(&projects, p, "Unity 工程", true)
		}
	}
	return roots, projects
}

func Itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// knownProjectPaths reads the project lists of Unity Hub and VRChat Creator Companion / ALCOM.
func knownProjectPaths() []string {
	var out []string
	if ad := os.Getenv("APPDATA"); ad != "" {
		if b, err := os.ReadFile(filepath.Join(ad, "UnityHub", "projects-v1.json")); err == nil {
			var v struct {
				Data map[string]struct {
					Path string `json:"path"`
				} `json:"data"`
			}
			if json.Unmarshal(b, &v) == nil {
				for k, p := range v.Data {
					if p.Path != "" {
						out = append(out, filepath.FromSlash(p.Path))
					} else {
						out = append(out, filepath.FromSlash(k))
					}
				}
			}
		}
	}
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		for _, f := range []string{"settings.json", "vrc-get-settings.json"} {
			if b, err := os.ReadFile(filepath.Join(la, "VRChatCreatorCompanion", f)); err == nil {
				var v struct {
					UserProjects       []string `json:"userProjects"`
					DefaultProjectPath string   `json:"defaultProjectPath"`
				}
				if json.Unmarshal(b, &v) == nil {
					out = append(out, v.UserProjects...)
					if v.DefaultProjectPath != "" {
						if ents, err := os.ReadDir(v.DefaultProjectPath); err == nil {
							for _, e := range ents {
								if e.IsDir() {
									out = append(out, filepath.Join(v.DefaultProjectPath, e.Name()))
								}
							}
						}
					}
				}
			}
		}
	}
	return out
}

func defaultProjectDirs() []string {
	var out []string
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		out = append(out, filepath.Join(la, "VRChatProjects"))
	}
	if home, _ := os.UserHomeDir(); home != "" {
		out = append(out, filepath.Join(home, "VRChatProjects"), filepath.Join(home, "ALCOM"), filepath.Join(home, "Documents", "VRChatProjects"))
	}
	return out
}
