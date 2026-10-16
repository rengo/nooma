package prospection

import (
	"testing"
	"time"
	_ "time/tzdata" // the DST case needs real rules, and core tests read no file

	"github.com/rengo/nooma/internal/core/classify"
)

// askedFixture is ADR-0031's scenario clock: zone -03:00, the observed message
// sent on 2026-10-14 at 20:00.
type askedFixture struct {
	zone *time.Location
	now  time.Time
}

func newAsked() askedFixture {
	zone := time.FixedZone("ART", -3*60*60)
	return askedFixture{zone: zone, now: time.Date(2026, 10, 14, 20, 0, 0, 0, zone)}
}

func (f askedFixture) at(day, hour, minute int) time.Time {
	return time.Date(2026, 10, day, hour, minute, 0, 0, f.zone)
}

// bareDate is how Decode reads "2026-10-<day>": midnight in the zone.
func (f askedFixture) bareDate(day int) time.Time { return f.at(day, 0, 0) }

func dueClass(k classify.Kind, due time.Time, dayOnly bool) classify.Classification {
	return classify.Classification{Kind: &k, DueAt: &due, DueAtDayOnly: dayOnly}
}

func (f askedFixture) arm(t *testing.T, c classify.Classification, now time.Time, prefs ReminderPrefs) []Plan {
	t.Helper()
	plans, ok := Arm(c, true, prefs, now)
	if !ok {
		t.Fatalf("Arm armed nothing: %+v", plans)
	}
	return plans
}

func wantPlan(t *testing.T, got Plan, fireAt, about time.Time) {
	t.Helper()
	if got.What != ArmTrigger || !got.FireAt.Equal(fireAt) || !got.About.Equal(about) || !got.Asked {
		t.Errorf("plan = %+v, want an asked trigger about %v firing %v", got, about, fireAt)
	}
}

func TestArmAsked_NamedTime(t *testing.T) {
	f := newAsked()
	due := f.at(15, 10, 0)
	plans := f.arm(t, dueClass(classify.KindTask, due, false), f.now, DefaultReminderPrefs())
	if len(plans) != 1 {
		t.Fatalf("plans = %+v, want one", plans)
	}
	wantPlan(t, plans[0], f.at(15, 9, 45), due)
	if plans[0].LeadMinutes != 15 || plans[0].Immediate || plans[0].DayOnly || !plans[0].NightExempt {
		t.Errorf("plan = %+v, want lead 15, not immediate, not day-only, night-exempt", plans[0])
	}
}

func TestArmAsked_DayOnly(t *testing.T) {
	f := newAsked()
	due := f.bareDate(15)
	plans := f.arm(t, dueClass(classify.KindTask, due, true), f.now, DefaultReminderPrefs())
	if len(plans) != 1 {
		t.Fatalf("plans = %+v, want one", plans)
	}
	wantPlan(t, plans[0], f.at(15, 9, 0), due)
	if !plans[0].DayOnly || plans[0].NightExempt || plans[0].LeadMinutes != 0 || len(plans[0].Leads) != 0 {
		t.Errorf("plan = %+v, want day-only, not night-exempt, lead 0, serving no lead", plans[0])
	}
}

func TestArmAsked_DayOnlyFollowsDateOnlyAt(t *testing.T) {
	f := newAsked()
	prefs := DefaultReminderPrefs()
	prefs.DateOnlyAt = TimeOfDay{Hour: 8, Minute: 30}
	plans := f.arm(t, dueClass(classify.KindTask, f.bareDate(15), true), f.now, prefs)
	wantPlan(t, plans[0], f.at(15, 8, 30), f.bareDate(15))
}

func TestArmAsked_NamedMidnightIsNotDayOnly(t *testing.T) {
	f := newAsked()
	due := f.at(16, 0, 0)
	plans := f.arm(t, dueClass(classify.KindTask, due, false), f.now, DefaultReminderPrefs())
	wantPlan(t, plans[0], f.at(15, 23, 45), due)
	if plans[0].DayOnly {
		t.Error("a timestamp at midnight is a named time, never day only")
	}
}

func TestArmAsked_DSTDayWithoutMidnightStaysDayOnly(t *testing.T) {
	santiago, err := time.LoadLocation("America/Santiago")
	if err != nil {
		t.Fatal(err)
	}
	// Chile springs forward at 24:00 on 2026-09-06, so that day has no midnight:
	// its first instant is 01:00, which is what design D3 assumes a bare date
	// decodes to (see the PR note: Go's own ParseInLocation lands a day early).
	due := time.Date(2026, 9, 6, 1, 0, 0, 0, santiago)
	if due.Format("2006-01-02") != "2026-09-06" || due.Hour() != 1 {
		t.Fatalf("precondition: first instant of 2026-09-06 = %v, want 01:00 that day", due)
	}
	now := time.Date(2026, 9, 4, 20, 0, 0, 0, santiago)
	plans, ok := Arm(dueClass(classify.KindTask, due, true), true, DefaultReminderPrefs(), now)
	if !ok || len(plans) != 1 {
		t.Fatalf("Arm = %+v, %v", plans, ok)
	}
	want := time.Date(2026, 9, 6, 9, 0, 0, 0, santiago)
	if !plans[0].FireAt.Equal(want) || !plans[0].DayOnly {
		t.Errorf("plan = %+v, want day-only at %v, not 01:00 minus a lead", plans[0], want)
	}
}

func TestArmAsked_EventGetsThreeLeads(t *testing.T) {
	f := newAsked()
	at := f.at(17, 10, 0)
	k := classify.KindEvent
	plans := f.arm(t, classify.Classification{Kind: &k, EventAt: &at}, f.now, DefaultReminderPrefs())
	if len(plans) != 3 {
		t.Fatalf("plans = %+v, want three", plans)
	}
	fires := []time.Time{f.at(16, 10, 0), f.at(17, 8, 0), f.at(17, 9, 45)}
	asked := []bool{false, false, true}
	for i, p := range plans {
		if !p.FireAt.Equal(fires[i]) || p.Asked != asked[i] {
			t.Errorf("plan %d = %+v, want %v asked=%v", i, p, fires[i], asked[i])
		}
	}
}

func TestArmAsked_EventLeadsThatLandTogetherAreOneAskedReminder(t *testing.T) {
	f := newAsked()
	at := f.at(17, 10, 0)
	k := classify.KindEvent
	prefs := DefaultReminderPrefs()
	prefs.RemindMeLead = 2 * time.Hour
	plans := f.arm(t, classify.Classification{Kind: &k, EventAt: &at}, f.now, prefs)
	if len(plans) != 2 {
		t.Fatalf("plans = %+v, want the 24 h and one merged 2 h", plans)
	}
	merged := plans[1]
	if !merged.Asked || !merged.NightExempt || len(merged.Leads) != 1 || merged.Leads[0] != 2*time.Hour {
		t.Errorf("merged = %+v, want asked, night-exempt, serving the 2 h lead once", merged)
	}
	// A different asked lead on the same instant as another lead is still two leads' worth.
	prefs.TimedLeads = []time.Duration{24 * time.Hour, 3 * time.Hour}
	prefs.RemindMeLead = 3 * time.Hour
	plans = f.arm(t, classify.Classification{Kind: &k, EventAt: &at}, f.now, prefs)
	if len(plans) != 2 || len(plans[1].Leads) != 1 {
		t.Errorf("plans = %+v, want the 3 h lead once", plans)
	}
}

func TestArmAsked_PastLeadAtOnce(t *testing.T) {
	f := newAsked()
	due := f.at(14, 20, 10)
	plans := f.arm(t, dueClass(classify.KindTask, due, false), f.now, DefaultReminderPrefs())
	wantPlan(t, plans[0], f.now, due)
	if !plans[0].Immediate || !plans[0].NightExempt || plans[0].LeadMinutes != 0 {
		t.Errorf("plan = %+v, want immediate, night-exempt, lead 0", plans[0])
	}
}

func TestArmAsked_PastLeadInQuietHoursRingsAtOnce(t *testing.T) {
	f := newAsked()
	prefs := DefaultReminderPrefs()
	prefs.QuietEnd = TimeOfDay{Hour: 8}
	now := f.at(15, 6, 20)
	due := f.at(15, 6, 30)
	plans := f.arm(t, dueClass(classify.KindTask, due, false), now, prefs)
	wantPlan(t, plans[0], now, due)
	if !plans[0].Immediate {
		t.Errorf("plan = %+v, want an at-once reminder: quiet hours do not drop an asked lead", plans[0])
	}
}

func TestArmAsked_NamedTimeInsideQuietHoursNotMoved(t *testing.T) {
	f := newAsked()
	due := f.at(15, 6, 30)
	plans := f.arm(t, dueClass(classify.KindTask, due, false), f.now, DefaultReminderPrefs())
	wantPlan(t, plans[0], f.at(15, 6, 15), due)
}

func TestArmAsked_TooLateTodayRefused(t *testing.T) {
	f := newAsked()
	if AtOnceMarginMinutes != 7 {
		t.Fatal("the margin drifted from doc 02 §13's at_once_margin_minutes row")
	}
	today := dueClass(classify.KindTask, f.bareDate(14), true)
	for _, tc := range []struct {
		name string
		now  time.Time
		late bool
	}{
		{"23:50 leaves ten minutes", f.at(14, 23, 50), false},
		{"23:53 leaves exactly seven", f.at(14, 23, 53), false},
		{"23:54 leaves six", f.at(14, 23, 54), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plans, ok := Arm(today, true, DefaultReminderPrefs(), tc.now)
			if tc.late {
				if ok || len(plans) != 1 || plans[0].What != ArmNothing || plans[0].Why != RefusalTooLate {
					t.Fatalf("Arm = %+v, %v, want one too_late refusal", plans, ok)
				}
				return
			}
			if !ok || len(plans) != 1 || !plans[0].FireAt.Equal(tc.now) || !plans[0].Immediate ||
				!plans[0].NightExempt || !plans[0].DayOnly || len(plans[0].Leads) != 0 {
				t.Fatalf("Arm = %+v, %v, want one at-once, night-exempt day-only reminder serving no lead", plans, ok)
			}
		})
	}
	t.Run("a named time under seven minutes away", func(t *testing.T) {
		plans, ok := Arm(dueClass(classify.KindTask, f.at(14, 20, 6), false), true, DefaultReminderPrefs(), f.now)
		if ok || plans[0].Why != RefusalTooLate {
			t.Errorf("Arm = %+v, %v, want too_late", plans, ok)
		}
	})
	t.Run("an asked lead still ahead is armed, whatever is left before the instant", func(t *testing.T) {
		prefs := DefaultReminderPrefs()
		prefs.RemindMeLead = time.Minute
		due := f.now.Add(3 * time.Minute)
		plans, ok := Arm(dueClass(classify.KindTask, due, false), true, prefs, f.now)
		if !ok || len(plans) != 1 || plans[0].Immediate || !plans[0].FireAt.Equal(f.now.Add(2*time.Minute)) {
			t.Errorf("Arm = %+v, %v, want one reminder at the lead, two minutes from now", plans, ok)
		}
	})
	t.Run("a day already over is past, not too late", func(t *testing.T) {
		plans, ok := Arm(dueClass(classify.KindTask, f.bareDate(13), true), true, DefaultReminderPrefs(), f.now)
		if ok || plans[0].Why != RefusalAlreadyPast {
			t.Errorf("Arm = %+v, %v, want already_past", plans, ok)
		}
	})
}

func TestArmAsked_AnyKindWithADate(t *testing.T) {
	f := newAsked()
	due := f.at(15, 10, 0)
	plans := f.arm(t, dueClass(classify.KindMentalLoad, due, false), f.now, DefaultReminderPrefs())
	wantPlan(t, plans[0], f.at(15, 9, 45), due)

	// The other field serves when a kind's own is absent.
	k := classify.KindTask
	plans = f.arm(t, classify.Classification{Kind: &k, EventAt: &due}, f.now, DefaultReminderPrefs())
	wantPlan(t, plans[0], f.at(15, 9, 45), due)

	k = classify.KindKnowledge
	if plans, ok := Arm(classify.Classification{Kind: &k}, true, DefaultReminderPrefs(), f.now); ok || plans[0].Why != RefusalNoDate {
		t.Errorf("asked with no date = %+v, %v, want no_date", plans, ok)
	}
	k = classify.KindChitchat
	if plans, ok := Arm(classify.Classification{Kind: &k, DueAt: &due}, true, DefaultReminderPrefs(), f.now); ok || plans[0].Why != RefusalKindNotArming {
		t.Errorf("asked chitchat = %+v, %v, want kind_not_arming", plans, ok)
	}
}

func TestArmAsked_TimerUnchanged(t *testing.T) {
	f := newAsked()
	due := f.at(14, 20, 30)
	c := dueClass(classify.KindTimer, due, false)
	asked, okA := Arm(c, true, DefaultReminderPrefs(), f.now)
	plain, okP := Arm(c, false, DefaultReminderPrefs(), f.now)
	if !okA || !okP || len(asked) != 1 || asked[0].Asked || asked[0].What != ArmTimer ||
		!asked[0].FireAt.Equal(due) || asked[0].FireAt != plain[0].FireAt {
		t.Errorf("timer asked = %+v, plain = %+v: a timer fires at its instant whatever the words", asked, plain)
	}
}

func TestArmUnasked_EveryExistingArmAndFollowTestUnchanged(t *testing.T) {
	f := newAsked()
	due := f.at(17, 10, 0)
	if plans, ok := Arm(dueClass(classify.KindTask, due, false), false, DefaultReminderPrefs(), f.now); ok || plans[0].Why != RefusalKindNotArming {
		t.Errorf("an unasked dated task = %+v, %v, want kind_not_arming", plans, ok)
	}
	k := classify.KindEvent
	plans, ok := Arm(classify.Classification{Kind: &k, EventAt: &due}, false, DefaultReminderPrefs(), f.now)
	if !ok || len(plans) != 2 || plans[0].Asked || plans[1].Asked || plans[0].NightExempt || plans[1].NightExempt {
		t.Errorf("an unasked event = %+v, %v, want the two ADR-0029 leads, none asked", plans, ok)
	}
}

func TestParseRemindMeLead_Bounds(t *testing.T) {
	for _, tc := range []struct {
		minutes int
		ok      bool
	}{{0, false}, {1, true}, {43200, true}, {43201, false}, {-5, false}} {
		got, err := ParseRemindMeLead(tc.minutes)
		if (err == nil) != tc.ok {
			t.Errorf("ParseRemindMeLead(%d) error = %v, want ok=%v", tc.minutes, err, tc.ok)
		}
		if tc.ok && got != time.Duration(tc.minutes)*time.Minute {
			t.Errorf("ParseRemindMeLead(%d) = %v", tc.minutes, got)
		}
	}
	if got := ResolveReminderPrefs(nil, nil, nil, nil, nil).RemindMeLead; got != 15*time.Minute {
		t.Errorf("a NULL lead resolves to %v, want 15m", got)
	}
	zero, bad, ok := 0, 43201, 30
	if got := ResolveReminderPrefs(nil, nil, nil, nil, &zero).RemindMeLead; got != 15*time.Minute {
		t.Errorf("an invalid lead resolves to %v, want 15m", got)
	}
	if got := ResolveReminderPrefs(nil, nil, nil, nil, &bad).RemindMeLead; got != 15*time.Minute {
		t.Errorf("an invalid lead resolves to %v, want 15m", got)
	}
	if got := ResolveReminderPrefs(nil, nil, nil, nil, &ok).RemindMeLead; got != 30*time.Minute {
		t.Errorf("a valid lead resolves to %v, want 30m", got)
	}
	if DefaultRemindMeLeadMinutes != 15 {
		t.Error("the default drifted from doc 02 §13")
	}
}

func TestVerdict_NightExemptOverdueFromFireAt(t *testing.T) {
	f := newAsked()
	fire := f.at(15, 3, 0)
	now := f.at(15, 9, 5)
	q := QuietHours{End: TimeOfDay{Hour: 7}}
	exempt := Reminder{FireAt: fire, EventAt: f.at(15, 10, 0), Asked: true, NightExempt: true} // a 7 h lead
	if v, reason := q.ReminderVerdict(exempt, now); v != VerdictStale || reason != LapseOverdue {
		t.Errorf("exempt 03:00 row at 09:05 = %q/%q, want stale/overdue: lateness runs from fire_at", v, reason)
	}
	if v, _ := q.ReminderVerdict(exempt, fire.Add(time.Minute)); v != VerdictDeliver {
		t.Errorf("exempt row inside the night = %q, want deliver, never defer", v)
	}
	if v, _ := q.ReminderVerdict(exempt, f.at(15, 7, 0)); v != VerdictDeliver {
		t.Errorf("exempt 03:00 row at 07:00 = %q, want deliver: four hours late is inside the 6 h trigger window", v)
	}
	plain := exempt
	plain.NightExempt = false
	if v, _ := q.ReminderVerdict(plain, f.at(15, 3, 1)); v != VerdictDefer {
		t.Errorf("unexempt row inside the night = %q, want defer", v)
	}
	if v, _ := q.ReminderVerdict(plain, now); v != VerdictDeliver {
		t.Errorf("unexempt row at 09:05 = %q, want deliver: it is measured from quiet end", v)
	}
}

func TestVerdict_AskedNamedMidnightPassesAtMidnight(t *testing.T) {
	f := newAsked()
	midnight := f.at(16, 0, 0)
	asked := Reminder{FireAt: f.at(15, 23, 45), EventAt: midnight, Asked: true, NightExempt: true, Waiting: true}
	q := DefaultQuietHours()
	if v, reason := q.ReminderVerdict(asked, midnight); v != VerdictStale || reason != LapsePassed {
		t.Errorf("asked named midnight at 00:00 = %q/%q, want stale/passed", v, reason)
	}
	if v, _ := q.ReminderVerdict(asked, f.at(15, 23, 50)); v != VerdictDeliver {
		t.Errorf("asked named midnight at 23:50 = %q, want deliver", v)
	}
	dayOnly := Reminder{FireAt: f.at(15, 9, 0), EventAt: f.at(15, 1, 0), Asked: true, DayOnly: true, Waiting: true}
	if v, _ := q.ReminderVerdict(dayOnly, f.at(15, 10, 0)); v != VerdictDeliver {
		t.Errorf("asked day-only row at 10:00 = %q, want deliver: it is passed from the next midnight, whatever hour its own midnight has", v)
	}
	if v, reason := q.ReminderVerdict(dayOnly, f.at(16, 0, 0)); v != VerdictStale || reason != LapsePassed {
		t.Errorf("asked day-only row the next midnight = %q/%q, want stale/passed", v, reason)
	}
}

func TestVerdict_UnaskedMidnightKeepsDayGranular(t *testing.T) {
	f := newAsked()
	unasked := Reminder{FireAt: f.at(15, 23, 45), EventAt: f.at(16, 0, 0), Waiting: true}
	if v, _ := DefaultQuietHours().ReminderVerdict(unasked, f.at(16, 0, 0)); v != VerdictDeliver {
		t.Errorf("unasked midnight row at 00:00 = %q, want deliver: the midnight heuristic still reads it as a day", v)
	}
}

func TestArmAsked_ABehindAskedLeadRingsAtOnceEvenWhenOthersAreAhead(t *testing.T) {
	f := newAsked()
	at := f.at(15, 10, 0)
	k := classify.KindEvent
	prefs := DefaultReminderPrefs()
	prefs.RemindMeLead = 30 * time.Hour // 10-14 04:00, behind; the 2 h lead (10-15 08:00) is ahead
	plans := f.arm(t, classify.Classification{Kind: &k, EventAt: &at}, f.now, prefs)
	if len(plans) != 2 {
		t.Fatalf("plans = %+v, want the at-once reminder and the 2 h lead", plans)
	}
	if !plans[0].FireAt.Equal(f.now) || !plans[0].Immediate || !plans[0].Asked || !plans[0].NightExempt || len(plans[0].Leads) != 0 {
		t.Errorf("first = %+v, want an asked, night-exempt reminder at once serving no lead (D3 read literally)", plans[0])
	}
	if !plans[1].FireAt.Equal(f.at(15, 8, 0)) || plans[1].Asked {
		t.Errorf("second = %+v, want the unasked 2 h lead", plans[1])
	}
}

func TestArm_AnUnaskedEventMinutesAwayStillRingsAtOnce(t *testing.T) {
	f := newAsked()
	at := f.now.Add(3 * time.Minute) // every lead behind and less than the asked margin left
	k := classify.KindEvent
	plans, ok := Arm(classify.Classification{Kind: &k, EventAt: &at}, false, DefaultReminderPrefs(), f.now)
	if !ok || len(plans) != 1 || !plans[0].Immediate || plans[0].Asked || plans[0].NightExempt {
		t.Errorf("plans = %+v, %v, want ADR-0029 point 3's unasked at-once reminder, margin not applied", plans, ok)
	}
}

func TestArmAsked_AnAskedRepeatingRequestIsTheOneShotUntilLink7(t *testing.T) {
	f := newAsked()
	rule := classify.RecurrenceRuleDaily
	for _, k := range []classify.Kind{classify.KindTask, classify.KindRecurringReminder, classify.KindEvent} {
		due := f.at(15, 21, 0) // far enough ahead that an event's 24 h and 2 h leads are ahead too
		c := classify.Classification{Kind: &k, DueAt: &due, EventAt: &due, RecurrenceRule: &rule}
		plans, ok := Arm(c, true, DefaultReminderPrefs(), f.now)
		if !ok || len(plans) == 0 {
			t.Fatalf("%s: Arm = %+v, %v", k, plans, ok)
		}
		for _, p := range plans {
			if p.What != ArmTrigger || p.Rule != "" {
				t.Errorf("%s: plan = %+v, want a one-shot trigger, never the recurring row", k, p)
			}
		}
		last := plans[len(plans)-1]
		if k != classify.KindEvent && (len(plans) != 1 || !last.FireAt.Equal(f.at(15, 20, 45)) || !last.Asked) {
			t.Errorf("%s: plans = %+v, want one asked one-shot at 20:45", k, plans)
		}
	}
	// Unasked, a recurring reminder keeps its recurring trigger.
	k := classify.KindRecurringReminder
	at := f.at(20, 12, 0)
	plans, _ := Arm(classify.Classification{Kind: &k, EventAt: &at, RecurrenceRule: &rule}, false, DefaultReminderPrefs(), f.now)
	if plans[0].What != ArmRecurring {
		t.Errorf("unasked plan = %+v, want the recurring trigger", plans[0])
	}
}

func TestArmAsked_WhichFieldTheUnitIsRemindedAbout(t *testing.T) {
	f := newAsked()
	eventAt, dueAt := f.at(17, 10, 0), f.at(16, 10, 0)
	both := func(k classify.Kind) classify.Classification {
		return classify.Classification{Kind: &k, EventAt: &eventAt, DueAt: &dueAt}
	}
	for _, tc := range []struct {
		name string
		c    classify.Classification
		last time.Time // the asked lead, the last plan
		at   time.Time
	}{
		{"an event reads event_at", both(classify.KindEvent), f.at(17, 9, 45), eventAt},
		{"a recurring reminder reads event_at", both(classify.KindRecurringReminder), f.at(17, 9, 45), eventAt},
		{"a task reads due_at", both(classify.KindTask), f.at(16, 9, 45), dueAt},
		{"a mental load reads due_at", both(classify.KindMentalLoad), f.at(16, 9, 45), dueAt},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plans := f.arm(t, tc.c, f.now, DefaultReminderPrefs())
			last := plans[len(plans)-1]
			wantPlan(t, last, tc.last, tc.at)
		})
	}
	t.Run("a task falling back to a bare-date event_at keeps its day-only shape", func(t *testing.T) {
		bare := f.bareDate(16)
		k := classify.KindTask
		plans := f.arm(t, classify.Classification{Kind: &k, EventAt: &bare, EventAtDayOnly: true}, f.now, DefaultReminderPrefs())
		wantPlan(t, plans[0], f.at(16, 9, 0), bare)
		if !plans[0].DayOnly {
			t.Errorf("plan = %+v, want day-only", plans[0])
		}
	})
	t.Run("an event falling back to a bare-date due_at keeps its day-only shape", func(t *testing.T) {
		bare := f.bareDate(16)
		k := classify.KindMentalLoad
		c := classify.Classification{Kind: &k, DueAt: &bare, DueAtDayOnly: true}
		wantPlan(t, f.arm(t, c, f.now, DefaultReminderPrefs())[0], f.at(16, 9, 0), bare)
		k = classify.KindEvent
		plans := f.arm(t, classify.Classification{Kind: &k, DueAt: &bare, DueAtDayOnly: true}, f.now, DefaultReminderPrefs())
		wantPlan(t, plans[len(plans)-1], f.at(16, 9, 0), bare)
		if !plans[len(plans)-1].DayOnly {
			t.Errorf("plan = %+v, want day-only", plans[len(plans)-1])
		}
	})
}

func TestArmAsked_AnAskedEventsOtherLeadsFollowTheDayOnlyFlagNotTheMidnight(t *testing.T) {
	f := newAsked()
	k := classify.KindEvent
	named := f.at(17, 0, 0) // said "a medianoche": a named time
	plans := f.arm(t, classify.Classification{Kind: &k, EventAt: &named}, f.now, DefaultReminderPrefs())
	fires := []time.Time{f.at(16, 7, 0), f.at(16, 22, 0), f.at(16, 23, 45)} // the 24 h lead leaves the night (ADR-0030)
	if len(plans) != 3 {
		t.Fatalf("plans = %+v, want the 24 h, 2 h and asked leads of a timed event", plans)
	}
	for i, p := range plans {
		if !p.FireAt.Equal(fires[i]) || p.DayOnly {
			t.Errorf("plan %d = %+v, want %v, not day-only", i, p, fires[i])
		}
	}
	bare := f.bareDate(17)
	plans = f.arm(t, classify.Classification{Kind: &k, EventAt: &bare, EventAtDayOnly: true}, f.now, DefaultReminderPrefs())
	if len(plans) != 2 || !plans[0].FireAt.Equal(f.at(16, 9, 0)) || !plans[1].FireAt.Equal(f.at(17, 9, 0)) {
		t.Errorf("plans = %+v, want the day before and that day at 09:00", plans)
	}
	// Unasked, the midnight heuristic of ADR-0029 is untouched.
	un, ok := Arm(classify.Classification{Kind: &k, EventAt: &named}, false, DefaultReminderPrefs(), f.now)
	if !ok || len(un) != 1 || !un[0].FireAt.Equal(f.at(16, 9, 0)) {
		t.Errorf("unasked plans = %+v, want one reminder the day before at 09:00 (a midnight reads as a date)", un)
	}
}
