package correction

import (
	"strings"
	"time"

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
// into a body, to next written the same way. The date is rewritten
// whenever it appears; the time only when it changed. It reports whether
// text changed.
//
// The frame the body was written in is not stored — the column holds UTC
// — so the user's zone is tried first and UTC second, and the rewrite
// happens in the first frame where previous appears. A body naming the
// instant in any other form is returned unchanged.
func FollowDate(text string, previous, next time.Time, zone *time.Location) (string, bool) {
	frames := []*time.Location{zone, time.UTC}
	// A frame where every moved part appears wins over one where only some
	// does: 10:00Z is 07:00 in UTC-3 on the same date, and taking that frame
	// on the date alone would leave "10:00" behind.
	for _, whole := range []bool{true, false} {
		for _, frame := range frames {
			p, n := previous.In(frame), next.In(frame)
			pd, nd := p.Format(textDateLayout), n.Format(textDateLayout)
			pt, nt := p.Format(textTimeLayout), n.Format(textTimeLayout)
			timeMoved := pt != nt
			hasDate, hasTime := hasToken(text, pd), timeMoved && hasToken(text, pt)
			found := hasDate || hasTime
			if whole {
				found = hasDate && (hasTime || !timeMoved)
			}
			if !found {
				continue
			}
			out := replaceToken(text, pd, nd)
			if timeMoved {
				out = replaceToken(out, pt, nt)
			}
			return out, out != text
		}
	}
	return text, false
}

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

// hasToken reports whether tok appears in s with no digit on either side.
func hasToken(s, tok string) bool {
	return replaceToken(s, tok, "\x00") != s
}

// replaceToken replaces every occurrence of tok in s that has no digit on
// either side, so 10:00 inside 110:00 is left alone.
func replaceToken(s, tok, with string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, tok)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		end := i + len(tok)
		bounded := (i == 0 || !isDigit(s[i-1])) && (end == len(s) || !isDigit(s[end]))
		b.WriteString(s[:i])
		if bounded {
			b.WriteString(with)
		} else {
			b.WriteString(tok)
		}
		s = s[end:]
	}
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
