package core

var AppVersion = "1.7.6" // a var so test builds can set it with -ldflags "-X vrclib/internal/core.AppVersion=…"

var UpdatedFrom string // the version this run was updated from (shown once in the window)
