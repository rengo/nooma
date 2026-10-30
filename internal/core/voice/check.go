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
)

// Check is what gets sent for the model's output: the output, unquoted, when
// its shape holds, else draft. The fallback is the return value, so no caller
// can send a rejected wording by forgetting it (I56).
func Check(f Facts, draft, output string) (string, Rejection) {
	out := unquote(output)
	if why := reject(f, draft, out); why != Accepted {
		return draft, why
	}
	return out, Accepted
}

func reject(f Facts, draft, out string) Rejection {
	switch {
	case out == "":
		return RejectEmpty
	case hasEmoji(out):
		return RejectEmoji
	case hasMarkup(out):
		return RejectMarkup
	case utf8.RuneCountInString(out) > max(MinRunes, RunesFactor*utf8.RuneCountInString(draft)):
		return RejectLength
	case strings.Count(out, "?") > strings.Count(draft, "?"):
		return RejectQuestion
	case sentences(out) > max(MaxSentences, sentences(draft)):
		return RejectSentences
	}
	for _, k := range f.Keep {
		if !strings.Contains(out, k) {
			return RejectKeep
		}
	}
	return Accepted
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

// hasEmoji reports a pictograph, a dingbat, or an emoji joiner or selector.
func hasEmoji(s string) bool {
	for _, r := range s {
		if (r >= 0x1F000 && r <= 0x1FAFF) || (r >= 0x2600 && r <= 0x27BF) || (r >= 0x2B00 && r <= 0x2BFF) ||
			r == 0xFE0F || r == 0x200D || r == 0x20E3 {
			return true
		}
	}
	return false
}

// hasMarkup reports markdown or JSON: Telegram is sent plain text, which shows
// either as written.
func hasMarkup(s string) bool {
	if strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[") {
		return true
	}
	for _, m := range []string{"**", "__", "`", "]("} {
		if strings.Contains(s, m) {
			return true
		}
	}
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
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
