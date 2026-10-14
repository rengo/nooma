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
	if !plans[0].DayOnly || plans[0].NightExempt || plans[0].LeadMinutes != 0 {
		t.Errorf("plan = %+v, want day-only, not night-exempt, lead 0", plans[0])
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
	if !merged.Asked || !merged.NightExempt || len(merged.Leads) != 2 {
		t.Errorf("merged = %+v, want asked, night-exempt, serving both leads", merged)
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
	if AtOnceMarginMinutes != 5 {
		t.Fatal("the margin drifted from doc 02 §13's at_once_margin_minutes row")
	}
	today := dueClass(classify.KindTask, f.bareDate(14), true)
	for _, tc := range []struct {
		name string
		now  time.Time
		late bool
	}{
		{"23:50 leaves ten minutes", f.at(14, 23, 50), false},
		{"23:55 leaves exactly five", f.at(14, 23, 55), false},
		{"23:56 leaves four", f.at(14, 23, 56), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plans, ok := Arm(today, true, DefaultReminderPrefs(), tc.now)
			if tc.late {
				if ok || len(plans) != 1 || plans[0].What != ArmNothing || plans[0].Why != RefusalTooLate {
					t.Fatalf("Arm = %+v, %v, want one too_late refusal", plans, ok)
				}
				return
			}
			if !ok || len(plans) != 1 || !plans[0].FireAt.Equal(tc.now) || !plans[0].Immediate {
				t.Fatalf("Arm = %+v, %v, want one at-once reminder", plans, ok)
			}
		})
	}
	t.Run("a named time under five minutes away", func(t *testing.T) {
		plans, ok := Arm(dueClass(classify.KindTask, f.at(14, 20, 3), false), true, DefaultReminderPrefs(), f.now)
		if ok || plans[0].Why != RefusalTooLate {
			t.Errorf("Arm = %+v, %v, want too_late", plans, ok)
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
