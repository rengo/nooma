package correction

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/rengo/nooma/internal/core/unit"
)

// The form capture asks the model to write a resolved instant in, inside
// normalized_content (classify.BuildPrompt). FollowDate rewrites exactly
// this form and no other: it is what the system itself resolved, so
// rewriting it needs no inference (doc 02 §5 step 4).
const (
	textDateLayout = "2006-01-02"
	textTimeLayout = "15:04"
)

// FollowDate rewrites, in text, the instant previous as capture writes it
// into a body, to next written the same way. It reports whether text
// changed.
//
// Only the stated instant is rewritten: a date token, and the time token
// that follows it within the same phrase (connected by "T", a space, " at "
// or " a las "). A time with no date before it is never touched — "gym
// daily at 10:00" names a habit, not this unit's instant — and neither is
// a range ("10:00-11:00") or anything inside a URL or a query value. A body
// with no date token for previous is returned unchanged: the residual of
// not inferring (doc 02 §5 step 4).
//
// The frame the body was written in is not stored — the column holds UTC
// — so the user's zone is tried first and UTC second. A frame where the
// date and its moved time both appear wins over one where only the date
// does: 10:00Z is 07:00 in UTC-3 on the same date.
func FollowDate(text string, previous, next time.Time, zone *time.Location) (string, bool) {
	for _, whole := range []bool{true, false} {
		for _, frame := range []*time.Location{zone, time.UTC} {
			p, n := previous.In(frame), next.In(frame)
			out, dates, times := rewriteInstant(text,
				p.Format(textDateLayout), n.Format(textDateLayout),
				p.Format(textTimeLayout), n.Format(textTimeLayout))
			timeMoved := p.Format(textTimeLayout) != n.Format(textTimeLayout)
			if dates == 0 || whole && timeMoved && times == 0 {
				continue
			}
			return out, out != text
		}
	}
	return text, false
}

// connectors join a date token to the time token of the same instant,
// longest first so " a las " is not read as " ".
var connectors = []string{" a las ", " a la ", " at ", "T", " "}

// rewriteInstant replaces every standalone date token pd with nd and, when
// a time token pt follows it through a connector, that time with nt. It
// counts the date tokens and the anchored time tokens it found.
func rewriteInstant(text, pd, nd, pt, nt string) (string, int, int) {
	var b strings.Builder
	dates, times := 0, 0
	rest, offset := text, 0
	for {
		i := strings.Index(rest, pd)
		if i < 0 {
			b.WriteString(rest)
			return b.String(), dates, times
		}
		end := i + len(pd)
		if !standalone(text, offset+i, offset+end) {
			b.WriteString(rest[:end])
			rest, offset = rest[end:], offset+end
			continue
		}
		dates++
		b.WriteString(rest[:i])
		b.WriteString(nd)
		for _, c := range connectors {
			t := end + len(c)
			if strings.HasPrefix(rest[end:], c) && strings.HasPrefix(rest[t:], pt) &&
				timeEnds(rest, t+len(pt)) {
				times++
				b.WriteString(c)
				b.WriteString(nt)
				end = t + len(pt)
				break
			}
		}
		rest, offset = rest[end:], offset+end
	}
}

// standalone reports whether text[i:j] is a date token of its own rather
// than part of something else:
//
//   - not glued inside a word or file name: no letter, digit, "-" or "_"
//     before it, and no digit, "-", "_" or "." followed by a letter or
//     digit after it ("minutes-2026-10-16.pdf", "2026-10-16-17");
//   - not part of a path or query: no URL mark on either side;
//   - not inside a word carrying a URL scheme.
func standalone(text string, i, j int) bool {
	if before, _ := utf8.DecodeLastRuneInString(text[:i]); i > 0 &&
		(unicode.IsLetter(before) || unicode.IsDigit(before) || before == '-' || before == '_' || isURLMark(text, i-1)) {
		return false
	}
	if j < len(text) {
		c := text[j]
		if isDigit(c) || c == '-' || c == '_' || isURLMark(text, j) ||
			c == '.' && j+1 < len(text) && isAlnum(text[j+1]) {
			return false
		}
	}
	start := strings.LastIndexAny(text[:i], " \t\n") + 1
	stop := strings.IndexAny(text[j:], " \t\n")
	if stop < 0 {
		stop = len(text) - j
	}
	return !strings.Contains(text[start:j+stop], "://")
}

// timeEnds reports whether a time token ending at j is not continued by a
// digit, a range dash, or a URL mark.
func timeEnds(s string, j int) bool {
	return j == len(s) || !isDigit(s[j]) && s[j] != '-' && !isURLMark(s, j)
}

// isURLMark reports whether s[k] belongs to a path or query. "/" and "="
// always do. "?", "#" and "&" are also sentence punctuation, so they count
// only when something follows them and the word around them is URL-shaped
// (holds a "/" or "="): "on 2026-10-16?" and "10:00#urgent" are prose.
func isURLMark(s string, k int) bool {
	switch s[k] {
	case '/', '=':
		return true
	case '?', '#', '&':
		if k+1 == len(s) || strings.IndexByte(" \t\n", s[k+1]) >= 0 {
			return false
		}
		start := strings.LastIndexAny(s[:k], " \t\n") + 1
		stop := strings.IndexAny(s[k:], " \t\n")
		if stop < 0 {
			stop = len(s) - k
		}
		return strings.ContainsAny(s[start:k+stop], "/=")
	}
	return false
}

func isAlnum(c byte) bool { return isDigit(c) || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// CarryText returns plan with, appended, the content edit its date edit
// carries: the unit's body rewritten by FollowDate from the edited
// column's previous value. plan is returned unchanged when it writes no
// date, when that column was empty, or when the body does not name it.
func CarryText(plan []Edit, u unit.Unit, zone *time.Location) []Edit {
	for _, e := range plan {
		var previous *time.Time
		var next time.Time
		if v, ok := e.EventAt(); ok {
			previous, next = u.EventAt, v
		} else if v, ok := e.DueAt(); ok {
			previous, next = u.DueAt, v
		}
		if previous == nil {
			continue
		}
		if body, changed := FollowDate(u.Content, *previous, next, zone); changed {
			return append(append([]Edit(nil), plan...), NewContentEdit(body))
		}
	}
	return plan
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
