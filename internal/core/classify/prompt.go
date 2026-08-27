package classify

import (
	"strings"
	"time"
)

// localDateLayout is how the injected local date is rendered. It is the same
// layout Decode accepts for a date-only value, so the model is shown dates in
// the shape it is asked to answer in.
const localDateLayout = "2006-01-02"

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
func BuildPrompt(text string, beliefs []Belief, now time.Time) string {
	var b strings.Builder

	b.WriteString("You classify a single message into Nooma's memory model.\n")
	b.WriteString("Answer with one JSON object and nothing else — no prose, no code fence.\n\n")

	b.WriteString("Context\n")
	b.WriteString("  Local date: " + now.Format(localDateLayout) + "\n")
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
	writeBeliefs(&b, beliefs)
	b.WriteString("\n")

	b.WriteString("Required fields\n")
	b.WriteString("  type                 one of: " + joinVocabulary(AllKinds()) + "\n")
	b.WriteString("  normalized_content   the message rewritten as a clean, self-contained " +
		"statement\n")
	b.WriteString("  weight               0-1, how much this matters to the user\n")
	b.WriteString("  decay_rate           per-day forgetting rate; low for emotional or " +
		"identity-shaped content, high for routine tasks\n\n")

	b.WriteString("Optional fields — omit any that do not apply\n")
	b.WriteString("  structured_data      free-form JSON object, shape follows from type\n")
	b.WriteString("  event_at, due_at     absolute; either \"YYYY-MM-DD\" or an RFC3339 timestamp\n")
	b.WriteString("                       carrying its offset, e.g. " + now.Format(exampleLayout) + ".\n")
	b.WriteString("                       A timestamp without an offset is not accepted.\n")
	b.WriteString("                       event_at is when a thing happens in the world; due_at is\n")
	b.WriteString("                       when something is owed. A timer has no world event, so its\n")
	b.WriteString("                       instant belongs in due_at and never in event_at\n")
	b.WriteString("  interrupt_level      0-1, how urgently this should interrupt the user " +
		"versus wait for a digest\n")
	b.WriteString("  recurrence_rule      one of: " + joinVocabulary(AllRecurrenceRules()) +
		" — only for a recurring reminder\n")
	// **The language of the message, never the language of this prompt.**
	// The instruction says so outright because the model is reading two
	// languages at once here — these English instructions and, often, a
	// message in something else — and the field it is being asked for is
	// about the second one.
	b.WriteString("  language             one of: " + joinVocabulary(AllLanguages()) +
		" — the language the MESSAGE is written in, not the language of\n")
	b.WriteString("                       these instructions. Omit it if the message is in " +
		"neither\n")
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
	b.WriteString("  files the question away instead of answering it.\n\n")

	b.WriteString("Corrections\n")
	b.WriteString("  A correction carries the corrected VALUE, not a description of the change.\n")
	b.WriteString("  If it corrects a date, resolve it against the local date above and put the\n")
	b.WriteString("  new date in event_at or due_at — those fields are how a correction takes\n")
	b.WriteString("  effect, and a correction that omits them changes nothing.\n")
	b.WriteString("  A correction still answers every required field above, like any other type.\n\n")

	b.WriteString("Message\n")
	b.WriteString(text + "\n")

	return b.String()
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
