package classify

import (
	"regexp"
	"strings"
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
// for none (spec R12, I14). It is trimmed. A blank one, one holding a newline
// or any control character, one over MaxTitleRunes, or one holding an ISO date
// or an HH:MM is dropped, never repaired: the unit then reads its content.
func ResolveTitle(raw string) *string {
	title := strings.TrimSpace(raw)
	if title == "" || utf8.RuneCountInString(title) > MaxTitleRunes || datedTitle.MatchString(title) {
		return nil
	}
	if strings.IndexFunc(title, unicode.IsControl) >= 0 {
		return nil
	}
	return &title
}
