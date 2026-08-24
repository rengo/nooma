package brain

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/focus"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

// digestNow is the first pass at or after DigestHour on a Wednesday.
var digestNow = time.Date(2026, 8, 5, prospection.DigestHour, 5, 0, 0, time.UTC)

// undeliveredTriggers returns a fixed set from Undelivered and records
// what was surfaced.
type undeliveredTriggers struct {
	emptyTriggers
	pending  []ports.DueTrigger
	surfaced []string
}

func (r *undeliveredTriggers) Undelivered(context.Context) ([]ports.DueTrigger, error) {
	return r.pending, nil
}

func (r *undeliveredTriggers) Surface(_ context.Context, id string, _ time.Time) error {
	r.surfaced = append(r.surfaced, id)
	return nil
}

// digestUnits answers LiveFocusCandidates from a fixed map.
type digestUnits struct {
	memrepo.Units
	byID map[string]focus.Candidate
	// asked records the ids the digest passed in, so a test can assert a
	// NULL unit id never reaches this port.
	asked []string
}

func (u *digestUnits) LiveFocusCandidates(_ context.Context, ids []string) ([]focus.Candidate, error) {
	u.asked = append(u.asked, ids...)
	out := make([]focus.Candidate, 0, len(ids))
	for _, id := range ids {
		if c, ok := u.byID[id]; ok {
			out = append(out, c)
		}
	}
	return out, nil
}

func digestTrigger(id, unitID, text string) ports.DueTrigger {
	t := ports.DueTrigger{ID: id, FireAt: digestNow.Add(-time.Hour), Payload: ports.TriggerPayload{ActionText: text}}
	if unitID != "" {
		t.UnitID = &unitID
	}
	return t
}

func digestRunner(t *testing.T, triggers ports.TriggerRepo, units ports.UnitRepo, state ports.StateRepo, log ports.DecisionLog, ch ports.Channel) checkRunner {
	t.Helper()
	return checkRunner{
		triggers: triggers, timers: &emptyTimers{}, ids: &countingIDs{},
		log: log, channel: ch, units: units, state: state,
	}
}

// TestDigest_IsSentOnceADay is R3.1.
func TestDigest_IsSentOnceADay(t *testing.T) {
	triggers := &undeliveredTriggers{pending: []ports.DueTrigger{digestTrigger("trg-1", "u-1", "renew the passport")}}
	units := &digestUnits{byID: map[string]focus.Candidate{"u-1": {ID: "u-1", Weight: 1}}}
	ch := &sendingChannel{}
	log := memrepo.NewDecisionLog()
	r := digestRunner(t, triggers, units, memrepo.NewState(), log, ch)

	ctx := context.Background()
	if _, err := r.assembleDigest(ctx, digestNow, true); err != nil {
		t.Fatalf("first digest: %v", err)
	}
	if ch.count() != 1 {
		t.Fatalf("the first pass sent %d digest(s), want 1", ch.count())
	}

	// A second pass the same day.
	if _, err := r.assembleDigest(ctx, digestNow.Add(time.Hour), true); err != nil {
		t.Fatalf("second digest: %v", err)
	}
	if ch.count() != 1 {
		t.Fatalf("a second pass on the same day sent another digest — at a five-minute cadence that is 288 messages a day")
	}

	// The next day.
	if _, err := r.assembleDigest(ctx, digestNow.AddDate(0, 0, 1), true); err != nil {
		t.Fatalf("next day: %v", err)
	}
	if ch.count() != 2 {
		t.Fatalf("the next day sent %d digest(s) in total, want 2", ch.count())
	}
}

// TestDigest_IsNotDueBeforeTheDigestHour.
func TestDigest_IsNotDueBeforeTheDigestHour(t *testing.T) {
	triggers := &undeliveredTriggers{pending: []ports.DueTrigger{digestTrigger("trg-1", "u-1", "x")}}
	ch := &sendingChannel{}
	r := digestRunner(t, triggers, &digestUnits{}, memrepo.NewState(), memrepo.NewDecisionLog(), ch)

	early := time.Date(2026, 8, 5, prospection.DigestHour-1, 0, 0, 0, time.UTC)
	if _, err := r.assembleDigest(context.Background(), early, true); err != nil {
		t.Fatalf("assembleDigest: %v", err)
	}
	if ch.count() != 0 {
		t.Fatal("a digest went out before the digest hour")
	}
}

// TestDigest_EmptyIsNotSent is m3a's own open question, decided here.
//
// A message every morning saying nothing happened is a message people
// learn to ignore — and then the one that matters arrives in the shape
// they learned to ignore.
func TestDigest_EmptyIsNotSent(t *testing.T) {
	ch := &sendingChannel{}
	r := digestRunner(t, &undeliveredTriggers{}, &digestUnits{}, memrepo.NewState(), memrepo.NewDecisionLog(), ch)

	if _, err := r.assembleDigest(context.Background(), digestNow, true); err != nil {
		t.Fatalf("assembleDigest: %v", err)
	}
	if ch.count() != 0 {
		t.Fatal("an empty digest was sent")
	}
}

// TestDigest_LowEnergyHoldsItemsBackAndCountsTheDeferral is R3.2 and I09.
func TestDigest_LowEnergyHoldsItemsBackAndCountsTheDeferral(t *testing.T) {
	pending := make([]ports.DueTrigger, 0, 6)
	byID := map[string]focus.Candidate{}
	for i := 0; i < 6; i++ {
		id := "trg-" + string(rune('a'+i))
		unit := "u-" + string(rune('a'+i))
		pending = append(pending, digestTrigger(id, unit, "item "+id))
		// Descending weight, so the ranking has something to order by.
		byID[unit] = focus.Candidate{ID: unit, Weight: float64(6-i) / 6, LastTouchedAt: digestNow, CreatedAt: digestNow}
	}

	triggers := &undeliveredTriggers{pending: pending}
	state := memrepo.NewState()
	state.RecordEnergy(prospection.EnergyReading{Level: prospection.LowEnergyMax - 0.1, RecordedAt: digestNow.Add(-time.Hour)})
	log := memrepo.NewDecisionLog()
	ch := &sendingChannel{}

	carried, err := digestRunner(t, triggers, &digestUnits{byID: byID}, state, log, ch).
		assembleDigest(context.Background(), digestNow, true)
	if err != nil {
		t.Fatalf("assembleDigest: %v", err)
	}

	if carried != prospection.LowEnergyDigestSize {
		t.Fatalf("carried %d, want LowEnergyDigestSize (%d) — the care gate is what holds the rest back", carried, prospection.LowEnergyDigestSize)
	}
	if len(triggers.surfaced) != carried {
		t.Errorf("surfaced %d but carried %d — only what went out is marked delivered", len(triggers.surfaced), carried)
	}

	rows, err := log.Since(context.Background(), digestNow.Add(-24*time.Hour), -1)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	held := 0
	for _, row := range rows {
		if row.Action == ports.ActionCheckDigestHeld {
			held++
		}
	}
	if want := len(pending) - carried; held != want {
		t.Fatalf("%d held rows, want %d — the audit trail IS the deferral counter, so a held item that wrote no row would reset its own patience every morning", held, want)
	}
}

// TestDigest_ANullUnitIDNeverReachesTheFocusQuery is m3b's own stated
// obligation, and this is the caller it named.
func TestDigest_ANullUnitIDNeverReachesTheFocusQuery(t *testing.T) {
	triggers := &undeliveredTriggers{pending: []ports.DueTrigger{
		digestTrigger("trg-pattern", "", "a pattern watcher"), // no unit id
		digestTrigger("trg-unit", "u-1", "an ordinary one"),
	}}
	units := &digestUnits{byID: map[string]focus.Candidate{"u-1": {ID: "u-1", Weight: 1}}}

	if _, err := digestRunner(t, triggers, units, memrepo.NewState(), memrepo.NewDecisionLog(), &sendingChannel{}).
		assembleDigest(context.Background(), digestNow, true); err != nil {
		t.Fatalf("assembleDigest: %v", err)
	}

	for _, asked := range units.asked {
		if asked == "" {
			t.Fatal("an empty unit id reached LiveFocusCandidates — a pattern_based trigger has none, and m3b's port names not passing one as the caller's obligation")
		}
	}
	if len(units.asked) != 1 {
		t.Errorf("asked for %v, want only the one trigger that has a unit", units.asked)
	}
}

// TestDigest_NoEnergyReadingIsNotLowEnergy is R3.3's own trap: a vault
// whose owner has never answered a check-in must not silently stop
// speaking.
func TestDigest_NoEnergyReadingIsNotLowEnergy(t *testing.T) {
	pending := make([]ports.DueTrigger, 0, 6)
	byID := map[string]focus.Candidate{}
	for i := 0; i < 6; i++ {
		id := "trg-" + string(rune('a'+i))
		unit := "u-" + string(rune('a'+i))
		pending = append(pending, digestTrigger(id, unit, "item"))
		byID[unit] = focus.Candidate{ID: unit, Weight: 1}
	}

	carried, err := digestRunner(t, &undeliveredTriggers{pending: pending}, &digestUnits{byID: byID},
		memrepo.NewState(), memrepo.NewDecisionLog(), &sendingChannel{}).
		assembleDigest(context.Background(), digestNow, true)
	if err != nil {
		t.Fatalf("assembleDigest: %v", err)
	}
	if carried != len(pending) {
		t.Fatalf("carried %d of %d with no energy reading at all — absence is not a low reading, and a vault that has never been asked would otherwise stop speaking", carried, len(pending))
	}
}

// TestRenderDigest_DoesNotMentionWhatItHeld: the point of holding
// something back is that the person does not have to think about it today.
func TestRenderDigest_DoesNotMentionWhatItHeld(t *testing.T) {
	pending := []ports.DueTrigger{
		digestTrigger("trg-carried", "u-1", "the carried one"),
		digestTrigger("trg-held", "u-2", "the held one"),
	}
	got := renderDigest([]prospection.DigestItem{{ID: "trg-carried"}}, pending)

	if !strings.Contains(got, "the carried one") {
		t.Errorf("the digest does not name what it carried:\n%s", got)
	}
	if strings.Contains(got, "the held one") {
		t.Errorf("the digest names what it held back:\n%s\n\nthat defeats the low-energy gate it came from", got)
	}
}
