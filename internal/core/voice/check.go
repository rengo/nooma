package voice

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Rejection is why a wording was not sent, or Accepted. Brain records it.
type Rejection string

// The reasons a wording is not sent. The draft is sent instead.
const (
	Accepted        Rejection = ""
	RejectEmpty     Rejection = "empty"
	RejectEmoji     Rejection = "emoji"
	RejectMarkup    Rejection = "markup"
	RejectLength    Rejection = "length"
	RejectQuestion  Rejection = "question"
	RejectSentences Rejection = "sentences"
	RejectKeep      Rejection = "keep"
	// RejectFact: a number, a date, or a day, month or time word outside a slot.
	RejectFact Rejection = "fact"
	// RejectSlot: a slot unknown, missing, repeated, or out of its item's place.
	RejectSlot Rejection = "slot"
	// RejectWord: a word that is not a common word nor the draft's, the
	// message's or the items' own.
	RejectWord Rejection = "word"
)

// Check is what gets sent for the model's output: the output with its slots
// filled, when it holds, else draft. The fallback is the return value, so no
// caller can send a rejected wording by forgetting it (I56).
//
// The model never writes a fact. It writes {n} for item n's own words, {n.when}
// for its day and time, and {count} for how many items there are, and fill puts
// in the same words the fixed text would. So a swapped, borrowed or miscounted
// day cannot be written: a date, a number or a day word outside a slot is
// RejectFact, a slot out of place is RejectSlot, and a word that is neither a
// common word of the language nor one of the draft's, the message's or the
// items' is RejectWord, which keeps an invented item out as well.
func Check(f Facts, draft, output string) (string, Rejection) {
	out := unquote(output)
	sent, why := check(f, draft, out)
	if why != Accepted {
		return draft, why
	}
	return sent, Accepted
}

func check(f Facts, draft, out string) (string, Rejection) {
	switch {
	case out == "":
		return "", RejectEmpty
	case hasEmoji(out):
		return "", RejectEmoji
	case hasMarkup(out) || bullets(out) > max(bullets(draft), len(f.Items)):
		return "", RejectMarkup
	case strings.Count(out, "?") > strings.Count(draft, "?"):
		return "", RejectQuestion
	}
	slots, rest, ok := parseSlots(f, out)
	switch {
	case !ok:
		return "", RejectSlot
	case sentences(rest) > max(MaxSentences, sentences(draft)):
		return "", RejectSentences
	}
	for _, k := range f.Keep {
		if !strings.Contains(rest, k) {
			return "", RejectKeep
		}
		rest = strings.ReplaceAll(rest, k, " ")
	}
	v := vocabularyFor(f.Language)
	if why := v.words(f, draft, rest); why != Accepted {
		return "", why
	}
	if !placed(f, v, out, slots) {
		return "", RejectSlot
	}
	sent := fill(f, out, slots)
	if utf8.RuneCountInString(sent) > max(MinRunes, RunesFactor*utf8.RuneCountInString(draft)) {
		return "", RejectLength
	}
	return sent, Accepted
}

// bullets counts the list lines of text.
func bullets(text string) int {
	n := 0
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "•") {
			n++
		}
	}
	return n
}

// sentences counts the sentences of text outside list lines.
func sentences(text string) int {
	n := 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "•") || strings.HasPrefix(line, "-") {
			continue
		}
		rs, open := []rune(line), false
		for i, c := range rs {
			switch {
			case strings.ContainsRune(".!?…", c):
				if (i+1 == len(rs) || unicode.IsSpace(rs[i+1])) && open {
					n, open = n+1, false
				}
			case !unicode.IsSpace(c):
				open = true
			}
		}
		if open {
			n++
		}
	}
	return n
}

// hasEmoji reports a pictograph, a technical symbol (⏰ ⌛), a dingbat, or an emoji
// joiner or selector.
func hasEmoji(s string) bool {
	for _, r := range s {
		if (r >= 0x1F000 && r <= 0x1FAFF) || (r >= 0x2300 && r <= 0x23FF) || (r >= 0x2600 && r <= 0x27BF) || (r >= 0x2B00 && r <= 0x2BFF) ||
			r == 0xFE0F || r == 0x200D || r == 0x20E3 {
			return true
		}
	}
	return false
}

// hasMarkup reports markdown or JSON: Telegram is sent plain text, which shows
// either as written. A list is "•" lines, as the fixed text writes it.
func hasMarkup(s string) bool {
	if strings.HasPrefix(s, "{\"") || strings.HasPrefix(s, "[") {
		return true
	}
	for _, m := range []string{"*", "__", "`", "]("} {
		if strings.Contains(s, m) {
			return true
		}
	}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "- ") {
			return true
		}
	}
	return false
}

// unquote trims s and one pair of quotes wrapping the whole of it.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	rs := []rune(s)
	for _, q := range [][2]rune{{'"', '"'}, {'“', '”'}, {'«', '»'}, {'\'', '\''}} {
		if len(rs) >= 2 && rs[0] == q[0] && rs[len(rs)-1] == q[1] {
			inner := string(rs[1 : len(rs)-1])
			if !strings.ContainsRune(inner, q[0]) && !strings.ContainsRune(inner, q[1]) {
				return strings.TrimSpace(inner)
			}
		}
	}
	return s
}
