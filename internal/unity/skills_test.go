package unity

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vrclib/internal/core"
)

// UnitySkills in a new project: by its README's link when this computer's git reaches GitHub (Unity fetches
// it), else the copy the program carries — with the reason in the note.
func TestInstallSkills(t *testing.T) {
	old := gitReaches
	t.Cleanup(func() { gitReaches = old })
	newProj := func() string {
		p := t.TempDir()
		_ = os.MkdirAll(filepath.Join(p, "Packages"), 0755)
		if err := writeUnityManifest(p); err != nil {
			t.Fatal(err)
		}
		return p
	}

	gitReaches = func(ctx context.Context, repo string) (bool, string) {
		if repo != "https://github.com/Besty0728/Unity-Skills.git" {
			t.Errorf("asked of %s", repo)
		}
		return true, ""
	}
	p := newProj()
	n := installSkills(context.Background(), p)
	var m struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	b, _ := os.ReadFile(filepath.Join(p, "Packages", "manifest.json"))
	if err := json.Unmarshal(b, &m); err != nil || m.Dependencies[SkillsPkg] != "https://github.com/Besty0728/Unity-Skills.git?path=/SkillsForUnity" || m.Dependencies["com.unity.modules.animation"] != "1.0.0" {
		t.Errorf("manifest: %s", b)
	}
	if !strings.Contains(n, "通过 Git 安装") || core.StatOK(filepath.Join(p, "Packages", SkillsPkg)) {
		t.Errorf("by git: %q", n)
	}
	if kind, ver := skillsInProject(p); kind != "own" || ver != "Git" {
		t.Errorf("before Unity fetched it: %s %s", kind, ver)
	}
	cache := filepath.Join(p, "Library", "PackageCache", SkillsPkg+"@1a2b3c")
	_ = os.MkdirAll(cache, 0755)
	_ = os.WriteFile(filepath.Join(cache, "package.json"), []byte(`{"name":"com.besty.unity-skills","version":"2.9.0"}`), 0644)
	if kind, ver := skillsInProject(p); kind != "own" || ver != "2.9.0" {
		t.Errorf("fetched: %s %s", kind, ver)
	}

	for _, why := range []string{"本机未安装 Git", "Git 无法连接 GitHub"} {
		gitReaches = func(context.Context, string) (bool, string) { return false, why }
		p = newProj()
		n = installSkills(context.Background(), p)
		if n != "UnitySkills："+why+"，已安装软件自带的 "+SkillsVersion {
			t.Errorf("without git: %q", n)
		}
		if kind, ver := skillsInProject(p); kind != "ours" || ver != SkillsVersion {
			t.Errorf("the program's copy: %s %s", kind, ver)
		}
		b, _ = os.ReadFile(filepath.Join(p, "Packages", "manifest.json"))
		if strings.Contains(string(b), SkillsPkg) {
			t.Errorf("a link Unity cannot fetch went into the manifest: %s", b)
		}
	}
}

// The new-project window offers it, ticked; the VPM repositories are never asked for it.
func TestSkillsOffered(t *testing.T) {
	found := false
	for _, p := range gitPlugins {
		if p.Pkg == SkillsPkg && p.Default && p.Git {
			found = true
		}
	}
	for _, p := range basePlugins {
		if p.Pkg == SkillsPkg {
			t.Error("UnitySkills among the VPM plugins")
		}
	}
	if !found || !isGitPlugin(SkillsPkg) || isGitPlugin("nadena.dev.modular-avatar") {
		t.Error("UnitySkills is not offered as a ticked git plugin")
	}
}
