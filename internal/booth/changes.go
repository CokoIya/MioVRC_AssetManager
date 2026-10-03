package booth

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"vrclib/internal/core"
)

func descHash(s string) string {
	h := sha1.Sum([]byte(strings.Join(strings.Fields(s), " ")))
	return hex.EncodeToString(h[:8])
}

// boothChanges compares a fresh fetch with the stored one and notes what the shop changed.
func boothChanges(old, bi *core.BoothInfo) {
	if old == nil {
		return
	}
	bi.Changed, bi.ChangeNote = old.Changed, old.ChangeNote // keep an earlier, unseen notice
	if old.Name == "" || old.Ver != bi.Ver || bi.Name == "" {
		return // fetched differently: nothing to compare
	}
	var notes []string
	if old.Name != bi.Name {
		notes = append(notes, "商品名称已更改")
	}
	if old.Price != bi.Price && old.Price != "" && bi.Price != "" {
		notes = append(notes, fmt.Sprintf("价格 %s → %s", old.Price, bi.Price))
	}
	if descHash(old.Desc) != descHash(bi.Desc) {
		notes = append(notes, "商品说明已更改")
	}
	if len(old.Images) != len(bi.Images) {
		notes = append(notes, "商品图片已更改")
	}
	if len(notes) > 0 {
		bi.Changed, bi.ChangeNote = time.Now().Unix(), strings.Join(notes, "，")
	}
}
