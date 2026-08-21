package prospection

import (
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
)

// TestLeadTime proves spec R5.2's arithmetic and its explicit refusal to
// clamp at this layer.
func TestLeadTime(t *testing.T) {
	loc := time.FixedZone("UTC+2", 2*60*60)
	at := func(day int) time.Time { return time.Date(2027, time.June, day, 9, 30, 0, 0, loc) }

	t.Run("an event nine days out fires two days out", func(t *testing.T) {
		if got, want := LeadTime(at(10)), at(3); !got.Equal(want) {
			t.Errorf("LeadTime(%v) = %v, want %v", at(10), got, want)
		}
	})

	t.Run("the wall clock and zone are carried, not reset", func(t *testing.T) {
		got := LeadTime(at(10))
		if got.Hour() != 9 || got.Minute() != 30 {
			t.Errorf("LeadTime = %v, want the event's own wall clock (09:30) — the horizon "+
				"shifts the date, not the time of day", got)
		}
		if got.Location().String() != loc.String() {
			t.Errorf("LeadTime = %v, want the event's own zone", got)
		}
	})

	t.Run("an event closer than the horizon returns a past instant, unclamped", func(t *testing.T) {
		// Spec R5.2 is explicit that this layer does not clamp. The clamp is
		// Arm's, because "do not arm in the past" is a fact about arming and
		// this function answers a fact about the event.
		eventAt := at(3)
		got := LeadTime(eventAt)
		if !got.Before(eventAt) {
			t.Fatalf("LeadTime(%v) = %v, want an earlier instant", eventAt, got)
		}
		if want := time.Date(2027, time.May, 27, 9, 30, 0, 0, loc); !got.Equal(want) {
			t.Errorf("LeadTime = %v, want %v — the horizon crosses the month boundary rather "+
				"than stopping at it", got, want)
		}
	})
}

func armPtr[T any](v T) *T { return &v }

// TestArm walks design §3.7's five-row table, one subtest per row, plus the
// three decisions the table encodes rather than states.
func TestArm(t *testing.T) {
	loc := time.FixedZone("UTC+2", 2*60*60)
	now := time.Date(2027, time.June, 1, 9, 0, 0, 0, loc)
	at := func(month time.Month, day int) time.Time {
		return time.Date(2027, month, day, 18, 0, 0, 0, loc)
	}
	kind := func(k classify.Kind) *classify.Kind { return &k }

	t.Run("a timer arms at its own due_at, never its event_at", func(t *testing.T) {
		due := at(time.June, 1).Add(-8 * time.Hour) // 10:00, one hour out
		c := classify.Classification{
			Kind:  kind(classify.KindTimer),
			DueAt: armPtr(due),
			// Deliberately populated and deliberately wrong to read: I18
			// forbids interchanging the timestamps, so a timer that read
			// event_at would land here instead.
			EventAt: armPtr(at(time.December, 25)),
		}

		plan, ok := Arm(c, now)
		if !ok || plan.What != ArmTimer {
			t.Fatalf("Arm = (%+v, %v), want an ArmTimer plan", plan, ok)
		}
		if !plan.FireAt.Equal(due) {
			t.Errorf("FireAt = %v, want the timer's own due_at (%v) — reading event_at here "+
				"would fire in December", plan.FireAt, due)
		}
		if plan.LeadDays != 0 {
			t.Errorf("LeadDays = %d, want 0 — a timer fires at the instant the user named, "+
				"with no horizon in front of it", plan.LeadDays)
		}
	})

	t.Run("a dated event arms a trigger at the lead-time horizon", func(t *testing.T) {
		event := at(time.June, 20)
		c := classify.Classification{Kind: kind(classify.KindEvent), EventAt: armPtr(event)}

		plan, ok := Arm(c, now)
		if !ok || plan.What != ArmTrigger {
			t.Fatalf("Arm = (%+v, %v), want an ArmTrigger plan", plan, ok)
		}
		if want := LeadTime(event); !plan.FireAt.Equal(want) {
			t.Errorf("FireAt = %v, want %v", plan.FireAt, want)
		}
		if plan.LeadDays != EventLeadDays {
			t.Errorf("LeadDays = %d, want %d", plan.LeadDays, EventLeadDays)
		}
	})

	t.Run("an event closer than the horizon arms at now, not in the past", func(t *testing.T) {
		// Two days out: the horizon is five days behind us. Arming there
		// would be expired by the staleness gate on the very next pass, and
		// the system is not late for an event it only just learned about.
		event := at(time.June, 3)
		c := classify.Classification{Kind: kind(classify.KindEvent), EventAt: armPtr(event)}

		plan, ok := Arm(c, now)
		if !ok || plan.What != ArmTrigger {
			t.Fatalf("Arm = (%+v, %v), want an ArmTrigger plan", plan, ok)
		}
		if plan.FireAt.Before(now) {
			t.Errorf("FireAt = %v, which is before now (%v) — the lead time is a horizon, and "+
				"arming behind it hands the staleness gate something born expired",
				plan.FireAt, now)
		}
		if !plan.FireAt.Equal(now) {
			t.Errorf("FireAt = %v, want exactly now (%v) — clamped, not offset", plan.FireAt, now)
		}
	})

	t.Run("a recurring reminder arms from its own anchor", func(t *testing.T) {
		event := at(time.September, 4)
		c := classify.Classification{
			Kind:           kind(classify.KindRecurringReminder),
			EventAt:        armPtr(event),
			RecurrenceRule: armPtr(classify.RecurrenceRuleYearly),
		}

		plan, ok := Arm(c, now)
		if !ok || plan.What != ArmRecurring {
			t.Fatalf("Arm = (%+v, %v), want an ArmRecurring plan", plan, ok)
		}
		if plan.Rule != RuleYearly {
			t.Errorf("Rule = %q, want %q — the classify-side vocabulary is converted here, at "+
				"the only call site on the legal side of the import edge (Finding F3)",
				plan.Rule, RuleYearly)
		}
		if plan.Anchor.Month != time.September || plan.Anchor.Day != 4 {
			t.Errorf("Anchor = %+v, want {September, 4} — the anchor is the event's own month "+
				"and day, not the instant it was captured", plan.Anchor)
		}
		if want := LeadTime(NextOccurrence(RuleYearly, plan.Anchor, now)); !plan.FireAt.Equal(want) {
			t.Errorf("FireAt = %v, want %v", plan.FireAt, want)
		}
	})

	t.Run("both recurrence rules convert, not just the default one", func(t *testing.T) {
		// The conversion Finding F3 left to this call site has two members,
		// and a switch that returned RuleYearly for both would pass every
		// other test in this file. Caught by mutation before review, and
		// pinned here so it stays caught.
		event := at(time.September, 4)

		for _, tc := range []struct {
			from classify.RecurrenceRule
			want Rule
		}{
			{classify.RecurrenceRuleYearly, RuleYearly},
			{classify.RecurrenceRuleMonthly, RuleMonthly},
		} {
			c := classify.Classification{
				Kind:           kind(classify.KindRecurringReminder),
				EventAt:        armPtr(event),
				RecurrenceRule: armPtr(tc.from),
			}

			plan, ok := Arm(c, now)
			if !ok || plan.What != ArmRecurring {
				t.Fatalf("%q: Arm = (%+v, %v), want an ArmRecurring plan", tc.from, plan, ok)
			}
			if plan.Rule != tc.want {
				t.Errorf("classify rule %q became %q, want %q", tc.from, plan.Rule, tc.want)
			}
			// Clamped, and for the monthly rule that clamp is the ordinary
			// case rather than the exception: the next occurrence of a given
			// day is at most a month out, so a seven-day horizon in front of
			// it frequently lands before now. A monthly reminder therefore
			// arms at now more often than not, which is the correct reading
			// of "the system is not late for something it just learned".
			want := clampToNow(LeadTime(NextOccurrence(tc.want, plan.Anchor, now)), now)
			if !plan.FireAt.Equal(want) {
				t.Errorf("%q: FireAt = %v, want %v — a monthly reminder must not be scheduled "+
					"a year out", tc.from, plan.FireAt, want)
			}
			if plan.FireAt.Before(now) {
				t.Errorf("%q: FireAt = %v is before now", tc.from, plan.FireAt)
			}
		}
	})

	t.Run("a recurring reminder with a degraded rule arms the one-shot occurrence", func(t *testing.T) {
		event := at(time.September, 4)
		c := classify.Classification{
			Kind:    kind(classify.KindRecurringReminder),
			EventAt: armPtr(event),
			// nil: classify degraded it, or the model never claimed one.
			RecurrenceRule: nil,
		}

		plan, ok := Arm(c, now)
		if !ok || plan.What != ArmTrigger {
			t.Fatalf("Arm = (%+v, %v), want an ArmTrigger plan — the capture is honoured and "+
				"the recurrence is not invented", plan, ok)
		}
		if plan.Rule != "" || plan.Anchor != (Anchor{}) {
			t.Errorf("plan carries Rule %q and Anchor %+v, want neither — a one-shot trigger "+
				"has no recurrence to remember", plan.Rule, plan.Anchor)
		}
	})

	t.Run("nothing is armed, and the two reasons are distinguishable", func(t *testing.T) {
		cases := map[string]classify.Classification{
			"a kind that arms nothing": {
				Kind: kind(classify.KindTask), DueAt: armPtr(at(time.June, 20)),
			},
			"an undated event": {
				Kind: kind(classify.KindEvent),
			},
			"an event already over": {
				Kind: kind(classify.KindEvent), EventAt: armPtr(at(time.May, 1)),
			},
			"a timer already due": {
				Kind: kind(classify.KindTimer), DueAt: armPtr(at(time.May, 1)),
			},
			"a recurring reminder with no date": {
				Kind:           kind(classify.KindRecurringReminder),
				RecurrenceRule: armPtr(classify.RecurrenceRuleMonthly),
			},
			"no kind at all": {},
		}

		for name, c := range cases {
			t.Run(name, func(t *testing.T) {
				plan, ok := Arm(c, now)
				if ok {
					t.Errorf("Arm = (%+v, true), want false", plan)
				}
				if plan.What != ArmNothing {
					t.Errorf("What = %q, want %q", plan.What, ArmNothing)
				}
			})
		}
	})

	t.Run("the interrupt is always resolved, never left unset", func(t *testing.T) {
		// A zero Interrupt reports itself degraded, which is safe, but the
		// plan must carry a resolution rather than a zero value that merely
		// resembles one — otherwise a claimed 0.0 and an absent reading
		// become indistinguishable at the arming layer, which is the whole
		// thing ResolveInterrupt exists to keep apart.
		event := at(time.June, 20)

		claimed := classify.Classification{
			Kind: kind(classify.KindEvent), EventAt: armPtr(event),
			InterruptLevel: armPtr(0.0),
		}
		plan, _ := Arm(claimed, now)
		if plan.Interrupt.Degraded() {
			t.Errorf("a claimed 0.0 produced a degraded Interrupt — the plan must carry the " +
				"model's own claim, not a zero value")
		}

		absent := classify.Classification{Kind: kind(classify.KindEvent), EventAt: armPtr(event)}
		plan, _ = Arm(absent, now)
		if !plan.Interrupt.Degraded() {
			t.Errorf("an absent reading produced a non-degraded Interrupt")
		}
		if plan.Interrupt.Route() != RouteDigest {
			t.Errorf("Route = %q, want %q — a degraded reading can never reach push",
				plan.Interrupt.Route(), RouteDigest)
		}

		urgent := classify.Classification{
			Kind: kind(classify.KindEvent), EventAt: armPtr(event),
			InterruptLevel: armPtr(0.95),
		}
		plan, _ = Arm(urgent, now)
		if plan.Interrupt.Route() != RoutePush {
			t.Errorf("Route = %q, want %q for a claimed 0.95", plan.Interrupt.Route(), RoutePush)
		}
	})
}

// TestArm_ReadsOneTimestampPerKind is I18 as a property of Arm's own body
// rather than as a set of examples: the three timestamps are never
// interchanged, and each Kind reads exactly the one that belongs to it.
//
// The fixture makes every timestamp distinct and every wrong answer
// obvious. Reading CreatedAt would arm in the past; reading the wrong one
// of EventAt/DueAt would arm six months out. A test that only checked the
// right answer would pass on an implementation that happened to agree by
// coincidence on one fixture — this one names what each wrong read looks
// like.
//
// Disclosed per m2a C9: Arm already compiles and passes by this point, so
// there is no missing-symbol red available.
func TestArm_ReadsOneTimestampPerKind(t *testing.T) {
	loc := time.FixedZone("UTC+2", 2*60*60)
	now := time.Date(2027, time.June, 1, 9, 0, 0, 0, loc)

	created := time.Date(2020, time.January, 1, 0, 0, 0, 0, loc) // long past
	event := time.Date(2027, time.July, 10, 18, 0, 0, 0, loc)    // ~6 weeks out
	due := time.Date(2027, time.June, 1, 17, 0, 0, 0, loc)       // 8 hours out
	kind := func(k classify.Kind) *classify.Kind { return &k }

	t.Run("a timer reads due_at only", func(t *testing.T) {
		plan, ok := Arm(classify.Classification{
			Kind: kind(classify.KindTimer), DueAt: &due, EventAt: &event,
		}, now)
		if !ok {
			t.Fatal("Arm returned false for a due timer")
		}
		switch {
		case plan.FireAt.Equal(event):
			t.Errorf("FireAt = %v, the EVENT instant — a timer that reads event_at fires six "+
				"weeks late", plan.FireAt)
		case plan.FireAt.Equal(created):
			t.Errorf("FireAt = %v, the CREATED instant", plan.FireAt)
		case !plan.FireAt.Equal(due):
			t.Errorf("FireAt = %v, want the due instant %v", plan.FireAt, due)
		}
	})

	t.Run("an event reads event_at only", func(t *testing.T) {
		plan, ok := Arm(classify.Classification{
			Kind: kind(classify.KindEvent), EventAt: &event, DueAt: &due,
		}, now)
		if !ok {
			t.Fatal("Arm returned false for a dated event")
		}
		if plan.FireAt.Equal(due) || plan.FireAt.Equal(LeadTime(due)) {
			t.Errorf("FireAt = %v, derived from the DUE instant — an event that reads due_at "+
				"arms against a timestamp that means something else", plan.FireAt)
		}
		if want := LeadTime(event); !plan.FireAt.Equal(want) {
			t.Errorf("FireAt = %v, want %v", plan.FireAt, want)
		}
	})

	t.Run("no Kind arms from a value Arm was never given", func(t *testing.T) {
		// Arm takes no created_at at all — Classification has no such field,
		// so two thirds of I18 are unrepresentable here rather than merely
		// untested. Swept across the whole vocabulary so a Kind added later
		// cannot quietly reach for one.
		for _, k := range classify.AllKinds() {
			c := classify.Classification{Kind: kind(k), EventAt: &event, DueAt: &due}
			plan, ok := Arm(c, now)
			if !ok {
				continue
			}
			if plan.FireAt.Before(now) {
				t.Errorf("Kind %q armed at %v, before now — no timestamp Arm reads can be in "+
					"the past after clamping", k, plan.FireAt)
			}
			if plan.FireAt.Year() == created.Year() {
				t.Errorf("Kind %q armed in %d, the created year", k, plan.FireAt.Year())
			}
		}
	})
}

// TestPlanAndArmamentVocabulary pins the two exported types Arm hands back,
// and one distinction a caller could otherwise get wrong.
//
// A zero Plan carries What == "", which is NOT ArmNothing. That matters
// because the two mean different things: ArmNothing is a decision Arm made
// and stands behind, while "" is a Plan nobody produced. A caller writing
// `if plan.What != ArmNothing { arm(plan) }` would arm a zero Plan — so
// this asserts that every value Arm returns, on every path including its
// refusals, carries a named Armament.
func TestPlanAndArmamentVocabulary(t *testing.T) {
	loc := time.FixedZone("UTC+2", 2*60*60)
	now := time.Date(2027, time.June, 1, 9, 0, 0, 0, loc)
	kind := func(k classify.Kind) *classify.Kind { return &k }

	named := map[Armament]bool{ArmNothing: true, ArmTimer: true, ArmTrigger: true, ArmRecurring: true}

	var zero Plan
	if named[zero.What] {
		t.Errorf("a zero Plan reports What = %q, a named Armament — the zero value must be "+
			"distinguishable from a decision Arm actually made", zero.What)
	}

	// Every Kind, armed or refused, through the real function.
	for _, k := range classify.AllKinds() {
		for _, c := range []classify.Classification{
			{Kind: kind(k)},
			{Kind: kind(k), EventAt: &now, DueAt: &now},
			{Kind: kind(k), EventAt: armPtr(now.AddDate(0, 2, 0)), DueAt: armPtr(now.Add(time.Hour))},
		} {
			plan, _ := Arm(c, now)
			if !named[plan.What] {
				t.Errorf("Kind %q produced What = %q, which is not one of the four named "+
					"Armament values — every path out of Arm is a decision, including a refusal",
					k, plan.What)
			}
		}
	}

	// And with no Kind at all, which is its own path.
	if plan, _ := Arm(classify.Classification{}, now); plan.What != ArmNothing {
		t.Errorf("a classification with no Kind produced What = %q, want %q", plan.What, ArmNothing)
	}
}
