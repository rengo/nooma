package brain

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/focus"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

// TestDeliver_APushIsATurnOfTheChat: a reminder Nooma pushed is a turn about
// its unit, so "no, el jueves" sent back corrects it (ADR-0036). A failed
// send is no turn: the user never saw it.
func TestDeliver_APushIsATurnOfTheChat(t *testing.T) {
	unitID := "dentist"
	push := pushTrigger("trg-1", deliverFireAt, 0.9)
	push.UnitID = &unitID
	for _, failing := range []bool{false, true} {
		conv := NewConversation()
		ch := &sendingChannel{}
		if failing {
			ch.err = context.DeadlineExceeded
		}
		_, err := checkRunner{triggers: &deliveringTriggers{due: []ports.DueTrigger{push}}, timers: &emptyTimers{}, ids: &countingIDs{},
			log: &recordingLog{}, channel: ch, conversation: testConversation, conv: conv}.at(context.Background(), deliverNow, true)
		if err != nil {
			t.Fatal(err)
		}
		got := conv.Recent(testConversation, deliverNow)
		if failing {
			if len(got) != 0 {
				t.Errorf("a push that failed to send is a turn: %+v", got)
			}
			continue
		}
		want := []Turn{{At: deliverNow, Speaker: classify.SpeakerPush, Text: ch.sent[0], UnitIDs: []string{unitID}}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("turns = %+v, want %+v", got, want)
		}
	}
}

// TestConversation_KeepsTheLastTurnsPerChat: each chat has its own turns,
// and no more than a prompt shows are kept; a nil log holds nothing.
func TestConversation_KeepsTheLastTurnsPerChat(t *testing.T) {
	now := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	c := NewConversation()
	for i := range classify.ConversationTurns + 2 {
		c.Add("a", Turn{At: now, Text: string(rune('0' + i))})
	}
	c.Add("b", Turn{At: now, Text: "other"})
	got := c.Recent("a", now)
	if len(got) != classify.ConversationTurns || got[0].Text != "2" {
		t.Errorf("chat a holds %+v, want the last %d", got, classify.ConversationTurns)
	}
	if b := c.Recent("b", now); len(b) != 1 || b[0].Text != "other" {
		t.Errorf("chat b holds %+v", b)
	}
	var none *Conversation
	none.Add("a", Turn{At: now})
	if got := none.Recent("a", now); got != nil {
		t.Errorf("a nil log holds %+v", got)
	}
}

// TestDigest_IsATurnAboutTheUnitsItNamed: a reply to the morning digest can
// correct what it carried (ADR-0036).
func TestDigest_IsATurnAboutTheUnitsItNamed(t *testing.T) {
	triggers := &undeliveredTriggers{pending: []ports.DueTrigger{
		digestTrigger("trg-1", "u-1", "renew the passport"), digestTrigger("trg-2", "u-2", "Ana's birthday"),
	}}
	units := &digestUnits{byID: map[string]focus.Candidate{"u-1": {ID: "u-1", Weight: 1}, "u-2": {ID: "u-2", Weight: 1}}}
	ch := &sendingChannel{}
	r := digestRunner(t, triggers, units, memrepo.NewState(), memrepo.NewDecisionLog(), ch)
	r.conv = NewConversation()
	if _, _, err := r.assembleDigest(context.Background(), digestNow, true); err != nil {
		t.Fatal(err)
	}
	got := r.conv.Recent(testConversation, digestNow)
	if len(got) != 1 || got[0].Speaker != classify.SpeakerPush || got[0].Text != ch.sent[0] || !reflect.DeepEqual(got[0].UnitIDs, []string{"u-1", "u-2"}) {
		t.Errorf("turns = %+v, want the digest about u-1 and u-2", got)
	}
}
