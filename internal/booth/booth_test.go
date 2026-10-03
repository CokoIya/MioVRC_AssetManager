package booth

import (
	"testing"
)

func TestBoothParse(t *testing.T) {
	live := `{"id":"8562330","name":"九尾オリジナル3Dモデル「輝夜」-kaguya-","description":"desc","price":"¥ 6,000","url":"https://paryi.booth.pm/items/8562330",
	"category":{"id":208,"name":"3Dキャラクター","parent":{"name":"3Dモデル"}},"shop":{"name":"IKUSIA","subdomain":"paryi","thumbnail_url":"x"},
	"tags":[{"name":"髪の毛","url":"u"},{"name":"髪型","url":"u"}],"images":[{"caption":null,"original":"o1","resized":"r1"},{"caption":null,"original":"o2","resized":"r2"}],
	"variations":[{"id":1}],"is_adult":false}`
	it, err := parseBoothItem([]byte(live))
	if err != nil || it.Name == "" || len(it.Tags) != 2 || it.Tags[0] != "髪の毛" || len(it.Images) != 2 || it.Image != "o1" || it.ShopURL != "https://paryi.booth.pm/" || it.Category != "3Dキャラクター" {
		t.Fatalf("%v %+v", err, it)
	}
	old := `{"name":"a","tags":["x","y"],"price":1500,"images":null}`
	it, err = parseBoothItem([]byte(old))
	if err != nil || len(it.Tags) != 2 || it.Price != "1500" {
		t.Fatalf("%v %+v", err, it)
	}
	page := `<section class="shop__text"><h2 class="a">商品説明</h2><p class="b">一行目
二行目 &lt;注意&gt;</p></section><section class="shop__text"><h2 class="a">利用規約</h2><p>VN3<br>再配布禁止</p></section><section class="shop__text"><h2>画像</h2><img src=x></section>`
	got := boothSections([]byte(page))
	want := "【商品説明】\n一行目\n二行目 <注意>\n\n【利用規約】\nVN3\n再配布禁止"
	if got != want {
		t.Fatalf("%q", got)
	}
	if s := trimOgTitle("九尾 - kaguya - IKUSIA - BOOTH"); s != "九尾 - kaguya" {
		t.Fatal(s)
	}
	if s := boothThumb300("https://booth.pximg.net/c/72x72_a2_g5/u/i/1/a_base_resized.jpg"); s != "https://booth.pximg.net/c/300x300_a2_g5/u/i/1/a_base_resized.jpg" {
		t.Fatal(s)
	}
	if s := boothThumb300("https://booth.pximg.net/u/i/1/a_base_resized.jpg"); s != "https://booth.pximg.net/u/i/1/a_base_resized.jpg" {
		t.Fatal(s)
	}
	if s := trimOgTitle("plain"); s != "plain" {
		t.Fatal(s)
	}
}
