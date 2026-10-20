//go:build integration

package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/repocontract"
)

// TestConversationRepo_Contract runs the same repocontract.RunConversationRepo
// suite the in-memory fake answers at L2, against a real migrated vault.
func TestConversationRepo_Contract(t *testing.T) {
	repocontract.RunConversationRepo(t, func(t *testing.T) ports.ConversationRepo {
		return NewConversationRepo(openTestVault(t))
	})
}

// TestConversationRepo_TurnsSurviveReopeningTheVault: the point of storing
// the chat (ADR-0041). A turn appended before the vault closes is read after
// it opens again, as a restarted serve reads it.
func TestConversationRepo_TurnsSurviveReopeningTheVault(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "vault.db")
	v, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	turn := ports.ConversationTurn{Conversation: "42", At: triggerFixtureTime, Speaker: classify.SpeakerPush,
		Text: "Buen día.", UnitIDs: []string{"u-1"}}
	if err := NewConversationRepo(v).Append(ctx, turn); err != nil {
		t.Fatal(err)
	}
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	got, err := NewConversationRepo(reopened).Last(ctx, "42", classify.ConversationTurns)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Text != turn.Text || !got[0].At.Equal(turn.At) || len(got[0].UnitIDs) != 1 {
		t.Errorf("after reopening: got %+v, want the turn appended before", got)
	}
}
