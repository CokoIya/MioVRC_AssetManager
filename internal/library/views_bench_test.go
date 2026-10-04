package library

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"vrclib/internal/core"
)

// bigStore: a library of n outfits in folders, a tenth of them bought on Booth with the shop's page read.
func bigStore(tb testing.TB, n int) *core.Store {
	tb.Helper()
	core.DataDir = tb.TempDir()
	st := core.LoadStore(filepath.Join(core.DataDir, "library.json"))
	bodies := []string{"Kaguya", "Plum", "Manuka", "Shinano", "Selestia"}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("Outfit Series %d for %s ver1.%d", i/3, bodies[i%5], i%7)
		a := &core.Asset{Key: fmt.Sprintf("name:outfit%d", i), Name: name, RawName: name, Category: "衣服",
			Locations: []core.Location{{Path: `D:\assets\` + name, Kind: "dir", Root: `D:\assets`}},
			Covers:    []string{`D:\assets\` + name + `\thumb.png`}, MetaDirs: make([]string, 20), Packages: []string{`D:\assets\` + name + `\a.unitypackage`}}
		if i%10 == 0 {
			id := fmt.Sprint(5000000 + i)
			a.BoothID = id
			st.Booth[id] = &core.BoothInfo{ID: id, Name: "【8アバター対応】" + name, Shop: "Shop " + fmt.Sprint(i%40), Category: "3D衣装", Tags: []string{"VRChat", "制服", bodies[i%5]}}
			st.Purchases[id] = &core.Purchase{ID: id, Name: name, Files: []string{name + ".zip"}, Downloads: []string{fmt.Sprint(i)}}
		}
		st.Assets = append(st.Assets, a)
	}
	return st
}

// forgetNames: as after a start, nothing worked out from the names is remembered.
func forgetNames() {
	queryMemo, zhMemo, stemMemo, groupMemo, restMemo = nameMemo[string]{}, nameMemo[string]{}, nameMemo[string]{}, nameMemo[string]{}, nameMemo[string]{}
	forMemo, styleMemo, plainMemo = nameMemo[[]string]{}, nameMemo[[]string]{}, nameMemo[bool]{}
}

// The cards are the same whether what is worked out from the names was remembered or not, also after the
// tables the answers depend on (base bodies, style tags) were changed in the settings.
func TestViewsRememberedOrNot(t *testing.T) {
	st := bigStore(t, 300)
	st.Assets = append(st.Assets,
		&core.Asset{Key: "name:exoframemilfy", Name: "EXO_FRAME_For_Milfy_v1.01", RawName: "EXO_FRAME_For_Milfy_v1.01", Category: "衣服"},
		&core.Asset{Key: "name:exoframerurune", Name: "EXO_FRAME_For_Rurune_v1.01", RawName: "EXO_FRAME_For_Rurune_v1.01", Category: "衣服"},
		&core.Asset{Key: "name:123456", Name: "5000000", RawName: "5000000", Category: "其他", BoothID: "5000000"},
		&core.Asset{Key: "name:maidpsd", Name: "Maid Set PSD", RawName: "Maid Set PSD", Category: "材质"})
	cards := func() string {
		st.Mu.RLock()
		defer st.Mu.RUnlock()
		b, err := json.Marshal(AllViews(st))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	forgetNames()
	first := cards()
	if again := cards(); again != first {
		t.Fatal("the second time the cards are not the same")
	}
	st.Settings.Bases = append([]string{"Milfy=milfy", "Outfit=outfit"}, st.Settings.Bases[3:]...)
	st.Settings.Styles = []string{"Tech=exo|frame", "女仆=maid"}
	changed := cards()
	forgetNames()
	if fresh := cards(); changed != fresh || changed == first {
		t.Errorf("after the settings changed: same as worked out anew %v, same as before %v", changed == fresh, changed == first)
	}
}

// What the window asks for after every change: all cards, built while the store is held.
func BenchmarkAllViews(b *testing.B) {
	for _, n := range []int{2000, 10000} {
		st := bigStore(b, n)
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			AllViews(st) // the first time after a start costs more: what is worked out from the names is kept
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				st.Mu.RLock()
				v := AllViews(st)
				st.Mu.RUnlock()
				if len(v) != n {
					b.Fatal(len(v))
				}
			}
		})
	}
}

// The first time after a start, with nothing remembered yet.
func BenchmarkAllViewsFirst(b *testing.B) {
	st := bigStore(b, 10000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		forgetNames()
		AllViews(st)
	}
}

func BenchmarkStateJSON(b *testing.B) {
	st := bigStore(b, 10000)
	v := AllViews(st)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := json.Marshal(v); err != nil {
			b.Fatal(err)
		}
	}
}
