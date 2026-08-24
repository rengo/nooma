package brain

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
)

// sendingChannel records what was sent, and can be made to fail.
type sendingChannel struct {
	mu   sync.Mutex
	sent []string
	err  error
}

func (c *sendingChannel) Name() string { return "test" }
func (c *sendingChannel) Receive(context.Context) ([]ports.ChannelMessage, error) {
	return nil, nil
}
func (c *sendingChannel) Confirm(context.Context, string) error { return nil }
func (c *sendingChannel) Close() error                          { return nil }

func (c *sendingChannel) Send(_ context.Context, _ ports.ConversationID, text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	c.sent = append(c.sent, text)
	return nil
}

func (c *sendingChannel) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.sent)
}

// deliveringTriggers is a TriggerRepo that returns one due trigger and
// records the transitions applied to it.
type deliveringTriggers struct {
	emptyTriggers
	due        []ports.DueTrigger
	fired      []string
	surfaced   []string
	surfaceErr error
}

func (r *deliveringTriggers) Due(context.Context, time.Time) ([]ports.DueTrigger, error) {
	return r.due, nil
}

func (r *deliveringTriggers) Fire(_ context.Context, id string, _ time.Time) error {
	r.fired = append(r.fired, id)
	return nil
}

func (r *deliveringTriggers) Surface(_ context.Context, id string, _ time.Time) error {
	if r.surfaceErr != nil {
		return r.surfaceErr
	}
	r.surfaced = append(r.surfaced, id)
	return nil
}

func pushTrigger(id string, fireAt time.Time, level float64) ports.DueTrigger {
	return ports.DueTrigger{
		ID:             id,
		FireAt:         fireAt,
		InterruptLevel: &level,
		Payload:        ports.TriggerPayload{ActionText: "renew the passport"},
	}
}

// deliverableNow is a Wednesday mid-morning: past fireAt, inside no quiet
// window, and not yet stale.
var (
	deliverFireAt = time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	deliverNow    = deliverFireAt.Add(time.Minute)
)

// TestDeliver_APushRoutedTriggerIsSentAndMarked is R2.1.
func TestDeliver_APushRoutedTriggerIsSentAndMarked(t *testing.T) {
	triggers := &deliveringTriggers{due: []ports.DueTrigger{
		pushTrigger("trg-1", deliverFireAt, 0.9),
	}}
	ch := &sendingChannel{}
	log := &recordingLog{}

	report, err := checkRunner{triggers: triggers, timers: &emptyTimers{}, ids: &countingIDs{}, log: log, channel: ch}.
		at(context.Background(), deliverNow, true)
	if err != nil {
		t.Fatalf("at: %v", err)
	}

	if len(triggers.fired) != 1 || len(ch.sent) != 1 || len(triggers.surfaced) != 1 {
		t.Fatalf("fired %v, sent %d, surfaced %v — want one of each", triggers.fired, ch.count(), triggers.surfaced)
	}
	if report.TriggersFired != 1 || report.TriggersDelivered != 1 {
		t.Errorf("report = fired %d, delivered %d, want 1 and 1", report.TriggersFired, report.TriggersDelivered)
	}
	if got := log.count(ports.ActionCheckTriggerDelivered); got != 1 {
		t.Errorf("%d delivered rows, want 1", got)
	}
}

// TestDeliver_ADigestRoutedTriggerFiresAndWaits: it is not sent, and it is
// not marked — the digest finds it in Undelivered.
func TestDeliver_ADigestRoutedTriggerFiresAndWaits(t *testing.T) {
	triggers := &deliveringTriggers{due: []ports.DueTrigger{
		pushTrigger("trg-1", deliverFireAt, 0.2), // below PushThreshold
	}}
	ch := &sendingChannel{}

	report, err := checkRunner{triggers: triggers, timers: &emptyTimers{}, ids: &countingIDs{}, log: &recordingLog{}, channel: ch}.
		at(context.Background(), deliverNow, true)
	if err != nil {
		t.Fatalf("at: %v", err)
	}

	if len(triggers.fired) != 1 {
		t.Fatalf("fired %v, want one — a digest-routed trigger still fires", triggers.fired)
	}
	if ch.count() != 0 || len(triggers.surfaced) != 0 {
		t.Fatalf("sent %d and surfaced %v — a digest-routed trigger waits for the digest", ch.count(), triggers.surfaced)
	}
	if report.TriggersDelivered != 0 {
		t.Errorf("TriggersDelivered = %d, want 0", report.TriggersDelivered)
	}
}

// TestDeliver_AFailedSendLeavesItUndelivered is the ordering that makes
// the retry path work at all.
//
// Surface after Send, never before: a trigger the user never saw must not
// be recorded as delivered. Writing surfaced_at first would make the
// failure invisible — the row would claim a delivery and the next pass's
// Undelivered would never see it again.
func TestDeliver_AFailedSendLeavesItUndelivered(t *testing.T) {
	triggers := &deliveringTriggers{due: []ports.DueTrigger{
		pushTrigger("trg-1", deliverFireAt, 0.9),
	}}
	ch := &sendingChannel{err: errors.New("telegram is down")}
	log := &recordingLog{}

	report, err := checkRunner{triggers: triggers, timers: &emptyTimers{}, ids: &countingIDs{}, log: log, channel: ch}.
		at(context.Background(), deliverNow, true)
	if err != nil {
		t.Fatalf("at: %v — a transport failure must not fail the pass; every trigger behind this one would pay for it", err)
	}

	if len(triggers.surfaced) != 0 {
		t.Fatalf("surfaced %v after a failed send — the row now claims a delivery the user never saw, and Undelivered will never return it again", triggers.surfaced)
	}
	if got := log.count(ports.ActionCheckDeliveryFailed); got != 1 {
		t.Errorf("%d delivery-failed rows, want 1", got)
	}
	if report.TriggersDelivered != 0 {
		t.Errorf("TriggersDelivered = %d, want 0", report.TriggersDelivered)
	}
}

// TestDeliver_ASentButUnrecordedDeliveryIsLoud is the one state the
// ordering cannot make safe. If Surface fails after Send succeeded, the
// message is out and the row does not know — and a silent failure there
// means re-delivering the same nudge every pass, forever.
func TestDeliver_ASentButUnrecordedDeliveryIsLoud(t *testing.T) {
	triggers := &deliveringTriggers{
		due:        []ports.DueTrigger{pushTrigger("trg-1", deliverFireAt, 0.9)},
		surfaceErr: errors.New("the vault is closed"),
	}

	_, err := checkRunner{triggers: triggers, timers: &emptyTimers{}, ids: &countingIDs{}, log: &recordingLog{}, channel: &sendingChannel{}}.
		at(context.Background(), deliverNow, true)
	if err == nil {
		t.Fatal("at returned nil after a send that could not be recorded — the same nudge would be re-delivered every pass")
	}
}

// TestDeliver_NoChannelIsNotAFailure: `nooma check` on a vault with no
// Telegram still fires and expires. It simply has nowhere to speak.
func TestDeliver_NoChannelIsNotAFailure(t *testing.T) {
	triggers := &deliveringTriggers{due: []ports.DueTrigger{
		pushTrigger("trg-1", deliverFireAt, 0.9),
	}}

	report, err := checkRunner{triggers: triggers, timers: &emptyTimers{}, ids: &countingIDs{}, log: &recordingLog{}}.
		at(context.Background(), deliverNow, true)
	if err != nil {
		t.Fatalf("at with no channel: %v", err)
	}
	if report.TriggersFired != 1 {
		t.Errorf("TriggersFired = %d, want 1 — the pass still does its work", report.TriggersFired)
	}
	if len(triggers.surfaced) != 0 {
		t.Errorf("surfaced %v with no channel — nothing reached the user", triggers.surfaced)
	}
}

// TestDeliver_DryRunSendsNothing: Q1's ruling applied to a new effect. A
// preview that messaged the user would be a preview of nothing.
func TestDeliver_DryRunSendsNothing(t *testing.T) {
	triggers := &deliveringTriggers{due: []ports.DueTrigger{
		pushTrigger("trg-1", deliverFireAt, 0.9),
	}}
	ch := &sendingChannel{}
	log := &recordingLog{}

	report, err := checkRunner{triggers: triggers, timers: &emptyTimers{}, ids: &countingIDs{}, log: log, channel: ch}.
		at(context.Background(), deliverNow, false)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}

	if ch.count() != 0 || len(triggers.fired) != 0 || len(log.actions) != 0 {
		t.Fatalf("a dry run sent %d, fired %v and wrote %v — it must report and change nothing", ch.count(), triggers.fired, log.actions)
	}
	if report.TriggersFired != 1 || report.TriggersDelivered != 1 {
		t.Errorf("the dry run reported fired %d delivered %d, want 1 and 1 — it must reach the same verdict it would have acted on",
			report.TriggersFired, report.TriggersDelivered)
	}
}

// TestRenderPush_SaysWhenItIsLate is R4.2's half that belongs to a push.
func TestRenderPush_SaysWhenItIsLate(t *testing.T) {
	t.Run("on time, no caveat", func(t *testing.T) {
		got := renderPush(pushTrigger("t", deliverFireAt, 0.9), deliverFireAt.Add(time.Minute))
		if got != "renew the passport" {
			t.Errorf("render = %q, want the text alone", got)
		}
	})

	t.Run("late enough, the caveat appears", func(t *testing.T) {
		late := deliverFireAt.Add(time.Duration(prospection.DelayCaveatMinutes) * time.Minute)
		got := renderPush(pushTrigger("t", deliverFireAt, 0.9), late)
		if got == "renew the passport" {
			t.Errorf("render = %q at exactly the caveat boundary, want the delay mentioned — DelayCaveat is inclusive by m3a's F6", got)
		}
	})
}
