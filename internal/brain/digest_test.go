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
		log: log, channel: ch, units: units, state: state, conversation: testConversation}
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
	got := renderDigest([]prospection.DigestItem{{ID: "trg-carried"}}, pending, nil)

	if !strings.Contains(got, "the carried one") {
		t.Errorf("the digest does not name what it carried:\n%s", got)
	}
	if strings.Contains(got, "the held one") {
		t.Errorf("the digest names what it held back:\n%s\n\nthat defeats the low-energy gate it came from", got)
	}
}

// --- m3e: the digest's second item source -----------------------------

// digestQuestionNow is when the fixture's questions were created — before
// digestNow, so a question is always queued by the time a digest assembles.
var digestQuestionNow = digestNow.Add(-24 * time.Hour)

// queuedQuestions returns a PendingQuestions fake holding one queued
// question per (id, createdAt, from, to) row, each over its own relation.
//
// The relation ids are derived from the question id rather than passed in:
// the fake's relation-existence set mirrors the real SQLite N1 guard, not a
// live foreign key, so every question needs one and none of these tests has
// anything to say about which.
func queuedQuestions(t *testing.T, rows ...ports.RelationQuestion) *memrepo.PendingQuestions {
	t.Helper()
	repo := memrepo.NewPendingQuestions()
	for _, row := range rows {
		relID := "rel-" + row.ID
		repo.EnsureRelation(t, relID, "same_topic", "u-from-"+row.ID, "u-to-"+row.ID, row.FromContent, row.ToContent)
		if err := repo.Create(context.Background(), ports.PendingQuestion{
			ID: row.ID, Kind: ports.QuestionKindRelation, RelationID: relID, CreatedAt: row.CreatedAt,
		}); err != nil {
			t.Fatalf("seeding question %s: %v", row.ID, err)
		}
	}
	return repo
}

// question is one queuedQuestions row, spelled short.
func question(id string, createdAt time.Time, from, to string) ports.RelationQuestion {
	return ports.RelationQuestion{ID: id, CreatedAt: createdAt, FromContent: from, ToContent: to}
}

// questionRunner is digestRunner with a question store attached.
func questionRunner(t *testing.T, triggers ports.TriggerRepo, units ports.UnitRepo, state ports.StateRepo, log ports.DecisionLog, ch ports.Channel, questions ports.PendingQuestionRepo) checkRunner {
	t.Helper()
	r := digestRunner(t, triggers, units, state, log, ch)
	r.questions = questions
	return r
}

// countAction is how many rows of one action the log holds.
func countAction(t *testing.T, log *memrepo.DecisionLog, at time.Time, action ports.DecisionAction) int {
	t.Helper()
	rows, err := log.Since(context.Background(), at.Add(-30*24*time.Hour), -1)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	n := 0
	for _, row := range rows {
		if row.Action == action {
			n++
		}
	}
	return n
}

// TestDigest_WithNoQueuedQuestionIsUnchanged is the control this whole
// group needs: the second item source must be invisible when the queue is
// empty, or every assertion below is about the fixture rather than about
// the feature.
func TestDigest_WithNoQueuedQuestionIsUnchanged(t *testing.T) {
	triggers := &undeliveredTriggers{pending: []ports.DueTrigger{digestTrigger("trg-1", "u-1", "renew the passport")}}
	units := &digestUnits{byID: map[string]focus.Candidate{"u-1": {ID: "u-1", Weight: 1}}}
	ch := &sendingChannel{}
	log := memrepo.NewDecisionLog()

	if _, err := questionRunner(t, triggers, units, memrepo.NewState(), log, ch, queuedQuestions(t)).
		assembleDigest(context.Background(), digestNow, true); err != nil {
		t.Fatalf("assembleDigest: %v", err)
	}

	if ch.count() != 1 {
		t.Fatalf("sent %d digest(s), want 1", ch.count())
	}
	if got, want := ch.sent[0], "Here is 1 thing for today:\n• renew the passport"; got != want {
		t.Errorf("digest text = %q, want the trigger-only shape %q — an empty queue must change nothing", got, want)
	}
	if n := countAction(t, log, digestNow, ports.ActionCheckDigestQuestionAsked); n != 0 {
		t.Errorf("%d question_asked row(s) with an empty queue, want 0", n)
	}
}

// TestDigest_AsksExactlyOneQuestionOldestFirst is R3's cap and design
// §3.5's FIFO tie-break, together: N queued questions produce exactly one
// digest line, and it is the oldest.
//
// The cap is the whole of owner ruling Q2 — a relation has two endpoints
// and focus.Priority ranks one unit, so a question cannot compete with the
// ranked items and is appended instead. Asking two at once would turn the
// digest into a form.
func TestDigest_AsksExactlyOneQuestionOldestFirst(t *testing.T) {
	// q-a is the NEWEST and sorts FIRST by id: a digest that took the head
	// of an id-ordered read would ask it, and the oldest question would
	// wait behind every question created after it.
	questions := queuedQuestions(t,
		question("q-a", digestQuestionNow.Add(2*time.Hour), "the newest from", "the newest to"),
		question("q-b", digestQuestionNow, "the oldest from", "the oldest to"),
		question("q-c", digestQuestionNow.Add(time.Hour), "the middle from", "the middle to"),
	)
	triggers := &undeliveredTriggers{pending: []ports.DueTrigger{digestTrigger("trg-1", "u-1", "renew the passport")}}
	units := &digestUnits{byID: map[string]focus.Candidate{"u-1": {ID: "u-1", Weight: 1}}}
	ch := &sendingChannel{}
	log := memrepo.NewDecisionLog()

	ctx := context.Background()
	if _, err := questionRunner(t, triggers, units, memrepo.NewState(), log, ch, questions).
		assembleDigest(ctx, digestNow, true); err != nil {
		t.Fatalf("assembleDigest: %v", err)
	}

	if ch.count() != 1 {
		t.Fatalf("sent %d digest(s), want 1", ch.count())
	}
	text := ch.sent[0]
	if !strings.Contains(text, "the oldest from") || !strings.Contains(text, "the oldest to") {
		t.Errorf("digest text = %q, want it to name the OLDEST queued question's endpoints — the tie-break is created_at then id, FIFO", text)
	}
	for _, other := range []string{"the newest from", "the middle from"} {
		if strings.Contains(text, other) {
			t.Errorf("digest text = %q also names %q — at most one question per digest (Q2)", text, other)
		}
	}

	// Asked exactly once, and only the one that went out.
	open, err := questions.Open(ctx)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(open) != 1 || open[0].ID != "q-b" {
		t.Fatalf("Open() = %+v, want exactly q-b — MarkAsked runs once per digest, on the question it asked", open)
	}
	unasked, err := questions.Unasked(ctx)
	if err != nil {
		t.Fatalf("Unasked: %v", err)
	}
	if len(unasked) != 2 {
		t.Errorf("Unasked() = %+v, want the two questions this digest did not ask", unasked)
	}
	if n := countAction(t, log, digestNow, ports.ActionCheckDigestQuestionAsked); n != 1 {
		t.Errorf("%d question_asked row(s), want exactly 1 — I12 is one row per effect", n)
	}
}

// TestDigest_AQuestionAloneIsStillSent is R3's own MUST, and it reverses
// what digest.go decided in m3d.
//
// "An empty digest is not sent" is about Carry's items having nothing to
// say. A digest that asks "I linked X with Y, are they related?" has
// content and it wants something — and a vault with an uncertain relation
// and no triggers would otherwise stay silent forever, which is the demo
// the proposal describes.
func TestDigest_AQuestionAloneIsStillSent(t *testing.T) {
	questions := queuedQuestions(t, question("q-1", digestQuestionNow, "plan the offsite", "the travel budget"))
	ch := &sendingChannel{}
	log := memrepo.NewDecisionLog()

	carried, err := questionRunner(t, &undeliveredTriggers{}, &digestUnits{}, memrepo.NewState(), log, ch, questions).
		assembleDigest(context.Background(), digestNow, true)
	if err != nil {
		t.Fatalf("assembleDigest: %v", err)
	}

	if ch.count() != 1 {
		t.Fatalf("sent %d digest(s) with zero triggers and one queued question, want 1 — a pending question is not nothing to say", ch.count())
	}
	if !strings.Contains(ch.sent[0], "plan the offsite") || !strings.Contains(ch.sent[0], "the travel budget") {
		t.Errorf("digest text = %q, want both endpoints named", ch.sent[0])
	}
	if carried != 0 {
		t.Errorf("DigestCarried = %d, want 0 — a question is not a carried trigger item, and counting it as one would let it feed the deferral arithmetic", carried)
	}
	if n := countAction(t, log, digestNow, ports.ActionCheckDigestSent); n != 1 {
		t.Errorf("%d digest_sent row(s), want 1 — the digest that went out must be counted, or tomorrow's DigestDue reads the wrong last-sent instant", n)
	}
}

// TestDigest_LowEnergyAsksNothingAndLeavesTheQueueUntouched is design
// §3.5's rule 2: doc 02 §7's care gate holds back non-urgent items, and a
// graph question is the most non-urgent thing this system produces.
//
// Untouched matters as much as unasked: a question consumed by a digest
// that did not ask it would be lost, since MarkAsked is one-way.
func TestDigest_LowEnergyAsksNothingAndLeavesTheQueueUntouched(t *testing.T) {
	questions := queuedQuestions(t, question("q-1", digestQuestionNow, "plan the offsite", "the travel budget"))
	triggers := &undeliveredTriggers{pending: []ports.DueTrigger{digestTrigger("trg-1", "u-1", "renew the passport")}}
	units := &digestUnits{byID: map[string]focus.Candidate{"u-1": {ID: "u-1", Weight: 1}}}
	state := memrepo.NewState()
	state.RecordEnergy(prospection.EnergyReading{Level: prospection.LowEnergyMax - 0.1, RecordedAt: digestNow.Add(-time.Hour)})
	ch := &sendingChannel{}

	ctx := context.Background()
	if _, err := questionRunner(t, triggers, units, state, memrepo.NewDecisionLog(), ch, questions).
		assembleDigest(ctx, digestNow, true); err != nil {
		t.Fatalf("assembleDigest: %v", err)
	}

	if ch.count() != 1 {
		t.Fatalf("sent %d digest(s), want 1 — low energy truncates the digest, it does not cancel it", ch.count())
	}
	if strings.Contains(ch.sent[0], "plan the offsite") {
		t.Errorf("a low-energy digest asked a relation question:\n%s", ch.sent[0])
	}
	unasked, err := questions.Unasked(ctx)
	if err != nil {
		t.Fatalf("Unasked: %v", err)
	}
	if len(unasked) != 1 {
		t.Fatalf("Unasked() = %+v after a low-energy digest, want the question still queued — MarkAsked is one-way, so a question consumed but not asked is a question lost", unasked)
	}
}

// TestDigest_WithoutAQuestionRepoStillSends is the nil-tolerance
// checkRunner.questions shares with channel, units and state: a pass
// without it still fires, delivers and expires, it simply asks nothing.
func TestDigest_WithoutAQuestionRepoStillSends(t *testing.T) {
	triggers := &undeliveredTriggers{pending: []ports.DueTrigger{digestTrigger("trg-1", "u-1", "renew the passport")}}
	units := &digestUnits{byID: map[string]focus.Candidate{"u-1": {ID: "u-1", Weight: 1}}}
	ch := &sendingChannel{}

	// digestRunner leaves questions nil, which is the case under test.
	if _, err := digestRunner(t, triggers, units, memrepo.NewState(), memrepo.NewDecisionLog(), ch).
		assembleDigest(context.Background(), digestNow, true); err != nil {
		t.Fatalf("assembleDigest with a nil question repo: %v", err)
	}
	if ch.count() != 1 {
		t.Fatalf("sent %d digest(s) with a nil question repo, want 1", ch.count())
	}
}

// TestDigest_DryRunAsksNothing: a preview reads the queue and reports the
// same counts, and marks nothing asked. MarkAsked being one-way makes this
// the expensive half of Q1's suppression rule — a dry run that consumed a
// question would cost a question per invocation of `nooma check --dry-run`.
func TestDigest_DryRunAsksNothing(t *testing.T) {
	questions := queuedQuestions(t, question("q-1", digestQuestionNow, "plan the offsite", "the travel budget"))
	ch := &sendingChannel{}

	ctx := context.Background()
	if _, err := questionRunner(t, &undeliveredTriggers{}, &digestUnits{}, memrepo.NewState(), memrepo.NewDecisionLog(), ch, questions).
		assembleDigest(ctx, digestNow, false); err != nil {
		t.Fatalf("assembleDigest(dry run): %v", err)
	}
	if ch.count() != 0 {
		t.Fatalf("a dry run sent %d digest(s)", ch.count())
	}
	unasked, err := questions.Unasked(ctx)
	if err != nil {
		t.Fatalf("Unasked: %v", err)
	}
	if len(unasked) != 1 {
		t.Fatalf("Unasked() = %+v after a dry run, want the question still queued", unasked)
	}
}

// TestRenderDigest_NamesBothEndpointsAfterTheItems is R3's line-shape
// MUST, matching doc 02 §4's own wording.
func TestRenderDigest_NamesBothEndpointsAfterTheItems(t *testing.T) {
	pending := []ports.DueTrigger{digestTrigger("trg-1", "u-1", "renew the passport")}
	q := question("q-1", digestQuestionNow, "plan the offsite", "the travel budget")

	got := renderDigest([]prospection.DigestItem{{ID: "trg-1"}}, pending, &q)

	want := "Here is 1 thing for today:\n• renew the passport\n\n" +
		"One more — I linked \"plan the offsite\" with \"the travel budget\". Are they related?"
	if got != want {
		t.Errorf("renderDigest() =\n%q\nwant\n%q", got, want)
	}
}

// TestRenderDigest_WithoutItemsDropsTheHeaderRatherThanLie: a digest sent
// for a question alone cannot say "Here are 0 things for today" — the
// header counts Carry's items, and there are none.
//
// The question sentence itself is the same one the appended form uses, so
// the two shapes cannot drift into two different questions.
func TestRenderDigest_WithoutItemsDropsTheHeaderRatherThanLie(t *testing.T) {
	q := question("q-1", digestQuestionNow, "plan the offsite", "the travel budget")

	got := renderDigest(nil, nil, &q)

	if strings.Contains(got, "for today") {
		t.Errorf("renderDigest() with no carried items still writes the item header:\n%q", got)
	}
	if want := "I linked \"plan the offsite\" with \"the travel budget\". Are they related?"; got != want {
		t.Errorf("renderDigest() = %q, want %q", got, want)
	}
}

// TestRenderDigest_TruncatesEndpointsToTheSnippetBound: units.content is
// unbounded and a digest line is not.
func TestRenderDigest_TruncatesEndpointsToTheSnippetBound(t *testing.T) {
	// Multi-byte on purpose: a byte-wise truncation would cut a rune in
	// half and render a replacement character.
	long := strings.Repeat("á", questionSnippetRunes+20)
	q := question("q-1", digestQuestionNow, long, "short")

	got := renderDigest(nil, nil, &q)

	quoted := strings.Repeat("á", questionSnippetRunes) + "…"
	if !strings.Contains(got, quoted) {
		t.Errorf("renderDigest() = %q, want the from-endpoint truncated to %d runes with an ellipsis", got, questionSnippetRunes)
	}
	if strings.Contains(got, "�") {
		t.Errorf("renderDigest() = %q — a rune was cut in half", got)
	}
	if strings.Contains(got, "short…") {
		t.Errorf("renderDigest() = %q — an endpoint shorter than the bound was given an ellipsis anyway", got)
	}
}
