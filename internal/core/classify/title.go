package classify

import (
	"bytes"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// MaxTitleRunes is the longest title a unit keeps — doc 02 §13. A reminder
// says its title in one line beside a date; a sentence does not fit there.
const MaxTitleRunes = 60

// datedTitle matches what a date-free title must not hold: an ISO date or an
// HH:MM. Words that name a day or hour ("mañana", "jueves", "3pm") cannot be
// caught without a word list, which core does not hold (ADR-0033); the prompt
// rule and the doctor measure cover them.
var datedTitle = regexp.MustCompile(`\d{4}-\d{2}-\d{2}|\d{1,2}:\d{2}`)

// ResolveTitle is the title the model's text stands for, or nil when it stands
// for none (spec R12, I14). It is trimmed. A blank one, one holding any
// non-graphic rune (a control character, a line or paragraph separator, a
// format character such as a zero-width or bidi one), one over MaxTitleRunes, or
// one holding an ISO date or an HH:MM is dropped, never repaired: the unit then
// reads its content. What is checked is the rune's class, not how it renders: a
// graphic character that draws as blank is not caught.
func ResolveTitle(raw string) *string {
	title := strings.TrimSpace(raw)
	if title == "" || utf8.RuneCountInString(title) > MaxTitleRunes || datedTitle.MatchString(title) {
		return nil
	}
	for _, r := range title {
		// Graphic is letters, marks, numbers, punctuation, symbols and spaces:
		// it leaves out control characters, line and paragraph separators and
		// format characters (zero-width, bidi override), which a reader cannot
		// see and a line cannot hold.
		if !unicode.IsGraphic(r) {
			return nil
		}
	}
	return &title
}

// DecodeTitle reads the answer to the naming prompt (ADR-0065): the title
// ResolveTitle keeps, or nil with the reason none was kept. A missing or null
// title is ReasonAbsent, a non-string one ReasonWrongType, and one ResolveTitle
// drops ReasonBadFormat, as assignTitle reports it at capture.
func DecodeTitle(raw []byte) (*string, Reason) {
	fields, _ := Salvage(raw)
	value, ok := fields["title"]
	if !ok || string(bytes.TrimSpace(value)) == "null" {
		return nil, ReasonAbsent
	}
	var c Classification
	if r := assignTitle(value, &c, time.Time{}); r != "" {
		return nil, r
	}
	return c.Title, ""
}
