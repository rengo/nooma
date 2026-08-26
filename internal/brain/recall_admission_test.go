package brain

import (
	"context"
	"testing"

	"github.com/rengo/nooma/internal/core/recall"
)

// TestScoredFor_TheLexicalLegNoLongerAdmits is ADR-0020's second half, and
// the reason a similarity floor alone would not have fixed anything.
//
// recall.Tokenize lowercases and splits on non-alphanumerics with no
// stopword handling, so "cuando tengo gym" tokenises to
// [cuando tengo gym]. Measured against the live vault:
//
//	MATCH 'tengo'  ->  1 hit   (the dentist unit)
//	MATCH 'gym'    ->  0 hits
//
// The lexical leg admitted the dentist on a function word. With the vector
// leg floored and the lexical leg still admitting, the answer would have
// been identical.
//
// The lexical leg keeps its ranking contribution: an admitted unit that
// also matches lexically ranks above one that does not, which is what
// hybrid fusion is for once admission is settled (ADR-0010 stays the
// fusion).
//
// Mutation: let lexical ids into the admitted set and this fails.
func TestScoredFor_TheLexicalLegNoLongerAdmits(t *testing.T) {
	admitted := recall.Admit([]recall.Scored{{ID: "dentist", Score: 0.12}})
	if len(admitted) != 0 {
		t.Fatalf("this fixture needs the vector leg to reject the unit; it admitted %+v", admitted)
	}

	svc := &RecallService{
		index: NewIndex(recall.VectorIndex{}),
		lex:   lexicalAlways{ids: []string{"dentist"}},
		units: unitsHolding("dentist"),
		embed: embedRefusing{},
	}

	got, _, err := svc.ScoredFor(context.Background(), "cuando tengo gym")
	if err != nil {
		t.Fatalf("ScoredFor: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("recall answered with %d unit(s) on a lexical match alone: %+v — the lexical "+
			"leg ranks admitted results, it does not admit them", len(got), got)
	}
}
