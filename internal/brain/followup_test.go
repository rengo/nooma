package brain

import (
	"context"
	"errors"
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
		conv := NewConversation(memrepo.NewConversations())
		ch := &sendingChannel{}
		if failing {
			ch.err = context.DeadlineExceeded
		}
		_, err := checkRunner{triggers: &deliveringTriggers{due: []ports.DueTrigger{push}}, timers: &emptyTimers{}, ids: &countingIDs{},
			log: &recordingLog{}, channel: ch, conversation: testConversation, conv: conv}.at(context.Background(), deliverNow, true)
		if err != nil {
			t.Fatal(err)
		}
		got := conv.Recent(context.Background(), testConversation)
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

// TestConversation_ShowsTheLastTurnsPerChat: each chat has its own turns,
// and a prompt is shown no more than its last ConversationTurns, however old;
// a nil log holds nothing.
func TestConversation_ShowsTheLastTurnsPerChat(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	c := NewConversation(memrepo.NewConversations())
	for i := range classify.ConversationTurns + 2 {
		c.Add(ctx, "a", Turn{At: now.Add(-time.Duration(i) * 24 * time.Hour), Text: string(rune('0' + i))})
	}
	c.Add(ctx, "b", Turn{At: now, Text: "other"})
	got := c.Recent(ctx, "a")
	if len(got) != classify.ConversationTurns || got[0].Text != "2" {
		t.Errorf("chat a shows %+v, want the last %d", got, classify.ConversationTurns)
	}
	if b := c.Recent(ctx, "b"); len(b) != 1 || b[0].Text != "other" {
		t.Errorf("chat b shows %+v", b)
	}
	var none *Conversation
	none.Add(ctx, "a", Turn{At: now})
	if got := none.Recent(ctx, "a"); got != nil {
		t.Errorf("a nil log holds %+v", got)
	}
}

// brokenTurns is a vault whose chat can be neither written nor read.
type brokenTurns struct{}

func (brokenTurns) Append(context.Context, ports.ConversationTurn) error {
	return errors.New("disk I/O error")
}

func (brokenTurns) Last(context.Context, ports.ConversationID, int) ([]ports.ConversationTurn, error) {
	return nil, errors.New("disk I/O error")
}

// TestConversation_AnUnreadableChatIsReadAlone: a chat the vault cannot
// write or read fails nothing already said; the message is read alone.
func TestConversation_AnUnreadableChatIsReadAlone(t *testing.T) {
	c := NewConversation(brokenTurns{})
	c.Add(context.Background(), "a", Turn{Text: "x"})
	if got := c.Recent(context.Background(), "a"); got != nil {
		t.Errorf("an unreadable chat shows %+v, want none", got)
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
	r.conv = NewConversation(memrepo.NewConversations())
	if _, _, err := r.assembleDigest(context.Background(), digestNow, true); err != nil {
		t.Fatal(err)
	}
	got := r.conv.Recent(context.Background(), testConversation)
	if len(got) != 1 || got[0].Speaker != classify.SpeakerPush || got[0].Text != ch.sent[0] || !reflect.DeepEqual(got[0].UnitIDs, []string{"u-1", "u-2"}) {
		t.Errorf("turns = %+v, want the digest about u-1 and u-2", got)
	}
}

// TestNewConversation_RefusesNoStore: a chat log with no store would read
// every message alone and forget every turn, silently. Wiring it so is a bug
// the process must not start with; a nil *Conversation is the legal "no chat".
func TestNewConversation_RefusesNoStore(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("NewConversation(nil) returned a log; want a panic naming the missing store")
		}
	}()
	NewConversation(nil)
}
