package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/ports"
)

// ConversationRepo is the SQLite implementation of ports.ConversationRepo
// over conversation_turns (migration 0012, ADR-0041). It only inserts and
// selects: no statement here updates or deletes a turn.
type ConversationRepo struct {
	db *sql.DB
}

// NewConversationRepo returns a ports.ConversationRepo backed by v's
// already-migrated vault.
func NewConversationRepo(v *Vault) *ConversationRepo {
	return &ConversationRepo{db: v.db}
}

var _ ports.ConversationRepo = (*ConversationRepo)(nil)

// Append implements ports.ConversationRepo. seq is left to SQLite, so the
// row lands after every turn already stored.
func (r *ConversationRepo) Append(ctx context.Context, t ports.ConversationTurn) error {
	ids, err := json.Marshal(append([]string{}, t.UnitIDs...))
	if err != nil {
		return fmt.Errorf("encoding a turn's unit ids: %w", err)
	}
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO conversation_turns (conversation, said_at, speaker, text, unit_ids) VALUES (?, ?, ?, ?, ?)`,
		string(t.Conversation), formatUnitTime(t.At), string(t.Speaker), t.Text, string(ids)); err != nil {
		return fmt.Errorf("appending a turn to conversation %q: %w", t.Conversation, err)
	}
	return nil
}

// Last implements ports.ConversationRepo: the newest n by seq over
// idx_conversation_turns_chat, returned oldest first.
func (r *ConversationRepo) Last(ctx context.Context, id ports.ConversationID, n int) ([]ports.ConversationTurn, error) {
	if n < 1 {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT said_at, speaker, text, unit_ids FROM conversation_turns
WHERE conversation = ?
ORDER BY seq DESC
LIMIT ?`, string(id), n)
	if err != nil {
		return nil, fmt.Errorf("reading conversation %q: %w", id, err)
	}
	defer func() { _ = rows.Close() }()

	var out []ports.ConversationTurn
	for rows.Next() {
		var saidAt, speaker, ids string
		t := ports.ConversationTurn{Conversation: id}
		if err := rows.Scan(&saidAt, &speaker, &t.Text, &ids); err != nil {
			return nil, fmt.Errorf("scanning a turn of conversation %q: %w", id, err)
		}
		if t.At, err = time.Parse(unitTimeLayout, saidAt); err != nil {
			return nil, fmt.Errorf("a turn of conversation %q: said_at: %w", id, err)
		}
		if err := json.Unmarshal([]byte(ids), &t.UnitIDs); err != nil {
			return nil, fmt.Errorf("a turn of conversation %q: unit_ids: %w", id, err)
		}
		if len(t.UnitIDs) == 0 {
			t.UnitIDs = nil
		}
		t.Speaker = classify.Speaker(speaker)
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading conversation %q: %w", id, err)
	}
	slices.Reverse(out)
	return out, nil
}
