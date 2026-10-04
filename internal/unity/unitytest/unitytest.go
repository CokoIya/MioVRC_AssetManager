package unitytest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"vrclib/internal/unity"
)

// FakeUnity plays the pipeline package: a heartbeat, and an answer to every request file.
func FakeUnity(t *testing.T, project string, answer func(cmd string, args map[string]any) (any, string)) {
	t.Helper()
	dir := unity.BridgeDir(project)
	_ = os.MkdirAll(dir, 0755)
	_ = os.MkdirAll(filepath.Join(project, "Packages", unity.PipePkg), 0755)
	alive := func() { // swapped in whole, as the plugin does: a reader never finds half a file
		tmp := filepath.Join(dir, "alive.tmp")
		_ = os.WriteFile(tmp, []byte(`{"bridge":"`+unity.EmbeddedPipelineVersion()+`","pid":1,"ma":"1.18.3","sdk":true,"skills":{"installed":true,"running":false}}`), 0644)
		_ = os.Rename(tmp, filepath.Join(dir, "alive.json"))
	}
	alive()
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			case <-time.After(15 * time.Millisecond):
			}
			alive()
			reqs, _ := filepath.Glob(filepath.Join(dir, "req_*.json"))
			for _, r := range reqs {
				run := filepath.Join(dir, "run_"+strings.TrimPrefix(filepath.Base(r), "req_"))
				if os.Rename(r, run) != nil {
					continue
				}
				r = run
				b, err := os.ReadFile(r)
				if err != nil {
					continue
				}
				var q struct {
					ID   string         `json:"id"`
					Cmd  string         `json:"cmd"`
					Args map[string]any `json:"args"`
				}
				if json.Unmarshal(b, &q) != nil {
					continue
				}
				res, errText := answer(q.Cmd, q.Args)
				if errText == "__drop__" { // taken and never answered
					_ = os.Remove(r)
					continue
				}
				out := map[string]any{"id": q.ID, "cmd": q.Cmd, "ok": errText == "", "result": res}
				if errText != "" {
					out["error"] = errText
				}
				j, _ := json.Marshal(out)
				_ = os.WriteFile(filepath.Join(dir, "res_"+q.ID+".tmp"), j, 0644)
				_ = os.Rename(filepath.Join(dir, "res_"+q.ID+".tmp"), filepath.Join(dir, "res_"+q.ID+".json"))
				_ = os.Remove(r)
			}
		}
	}()
	t.Cleanup(func() { close(done); wg.Wait() })
}
