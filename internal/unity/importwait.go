package unity

import (
	"context"
	"errors"
	"time"

	"vrclib/internal/core"
)

var importPoll = 300 * time.Millisecond

// ImportAndWait runs one import from start to end, for work that imports into several projects one after
// another: it waits its turn behind an import that is under way, starts its own, and returns that import as
// it ended ("done" or "failed"). onChange is told each time its stage or message changes. When ctx ends, an
// import that has not begun does not begin, and one that waits for the player's choice is called off; files
// that are being written are finished first.
func ImportAndWait(ctx context.Context, st *core.Store, req ImportReq, onChange func(ImportJob)) (ImportJob, error) {
	for {
		if ctx.Err() != nil {
			return ImportJob{}, ctx.Err()
		}
		err := StartImport(st, req)
		if err == nil {
			break
		}
		if err != errImportBusy {
			return ImportJob{}, err
		}
		select {
		case <-ctx.Done():
			return ImportJob{}, ctx.Err()
		case <-time.After(importPoll):
		}
	}
	var last ImportJob
	id, called := int64(0), false
	for {
		j := ImportSnapshot()
		if j != nil && id == 0 {
			id = j.ID
		}
		if j == nil || j.ID != id {
			// closed by the player the moment it ended, or another one has taken its place
			if last.Stage == "done" || last.Stage == "failed" {
				return last, nil
			}
			return last, errors.New("导入已被关闭")
		}
		if onChange != nil && (j.Stage != last.Stage || j.Msg != last.Msg) {
			onChange(*j)
		}
		last = *j
		if j.Stage == "done" || j.Stage == "failed" {
			return last, nil
		}
		if ctx.Err() != nil && j.Stage == "choose" && !called {
			called = true
			DismissImport()
		}
		time.Sleep(importPoll)
	}
}
