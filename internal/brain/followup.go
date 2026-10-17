package brain

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// Conversation is the recent chat, per conversation, held in memory only
// (ADR-0036): what the user said, what Nooma answered, and what it pushed.
// A restart forgets it, which costs one follow-up being read alone, as every
// message was before. Nil is legal everywhere and holds nothing.
type Conversation struct {
	mu    sync.Mutex
	turns map[ports.ConversationID][]Turn
}

// Turn is one turn of a chat: when, who, what, and the units it concerned.
type Turn struct {
	At      time.Time
	Speaker classify.Speaker
	Text    string
	UnitIDs []string
}

// NewConversation returns an empty log.
func NewConversation() *Conversation {
	return &Conversation{turns: map[ports.ConversationID][]Turn{}}
}

// Add appends t to the chat id, keeping no more turns than a prompt shows.
func (c *Conversation) Add(id ports.ConversationID, t Turn) {
	if c == nil || id == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	turns := append(c.turns[id], t)
	c.turns[id] = turns[max(0, len(turns)-classify.ConversationTurns):]
}

// Recent is the chat id's turns inside the window at now, oldest first.
func (c *Conversation) Recent(id ports.ConversationID, now time.Time) []Turn {
	if c == nil || id == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	turns := c.turns[id]
	ats := make([]time.Time, len(turns))
	for i, t := range turns {
		ats[i] = t.At
	}
	return slices.Clone(turns[classify.RecentFrom(ats, now):])
}

// WithConversation lets capture read a chat message in its recent
// conversation (ADR-0036). Without it every message is read alone.
func (s *CaptureService) WithConversation(c *Conversation) *CaptureService {
	s.run.conv = c
	return s
}

// conversationFor builds what the prompt shows for a chat message: the turns,
// and the live units they concern as they stand now, lettered in the order
// they first appear. shown maps a position to its unit id. A unit that cannot
// be read is left out rather than failing a capture the user is waiting on.
func (r captureRunner) conversationFor(ctx context.Context, in CaptureInput, now time.Time) (classify.Conversation, []string) {
	turns := r.conv.Recent(in.Conversation, now)
	if len(turns) == 0 || in.ReferentID != "" {
		return classify.Conversation{}, nil
	}
	var ids []string
	for _, t := range turns {
		for _, id := range t.UnitIDs {
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
	}
	live, err := r.units.LiveByIDs(ctx, ids)
	if err != nil {
		live = nil
	}
	byID := make(map[string]unit.Unit, len(live))
	for _, u := range live {
		byID[u.ID] = u
	}
	var conv classify.Conversation
	var shown []string
	for _, id := range ids {
		u, ok := byID[id]
		if !ok || len(shown) == classify.MaxConversationItems {
			continue
		}
		shown = append(shown, id)
		conv.Items = append(conv.Items, classify.ConversationItem{Content: u.Content, Title: u.Title, EventAt: u.EventAt, DueAt: u.DueAt})
	}
	for _, t := range turns {
		ct := classify.Turn{Speaker: t.Speaker, Text: t.Text}
		for _, id := range t.UnitIDs {
			if pos := slices.Index(shown, id); pos >= 0 {
				ct.About = append(ct.About, pos+1)
			}
		}
		conv.Turns = append(conv.Turns, ct)
	}
	return conv, shown
}

// askWhich sends the model's own question when it could not tell which item
// a follow-up is about, and stores nothing (ADR-0036). The ask is in the
// trail with the items it was shown.
func (r captureRunner) askWhich(ctx context.Context, question string, shown []string, now time.Time) (CaptureResult, error) {
	if err := r.correction.recordAmbiguousDecision(ctx, now, struct {
		Reason   AskReason `json:"reason"`
		Shown    []string  `json:"shown"`
		Question string    `json:"question"`
	}{Reason: AskReferentUnclear, Shown: shown, Question: question}); err != nil {
		return CaptureResult{}, err
	}
	return CaptureResult{Outcome: OutcomeAsked, Reply: question, Correction: &Correction{Ambiguous: true, Why: AskReferentUnclear}}, nil
}

// ConcernedUnits is the unit a capture's result is about, for its turn in
// the conversation: the one it stored or corrected, or none.
func (r CaptureResult) ConcernedUnits() []string {
	switch {
	case r.UnitID != "":
		return []string{r.UnitID}
	case r.Correction != nil && r.Correction.UnitID != "":
		return []string{r.Correction.UnitID}
	}
	return nil
}
