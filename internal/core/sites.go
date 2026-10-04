package core

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func BoothDLBase() string {
	if v := os.Getenv("VRCLIB_BOOTH_DL"); v != "" { // tests only
		return strings.TrimRight(v, "/")
	}
	return "https://booth.pm"
}

func BoothWebBase() string {
	if v := os.Getenv("VRCLIB_BOOTH_WEB"); v != "" { // tests only
		return strings.TrimRight(v, "/")
	}
	return "https://booth.pm"
}

func GumroadBase() string {
	if v := os.Getenv("VRCLIB_GUMROAD_BASE"); v != "" { // tests only
		return strings.TrimRight(v, "/")
	}
	return "https://gumroad.com"
}
func GumroadLibraryURL() string { return GumroadBase() + "/library" }

func PanBase() string {
	if v := os.Getenv("VRCLIB_PAN_BASE"); v != "" { // tests only
		return strings.TrimRight(v, "/")
	}
	return "https://pan.baidu.com"
}

func BoothAccountsBase() string {
	if v := os.Getenv("VRCLIB_BOOTH_BASE"); v != "" { // tests only
		return strings.TrimRight(v, "/")
	}
	return "https://accounts.booth.pm"
}

func OrderURL(id string) string { return BoothAccountsBase() + "/orders/" + id }
func LibraryURL(gift bool) string {
	if gift {
		return BoothAccountsBase() + "/library/gifts"
	}
	return BoothAccountsBase() + "/library"
}

func BoothProfileDir() string { return filepath.Join(DataDir, "booth-profile") }

func JinxxyBase() string {
	if v := os.Getenv("VRCLIB_JINXXY_BASE"); v != "" { // tests only
		return strings.TrimRight(v, "/")
	}
	return "https://jinxxy.com"
}

// JinxxySearchURL: Jinxxy's own search has no address a query can be put into (the market's pages ignore
// one), so the words go to Bing, kept to that site.
func JinxxySearchURL(q string) string {
	base := "https://cn.bing.com"
	if v := os.Getenv("VRCLIB_BING_BASE"); v != "" { // tests only
		base = strings.TrimRight(v, "/")
	}
	return base + "/search?q=" + url.QueryEscape("site:jinxxy.com "+strings.TrimSpace(q))
}
