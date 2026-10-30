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
	"strings"
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
	// its question. It is never a fact: a number in it is not one, though its
	// words may be said back.
	Message string
	Items   []Item
	// Keep are literals the wording must carry exactly, such as an undo
	// instruction the user has to type back.
	Keep []string
}

// vocabulary is what a language needs to fill a {n.when} and to read the words
// around the slots. Every list is of what Nooma itself says, never of what a
// user writes (ADR-0033 rules out reading intent from word lists).
type vocabulary struct {
	weekdays   [7]string // full, singular, indexed by time.Weekday
	onDate     string    // a day: weekday, day of the month, month name
	yearSuffix string    // a year other than now's
	at, atOne  string    // a time; atOne for one o'clock where it differs
	labels     map[int]string
	// noBefore may not come right before a {n.when}, which has its own
	// preposition; separators part two items.
	noBefore, separators []string
	// forbidden are the day, time and number words a wording may only say
	// through a slot (besides digits and day and month names).
	forbidden []string
	// frame is the common words a wording may use besides the draft's, the
	// message's and the items' own: articles, prepositions, pronouns, the verbs
	// of an answer (ADR-0063). A word outside it sends the draft, and keeps an
	// invented item out ("también el gimnasio"). It holds no negation ("no",
	// "ni", "nothing") and no singular count noun ("cosa", "thing"): those are
	// sayable only when the draft or an item says them, so "No encontré nada"
	// licenses its own and "no tenés dentista" for a found one is not sent.
	frame []string
	// none fills an empty recall's {none}. noneFrame is what an empty recall may
	// add to frame ("por ahora", "todavía"), and claims what it may never say
	// outside {none}, whatever the message says: each one claims a finding.
	none              string
	noneFrame, claims []string
}

var vocabularies = map[classify.Language]vocabulary{
	classify.LanguageEN: {
		weekdays:   phrase.For(classify.LanguageEN).WeekdayNames,
		onDate:     "on %s %d %s",
		yearSuffix: " %d",
		at:         "at %s",
		none:       "I found nothing",
		noneFrame:  strings.Fields("sorry yet still now so far"),
		claims: strings.Fields(`a an one some something thing things have has had there found noted scheduled set
			yes appointment appointments got`),
		labels:     map[int]string{-1: "yesterday", 0: "today", 1: "tomorrow", 2: "the day after tomorrow"},
		noBefore:   []string{"on", "at", "the", "in", "of"},
		separators: []string{"and", "or", "then"},
		forbidden: strings.Fields(`today tomorrow yesterday tonight morning afternoon evening night noon midnight
			week weeks weekend month months year years hour hours minute minutes
			two three four five six seven eight nine ten eleven twelve fifteen twenty thirty`),
		frame: strings.Fields(`the a an of to in on at for with by from about and or but if yes also you your have has had is are
			was were be will there here it its this that these those things something found noted scheduled set
			first then after before later all done okay ok i my me we got s re ve ll d m appointments what is
			coming up remember look topic`),
	},
	classify.LanguageES: {
		weekdays:   [7]string{"domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"},
		onDate:     "el %s %d de %s",
		yearSuffix: " de %d",
		at:         "a las %s",
		atOne:      "a la %s",
		none:       "no encontré nada",
		noneFrame:  strings.Fields("ahora todavia aun perdon"),
		claims: strings.Fields(`un una uno unos unas algo cosa cosas hay tenes tienes tiene tengo tenias anotado anotada
			anotados agendado agendada programado programada encontre anote pendiente pendientes turno turnos
			cita citas si ya listo`),
		labels:     map[int]string{-1: "ayer", 0: "hoy", 1: "mañana", 2: "pasado mañana"},
		noBefore:   []string{"el", "la", "los", "las", "lo", "a", "al", "de", "del", "en"},
		separators: []string{"y", "e", "o", "u"},
		forbidden: strings.Fields(`hoy manana ayer anteayer anoche pasado tarde noche mediodia madrugada semana semanas
			finde proximo proxima siguiente mes meses ano anos hora horas minuto minutos
			dos tres cuatro cinco seis siete ocho nueve diez once doce quince veinte treinta`),
		frame: strings.Fields(`el la los las lo un una unos unas de del a al en con por para sobre entre y e o u que como pero si
			ya tambien te ti tu tus vos sos tenes tienes tiene tengo tenias hay es son esta estan estas sera
			queda quedan encontre anote anotado anotada anotados algo eso esto este estos ese esa esas esos
			primero primera despues antes luego entonces cosas pendiente pendientes aca aqui ahi agenda agendado
			agendada programado programada turnos citas listo dale bien me mi mis nos le les su sus todo todos
			toda mas espera esperan aviso recorda acordate toca vas ir che mira tema acerca`),
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
