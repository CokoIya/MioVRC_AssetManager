package unity

import (
	"path/filepath"
	"testing"
	"time"

	"vrclib/internal/core"
)

// What the player hears of an import: the choice it waits for, always; its end after a long import, not after a
// short one nor one the player cancelled; nothing of a split, nor of the end of an import that is part of a bigger
// piece of work (a new project's, an update's: that work's own end is told) — but the choice it waits for, yes.
func TestImportNotice(t *testing.T) {
	proj := filepath.Join(t.TempDir(), "Manuka")
	long := int64(core.NoticeLongEnough / time.Second)
	for _, c := range []struct {
		name string
		j    ImportJob
		age  int64
		want core.Notice
	}{
		{"choose", ImportJob{Stage: "choose", Project: proj}, 1, core.Notice{Kind: "ask", What: "import", Name: "Manuka"}},
		{"done, long", ImportJob{Stage: "done", Project: proj}, long, core.Notice{Kind: "done", What: "import", Name: "Manuka"}},
		{"failed, long", ImportJob{Stage: "failed", Project: proj, Msg: "解压失败"}, long + 30, core.Notice{Kind: "fail", What: "import", Name: "Manuka"}},
		{"done, short", ImportJob{Stage: "done", Project: proj}, long - 1, core.Notice{}},
		{"cancelled", ImportJob{Stage: "failed", Project: proj, Msg: "已取消"}, long + 60, core.Notice{}},
		{"under way", ImportJob{Stage: "import", Project: proj}, long + 60, core.Notice{}},
		{"split", ImportJob{Stage: "done", Split: true}, long + 60, core.Notice{}},
		{"a new project's, done", ImportJob{Stage: "done", Project: proj, quiet: true}, long + 60, core.Notice{}},
		{"a new project's, choose", ImportJob{Stage: "choose", Project: proj, quiet: true}, 1, core.Notice{Kind: "ask", What: "import", Name: "Manuka"}},
	} {
		j := c.j
		j.At = 1000
		if got := importNotice(&j, 1000+c.age); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}
