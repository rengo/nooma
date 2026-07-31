package recall

import (
	"strings"
	"unicode"
)

// Tokenize splits text into the lowercase word tokens the lexical leg
// searches for (design D5 — recall.Tokenize is core, because it is the
// recall-quality decision the golden corpus pins, and it is pure;
// rendering those tokens as FTS5 MATCH syntax belongs to store/sqlite,
// which does not decide what a word is).
//
// A token is a maximal run of letters or digits; every other rune is a
// separator, discarded rather than kept as part of a token, so punctuation
// never leaks into what the lexical leg searches for.
func Tokenize(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}
