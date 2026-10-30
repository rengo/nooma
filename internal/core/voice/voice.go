// Package voice words what the code found or did as a natural chat message,
// and decides whether the model's wording may be sent (doc 02 §5 step 2,
// ADR-0061).
//
// **The code decides; the model only words it.** Facts are what the code
// already produced, and the draft is the fixed text it would send today
// (internal/core/phrase). The model rewrites the draft from the facts, and
// Check sends that wording only when every date, time, number and day name in
// it is one the facts or the draft hold; otherwise it sends the draft. The
// person always gets an answer, and never one the vault does not back.
//
// Pure: the instant arrives in Facts.Now, the call to a model is brain's.
package voice

import (
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/phrase"
)

// TimeoutSeconds bounds one wording call (doc 02 §13 `reply_phrasing_timeout`).
// Past it the draft is sent: the client chose to wait up to 5 s for a natural reply.
const TimeoutSeconds = 5

// MaxSentences is how many sentences a wording may have outside list lines,
// unless its draft already has more (§13 `reply_max_sentences`; the client's
// "one or two sentences").
const MaxSentences = 2

// MinRunes and RunesFactor bound a wording's length: the larger of MinRunes and
// RunesFactor times its draft's (§13 `reply_min_runes`, `reply_runes_factor`). A chat reply is read
// on a phone; a wording far longer than its draft has added something.
const (
	MinRunes    = 280
	RunesFactor = 2
)

// Intent is what the message is for, and tells the model how to frame it.
type Intent string

const (
	// RecallFound answers a question with the units the recall found.
	RecallFound Intent = "recall_found"
	// RecallNone answers a question the recall found nothing for.
	RecallNone Intent = "recall_none"
)

// Item is one thing the message is about: a unit's title (else its content),
// its type, and its instant in the user's zone, a day alone when DayOnly.
type Item struct {
	Text, Type string
	At         *time.Time
	DayOnly    bool
}

// Facts are what the code produced, and all the model may say.
type Facts struct {
	Intent   Intent
	Language classify.Language
	// Now is the operation's one instant; its zone is the user's.
	Now time.Time
	// Message is the user's own message, for its register and the shape of
	// its question. It is never a fact: a number in it is not licensed.
	Message string
	Items   []Item
	// Keep are literals the wording must carry exactly, such as an undo
	// instruction the user has to type back.
	Keep []string
}

// vocabulary is what a language needs to word a date.
type vocabulary struct {
	weekdays   [7]string // full, singular, indexed by time.Weekday
	longDate   string    // weekday, day, month name
	yearSuffix string    // a year other than now's
	labels     map[int]string
}

var vocabularies = map[classify.Language]vocabulary{
	classify.LanguageEN: {
		weekdays:   phrase.For(classify.LanguageEN).WeekdayNames,
		longDate:   "%s %d %s",
		yearSuffix: " %d",
		labels:     map[int]string{-1: "yesterday", 0: "today", 1: "tomorrow", 2: "the day after tomorrow"},
	},
	classify.LanguageES: {
		weekdays:   [7]string{"domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"},
		longDate:   "%s %d de %s",
		yearSuffix: " de %d",
		labels:     map[int]string{-1: "ayer", 0: "hoy", 1: "mañana", 2: "pasado mañana"},
	},
}

// vocabularyFor is l's vocabulary, the fallback's for a language outside the table.
func vocabularyFor(l classify.Language) vocabulary {
	if v, ok := vocabularies[l]; ok {
		return v
	}
	return vocabularies[classify.Fallback()]
}

// dayOffset is how many calendar days t is after now, in now's zone.
func dayOffset(now, t time.Time) int {
	civil := func(x time.Time) time.Time {
		x = x.In(now.Location())
		return time.Date(x.Year(), x.Month(), x.Day(), 0, 0, 0, 0, time.UTC)
	}
	return int(civil(t).Sub(civil(now)).Hours() / 24)
}
