package naming

import "testing"

// An item's page is known whatever language Booth showed it in — the address a browser set to Chinese or English
// gives (booth.pm/zh-cn/items/…) as much as the Japanese one —, and from a shop's own site.
func TestBoothURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://booth.pm/ja/items/7727656":                                  "7727656",
		"https://booth.pm/zh-cn/items/7727656":                               "7727656",
		"https://booth.pm/zh-tw/items/7727656?srsltid=AfmBOo":                "7727656",
		"https://booth.pm/en/items/7727656#top":                              "7727656",
		"https://booth.pm/ko/items/7727656/":                                 "7727656",
		"https://booth.pm/jp/items/7727656":                                  "7727656",
		"https://booth.pm/ZH-CN/items/7727656":                               "7727656",
		"https://booth.pm/items/7727656":                                     "7727656",
		"https://komado.booth.pm/items/7727656":                              "7727656",
		"booth.pm/zh-cn/items/7727656":                                       "7727656",
		"[InternetShortcut]\r\nURL=https://booth.pm/zh-cn/items/7727656\r\n": "7727656",
		"https://booth.pm/zh-cn/items/123":                                   "",
		"https://booth.pm/zh-cn/items/12345678901":                           "",
		"https://booth.pm/zh-cn/browse/3Dモデル":                                "",
		"https://booth.pm/zh-cn/items/abc":                                   "",
	} {
		got := ""
		if m := ReBoothURL.FindStringSubmatch(in); m != nil {
			got = m[1]
		}
		if got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
