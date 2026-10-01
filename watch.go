package main

// New downloads while the app is open: the asset folders are looked at every few seconds (names,
// sizes and times a few levels deep). Once a change has settled — no browser or netdisk download
// still in progress — the folders are scanned again and the new assets show up on their own.

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// names of files that are still being downloaded
var partialSuffixes = []string{".crdownload", ".part", ".partial", ".download", ".tmp", ".!ut", ".baiduyun.downloading",
	".baiduyun.p.downloading", ".bc!", ".opdownload", ".xltd", ".td"}

func isPartialDownload(name string) bool {
	l := strings.ToLower(name)
	for _, s := range partialSuffixes {
		if strings.HasSuffix(l, s) {
			return true
		}
	}
	return false
}

// rootsFingerprint sums up the asset folders down to a few levels; partial is true while
// something is still downloading.
func rootsFingerprint(roots []string, skip string) (fp string, partial bool) {
	h := sha1.New()
	budget := 30000
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		ents, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range ents {
			if budget <= 0 {
				return
			}
			budget--
			n := e.Name()
			if strings.HasPrefix(n, ".") || strings.HasPrefix(n, "~$") {
				continue
			}
			p := filepath.Join(dir, n)
			if skip != "" && strings.EqualFold(filepath.Clean(p), skip) {
				continue // the library's own data folder
			}
			if isPartialDownload(n) {
				partial = true
			}
			fi, err := e.Info()
			if err != nil {
				continue
			}
			h.Write([]byte(p))
			h.Write([]byte(strconv.FormatInt(fi.ModTime().UnixNano(), 36)))
			if !e.IsDir() {
				h.Write([]byte(strconv.FormatInt(fi.Size(), 36)))
			} else if depth < 3 && !skipDirNames[strings.ToLower(n)] {
				walk(p, depth+1)
			}
		}
	}
	for _, r := range roots {
		h.Write([]byte(r))
		walk(r, 1)
	}
	return hex.EncodeToString(h.Sum(nil)), partial
}

// watchRoots runs for the life of the app.
func watchRoots(st *Store) {
	const every = 8 * time.Second
	last, pendingSince := "", time.Time{}
	for {
		time.Sleep(every)
		st.mu.RLock()
		roots, off, ready := append([]string{}, st.Settings.Roots...), st.Settings.NoWatch, st.Settings.SetupDone
		st.mu.RUnlock()
		if off || !ready || len(roots) == 0 {
			last = ""
			continue
		}
		fp, partial := rootsFingerprint(roots, filepath.Clean(dataDir))
		switch {
		case last == "":
			last = fp // first look: nothing to compare with
		case fp != last:
			last, pendingSince = fp, time.Now() // changed: wait until it settles
		case !pendingSince.IsZero() && !partial && time.Since(pendingSince) >= 2*every && !pipelineBusy():
			pendingSince = time.Time{}
			st.mu.RLock()
			auto := st.Settings.AutoBooth
			st.mu.RUnlock()
			logf("素材文件夹有变化，重新扫描")
			StartPipeline(st, true, true, auto, false, nil)
		}
	}
}
