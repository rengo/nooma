// Package repocontract — see unitrepo.go for the package contract.
package repocontract

import (
	"context"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/ports"
)

// RunConversationRepo runs the ports.ConversationRepo contract (ADR-0041)
// against a fresh repository built by newRepo for every subtest, holding no
// turn yet. Instants are whole seconds, the precision the vault stores.
func RunConversationRepo(t *testing.T, newRepo func(t *testing.T) ports.ConversationRepo) {
	t.Helper()

	t.Run("the port declares no removal method", func(t *testing.T) {
		assertNoRemovalMethod(t, reflect.TypeOf((*ports.ConversationRepo)(nil)).Elem())
	})

	t.Run("Last returns a turn as it was appended", func(t *testing.T) {
		repo := newRepo(t)
		want := ports.ConversationTurn{Conversation: "42", At: contractNow, Speaker: classify.SpeakerPush,
			Text: "Buen día.\nHoy vence: Renovar el pasaporte", UnitIDs: []string{"u-1", "u-2"}}
		appendTurns(t, repo, want)

		got := lastTurns(t, repo, "42", 10)
		if len(got) != 1 {
			t.Fatalf("Last: got %d turns, want 1", len(got))
		}
		if !got[0].At.Equal(want.At) {
			t.Errorf("At: got %s, want %s", got[0].At, want.At)
		}
		got[0].At = want.At
		if !reflect.DeepEqual(got[0], want) {
			t.Errorf("Last: got %+v, want %+v", got[0], want)
		}
	})

	t.Run("a turn about no unit reads back with none", func(t *testing.T) {
		repo := newRepo(t)
		appendTurns(t, repo, ports.ConversationTurn{Conversation: "42", At: contractNow, Speaker: classify.SpeakerUser, Text: "hola"})
		if got := lastTurns(t, repo, "42", 10); len(got) != 1 || len(got[0].UnitIDs) != 0 {
			t.Errorf("Last: got %+v, want one turn about no unit", got)
		}
	})

	t.Run("Last keeps the last n in append order, at any age", func(t *testing.T) {
		repo := newRepo(t)
		// Appended out of time order, and a year apart: the order is the
		// append order, and age bounds nothing.
		for i := range 5 {
			appendTurns(t, repo, ports.ConversationTurn{Conversation: "42", At: contractNow.Add(-time.Duration(i) * 24 * 365 * time.Hour),
				Speaker: classify.SpeakerUser, Text: strconv.Itoa(i)})
		}
		got := lastTurns(t, repo, "42", 3)
		if texts := turnTexts(got); !reflect.DeepEqual(texts, []string{"2", "3", "4"}) {
			t.Errorf("Last(3): got %v, want [2 3 4], the last three appended, oldest first", texts)
		}
		if texts := turnTexts(lastTurns(t, repo, "42", 10)); len(texts) != 5 {
			t.Errorf("Last(10) over five turns: got %v, want all five", texts)
		}
	})

	t.Run("Last of one conversation never shows another's", func(t *testing.T) {
		repo := newRepo(t)
		appendTurns(t, repo,
			ports.ConversationTurn{Conversation: "a", At: contractNow, Speaker: classify.SpeakerUser, Text: "a1"},
			ports.ConversationTurn{Conversation: "b", At: contractNow, Speaker: classify.SpeakerUser, Text: "b1"},
			ports.ConversationTurn{Conversation: "a", At: contractNow, Speaker: classify.SpeakerNooma, Text: "a2"},
		)
		if texts := turnTexts(lastTurns(t, repo, "a", 10)); !reflect.DeepEqual(texts, []string{"a1", "a2"}) {
			t.Errorf("Last(a): got %v, want [a1 a2]", texts)
		}
		if got := lastTurns(t, repo, "c", 10); len(got) != 0 {
			t.Errorf("Last of a conversation with no turn: got %+v, want none", got)
		}
	})

	t.Run("Last below one returns none", func(t *testing.T) {
		repo := newRepo(t)
		appendTurns(t, repo, ports.ConversationTurn{Conversation: "42", At: contractNow, Speaker: classify.SpeakerUser, Text: "x"})
		for _, n := range []int{0, -1} {
			if got := lastTurns(t, repo, "42", n); len(got) != 0 {
				t.Errorf("Last(%d): got %+v, want none", n, got)
			}
		}
	})

	t.Run("a returned turn is a copy", func(t *testing.T) {
		repo := newRepo(t)
		ids := []string{"u-1"}
		appendTurns(t, repo, ports.ConversationTurn{Conversation: "42", At: contractNow, Speaker: classify.SpeakerUser, Text: "x", UnitIDs: ids})
		ids[0] = "changed by the caller"
		got := lastTurns(t, repo, "42", 1)
		if len(got) != 1 || len(got[0].UnitIDs) != 1 {
			t.Fatalf("Last: got %+v, want one turn about u-1", got)
		}
		got[0].UnitIDs[0] = "changed by the reader"
		if again := lastTurns(t, repo, "42", 1); again[0].UnitIDs[0] != "u-1" {
			t.Errorf("UnitIDs: got %v, want [u-1] — a caller's slice reached the store", again[0].UnitIDs)
		}
	})
}

func appendTurns(t *testing.T, repo ports.ConversationRepo, turns ...ports.ConversationTurn) {
	t.Helper()
	for _, turn := range turns {
		if err := repo.Append(context.Background(), turn); err != nil {
			t.Fatalf("Append(%q): %v", turn.Text, err)
		}
	}
}

func lastTurns(t *testing.T, repo ports.ConversationRepo, id ports.ConversationID, n int) []ports.ConversationTurn {
	t.Helper()
	got, err := repo.Last(context.Background(), id, n)
	if err != nil {
		t.Fatalf("Last(%q, %d): %v", id, n, err)
	}
	return got
}

func turnTexts(turns []ports.ConversationTurn) []string {
	out := make([]string, len(turns))
	for i, turn := range turns {
		out[i] = turn.Text
	}
	return out
}
