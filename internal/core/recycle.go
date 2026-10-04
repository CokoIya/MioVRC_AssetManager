package core

import "errors"

// ErrNoRecycleBin: the drive a file is on has no Recycle Bin (a network drive, a USB stick), so it stays.
var ErrNoRecycleBin = errors.New("所在磁盘没有回收站")
