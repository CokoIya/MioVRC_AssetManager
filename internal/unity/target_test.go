package unity

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// What the project builds for is what the plugin's heartbeat last said: fresh or hours old (Unity is closed),
// with a BOM in front; not known without one, or from a plugin that does not say it.
func TestProjectTarget(t *testing.T) {
	p := t.TempDir()
	if got := ProjectTarget(p); got != "" {
		t.Errorf("no heartbeat: %q", got)
	}
	alive := filepath.Join(BridgeDir(p), "alive.json")
	for _, c := range []struct {
		body string
		age  time.Duration
		want string
	}{
		{`{"bridge":"1.4.1","pid":4242,"target":"quest","skills":{"installed":true}}`, 0, "quest"},
		{`{"bridge":"1.4.1","target":"quest"}`, 3 * time.Hour, "quest"},
		{`{"bridge":"1.4.1","target":"pc"}`, 3 * time.Hour, "pc"},
		{"\xef\xbb\xbf" + `{"bridge":"1.4.1","target":"quest"}`, 0, "quest"},
		{`{"bridge":"1.4.0","pid":4242}`, 0, ""},
		{`{"target":"switch"}`, 0, ""},
		{`{"bridge":"1.4.1","tar`, 0, ""}, // half written
	} {
		put(t, alive, []byte(c.body))
		at := time.Now().Add(-c.age)
		_ = os.Chtimes(alive, at, at)
		if got := ProjectTarget(p); got != c.want {
			t.Errorf("%s (%v old): %q, want %q", c.body, c.age, got, c.want)
		}
	}
}

// The plugin's own file of the target stays when the editor quits (the heartbeat is removed then): it is read
// first, and the heartbeat only without it.
func TestProjectTargetAfterUnityQuit(t *testing.T) {
	p := t.TempDir()
	put(t, filepath.Join(BridgeDir(p), "target"), []byte("quest"))
	if got := ProjectTarget(p); got != "quest" {
		t.Errorf("target file alone: %q", got)
	}
	put(t, filepath.Join(BridgeDir(p), "alive.json"), []byte(`{"bridge":"1.4.2","target":"pc"}`))
	if got := ProjectTarget(p); got != "quest" {
		t.Errorf("with a heartbeat: %q", got)
	}
	put(t, filepath.Join(BridgeDir(p), "target"), []byte("ps5"))
	if got := ProjectTarget(p); got != "pc" {
		t.Errorf("a target file that says nothing known: %q", got)
	}
}
