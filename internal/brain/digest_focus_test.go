package brain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/focus"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// The digest is the focus keeper's second writer (design m4c §3.5): it
// computes after its last unsent path and publishes right after Send
// succeeds. Every test here builds on FX-H (focuskeeper_test.go) with a
// fresh keeper, so what a digest did or did not publish is read off the next
// Today request: B rises to 1.03, inside A's margin, and the first request to
// reach the keeper shows A in slot 7 if an incumbent holding A was published,
// and B if nothing was.

// seedTrigger seeds one fired, undelivered trigger on a knowledge unit.
// Knowledge is in neither focus, so the unit never enters the pool whose
// incumbent is under test. It returns an error rather than failing t, so a
// goroutine can use it.
func (f *fxH) seedTrigger(id, unitID string) error {
	ctx := context.Background()
	if err := f.units.Create(ctx, unit.Unit{
		ID: unitID, Type: unit.TypeKnowledge, Status: unit.StatusPool, Content: unitID,
		Weight: 1, LastTouchedAt: todayNow, CreatedAt: todayNow, UpdatedAt: todayNow,
	}); err != nil {
		return err
	}
	uid, fireAt := unitID, digestNow.Add(-time.Hour)
	if err := f.triggers.Create(ctx, ports.Trigger{
		ID: id, UnitID: &uid, Kind: ports.TriggerKindTimeBased,
		Payload: ports.TriggerPayload{ActionText: "act on " + id}, FireAt: &fireAt, CreatedAt: digestNow,
	}); err != nil {
		return err
	}
	return f.triggers.Fire(ctx, id, digestNow)
}

func (f *fxH) pendingTrigger(t *testing.T, id, unitID string) {
	t.Helper()
	if err := f.seedTrigger(id, unitID); err != nil {
		t.Fatalf("seed trigger %s: %v", id, err)
	}
}

// digestRunner is the digest over the fixture's own stores and keeper.
func (f *fxH) digestRunner(ch ports.Channel) checkRunner {
	return checkRunner{
		triggers: f.triggers, timers: &emptyTimers{}, ids: f.ids,
		log: f.log, channel: ch, units: f.flaky, state: f.state,
		conversation: testConversation, questions: f.questions, focus: f.keeper,
	}
}

// slotSevenAfter raises B inside A's margin and returns who holds the task
// focus's last slot on the next Today request.
func (f *fxH) slotSevenAfter(t *testing.T) string {
	t.Helper()
	f.setWeight(t, "B", 1.03)
	got, _ := focusIDs(f.request(t))
	return got[len(got)-1]
}

// digestFixture is FX-H with one pending trigger: a digest is due and has
// content.
func digestFixture(t *testing.T) *fxH {
	t.Helper()
	f := newFxH(t)
	f.taskContest(t, 1.0, 0.99)
	f.pendingTrigger(t, "trg-1", "k-1")
	return f
}

func (f *fxH) focusRows(t *testing.T) int {
	t.Helper()
	return countAction(t, f.log, digestNow, ports.ActionCheckFocusUnavailable)
}

func TestDigest_FailedSendPublishesNothing(t *testing.T) {
	f := digestFixture(t)
	ch := &sendingChannel{err: errKeeperBoom}
	if _, err := f.digestRunner(ch).assembleDigest(context.Background(), digestNow, true); err != nil {
		t.Fatalf("assembleDigest: %v", err)
	}
	if got := f.slotSevenAfter(t); got != "B" {
		t.Fatalf("slot 7 = %s, want B — a digest whose Send failed must not publish its selection", got)
	}
}

// surfaceFailingTriggers is the fixture's trigger repo whose Surface fails.
type surfaceFailingTriggers struct{ ports.TriggerRepo }

func (surfaceFailingTriggers) Surface(context.Context, string, time.Time) error {
	return errKeeperBoom
}

// TestDigest_PublishesBeforeSurface pins where the publish sits: the user has
// seen the digest once Send succeeds, so a later Surface failure must not take
// the incumbent back. Moving the publish below the Surface loop leaves every
// other test green; this one is red there.
func TestDigest_PublishesBeforeSurface(t *testing.T) {
	f := digestFixture(t)
	r := f.digestRunner(&sendingChannel{})
	r.triggers = surfaceFailingTriggers{TriggerRepo: f.triggers}
	if _, err := r.assembleDigest(context.Background(), digestNow, true); !errors.Is(err, errKeeperBoom) {
		t.Fatalf("assembleDigest error = %v, want the Surface failure", err)
	}
	if got := f.slotSevenAfter(t); got != "A" {
		t.Fatalf("slot 7 = %s, want A — the digest was sent, so its selection is the incumbent even when Surface fails", got)
	}
}

func TestDigest_DryRunPublishesNothing(t *testing.T) {
	f := digestFixture(t)
	if _, err := f.digestRunner(&sendingChannel{}).assembleDigest(context.Background(), digestNow, false); err != nil {
		t.Fatalf("assembleDigest: %v", err)
	}
	if got := f.slotSevenAfter(t); got != "B" {
		t.Fatalf("slot 7 = %s, want B — a dry run must not publish", got)
	}
}

func TestDigest_EmptyDigestPublishesNothing(t *testing.T) {
	f := newFxH(t)
	f.taskContest(t, 1.0, 0.99)
	ch := &sendingChannel{}
	if _, err := f.digestRunner(ch).assembleDigest(context.Background(), digestNow, true); err != nil {
		t.Fatalf("assembleDigest: %v", err)
	}
	if ch.count() != 0 {
		t.Fatal("an empty digest was sent")
	}
	if got := f.slotSevenAfter(t); got != "B" {
		t.Fatalf("slot 7 = %s, want B — an empty digest must not publish", got)
	}
}

func TestDigest_NoConversationPublishesNothing(t *testing.T) {
	f := digestFixture(t)
	r := f.digestRunner(&sendingChannel{})
	r.conversation = ""
	if _, err := r.assembleDigest(context.Background(), digestNow, true); err != nil {
		t.Fatalf("assembleDigest: %v", err)
	}
	if got := countAction(t, f.log, digestNow, ports.ActionCheckDeliveryFailed); got != 1 {
		t.Fatalf("check.delivery_failed rows = %d, want 1 — the digest had nowhere to go", got)
	}
	if got := f.slotSevenAfter(t); got != "B" {
		t.Fatalf("slot 7 = %s, want B — a digest with no conversation must not publish", got)
	}
}

// TestDigest_QuestionOnlyDigestPublishesWhenSent: the user saw a digest, so
// its selection becomes the incumbent, though it carried no item.
func TestDigest_QuestionOnlyDigestPublishesWhenSent(t *testing.T) {
	f := newFxH(t)
	f.taskContest(t, 1.0, 0.99)
	f.questions.PendingQuestions = queuedQuestions(t, question("q-1", digestQuestionNow, "plan the offsite", "the travel budget"))
	ch := &sendingChannel{}
	carried, err := f.digestRunner(ch).assembleDigest(context.Background(), digestNow, true)
	if err != nil {
		t.Fatalf("assembleDigest: %v", err)
	}
	if carried != 0 || ch.count() != 1 {
		t.Fatalf("carried %d, sent %d, want a question-only digest: 0 items, 1 message", carried, ch.count())
	}
	if got := f.slotSevenAfter(t); got != "A" {
		t.Fatalf("slot 7 = %s, want A — a sent question-only digest publishes its selection", got)
	}
}

// TestDigest_EmptyDigestDoesNotCompute: the empty-digest return sits above
// every keeper read, so a keeper that cannot compute leaves an empty digest
// with no check.focus.unavailable row.
func TestDigest_EmptyDigestDoesNotCompute(t *testing.T) {
	f := newFxH(t)
	f.taskContest(t, 1.0, 0.99)
	f.cfgPort.fail = true
	if _, err := f.digestRunner(&sendingChannel{}).assembleDigest(context.Background(), digestNow, true); err != nil {
		t.Fatalf("assembleDigest: %v", err)
	}
	if got := f.focusRows(t); got != 0 {
		t.Fatalf("check.focus.unavailable rows = %d, want 0 — an empty digest never reaches the keeper", got)
	}
}

// TestDigest_NoConversationDoesNotCompute: compute runs after the
// conversation check, so a digest with nowhere to go writes only its
// delivery-failed row.
func TestDigest_NoConversationDoesNotCompute(t *testing.T) {
	f := digestFixture(t)
	f.cfgPort.fail = true
	r := f.digestRunner(&sendingChannel{})
	r.conversation = ""
	if _, err := r.assembleDigest(context.Background(), digestNow, true); err != nil {
		t.Fatalf("assembleDigest: %v", err)
	}
	if got := f.focusRows(t); got != 0 {
		t.Fatalf("check.focus.unavailable rows = %d, want 0 — compute runs only once the digest can be sent", got)
	}
	if got := countAction(t, f.log, digestNow, ports.ActionCheckDeliveryFailed); got != 1 {
		t.Fatalf("check.delivery_failed rows = %d, want 1", got)
	}
}

// TestDigest_FocusErrorStillSendsDigest: a focus that cannot be computed must
// not stop a digest the user is owed. It is sent, its item surfaced, and the
// failure recorded once.
func TestDigest_FocusErrorStillSendsDigest(t *testing.T) {
	f := digestFixture(t)
	f.cfgPort.fail = true
	ch := &sendingChannel{}
	carried, err := f.digestRunner(ch).assembleDigest(context.Background(), digestNow, true)
	if err != nil {
		t.Fatalf("assembleDigest: %v", err)
	}
	if carried != 1 || ch.count() != 1 {
		t.Fatalf("carried %d, sent %d, want the digest sent with its 1 item", carried, ch.count())
	}
	pending, err := f.triggers.Undelivered(context.Background())
	if err != nil {
		t.Fatalf("Undelivered: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("%d trigger(s) still undelivered, want the item surfaced", len(pending))
	}
	if got := f.focusRows(t); got != 1 {
		t.Fatalf("check.focus.unavailable rows = %d, want exactly 1", got)
	}
	if got := string(ports.ActionCheckFocusUnavailable); got != "check.focus.unavailable" {
		t.Fatalf("the action is %q, want the vocabulary name doc 02 and the glass box show", got)
	}
	if got := countAction(t, f.log, digestNow, ports.ActionCheckDigestSent); got != 1 {
		t.Fatalf("check.digest.sent rows = %d, want 1", got)
	}
}

// TestDigest_FocusErrorRepeatsPerRetryScan: DigestDue holds until a digest is
// sent, so with a failing focus and a failing channel every scan tick writes
// one focus row beside its delivery-failed row.
func TestDigest_FocusErrorRepeatsPerRetryScan(t *testing.T) {
	f := digestFixture(t)
	f.cfgPort.fail = true
	r := f.digestRunner(&sendingChannel{err: errKeeperBoom})
	for i := 0; i < 2; i++ {
		if _, err := r.assembleDigest(context.Background(), digestNow.Add(time.Duration(i)*5*time.Minute), true); err != nil {
			t.Fatalf("scan %d: %v", i+1, err)
		}
	}
	if got := f.focusRows(t); got != 2 {
		t.Fatalf("check.focus.unavailable rows = %d, want 2 — one per retry scan", got)
	}
	if got := countAction(t, f.log, digestNow, ports.ActionCheckDeliveryFailed); got != 2 {
		t.Fatalf("check.delivery_failed rows = %d, want 2", got)
	}
}

// TestDigest_DryRunFocusErrorWritesNothing: check.focus.unavailable is a
// decision_log write, so a dry run neither computes nor records it, and its
// count equals the wet run's.
func TestDigest_DryRunFocusErrorWritesNothing(t *testing.T) {
	dry := digestFixture(t)
	dry.cfgPort.fail = true
	dryCount, err := dry.digestRunner(&sendingChannel{}).assembleDigest(context.Background(), digestNow, false)
	if err != nil {
		t.Fatalf("dry assembleDigest: %v", err)
	}
	rows, err := dry.log.Since(context.Background(), digestNow.Add(-24*time.Hour), -1)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("a dry run wrote %d decision_log row(s): %+v", len(rows), rows)
	}

	wet := digestFixture(t)
	wet.cfgPort.fail = true
	wetCount, err := wet.digestRunner(&sendingChannel{}).assembleDigest(context.Background(), digestNow, true)
	if err != nil {
		t.Fatalf("wet assembleDigest: %v", err)
	}
	if dryCount != wetCount {
		t.Fatalf("dry run counted %d item(s), the wet run %d — a dry run must preview the real one", dryCount, wetCount)
	}
}

// TestDigest_FocusErrorPublishesNothing is the L1-13b killer: the keeper is
// SEEDED (Today holds {F1..F6, A}), a toggled failing config then fails only
// the digest's compute, and afterwards A must still be held. A publish of an
// empty round on the error would erase it and B would take the slot.
func TestDigest_FocusErrorPublishesNothing(t *testing.T) {
	f := digestFixture(t)
	seed, _ := focusIDs(f.request(t))
	assertIDs(t, "seeding request task focus", seed, append(ids("F", 6), "A"))

	f.cfgPort.fail = true
	ch := &sendingChannel{}
	if _, err := f.digestRunner(ch).assembleDigest(context.Background(), digestNow, true); err != nil {
		t.Fatalf("assembleDigest: %v", err)
	}
	if ch.count() != 1 {
		t.Fatalf("the digest was not sent")
	}
	f.cfgPort.fail = false

	if got := f.slotSevenAfter(t); got != "A" {
		t.Fatalf("slot 7 = %s, want A — a failed digest compute must leave the incumbent where it was", got)
	}
}

// TestDigest_TodaySeesDigestIncumbent is R9's second scenario: a digest run
// whose Select keeps A against a challenger inside the margin leaves A held,
// so Today's very first request keeps it: the digest's incumbent is Today's.
func TestDigest_TodaySeesDigestIncumbent(t *testing.T) {
	f := digestFixture(t)
	ch := &sendingChannel{}
	if _, err := f.digestRunner(ch).assembleDigest(context.Background(), digestNow, true); err != nil {
		t.Fatalf("assembleDigest: %v", err)
	}
	if ch.count() != 1 {
		t.Fatal("the digest was not sent")
	}
	if got := f.slotSevenAfter(t); got != "A" {
		t.Fatalf("slot 7 = %s, want A — Today must find the incumbent the digest published", got)
	}
}

// TestDigest_SecondDigestSeesFirst is R9's third scenario: with Today never
// requested, the second digest Selects against the first one's incumbent.
// After the first digest holds A, B rises inside the margin; the second digest
// must keep A and publish it, where a digest ignoring what it loaded would
// publish B.
func TestDigest_SecondDigestSeesFirst(t *testing.T) {
	f := digestFixture(t)
	ctx := context.Background()
	if _, err := f.digestRunner(&sendingChannel{}).assembleDigest(ctx, digestNow, true); err != nil {
		t.Fatalf("first digest: %v", err)
	}

	f.setWeight(t, "B", 1.03)
	f.pendingTrigger(t, "trg-2", "k-2")
	ch := &sendingChannel{}
	next := digestNow.AddDate(0, 0, 1)
	if _, err := f.digestRunner(ch).assembleDigest(ctx, next, true); err != nil {
		t.Fatalf("second digest: %v", err)
	}
	if ch.count() != 1 {
		t.Fatal("the second digest was not sent")
	}

	got, _ := focusIDs(f.request(t))
	if last := got[len(got)-1]; last != "A" {
		t.Fatalf("slot 7 = %s, want A — the second digest must Select against the first one's incumbent", last)
	}
}

// taggedRound is a whole two-Kind snapshot whose task and load members carry
// the same writer tag, so a reader can tell a complete selection from a mix.
func taggedRound(tag string) focusRound {
	return focusRound{next: &incumbent{byKind: map[focus.Kind]focus.Selection{
		focus.KindTask: {Kind: focus.KindTask, Members: []string{tag}},
		focus.KindLoad: {Kind: focus.KindLoad, Members: []string{tag}},
	}}}
}

// consistent reports whether snap is one writer's complete selection: either
// a tagged round (task and load carry the same tag) or a real round over a
// vault with no load units (an empty load focus).
func consistent(snap *incumbent) bool {
	task := snap.byKind[focus.KindTask].Members
	load := snap.byKind[focus.KindLoad].Members
	if len(task) == 1 && strings.HasPrefix(task[0], "w") {
		return len(load) == 1 && load[0] == task[0]
	}
	return len(load) == 0
}

// TestFocusKeeper_ConcurrentTodayAndDigestAreRaceFree is R5: concurrent Today
// requests and digest runs, plus two more writers publishing tagged snapshots,
// are `go test -race` clean, and every snapshot a reader ever observes is one
// writer's complete selection, never a mix of two. The race detector is what
// fails a plain, non-atomic `held`; the tags are what fail a per-Kind setter.
func TestFocusKeeper_ConcurrentTodayAndDigestAreRaceFree(t *testing.T) {
	f := digestFixture(t)
	ctx := context.Background()
	const rounds = 40

	var writers, readers sync.WaitGroup
	stop := make(chan struct{})
	var mixed atomic.Int64

	for r := 0; r < 2; r++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if snap := f.keeper.held.Load(); snap != nil && !consistent(snap) {
					mixed.Add(1)
				}
			}
		}()
	}

	for _, tag := range []string{"w1", "w2"} {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for i := 0; i < rounds; i++ {
				f.keeper.publish(taggedRound(fmt.Sprintf("%s-%d", tag, i)))
			}
		}()
	}

	writers.Add(1)
	go func() {
		defer writers.Done()
		for i := 0; i < rounds; i++ {
			if _, err := f.svc.Today(ctx); err != nil {
				t.Errorf("Today: %v", err)
				return
			}
		}
	}()

	writers.Add(1)
	go func() {
		defer writers.Done()
		r := f.digestRunner(&sendingChannel{})
		for i := 0; i < rounds; i++ {
			if err := f.seedTrigger(fmt.Sprintf("trg-race-%d", i), fmt.Sprintf("k-race-%d", i)); err != nil {
				t.Errorf("seed trigger: %v", err)
				return
			}
			if _, err := r.assembleDigest(ctx, digestNow.AddDate(0, 0, i+1), true); err != nil {
				t.Errorf("digest %d: %v", i, err)
				return
			}
		}
	}()

	writers.Wait()
	close(stop)
	readers.Wait()

	if n := mixed.Load(); n != 0 {
		t.Fatalf("a reader observed %d snapshot(s) that mixed two writers' selections", n)
	}
	final := f.keeper.held.Load()
	if final == nil {
		t.Fatal("no incumbent after the writers finished")
	}
	if !consistent(final) {
		t.Fatalf("the final incumbent mixes two writers' selections: %+v", final.byKind)
	}
}

// failingFocusRow is a DecisionLog that refuses to record check.focus.unavailable.
type failingFocusRow struct{ ports.DecisionLog }

func (l failingFocusRow) Record(ctx context.Context, d ports.Decision) error {
	if d.Action == ports.ActionCheckFocusUnavailable {
		return errKeeperBoom
	}
	return l.DecisionLog.Record(ctx, d)
}

// TestDigest_FocusErrorRowWriteFailureIsReturned: the audit row is the one
// thing a failed focus must still write, so failing to write it fails the
// pass before anything is sent, as every other audit write does.
func TestDigest_FocusErrorRowWriteFailureIsReturned(t *testing.T) {
	f := digestFixture(t)
	f.cfgPort.fail = true
	ch := &sendingChannel{}
	r := f.digestRunner(ch)
	r.log = failingFocusRow{f.log}
	if _, err := r.assembleDigest(context.Background(), digestNow, true); !errors.Is(err, errKeeperBoom) {
		t.Fatalf("assembleDigest err = %v, want the audit-write error", err)
	}
	if ch.count() != 0 {
		t.Fatal("the digest was sent although its audit row could not be written")
	}
}
