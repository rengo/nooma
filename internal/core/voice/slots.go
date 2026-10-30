package voice

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rengo/nooma/internal/core/phrase"
)

// slot is one placeholder in a wording: item n's words ({n}), its day and time
// ({n.when}), or the number of items ({count}, item -1), at out[start:end].
type slot struct {
	item       int
	when       bool
	start, end int
}

var slotPattern = regexp.MustCompile(`\{([^{}]*)\}`)

// placeholder stands where a slot was: not a word, not a number, not a space.
const placeholder = "§"

// parseSlots finds the slots of out, and out with each one replaced by
// placeholder. ok is false for an unknown slot, an item that does not exist, or
// a stray brace.
func parseSlots(out string, items int) (slots []slot, rest string, ok bool) {
	for _, m := range slotPattern.FindAllStringSubmatchIndex(out, -1) {
		name := out[m[2]:m[3]]
		s := slot{item: -1, start: m[0], end: m[1]}
		if name != "count" || items == 0 {
			num, when := strings.CutSuffix(name, ".when")
			n, err := strconv.Atoi(num)
			if err != nil || n < 1 || n > items {
				return nil, "", false
			}
			s.item, s.when = n-1, when
		}
		slots = append(slots, s)
	}
	rest = slotPattern.ReplaceAllString(out, placeholder)
	return slots, rest, !strings.ContainsAny(rest, "{}")
}

// words reads what rest, a wording outside its slots, says in its own words: a
// number, a date, or a day, month, time or number word is RejectFact; a word
// that is neither a common word of the language nor one the draft, the message
// or an item's text holds is RejectWord.
func (v vocabulary) words(f Facts, draft, rest string) Rejection {
	set := phrase.For(f.Language)
	dated := map[string]bool{}
	for _, w := range slices.Concat(v.forbidden, v.weekdays[:], set.WeekdayNames[:], set.Weekdays[:],
		set.MonthNames[:], set.Months[:]) {
		dated[fold(w)] = true
	}
	allowed := map[string]bool{}
	texts := []string{draft, f.Message}
	for _, it := range f.Items {
		texts = append(texts, it.Text)
	}
	for _, w := range append(wordsOf(strings.Join(texts, " ")), v.frame...) {
		allowed[fold(w)] = true
	}
	if strings.ContainsFunc(rest, isDigit) {
		return RejectFact
	}
	for _, w := range wordsOf(rest) {
		switch w = fold(w); {
		case dated[w]:
			return RejectFact
		case !allowed[w]:
			return RejectWord
		}
	}
	return Accepted
}

// placed reports whether out's slots are where they may be: each item's {n}
// once and its {n.when} once (none for an item with no instant), {count} at
// most once, an item's two slots with no other item's between them, a
// separator between two items, and no article or preposition before a
// {n.when}, which carries its own ("mañana a las 10:00", "el jueves 5…").
func placed(f Facts, v vocabulary, out string, slots []slot) bool {
	seen := map[[2]int]int{}
	var order []slot
	for _, s := range slots {
		w := 0
		if s.when {
			w = 1
		}
		seen[[2]int{s.item, w}]++
		if s.item < 0 {
			continue
		}
		if s.when && slices.Contains(v.noBefore, lastWord(out[:s.start])) {
			return false
		}
		order = append(order, s)
	}
	if seen[[2]int{-1, 0}] > 1 {
		return false
	}
	for i, it := range f.Items {
		whens := 0
		if it.At != nil {
			whens = 1
		}
		if seen[[2]int{i, 0}] != 1 || seen[[2]int{i, 1}] != whens {
			return false
		}
	}
	for i := 1; i < len(order); i++ {
		prev, next := order[i-1], order[i]
		if prev.item == next.item {
			continue
		}
		if slices.ContainsFunc(order[i+1:], func(s slot) bool { return s.item == prev.item }) ||
			!separated(v, out[prev.end:next.start]) {
			return false
		}
	}
	return true
}

// separated reports whether between, the text between two items, parts them.
func separated(v vocabulary, between string) bool {
	if strings.ContainsAny(between, ",;:.\n") {
		return true
	}
	return slices.ContainsFunc(wordsOf(between), func(w string) bool { return slices.Contains(v.separators, fold(w)) })
}

// lastWord is the last word of text's last line, folded, or "".
func lastWord(text string) string {
	if i := strings.LastIndexByte(text, '\n'); i >= 0 {
		text = text[i+1:]
	}
	ws := wordsOf(text)
	if len(ws) == 0 || !strings.HasSuffix(strings.ToLower(strings.TrimRightFunc(text, unicode.IsSpace)), ws[len(ws)-1]) {
		return ""
	}
	return fold(ws[len(ws)-1])
}

// fill puts each slot's words in out: the item's text, its day and time as the
// code renders them, and the count, capitalised at a sentence's start and
// lower-cased elsewhere.
func fill(f Facts, out string, slots []slot) string {
	var b strings.Builder
	last := 0
	for _, s := range slots {
		b.WriteString(out[last:s.start])
		var text string
		switch {
		case s.item < 0:
			text = strconv.Itoa(len(f.Items))
		case s.when:
			text = when(f, f.Items[s.item])
		default:
			text = f.Items[s.item].Text
		}
		b.WriteString(caseFor(text, atStart(b.String())))
		last = s.end
	}
	b.WriteString(out[last:])
	return b.String()
}

// atStart reports whether text ends where a sentence starts.
func atStart(text string) bool {
	text = strings.TrimRightFunc(text, func(r rune) bool {
		return unicode.IsSpace(r) && r != '\n' || strings.ContainsRune("«\"“¿¡•(", r)
	})
	return text == "" || strings.HasSuffix(text, "\n") || strings.ContainsRune(".!?…", lastRune(text))
}

func lastRune(s string) rune {
	r, _ := utf8.DecodeLastRuneInString(s)
	return r
}

// caseFor is text with its first letter upper at a sentence's start, else lower
// unless the text opens with an acronym.
func caseFor(text string, start bool) string {
	rs := []rune(text)
	switch {
	case len(rs) == 0:
	case start:
		rs[0] = unicode.ToUpper(rs[0])
	case len(rs) == 1 || !unicode.IsUpper(rs[1]):
		rs[0] = unicode.ToLower(rs[0])
	}
	return string(rs)
}

// when is item it's day and time as {n.when} says them, with their own
// preposition: a relative day near today, else the weekday and the date, and
// the time unless a day alone.
func when(f Facts, it Item) string {
	v, set := vocabularyFor(f.Language), phrase.For(f.Language)
	t := it.At.In(f.Now.Location())
	day, near := v.labels[dayOffset(f.Now, t)]
	if !near {
		day = fmt.Sprintf(v.onDate, v.weekdays[t.Weekday()], t.Day(), set.MonthNames[t.Month()-1])
		if t.Year() != f.Now.Year() {
			day += fmt.Sprintf(v.yearSuffix, t.Year())
		}
	}
	if it.DayOnly {
		return day
	}
	at := v.at
	if t.Hour() == 1 && v.atOne != "" {
		at = v.atOne
	}
	return day + " " + fmt.Sprintf(at, fmt.Sprintf("%d:%02d", t.Hour(), t.Minute()))
}

// wordsOf is the words of text, lower-cased.
func wordsOf(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) })
}

var unaccent = strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n")

// fold is w lower-cased and without accents, so "despues" is "después".
func fold(w string) string { return unaccent.Replace(strings.ToLower(w)) }

func isDigit(r rune) bool { return r >= '0' && r <= '9' }
