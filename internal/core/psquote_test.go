package core

import "testing"

// psSingle: what PowerShell takes for a single quote — the ASCII one and the typographic ones.
func psSingle(r rune) bool { return r == '\'' || r >= '\u2018' && r <= '\u201b' }

// psRead reads a single-quoted string the way PowerShell's tokenizer does: it ends at a quote not followed by
// another; of two in a row the second is kept. What follows it is returned too (ok: the string was closed).
func psRead(lit string) (val, rest string, ok bool) {
	r := []rune(lit)
	if len(r) == 0 || !psSingle(r[0]) {
		return "", lit, false
	}
	var out []rune
	for i := 1; i < len(r); i++ {
		if psSingle(r[i]) {
			if i+1 < len(r) && psSingle(r[i+1]) {
				out = append(out, r[i+1])
				i++
				continue
			}
			return string(out), string(r[i+1:]), true
		}
		out = append(out, r[i])
	}
	return "", "", false
}

// A title, path or name put into a PowerShell script stays one value, whatever quote it holds: PowerShell ends a
// single-quoted string at a typographic quote (U+2018–U+201B) as much as at the ASCII one.
func TestPSQuote(t *testing.T) {
	for in, want := range map[string]string{
		"":                      "''",
		`D:\VRC\Avatars`:        `'D:\VRC\Avatars'`,
		"it's":                  "'it''s'",
		"a\u2018b\u2019c":       "'a\u2018\u2018b\u2019\u2019c'",
		"\u201a\u201b":          "'\u201a\u201a\u201b\u201b'",
		"\u201c引号\u201d $env:x": "'\u201c引号\u201d $env:x'", // (double quotes and $ mean nothing in a single-quoted string)
	} {
		if got := psQuote(in); got != want {
			t.Errorf("psQuote(%q) = %q, want %q", in, got, want)
		}
	}
	for _, s := range []string{
		"plain", "", "'", "''", "x'", "'x", "\u2019; Start-Process calc; \u2019", "\u2018) ; calc ; (\u2018", "a\u201a\u201bb", "\u2019'\u2018", "x\u2019\u2019",
		`C:\Users\O'Brien\Desktop\MioVRC_AssetManager.lnk`, "名前\u2019s 衣装 (1)", "\u201c\u201d\u201e\u201f",
	} {
		lit := psQuote(s) + "; Remove-Item x"
		val, rest, ok := psRead(lit)
		if !ok || val != s || rest != "; Remove-Item x" {
			t.Errorf("%q: PowerShell reads %q, then %q", s, val, rest)
		}
	}
}
