package relation

import "testing"

func judgment(outcome Outcome) Judgment {
	o, target, typ, strength, confidence := outcome, "u-1", "same_topic", 0.6, 0.7
	return Judgment{Outcome: &o, TargetUnitID: &target, Type: &typ, Strength: &strength, Confidence: &confidence}
}

var offeredOne = []string{"u-1"}

// TestPropose_ADecidedJudgmentProposesItsRelation: a related judgment with
// every field proposes exactly what it said.
func TestPropose_ADecidedJudgmentProposesItsRelation(t *testing.T) {
	p, r := Propose(judgment(OutcomeRelated), offeredOne)
	if r != Proposed {
		t.Fatalf("reading = %v, want Proposed", r)
	}
	want := Proposal{Outcome: OutcomeRelated, TargetUnitID: "u-1", Type: "same_topic", Strength: 0.6, Confidence: 0.7}
	if p != want {
		t.Errorf("proposal = %+v, want %+v", p, want)
	}
}

// TestPropose_ADuplicateIsTypedDuplicate: doc 02 §4, a duplicate is recorded
// as a relation. The judge is told to omit the type for one, so a missing type
// does not refuse it, and a type it gave anyway is not what it is stored as.
func TestPropose_ADuplicateIsTypedDuplicate(t *testing.T) {
	j := judgment(OutcomeDuplicate)
	for name, typ := range map[string]*string{"omitted": nil, "given anyway": j.Type} {
		t.Run(name, func(t *testing.T) {
			j.Type = typ
			p, r := Propose(j, offeredOne)
			if r != Proposed || p.Type != TypeDuplicate {
				t.Errorf("Propose = %+v, %v; want a proposal typed %q", p, r, TypeDuplicate)
			}
		})
	}
}

// TestPropose_AMissingStrengthIsZero: strength is not among the fields whose
// absence means the judge decided nothing (doc 02 §4); it reads as 0.
func TestPropose_AMissingStrengthIsZero(t *testing.T) {
	j := judgment(OutcomeRelated)
	j.Strength = nil
	if p, r := Propose(j, offeredOne); r != Proposed || p.Strength != 0 {
		t.Errorf("Propose = %+v, %v; want a proposal with strength 0", p, r)
	}
}

// TestPropose_AJudgmentThatDecidedNothingProposesNothing: doc 02 §4, "if it
// degrades so far that the outcome, its confidence or its target is missing,
// no relation is stored"; nor for outcome new, nor for a related one with no
// type to store it under.
func TestPropose_AJudgmentThatDecidedNothingProposesNothing(t *testing.T) {
	for name, mutate := range map[string]func(*Judgment){
		"outcome new":          func(j *Judgment) { o := OutcomeNew; j.Outcome = &o },
		"no outcome":           func(j *Judgment) { j.Outcome = nil },
		"no confidence":        func(j *Judgment) { j.Confidence = nil },
		"no target":            func(j *Judgment) { j.TargetUnitID = nil },
		"related with no type": func(j *Judgment) { j.Type = nil },
		"related with an empty type": func(j *Judgment) {
			empty := ""
			j.Type = &empty
		},
	} {
		t.Run(name, func(t *testing.T) {
			j := judgment(OutcomeRelated)
			mutate(&j)
			if _, r := Propose(j, offeredOne); r != DecidedNothing {
				t.Errorf("reading = %v, want DecidedNothing", r)
			}
		})
	}
}

// TestPropose_ATargetNotOfferedIsTargetUnknown: ADR-0026, apart from deciding
// nothing, because the caller records it.
func TestPropose_ATargetNotOfferedIsTargetUnknown(t *testing.T) {
	for _, outcome := range []Outcome{OutcomeRelated, OutcomeDuplicate} {
		for _, offered := range [][]string{{"u-2"}, nil} {
			if _, r := Propose(judgment(outcome), offered); r != TargetUnknown {
				t.Errorf("%s naming u-1 with %v offered: reading = %v, want TargetUnknown", outcome, offered, r)
			}
		}
	}
}

// TestReading_TheThreeAreDistinct: a caller switches on the Reading, and two
// that compared equal would send a refusal down the proposal's path.
func TestReading_TheThreeAreDistinct(t *testing.T) {
	seen := map[Reading]bool{}
	for _, r := range []Reading{DecidedNothing, TargetUnknown, Proposed} {
		if seen[r] {
			t.Errorf("reading %v repeats", r)
		}
		seen[r] = true
	}
	if DecidedNothing != Reading(0) {
		t.Error("the zero Reading must be DecidedNothing, so an unset one stores nothing")
	}
}
