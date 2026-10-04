package cloudshare

import "testing"

func TestParseLinks(t *testing.T) {
	for _, c := range []struct {
		text, key, url, name string
		folder               bool
	}{
		// Google Drive files
		{"https://drive.google.com/file/d/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456/view?usp=sharing", "gd:1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "https://drive.google.com/file/d/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456/view", "", false},
		{"https://drive.google.com/file/d/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456/view?usp=drive_link", "gd:1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "https://drive.google.com/file/d/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456/view", "", false},
		{"https://drive.google.com/file/d/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456/edit", "gd:1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "", "", false},
		{"https://drive.google.com/file/d/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "gd:1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "", "", false},
		{"https://drive.google.com/file/u/0/d/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456/view", "gd:1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "", "", false},
		{"https://drive.google.com/open?id=1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "gd:1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "https://drive.google.com/open?id=1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "", false},
		{"https://drive.google.com/open?id=1AbCdEfGhIjKlMnOpQrStUvWxYz0123456&usp=sharing", "gd:1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "https://drive.google.com/open?id=1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "", false},
		{"https://drive.google.com/open?usp=sharing&id=1AbCdEfGhIjKlMnOpQrStUvWxYz0123456&resourcekey=0-AbC_12", "gd:1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "https://drive.google.com/open?id=1AbCdEfGhIjKlMnOpQrStUvWxYz0123456&resourcekey=0-AbC_12", "", false},
		{"https://drive.google.com/uc?id=1AbCdEfGhIjKlMnOpQrStUvWxYz0123456&export=download", "gd:1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "", "", false},
		{"https://drive.google.com/uc?export=download&id=1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "gd:1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "", "", false},
		{"https://docs.google.com/uc?export=download&id=1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "gd:1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "", "", false},
		{"https://drive.usercontent.google.com/download?id=1AbCdEfGhIjKlMnOpQrStUvWxYz0123456&export=download&confirm=t", "gd:1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "", "", false},
		{"https://drive.google.com/file/d/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456/view?resourcekey=0-AbC_12-xyz&usp=sharing", "gd:1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "https://drive.google.com/file/d/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456/view?resourcekey=0-AbC_12-xyz", "", false},
		// a Workspace account's links, the download host with an account number
		{"https://drive.google.com/a/example.com/file/d/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456/view", "gd:1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "https://drive.google.com/file/d/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456/view", "", false},
		{"https://drive.google.com/corp/drive/folders/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "gd:1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "https://drive.google.com/drive/folders/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "", true},
		{"https://drive.google.com/a/example.com/drive/folders/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "gd:1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "", "", true},
		{"https://drive.usercontent.google.com/u/0/uc?id=1AbCdEfGhIjKlMnOpQrStUvWxYz0123456&export=download", "gd:1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "", "", false},
		{"https://drive.google.com/u/0/uc?id=1AbCdEfGhIjKlMnOpQrStUvWxYz0123456&export=download", "gd:1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "", "", false},
		// Google Drive folders
		{"https://drive.google.com/drive/folders/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456?usp=sharing", "gd:1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "https://drive.google.com/drive/folders/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "", true},
		{"https://drive.google.com/drive/u/1/folders/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "gd:1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "", "", true},
		{"https://drive.google.com/drive/mobile/folders/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "gd:1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "", "", true},
		{"https://drive.google.com/folderview?id=1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "gd:1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "", "", true},
		{"https://drive.google.com/embeddedfolderview?id=1AbCdEfGhIjKlMnOpQrStUvWxYz0123456#list", "gd:1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "", "", true},
		{"https://drive.google.com/drive/folders/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456?resourcekey=0-Key_1&usp=drive_link", "gd:1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "https://drive.google.com/drive/folders/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456?resourcekey=0-Key_1", "", true},
		// chat text, full-width characters, glued punctuation
		{"衣服在这里 https://drive.google.com/file/d/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456/view?usp=sharing，解压密码在说明里。", "gd:1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "", "", false},
		{"ｈｔｔｐｓ：／／ｄｒｉｖｅ．ｇｏｏｇｌｅ．ｃｏｍ／ｆｉｌｅ／ｄ／１ＡｂＣｄＥｆＧｈＩｊＫｌＭｎＯｐＱｒＳｔＵｖＷｘＹｚ０１２３４５６／ｖｉｅｗ", "gd:1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "", "", false},
		{"here: drive.google.com/drive/folders/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456 (all versions)", "gd:1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "", "", true},
		{"<a href=\"https://drive.google.com/file/d/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456/view\">file</a>", "gd:1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "", "", false},
		// Dropbox
		{"https://www.dropbox.com/s/abc123def456ghi/Outfit_v1.2.zip?dl=0", "db:abc123def456ghi", "https://www.dropbox.com/s/abc123def456ghi/Outfit_v1.2.zip", "Outfit_v1.2.zip", false},
		{"https://www.dropbox.com/s/abc123def456ghi/Outfit_v1.2.zip?dl=1", "db:abc123def456ghi", "https://www.dropbox.com/s/abc123def456ghi/Outfit_v1.2.zip", "Outfit_v1.2.zip", false},
		{"https://dl.dropboxusercontent.com/s/abc123def456ghi/Outfit_v1.2.zip", "db:abc123def456ghi", "https://www.dropbox.com/s/abc123def456ghi/Outfit_v1.2.zip", "Outfit_v1.2.zip", false},
		{"https://www.dropbox.com/scl/fi/abc123def456ghi/Outfit_v1.2.zip?rlkey=k1k2k3k4k5k6&st=ab12cd34&dl=0", "db:abc123def456ghi", "https://www.dropbox.com/scl/fi/abc123def456ghi/Outfit_v1.2.zip?rlkey=k1k2k3k4k5k6", "Outfit_v1.2.zip", false},
		{"https://www.dropbox.com/scl/fi/abc123def456ghi/Outfit_v1.2.zip?rlkey=k1k2k3k4k5k6&amp;dl=0", "db:abc123def456ghi", "https://www.dropbox.com/scl/fi/abc123def456ghi/Outfit_v1.2.zip?rlkey=k1k2k3k4k5k6", "Outfit_v1.2.zip", false},
		{"https://www.dropbox.com/scl/fo/abc123def456ghi/AKs9d_xyz?rlkey=k1k2k3k4k5k6&dl=0", "db:abc123def456ghi", "https://www.dropbox.com/scl/fo/abc123def456ghi/AKs9d_xyz?rlkey=k1k2k3k4k5k6", "AKs9d_xyz", true},
		{"https://www.dropbox.com/scl/fo/abc123def456ghi/h?rlkey=k1k2k3k4k5k6&dl=0", "db:abc123def456ghi", "https://www.dropbox.com/scl/fo/abc123def456ghi/h?rlkey=k1k2k3k4k5k6", "", true},
		{"https://www.dropbox.com/sh/abc123def456ghi/AAC09AQRODS_F7EVYXt5mgcMa?dl=0", "db:abc123def456ghi", "https://www.dropbox.com/sh/abc123def456ghi/AAC09AQRODS_F7EVYXt5mgcMa", "", true},
		{"https://www.dropbox.com/sh/abc123def456ghi/AAC09AQRODS_F7EVYXt5mgcMa/Textures?dl=0", "db:abc123def456ghi", "https://www.dropbox.com/sh/abc123def456ghi/AAC09AQRODS_F7EVYXt5mgcMa/Textures", "", true},
		{"Dropbox: https://www.dropbox.com/s/abc123def456ghi/Outfit.zip?dl=0。有问题找我", "db:abc123def456ghi", "https://www.dropbox.com/s/abc123def456ghi/Outfit.zip", "Outfit.zip", false},
		{"https://www.dropbox.com/s/abc123def456ghi/Outfit.zip.", "db:abc123def456ghi", "https://www.dropbox.com/s/abc123def456ghi/Outfit.zip", "Outfit.zip", false},
		{"ｈｔｔｐｓ：／／ｗｗｗ．ｄｒｏｐｂｏｘ．ｃｏｍ／ｓ／ａｂｃ１２３ｄｅｆ４５６ｇｈｉ／Ｏｕｔｆｉｔ．ｚｉｐ？ｄｌ＝０", "db:abc123def456ghi", "https://www.dropbox.com/s/abc123def456ghi/Outfit.zip", "Outfit.zip", false},
		// a name inside the link is kept exactly as pasted: punctuation before "?dl=0" is the name's, and so are
		// the full-width characters of a name in a link typed in plain ones
		{"https://www.dropbox.com/s/abc123def456ghi/Dress%20(v2)?dl=0", "db:abc123def456ghi", "https://www.dropbox.com/s/abc123def456ghi/Dress%20(v2)", "Dress%20(v2)", false},
		{"https://www.dropbox.com/scl/fo/abc123def456ghi/AMx.?rlkey=k1k2k3k4k5k6&dl=0", "db:abc123def456ghi", "https://www.dropbox.com/scl/fo/abc123def456ghi/AMx.?rlkey=k1k2k3k4k5k6", "AMx.", true},
		{"https://www.dropbox.com/scl/fi/abc123def456ghi/髪型（新）.zip?rlkey=k1k2k3k4k5k6&dl=0", "db:abc123def456ghi", "https://www.dropbox.com/scl/fi/abc123def456ghi/髪型（新）.zip?rlkey=k1k2k3k4k5k6", "髪型（新）.zip", false},
		{"下载：https://www.dropbox.com/s/abc123def456ghi/髪型（新）.zip?dl=0　提取码无", "db:abc123def456ghi", "https://www.dropbox.com/s/abc123def456ghi/髪型（新）.zip", "髪型（新）.zip", false},
		{"https://www.dropbox.com/sh/abc123def456ghi/AAC09AQRODS_F7EVYXt5mgcMa/Textures%20(4K)?dl=0", "db:abc123def456ghi", "https://www.dropbox.com/sh/abc123def456ghi/AAC09AQRODS_F7EVYXt5mgcMa/Textures%20(4K)", "", true},
		// a name that ends the link: the chat's punctuation goes, a bracket the name opened stays
		{"https://www.dropbox.com/s/abc123def456ghi/Dress%20(v2)", "db:abc123def456ghi", "https://www.dropbox.com/s/abc123def456ghi/Dress%20(v2)", "Dress%20(v2)", false},
		{"（见 https://www.dropbox.com/s/abc123def456ghi/Dress%20(v2).zip）。", "db:abc123def456ghi", "https://www.dropbox.com/s/abc123def456ghi/Dress%20(v2).zip", "Dress%20(v2).zip", false},
		{"(https://www.dropbox.com/s/abc123def456ghi/Outfit.zip)", "db:abc123def456ghi", "https://www.dropbox.com/s/abc123def456ghi/Outfit.zip", "Outfit.zip", false},
		// the first link of a text wins
		{"https://drive.google.com/file/d/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456/view or https://www.dropbox.com/s/abc123def456ghi/a.zip?dl=0", "gd:1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "", "", false},
		{"https://drive.google.com/file/d/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456/view　或 https://www.dropbox.com/s/abc123def456ghi/髪（新）.zip?dl=0", "gd:1AbCdEfGhIjKlMnOpQrStUvWxYz0123456", "", "", false},
		{"https://www.dropbox.com/s/abc123def456ghi/a.zip?dl=0 or https://drive.google.com/file/d/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456/view", "db:abc123def456ghi", "", "a.zip", false},
		// not these
		{"https://docs.google.com/document/d/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456/edit", "", "", "", false},
		{"https://docs.google.com/spreadsheets/d/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456/edit", "", "", "", false},
		{"https://pan.baidu.com/s/1AbCdEfGh?pwd=ab12", "", "", "", false},
		{"https://www.dropbox.com/t/AbCdEfGhIjKlMnOp", "", "", "", false}, // Dropbox Transfer: another product
		{"https://example.com/file/d/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456/view", "", "", "", false},
		{"https://drive.google.com/drive/my-drive", "", "", "", false},
		{"", "", "", "", false},
	} {
		l := Parse(c.text)
		switch {
		case l == nil && c.key == "":
			continue
		case l == nil:
			t.Errorf("%s\n\tnot recognised, want %s", c.text, c.key)
			continue
		case c.key == "":
			t.Errorf("%s\n\trecognised as %s, want nothing", c.text, l.Key())
			continue
		}
		if l.Key() != c.key || (c.url != "" && l.URL != c.url) || l.Name != c.name || l.Folder != c.folder {
			t.Errorf("%s\n\tkey %s url %s name %q folder %v, want %s %s %q %v", c.text, l.Key(), l.URL, l.Name, l.Folder, c.key, c.url, c.name, c.folder)
		}
		if again := Parse(l.URL); again == nil || *again != *l { // the link as it is kept reads as the same share
			t.Errorf("%s\n\tkept as %s, which reads as %+v", c.text, l.URL, again)
		}
		if KeyOf(c.text) != c.key || !IsCloudKey(c.key) || ServiceOf(c.key) != c.key[:2] || ServiceOf(c.key+"#/a") != c.key[:2] {
			t.Errorf("%s: key helpers disagree", c.text)
		}
	}
	if IsCloudKey("pan:1abc") || ServiceOf("pan:1abc") != "" || ServiceOf("gdx") != "" || Label("pan") != "" || Label(GDrive) != "Google Drive" || Label(Dropbox) != "Dropbox" {
		t.Error("key helpers on other keys")
	}
}
