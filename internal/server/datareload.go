package server

import (
	"vrclib/internal/booth"
	"vrclib/internal/library"
	"vrclib/internal/unity"
)

// data files other packages keep in memory are read again after a library import or its undo replaced them
func init() {
	library.OnDataImported(func() {
		booth.WishReload()
		booth.FollowReload()
		unity.CheckupsReload()
	})
}
