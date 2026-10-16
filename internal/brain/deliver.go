package brain

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/rengo/nooma/internal/core/phrase"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
)

// fireAndDeliver moves one due trigger armed -> fired and, when it routes
// to push (triggerRoute), queues it for the pass's pushes (pushQueue). A real
// delivery happens when the queue is flushed; delivered is a dry run's preview.
//
// **The two counts differ on purpose.** Firing is a decision this pass
// made; delivering is an outcome it may not have achieved. A digest-routed
// trigger fires and is not delivered — it waits for the digest. A
// push-routed one whose send fails fires and is not delivered either, and
// a later pass picks it up from Undelivered.
//
// The ordering is the whole of R2.1, kept by deliver: **Send first, Surface second.** A
// trigger the user never saw must not be recorded as delivered, and m3b
// left surfaced_at NULL precisely so this could be the thing that fills
// it. Writing it first would make the failure invisible — the row would
// claim a delivery, and the retry path would never see it again.
func (r checkRunner) fireAndDeliver(ctx context.Context, t ports.DueTrigger, now time.Time, commit bool, q *pushQueue) (fired, delivered int, err error) {
	if !commit {
		// A dry run reaches the same verdict and reports the same intent.
		// It does not send: a preview that messaged the user would be a
		// preview of nothing (m3b's Q1 ruling, applied to a new effect).
		if triggerRoute(t) == prospection.RoutePush {
			return 1, 1, nil
		}
		return 1, 0, nil
	}

	if err := r.triggers.Fire(ctx, t.ID, now); err != nil {
		skipped, skipErr := r.skipOnConflict(ctx, now, err, ports.ErrTriggerStatusConflict, "triggers", t.ID, t.FireAt)
		if skipErr != nil {
			return 0, 0, skipErr
		}
		if skipped {
			return 0, 0, nil
		}
		return 0, 0, fmt.Errorf("check: fire trigger %q: %w", t.ID, err)
	}

	route := triggerRoute(t)
	if err := r.record(ctx, now, ports.ActionCheckTriggerFired,
		fmt.Sprintf("trigger %q came due and fired, routed to %s", t.ID, route),
		checkDetail{ID: t.ID, FireAt: t.FireAt.UTC().Format(time.RFC3339), Verdict: string(prospection.VerdictDeliver)},
	); err != nil {
		return 0, 0, err
	}

	if route != prospection.RoutePush {
		// The digest's, and it is already in Undelivered. Nothing more to
		// do here, and no second row: waiting is not an effect.
		return 1, 0, nil
	}

	q.add(t)
	return 1, 0, nil
}

// triggerRoute is where a due trigger goes: an event reminder (one-shot) is
// pushed, a recurring one follows its interrupt level (ADR-0030 point 1).
func triggerRoute(t ports.DueTrigger) prospection.Route {
	return prospection.RouteTrigger(t.RecurrenceRule != nil, prospection.ResolveInterrupt(t.InterruptLevel))
}

// deliver sends the reminders of one unit as one message and records that it
// reached the user, and reports how many reminders it delivered. A recorded
// failure is not an error the pass should stop for — and returning only an
// error would make a failed send indistinguishable from a successful one to the
// caller counting deliveries.
//
// A send failure is recorded and swallowed: the triggers stay fired with
// surfaced_at NULL, so the next pass finds them in Undelivered and tries
// again. Failing the pass would cost every trigger behind this one for a
// transport problem that is usually momentary — the same posture
// skipOnConflict takes toward a lost race.
func (r checkRunner) deliver(ctx context.Context, group []ports.DueTrigger, now time.Time) (int, error) {
	if r.channel == nil {
		// No channel configured. Not an error and not a failure to
		// record: a vault with no channel has nowhere to speak, and
		// saying so once per trigger would fill the glass box with the
		// same fact.
		return 0, nil
	}

	conversation := r.conversationFor(group[0])
	if conversation == "" {
		// A channel with nowhere to address. Recorded rather than silent,
		// because unlike a missing channel this is a config the user
		// meant to work: they enabled Telegram and the vault still cannot
		// name one conversation to push to. Sending anyway is what
		// produced a five-minute loop reporting a strconv parse error,
		// which names the symptom and hides the cause.
		for _, t := range group {
			if err := r.record(ctx, now, ports.ActionCheckDeliveryFailed,
				fmt.Sprintf("trigger %q fired but this vault has no conversation to push to; "+
					"allowed_chat_ids must name exactly one for a nudge to have a destination", t.ID),
				checkDetail{ID: t.ID, FireAt: t.FireAt.UTC().Format(time.RFC3339)}); err != nil {
				return 0, err
			}
		}
		return 0, nil
	}

	if err := r.channel.Send(ctx, conversation, r.pushText(ctx, group, now)); err != nil {
		for _, t := range group {
			if !r.outage.first(t.ID) {
				// Said already in this outage; the retry each pass makes is not news.
				continue
			}
			if recErr := r.record(ctx, now, ports.ActionCheckDeliveryFailed,
				fmt.Sprintf("trigger %q fired but could not be delivered; it stays undelivered and a later pass will try again: %v", t.ID, err),
				checkDetail{ID: t.ID, FireAt: t.FireAt.UTC().Format(time.RFC3339)}); recErr != nil {
				return 0, recErr
			}
		}
		return 0, nil
	}

	r.outage.clear()

	// Every row went out in this one message, so each is marked even when an
	// earlier one could not be: a row left unmarked is resent by a restart.
	delivered := 0
	var failed error
	for _, t := range group {
		if err := r.triggers.Surface(ctx, t.ID, r.stamp(now)); err != nil {
			// Another pass moved the row on first: its business, not a failure.
			skipped, skipErr := r.skipOnConflict(ctx, now, err, ports.ErrTriggerStatusConflict, "triggers", t.ID, t.FireAt)
			if skipErr != nil {
				return delivered, skipErr
			}
			if skipped {
				continue
			}
			// The message went out and the row could not record it. Loud:
			// this is the one state the ordering cannot make safe. The retry
			// would send it again every pass, so the process remembers it and
			// never does; a restart, which forgets, sends it once more.
			r.unmarked.first(t.ID)
			if failed == nil {
				failed = fmt.Errorf("check: trigger %q was sent but not marked delivered: %w", t.ID, err)
			}
			continue
		}
		delivered++
		if err := r.record(ctx, now, ports.ActionCheckTriggerDelivered,
			fmt.Sprintf("trigger %q was delivered", t.ID),
			checkDetail{ID: t.ID, FireAt: t.FireAt.UTC().Format(time.RFC3339)}); err != nil {
			return delivered, err
		}
	}
	return delivered, failed
}

// conversationFor is where a trigger's delivery goes.
//
// One conversation, resolved once at wiring time and carried by the runner
// — a trigger carries a unit id, not a chat id, because doc 02's model is
// one person and one brain. It stays a method taking a trigger rather than
// a bare field read: that is the seam a multi-conversation vault would
// widen, and the argument is what such a vault would dispatch on.
//
// Empty means this vault has no proactive destination, which callers check
// BEFORE sending. It returned the empty string unconditionally until a live
// vault spent five minutes at a time asking a transport to parse "" as a
// chat id.
func (r checkRunner) conversationFor(ports.DueTrigger) ports.ConversationID {
	return r.conversation
}

// ProactiveConversation resolves a vault's one push destination from its
// allowed chat ids, or nothing.
//
// **Exactly one, or none.** allowed_chat_ids is a list, and a vault that
// allows a person AND a group has no single obvious destination; taking the
// first would send a private nudge to whichever the YAML happened to name
// first. Doc 02's model is one person and one brain, so a vault that cannot
// name one conversation has no proactive destination — it keeps capturing
// and answering, and it does not guess. Non-negotiable #7: the safety is
// structural, because with no conversation the send is never attempted.
func ProactiveConversation(allowedChatIDs []int64) ports.ConversationID {
	if len(allowedChatIDs) != 1 {
		return ""
	}
	return ports.ConversationID(strconv.FormatInt(allowedChatIDs[0], 10))
}

// renderPush is what a pushed trigger says when its unit cannot be read.
func renderPush(t ports.DueTrigger, now time.Time, set phrase.Set) string {
	return withDelayCaveat(frozenText(t, set), t.FireAt, now, set)
}

func frozenText(t ports.DueTrigger, set phrase.Set) string {
	if t.Payload.ActionText == "" {
		return set.SomethingYouAsked
	}
	return clip(t.Payload.ActionText)
}

func withDelayCaveat(text string, fireAt, now time.Time, set phrase.Set) string {
	if prospection.DelayCaveat(now.Sub(fireAt)) {
		return set.Delayed(text)
	}
	return text
}

// stamp is the pass's next surfaced_at; a runner built without a pass (a test
// calling one step directly) stamps now.
func (r checkRunner) stamp(now time.Time) time.Time {
	if r.stamps == nil {
		return now
	}
	return r.stamps.take(now)
}
