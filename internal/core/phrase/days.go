package phrase

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Moment is an instant as a reply names it (ADR-0064 point 2).
type Moment struct {
	At      time.Time // read in now's zone
	DayOnly bool      // a day alone: no time is said
	Due     bool      // a task's due day, said as "para ..." / "due ..."
}

// Item is one thing a reply names: its title, else its content, and its
// instant when it has one. Names is the words of the unit's title and of its
// content, each text without its own first word, read only to tell a name
// from an ordinary first word (Inline, ADR-0064 point 3).
type Item struct {
	Text  string
	When  *Moment
	Names string
}

// dayOffset counts calendar days from now to t in now's zone.
func dayOffset(now, t time.Time) int {
	civil := func(x time.Time) time.Time {
		x = x.In(now.Location())
		return time.Date(x.Year(), x.Month(), x.Day(), 0, 0, 0, 0, time.UTC)
	}
	return int(civil(t).Sub(civil(now)).Hours() / 24)
}

// relative names the days around now, and "" for any other.
func (s Set) relative(offset int) string {
	switch offset {
	case -1:
		return s.Yesterday
	case 0:
		return s.TodayWord
	case 1:
		return s.Tomorrow
	case 2:
		return s.DayAfter
	}
	return ""
}

// Day names m's day as a noun phrase: "mañana", "el jueves 26 de noviembre".
func (s Set) Day(m Moment, now time.Time) string {
	t := m.At.In(now.Location())
	if word := s.relative(dayOffset(now, t)); word != "" {
		return word
	}
	day := fmt.Sprintf(s.OnDate, s.DayNames[t.Weekday()], t.Day(), s.MonthNames[t.Month()-1])
	if t.Year() != now.Year() {
		day += fmt.Sprintf(s.YearSuffix, t.Year())
	}
	return day
}

// Clock says an hour: "a las 8:30", "a la 1:00".
func (s Set) Clock(t time.Time) string {
	frame := s.At
	if t.Hour() == 1 {
		frame = s.AtOne
	}
	return fmt.Sprintf(frame, fmt.Sprintf("%d:%02d", t.Hour(), t.Minute()))
}

// past says whether m is behind now: a timed instant before it, a day alone
// before now's day.
func past(m Moment, now time.Time) bool {
	t := m.At.In(now.Location())
	if m.DayOnly {
		return dayOffset(now, t) < 0
	}
	return t.Before(now)
}

// said is m's adverbial day and time. tensed adds the past of a due day that
// has gone ("era para ayer"); a caller whose verb already carries the tense
// leaves it out.
func (s Set) said(m Moment, now time.Time, tensed bool) string {
	t := m.At.In(now.Location())
	day := s.Day(m, now)
	switch {
	case m.Due:
		frame := s.DueFuture
		if tensed && past(m, now) {
			frame = s.DuePast
		}
		day = fmt.Sprintf(frame, day)
	case s.relative(dayOffset(now, t)) == "":
		day = fmt.Sprintf(s.OnDay, day)
	}
	if m.DayOnly {
		return day
	}
	return day + " " + s.Clock(t)
}

// When says m as an adverb with its time: "mañana a las 10:00", "on Thursday
// 26 November at 8:30"; a due day as due, in the past once it has gone.
func (s Set) When(m Moment, now time.Time) string { return s.said(m, now, true) }

// Inline is the item's words as they stand inside a sentence. Spanish lowers
// the first letter, since no frame can know the item's gender or whether its
// words are a noun or a verb, unless the first word is an acronym or is
// written capitalised in Names. A trailing period goes.
func (s Set) Inline(it Item) string {
	text := strings.TrimSuffix(strings.TrimSpace(it.Text), ".")
	if !s.LowerFirst {
		return text
	}
	first, size := utf8.DecodeRuneInString(text)
	if !unicode.IsLetter(first) {
		return text
	}
	word := firstWord(text)
	if upper(word) > 1 || upper(word) == 1 && slices.Contains(letterWords(it.Names), word) {
		return text
	}
	return string(unicode.ToLower(first)) + text[size:]
}

// upper counts the upper-case letters of w.
func upper(w string) int {
	n := 0
	for _, r := range w {
		if unicode.IsUpper(r) {
			n++
		}
	}
	return n
}

func letterWords(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) })
}

func firstWord(text string) string {
	if ws := letterWords(text); len(ws) > 0 {
		return ws[0]
	}
	return ""
}

// Recall words a recall's answer from what was found, in the past tense for
// an item whose instant is behind now (ADR-0064 points 1 and 4).
func (s Set) Recall(items []Item, now time.Time) string {
	switch len(items) {
	case 0:
		return s.NothingFound
	case 1:
		it := items[0]
		if it.When == nil {
			return fmt.Sprintf(s.RecallOneUndated, s.Inline(it))
		}
		frame := s.RecallOne
		if past(*it.When, now) {
			frame = s.RecallOnePast
		}
		return fmt.Sprintf(frame, s.Inline(it), s.said(*it.When, now, false))
	}
	var b strings.Builder
	fmt.Fprintf(&b, s.RecallMany, len(items))
	for _, it := range items {
		b.WriteString("\n" + fmt.Sprintf(s.DigestLine, s.line(it, now)))
	}
	return b.String()
}

// line is an item on a list: its words as stored, then its day and time.
func (s Set) line(it Item, now time.Time) string {
	if it.When == nil {
		return strings.TrimSpace(it.Text)
	}
	text := strings.TrimSuffix(strings.TrimSpace(it.Text), ".")
	return fmt.Sprintf(s.ItemWhen, text, s.When(*it.When, now))
}

// Push words a reminder: the item's words and, when it has an instant, its day
// and time as a person says them (ADR-0064 point 1).
func (s Set) Push(it Item, now time.Time) string {
	words := s.Inline(it)
	if it.When != nil {
		words = fmt.Sprintf(s.ItemWhen, words, s.When(*it.When, now))
	}
	return fmt.Sprintf(s.PushFrame, words)
}

// Ring is a ringing timer's fixed sentence around its words (ADR-0064 point 7).
func (s Set) Ring(words string) string {
	return fmt.Sprintf(s.TimerRing, s.Inline(Item{Text: words}))
}

// NamesOf is Item.Names for a unit: its title and its content, each without
// its own first word.
func NamesOf(title, content string) string {
	past := func(text string) string {
		if words := strings.Fields(text); len(words) > 0 {
			return strings.Join(words[1:], " ")
		}
		return ""
	}
	return past(title) + " " + past(content)
}
