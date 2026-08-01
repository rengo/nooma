package repocontract

import (
	"context"
	"testing"

	"github.com/rengo/nooma/internal/ports"
)

// LexicalSeeder is what a LexicalSearch implementation must offer the
// contract so the suite can put documents in front of it. It is deliberately
// NOT part of ports.LexicalSearch: production never seeds a lexical index by
// hand — SQLite's units_fts is kept current by a trigger — so a seeding
// method on the port would exist only for tests and would invite someone to
// call it.
type LexicalSeeder interface {
	ports.LexicalSearch

	// SeedLexical makes a unit findable under content. The real
	// implementation satisfies this in its test wrapper by inserting a unit;
	// the fake stores the tokens.
	SeedLexical(t *testing.T, id, content string)
}

// RunLexicalSearch runs the ports.LexicalSearch contract against a fresh
// implementation built by newSearch for every subtest.
//
// The suite asserts only what the port promises and what both implementations
// can honour: which ids come back, that k bounds them, and that an empty
// result is not an error. It deliberately does **not** pin a scoring
// formula — bm25's ranking is FTS5's, the fake's is not, and a contract that
// demanded identical ordering for partial matches would force the fake to
// reimplement bm25 or force the store to abandon it. Ranking quality is the
// recall corpus's job (PR 9c's L3 test), not this contract's.
func RunLexicalSearch(t *testing.T, newSearch func(t *testing.T) LexicalSeeder) {
	t.Helper()

	t.Run("a matching token returns the unit", func(t *testing.T) {
		s := newSearch(t)
		s.SeedLexical(t, "unit-1", "buy descaling solution for the coffee machine")

		got, err := s.SearchLexical(context.Background(), []string{"descaling"}, 10)
		if err != nil {
			t.Fatalf("SearchLexical: %v", err)
		}
		if len(got) != 1 || got[0] != "unit-1" {
			t.Errorf("SearchLexical = %v, want [unit-1]", got)
		}
	})

	// Tokens are OR-ed, not AND-ed (design D5: the adapter "quotes each
	// token and joins with OR"). Recall is a candidate generator — narrowing
	// to units containing every token would drop the near-misses fusion
	// exists to rank.
	t.Run("tokens are or-ed, not and-ed", func(t *testing.T) {
		s := newSearch(t)
		s.SeedLexical(t, "unit-coffee", "the coffee machine needs descaling")
		s.SeedLexical(t, "unit-passport", "renew the passport")

		got, err := s.SearchLexical(context.Background(), []string{"coffee", "passport"}, 10)
		if err != nil {
			t.Fatalf("SearchLexical: %v", err)
		}
		if len(got) != 2 {
			t.Errorf("SearchLexical = %v, want both units — tokens are or-ed, so a unit "+
				"matching one token is a candidate", got)
		}
	})

	t.Run("k bounds the results", func(t *testing.T) {
		s := newSearch(t)
		for _, id := range []string{"unit-1", "unit-2", "unit-3"} {
			s.SeedLexical(t, id, "descaling solution")
		}

		got, err := s.SearchLexical(context.Background(), []string{"descaling"}, 2)
		if err != nil {
			t.Fatalf("SearchLexical: %v", err)
		}
		if len(got) != 2 {
			t.Errorf("SearchLexical with k=2 returned %d ids, want 2: %v", len(got), got)
		}
	})

	// No match is an ordinary outcome of recall. Returning an error would
	// make every caller special-case the most common case in a young vault.
	t.Run("no match returns empty, not an error", func(t *testing.T) {
		s := newSearch(t)
		s.SeedLexical(t, "unit-1", "renew the passport")

		got, err := s.SearchLexical(context.Background(), []string{"thermodynamics"}, 10)
		if err != nil {
			t.Fatalf("SearchLexical = _, %v, want a nil error on no match", err)
		}
		if len(got) != 0 {
			t.Errorf("SearchLexical = %v, want no ids", got)
		}
	})

	// An empty token list means the caller tokenized to nothing — a message
	// of pure stop words, or of punctuation. It must not mean "match
	// everything": that would hand fusion the entire vault as candidates.
	t.Run("no tokens returns nothing, not everything", func(t *testing.T) {
		s := newSearch(t)
		s.SeedLexical(t, "unit-1", "renew the passport")

		got, err := s.SearchLexical(context.Background(), nil, 10)
		if err != nil {
			t.Fatalf("SearchLexical with no tokens = _, %v, want a nil error", err)
		}
		if len(got) != 0 {
			t.Errorf("SearchLexical with no tokens = %v, want nothing — an empty query must "+
				"not become a whole-vault scan", got)
		}
	})

	// One unit matching several tokens is still one candidate. A leg that
	// returned it once per matched token would give it that many chances to
	// score in fusion.
	t.Run("a unit matching several tokens appears once", func(t *testing.T) {
		s := newSearch(t)
		s.SeedLexical(t, "unit-1", "the coffee machine needs descaling solution")

		got, err := s.SearchLexical(context.Background(),
			[]string{"coffee", "descaling", "solution"}, 10)
		if err != nil {
			t.Fatalf("SearchLexical: %v", err)
		}
		if len(got) != 1 {
			t.Errorf("SearchLexical = %v, want unit-1 exactly once", got)
		}
	})
}
