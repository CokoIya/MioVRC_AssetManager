package netdisk

import (
	"encoding/json"
	"regexp"
	"strings"
)

type BDShare struct {
	ShareUK  json.Number     `json:"share_uk"`
	ShareID  json.Number     `json:"shareid"`
	Title    string          `json:"title"`
	RawList  json.RawMessage `json:"file_list"`
	FileList []PanRaw        `json:"-"`
}

var reMset = regexp.MustCompile(`locals\.mset\(`)

// ParseSharePage reads the share's data from its web page (two layouts are in use).
func ParseSharePage(page []byte) *BDShare {
	try := func(js []byte) *BDShare {
		var s BDShare
		if json.NewDecoder(strings.NewReader(string(js))).Decode(&s) != nil || s.ShareID.String() == "" {
			return nil
		}
		if json.Unmarshal(s.RawList, &s.FileList) != nil {
			var wrapped struct {
				List []PanRaw `json:"list"`
			}
			_ = json.Unmarshal(s.RawList, &wrapped)
			s.FileList = wrapped.List
		}
		return &s
	}
	if m := reLocals.FindSubmatch(page); m != nil {
		if s := try(m[1]); s != nil {
			return s
		}
	}
	if loc := reMset.FindIndex(page); loc != nil {
		if s := try(page[loc[1]:]); s != nil {
			return s
		}
	}
	// other wrappers ("try{{…}}"): the object around the first "shareid"
	if i := strings.Index(string(page), `"shareid"`); i > 0 {
		for k, tries := i, 0; k > 0 && tries < 60; k-- {
			if page[k] == '{' {
				tries++
				if s := try(page[k:]); s != nil && len(s.FileList) > 0 {
					return s
				}
			}
		}
	}
	return nil
}
