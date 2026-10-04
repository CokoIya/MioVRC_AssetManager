package core

import "sync/atomic"

// Downloading counts the download jobs that are queued or running (Booth, Gumroad, netdisk, and the
// unpacking that follows). The app does not quit on its own while it is above zero, and the window asks
// before closing.
var Downloading atomic.Int32

// Quitting is set once the program has started to shut down: background work stops at its next step, and
// /api/ping says so, which makes a copy started meanwhile wait instead of using a server about to go.
var Quitting atomic.Bool
