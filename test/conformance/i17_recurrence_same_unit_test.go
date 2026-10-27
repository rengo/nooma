// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// TestI17_EveryOccurrenceCarriesTheAnchorNotThePrevious proves invariant
// I17's arithmetic half (docs/06-harness.md §4, docs/02-cognitive-core.md
// §7): firing a recurring trigger creates the next one from the anchor, so
// the series never drifts off the date the user named.
//
// This is the arithmetic half. The other half, that the series really moves
// on when each occurrence fires, on the same unit, is
// TestI17_DaysPassAndARecurringReminderKeepsRinging.
//
// It is written as a walk rather than as a pair of examples. The drift this
// guards against is invisible for one step and only appears across a leap
// cycle: advance 29 February by a year and you get 28 February; advance
// THAT and you get 28 February forever. A two-step test passes on the
// broken implementation.
func TestI17_EveryOccurrenceCarriesTheAnchorNotThePrevious(t *testing.T) {
	loc := time.FixedZone("UTC-3", -3*60*60)

	cases := map[string]struct {
		rule   prospection.Rule
		anchor prospection.Anchor
		// wantDay reports the day of month the series must land on in the
		// given occurrence, computed from the calendar rather than from the
		// implementation.
		wantDay func(t time.Time) int
	}{
		"a 29 February anniversary": {
			rule:   prospection.RuleYearly,
			anchor: prospection.Anchor{Month: time.February, Day: 29},
			wantDay: func(t time.Time) int {
				y := t.Year()
				if y%4 == 0 && (y%100 != 0 || y%400 == 0) {
					return 29
				}
				return 28
			},
		},
		"a day-31 monthly reminder": {
			rule:   prospection.RuleMonthly,
			anchor: prospection.Anchor{Day: 31},
			wantDay: func(t time.Time) int {
				// The last day of t's own month, found the same way the
				// calendar finds it and not the way the code under test does.
				first := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
				return first.AddDate(0, 1, -1).Day()
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cursor := time.Date(2027, time.January, 1, 0, 0, 0, 0, loc)

			// 26 steps, derived rather than picked: the monthly case needs
			// two Februaries to show that a day-31 reminder returns to the
			// 31st after clamping rather than sticking at 28, and two
			// Februaries is 14 months. The yearly case needs to cross a
			// leap cycle twice for the same reason, which is 8 years. 26 is
			// the smaller bound that satisfies both when the same number
			// serves both walks, with a margin either side.
			for step := range 26 {
				next := prospection.NextOccurrence(tc.rule, tc.anchor, cursor)

				if !next.After(cursor) {
					t.Fatalf("step %d: occurrence %v does not advance past %v — a trigger that "+
						"re-arms onto its own instant fires forever", step, next, cursor)
				}
				if want := tc.wantDay(next); next.Day() != want {
					t.Fatalf("step %d: occurrence %v landed on day %d, want %d — the series has "+
						"drifted off its anchor, which is what re-deriving exists to prevent",
						step, next, next.Day(), want)
				}
				if tc.rule == prospection.RuleYearly && next.Month() != tc.anchor.Month {
					t.Fatalf("step %d: occurrence %v is in %v, want %v — a yearly series must "+
						"never leave the anchor's month", step, next, next.Month(), tc.anchor.Month)
				}

				cursor = next
			}
		})
	}
}

// i17Zone is the user's zone in the days-pass walks. A fixed offset keeps the
// expected dates plain calendar arithmetic.
var i17Zone = time.FixedZone("UTC-3", -3*60*60)

// i17Start is Friday 16 October 2026, 10:00 local: when every walk captures.
var i17Start = time.Date(2026, 10, 16, 10, 0, 0, 0, i17Zone)

// i17Step is how often the walk runs a check pass: often enough that an
// occurrence on the half hour goes out on time.
const i17Step = 30 * time.Minute

// i17Channel records each message with the local minute it went out at, and
// refuses the first fail sends as a transport outage would.
type i17Channel struct {
	clock *i37Clock
	fail  int
	sent  []string
}

func (c *i17Channel) Name() string                                            { return "test" }
func (c *i17Channel) Receive(context.Context) ([]ports.ChannelMessage, error) { return nil, nil }
func (c *i17Channel) Confirm(context.Context, string) error                   { return nil }
func (c *i17Channel) Close() error                                            { return nil }
func (c *i17Channel) Send(_ context.Context, _ ports.ConversationID, text string) error {
	if c.fail > 0 {
		c.fail--
		return errors.New("telegram is unreachable")
	}
	c.sent = append(c.sent, c.clock.now.In(i17Zone).Format("Mon 2006-01-02 15:04")+"  "+text)
	return nil
}

// i17Case is one recurring reminder captured at i17Start and lived for days,
// with the server down over [downFrom, downUntil) when those are set, and the
// channel refusing its first failSends sends.
type i17Case struct {
	name, text, eventAt, rule string
	interrupt                 float64
	days, failSends           int
	downFrom, downUntil       time.Time
}

// live captures the reminder through the real CaptureService and walks the
// clock, a CheckService pass every i17Step, over memrepo and a fake channel. It
// returns what the channel was sent, and checks I17's other half on the way
// out: one unit, and exactly one armed row of the series, on that unit.
func (c i17Case) live(t *testing.T) []string {
	t.Helper()
	ctx := context.Background()
	clock := &i37Clock{now: i17Start}
	units, triggers, embeddings := memrepo.NewUnits(), memrepo.NewTriggers(), memrepo.NewEmbeddings()
	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatal(err)
	}
	response := fmt.Sprintf(`{"type":"recurring_reminder","normalized_content":%q,"weight":0.6,"decay_rate":0.02,`+
		`"event_at":%q,"recurrence_rule":%q,"interrupt_level":%v}`, c.text, c.eventAt, c.rule, c.interrupt)
	llm := fakeprovider.New(t, i28Case(t, "i17", c.text, response), "i17")
	ids, log := &counterIDs{}, memrepo.NewDecisionLog()
	ch := &i17Channel{clock: clock, fail: c.failSends}
	capture := brain.NewCaptureService(clock, ids, units, embeddings, memrepo.NewLexical(), memrepo.NewRelations(), log,
		llm, llm, llm, fakeprovider.NewEmbeddingFake(embedFakeModel), brain.NewIndex(idx), memrepo.NewSignals(),
		triggers, memrepo.NewTimers(), 0.5, nil)
	check := brain.NewCheckService(clock, triggers, memrepo.NewTimers(), ids, log, ch, units, memrepo.NewState(),
		nil, "12449194", nil, nil, nil)

	if _, err := capture.Capture(ctx, brain.CaptureInput{Text: c.text, Channel: "chat"}); err != nil {
		t.Fatalf("Capture: %v", err)
	}
	for at := i17Start; at.Before(i17Start.AddDate(0, 0, c.days)); at = at.Add(i17Step) {
		if !at.Before(c.downFrom) && at.Before(c.downUntil) {
			continue
		}
		clock.now = at
		if _, err := check.Check(ctx, brain.CheckRequest{}); err != nil {
			t.Fatalf("Check at %v: %v", at, err)
		}
	}

	live, err := triggers.ArmedUnitIDs(ctx)
	if err != nil || len(live) != 1 || units.Count() != 1 {
		t.Errorf("units %d, units with an armed row %v (%v): want one unit carrying its series", units.Count(), live, err)
		return ch.sent
	}
	if armed, err := triggers.ArmedForUnit(ctx, live[0]); err != nil || len(armed) != 1 || armed[0].RecurrenceRule == nil {
		t.Errorf("armed rows of the unit = %+v (%v), want exactly one recurring row", armed, err)
	}
	return ch.sent
}

// TestI17_DaysPassAndARecurringReminderKeepsRinging is doc 02 §7's re-arm as a
// person lives it: capture a recurring reminder, let the days go by, and read
// every message the channel was sent. A recurring reminder rings on its
// occurrence, at the event's time or the date-only time, and moves on to the
// next one when it rings; a server that was down rings once on return, never
// once per missed date (ADR-0034).
//
// Each transcript is testdata/days-pass/<name>.txt and is compared exactly:
// one line per message, "<local day and minute it went out>  <text>".
// Regenerate with -update, and read the diff before committing it.
func TestI17_DaysPassAndARecurringReminderKeepsRinging(t *testing.T) {
	down := func(from, until time.Time) (time.Time, time.Time) { return from, until }
	missedFrom, missedUntil := down(time.Date(2026, 10, 20, 12, 0, 0, 0, i17Zone), time.Date(2026, 10, 24, 21, 0, 0, 0, i17Zone))
	cases := []i17Case{
		{name: "yearly-birthday-bare-date", text: "Leo's birthday", eventAt: "1990-10-23", rule: "yearly", interrupt: 0.2, days: 400},
		{name: "weekly-monday-0830", text: "Team standup", eventAt: "2026-10-19T08:30:00-03:00", rule: "weekly", interrupt: 0.8, days: 400},
		{name: "daily-2000-started-yesterday", text: "Take the pills", eventAt: "2026-10-15T20:00:00-03:00", rule: "daily", interrupt: 0.8, days: 400},
		{name: "monthly-31st-1800", text: "Pay rent", eventAt: "2026-10-31T18:00:00-03:00", rule: "monthly", interrupt: 0.8, days: 400},
		{name: "daily-2000-server-down-four-days", text: "Take the pills", eventAt: "2026-10-15T20:00:00-03:00", rule: "daily", interrupt: 0.8, days: 12,
			downFrom: missedFrom, downUntil: missedUntil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.live(t)
			path := filepath.Join("..", "..", "testdata", "days-pass", c.name+".txt")
			header := fmt.Sprintf("# %q, %s from %q, captured %s, lived %d days with a check every %s",
				c.text, c.rule, c.eventAt, i17Start.Format("Mon 2006-01-02 15:04 -07:00"), c.days, i17Step)
			if !c.downFrom.IsZero() {
				header += fmt.Sprintf(", the server down from %s to %s", c.downFrom.Format("Mon 01-02 15:04"), c.downUntil.Format("Mon 01-02 15:04"))
			}
			transcript := header + "\n" + strings.Join(got, "\n") + "\n"
			if *update {
				if err := os.WriteFile(path, []byte(transcript), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v (run with -update to write it)", path, err)
			}
			if transcript != string(want) {
				t.Errorf("%s differs from what the channel was sent.\ngot:\n%s\nwant:\n%s", path, transcript, want)
			}
		})
	}
}

// TestI17_AFailedRecurringPushIsPushedAgain: a recurring reminder is a push
// (ADR-0034), so one whose send failed is pushed again on the next pass, as a
// one-shot is (ADR-0032 point 2), and is not handed to the digest instead.
func TestI17_AFailedRecurringPushIsPushedAgain(t *testing.T) {
	got := i17Case{text: "Take the pills", eventAt: "2026-10-15T20:00:00-03:00", rule: "daily", interrupt: 0.8,
		days: 2, failSends: 1}.live(t)
	want := []string{
		"Fri 2026-10-16 20:30  Reminder: Take the pills — Fri 16 Oct (This is later than intended.)",
		"Sat 2026-10-17 20:00  Reminder: Take the pills — Sat 17 Oct",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("sent:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestI17_ARecurringReminderDueAtNightWaitsForQuietEnd: an occurrence inside
// quiet hours (00:00 to 07:00 by default) is held, then pushed at quiet end,
// and the series moves on to the next night's (I16, ADR-0034).
func TestI17_ARecurringReminderDueAtNightWaitsForQuietEnd(t *testing.T) {
	got := i17Case{text: "Back up the server", eventAt: "2026-10-16T00:30:00-03:00", rule: "daily", interrupt: 0.8,
		days: 2}.live(t)
	want := []string{
		"Sat 2026-10-17 07:00  Reminder: Back up the server — Sat 17 Oct (This is later than intended.)",
		"Sun 2026-10-18 07:00  Reminder: Back up the server — Sun 18 Oct (This is later than intended.)",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("sent:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
