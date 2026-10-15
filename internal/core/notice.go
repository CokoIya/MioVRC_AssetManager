package core

import (
	"bytes"
	"encoding/binary"
	"math"
	"sync"
	"time"
)

// What the program tells the player about while the player may be looking elsewhere — at Unity, or away from the
// computer: something it waits for the player's answer to, and the end of a long piece of work (a run of the
// pipeline, an import, a new project, an update put into projects). The window plays a sound for it (unless
// 「提示音」 is off in the settings) and, when it is not in front, flashes its taskbar button — or, hidden in the
// tray, says it in a notification there.

// A Notice: Kind "ask" (the program waits for the player's answer), "done" or "fail" (a long piece of work ended,
// well or not); What "pipe" (the pipeline, the AI, a saved recipe), "import", "newproj" (a new project), "update"
// (an asset's new version downloaded and put into its projects), or "test" (the settings' 「试听」: the sounds
// alone); Name the project it is about, if any (for "update": the asset).
type Notice struct {
	Kind, What, Name string
}

// NoticeHook does what the window does with a notice: set by the program on Windows, nil elsewhere (and in tests
// that do not set it).
var NoticeHook func(Notice)

// Notify: a notice for the player; it is written to the log too, so a report of a sound not heard can be
// looked into.
func Notify(n Notice) {
	Logf("提醒：%s %s %s", n.What, n.Kind, n.Name)
	if h := NoticeHook; h != nil {
		go h(n)
	}
}

// NoticeLongEnough: the end of a piece of work that took this long is told; one that ended at once ended while the
// player was looking.
var NoticeLongEnough = 15 * time.Second // (a variable: tests take it down)

// NoticeText: the notification's title and line in the interface's language ("" Chinese, "en", "ja").
func NoticeText(n Notice, lang string) (title, text string) {
	type pair struct{ title, text string }
	var t map[string]pair
	switch lang {
	case "en":
		t = map[string]pair{
			"pipe ask":     {"Your answer is needed", "The pipeline is waiting for you to confirm a step"},
			"pipe done":    {"Pipeline finished", ""},
			"pipe fail":    {"The pipeline stopped", "See the record in the window for why"},
			"import ask":   {"Your choice is needed", "Choose which unitypackage to import"},
			"import done":  {"Import finished", ""},
			"import fail":  {"The import failed", "See the window for why"},
			"newproj done": {"Project created", ""},
			"newproj fail": {"The project could not be created", "See the window for why"},
			"update done":  {"The update is in the projects", ""},
			"update fail":  {"The update did not finish", "See the window for why"},
		}
	case "ja":
		t = map[string]pair{
			"pipe ask":     {"確認が必要です", "パイプラインが操作の確認を待っています"},
			"pipe done":    {"パイプラインが完了しました", ""},
			"pipe fail":    {"パイプラインが中断しました", "理由はウィンドウの記録で確認できます"},
			"import ask":   {"選択が必要です", "インポートする unitypackage を選んでください"},
			"import done":  {"インポートが完了しました", ""},
			"import fail":  {"インポートに失敗しました", "理由はウィンドウで確認できます"},
			"newproj done": {"プロジェクトを作成しました", ""},
			"newproj fail": {"プロジェクトを作成できませんでした", "理由はウィンドウで確認できます"},
			"update done":  {"プロジェクトへの更新が完了しました", ""},
			"update fail":  {"プロジェクトへの更新が完了しませんでした", "理由はウィンドウで確認できます"},
		}
	default:
		t = map[string]pair{
			"pipe ask":     {"需要你确认", "流水线正在等待你确认一项操作"},
			"pipe done":    {"流水线已完成", ""},
			"pipe fail":    {"流水线已中断", "可在窗口的记录中查看原因"},
			"import ask":   {"需要你选择", "请选择要导入的 unitypackage"},
			"import done":  {"导入已完成", ""},
			"import fail":  {"导入失败", "可在窗口中查看原因"},
			"newproj done": {"工程创建完成", ""},
			"newproj fail": {"工程创建失败", "可在窗口中查看原因"},
			"update done":  {"已更新到工程", ""},
			"update fail":  {"更新到工程未完成", "可在窗口中查看原因"},
		}
	}
	p, ok := t[n.What+" "+n.Kind]
	if !ok {
		return AppName, ""
	}
	title, text = p.title, p.text
	if n.Name != "" {
		of := map[string]string{"en": "Project: ", "ja": "プロジェクト：", "": "工程："}[lang]
		if n.What == "update" {
			of = map[string]string{"en": "Asset: ", "ja": "アセット：", "": "素材："}[lang]
		}
		if text == "" {
			text = of + n.Name
		} else {
			text = of + n.Name + "\n" + text
		}
	}
	return title, text
}

// ---------- the sounds ----------

// NoticeSound: the sound of a notice of that kind, as a WAV file (16-bit mono PCM): two soft notes rising for
// "done", the same note twice — a knock — for "ask", two falling for "fail". Made here, not shipped: they play
// whatever sound scheme Windows is set to (a player with 「无声」 would otherwise hear nothing), and at a level
// that does not startle (a third of full scale).
func NoticeSound(kind string) []byte {
	soundsOnce.Do(func() {
		const c6, e6, g6, a5, e5 = 1046.50, 1318.51, 1567.98, 880.00, 659.25
		sounds = map[string][]byte{
			"done": wav([]note{{0, c6}, {0.13, g6}}, 0.85),
			"ask":  wav([]note{{0, e6}, {0.16, e6}, {0.55, e6}, {0.71, e6}}, 1.25),
			"fail": wav([]note{{0, a5}, {0.16, e5}}, 0.9),
		}
	})
	if b, ok := sounds[kind]; ok {
		return b
	}
	return sounds["done"]
}

var (
	soundsOnce sync.Once
	sounds     map[string][]byte
)

type note struct{ at, hz float64 } // when it starts (seconds), its pitch

const soundRate = 44100

// wav renders the notes — each a sine with a little of its octave and twelfth, struck and dying away like a soft
// bell — into a WAV file of the given length in seconds.
func wav(notes []note, secs float64) []byte {
	n := int(secs * soundRate)
	pcm := make([]float64, n)
	for _, nt := range notes {
		start := int(nt.at * soundRate)
		for i := start; i < n; i++ {
			t := float64(i-start) / soundRate
			env := math.Exp(-t/0.16) * math.Min(1, t/0.008) // 8 ms attack: struck softly, no click
			w := 2 * math.Pi * nt.hz * t
			pcm[i] += env * (math.Sin(w) + 0.3*math.Sin(2*w)*math.Exp(-t/0.05) + 0.12*math.Sin(3*w)*math.Exp(-t/0.03))
		}
	}
	peak := 0.0
	for _, v := range pcm {
		peak = math.Max(peak, math.Abs(v))
	}
	scale := 0.0
	if peak > 0 {
		scale = 0.33 * 32767 / peak
	}
	fade := int(0.03 * soundRate) // the last 30 ms go to silence
	var data bytes.Buffer
	for i, v := range pcm {
		if left := n - i; left < fade {
			v *= float64(left) / float64(fade)
		}
		_ = binary.Write(&data, binary.LittleEndian, int16(math.Round(v*scale)))
	}
	var b bytes.Buffer
	le := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	b.WriteString("RIFF")
	le(uint32(36 + data.Len()))
	b.WriteString("WAVEfmt ")
	le(uint32(16))
	le(uint16(1)) // PCM
	le(uint16(1)) // mono
	le(uint32(soundRate))
	le(uint32(soundRate * 2))
	le(uint16(2))
	le(uint16(16))
	b.WriteString("data")
	le(uint32(data.Len()))
	b.Write(data.Bytes())
	return b.Bytes()
}
