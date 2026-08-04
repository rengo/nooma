package classify

import (
	"strings"
	"time"
)

// localDateLayout is how the injected local date is rendered. It is the same
// layout Decode accepts for a date-only value, so the model is shown dates in
// the shape it is asked to answer in.
const localDateLayout = "2006-01-02"

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
	b.WriteString("  User timezone: " + now.Location().String() + "\n")
	b.WriteString("  Resolve every relative reference (\"tomorrow\", \"on Friday\") against " +
		"that date and zone, and answer with absolute dates.\n")
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
	b.WriteString("  event_at, due_at     absolute; \"YYYY-MM-DD\" or a full RFC3339 timestamp\n")
	for _, f := range orthogonalFields() {
		b.WriteString("  " + pad(f.name, 20) + " one of: " + f.members + "\n")
	}
	b.WriteString("\n")

	b.WriteString("One message can both be a capture and resolve a pending question — the " +
		"fields above are independent of each other and of type.\n")
	b.WriteString("Omit any field you are unsure of. A missing field is recoverable; " +
		"an invented one is not.\n\n")

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
