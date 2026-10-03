package core

import (
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
