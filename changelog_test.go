package main

import "testing"

func TestChangelog(t *testing.T) {
	cl := changelog()
	if len(cl) == 0 || cl[0].Version != appVersion || len(cl[0].Items) == 0 {
		t.Fatalf("CHANGELOG.md has no entry for %s at the top: %+v", appVersion, cl)
	}
	st := &Store{}
	wn := whatsNew(st)
	if len(wn) == 0 || wn[0].Version != appVersion {
		t.Fatalf("from before 1.6: %+v", wn)
	}
	for _, e := range wn {
		if !versionNewer(e.Version, "1.5.99") {
			t.Errorf("shows %s, older than 1.6", e.Version)
		}
	}
	st.NotesSeen = cl[1].Version // the version before this one
	if wn := whatsNew(st); len(wn) != 1 || wn[0].Version != appVersion {
		t.Errorf("one version since: %+v", wn)
	}
	st.NotesSeen = appVersion
	if whatsNew(st) != nil {
		t.Error("shown once already")
	}
	st.NotesSeen = "99.0.0"
	if whatsNew(st) != nil {
		t.Error("a later version was run before: no popup for an older one")
	}
}
