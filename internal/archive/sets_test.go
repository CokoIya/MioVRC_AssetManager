package archive

import (
	"path/filepath"
	"strings"
	"testing"

	"vrclib/internal/testkit"
)

func TestParse7zList(t *testing.T) {
	out := "Path = Dress\r\nFolder = +\r\nSize = 0\r\nAttributes = D\r\n\r\n" +
		"Path = Dress\\a.unitypackage\r\nFolder = -\r\nSize = 12345\r\nPacked Size = 999\r\nAttributes = A\r\n\r\n" +
		"Path = Dress\\tex\\名前 = x.png\r\nSize = 7\r\nAttributes = A\r\n"
	got := parse7zList(out)
	if len(got) != 2 || got[0].Name != "Dress/a.unitypackage" || got[0].Size != 12345 || got[1].Name != "Dress/tex/名前 = x.png" || got[1].Size != 7 {
		t.Errorf("got %+v", got)
	}
	// without -ba the archive itself comes first: it is no entry
	if got := parse7zList("Path = C:\\x\\Dress.7z\nType = 7z\nPhysical Size = 5\n\n----------\nPath = a.txt\nSize = 3\n"); len(got) != 1 || got[0].Name != "a.txt" {
		t.Errorf("with the archive's own block: %+v", got)
	}
}

func TestSetsAndEntries(t *testing.T) {
	dir := t.TempDir()
	z := filepath.Join(dir, "Dress.zip")
	testkit.MakeZip(t, z, map[string]string{"Dress/a.unitypackage": "12345", "Dress/sub/": "", "Dress/sub/b.png": "123"})
	for _, n := range []string{"Vol.part1.rar", "Vol.part2.rar", "Lone.part2.rar", "Split.zip.001", "Split.zip.002"} {
		testkit.WriteFile(t, filepath.Join(dir, n), 10)
	}
	var files []string
	for _, n := range []string{"Vol.part2.rar", "Dress.zip", "Split.zip.002", "Vol.part1.rar", "Lone.part2.rar", "Split.zip.001"} {
		files = append(files, filepath.Join(dir, n))
	}
	sets := Sets(files)
	var names []string
	for _, s := range sets {
		names = append(names, s.Name+":"+filepath.Base(s.Main)+":"+strings.Repeat("p", len(s.Parts)))
	}
	if strings.Join(names, " ") != "Dress:Dress.zip:p Split.zip:Split.zip.001:pp Vol:Vol.part1.rar:pp" && strings.Join(names, " ") != "Dress:Dress.zip:p Split:Split.zip.001:pp Vol:Vol.part1.rar:pp" {
		t.Errorf("sets: %v", names)
	}
	if files[0] != filepath.Join(dir, "Vol.part2.rar") {
		t.Error("the caller's list was reordered")
	}
	es, err := ListEntries(sets[0])
	if err != nil || len(es) != 2 || es[0].Name != "Dress/a.unitypackage" || es[0].Size != 5 || es[1].Name != "Dress/sub/b.png" {
		t.Errorf("entries: %+v %v", es, err)
	}
}
