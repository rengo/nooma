// Package phrase holds every sentence Nooma says to a person, in every
// language it can say them in — doc 02 §5 step 1, decided in
// docs/adr/0022-reply-language.md.
//
// **It is core, not channels, and that is the point.** Choosing words for
// a person is a pure function of what happened and which language to say
// it in: no clock, no repository, no provider. Leaving the sentences
// inside internal/channels made them untestable below L2 and gave the one
// surface that answers a person no way to prove it can answer in more
// than one language.
//
// **What is NOT here**: anything written for a developer. decision_log
// rationales, error strings and log lines stay English wherever they are
// (CLAUDE.md, and ADR-0022's own "the glass box stays English"). The
// dividing line is the audience, never the file.
package phrase

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
)

// Set is every sentence, in one language. One struct per language, and a
// caller reaches them through For.
//
// **A struct rather than a map[key]string** so a missing sentence is a
// zero value the completeness test can name, rather than a lookup miss
// some caller papers over with a default. Format strings carry their verbs
// in the field comment because the argument order genuinely differs
// between languages, and a renderer that guessed would produce a sentence
// that reads backwards.
type Set struct {
	// Noted answers a stored capture. Nothing to interpolate.
	Noted string
	// Corrected answers an applied correction.
	Corrected string
	// OutOfScope refuses a request for something Nooma does not do
	// (ADR-0021). It must not sound temporary: the capability is absent,
	// not unavailable.
	OutOfScope string
	// NoAnswer is the chat task's outage sentence. It must sound
	// temporary, which is exactly what separates it from OutOfScope — one
	// invites trying again and the other does not.
	NoAnswer string
	// AskWhichOne is the ask-shaped correction outcome.
	AskWhichOne string
	// NothingFound answers a recall that admitted nothing. "No results"
	// is an answer; an empty message is not.
	NothingFound string

	// NotScheduled prefixes an arming refusal — one %s, the reason.
	NotScheduled string
	// RefusalNoDate and RefusalPast are the two reasons a refusal can
	// carry, in the person's language. The typed prospection.Refusal
	// travels to the renderer precisely so these can exist: the English
	// sentence brain builds stays in the trail, where its audience is an
	// auditor.
	RefusalNoDate string
	RefusalPast   string

	// TimerSet takes one %s: the instant the timer fires, which for a
	// timer is also what it is about.
	TimerSet string
	// RecurringSet takes one %s: the next occurrence, never the date the
	// person stated — for a birthday already past this year those differ.
	RecurringSet string
	// NotedFor takes two %s: what the nudge is ABOUT, then when the nudge
	// arrives. Never the firing instant as the first — that answers a
	// question nobody asked (doc 02 §5 step 5).
	NotedFor string

	// The four lead-time phrases NotedFor's second %s is filled with.
	// LeadDays takes one %d.
	LeadImmediate string
	LeadHours     string
	LeadDayBefore string
	LeadDays      string

	// FoundOne takes one %d and FoundMany takes one %d. Two fields rather
	// than one plural rule because plural agreement is not a shared
	// mechanism across languages, and pretending it is produces "1 things"
	// in whichever language was not the author's.
	FoundOne  string
	FoundMany string

	// Weekdays is indexed by time.Weekday — Sunday is 0, as the stdlib
	// has it. Months is indexed by time.Month minus one.
	//
	// These exist because Go's standard library has no locale-aware time
	// formatting at all: t.Format("Mon 2 Jan") writes English names on
	// every machine in the world. ADR-0022 states that cost; this is what
	// paying it looks like.
	Weekdays [7]string
	Months   [12]string
	// DateOrder says whether the day number comes before the month name.
	// English writes "Fri 28 Aug" and Spanish writes "vie 28 ago" — the
	// same order here, but the field exists because the next language
	// added may not, and discovering that inside a Sprintf is worse than
	// naming it.
	DayBeforeMonth bool
}

// sets is the whole table. TestSetsCoverEveryLanguage (phrase_test.go)
// iterates classify.AllLanguages() against it, so widening the vocabulary
// without writing the sentences fails a test rather than silently
// answering in the fallback — which is the property ADR-0022 asks for.
var sets = map[classify.Language]Set{
	classify.LanguageEN: {
		Noted:        "Noted.",
		Corrected:    "Corrected.",
		OutOfScope:   "That is not something I can do.",
		NoAnswer:     "I could not answer that just now.",
		AskWhichOne:  "I need one more thing before I can change that — which one did you mean?",
		NothingFound: "I could not find anything about that.",

		NotScheduled:  "I did not set that: %s",
		RefusalNoDate: "no time was given, and guessing one is worse than not setting a reminder at all",
		RefusalPast:   "the time given has already passed",

		TimerSet:     "Timer set for %s.",
		RecurringSet: "Recurring reminder set for %s.",
		NotedFor:     "Noted for %s. I will remind you %s.",

		LeadImmediate: "right away",
		LeadHours:     "a few hours before",
		LeadDayBefore: "the day before",
		LeadDays:      "%d days before",

		FoundOne:  "Found %d thing:",
		FoundMany: "Found %d things:",

		Weekdays:       [7]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"},
		Months:         [12]string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"},
		DayBeforeMonth: true,
	},

	// Neutral Spanish, not a regional variant. The project's own language
	// rule (CLAUDE.md) asks for a professional register in Spanish
	// artifacts unless a variant is explicitly wanted, and a vault is not
	// the place to guess at one: a person who wants voseo is asking for a
	// preference, which is the self-model's job and not the classifier's.
	classify.LanguageES: {
		Noted:        "Anotado.",
		Corrected:    "Corregido.",
		OutOfScope:   "Eso no es algo que pueda hacer.",
		NoAnswer:     "No pude responder eso en este momento.",
		AskWhichOne:  "Necesito una cosa más antes de cambiarlo: ¿a cuál te referías?",
		NothingFound: "No encontré nada sobre eso.",

		NotScheduled:  "No lo programé: %s",
		RefusalNoDate: "no se indicó ninguna hora, y adivinarla es peor que no poner el recordatorio",
		RefusalPast:   "la hora indicada ya pasó",

		TimerSet:     "Temporizador puesto para el %s.",
		RecurringSet: "Recordatorio recurrente puesto para el %s.",
		NotedFor:     "Anotado para el %s. Te aviso %s.",

		LeadImmediate: "ahora mismo",
		LeadHours:     "unas horas antes",
		LeadDayBefore: "el día anterior",
		LeadDays:      "%d días antes",

		FoundOne:  "Encontré %d cosa:",
		FoundMany: "Encontré %d cosas:",

		Weekdays:       [7]string{"dom", "lun", "mar", "mié", "jue", "vie", "sáb"},
		Months:         [12]string{"ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sep", "oct", "nov", "dic"},
		DayBeforeMonth: true,
	},
}

// For returns the sentences for l.
//
// A language outside the table returns the fallback's set rather than a
// zero Set, because a zero Set renders empty strings and an empty reply is
// the one failure a person cannot tell from being ignored. The
// completeness test is what keeps this branch unreachable for any member
// of the vocabulary; it exists for the caller who builds a
// classify.Language out of thin air.
func For(l classify.Language) Set {
	if s, ok := sets[l]; ok {
		return s
	}
	return sets[classify.Fallback()]
}

// Time renders an instant the way a person reading l reads one.
//
// The instant's own zone, never the process's: the classification carried
// the user's offset in, and re-rendering it in the server's zone would
// tell someone in Buenos Aires about a reminder at a time nobody set.
func (s Set) Time(t time.Time) string {
	day := strconv.Itoa(t.Day())
	month := s.Months[int(t.Month())-1]
	clock := fmt.Sprintf("%02d:%02d", t.Hour(), t.Minute())

	head := day + " " + month
	if !s.DayBeforeMonth {
		head = month + " " + day
	}
	return s.Weekdays[int(t.Weekday())] + " " + head + ", " + clock
}

// Found renders the header over a recall's results, in agreement with n.
func (s Set) Found(n int) string {
	if n == 1 {
		return fmt.Sprintf(s.FoundOne, n)
	}
	return fmt.Sprintf(s.FoundMany, n)
}

// Lead names when a nudge arrives, relative to what it is about.
//
// It takes the gap and whether the plan called itself immediate, rather
// than deriving the second from the first: subtracting the two instants
// gives a true duration and a false promise — 41 hours reads as "the day
// before" for a reminder that arrives at once.
func (s Set) Lead(immediate bool, gap time.Duration) string {
	switch {
	case immediate, gap <= 0:
		return s.LeadImmediate
	case gap < 24*time.Hour:
		return s.LeadHours
	case gap < 48*time.Hour:
		return s.LeadDayBefore
	default:
		return fmt.Sprintf(s.LeadDays, int(gap.Hours()/24))
	}
}

// List renders a recall's header and its bullets as one message.
func (s Set) List(contents []string) string {
	var b strings.Builder
	b.WriteString(s.Found(len(contents)))
	for _, c := range contents {
		b.WriteString("\n• " + c)
	}
	return b.String()
}
