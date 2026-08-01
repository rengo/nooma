package classify

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/unit"
)

// The three instants I18 is about, deliberately distinguishable: no two are
// equal, and none is the zero time. A test using one instant for all three
// would pass against an implementation that assigned event_at to created_at
// — which is precisely the confusion I18 names.
var (
	ingestedAt = time.Date(2026, 8, 1, 10, 0, 0, 0, buenosAires)
	happensAt  = time.Date(2026, 8, 4, 18, 30, 0, 0, buenosAires)
	dueBy      = time.Date(2026, 8, 9, 23, 59, 0, 0, buenosAires)
)

// wholeClassification is a fully-populated Classification. Tests below shave
// exactly one field off it, so "everything else is right" is measured against
// a known baseline.
func wholeClassification() Classification {
	kind := KindTask
	content := "buy milk"
	weight := 0.75
	decay := 0.03
	event := happensAt
	due := dueBy
	return Classification{
		Kind:              &kind,
		NormalizedContent: &content,
		StructuredData:    json.RawMessage(`{"store":"corner"}`),
		Weight:            &weight,
		DecayRate:         &decay,
		EventAt:           &event,
		DueAt:             &due,
	}
}

func defaultPriors() Priors {
	return Priors{Weight: PriorWeight, DecayRate: PriorDecayRate}
}

// TestToUnit_ThreeTimestampsAreNeverCrossed is I18, and the reason ToUnit
// exists as its own function: it is the only place created_at, event_at and
// due_at meet.
//
// The wire shape makes two thirds of I18 unrepresentable by construction —
// event_at and due_at are separately-named keys, and created_at is not a key
// the provider is ever asked for, so Classification has no CreatedAt field at
// all. What remains representable is this: ToUnit could still assign the
// wrong one to the wrong column. Three distinguishable instants is what
// catches it.
func TestToUnit_ThreeTimestampsAreNeverCrossed(t *testing.T) {
	u, err := ToUnit(wholeClassification(), "unit-1", "chat", ingestedAt, defaultPriors())
	if err != nil {
		t.Fatalf("ToUnit error = %v, want nil", err)
	}

	// Ingestion time, three times over — all three are "when Nooma saw it".
	for name, got := range map[string]time.Time{
		"CreatedAt":     u.CreatedAt,
		"UpdatedAt":     u.UpdatedAt,
		"LastTouchedAt": u.LastTouchedAt,
	} {
		if !got.Equal(ingestedAt) {
			t.Errorf("%s = %v, want the ingestion instant %v", name, got, ingestedAt)
		}
		if got.Equal(happensAt) || got.Equal(dueBy) {
			t.Errorf("%s = %v — that is a date the model supplied, not the ingestion "+
				"instant (I18)", name, got)
		}
	}

	if u.EventAt == nil || !u.EventAt.Equal(happensAt) {
		t.Errorf("EventAt = %v, want %v", u.EventAt, happensAt)
	}
	if u.DueAt == nil || !u.DueAt.Equal(dueBy) {
		t.Errorf("DueAt = %v, want %v", u.DueAt, dueBy)
	}
	if u.EventAt != nil && u.DueAt != nil && u.EventAt.Equal(*u.DueAt) {
		t.Error("EventAt and DueAt are the same instant — they were crossed (I18)")
	}
}

// TestToUnit_AbsentDatesStayNil: nil is not the zero time. A unit with no
// event date must carry nil, not 0001-01-01 — the second would arm a trigger
// (§7) for a date two thousand years past, and would sort ahead of every real
// date in any query ordering by it.
func TestToUnit_AbsentDatesStayNil(t *testing.T) {
	c := wholeClassification()
	c.EventAt, c.DueAt = nil, nil

	u, err := ToUnit(c, "unit-1", "chat", ingestedAt, defaultPriors())
	if err != nil {
		t.Fatalf("ToUnit error = %v, want nil", err)
	}
	if u.EventAt != nil {
		t.Errorf("EventAt = %v, want nil — an absent date is not the zero time", *u.EventAt)
	}
	if u.DueAt != nil {
		t.Errorf("DueAt = %v, want nil", *u.DueAt)
	}
}

// TestToUnit_FixedFields covers the three values ToUnit decides rather than
// copies. Status is pool because a fresh capture is live by definition (doc
// 02 §1); Confidence is nil because Phase B writes no perception confidence
// (Q2), and nil is distinct from a legitimate 0.0.
func TestToUnit_FixedFields(t *testing.T) {
	u, err := ToUnit(wholeClassification(), "unit-1", "telegram", ingestedAt, defaultPriors())
	if err != nil {
		t.Fatalf("ToUnit error = %v, want nil", err)
	}

	if u.Status != unit.StatusPool {
		t.Errorf("Status = %q, want %q — a fresh capture is live", u.Status, unit.StatusPool)
	}
	if u.Confidence != nil {
		t.Errorf("Confidence = %v, want nil — Phase B writes none (Q2), and nil is not 0.0",
			*u.Confidence)
	}
	if u.ID != "unit-1" {
		t.Errorf("ID = %q, want %q", u.ID, "unit-1")
	}
	if u.Type != unit.TypeTask {
		t.Errorf("Type = %q, want %q", u.Type, unit.TypeTask)
	}
	if u.Content != "buy milk" {
		t.Errorf("Content = %q, want %q", u.Content, "buy milk")
	}
	if string(u.StructuredData) != `{"store":"corner"}` {
		t.Errorf("StructuredData = %s, want the payload verbatim", u.StructuredData)
	}
}

// TestToUnit_SourceComesFromTheCaller is C10.1's decision under test.
//
// units.source is NOT NULL DEFAULT 'chat', but the column default never
// fires: unitrepo.go passes the field explicitly, so a zero value persists as
// "" rather than as 'chat'. core must not name a channel of its own — it
// would be silently wrong from the UI, and silence is the problem. So the
// caller supplies it, and this test proves ToUnit does not overwrite or
// invent one.
func TestToUnit_SourceComesFromTheCaller(t *testing.T) {
	for _, source := range []string{"chat", "telegram", "ui"} {
		u, err := ToUnit(wholeClassification(), "unit-1", source, ingestedAt, defaultPriors())
		if err != nil {
			t.Fatalf("ToUnit error = %v, want nil", err)
		}
		if u.Source != source {
			t.Errorf("Source = %q, want %q — core does not name channels", u.Source, source)
		}
	}
}

// TestToUnit_PriorsFillDegradedWeightAndDecay is design D3 reaching the unit.
// A degraded weight is not a zero weight, and this is where that distinction
// stops being a type-level nicety and becomes a persisted number.
func TestToUnit_PriorsFillDegradedWeightAndDecay(t *testing.T) {
	t.Run("both degraded", func(t *testing.T) {
		c := wholeClassification()
		c.Weight, c.DecayRate = nil, nil

		u, err := ToUnit(c, "unit-1", "chat", ingestedAt, defaultPriors())
		if err != nil {
			t.Fatalf("ToUnit error = %v, want nil", err)
		}
		if u.Weight != PriorWeight {
			t.Errorf("Weight = %v, want the prior %v", u.Weight, PriorWeight)
		}
		if u.WeightDecayRate != PriorDecayRate {
			t.Errorf("WeightDecayRate = %v, want the prior %v", u.WeightDecayRate, PriorDecayRate)
		}
		if u.Weight == 0 || u.WeightDecayRate == 0 {
			t.Error("a degraded value became zero — a unit at weight 0 is indistinguishable " +
				"from one decayed to nothing, and λ=0 never decays at all")
		}
	})

	t.Run("model-supplied values win over the priors", func(t *testing.T) {
		u, err := ToUnit(wholeClassification(), "unit-1", "chat", ingestedAt, defaultPriors())
		if err != nil {
			t.Fatalf("ToUnit error = %v, want nil", err)
		}
		if u.Weight != 0.75 || u.WeightDecayRate != 0.03 {
			t.Errorf("weight/λ = %v/%v, want the model's 0.75/0.03 — the prior is a fallback, "+
				"not an override", u.Weight, u.WeightDecayRate)
		}
	})

	// A legitimate zero from the model is not a degraded value. This is the
	// whole reason the fields are pointers.
	t.Run("a model-supplied zero is not a degradation", func(t *testing.T) {
		c := wholeClassification()
		zero := 0.0
		c.Weight = &zero

		u, err := ToUnit(c, "unit-1", "chat", ingestedAt, defaultPriors())
		if err != nil {
			t.Fatalf("ToUnit error = %v, want nil", err)
		}
		if u.Weight != 0 {
			t.Errorf("Weight = %v, want 0 — the model said zero, and that is not the same "+
				"as saying nothing", u.Weight)
		}
	})
}

// TestToUnit_ErrorsOnTheTwoUnbuildableCases covers both conditions, and that
// there are exactly two (design D4 as C10.2 corrected it).
func TestToUnit_ErrorsOnTheTwoUnbuildableCases(t *testing.T) {
	t.Run("a Kind that persists no unit", func(t *testing.T) {
		// The six Kind values that map to no unit.Type — a timer is NEVER a
		// unit (doc 02 §8), and neither is chitchat or a recall request.
		for _, k := range []Kind{
			KindChitchat, KindOutOfScope, KindRecall,
			KindCorrection, KindTimer, KindRecurringReminder,
		} {
			c := wholeClassification()
			c.Kind = &k

			_, err := ToUnit(c, "unit-1", "chat", ingestedAt, defaultPriors())
			if !errors.Is(err, ErrNoUnitType) {
				t.Errorf("ToUnit with Kind %q error = %v, want ErrNoUnitType", k, err)
			}
		}
	})

	t.Run("an absent Kind", func(t *testing.T) {
		c := wholeClassification()
		c.Kind = nil

		if _, err := ToUnit(c, "unit-1", "chat", ingestedAt, defaultPriors()); !errors.Is(err, ErrNoUnitType) {
			t.Errorf("error = %v, want ErrNoUnitType — a degraded type has no unit to build", err)
		}
	})

	t.Run("absent content", func(t *testing.T) {
		c := wholeClassification()
		c.NormalizedContent = nil

		_, err := ToUnit(c, "unit-1", "chat", ingestedAt, defaultPriors())
		if !errors.Is(err, ErrNoContent) {
			t.Errorf("error = %v, want ErrNoContent — units.content is NOT NULL, and a unit "+
				"with empty content is one no recall can reach", err)
		}
	})

	// The two are distinguishable, because brain writes a different
	// decision_log rationale for each (I12).
	t.Run("the two errors are distinct", func(t *testing.T) {
		if errors.Is(ErrNoUnitType, ErrNoContent) || errors.Is(ErrNoContent, ErrNoUnitType) {
			t.Error("ErrNoUnitType and ErrNoContent are the same error — brain cannot then " +
				"say which happened")
		}
	})
}

// TestToUnit_ReturnsZeroUnitOnError: the error path yields nothing usable, so
// a caller that ignored the error cannot persist a half-built unit. Belt and
// braces against the failure the (value, error) shape is meant to prevent.
func TestToUnit_ReturnsZeroUnitOnError(t *testing.T) {
	c := wholeClassification()
	c.NormalizedContent = nil

	u, err := ToUnit(c, "unit-1", "chat", ingestedAt, defaultPriors())
	if err == nil {
		t.Fatal("expected an error")
	}

	// Field by field rather than u != unit.Unit{}: Unit carries a
	// json.RawMessage, so it is not comparable. Naming the fields is also
	// clearer about the claim — the ID was passed in and still must not
	// appear, which a whole-struct comparison would assert only by accident.
	if u.ID != "" {
		t.Errorf("ID = %q on the error path — the caller's id came back on a unit that "+
			"could not be built", u.ID)
	}
	if u.Content != "" || u.Type != "" || u.Status != "" || u.Source != "" {
		t.Errorf("got a partially-built unit alongside the error: %+v", u)
	}
	if !u.CreatedAt.IsZero() || u.Weight != 0 {
		t.Errorf("timestamps or weight were filled on the error path: %+v", u)
	}
}
