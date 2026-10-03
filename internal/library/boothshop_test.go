package library

import (
	"net/url"
	"strings"
	"testing"

	"vrclib/internal/core"
)

func TestShopURLs(t *testing.T) {
	bases := []string{"Plum=plum|プラム", "Chocolat=chocolat|ショコラ"}
	q := ShopQuery{Cats: []string{"衣服", "头发"}, Bases: []string{"Plum"}, Styles: []string{"可爱", "H"}, Sort: "new"}
	us := shopURLs(q, bases, core.DefaultStyles)
	if len(us) != 2 {
		t.Fatalf("one address per category: %v", us)
	}
	u0, _ := url.Parse(us[0])
	if u0.Path != "/ja/browse/3D衣装" || u0.Query().Get("q") != "プラム かわいい" || u0.Query().Get("sort") != "new" || u0.Query().Get("adult") != "include" {
		t.Errorf("clothes: %s", us[0])
	}
	u1, _ := url.Parse(us[1])
	if u1.Path != "/ja/browse/3Dモデル" || u1.Query().Get("q") != "プラム かわいい 髪型" {
		t.Errorf("hair: %s", us[1])
	}
	if us := shopURLs(ShopQuery{}, bases, core.DefaultStyles); len(us) != 1 || !strings.Contains(us[0], "sort=wish_lists") {
		t.Errorf("nothing picked: %v", us)
	}
}
