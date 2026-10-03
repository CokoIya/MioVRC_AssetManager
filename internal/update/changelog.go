package update

import (
	"strings"

	"vrclib"
	"vrclib/internal/core"
)

type ChangeEntry struct {
	Version string   `json:"version"`
	Items   []string `json:"items"`
}

// Changelog: "## 1.6.0" starts a version, "- …" lines are its items.
func Changelog() []ChangeEntry {
	var out []ChangeEntry
	for _, line := range strings.Split(vrclib.ChangelogMD, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "## "):
			out = append(out, ChangeEntry{Version: strings.TrimSpace(strings.TrimPrefix(line, "## "))})
		case strings.HasPrefix(line, "- ") && len(out) > 0:
			e := &out[len(out)-1]
			e.Items = append(e.Items, strings.TrimSpace(line[2:]))
		}
	}
	return out
}

// WhatsNew: the versions since the notes were last shown, newest first (nil when there is nothing
// new). A library without a mark comes from before 1.6, which had no notes. Caller holds st.mu.
func WhatsNew(st *core.Store) []ChangeEntry {
	since := st.NotesSeen
	if since == "" {
		since = "1.5.99"
	}
	if !VersionNewer(core.AppVersion, since) {
		return nil
	}
	var out []ChangeEntry
	for _, e := range Changelog() {
		if VersionNewer(e.Version, since) && !VersionNewer(e.Version, core.AppVersion) && len(out) < 6 {
			out = append(out, e)
		}
	}
	return out
}
