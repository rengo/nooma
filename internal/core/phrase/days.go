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
	// Unnamed marks a unit with an instant and no title: it is said as
	// something noted at that instant, never by its content, which already
	// carries the date (ADR-0065 point 5). Text is empty.
	Unnamed bool
}

// ItemOf is the item a reply names for a unit: its title, else its content. A
// unit with an instant and no title is Unnamed; one with neither keeps its
// content, which states no instant, so nothing repeats.
func ItemOf(title, content string, when *Moment) Item {
	it := Item{Text: title, When: when, Names: NamesOf(title, content)}
	switch {
	case it.Text != "":
	case when != nil:
		it.Unnamed = true
	default:
		it.Text = content
	}
	return it
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

// Dated is m as a noun phrase with its time, for a sentence that names what a
// thing is for or moved to: "mañana a las 11:00", "Thursday 12 November at
// 10:00". A day alone has no time.
func (s Set) Dated(m Moment, now time.Time) string {
	day := s.Day(m, now)
	if m.DayOnly {
		return day
	}
	return day + " " + s.Clock(m.At.In(now.Location()))
}

// Hour writes an hour as the replies do, 24-hour with no leading zero.
func (s Set) Hour(t time.Time) string { return fmt.Sprintf("%d:%02d", t.Hour(), t.Minute()) }

// Clock says an hour: "a las 8:30", "a la 1:00".
func (s Set) Clock(t time.Time) string {
	frame := s.At
	if t.Hour() == 1 {
		frame = s.AtOne
	}
	return fmt.Sprintf(frame, s.Hour(t))
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
	// An opening ¿ or ¡ is not the first letter: the word after it is.
	open := len(text) - len(strings.TrimLeft(text, "¿¡"))
	first, size := utf8.DecodeRuneInString(text[open:])
	if !unicode.IsLetter(first) {
		return text
	}
	word := firstWord(text)
	if upper(word) > 1 || upper(word) == 1 && slices.Contains(letterWords(it.Names), word) {
		return text
	}
	return text[:open] + string(unicode.ToLower(first)) + text[open+size:]
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
			return settled(fmt.Sprintf(s.RecallOneUndated, s.Inline(it)))
		}
		frame, unnamed := s.RecallOne, s.RecallOneUnnamed
		if past(*it.When, now) {
			frame, unnamed = s.RecallOnePast, s.RecallOnePastUnnamed
		}
		if it.Unnamed {
			return settled(fmt.Sprintf(unnamed, s.said(*it.When, now, false)))
		}
		return settled(fmt.Sprintf(frame, s.Inline(it), s.said(*it.When, now, false)))
	}
	var b strings.Builder
	fmt.Fprintf(&b, s.RecallMany, len(items))
	for _, it := range items {
		b.WriteString("\n" + fmt.Sprintf(s.DigestLine, s.DigestLineOf(it, now)))
	}
	return b.String()
}

// DigestLineOf is an item as one bullet of the digest or of a list: its words
// as stored, so the bullet keeps the capital, then its day and time, a due day
// as due (ADR-0064).
func (s Set) DigestLineOf(it Item, now time.Time) string {
	if it.Unnamed {
		return fmt.Sprintf(s.LineUnnamed, s.When(*it.When, now))
	}
	if it.When == nil {
		return strings.TrimSpace(it.Text)
	}
	text := strings.TrimSuffix(strings.TrimSpace(it.Text), ".")
	return fmt.Sprintf(s.ItemWhen, text, s.When(*it.When, now))
}

// Push words a reminder: the item's words and, when it has an instant, its day
// and time as a person says them (ADR-0064 point 1).
func (s Set) Push(it Item, now time.Time) string {
	if it.Unnamed {
		return settled(fmt.Sprintf(s.PushUnnamed, s.When(*it.When, now)))
	}
	words := s.Inline(it)
	if it.When != nil {
		words = fmt.Sprintf(s.ItemWhen, words, s.When(*it.When, now))
	}
	return settled(fmt.Sprintf(s.PushFrame, words))
}

// Ring is a ringing timer's fixed sentence around its words (ADR-0064 point 7).
func (s Set) Ring(words string) string {
	return settled(fmt.Sprintf(s.TimerRing, s.Inline(Item{Text: words})))
}

// settled drops the frame's period after words that already end a sentence
// with ?, ! or a clipped ellipsis.
func settled(sentence string) string {
	for _, end := range []string{"?.", "!.", "\u2026."} {
		if strings.HasSuffix(sentence, end) {
			return strings.TrimSuffix(sentence, ".")
		}
	}
	return sentence
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
