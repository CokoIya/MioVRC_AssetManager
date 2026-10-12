package pandl

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"vrclib/internal/core"
	"vrclib/internal/purchases"
)

// A file of the netdisk over several connections at once. Baidu limits each connection: an account with
// 超级会员 gets its speed only over several, which is how Baidu's own client downloads. The file is asked for in
// pieces (Range), each written where it belongs in the ".part" file; what is still to come is kept next to it
// (".part.json"), so a download that stops goes on where it was, in this run of the program or the next. An
// account without 超级会员 downloads over one connection, as before: Baidu limits the account as a whole, and
// more connections would only ask more of it.

var (
	rangeWorkers = 6               // connections at once for one file (超级会员)
	rangeMinSize = int64(32 << 20) // a smaller file comes over one connection
	rangePiece   = int64(16 << 20) // what is asked for at a time
)

// rangeState: what of a file is not on the disk yet, as [from, to) pieces.
type rangeState struct {
	Size    int64      `json:"size"`
	Path    string     `json:"path"` // the file in the netdisk
	Pending [][2]int64 `json:"pending"`
}

func rangeStateFile(part string) string { return part + ".json" }

// readRangeState: the state kept for part, nil when there is none, or none for this file.
func readRangeState(part string, f bdFile) *rangeState {
	b, err := os.ReadFile(rangeStateFile(part))
	if err != nil {
		return nil
	}
	var s rangeState
	if json.Unmarshal(b, &s) != nil || s.Size != f.Size || s.Path != f.Path {
		return nil
	}
	for _, p := range s.Pending {
		if p[0] < 0 || p[1] > f.Size || p[0] >= p[1] {
			return nil
		}
	}
	return &s
}

func writeRangeState(part string, s *rangeState) error {
	b, _ := json.Marshal(s)
	tmp := rangeStateFile(part) + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, rangeStateFile(part))
}

// errPieceCut: a piece that ended before its end without an error (the connection was closed).
var errPieceCut = errors.New("百度网盘返回的数据不完整")

// piece: a part of the file being fetched; from moves on as it arrives.
type piece struct{ from, to int64 }

// downloadRanges fetches what of f is not in part yet over up to workers connections, then makes it local.
func (b *bdClient) downloadRanges(f bdFile, local, part string, workers int, prog func(n int64)) error {
	st := readRangeState(part, f)
	if st == nil { // a new download, or one that went over one connection so far: what it got is kept
		var have int64
		if fi, err := os.Stat(part); err == nil && fi.Size() <= f.Size {
			have = fi.Size()
		} else if err == nil {
			_ = os.Remove(part)
		}
		st = &rangeState{Size: f.Size, Path: f.Path}
		for from := have; from < f.Size; from += rangePiece {
			st.Pending = append(st.Pending, [2]int64{from, min(from+rangePiece, f.Size)})
		}
	}
	var left int64
	for _, p := range st.Pending {
		left += p[1] - p[0]
	}
	if err := purchases.CheckSpace(filepath.Dir(local), left); err != nil {
		return err
	}
	fh, err := os.OpenFile(part, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return purchases.WriteErr(part, err)
	}
	closed := false
	defer func() {
		if !closed {
			fh.Close()
		}
	}()
	if fi, err := fh.Stat(); err == nil && fi.Size() < f.Size {
		if err := fh.Truncate(f.Size); err != nil {
			return purchases.WriteErr(part, err)
		}
	}
	if err := writeRangeState(part, st); err != nil {
		return purchases.WriteErr(part, err)
	}
	var progMu sync.Mutex
	report := func(n int64) {
		progMu.Lock()
		prog(n)
		progMu.Unlock()
	}
	report(f.Size - left) // what is on the disk already

	var (
		mu      sync.Mutex
		todo    []*piece
		active  = map[*piece]bool{}
		stop    error // ends it all: cancelled, the login, the disk, Baidu's no
		lastErr error
		fails   int  // tries in a row that brought nothing, all connections together
		alone   bool // Baidu turned several connections away: one goes on (lead)
		lead    = -1
		refused int // refusals while alone
	)
	for _, p := range st.Pending {
		todo = append(todo, &piece{p[0], p[1]})
	}
	save := func() { // (mu held) what is not on the disk: the pieces to come and the rest of those coming
		var p [][2]int64
		for _, x := range todo {
			p = append(p, [2]int64{x.from, x.to})
		}
		for x := range active {
			if x.from < x.to {
				p = append(p, [2]int64{x.from, x.to})
			}
		}
		sort.Slice(p, func(i, j int) bool { return p[i][0] < p[j][0] })
		_ = writeRangeState(part, &rangeState{Size: f.Size, Path: f.Path, Pending: p})
	}
	take := func(w int) *piece {
		mu.Lock()
		defer mu.Unlock()
		if stop != nil || len(todo) == 0 || (alone && w != lead) {
			return nil
		}
		x := todo[0]
		todo = todo[1:]
		active[x] = true
		return x
	}
	var wg sync.WaitGroup
	worker := func(w int) {
		defer wg.Done()
		for {
			x := take(w)
			if x == nil {
				return
			}
			before := x.from
			err := b.fetchPiece(f.Path, fh, x, &mu, report)
			mu.Lock()
			delete(active, x)
			if x.from < x.to {
				todo = append([]*piece{x}, todo...) // what is left of it comes next
			}
			got := x.from > before
			if err == nil && x.from < x.to {
				err = errPieceCut
			}
			if err == nil || got {
				fails = 0
			}
			var pe pcsError
			switch {
			case err == nil:
				save()
				mu.Unlock()
				continue
			case errors.Is(err, errPanCancelled) || purchases.IsLocalErr(err):
				stop = err
			case errors.As(err, &pe) && pe.auth():
				stop = ErrBaiduLogin
			case errors.As(err, &pe) && !alone:
				// Baidu turns several connections away: this one goes on alone, the others stop
				alone, lead = true, w
				core.Logf("百度网盘拒绝了分段下载（%s），改为单个连接下载", pe.Error())
			case errors.As(err, &pe) && w == lead:
				// … and one too: as one connection always did, once more under the other name of the client
				// when there is one; when that is turned away too, it is over
				refused++
				b.mu.Lock()
				switched := b.dlUA+1 < len(pcsUAs)
				if switched && refused > 1 {
					b.dlUA++
				}
				b.mu.Unlock()
				if !switched && (pe.status == 403 || pe.status == 404) {
					stop = err
				}
			}
			lastErr = err
			if stop == nil && !got {
				if fails++; fails >= 40 {
					stop = err
				}
			}
			save()
			n, quit := fails, stop != nil || (alone && w != lead)
			mu.Unlock()
			if quit {
				return
			}
			if !got { // nothing came: a little later
				if b.sleep(time.Duration(min(2+n*2, 20))*panRetryUnit) != nil {
					mu.Lock()
					if stop == nil {
						stop = errPanCancelled
					}
					mu.Unlock()
					return
				}
			}
		}
	}
	n := max(workers, 1)
	if left < int64(n)*rangePiece { // (no more connections than pieces)
		n = max(int((left+rangePiece-1)/rangePiece), 1)
	}
	for round := 0; ; round++ {
		wg.Add(n)
		for w := 0; w < n; w++ {
			go worker(w)
		}
		wg.Wait()
		mu.Lock()
		again := stop == nil && len(todo) > 0 && round < 40
		if again { // pieces put back after the connection that would have taken them ended: one goes on
			alone, lead, n = true, 0, 1
		}
		mu.Unlock()
		if !again {
			break
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if stop != nil {
		return stop
	}
	if len(todo) > 0 || len(active) > 0 {
		if lastErr == nil {
			lastErr = errors.New("下载失败，请重试")
		}
		return lastErr
	}
	if err := fh.Close(); err != nil {
		return purchases.WriteErr(part, err)
	}
	closed = true
	_ = os.Remove(local)
	if err := os.Rename(part, local); err != nil {
		return purchases.WriteErr(local, err)
	}
	_ = os.Remove(rangeStateFile(part))
	return nil
}

// fetchPiece asks for x and writes what comes where it belongs, moving x.from on (under mu).
func (b *bdClient) fetchPiece(remote string, fh *os.File, x *piece, mu *sync.Mutex, prog func(n int64)) error {
	g := purchases.NewStallGuard(b.ctx)
	defer g.Stop()
	netErr := func(err error) error {
		switch {
		case b.ctx.Err() != nil:
			return errPanCancelled
		case g.Stalled():
			return fmt.Errorf("下载中断（%d 秒内未收到数据）", int(purchases.StallAfter.Seconds()))
		}
		return fmt.Errorf("下载中断（%s）", purchases.NetErrText(err))
	}
	mu.Lock()
	from, to := x.from, x.to
	mu.Unlock()
	req, _ := http.NewRequestWithContext(g.Ctx, "GET", pcsURL(remote), nil)
	b.mu.Lock()
	req.Header.Set("User-Agent", pcsUAs[b.dlUA])
	b.mu.Unlock()
	req.Header.Set("Cookie", b.cookieHeader())
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", from, to-1))
	resp, err := b.dl.Do(req)
	if err != nil {
		return netErr(err)
	}
	defer resp.Body.Close()
	g.Kick()
	if resp.StatusCode != 206 && !(resp.StatusCode == 200 && from == 0) {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 600))
		return pcsError{resp.StatusCode, string(body)}
	}
	if cr := resp.Header.Get("Content-Range"); resp.StatusCode == 206 && cr != "" {
		var a, z int64
		if _, err := fmt.Sscanf(strings.TrimPrefix(cr, "bytes "), "%d-%d", &a, &z); err != nil || a != from {
			return fmt.Errorf("百度网盘返回的数据范围有误（%s）", cr)
		}
	}
	buf := make([]byte, 256<<10)
	at := from
	for at < to {
		want := buf
		if rest := to - at; rest < int64(len(want)) {
			want = want[:rest] // (a 200 answer is the whole file: only this piece of it is taken)
		}
		n, rerr := resp.Body.Read(want)
		if n > 0 {
			g.Kick()
			if _, werr := fh.WriteAt(want[:n], at); werr != nil {
				return purchases.WriteErr(fh.Name(), werr)
			}
			at += int64(n)
			mu.Lock()
			x.from = at
			mu.Unlock()
			prog(int64(n))
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return netErr(rerr)
		}
	}
	if at < to {
		return errPieceCut
	}
	return nil
}
