package ports

import "context"

// ConversationID, ChannelMessage and Channel are the red step's minimal
// declarations (PR 1, commit 1): enough for the contract suite to compile,
// nothing more. The fake answers with zero values, so every substantive
// assertion fails for its own reason.
type ConversationID string

// ChannelMessage is one inbound message.
type ChannelMessage struct {
	ID           string
	Conversation ConversationID
	Text         string
	Channel      string
}

// Channel is the port over a conversation surface.
type Channel interface {
	Name() string
	Receive(ctx context.Context) ([]ChannelMessage, error)
	Confirm(ctx context.Context, id string) error
	Send(ctx context.Context, conversation ConversationID, text string) error
	Close() error
}
