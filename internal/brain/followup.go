package brain

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// Conversation is a chat's recent turns, read from and appended to the vault
// (ADR-0041): what the user said, what Nooma answered, and what it pushed. A
// restart keeps it. Nil is legal everywhere and holds nothing.
type Conversation struct {
	turns ports.ConversationRepo
}

// Turn is one turn of a chat: when, who, what, and the units it concerned.
type Turn struct {
	At      time.Time
	Speaker classify.Speaker
	Text    string
	UnitIDs []string
}

// NewConversation returns the chat log stored in turns.
func NewConversation(turns ports.ConversationRepo) *Conversation {
	return &Conversation{turns: turns}
}

// Add appends t to the chat id. A turn that cannot be stored is warned about
// and dropped rather than failing what was already said: it costs a later
// follow-up being read without it.
func (c *Conversation) Add(ctx context.Context, id ports.ConversationID, t Turn) {
	if c == nil || id == "" {
		return
	}
	if err := c.turns.Append(ctx, ports.ConversationTurn{Conversation: id, At: t.At, Speaker: t.Speaker, Text: t.Text, UnitIDs: t.UnitIDs}); err != nil {
		slog.Warn("a turn of the chat could not be stored, so a follow-up to it is read without it", "error", err)
	}
}

// Recent is the chat id's last classify.ConversationTurns turns, oldest
// first, whatever their age: the model sees when each was said and the items
// as they stand now (ADR-0041). A chat that cannot be read is warned about
// and the message is read alone.
func (c *Conversation) Recent(ctx context.Context, id ports.ConversationID) []Turn {
	if c == nil || id == "" {
		return nil
	}
	stored, err := c.turns.Last(ctx, id, classify.ConversationTurns)
	if err != nil {
		slog.Warn("the chat could not be read, so the message is read alone", "error", err)
		return nil
	}
	turns := make([]Turn, len(stored))
	for i, t := range stored {
		turns[i] = Turn{At: t.At, Speaker: t.Speaker, Text: t.Text, UnitIDs: t.UnitIDs}
	}
	return turns
}

// WithConversation lets capture read a chat message in its recent
// conversation (ADR-0036). Without it every message is read alone.
func (s *CaptureService) WithConversation(c *Conversation) *CaptureService {
	s.run.conv = c
	return s
}

// conversationFor builds what the prompt shows for a chat message: the turns,
// and the live units they concern as they stand now, lettered in the order
// they first appear. When the turns name more live units than
// classify.MaxConversationItems, the ones the newest turns name are lettered,
// so the exchange a follow-up answers is always one it can bind. shown maps a
// position to its unit id. A unit that cannot be read is left out rather than
// failing a capture the user is waiting on.
func (r captureRunner) conversationFor(ctx context.Context, in CaptureInput, now time.Time) (classify.Conversation, []string) {
	turns := r.conv.Recent(ctx, in.Conversation)
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
	newest := map[string]bool{}
	for i := len(turns) - 1; i >= 0 && len(newest) < classify.MaxConversationItems; i-- {
		for _, id := range turns[i].UnitIDs {
			if _, ok := byID[id]; ok && len(newest) < classify.MaxConversationItems {
				newest[id] = true
			}
		}
	}
	var conv classify.Conversation
	var shown []string
	for _, id := range ids {
		u := byID[id]
		if !newest[id] {
			continue
		}
		shown = append(shown, id)
		conv.Items = append(conv.Items, classify.ConversationItem{Content: u.Content, Title: u.Title, EventAt: u.EventAt, DueAt: u.DueAt})
	}
	for _, t := range turns {
		ct := classify.Turn{At: t.At, Speaker: t.Speaker, Text: t.Text}
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
