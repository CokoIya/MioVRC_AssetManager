package core

import (
	"strings"
	"testing"
)

func TestNetdiskShareText(t *testing.T) {
	for _, s := range []string{
		"链接: https://pan.baidu.com/s/1AbCdEfG 提取码: x1y2 复制这段内容后打开百度网盘手机App，操作更方便哦",
		"  通过百度网盘分享的文件：衣服.zip\n链接：https://pan.baidu.com/s/1abc?pwd=abcd \n提取码：abcd  ",
		"pan.baidu.com/s/1abc 码 1234",
		"https://yun.baidu.com/s/1abc",
		"https://pan.baidu.com/share/init?surl=AbCdEf 提取码 x1y2",
		"https://drive.google.com/drive/folders/1AbCdEfGhIjKlMnOp?usp=sharing",
		"https://www.123pan.com/s/abcd-efgh",
		"https://wwi.lanzoup.com/iAbCd123",
		"https://drive.google.com/file/d/1AbCdEfGhIjKlMnOp/view?usp=sharing",
		"https://www.dropbox.com/scl/fi/abc/x.zip?rlkey=k&dl=0",
		"https://pan.quark.cn/s/abcdef",
		"https://www.alipan.com/s/abc",
		"HTTPS://PAN.BAIDU.COM/S/1ABC",
		// in full-width characters, as a chat may send it (the add dialog reads those too)
		"ｐａｎ．ｂａｉｄｕ．ｃｏｍ／ｓ／１ＡｂＣｄＥｆ 提取码 x1y2",
		"链接：ｈｔｔｐｓ：／／ｐａｎ．ｂａｉｄｕ．ｃｏｍ／ｓ／１ａｂｃ　提取码：ａｂｃｄ",
	} {
		if got := NetdiskShareText(s); got != strings.TrimSpace(s) {
			t.Errorf("not taken as a share: %q → %q", s, got)
		}
	}
	for _, s := range []string{
		"", "   ", "你好，在吗", "https://www.goofish.com/item?id=1", "my password is hunter2", "https://booth.pm/ja/items/123",
		"https://example.com/pan-baidu-com",
		// a text that only names a netdisk, or holds something that merely looks like one, is no share
		"my bank password is hunter2 — see also dropbox.com for the rest", "Mr. Alipanah wrote", `<div class="col-123panel">`,
		"https://example.com/?next=pan.baidu.com", "百度网盘 pan.baidu.com 的会员到期了", "https://notpan.baidu.com.evil.example/s/1abc",
		"百度网盘 ｐａｎ．ｂａｉｄｕ．ｃｏｍ 的会员到期了",
		"https://xpan.baidu.com/s/1abc", "https://drive.google.com/", "https://www.dropbox.com/login",
		"https://pan.baidu.com/s/1abc " + strings.Repeat("字", ClipMaxChars), // a whole document that happens to hold a link
		"https://pan.baidu.com/s/1abc\xff\xfe",
	} {
		if got := NetdiskShareText(s); got != "" {
			t.Errorf("taken as a share: %.60q", s)
		}
	}
}
