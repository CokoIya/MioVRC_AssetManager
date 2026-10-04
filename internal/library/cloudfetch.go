package library

import (
	"vrclib/internal/cloudshare"
	"vrclib/internal/core"
	"vrclib/internal/netdisk"
)

// fetchListing reads a share by the kind of its link: a Google Drive or Dropbox share through cloudshare, a
// Baidu one through netdisk (fetchPanListing, which tests replace). Both land in st.Pan under netdisk.ShareID,
// so the kind is decided as ShareID decides it: a link field that holds both ("百度 … / 海外 Drive …") is the
// Baidu share's.
func fetchListing(st *core.Store, link, pwd string) (*core.PanListing, error) {
	if netdisk.ShareSurl(link) == "" && cloudshare.KeyOf(link) != "" {
		return fetchCloudListing(st, link)
	}
	return fetchPanListing(st, link, pwd)
}

// fetchCloudListing: tests put their own reader here.
var fetchCloudListing = cloudshare.FetchListing
