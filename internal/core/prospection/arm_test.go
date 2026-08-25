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

	t.Run("every classify rule converts to its own prospection rule", func(t *testing.T) {
		// **A sweep over classify.AllRecurrenceRules(), not a hand-written
		// table.** The previous version of this subtest listed the two
		// members by hand and its own comment said "the conversion has two
		// members" — a sentence that stops being true the moment the
		// vocabulary grows, and a table that cannot notice when it does.
		//
		// recurrenceRule was an `if r == Monthly { … }; return RuleYearly`,
		// total only by the accident of the set having exactly two members.
		// A third would have collapsed into RuleYearly with no error, no
		// degradation and no decision_log row: a weekly reminder armed once
		// a year. The defect was not waiting to appear on its own — it was
		// waiting for someone to extend the vocabulary, which is the very
		// next thing this repository intends to do.
		//
		// The assertion compares the two vocabularies by their string
		// values, which is exact rather than approximate: both spell the
		// same words, so a member that converts to a rule spelling
		// something else has been mapped wrong, not merely mapped.
		//
		// Mutation: add a member to classify.AllRecurrenceRules() without
		// adding a case to recurrenceRule, and this fails.
		event := at(time.September, 4)

		rules := classify.AllRecurrenceRules()
		if len(rules) == 0 {
			t.Fatal("classify.AllRecurrenceRules() is empty — this sweep proves nothing")
		}

		for _, from := range rules {
			c := classify.Classification{
				Kind:           kind(classify.KindRecurringReminder),
				EventAt:        armPtr(event),
				RecurrenceRule: armPtr(from),
			}

			plan, ok := Arm(c, now)
			if !ok || plan.What != ArmRecurring {
				t.Fatalf("%q: Arm = (%+v, %v), want an ArmRecurring plan", from, plan, ok)
			}
			if string(plan.Rule) != string(from) {
				t.Errorf("classify rule %q became prospection rule %q — a member with no case "+
					"of its own falls through to the default and is armed as the wrong "+
					"recurrence entirely", from, plan.Rule)
			}

			// Carried over from the hand-written table this sweep replaced.
			// For the monthly rule the clamp is the ordinary case rather
			// than the exception: the next occurrence of a given day is at
			// most a month out, so a seven-day horizon in front of it
			// frequently lands before now. A monthly reminder therefore
			// arms at now more often than not, which is the correct reading
			// of "the system is not late for something it just learned".
			wantFire := clampToNow(LeadTime(NextOccurrence(plan.Rule, plan.Anchor, now)), now)
			if !plan.FireAt.Equal(wantFire) {
				t.Errorf("%q: FireAt = %v, want %v", from, plan.FireAt, wantFire)
			}
			if plan.FireAt.Before(now) {
				t.Errorf("%q: FireAt = %v is before now", from, plan.FireAt)
			}
		}
	})

	t.Run("the two vocabularies have the same members", func(t *testing.T) {
		// The sweep above proves every classify member maps somewhere
		// correct. This proves prospection declares no rule classify cannot
		// produce, and vice versa — the two halves of one vocabulary that
		// Finding F3 split across a package boundary, checked in both
		// directions so neither side can grow alone.
		//
		// Mutation: add a member to either AllX() alone and this fails.
		fromClassify := map[string]bool{}
		for _, r := range classify.AllRecurrenceRules() {
			fromClassify[string(r)] = true
		}
		fromProspection := map[string]bool{}
		for _, r := range AllRules() {
			fromProspection[string(r)] = true
		}

		for m := range fromClassify {
			if !fromProspection[m] {
				t.Errorf("classify declares %q and prospection does not — Arm would convert it "+
					"to something else", m)
			}
		}
		for m := range fromProspection {
			if !fromClassify[m] {
				t.Errorf("prospection declares %q and classify cannot produce it", m)
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

	t.Run("nothing is armed, and each refusal says which one it was", func(t *testing.T) {
		// Spec R6.1 requires the causes to be distinguishable, not merely
		// for all of them to arm nothing: m3b writes decision_log from this
		// plan, so a refusal that cannot say why is a hole in the glass box.
		// An undated event is a decoding failure worth surfacing; a chitchat
		// capture arming nothing is the system working correctly. Collapsing
		// the two makes the first invisible.
		cases := map[string]struct {
			c       classify.Classification
			wantWhy Refusal
		}{
			"a kind that arms nothing": {
				c:       classify.Classification{Kind: kind(classify.KindTask), DueAt: armPtr(at(time.June, 20))},
				wantWhy: RefusalKindNotArming,
			},
			"an undated event": {
				c:       classify.Classification{Kind: kind(classify.KindEvent)},
				wantWhy: RefusalNoDate,
			},
			"an event already over": {
				c:       classify.Classification{Kind: kind(classify.KindEvent), EventAt: armPtr(at(time.May, 1))},
				wantWhy: RefusalAlreadyPast,
			},
			"a timer already due": {
				c:       classify.Classification{Kind: kind(classify.KindTimer), DueAt: armPtr(at(time.May, 1))},
				wantWhy: RefusalAlreadyPast,
			},
			"a timer with no due_at": {
				c:       classify.Classification{Kind: kind(classify.KindTimer)},
				wantWhy: RefusalNoDate,
			},
			"a recurring reminder with no date": {
				c: classify.Classification{
					Kind:           kind(classify.KindRecurringReminder),
					RecurrenceRule: armPtr(classify.RecurrenceRuleMonthly),
				},
				wantWhy: RefusalNoDate,
			},
			"no kind at all": {
				c:       classify.Classification{},
				wantWhy: RefusalNoKind,
			},
		}

		seen := map[Refusal]int{}
		for name, tc := range cases {
			t.Run(name, func(t *testing.T) {
				plan, ok := Arm(tc.c, now)
				if ok {
					t.Errorf("Arm = (%+v, true), want false", plan)
				}
				if plan.What != ArmNothing {
					t.Errorf("What = %q, want %q", plan.What, ArmNothing)
				}
				if plan.Why != tc.wantWhy {
					t.Errorf("Why = %q, want %q — different causes must be auditable apart, "+
						"since m3b's decision_log write consumes this field", plan.Why, tc.wantWhy)
				}
			})
			seen[tc.wantWhy]++
		}

		// The property behind the table: the refusals are genuinely several
		// values. A Why that always returned one constant would satisfy any
		// single row above.
		if len(seen) < 4 {
			t.Errorf("the fixtures cover only %d distinct refusals (%v); the point of the field "+
				"is that causes differ", len(seen), seen)
		}
	})

	t.Run("an armed plan carries no refusal", func(t *testing.T) {
		plan, ok := Arm(classify.Classification{
			Kind: kind(classify.KindEvent), EventAt: armPtr(at(time.June, 20)),
		}, now)
		if !ok {
			t.Fatal("Arm returned false for a dated event")
		}
		if plan.Why != RefusalNone {
			t.Errorf("Why = %q on an armed plan, want %q — the field answers why NOT, and a "+
				"plan that armed something has no answer to give", plan.Why, RefusalNone)
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

// TestArm_RecurringIgnoresHowOldItsAnchorIs resolves Finding F9: spec R6.1
// says "a dated event or recurring_reminder whose instant is at or before
// now arms nothing", while design §3.7's table applies that refusal only to
// the one-shot rows. They disagree, and the design is right.
//
// A birthday's event_at is the birth date. It is ALWAYS in the past, by
// decades. Applying the past-instant refusal to a rule-bearing recurring
// reminder would make recurring reminders unrepresentable — the feature
// would refuse every input it exists to serve.
//
// The refusal governs a one-shot instant, which is a thing that happens
// once and can be over. A recurrence's event_at is not that: it is the
// ANCHOR, a month and a day the occurrence is re-derived from, and its year
// is discarded.
func TestArm_RecurringIgnoresHowOldItsAnchorIs(t *testing.T) {
	loc := time.FixedZone("UTC+2", 2*60*60)
	now := time.Date(2027, time.June, 1, 9, 0, 0, 0, loc)
	kind := func(k classify.Kind) *classify.Kind { return &k }

	born := time.Date(1985, time.September, 4, 6, 15, 0, 0, loc)
	plan, ok := Arm(classify.Classification{
		Kind:           kind(classify.KindRecurringReminder),
		EventAt:        &born,
		RecurrenceRule: armPtr(classify.RecurrenceRuleYearly),
	}, now)

	if !ok || plan.What != ArmRecurring {
		t.Fatalf("Arm = (%+v, %v), want an ArmRecurring plan — a birthday's event_at is the "+
			"birth date and is always decades past; refusing it would make the feature "+
			"refuse every input it exists for", plan, ok)
	}
	if plan.Anchor.Month != time.September || plan.Anchor.Day != 4 {
		t.Errorf("Anchor = %+v, want {September, 4} — the year is discarded, the month and day "+
			"are the whole point", plan.Anchor)
	}
	if !plan.FireAt.After(now) {
		t.Errorf("FireAt = %v, want a future instant", plan.FireAt)
	}

	// The contrast, in the same test so the boundary is visible: without a
	// rule, the same classification IS a one-shot occurrence, and a one-shot
	// occurrence decades past arms nothing.
	oneShot, ok := Arm(classify.Classification{
		Kind:    kind(classify.KindRecurringReminder),
		EventAt: &born,
	}, now)
	if ok || oneShot.Why != RefusalAlreadyPast {
		t.Errorf("without a rule the same input gave (%+v, %v), want a refusal with %q — a "+
			"date that is an anchor when a rule accompanies it is a spent instant when one "+
			"does not", oneShot, ok, RefusalAlreadyPast)
	}
}

// TestArm_AnchorIsTheDateAsStated pins which frame the anchor's month and
// day are read in, because two are in play: classify may decode an RFC3339
// event_at carrying its own offset, while NextOccurrence builds every
// occurrence in now's location.
//
// The anchor is read in the event's OWN zone, which is the date the user
// stated. An anniversary is a calendar date, not an instant: "4 September"
// means 4 September wherever the person later happens to be, and the
// occurrence is then materialised in the zone they are in now.
func TestArm_AnchorIsTheDateAsStated(t *testing.T) {
	nowLoc := time.FixedZone("UTC+2", 2*60*60)
	now := time.Date(2027, time.June, 1, 9, 0, 0, 0, nowLoc)
	kind := func(k classify.Kind) *classify.Kind { return &k }

	// Late on 4 September in Hawaii is already 5 September in the user's
	// current zone. The anniversary is the date as stated: the 4th.
	stated := time.Date(1985, time.September, 4, 23, 30, 0, 0, time.FixedZone("UTC-10", -10*60*60))
	if stated.In(nowLoc).Day() != 5 {
		t.Fatalf("fixture is broken: the instant should read as the 5th in now's zone, got %d",
			stated.In(nowLoc).Day())
	}

	plan, ok := Arm(classify.Classification{
		Kind:           kind(classify.KindRecurringReminder),
		EventAt:        &stated,
		RecurrenceRule: armPtr(classify.RecurrenceRuleYearly),
	}, now)
	if !ok {
		t.Fatal("Arm returned false")
	}
	if plan.Anchor.Day != 4 {
		t.Errorf("Anchor.Day = %d, want 4 — the anniversary is the date the user stated, read "+
			"in the zone they stated it in, not the date that instant falls on somewhere else",
			plan.Anchor.Day)
	}
}
