package purchases

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"

	"vrclib/internal/core"
)

// ---------- what every download shares (Booth, Gumroad, the netdisk) ----------

// ErrCancelled: the player pressed cancel (or the program is closing).
var ErrCancelled = errors.New("已取消")

// LocalErr: the disk, not the network — asking the server again does not help.
type LocalErr struct{ Msg string }

func (e *LocalErr) Error() string { return e.Msg }

func IsLocalErr(err error) bool {
	var le *LocalErr
	return errors.As(err, &le)
}

// diskFull: Windows says it with its own numbers (ERROR_HANDLE_DISK_FULL, ERROR_DISK_FULL).
func diskFull(err error) bool {
	var en syscall.Errno
	if !errors.As(err, &en) {
		return false
	}
	return en == syscall.ENOSPC || (runtime.GOOS == "windows" && (en == 39 || en == 112))
}

// WriteErr: a failed create, write or rename of p, as the player is told about it.
func WriteErr(p string, err error) error {
	if diskFull(err) {
		return &LocalErr{"磁盘空间不足，无法写入「" + p + "」"}
	}
	why := err.Error()
	var pe *fs.PathError
	switch {
	case errors.Is(err, fs.ErrPermission):
		why = "没有写入权限"
	case errors.Is(err, fs.ErrNotExist):
		why = "文件夹不存在"
	case errors.As(err, &pe):
		why = pe.Err.Error()
	}
	return &LocalErr{"无法写入「" + p + "」：" + why}
}

// DiskFree: free bytes on the disk of a folder (tests put a small disk here).
var DiskFree = core.DiskFree

// CheckSpace: room for need more bytes in dir? Nothing is said when the free space is not known.
func CheckSpace(dir string, need int64) error {
	free := DiskFree(dir)
	if need <= 0 || free == 0 || uint64(need) <= free {
		return nil
	}
	return &LocalErr{fmt.Sprintf("磁盘空间不足：「%s」所在磁盘剩余 %s，还需要 %s", dir, core.FmtMB(int64(free)), core.FmtMB(need))}
}

// StallAfter: a connection that brings nothing for this long is given up (and the download goes on from where
// it was). Big files take hours, so the whole request has no time limit.
var StallAfter = 60 * time.Second

// StallGuard ends a request when nothing arrives for a while: no answer to the connection or the TLS
// handshake, no headers, or a body that went silent.
type StallGuard struct {
	Ctx     context.Context
	cancel  context.CancelFunc
	t       *time.Timer
	d       time.Duration
	stalled atomic.Bool
}

func NewStallGuard(parent context.Context) *StallGuard {
	g := &StallGuard{d: StallAfter}
	g.Ctx, g.cancel = context.WithCancel(parent)
	g.t = time.AfterFunc(g.d, func() {
		g.stalled.Store(true)
		g.cancel()
	})
	return g
}

// Kick: something arrived.
func (g *StallGuard) Kick() { g.t.Reset(g.d) }

func (g *StallGuard) Stop() {
	g.t.Stop()
	g.cancel()
}

func (g *StallGuard) Stalled() bool { return g.stalled.Load() }

// Sleep waits, unless ctx ends first (false).
func Sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// NetErrText: why a request failed, without its address — a file's address carries the signature that
// opens it, and this text goes into the log.
func NetErrText(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return core.FriendlyNetErr(err)
}

// LogURL: an address for the log, host and path only.
func LogURL(u string) string {
	p, err := url.Parse(u)
	if err != nil || p.Host == "" {
		return "（地址无效）"
	}
	return p.Host + p.Path
}
