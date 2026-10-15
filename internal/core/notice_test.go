package core

import (
	"encoding/binary"
	"math"
	"strings"
	"testing"
	"unicode/utf16"
)

// Every notice has its words in each language, the project named; and they fit the tray's notification (a title
// of 63 characters, a text of 255).
func TestNoticeText(t *testing.T) {
	for _, lang := range []string{"", "en", "ja"} {
		for _, what := range []string{"pipe", "import", "newproj", "update"} {
			for _, kind := range []string{"ask", "done", "fail"} {
				if kind == "ask" && (what == "newproj" || what == "update") {
					continue // (their questions are an import's)
				}
				n := Notice{Kind: kind, What: what, Name: "Manuka Project"}
				title, text := NoticeText(n, lang)
				if title == "" || title == AppName || !strings.Contains(text, "Manuka Project") {
					t.Errorf("%q %s %s: %q / %q", lang, what, kind, title, text)
				}
				if len(utf16.Encode([]rune(title))) > 63 || len(utf16.Encode([]rune(text))) > 255 {
					t.Errorf("%q %s %s: too long for the tray", lang, what, kind)
				}
				if lang != "" && strings.ContainsAny(title+text, "线导选择确认") { // (simplified forms only: Japanese has 必要, 確認)
					t.Errorf("%q %s %s: Chinese left in %q / %q", lang, what, kind, title, text)
				}
			}
		}
	}
	if title, text := NoticeText(Notice{Kind: "done", What: "pipe"}, ""); title != "流水线已完成" || text != "" {
		t.Errorf("no project: %q / %q", title, text)
	}
	if title, text := NoticeText(Notice{Kind: "ask", What: "pipe", Name: "P"}, "en"); title != "Your answer is needed" || text != "Project: P\nThe pipeline is waiting for you to confirm a step" {
		t.Errorf("en ask: %q / %q", title, text)
	}
	if title, text := NoticeText(Notice{Kind: "done", What: "update", Name: "Moon Dress"}, ""); title != "已更新到工程" || text != "素材：Moon Dress" {
		t.Errorf("an update: %q / %q", title, text)
	}
}

// The sounds are WAV files Windows plays as they are: 16-bit mono PCM, short, soft (a third of full scale), starting
// and ending in silence (no click), and not the same as one another.
func TestNoticeSound(t *testing.T) {
	seen := map[string]bool{}
	for _, kind := range []string{"done", "ask", "fail"} {
		b := NoticeSound(kind)
		if len(b) < 44 || string(b[:4]) != "RIFF" || string(b[8:16]) != "WAVEfmt " || string(b[36:40]) != "data" {
			t.Fatalf("%s: not a WAV file", kind)
		}
		le16 := func(at int) int { return int(binary.LittleEndian.Uint16(b[at:])) }
		le32 := func(at int) int { return int(binary.LittleEndian.Uint32(b[at:])) }
		if le16(20) != 1 || le16(22) != 1 || le32(24) != soundRate || le16(34) != 16 || le32(40) != len(b)-44 || le32(4) != len(b)-8 {
			t.Errorf("%s: header %v", kind, b[:44])
		}
		n := (len(b) - 44) / 2
		secs := float64(n) / soundRate
		peak, first, last := 0.0, 0.0, 0.0
		for i := 0; i < n; i++ {
			v := float64(int16(binary.LittleEndian.Uint16(b[44+2*i:]))) / 32767
			peak = math.Max(peak, math.Abs(v))
			if i == 0 {
				first = v
			}
			last = v
		}
		if secs < 0.5 || secs > 1.5 || peak < 0.3 || peak > 0.34 || first != 0 || last != 0 {
			t.Errorf("%s: %.2f s, peak %.3f, first %v, last %v", kind, secs, peak, first, last)
		}
		seen[string(b)] = true
	}
	if len(seen) != 3 {
		t.Error("two of the sounds are the same")
	}
	if string(NoticeSound("other")) != string(NoticeSound("done")) {
		t.Error("an unknown kind has no sound")
	}
}

// Notify writes its line in the log and hands the notice to the window, without waiting for it.
func TestNotify(t *testing.T) {
	got := make(chan Notice, 1)
	old := NoticeHook
	NoticeHook = func(n Notice) { got <- n }
	t.Cleanup(func() { NoticeHook = old })
	Notify(Notice{Kind: "done", What: "pipe", Name: "P"})
	if n := <-got; n != (Notice{Kind: "done", What: "pipe", Name: "P"}) {
		t.Errorf("handed %+v", n)
	}
	NoticeHook = nil
	Notify(Notice{Kind: "ask", What: "pipe"}) // (nobody to hand it to: no harm)
}
