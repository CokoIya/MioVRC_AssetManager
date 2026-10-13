package unity

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
)

// ProjectTarget: the platform the project builds for, as the pipeline plugin last said it — "quest" (Android
// or iOS: the upload for Quest), "pc", or "" when it is not known (the project was never open with a plugin
// that says it). The plugin keeps it in a file of its own, which stays when the editor quits; its heartbeat has
// it too (an older copy of the plugin's file may be missing). What Unity last built for stays so while it is closed.
func ProjectTarget(p string) string {
	if b, err := os.ReadFile(filepath.Join(BridgeDir(p), "target")); err == nil {
		if t := string(bytes.TrimSpace(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")))); t == "quest" || t == "pc" {
			return t
		}
	}
	b, err := os.ReadFile(filepath.Join(BridgeDir(p), "alive.json"))
	if err != nil {
		return ""
	}
	var a struct {
		Target string `json:"target"`
	}
	if json.Unmarshal(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), &a) != nil {
		return ""
	}
	if a.Target == "quest" || a.Target == "pc" {
		return a.Target
	}
	return ""
}
