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
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/prospection"
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
	// Unchanged answers a correction the unit already satisfied.
	Unchanged string
	// Moved answers a correction that moved the unit: one %s, the instant it
	// moved to, named as NotedFor names it.
	Moved string

	// RemindYou answers a correction that set an event's leads: one %s, its
	// reminders (ExactLeads). NoReminderLeft answers one that left none before
	// the event. LeadsNotSet answers leads that could not be set: one %d, the
	// furthest a lead may be, in days (I40).
	RemindYou      string
	NoReminderLeft string
	LeadsNotSet    string

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
	// NotedNotSet answers a kept unit whose asked reminder was not set — one
	// %s, the reason.
	NotedNotSet string
	// RefusalNoDate, RefusalPast and RefusalTooLate are the reasons a
	// refusal can carry, in the person's language. The typed
	// prospection.Refusal travels to the renderer precisely so these can
	// exist: the English sentence brain builds stays in the trail, where its
	// audience is an auditor.
	RefusalNoDate  string
	RefusalPast    string
	RefusalTooLate string

	// TimerSet answers a timer: one %s, when it rings, which for a timer is also
	// what it is about (its hour alone on the day it was set, else its day and hour).
	TimerSet string
	// The answers to a chat's list or cancel of the live timers (ADR-0051).
	// TimersMany takes the count of every live timer, TimersMore the count
	// past the listed ones, and TimerCancelled the cancelled timer's line.
	TimersNone, TimersOne, TimersMany, TimersMore string
	TimerWhich, TimerCancelled, TimerGone         string
	// Greeting answers Telegram's /start and UnknownCommand any other bot
	// command, both without the model: a command is not a message to read.
	Greeting, UnknownCommand string
	// A voice note (ADR-0062): VoiceHeard takes the transcript and leads a
	// reply that did nothing actionable; VoiceUnheard answers one that could
	// not be transcribed, VoiceOff one with no transcription task bound, and
	// VoiceTooLong, taking the cap in minutes, one past it.
	VoiceHeard, VoiceUnheard, VoiceOff, VoiceTooLong string
	// EveryDay, EveryWeek, EveryMonth and EveryYear say when a recurring
	// reminder rings, every time it does (ADR-0031 point 7), rather than its
	// next occurrence alone. EveryDay takes the clock (Clock); EveryWeek the
	// weekday's name and the clock; EveryMonth the day of the month and the
	// clock; EveryYear the day, the month's name and the clock.
	EveryDay, EveryWeek, EveryMonth, EveryYear string
	// NotedFor takes two %s: what the nudge is ABOUT (Dated), then when the nudge
	// arrives. Never the firing instant as the first — that answers a
	// question nobody asked (doc 02 §5 step 5).
	NotedFor string
	// NotedForDigest takes one %s: the due day of a task that arms nothing,
	// which that morning's digest names (doc 02 §7).
	NotedForDigest string
	// NotedOn takes one %s: the instant of an event nothing was armed for,
	// its leads behind when told (ADR-0047) or only night left before it
	// (ADR-0030). It names what was read and promises no ring.
	NotedOn string
	// Closed takes one %s: the name of the task a done follow-up closed.
	// NothingClosed answers a done that closed nothing (ADR-0040): never a
	// confirmation of something Nooma did not do.
	Closed        string
	NothingClosed string

	// PushFrame frames a pushed reminder (ADR-0064): one %s, the item's words
	// with its day and time. TimerRing frames a ringing timer: one %s, its
	// words. AtWhen joins a unit's words to its instant: two %s, in that order.
	PushFrame string
	TimerRing string
	AtWhen    string

	// The sentences Nooma says first, without being asked (ADR-0032 point 5).
	// DigestLine frames one digest bullet: one %s, the line. Morning greets
	// the digest; DigestHeaderOne says it has one line and takes nothing,
	// DigestHeaderMany takes one %d, the number of lines, and both follow
	// Morning. OneMore opens the digest's question, on a line of its own, when
	// lines came before it. RelationQuestion takes two %s: what was linked, then
	// what it was linked with. Late is the note a reminder carries when it is
	// sent later than intended, and SomethingYouAsked is what a reminder says
	// when it has nothing of its own to say.
	DigestLine        string
	Morning           string
	DigestHeaderOne   string
	DigestHeaderMany  string
	OneMore           string
	RelationQuestion  string
	Late              string
	SomethingYouAsked string

	// AndMore ends a digest that dropped lines to fit a message: one %d, how
	// many lines it left for tomorrow. TimerNudge is what a timer with no words
	// says, TimerLate the note a timer carries when it is delivered later than
	// asked, and LanguageName is what the model is told to reword a timer in.
	AndMore string
	// AskAgain heads the digest's block of check-ins asked again (doc 02 §7),
	// and AskDone is one of them: one %s, the unit's title or content.
	AskAgain string
	AskDone  string
	// AskGoal is a goal check-in (doc 02 §7, ADR-0054): one %s, the goal.
	AskGoal      string
	TimerNudge   string
	TimerLate    string
	LanguageName string

	// The lead-time phrases NotedFor's second %s is filled with. LeadDays
	// and LeadMinutes take one %d; LeadMinute is a lead of one minute.
	// LeadThatDayAt takes one %s, the clock a day alone is reminded at.
	// LeadTodayAt takes one %s, the clock of a ring on the day the user told
	// Nooma about an event on a later day (ADR-0037).
	LeadImmediate string
	LeadMinute    string
	LeadMinutes   string
	LeadHours     string
	LeadDayBefore string
	LeadDays      string
	LeadThatDayAt string
	LeadTodayAt   string
	// And joins the last two of several lead phrases.
	And string

	// LeadHour and LeadHoursExact (one %d) say a whole number of hours as
	// that number, for leads the user just named (ExactLeads).
	LeadHour       string
	LeadHoursExact string

	// Recall frames (ADR-0064): the answer to "what do I have noted?".
	// RecallOne and RecallOnePast take the item's words and its day, and
	// RecallOneUndated its words alone; RecallMany takes the count. The
	// frame never puts an article before the item's words (Inline).
	RecallOne        string
	RecallOnePast    string
	RecallOneUndated string
	RecallMany       string
	// The unnamed frames (ADR-0065 point 5) say a unit that has an instant and
	// no title as something noted, with the instant and none of its words: the
	// push, the one-item recall ahead and past (each one %s, the day and
	// time) and the list line (one %s).
	PushUnnamed          string
	RecallOneUnnamed     string
	RecallOnePastUnnamed string
	LineUnnamed          string
	// ItemWhen joins an item's words and its day on a list line.
	ItemWhen string

	// Day and time words (ADR-0064 point 2). Yesterday to DayAfter name the
	// days around now; OnDate, with the full weekday and month names, any
	// other; YearSuffix follows a date in another year. At and AtOne frame a
	// clock (AtOne is the one o'clock Spanish says differently), OnDay an
	// adverbial date, DueFuture and DuePast a due day. LowerFirst says
	// whether Inline lowers an item's first letter.
	Yesterday, TodayWord, Tomorrow, DayAfter string
	OnDate, YearSuffix                       string
	At, AtOne, OnDay                         string
	DueFuture, DuePast                       string
	LowerFirst                               bool
	// DayNames are the singular weekday names, "el jueves"; WeekdayNames
	// stay plural for "todos los lunes".
	DayNames [7]string

	// Weekdays is indexed by time.Weekday — Sunday is 0, as the stdlib
	// has it. Months is indexed by time.Month minus one.
	//
	// These exist because Go's standard library has no locale-aware time
	// formatting at all: t.Format("Mon 2 Jan") writes English names on
	// every machine in the world. ADR-0022 states that cost; this is what
	// paying it looks like.
	Weekdays [7]string
	Months   [12]string
	// WeekdayNames and MonthNames are the same in full, for a sentence
	// rather than a date: "todos los lunes", "el 23 de octubre".
	WeekdayNames [7]string
	MonthNames   [12]string
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
		Noted:        "Got it, noted.",
		Corrected:    "Done, I fixed it.",
		Unchanged:    "It already said that, so I changed nothing.",
		Moved:        "Done, I moved it to %s.",
		OutOfScope:   "That is not something I can do.",
		NoAnswer:     "I could not answer that just now.",
		AskWhichOne:  "I need one more thing before I can change that — which one did you mean?",
		NothingFound: "I don't have anything noted about that.",

		RemindYou:      "I'll remind you %s.",
		NoReminderLeft: "No reminder is left before it.",
		LeadsNotSet:    "I did not change the reminders: I can only remind you before something with a day and a time, up to %d days before.",

		NotScheduled:   "I did not set that: %s",
		NotedNotSet:    "I noted it, but I didn't set a reminder: %s.",
		RefusalNoDate:  "no time was given, and guessing one is worse than not setting a reminder at all",
		RefusalPast:    "that time has already passed",
		RefusalTooLate: "too little time is left before it",

		TimerSet: "Sure, I'll remind you %s.",

		TimersNone:     "No timers are set.",
		TimersOne:      "There is 1 timer set:",
		TimersMany:     "There are %d timers set:",
		TimersMore:     "and %d more.",
		TimerWhich:     "Which one should I cancel?",
		TimerCancelled: "Cancelled: %s.",
		TimerGone:      "That timer is no longer set.",

		Greeting:       "Hi, I am Nooma. I note down what you write to me, remind you of what you ask, and answer what I know.",
		UnknownCommand: "I do not know that command. To note something down, just write it.",
		VoiceHeard:     "I heard: “%s”",
		VoiceUnheard:   "I could not make out the audio; could you type it?",
		VoiceOff:       "I cannot listen to voice notes here; could you type it?",
		VoiceTooLong:   "That voice note is too long for me (up to %d minutes); could you type it or send a shorter one?",
		EveryDay:       "Sure, I'll remind you every day %s.",
		EveryWeek:      "Sure, I'll remind you every %s %s.",
		EveryMonth:     "Sure, I'll remind you on day %s of every month %s.",
		EveryYear:      "Sure, I'll remind you every year on %s %s %s.",
		NotedFor:       "Got it, noted for %s. I'll remind you %s.",

		NotedForDigest: "Got it, noted for %s. I'll mention it in that morning's digest.",
		NotedOn:        "Got it, noted for %s.",
		Closed:         "Done, I closed \"%s\".",
		NothingClosed:  "I found nothing open to close.",

		PushFrame: "Don't forget: %s.",
		TimerRing: "Time's up: %s.",
		AtWhen:    "%s \u2014 %s",

		DigestLine:        "\u2022 %s",
		Morning:           "Good morning.",
		DigestHeaderOne:   "You have one thing:",
		DigestHeaderMany:  "You have %d things:",
		OneMore:           "One more question:\n",
		RelationQuestion:  "I linked \u201c%s\u201d with \u201c%s\u201d. Are they related?",
		Late:              "Sorry, this is later than intended.",
		SomethingYouAsked: "You asked me to remind you about something.",
		AndMore:           "And %d more I'll tell you about tomorrow.",
		AskAgain:          "Asking again:",
		AskDone:           "Is \"%s\" done?",
		AskGoal:           "Still on \"%s\"?",
		TimerNudge:        "You asked me to remind you about something now.",
		TimerLate:         "Sorry, this is later than you asked for.",
		LanguageName:      "English",

		LeadImmediate: "right away",
		LeadMinute:    "1 minute before",
		LeadMinutes:   "%d minutes before",
		LeadHours:     "a few hours before",
		LeadDayBefore: "the day before",
		LeadDays:      "%d days before",
		LeadThatDayAt: "that day %s",
		LeadTodayAt:   "today %s",
		And:           "and",

		LeadHour:       "1 hour before",
		LeadHoursExact: "%d hours before",

		RecallOne:        "You have \u201c%s\u201d %s.",
		RecallOnePast:    "You had \u201c%s\u201d %s.",
		RecallOneUndated: "You have this noted: %s.",

		PushUnnamed:          "Don't forget: you have something noted %s.",
		RecallOneUnnamed:     "You have something noted %s.",
		RecallOnePastUnnamed: "You had something noted %s.",
		LineUnnamed:          "Something noted, %s",
		RecallMany:           "You have %d things noted:",
		ItemWhen:             "%s, %s",

		Yesterday: "yesterday", TodayWord: "today", Tomorrow: "tomorrow", DayAfter: "the day after tomorrow",
		OnDate: "%s %d %s", YearSuffix: " %d",
		At: "at %s", AtOne: "at %s", OnDay: "on %s",
		DueFuture: "due %s", DuePast: "was due %s",
		DayNames: [7]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"},

		Weekdays:     [7]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"},
		Months:       [12]string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"},
		WeekdayNames: [7]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"},
		MonthNames: [12]string{"January", "February", "March", "April", "May", "June", "July", "August", "September",
			"October", "November", "December"},
		DayBeforeMonth: true,
	},

	// Neutral Spanish, not a regional variant. The project's own language
	// rule (CLAUDE.md) asks for a professional register in Spanish
	// artifacts unless a variant is explicitly wanted, and a vault is not
	// the place to guess at one: a person who wants voseo is asking for a
	// preference, which is the self-model's job and not the classifier's.
	classify.LanguageES: {
		Noted:        "Listo, anotado.",
		Corrected:    "Listo, lo corregí.",
		Unchanged:    "Ya decía eso, así que no cambié nada.",
		Moved:        "Listo, lo pasé para %s.",
		OutOfScope:   "Eso no es algo que pueda hacer.",
		NoAnswer:     "No pude responder eso en este momento.",
		AskWhichOne:  "Necesito una cosa más antes de cambiarlo: ¿a cuál te referías?",
		NothingFound: "No tengo nada anotado sobre eso.",

		RemindYou:      "Te aviso %s.",
		NoReminderLeft: "Ya no queda ningún aviso antes de esa hora.",
		LeadsNotSet:    "No cambié los avisos: solo puedo avisarte antes de algo con día y hora, hasta %d días antes.",

		NotScheduled:   "No lo programé: %s",
		NotedNotSet:    "Lo anoté, pero no te puse aviso: %s.",
		RefusalNoDate:  "no se indicó ninguna hora, y adivinarla es peor que no poner el recordatorio",
		RefusalPast:    "esa hora ya pasó",
		RefusalTooLate: "queda muy poco tiempo para esa hora",

		TimerSet: "Listo, te aviso %s.",

		TimersNone:     "No hay temporizadores puestos.",
		TimersOne:      "Hay 1 temporizador puesto:",
		TimersMany:     "Hay %d temporizadores puestos:",
		TimersMore:     "y %d más.",
		TimerWhich:     "¿Cuál cancelo?",
		TimerCancelled: "Cancelado: %s.",
		TimerGone:      "Ese temporizador ya no está puesto.",

		Greeting:       "Hola, soy Nooma. Anoto lo que me escribas, te aviso de lo que pidas y respondo lo que sé.",
		UnknownCommand: "No conozco ese comando. Para anotar algo, basta con escribirlo.",
		VoiceHeard:     "Entendí: «%s»",
		VoiceUnheard:   "No pude entender el audio; ¿me lo escribís?",
		VoiceOff:       "Acá no puedo escuchar audios; ¿me lo escribís?",
		VoiceTooLong:   "Ese audio es muy largo para mí (hasta %d minutos); ¿me lo escribís o mandás uno más corto?",
		EveryDay:       "Listo, te aviso todos los días %s.",
		EveryWeek:      "Listo, te aviso todos los %s %s.",
		EveryMonth:     "Listo, te aviso el día %s de cada mes %s.",
		EveryYear:      "Listo, te aviso todos los años el %s de %s %s.",
		NotedFor:       "Listo, anotado para %s. Te aviso %s.",

		NotedForDigest: "Listo, anotado para %s. Te lo recuerdo en el resumen de esa mañana.",
		NotedOn:        "Listo, anotado para %s.",
		Closed:         "Listo, saqué «%s».",
		NothingClosed:  "No encontré nada pendiente para cerrar.",

		PushFrame: "Acordate: %s.",
		TimerRing: "Es la hora: %s.",
		AtWhen:    "%s \u2014 %s",

		DigestLine:        "\u2022 %s",
		Morning:           "Buen día.",
		DigestHeaderOne:   "Tenés una cosa:",
		DigestHeaderMany:  "Tenés %d cosas:",
		OneMore:           "Una pregunta más:\n",
		RelationQuestion:  "Relacioné «%s» con «%s». ¿Tienen que ver?",
		Late:              "Perdón, te llega más tarde de lo previsto.",
		SomethingYouAsked: "Me pediste que te recordara algo.",
		AndMore:           "Y %d más que te cuento mañana.",
		AskAgain:          "Te pregunto de nuevo:",
		AskDone:           "¿Ya está «%s»?",
		AskGoal:           "¿Seguís con «%s»?",
		TimerNudge:        "Me pediste que te recordara algo ahora.",
		TimerLate:         "Perdón, te llega más tarde de lo que pediste.",
		LanguageName:      "Spanish",

		LeadImmediate: "ahora mismo",
		LeadMinute:    "1 minuto antes",
		LeadMinutes:   "%d minutos antes",
		LeadHours:     "unas horas antes",
		LeadDayBefore: "el día anterior",
		LeadDays:      "%d días antes",
		LeadThatDayAt: "ese día %s",
		LeadTodayAt:   "hoy %s",
		And:           "y",

		LeadHour:       "1 hora antes",
		LeadHoursExact: "%d horas antes",

		RecallOne:        "Tenés anotado %s %s.",
		RecallOnePast:    "Tenías anotado %s %s.",
		RecallOneUndated: "Tenés anotado: %s.",

		PushUnnamed:          "Acordate: tenés algo anotado %s.",
		RecallOneUnnamed:     "Tenés algo anotado %s.",
		RecallOnePastUnnamed: "Tenías algo anotado %s.",
		LineUnnamed:          "Algo anotado, %s",
		RecallMany:           "Tenés %d cosas anotadas:",
		ItemWhen:             "%s, %s",

		Yesterday: "ayer", TodayWord: "hoy", Tomorrow: "mañana", DayAfter: "pasado mañana",
		OnDate: "el %s %d de %s", YearSuffix: " de %d",
		At: "a las %s", AtOne: "a la %s", OnDay: "%s",
		DueFuture: "para %s", DuePast: "era para %s",
		LowerFirst: true,
		DayNames:   [7]string{"domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"},

		Weekdays: [7]string{"dom", "lun", "mar", "mié", "jue", "vie", "sáb"},
		Months:   [12]string{"ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sep", "oct", "nov", "dic"},
		// Plural, as "todos los %s" says it: lunes to viernes are their own plural.
		WeekdayNames: [7]string{"domingos", "lunes", "martes", "miércoles", "jueves", "viernes", "sábados"},
		MonthNames: [12]string{"enero", "febrero", "marzo", "abril", "mayo", "junio", "julio", "agosto", "septiembre",
			"octubre", "noviembre", "diciembre"},
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

// Time renders an instant the way a person reading l reads one. Chat replies
// no longer use it (Dated does); the recorded fixtures of testdata/llm/reminders
// carry its bytes, and test/conformance/reminder_phrasing_test.go pins them.
//
// The instant's own zone, never the process's: the classification carried
// the user's offset in, and re-rendering it in the server's zone would
// tell someone in Buenos Aires about a reminder at a time nobody set.
func (s Set) Time(t time.Time) string {
	return s.Date(t) + fmt.Sprintf(", %02d:%02d", t.Hour(), t.Minute())
}

// TimerAt says when a timer set at now rings: its hour alone on the day it
// was set, else its day and hour, the day read in fire's own zone.
func (s Set) TimerAt(fire, now time.Time) string {
	now = now.In(fire.Location())
	when := s.Clock(fire)
	if !sameDay(fire, now) {
		when = s.When(Moment{At: fire}, now)
	}
	return fmt.Sprintf(s.TimerSet, when)
}

// TimerLine names a live timer in a chat's list (ADR-0051): when it rings,
// by its hour alone on now's day as TimerAt says it, then what it says.
func (s Set) TimerLine(fire, now time.Time, text *string) string {
	when := s.Hour(fire)
	if !sameDay(fire, now) {
		when = s.Date(fire) + ", " + when
	}
	if text == nil || *text == "" {
		return when
	}
	return s.Line(when, *text)
}

// TimerCancelledLine says which timer a cancel cancelled, as TimerLine names it,
// in a sentence of its own: a text that already ends in a period is not
// given a second one.
func (s Set) TimerCancelledLine(fire, now time.Time, text *string) string {
	return fmt.Sprintf(s.TimerCancelled, strings.TrimRight(s.TimerLine(fire, now, text), "."))
}

// sameDay says whether fire falls on now's day, read in fire's own zone.
func sameDay(fire, now time.Time) bool {
	now = now.In(fire.Location())
	return fire.Year() == now.Year() && fire.YearDay() == now.YearDay()
}

// ThatDayAt names the hour on its own day a day alone is reminded at.
func (s Set) ThatDayAt(fire time.Time) string {
	return fmt.Sprintf(s.LeadThatDayAt, s.Clock(fire))
}

// Every says when a recurring reminder of rule and anchor rings, every time:
// at at's clock time, at's being the next occurrence. The weekday, day and
// month are the anchor's, so a day-31 monthly reminder says 31 in November
// too; an anchor with no weekday reads it from the occurrence.
func (s Set) Every(rule prospection.Rule, anchor prospection.Anchor, at time.Time) string {
	clock := s.Clock(at)
	switch rule {
	case prospection.RuleDaily:
		return fmt.Sprintf(s.EveryDay, clock)
	case prospection.RuleWeekly:
		weekday := at.Weekday()
		if anchor.Weekday != nil {
			weekday = *anchor.Weekday
		}
		return fmt.Sprintf(s.EveryWeek, s.WeekdayNames[weekday], clock)
	case prospection.RuleMonthly:
		return fmt.Sprintf(s.EveryMonth, strconv.Itoa(anchor.Day), clock)
	}
	return fmt.Sprintf(s.EveryYear, strconv.Itoa(anchor.Day), s.MonthNames[anchor.Month-1], clock)
}

// Date renders an instant as Time does, without the clock: a whole day has no hour.
func (s Set) Date(t time.Time) string {
	day := strconv.Itoa(t.Day())
	month := s.Months[int(t.Month())-1]
	head := day + " " + month
	if !s.DayBeforeMonth {
		head = month + " " + day
	}
	return s.Weekdays[int(t.Weekday())] + " " + head
}

// Line joins a unit's words to its instant, for a digest line.
func (s Set) Line(text, when string) string { return fmt.Sprintf(s.AtWhen, text, when) }

// DigestHeader heads a digest of n lines, in agreement with n, after the
// morning greeting.
func (s Set) DigestHeader(n int) string {
	if n == 1 {
		return s.Morning + " " + s.DigestHeaderOne
	}
	return s.Morning + " " + fmt.Sprintf(s.DigestHeaderMany, n)
}

// DigestItem frames one digest line.
func (s Set) DigestItem(line string) string { return fmt.Sprintf(s.DigestLine, line) }

// Question asks whether two things Nooma linked are related.
func (s Set) Question(from, to string) string { return fmt.Sprintf(s.RelationQuestion, from, to) }

// Done asks whether the thing words name is done (AskDone).
func (s Set) Done(words string) string { return fmt.Sprintf(s.AskDone, words) }

// Goal is the goal check-in about goal.
func (s Set) Goal(goal string) string { return fmt.Sprintf(s.AskGoal, goal) }

// More says how many digest lines were left for tomorrow.
func (s Set) More(n int) string { return fmt.Sprintf(s.AndMore, n) }

// Delayed notes that text is being said later than intended.
func (s Set) Delayed(text string) string { return text + " " + s.Late }

// Lead names when a nudge firing at fire arrives, relative to about.
//
// It takes whether the plan called itself immediate rather than deriving
// it: subtracting the two instants gives a true duration and a false
// promise — 41 hours reads as "the day before" for a reminder that arrives
// at once. The day is counted on the calendar, in about's own zone: 09:00
// the day before a date-only event is 15 hours ahead and is "the day
// before" (ADR-0029).
func (s Set) Lead(immediate bool, fire, about time.Time) string {
	if immediate || !about.After(fire) {
		return s.LeadImmediate
	}
	// Under an hour is said in minutes: an asked lead of 15 is not "a few
	// hours before" (ADR-0031 point 7).
	if gap := about.Sub(fire); gap < time.Hour {
		if minutes := int(gap / time.Minute); minutes > 1 {
			return fmt.Sprintf(s.LeadMinutes, minutes)
		}
		return s.LeadMinute
	}
	y, m, d := fire.In(about.Location()).Date()
	ay, am, ad := about.Date()
	days := int(time.Date(ay, am, ad, 0, 0, 0, 0, time.UTC).Sub(time.Date(y, m, d, 0, 0, 0, 0, time.UTC)).Hours() / 24)
	switch days {
	case 0:
		return s.LeadHours
	case 1:
		return s.LeadDayBefore
	default:
		return fmt.Sprintf(s.LeadDays, days)
	}
}

// Leads names every reminder of one event, earliest first, joined in the
// language; two that read alike are said once. immediate is the earliest
// firing's own fact. told is when the user told Nooma about the event (the
// capture instant): a ring on its local day, for an event on a later day, is
// today at its hour (ADR-0037). A zero told names none so.
func (s Set) Leads(immediate bool, told, about time.Time, fires ...time.Time) string {
	var said []string
	for i, fire := range fires {
		switch {
		case (!immediate || i > 0) && s.onToldDay(told, about, fire):
			said = append(said, s.TodayAt(fire.In(about.Location())))
		default:
			said = append(said, s.Lead(immediate && i == 0, fire, about))
		}
	}
	return s.join(said)
}

// ExactLeads is Leads for leads the user just named (I40): a reminder a whole
// number of hours, under a day, before about is said as that number ("1 hora
// antes"), where Lead would say "unas horas antes".
func (s Set) ExactLeads(about time.Time, fires ...time.Time) string {
	var said []string
	for _, fire := range fires {
		said = append(said, s.exactLead(about, fire))
	}
	return s.join(said)
}

// exactLead is one ring of ExactLeads.
func (s Set) exactLead(about, fire time.Time) string {
	gap := about.Sub(fire)
	switch {
	case gap == time.Hour:
		return s.LeadHour
	case gap > time.Hour && gap < 24*time.Hour && gap%time.Hour == 0:
		return fmt.Sprintf(s.LeadHoursExact, int(gap/time.Hour))
	}
	return s.Lead(false, fire, about)
}

// NamedLeads is ExactLeads for the rings of an event whose message named its
// leads, in minutes (I44, ADR-0045): a ring that serves none of them, the one
// kept on the event's own day when every named lead is behind (ADR-0039), is
// said as when it rings ("ese día a las 9:00"), never as a lead nobody named.
func (s Set) NamedLeads(about time.Time, named []int, fires ...time.Time) string {
	var said []string
	for _, fire := range fires {
		local := fire.In(about.Location())
		y, m, d := local.Date()
		ay, am, ad := about.Date()
		switch {
		case slices.Contains(named, int(about.Sub(fire)/time.Minute)):
			said = append(said, s.exactLead(about, fire))
		case y == ay && m == am && d == ad:
			said = append(said, s.ThatDayAt(local))
		default:
			said = append(said, s.Lead(false, fire, about))
		}
	}
	return s.join(said)
}

// DayLeads is Leads for a day alone, day being its local midnight: a ring on
// the day itself names its hour (ThatDayAt), and one on another day says how
// long before, so an asked event on a day alone names the day before too. A
// ring on the told day, before the day, is today at its hour (ADR-0037).
func (s Set) DayLeads(immediate bool, told, day time.Time, fires ...time.Time) string {
	var said []string
	for i, fire := range fires {
		y, m, d := fire.In(day.Location()).Date()
		dy, dm, dd := day.Date()
		switch {
		case immediate && i == 0:
			said = append(said, s.LeadImmediate)
		case s.onToldDay(told, day, fire):
			said = append(said, s.TodayAt(fire.In(day.Location())))
		case y == dy && m == dm && d == dd:
			said = append(said, s.ThatDayAt(fire.In(day.Location())))
		default:
			said = append(said, s.Lead(false, fire, day))
		}
	}
	return s.join(said)
}

// onToldDay reports whether fire is on told's local day, about's zone, for an
// about on a later local day.
func (s Set) onToldDay(told, about, fire time.Time) bool {
	if told.IsZero() {
		return false
	}
	day := func(t time.Time) time.Time {
		y, m, d := t.In(about.Location()).Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	return day(fire).Equal(day(told)) && day(about).After(day(told))
}

// TodayAt names the hour of a ring today.
func (s Set) TodayAt(fire time.Time) string {
	return fmt.Sprintf(s.LeadTodayAt, s.Clock(fire))
}

// join says each lead once, in order, the last two joined in the language.
func (s Set) join(leads []string) string {
	var said []string
	for _, p := range leads {
		if !slices.Contains(said, p) {
			said = append(said, p)
		}
	}
	if len(said) < 2 {
		return strings.Join(said, "")
	}
	return strings.Join(said[:len(said)-1], ", ") + " " + s.And + " " + said[len(said)-1]
}
