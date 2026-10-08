package pandl

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"vrclib/internal/testkit"
)

// A folder the player saved into the netdisk with Baidu's own app: listed folders first, downloaded into a
// folder of its name, unpacked and split into cards as a share's download is. Nothing is saved into the
// netdisk for it.
func TestDiskDownloadFolder(t *testing.T) {
	f, st := newFakeBaidu(t)
	st.Settings.NoExtract = false
	f.disk["/我的资源"] = &bdNode{dir: true}
	f.disk["/我的资源/衣服合集"] = &bdNode{dir: true}
	f.disk["/我的资源/衣服合集/2.zip"] = &bdNode{data: collectionZip(t, "Sailor Dress", "Twintail", "Cat Ears"), mtime: 10}
	f.disk["/我的资源/b.txt"] = &bdNode{data: []byte("x"), mtime: 10}
	f.disk["/我的资源/Apps"] = &bdNode{dir: true}

	ents, dir, err := ListDisk(st, " 我的资源/ ")
	if err != nil || dir != "/我的资源" {
		t.Fatalf("listing: %q %v", dir, err)
	}
	var got []string
	for _, e := range ents {
		got = append(got, e.Name+map[bool]string{true: "/", false: ""}[e.Dir]+" "+e.Path)
	}
	if strings.Join(got, ", ") != "Apps/ /我的资源/Apps, 衣服合集/ /我的资源/衣服合集, b.txt /我的资源/b.txt" {
		t.Errorf("listed: %v", got)
	}
	if _, _, err := ListDisk(st, "/nothing"); err == nil || err.Error() != "网盘中没有这个文件夹（可能已被移动或删除）" {
		t.Errorf("a folder that is not there: %v", err)
	}

	j := &PanJob{ID: 1, Key: diskPrefix + "/我的资源/衣服合集", Title: "衣服合集", Stage: "save"}
	if err := runPanJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	if j.Dir != filepath.Join(st.Settings.DownloadDir, "衣服合集") {
		t.Errorf("downloaded into %s", j.Dir)
	}
	if got := strings.Join(testkit.ListTree(j.Dir), ","); got != "2/Cat Ears/Cat Ears.unitypackage,2/Cat Ears/readme.txt,2/Sailor Dress/Sailor Dress.unitypackage,2/Sailor Dress/readme.txt,2/Twintail/Twintail.unitypackage,2/Twintail/readme.txt,2/说明.txt" {
		t.Errorf("on disk: %s", got)
	}
	if tr, rm := f.take(); tr != "" || rm != "" {
		t.Errorf("the netdisk was changed: saved %q, removed %q", tr, rm)
	}
	if j.Stage != "done" || j.Msg != "已下载 1 个文件（"+fmtBytes(int64(len(f.disk["/我的资源/衣服合集/2.zip"].data)))+"）；合集包，已拆分为 3 个素材" {
		t.Errorf("job: %s %q", j.Stage, j.Msg)
	}
	if got := marksUnder(st, j.Dir); got != ".=bundle 2=bundle" {
		t.Errorf("marks: %q", got)
	}
	if got := cardNames(st); got != "Cat Ears,Sailor Dress,Twintail" {
		t.Errorf("cards: %s", got)
	}
}

// A file of the netdisk comes into a folder of its own name (an archive is unpacked in it, as a share's is);
// one that is not there any more says so.
func TestDiskDownloadFile(t *testing.T) {
	f, st := newFakeBaidu(t)
	st.Settings.NoExtract = false
	f.disk["/Gothic Dress.zip"] = &bdNode{data: testkit.ZipBytes(t, map[string][]byte{"Gothic Dress.unitypackage": []byte("pkg")}), mtime: 10}
	j := &PanJob{ID: 1, Key: diskPrefix + "/Gothic Dress.zip", Title: "Gothic Dress.zip", Stage: "save"}
	if err := runPanJob(context.Background(), st, j); err != nil {
		t.Fatal(err)
	}
	if j.Dir != filepath.Join(st.Settings.DownloadDir, "Gothic Dress") || strings.Join(testkit.ListTree(j.Dir), ",") != "Gothic Dress/Gothic Dress.unitypackage" {
		t.Errorf("downloaded: %s %v", j.Dir, testkit.ListTree(j.Dir))
	}
	j = &PanJob{ID: 2, Key: diskPrefix + "/Gone.zip", Title: "Gone.zip", Stage: "save"}
	if err := runPanJob(context.Background(), st, j); err == nil || err.Error() != "网盘中未找到「Gone.zip」（可能已被移动或删除）" {
		t.Errorf("not there: %v", err)
	}
	if err := QueueDiskDownload(st, " / "); err == nil {
		t.Error("the whole netdisk was queued")
	}
	settleLibrary()
}

// Without a login there is nothing to list: the window is told so.
func TestDiskNeedsLogin(t *testing.T) {
	_, st := newFakeBaidu(t)
	ForgetBaiduSession()
	if _, _, err := ListDisk(st, "/"); !errors.Is(err, ErrBaiduLogin) {
		t.Errorf("not logged in: %v", err)
	}
}
