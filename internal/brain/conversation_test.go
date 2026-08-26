package brain

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// recordingChannel records every conversation a send was addressed to.
type recordingChannel struct {
	to   []ports.ConversationID
	sent []string
}

func (c *recordingChannel) Name() string { return "recording" }
func (c *recordingChannel) Receive(context.Context) ([]ports.ChannelMessage, error) {
	return nil, nil
}
func (c *recordingChannel) Confirm(context.Context, string) error { return nil }
func (c *recordingChannel) Send(_ context.Context, to ports.ConversationID, text string) error {
	c.to = append(c.to, to)
	c.sent = append(c.sent, text)
	return nil
}
func (c *recordingChannel) Close() error { return nil }

// TestDelivery_AddressesTheVaultsOwnConversation is the destination half of
// a nudge, and it was missing entirely.
//
// A live vault logged this every five minutes, forever:
//
//	check.delivery_failed — telegram: sending to "": not a chat id:
//	strconv.ParseInt: parsing "": invalid syntax
//
// conversationFor was `func(ports.DueTrigger) ports.ConversationID { return "" }`
// — a stub whose own comment explains the model it was waiting for: "a
// trigger carries a unit id, not a chat id: nothing in the schema links a
// nudge to a conversation, because doc 02's model is one person and one
// brain". That model is right, and it means the destination belongs to the
// VAULT, not to the trigger. Nobody had given the vault one.
//
// So this is not a schema change. It is the one conversation, resolved
// once at wiring time and carried by the runner.
//
// Mutation: return "" again and every subtest fails.
// testConversation is the destination every delivery fixture speaks to. A
// scan wired with a channel needs one: doc 02's model is one person and one
// brain, and a runner with a channel and no conversation is a vault that
// cannot push, which is its own case below rather than the default.
const testConversation = ports.ConversationID("12449194")

func TestDelivery_AddressesTheVaultsOwnConversation(t *testing.T) {
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	const conv = testConversation

	t.Run("a pushed trigger goes to it", func(t *testing.T) {
		ch := &recordingChannel{}
		triggers := &deliveringTriggers{due: []ports.DueTrigger{pushTrigger("trg-1", now, 0.9)}}
		r := checkRunner{
			triggers: triggers, timers: &emptyTimers{}, ids: &countingIDs{},
			log: &recordingLog{}, channel: ch, conversation: conv,
		}

		if _, err := r.at(context.Background(), now, true); err != nil {
			t.Fatalf("at: %v", err)
		}
		if len(ch.to) != 1 {
			t.Fatalf("the channel received %d sends, want 1", len(ch.to))
		}
		if ch.to[0] != conv {
			t.Errorf("addressed to %q, want %q — an empty conversation is what the transport "+
				"rejects as \"not a chat id\", every pass, forever", ch.to[0], conv)
		}
	})
}

// TestDelivery_RefusesToGuessAmongSeveralConversations is the privacy half,
// and the reason resolution is not "take the first one".
//
// allowed_chat_ids is a list. A vault that allows a person AND a group has
// no single obvious destination, and picking one silently would send a
// dentist appointment to whichever the YAML happened to name first. Doc 02's
// model is one person and one brain, so a vault that cannot name one
// conversation has no proactive destination at all — it keeps capturing and
// answering, and it does not guess.
//
// Structural rather than a warning: with no conversation the send is never
// attempted, so there is no wrong chat to reach.
//
// Mutation: fall back to the first id and this fails.
func TestDelivery_RefusesToGuessAmongSeveralConversations(t *testing.T) {
	for _, tc := range []struct {
		name string
		ids  []int64
	}{
		{"none configured", nil},
		{"two configured", []int64{12449194, -100987654}},
		{"three configured", []int64{1, 2, 3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ProactiveConversation(tc.ids)
			if got != "" {
				t.Errorf("ProactiveConversation(%v) = %q, want empty — a vault that cannot name "+
					"one conversation must not pick one", tc.ids, got)
			}
		})
	}

	if got := ProactiveConversation([]int64{12449194}); got != "12449194" {
		t.Errorf("ProactiveConversation with exactly one id = %q, want it — one person, one "+
			"brain, one conversation", got)
	}
}

// TestDelivery_WithNoConversationRecordsWhyAndSendsNothing: a vault with no
// resolvable destination must not call Send at all. Calling it and letting
// the transport reject an empty id is what produced the five-minute failure
// loop; the reason a reader needs is "this vault has nowhere to push", not
// a parse error from strconv.
func TestDelivery_WithNoConversationRecordsWhyAndSendsNothing(t *testing.T) {
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	ch := &recordingChannel{}
	log := &recordingLog{}
	triggers := &deliveringTriggers{due: []ports.DueTrigger{pushTrigger("trg-1", now, 0.9)}}
	r := checkRunner{
		triggers: triggers, timers: &emptyTimers{}, ids: &countingIDs{},
		log: log, channel: ch, conversation: "",
	}

	report, err := r.at(context.Background(), now, true)
	if err != nil {
		t.Fatalf("at: %v", err)
	}
	if report.TriggersDelivered != 0 {
		t.Errorf("report claims %d trigger(s) delivered with nowhere to deliver to", report.TriggersDelivered)
	}
	if len(ch.sent) != 0 {
		t.Errorf("the channel was asked to send %d message(s) with no conversation: %q",
			len(ch.sent), ch.sent)
	}
	if !strings.Contains(strings.Join(log.rationales, "\n"), "conversation") {
		t.Errorf("no recorded reason mentions the conversation; a reader gets a transport "+
			"parse error instead of the actual cause: %q", log.rationales)
	}
}
