package main

// The release history shipped inside the program: shown in the update window, and once after an
// update ("what changed in this version").

import (
	_ "embed"
	"strings"
)

//go:embed CHANGELOG.md
var changelogMD string

type ChangeEntry struct {
	Version string   `json:"version"`
	Items   []string `json:"items"`
}

// changelog: "## 1.6.0" starts a version, "- …" lines are its items.
func changelog() []ChangeEntry {
	var out []ChangeEntry
	for _, line := range strings.Split(changelogMD, "\n") {
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

// whatsNew: the versions since the notes were last shown, newest first (nil when there is nothing
// new). A library without a mark comes from before 1.6, which had no notes. Caller holds st.mu.
func whatsNew(st *Store) []ChangeEntry {
	since := st.NotesSeen
	if since == "" {
		since = "1.5.99"
	}
	if !versionNewer(appVersion, since) {
		return nil
	}
	var out []ChangeEntry
	for _, e := range changelog() {
		if versionNewer(e.Version, since) && !versionNewer(e.Version, appVersion) && len(out) < 6 {
			out = append(out, e)
		}
	}
	return out
}
