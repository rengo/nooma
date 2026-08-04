package classify

import (
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
