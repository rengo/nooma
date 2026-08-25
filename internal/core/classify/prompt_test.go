package classify

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

// buenosAires and kolkata are fixed in memory rather than loaded with
// time.LoadLocation, which reads tzdata off disk — depguard denies os to
// internal/core/** with no $test selector. Kolkata's +05:30 is deliberate: a
// half-hour offset catches an implementation that renders a whole-hour zone
// by accident.
var (
	buenosAires = time.FixedZone("America/Argentina/Buenos_Aires", -3*60*60)
	kolkata     = time.FixedZone("Asia/Kolkata", 5*60*60+30*60)
)

// TestBuildPrompt_CarriesTheInstantsOwnZone is design D4's whole timezone
// mechanism under test. Doc 02 §5 injects "local date + user timezone" so the
// model can resolve "tomorrow" and "on Friday"; harness §2 forbids core from
// reading that zone from the OS. The resolution: the zone travels inside the
// time.Time the clock returns.
//
// So the same wall-clock moment in two zones must produce two different
// prompts. If it does not, BuildPrompt is reading the zone from somewhere
// other than its argument — which is the failure the whole mechanism exists
// to prevent, and which would pass unnoticed on a machine whose own zone
// matched the fixture.
func TestBuildPrompt_CarriesTheInstantsOwnZone(t *testing.T) {
	instant := time.Date(2026, 8, 4, 9, 30, 0, 0, time.UTC)

	inBuenosAires := BuildPrompt("pick up the dry cleaning on Friday", nil, instant.In(buenosAires))
	inKolkata := BuildPrompt("pick up the dry cleaning on Friday", nil, instant.In(kolkata))

	if inBuenosAires == inKolkata {
		t.Fatal("the same instant in two zones produced identical prompts — the zone is not " +
			"reaching the prompt, so the model cannot resolve \"tomorrow\" correctly (design D4)")
	}

	if !strings.Contains(inBuenosAires, buenosAires.String()) {
		t.Errorf("prompt does not name the zone %q — doc 02 §5 injects the user timezone",
			buenosAires.String())
	}
	if !strings.Contains(inKolkata, kolkata.String()) {
		t.Errorf("prompt does not name the zone %q", kolkata.String())
	}
}

// TestBuildPrompt_RendersTheLocalDate covers the other half of §5's injected
// context: the date the model resolves relative references against. It must
// be the date in the instant's own zone, not UTC's — the two disagree for
// part of every day, which is exactly when "tomorrow" goes wrong.
func TestBuildPrompt_RendersTheLocalDate(t *testing.T) {
	// 01:00 UTC on the 5th is still 22:00 on the 4th in Buenos Aires.
	instant := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC).In(buenosAires)

	prompt := BuildPrompt("remind me tomorrow", nil, instant)

	if !strings.Contains(prompt, "2026-08-04") {
		t.Errorf("prompt does not carry the local date 2026-08-04; it is 01:00 UTC on the 5th "+
			"but still the 4th in %s, and the model resolves \"tomorrow\" from this.\n\n%s",
			buenosAires.String(), prompt)
	}
	if strings.Contains(prompt, "2026-08-05") {
		t.Error("prompt carries 2026-08-05 — that is the UTC date, not the user's")
	}
}

// TestBuildPrompt_CarriesTheMessage: the text under classification reaches
// the prompt intact. Stated separately because everything else here is
// context, and context with no message is a well-formed prompt that asks
// nothing.
func TestBuildPrompt_CarriesTheMessage(t *testing.T) {
	const message = `she said "call me Friday" — and I said ok`

	prompt := BuildPrompt(message, nil, time.Date(2026, 8, 4, 9, 0, 0, 0, buenosAires))

	if !strings.Contains(prompt, message) {
		t.Errorf("prompt does not contain the message verbatim, quotes and dashes included.\n\n%s",
			prompt)
	}
}

// TestBuildPrompt_RendersEveryVocabularyFromItsOwnSource is the guard against
// the failure mode a hand-written prompt invites: the code gains a taxonomy
// value and the prompt keeps asking for the old thirteen. Every vocabulary in
// the prompt is rendered from AllX(), so drift is unrepresentable — and this
// test iterates the same AllX() rather than a literal list, so a value added
// later is checked without editing this file.
func TestBuildPrompt_RendersEveryVocabularyFromItsOwnSource(t *testing.T) {
	prompt := BuildPrompt("anything", nil, time.Date(2026, 8, 4, 9, 0, 0, 0, buenosAires))

	for _, k := range AllKinds() {
		if !strings.Contains(prompt, string(k)) {
			t.Errorf("prompt does not offer the taxonomy value %q — the model cannot return "+
				"a value it was never told about", k)
		}
	}

	orthogonal := map[string][]string{
		"nudge_outcome":        asStrings(AllNudgeOutcomes()),
		"relation_outcome":     asStrings(AllRelationOutcomes()),
		"state_outcome":        asStrings(AllStateOutcomes()),
		"task_checkin_outcome": asStrings(AllTaskCheckinOutcomes()),
		"list_op":              asStrings(AllListOps()),
		"person_ref_status":    asStrings(AllPersonRefStatuses()),
	}
	if len(orthogonal) != 6 {
		t.Fatalf("table covers %d orthogonal fields, doc 02:120-123 names 6", len(orthogonal))
	}
	for field, members := range orthogonal {
		if !strings.Contains(prompt, field) {
			t.Errorf("prompt never names the field %q", field)
		}
		for _, m := range members {
			if !strings.Contains(prompt, m) {
				t.Errorf("prompt does not offer %s = %q", field, m)
			}
		}
	}
}

// TestBuildPrompt_RendersProspectionFields pins design §3.8's prompt
// widening: the model is asked for interrupt_level and recurrence_rule
// alongside the fields M1 already asked for, with doc 02 §7's own guidance —
// the [0,1] range for one, the closed vocabulary for the other — stated,
// not left implied. recurrence_rule's vocabulary is rendered from
// AllRecurrenceRules(), the same "no drift" property the six orthogonal
// fields already have (TestBuildPrompt_RendersEveryVocabularyFromItsOwnSource).
func TestBuildPrompt_RendersProspectionFields(t *testing.T) {
	prompt := BuildPrompt("anything", nil, time.Date(2026, 8, 4, 9, 0, 0, 0, buenosAires))

	// The timer's own field rule (design §3.7 decision 1). A model left to
	// guess which of the two date fields a reminder belongs in will
	// sometimes guess event_at, and I18 forbids the two being
	// interchanged — so the prompt states it rather than hoping.
	for _, want := range []string{"due_at", "never in event_at"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt does not state the timer's field rule: missing %q", want)
		}
	}

	for _, want := range []string{"interrupt_level", "recurrence_rule", "0-1"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt does not mention %q", want)
		}
	}
	for _, m := range AllRecurrenceRules() {
		if !strings.Contains(prompt, string(m)) {
			t.Errorf("prompt does not offer recurrence_rule = %q", m)
		}
	}
}

func asStrings[T ~string](vs []T) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, string(v))
	}
	return out
}

// TestBuildPrompt_BeliefsAreOptionalAndRendered covers design D4's deliberate
// dead parameter. Nothing in M1 reads self_beliefs, so brain passes nil — and
// the parameter exists anyway, so M2's capture→derive→inject cycle is a
// wiring change rather than a prompt rewrite.
//
// A parameter that is always nil is a parameter nobody has run. This test is
// what keeps it honest before M2 depends on it.
func TestBuildPrompt_BeliefsAreOptionalAndRendered(t *testing.T) {
	now := time.Date(2026, 8, 4, 9, 0, 0, 0, buenosAires)

	withoutBeliefs := BuildPrompt("anything", nil, now)
	withBeliefs := BuildPrompt("anything", []Belief{
		{Facet: "goal", Content: "ship Nooma's first milestone"},
		{Facet: "habit", Content: "practices guitar most evenings"},
	}, now)

	if withoutBeliefs == withBeliefs {
		t.Fatal("beliefs changed nothing in the prompt — the parameter is decorative, and M2 " +
			"would discover that only once it started passing real ones")
	}

	for _, want := range []string{
		"goal", "ship Nooma's first milestone",
		"habit", "practices guitar most evenings",
	} {
		if !strings.Contains(withBeliefs, want) {
			t.Errorf("prompt with beliefs does not contain %q", want)
		}
	}

	// The empty case must not leave a dangling header with nothing under it.
	if strings.Contains(withoutBeliefs, "goal") {
		t.Error("prompt with nil beliefs mentions a belief facet")
	}
}

// TestBuildPrompt_IsPure: same arguments, same string, every time. Stated
// because BuildPrompt is exactly the kind of function that grows a
// time.Now() or a map iteration by accident, and either would surface here as
// flakiness rather than as a clear failure.
func TestBuildPrompt_IsPure(t *testing.T) {
	now := time.Date(2026, 8, 4, 9, 0, 0, 0, buenosAires)
	beliefs := []Belief{{Facet: "goal", Content: "ship it"}}

	first := BuildPrompt("anything", beliefs, now)
	for i := range 20 {
		if got := BuildPrompt("anything", beliefs, now); got != first {
			t.Fatalf("call %d differed from the first — BuildPrompt is not deterministic, "+
				"most likely a map iterated in place of a slice", i+2)
		}
	}
}

// TestBuildPrompt_AsksACorrectionForTheCorrectedValue pins doc 02 §5 step 4's
// prompt clause. It is not a wording preference: a live model asked to
// classify "no, the dentist is on the 15th, not the 14th" returned no
// event_at at all — it read "correction" as "not an event" — and the
// correction path then took its content fallback, overwriting the unit's body
// while leaving the wrong date in place. The second clause is equally
// load-bearing: naming the date without restating the required fields made
// the same model drop weight and decay_rate to make room.
func TestBuildPrompt_AsksACorrectionForTheCorrectedValue(t *testing.T) {
	p := BuildPrompt("no, the dentist is on the 15th, not the 14th", nil, time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC))

	for _, want := range []string{
		"corrected VALUE",
		"event_at or due_at",
		"still answers every required field",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("BuildPrompt does not tell the model %q; a correction that omits the corrected date changes nothing", want)
		}
	}
}

// TestBuildPrompt_SeparatesAskingFromTelling pins the type-selection clause.
// Before it, the prompt named the thirteen-member vocabulary and defined no
// member of it, so a live model classified "what do you know about X?" as
// knowledge — it matched the word, not the act. That failure is worse than a
// mislabel: a recall stores nothing and is answered, so the question was
// captured as a unit instead of being answered, and the answer never came.
//
// The sibling failure the same clause covers: "move the passport renewal to
// the 20th" classified as task, because an imperative reads as new work unless
// the prompt says that moving an existing thing corrects it.
//
// This test proves the clause is in the prompt. It cannot prove the model
// obeys it — no test here reaches a provider (non-negotiable #5). Only
// `nooma doctor` against a real key measures that, and doctor's gate grades
// decode quality, not which type came back, so even that is a human reading
// the output.
func TestBuildPrompt_SeparatesAskingFromTelling(t *testing.T) {
	p := BuildPrompt("what do you know about my passport?", nil, time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC))

	for _, want := range []string{
		"what the message DOES",
		"the user ASKS",
		"the user TELLS",
		"the user ALTERS",
		"files the question away instead of answering it",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("BuildPrompt does not tell the model %q; without it a question about what Nooma knows is captured as knowledge instead of answered", want)
		}
	}
}

// TestBuildPrompt_CarriesAUsableOffsetForTheProcessZone is the production
// case the FixedZone fixtures above cannot reach.
//
// Every caller in the repo supplies an instant whose Location is time.Local
// — cmd/nooma/wiring.go's systemClock and cmd/nooma/doctor.go's quality gate
// both pass time.Now(). time.Local.String() is the literal string "Local" on
// every machine, whatever the zone actually is, because time.Local is a
// sentinel Location rather than a named one. So a prompt that renders the
// zone's NAME tells the model "User timezone: Local" and then asks it to
// answer with absolute instants — a zone label from which no offset can be
// written.
//
// The fix is not configuration: doc 02 §5.1 forbids a timezone key on
// purpose, because the instant already carries the fact. What the instant
// genuinely carries is the OFFSET, so that is what the prompt must render.
//
// The FixedZone tests pass today and would keep passing forever, because a
// test zone constructed with a real IANA name is the one shape production
// never produces.
//
// Mutation: render now.Location().String() again and this fails.
func TestBuildPrompt_CarriesAUsableOffsetForTheProcessZone(t *testing.T) {
	now := time.Date(2026, 8, 4, 9, 30, 0, 0, time.Local)

	prompt := BuildPrompt("take the bread out of the oven in 40 minutes", nil, now)

	if strings.Contains(prompt, "timezone: Local") {
		t.Error("the prompt renders the zone as \"Local\" — time.Local.String() is that " +
			"literal string on every machine, so the model is told a zone it cannot turn " +
			"into an offset")
	}
	if want := now.Format(time.RFC3339); !strings.Contains(prompt, want) {
		t.Errorf("the prompt does not carry the instant %q — the offset is the half of the "+
			"zone the model can actually use, and now.Format already has it", want)
	}
}

// TestBuildPrompt_ShowsADateFormatTheDecoderAccepts closes the gap between
// what the prompt asks for and what Decode takes.
//
// assignTime accepts exactly time.RFC3339 or "2006-01-02" and nothing else,
// and Go's RFC3339 parser REQUIRES an offset or a Z. The prompt named the
// standard without ever showing one, so a model answering
// "2026-08-04T15:00:00" has followed the prompt as written and is still
// degraded as bad_format — which is two of doctor's three live failures.
//
// The assertion is that some literal in the prompt actually parses, rather
// than that the prompt contains particular bytes: the point is that an
// example EXISTS and is valid, not that it is worded one way.
//
// Mutation: drop the example from the event_at/due_at line and this fails.
func TestBuildPrompt_ShowsADateFormatTheDecoderAccepts(t *testing.T) {
	prompt := BuildPrompt("take the bread out of the oven in 40 minutes", nil,
		time.Date(2026, 8, 4, 9, 30, 0, 0, time.UTC))

	line := ""
	for _, l := range strings.Split(prompt, "\n") {
		if strings.Contains(l, "event_at, due_at") || strings.Contains(l, "e.g.") {
			line += l + "\n"
		}
	}
	if line == "" {
		t.Fatal("no event_at/due_at line in the prompt at all")
	}

	timestamps := regexp.MustCompile(`\d{4}-\d{2}-\d{2}T[0-9:]+(?:Z|[+-]\d{2}:\d{2})`).FindAllString(line, -1)
	if len(timestamps) == 0 {
		t.Fatalf("the date-format line shows no timestamp example, so \"a full RFC3339 "+
			"timestamp\" is a standard's name and nothing more:\n%s", line)
	}
	for _, ts := range timestamps {
		if _, err := time.Parse(time.RFC3339, ts); err != nil {
			t.Errorf("the prompt offers %q as an example, which the decoder itself rejects: %v", ts, err)
		}
	}
}

// TestBuildPrompt_SaysTheOffsetIsMandatory: an example alone can be read as
// one of several accepted shapes. The decoder accepts no zone-less
// timestamp at all, so the prompt has to say so rather than leave it to be
// inferred from a sample.
func TestBuildPrompt_SaysTheOffsetIsMandatory(t *testing.T) {
	prompt := BuildPrompt("anything", nil, time.Date(2026, 8, 4, 9, 30, 0, 0, time.UTC))

	if !strings.Contains(prompt, "offset") {
		t.Error("the prompt never mentions the offset, which Go's RFC3339 parser requires " +
			"and which a model has no other way to know is mandatory")
	}
}
