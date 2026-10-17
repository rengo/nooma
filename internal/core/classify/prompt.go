package classify

import (
	"strconv"
	"strings"
	"time"

	"github.com/rengo/nooma/internal/core/unit"
)

// localDateLayout is how the injected local date is rendered. It is the same
// layout Decode accepts for a date-only value, so the model is shown dates in
// the shape it is asked to answer in.
const localDateLayout = "2006-01-02"

// localTimeLayout is how the injected local time is rendered: the HH:MM,
// 24-hour form normalized_content is asked to write a time in.
const localTimeLayout = "15:04"

// offsetLayout renders the instant's UTC offset, and exampleLayout renders
// the whole instant in the exact shape Decode's assignTime accepts.
//
// Both are built from now rather than written as literals so the example can
// never drift from a format the decoder would reject — the same anti-drift
// property joinVocabulary gives the closed sets, applied to a layout instead
// of a vocabulary. exampleLayout IS time.RFC3339: naming it here says that
// the shown example and the accepted format are one decision, not two that
// happen to agree.
const (
	offsetLayout  = "Z07:00"
	exampleLayout = time.RFC3339
)

// CalendarDays is how many days, today included, the classify context lists
// as a calendar — doc 02 §5 step 1 and §13. Fourteen so that "next Friday"
// said on a Friday, the furthest a bare weekday reaches, is still a row the
// model reads rather than a date it computes.
const CalendarDays = 14

// calendarRowHour is the time of day each calendar row is built at — a
// rendering detail, not a behavioural number: only the row's date is shown.
const calendarRowHour = 12

// Belief is a projection of one row of self_beliefs — design D4.
//
// Nothing in M1 reads that table (derive is M2, seeding is M4), so brain
// passes nil today. The type and the parameter exist anyway: with them, M2's
// capture→derive→inject cycle is a wiring change; without them it is a prompt
// rewrite, made under deadline, against a prompt whose behavior is already
// calibrated.
type Belief struct {
	Facet   string
	Content string
}

// BuildPrompt renders the capture-classification prompt — doc 02 §5 step 1,
// design D4. It is pure: same arguments, same string.
//
// now supplies the injected local date and the user's timezone, both of which
// §5 requires so the model can resolve "tomorrow" and "on Friday". The zone
// travels inside the instant rather than arriving from configuration or the
// environment: internal/core reads no OS state, there is no timezone key in
// nooma.yml, and the real Clock adapter's time.Now() already carries the
// process zone. A test Clock fixing a Location is what makes these assertions
// stable.
func BuildPrompt(text string, beliefs []Belief, now time.Time, archiveThreshold float64, rc ReminderContext) string {
	return buildPrompt(text, nil, beliefs, now, archiveThreshold, rc)
}

// BuildCorrectionPrompt is BuildPrompt for a message that corrects target,
// a unit its caller already named (doc 02 §5 step 4, I30): the same
// instructions plus the unit as it stands, and the rule that a correction
// is a patch. Without the unit, "Es a las 8am" names no date, so the model
// could only answer with content, and its normalization of the utterance
// replaced the unit's whole body while the date stayed where it was.
func BuildCorrectionPrompt(text string, target unit.Unit, beliefs []Belief, now time.Time, archiveThreshold float64, rc ReminderContext) string {
	return buildPrompt(text, &target, beliefs, now, archiveThreshold, rc)
}

func buildPrompt(text string, target *unit.Unit, beliefs []Belief, now time.Time, archiveThreshold float64, rc ReminderContext) string {
	var b strings.Builder

	b.WriteString("You classify a single message into Nooma's memory model.\n")
	b.WriteString("Answer with one JSON object and nothing else — no prose, no code fence.\n\n")

	b.WriteString("Context\n")
	// **The weekday and the time, not the date alone.** Given only
	// "2026-10-09", a small model had to work out that it was a Friday, and
	// resolved "dentista el viernes a las 10" to Tuesday 2026-10-13.
	b.WriteString("  Local date: " + now.Format(localDateLayout) + " (" + now.Weekday().String() + ")\n")
	b.WriteString("  Local time: " + now.Format(localTimeLayout) + "\n")
	// **The offset, not the zone's name.** Every caller supplies an instant
	// whose Location is time.Local, and time.Local.String() is the literal
	// string "Local" on every machine — a sentinel Location rather than a
	// named one. Rendering the name told the model "User timezone: Local"
	// and then asked it for absolute instants, which is a label no offset
	// can be derived from. The offset is the half of the zone the instant
	// genuinely carries, and it is the half the model has to write.
	b.WriteString("  UTC offset: " + now.Format(offsetLayout) + "\n")
	b.WriteString("  Resolve every relative reference (\"tomorrow\", \"on Friday\") against " +
		"that date and offset, and answer with absolute dates.\n")
	writeCalendar(&b, now)
	writeBeliefs(&b, beliefs)
	b.WriteString("\n")

	b.WriteString("Required fields\n")
	b.WriteString("  type                 one of: " + joinVocabulary(AllKinds()) + "\n")
	// **In the message's own language.** Without this line the model
	// rewrites into the language of these instructions, and a vault
	// stopped holding what its owner wrote: "recordame comprar cafe" was
	// stored as "Record to buy coffee." — translated, and mistranslated,
	// since "recordame" is "remind me" and not "record".
	//
	// The cost is not only fidelity. This string is what gets embedded
	// (brain.embedAndStore sends u.Content), while a recall query is
	// embedded raw (I22 — never normalized_content). Translating the
	// stored side put every vector comparison across a language boundary,
	// under ADR-0020's admission floor, which is the leg that decides
	// whether an answer is given at all.
	b.WriteString("  normalized_content   the message rewritten as a clean, self-contained " +
		"statement,\n")
	b.WriteString("                       **in the same language the message was written " +
		"in**. Do not\n")
	b.WriteString("                       translate it — this is what the person will read " +
		"back later,\n")
	b.WriteString("                       and what their own words are searched against.\n")
	// **One form for a resolved instant.** A later correction that moves
	// the date rewrites it in the body (correction.FollowDate), and it can
	// only find a form it knows: "a las 10" in one capture and "10:00" in
	// the next leaves the body naming a time the unit no longer has.
	b.WriteString("                       Where it names a date or time you resolved, write the " +
		"date as\n")
	b.WriteString("                       YYYY-MM-DD and the time as HH:MM, 24-hour (e.g. " +
		now.Format("2006-01-02 15:04") + ")\n")
	// **The scale, not just the question.** These two were asked for as
	// bare 0-1 floats — "how much this matters", "per-day forgetting
	// rate" — and a real model answered 0.5 and 0.3 for "buy coffee",
	// which are reasonable answers to the questions as posed and which
	// deleted the memory sixty-two seconds later. Nothing told the model
	// that the low end of one scale is a delete, or that a rate of 0.3
	// empties a memory in days.
	//
	// doc 02 §2 puts this decision with the model on purpose, and this
	// does not take it back: the model still chooses. It stops choosing
	// blind, which is the actual defect — §2's other half, the self-model
	// beliefs that personalize the value, is not wired yet, so the model
	// has been asked to personalize with nothing to personalize from.
	b.WriteString("  weight               0-1, how much this matters to the user. **A unit " +
		"whose weight\n")
	b.WriteString("                       falls below " + trimFloat(archiveThreshold) +
		" is archived on the next nightly pass and\n")
	b.WriteString("                       leaves active memory.** Anything worth keeping " +
		"belongs\n")
	b.WriteString("                       well above that; use the low end only for something " +
		"genuinely\n")
	b.WriteString("                       disposable\n")
	b.WriteString("  decay_rate           per-day forgetting rate, applied as " +
		"weight * exp(-rate * days).\n")
	b.WriteString("                       0.01 halves a memory in about 70 days; 0.1 in 7; " +
		"0.3 in 2.\n")
	b.WriteString("                       Low for emotional or identity-shaped content, " +
		"higher for a\n")
	b.WriteString("                       routine task — but a task worth remembering next " +
		"week needs\n")
	b.WriteString("                       a rate that still leaves it above the archive " +
		"threshold then\n")
	// **Required here, optional in the decoder — two decisions, not one.**
	// Listing it below as optional was a real defect: a live capture in
	// Spanish came back answered in English, and the trail showed the
	// model had simply not emitted the field. It was obeying the prompt.
	// "Optional fields — omit any that do not apply" is a fair
	// instruction for event_at, which genuinely does not apply to a
	// greeting; every message is written in SOME language, so the same
	// framing asked the model to skip a field that always applies.
	//
	// What stays optional is the wire contract (decode.go): a response
	// without this field still decodes, because every recording in
	// testdata/llm/cases/ predates it and `nooma doctor` counts a clean
	// case as one with zero degradations. Asking for it and surviving
	// without it are independent, and collapsing them into one "optional"
	// is what produced the defect.
	//
	// **The language of the message, never the language of this prompt.**
	// Said outright because the model is reading two languages at once
	// here — these English instructions and, often, a message in
	// something else — and the field is about the second one.
	b.WriteString("  language             one of: " + joinVocabulary(AllLanguages()) +
		" — the language the MESSAGE is written in, not the language\n")
	b.WriteString("                       of these instructions. Every message is written in " +
		"some\n")
	b.WriteString("                       language, so this always applies. Leave it out only " +
		"if the\n")
	b.WriteString("                       message is in neither of them — an absent value means " +
		"\"not\n")
	b.WriteString("                       one of these\", and naming one you did not read is " +
		"worse\n\n")

	b.WriteString("Optional fields — omit any that do not apply\n")
	b.WriteString("  structured_data      free-form JSON object, shape follows from type\n")
	b.WriteString("  event_at, due_at     absolute; either \"YYYY-MM-DD\" or an RFC3339 timestamp\n")
	b.WriteString("                       carrying its offset, e.g. " + now.Format(exampleLayout) + ".\n")
	b.WriteString("                       A timestamp without an offset is not accepted.\n")
	b.WriteString("                       event_at is when a thing happens in the world; due_at is\n")
	b.WriteString("                       when something is owed. A timer has no world event, so its\n")
	b.WriteString("                       instant belongs in due_at and never in event_at\n")
	b.WriteString("  title                a short name for what to remember, in the message's own\n")
	b.WriteString("                       language, at most " + strconv.Itoa(MaxTitleRunes) + " characters, **with no date, no\n")
	b.WriteString("                       day and no time in it**: not \"2026-10-14\", not \"22:30\", not\n")
	b.WriteString("                       \"mañana\", \"jueves\" or \"3pm\". \"Reunión con Juan\", never\n")
	b.WriteString("                       \"Reunión con Juan mañana a las 22:30\". Give one for anything\n")
	b.WriteString("                       that has a date or that the user asks to be reminded of\n")
	b.WriteString("  interrupt_level      0-1, how urgently this should interrupt the user " +
		"versus wait for a digest\n")
	b.WriteString("  recurrence_rule      one of: " + joinVocabulary(AllRecurrenceRules()) +
		" — only for a recurring reminder\n")
	for _, f := range orthogonalFields() {
		b.WriteString("  " + pad(f.name, 20) + " one of: " + f.members + "\n")
	}
	b.WriteString("\n")

	b.WriteString("One message can both be a capture and resolve a pending question — the " +
		"fields above are independent of each other and of type.\n")
	b.WriteString("Omit any field you are unsure of. A missing field is recoverable; " +
		"an invented one is not.\n\n")

	b.WriteString("Choosing the type\n")
	b.WriteString("  Three of the types above are decided by what the message DOES, not by what\n")
	b.WriteString("  it is about. Read the message as an act before you read it as a subject.\n")
	b.WriteString("  recall       the user ASKS for something Nooma may already hold. A question\n")
	b.WriteString("               about what Nooma knows is recall however it is worded — asking\n")
	b.WriteString("               what you know about a topic is not knowledge about that topic.\n")
	b.WriteString("  knowledge    the user TELLS Nooma a fact to keep.\n")
	b.WriteString("  correction   the user ALTERS something captured earlier — a new date, a new\n")
	b.WriteString("               value, a different detail. An imperative that moves or changes\n")
	b.WriteString("               an existing thing corrects it; it does not create a new one.\n")
	b.WriteString("  A recall stores nothing and is answered. Classifying a question as knowledge\n")
	b.WriteString("  files the question away instead of answering it.\n")
	// **When, not what.** "dentista el viernes a las 10" came back as a
	// task with a due_at, and a task arms nothing (prospection.Arm), so the
	// appointment got no reminder. The prompt defined event_at and due_at
	// but never said which type each belongs to.
	b.WriteString("  Two more are decided by how the message is dated.\n")
	b.WriteString("  event        something that HAPPENS at a set day or time — an appointment, a\n")
	b.WriteString("               meeting, the dentist. Its day and time go in event_at.\n")
	b.WriteString("  task         something the user has to GET DONE, by a deadline if one is named.\n")
	b.WriteString("               The deadline goes in due_at.\n")
	b.WriteString("  An appointment filed as a task gets no reminder.\n")
	// **A reply is not a memory.** A bare "listo" came back as a task, and
	// capture stored "La tarea está hecha" as a unit of its own.
	b.WriteString("  chitchat     a message that only answers Nooma (a check-in, a nudge, a question it\n")
	b.WriteString("               asked) or only talks, with nothing new to keep. It stores nothing:\n")
	b.WriteString("               its answer travels in the outcome fields, never as a new task.\n")
	// **One place for the which-item answer.** It only answers Nooma, like a
	// chitchat, and can settle a correction; three rounds of clauses here and
	// in the check-in rules kept contradicting Follow-ups (ADR-0036).
	if chatShown(rc) {
		b.WriteString(whichItemPointer)
	}
	b.WriteString("\n")

	b.WriteString("Corrections\n")
	b.WriteString("  A correction carries the corrected VALUE, not a description of the change.\n")
	b.WriteString("  If it corrects a date, resolve it against the local date above and put the\n")
	b.WriteString("  new date in event_at or due_at — those fields are how a correction takes\n")
	b.WriteString("  effect, and a correction that omits them changes nothing.\n")
	b.WriteString("  A correction still answers every required field above, like any other type.\n\n")

	writeReminders(&b, rc)
	// **Only beside a chat** (ADR-0036). A follow-up and a which-item answer
	// exist only when one is shown; without one the prompt is the one the
	// reminder set was read with before, which the follow-up text cost two
	// nudge readings.
	if chatShown(rc) {
		writeFollowUps(&b, rc.Conversation, now)
	}

	if target != nil {
		writeTarget(&b, *target, now)
	}

	b.WriteString("Message\n")
	b.WriteString(text + "\n")

	return b.String()
}

// writeReminders renders the reminder fields (ADR-0033 point 1) and, only when
// there is one, the question Nooma has open and the numbered list a position
// refers to. The model reads what the user means; the code checks every answer
// against what it sent, so this section explains and never asks the model to
// name a reminder.
//
// **One list.** An open which_reminder question carries its own list inside its
// text, and then the candidates are left out: two lists in one prompt would
// give "2" two meanings. Any other open question leaves the candidates in.
func writeReminders(b *strings.Builder, rc ReminderContext) {
	b.WriteString("Reminders\n")
	b.WriteString("  Read what the user MEANS in any language. These fields only say what you read;\n")
	b.WriteString("  Nooma checks them before it does anything.\n")
	b.WriteString("  remind_request     true when the user asks Nooma to remind them of what THIS\n")
	b.WriteString("                     message stores: \"recordame comprar café mañana a las 10\",\n")
	b.WriteString("                     \"remind me to call Ana at 5pm\", \"si no, avisame\", \"no te\n")
	b.WriteString("                     olvides de recordarme X\", \"haceme acordar\". false (or null) for\n")
	b.WriteString("                     everything else: a plain note; a question, as in \"recordame qué\n")
	b.WriteString("                     hablamos con Juan\" (that asks what was said: a recall); a\n")
	b.WriteString("                     reminder someone else sent, as in \"my dentist sent a reminder to\n")
	b.WriteString("                     confirm\"; the past, as in \"recordé que tenía que comprar café\";\n")
	b.WriteString("                     or reported speech, as in \"Ana me dijo 'avisame cuando llegues'\"\n")
	b.WriteString("  reminder_intent    one of: " + joinVocabulary(AllReminderIntents()) + "\n")
	b.WriteString("                     stop: the user asks to stop being reminded of something that\n")
	b.WriteString("                     already exists (\"no me lo recuerdes más\", \"don't remind me\n")
	// **Done by its meaning, never by example.** Listing "listo, nos vemos
	// mañana" here sent a farewell to done, the reading ADR-0033 names a
	// misread, and a stop read a done beside it.
	b.WriteString("                     anymore\"). A stop is not also a check-in answer: give it no\n")
	b.WriteString("                     task_checkin_outcome and no nudge_outcome. Saying a task is\n")
	b.WriteString("                     finished is none, not stop: it is task_checkin_outcome done.\n")
	b.WriteString("                     keep: only while the open question below is an undo, when the\n")
	b.WriteString("                     user says the stop was a mistake (\"no, seguí\", \"keep it\"). A\n")
	b.WriteString("                     bare \"sí\", \"ok\" or \"dale\" is none\n")
	b.WriteString("  question_answer    one of: " + joinVocabulary(AllQuestionAnswers()) + ", and only while a\n")
	// **The instant, not only the words.** A bare day answering remind_when came
	// back with the day in normalized_content alone, which arms nothing.
	b.WriteString("                     question below is open: when (it gives a day or time: put that\n")
	b.WriteString("                     instant in due_at, or in event_at for an event, a day with no time\n")
	b.WriteString("                     as a bare YYYY-MM-DD; normalized_content alone loses it), no (it\n")
	b.WriteString("                     declines: \"no\", \"no hace falta\", \"nope\"), choice (it picks an\n")
	b.WriteString("                     entry of the numbered list; put the position in choice_number,\n")
	b.WriteString("                     1 to " + strconv.Itoa(MaxReminderChoices) + "). Otherwise none or leave it out\n")
	b.WriteString("  reminder_target    for a stop: the position, 1 to " + strconv.Itoa(MaxReminderChoices) +
		", of the numbered reminder it is\n")
	b.WriteString("                     about, or \"" + ReminderTargetUnclear + "\" when the one the user describes is not\n")
	b.WriteString("                     in the list or you cannot tell. Never invent a position\n")
	if chatShown(rc) {
		b.WriteString(whichItemPointer)
	}
	if rc.Open != nil {
		b.WriteString("\n" + openQuestionHeading + " (" + string(rc.Open.Kind) + ")\n")
		// Verbatim and unindented: the text, and the list in it, are what the
		// user read, and a position means the line that starts with it.
		b.WriteString("  Nooma asked the user, in these words:\n")
		b.WriteString(strings.TrimRight(rc.Open.Text, "\n") + "\n")
		b.WriteString("  The message may be the answer. Read it as one only if it answers THIS question.\n")
		// **The question is about a stop; the answer is not one.** Asked "which
		// one" with no reason given, the model guessed the reason and read "el 2"
		// as a stop of its own. The stop is the one Nooma holds (ADR-0033).
		if rc.Open.Kind == QuestionWhichReminder {
			b.WriteString("  Nooma asked this to learn which reminder a stop the user already asked for is\n")
			b.WriteString("  about. An answer picks that reminder: set question_answer choice and\n")
			b.WriteString("  choice_number, and nothing else. The answer is not itself a stop: reminder_intent\n")
			b.WriteString("  is none and reminder_target is left out. Nooma completes the stop it holds.\n")
		}
	} else {
		// Said, not left to be noticed: "only while a question is open" asked the
		// model to see an absence, and a bare "no" was read as an answer.
		b.WriteString("\n  No reminder question is open, so question_answer is none, whatever the\n")
		b.WriteString("  message sounds like.\n")
	}
	if len(rc.Candidates) > 0 && (rc.Open == nil || rc.Open.Kind != QuestionWhichReminder) {
		b.WriteString("\n" + candidatesHeading + "\n")
		// The list is what a stop names a position in, so it is in every prompt
		// that has one; it is not a hint that the user wants one stopped.
		b.WriteString("  Shown so a stop can name one by its position. Being listed here is not a\n")
		b.WriteString("  request to stop: only a message that asks to stop being reminded is a stop.\n")
		b.WriteString(RenderReminderList(rc.Candidates))
	}
	b.WriteString("\n")
	writeCheckins(b, chatShown(rc))
}

// writeCheckins says what each check-in outcome means (doc 02 §5 step 1). The
// fields alone, with their values named and never explained, got no outcome
// back for a task done or put off or a nudge taken up, and the code resolves a
// check-in from the outcome alone. A meaning, never the words for it: the
// model reads what the user means in any language.
func writeCheckins(b *strings.Builder, chat bool) {
	b.WriteString(checkinHeading + "\n")
	b.WriteString("  Nooma asks the user about a task it reminded them of, and nudges them toward\n")
	// With a chat shown, a push may be in it: it is which one is open that is
	// never shown.
	if chat {
		b.WriteString("  something they meant to do. Which one is open is not shown here: say what the\n")
	} else {
		b.WriteString("  something they meant to do. The open one is not shown here: say what the\n")
	}
	b.WriteString("  message answers, and Nooma matches it to what it asked, or drops it.\n")
	// **The bare word's rule** (format.md): a short reply with nothing asked
	// in front of the model is the check-in's answer. A bare yes and a nudge
	// taken up read no outcome without it, and the nudge stayed open. A bare
	// decline is "ask me later" (the client's ruling on #365): only an
	// explicit refusal closes the check-in.
	b.WriteString("  With no reminder question open, a message that is only a short reply\n")
	b.WriteString("  (agreeing, declining, or saying it is done) answers the check-in: give the\n")
	b.WriteString("  outcome it means. A short decline alone means not yet: task_checkin_outcome\n")
	b.WriteString("  snooze, and no nudge_outcome. Only an explicit refusal, to do the task or to be\n")
	b.WriteString("  asked about it, is drop or declined.\n")
	// **The other half of the bare-word rule.** An answer to an undo or a
	// remind_when, or to a relation question (never a reminder question), was
	// invited to carry a check-in outcome too, and closed a check-in nobody
	// answered.
	b.WriteString("  A message that answers an open reminder question, or says whether two things\n")
	b.WriteString("  are related, answers that and not a check-in: it carries no\n")
	b.WriteString("  task_checkin_outcome and no nudge_outcome.\n")
	if chat {
		b.WriteString(whichItemPointer)
	}
	// **A farewell is not a done.** It is a short reply, and one that began
	// with the bare done word was read as done, ADR-0033's named misread.
	b.WriteString("  A message that only closes the conversation, a goodbye or a thanks, answers\n")
	b.WriteString("  no check-in, even when it begins with a word that alone would.\n")
	b.WriteString("  task_checkin_outcome, about a task:\n")
	for _, o := range AllTaskCheckinOutcomes() {
		b.WriteString("    " + pad(string(o), 9) + taskCheckinMeaning(o) + "\n")
	}
	b.WriteString("  nudge_outcome, about a nudge:\n")
	for _, o := range AllNudgeOutcomes() {
		b.WriteString("    " + pad(string(o), 9) + nudgeMeaning(o) + "\n")
	}
	b.WriteString("\n")
}

func taskCheckinMeaning(o TaskCheckinOutcome) string {
	switch o {
	case TaskCheckinOutcomeDone:
		return "the user says the task is finished"
	case TaskCheckinOutcomeSnooze:
		return "the user puts the task off to later and still means to do it"
	case TaskCheckinOutcomeDrop:
		return "the user gives the task up: it no longer needs doing"
	}
	return ""
}

func nudgeMeaning(o NudgeOutcome) string {
	switch o {
	case NudgeOutcomeEngaged:
		return "the user takes the nudge up: agrees to it, or asks to go on with it"
	case NudgeOutcomeDeclined:
		return "the user turns the nudge down"
	}
	return ""
}

// writeTarget renders the unit a correction patches, its instants in the
// user's own frame (the one the message speaks in), and the patch rule.
func writeTarget(b *strings.Builder, u unit.Unit, now time.Time) {
	instant := func(t *time.Time) string { return shownInstant(t, now) }
	title := "none"
	if u.Title != "" {
		title = strconv.Quote(u.Title)
	}
	b.WriteString("Correcting this unit\n")
	b.WriteString("  The message below corrects this unit, whatever it reads like on its own.\n")
	// Quoted, so a body's own line breaks — or a line of it reading
	// "Message" — cannot pass for a section of this prompt.
	b.WriteString("  Current text (quoted): " + strconv.Quote(u.Content) + "\n")
	b.WriteString("  Current event_at: " + instant(u.EventAt) + "\n")
	b.WriteString("  Current due_at: " + instant(u.DueAt) + "\n")
	b.WriteString("  Current title (quoted): " + title + "\n")
	b.WriteString("  A correction is a PATCH: change only what the message speaks about and keep\n")
	b.WriteString("  everything else.\n")
	b.WriteString("  normalized_content is the unit's WHOLE corrected text — the current text with\n")
	b.WriteString("  only that change applied, never the message on its own.\n")
	b.WriteString("  If the message changes the day or the time of event_at or due_at, answer that\n")
	b.WriteString("  field with the complete new instant:\n")
	b.WriteString("  keep the current date when only the time changes,\n")
	b.WriteString("  and keep the current time when only the date changes.\n")
	b.WriteString("  An instant shown as a bare date has no time: when only its day changes, answer\n")
	b.WriteString("  the new bare date, YYYY-MM-DD, and add no time.\n")
	b.WriteString("  If the message changes the day or the time, also answer a fresh title, with no\n")
	b.WriteString("  date, day or time in it as always: a title that still names the old day or\n")
	b.WriteString("  time is stale once it has moved.\n\n")
}

// chatShown is whether the prompt shows a recent chat, and so carries the
// follow-up machinery (ADR-0036).
func chatShown(rc ReminderContext) bool { return len(rc.Conversation.Turns) > 0 }

// shownInstant is how the prompt shows a stored instant, in the user's frame.
// An instant that is the first moment of its local day is a day with no time:
// shown as the bare date the model would write it as, never as a midnight it
// would then be told to "keep".
func shownInstant(t *time.Time, now time.Time) string {
	if t == nil {
		return "none"
	}
	local := t.In(now.Location())
	if start, ok := parseDateOnly(local.Format(dateOnlyLayout), now.Location()); ok && start.Equal(local) {
		return local.Format(dateOnlyLayout)
	}
	return local.Format(exampleLayout)
}

// writeCalendar renders the next CalendarDays days, today first and marked,
// and the rule a bare weekday resolves by — doc 02 §5 step 1. A model reads a
// weekday off this table rather than computing one, which is the arithmetic
// it got wrong.
//
// Each row is built at noon of its civil date, in the instant's own zone,
// rather than by stepping now itself: now's time of day can fall in a DST
// gap on a later date — Chile skips midnight to 01:00 — and a time in a gap
// normalises onto the neighbouring day, repeating one date and dropping
// the next. No zone moves its clocks at noon.
func writeCalendar(b *strings.Builder, now time.Time) {
	b.WriteString("  A weekday named alone (\"on Friday\", \"el viernes\") means its next " +
		"occurrence after today;\n")
	b.WriteString("  named on that same weekday, it means next week's, 7 days later. " +
		"\"today\" (\"hoy\") means today.\n")
	b.WriteString("  Read the date off this calendar; do not compute it:\n")
	for i := 0; i < CalendarDays; i++ {
		day := time.Date(now.Year(), now.Month(), now.Day()+i, calendarRowHour, 0, 0, 0, now.Location())
		b.WriteString("    " + pad(day.Weekday().String(), 10) + day.Format(localDateLayout))
		if i == 0 {
			b.WriteString(" (today)")
		}
		b.WriteString("\n")
	}
}

// writeBeliefs renders the self-model projection, or nothing at all when
// there is none. Nothing at all — not an empty header — because a heading
// with no content under it reads to a model as a section it failed to fill.
func writeBeliefs(b *strings.Builder, beliefs []Belief) {
	if len(beliefs) == 0 {
		return
	}
	b.WriteString("  What is known about this user:\n")
	for _, belief := range beliefs {
		b.WriteString("    " + belief.Facet + ": " + belief.Content + "\n")
	}
}

// orthogonalFields pairs each of the six orthogonal resolution fields with
// its rendered vocabulary — doc 02 §5 at 02:120-123.
//
// A slice rather than a map, so the prompt is byte-identical across runs: Go
// randomizes map iteration, and a prompt that varies run to run cannot be
// diffed, cached, or recorded as a golden case.
func orthogonalFields() []struct{ name, members string } {
	return []struct{ name, members string }{
		{"nudge_outcome", joinVocabulary(AllNudgeOutcomes())},
		{"relation_outcome", joinVocabulary(AllRelationOutcomes())},
		{"state_outcome", joinVocabulary(AllStateOutcomes())},
		{"task_checkin_outcome", joinVocabulary(AllTaskCheckinOutcomes())},
		{"list_op", joinVocabulary(AllListOps())},
		{"person_ref_status", joinVocabulary(AllPersonRefStatuses())},
	}
}

// joinVocabulary renders a closed vocabulary for the prompt, from the same
// AllX() the decoder matches against. That shared source is the point: a
// value added to a vocabulary reaches the prompt with no second edit, so the
// model can never be asked for a set the decoder would then reject.
func joinVocabulary[T ~string](all []T) string {
	parts := make([]string, 0, len(all))
	for _, v := range all {
		parts = append(parts, string(v))
	}
	return strings.Join(parts, " | ")
}

// pad right-pads name to width so the field list stays in columns. Long names
// are returned unchanged rather than truncated — a misaligned prompt is a
// cosmetic problem, a truncated field name is a wrong one.
func pad(name string, width int) string {
	if len(name) >= width {
		return name
	}
	return name + strings.Repeat(" ", width-len(name))
}

// trimFloat renders a threshold the way a person writes one — "0.5", not
// "0.500000". strconv with -1 precision gives the shortest representation
// that round-trips, which is what a prompt wants: a model shown
// "0.5000000000000001" reads noise where a boundary was meant.
func trimFloat(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// The headings of the two optional reminder sections. A section is written only
// when there is something under it, so the heading's presence is the test that
// it was.
const (
	openQuestionHeading = "Open reminder question"
	// whichItemPointer is the one line every section that could claim an
	// answer to Nooma's which-item question carries instead of a rule of its
	// own: the answer is defined in Follow-ups and nowhere else (ADR-0036).
	whichItemPointer  = "  An answer to Nooma's which-item question is read under Follow-ups, not here.\n"
	candidatesHeading = "Reminders that may be stopped"
	checkinHeading    = "Check-in answers"
)
