package memrepo

import (
	"context"
	"slices"
	"sync"

	"github.com/rengo/nooma/internal/ports"
)

// Conversations is an in-memory ports.ConversationRepo: every turn ever
// appended, in append order. Two instances share nothing; two readers of
// one instance share the turns, as two brains over one vault do.
type Conversations struct {
	mu    sync.Mutex
	turns []ports.ConversationTurn
}

var _ ports.ConversationRepo = (*Conversations)(nil)

// NewConversations returns an empty in-memory ports.ConversationRepo.
func NewConversations() *Conversations { return &Conversations{} }

// Append implements ports.ConversationRepo.
func (r *Conversations) Append(_ context.Context, t ports.ConversationTurn) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	t.UnitIDs = slices.Clone(t.UnitIDs)
	r.turns = append(r.turns, t)
	return nil
}

// Last implements ports.ConversationRepo.
func (r *Conversations) Last(_ context.Context, id ports.ConversationID, n int) ([]ports.ConversationTurn, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []ports.ConversationTurn
	for i := len(r.turns) - 1; i >= 0 && len(out) < n; i-- {
		if t := r.turns[i]; t.Conversation == id {
			t.UnitIDs = slices.Clone(t.UnitIDs)
			out = append(out, t)
		}
	}
	slices.Reverse(out)
	return out, nil
}
