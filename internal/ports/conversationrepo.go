package ports

import (
	"context"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
)

// ConversationTurn is one stored turn of a chat (ADR-0041): when, who, what,
// and the units it concerned.
type ConversationTurn struct {
	Conversation ConversationID
	At           time.Time
	Speaker      classify.Speaker
	Text         string
	UnitIDs      []string
}

// ConversationRepo is the chat's turns in the vault, over
// conversation_turns (migration 0012). It only appends: a turn is never
// changed or removed (I03), and the read takes the last turns rather than
// pruning the older ones.
type ConversationRepo interface {
	// Append stores t after every turn already stored for its conversation.
	Append(ctx context.Context, t ConversationTurn) error
	// Last returns the conversation's last n turns in the order they were
	// appended, oldest first, whatever their age. n below 1 returns none.
	Last(ctx context.Context, conversation ConversationID, n int) ([]ConversationTurn, error)
}
