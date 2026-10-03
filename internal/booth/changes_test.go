package booth

import (
	"testing"

	"vrclib/internal/core"
)

func TestBoothChanges(t *testing.T) {
	old := &core.BoothInfo{Name: "X", Price: "¥ 1,000", Desc: "a\nb", Images: []string{"1"}, Ver: 4}
	bi := &core.BoothInfo{Name: "X", Price: "¥ 1,200", Desc: "a\nb\nv1.07 更新", Images: []string{"1"}, Ver: 4}
	boothChanges(old, bi)
	if bi.Changed == 0 || bi.ChangeNote != "价格 ¥ 1,000 → ¥ 1,200，商品说明已更改" {
		t.Errorf("%q", bi.ChangeNote)
	}
	same := &core.BoothInfo{Name: "X", Price: "¥ 1,000", Desc: "a  b", Images: []string{"1"}, Ver: 4}
	boothChanges(old, same)
	if same.Changed != 0 {
		t.Error("whitespace-only change flagged")
	}
}
