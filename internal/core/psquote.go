package core

import "strings"

// psQuotes: what ends a single-quoted string in PowerShell — the ASCII quote and the typographic ones
// (U+2018–U+201B) alike — each written twice, which PowerShell reads as that one character.
var psQuotes = strings.NewReplacer("'", "''", "\u2018", "\u2018\u2018", "\u2019", "\u2019\u2019", "\u201a", "\u201a\u201a", "\u201b", "\u201b\u201b")

// psQuote: s as a single-quoted PowerShell string, for the scripts the Windows dialogs and the shortcut are made
// with (a title, a path, a name): whatever quote it holds, it stays one value.
func psQuote(s string) string { return "'" + psQuotes.Replace(s) + "'" }
