package brain

import (
	"context"
	"testing"

	"github.com/rengo/nooma/internal/core/recall"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// admissionLexical answers every query with the same ids, which is what
// the live FTS index did for "cuando tengo gym": it matched on "tengo".
type admissionLexical struct{ ids []string }

func (l admissionLexical) SearchLexical(context.Context, []string, int) ([]string, error) {
	return l.ids, nil
}

// admissionUnits resolves any id to a unit, so a leaked id becomes a
// visible answer rather than a silent drop.
type admissionUnits struct{ ports.UnitRepo }

func (admissionUnits) LiveByIDs(_ context.Context, ids []string) ([]unit.Unit, error) {
	out := make([]unit.Unit, len(ids))
	for i, id := range ids {
		out[i] = unit.Unit{ID: id, Content: "Tengo cita con el dentista el 2026-08-28."}
	}
	return out, nil
}

// admissionEmbed answers with a vector orthogonal to the stored one, so
// the cosine is 0 — far below the floor, and nothing like an error: the
// semantic leg WORKS and rejects.
type admissionEmbed struct{}

func (admissionEmbed) Embed(context.Context, ports.EmbedRequest) (ports.EmbedResponse, error) {
	return ports.EmbedResponse{Model: "test-model", Vector: []float32{1, 0}}, nil
}

// TestScoredFor_TheLexicalLegNoLongerAdmits is ADR-0020's second half, and
// the reason a similarity floor alone would have fixed nothing.
//
// recall.Tokenize lowercases and splits on non-alphanumerics with no
// stopword handling, so "cuando tengo gym" tokenises to
// [cuando tengo gym]. Measured against the live vault:
//
//	MATCH 'tengo'  ->  1 hit   (the dentist unit)
//	MATCH 'gym'    ->  0 hits
//
// The lexical leg admitted the dentist on a function word. With the vector
// leg floored and the lexical leg still admitting, the reader would have
// received the identical wrong answer.
//
// The embedding here SUCCEEDS and returns a vector orthogonal to the
// stored one — cosine 0. That distinction matters: a failing embedder
// makes the lexical leg the only leg, which is a documented fallback and a
// different case. This one is the semantic leg working and saying no.
//
// Mutation: drop the intersect and this fails with the dentist returned.
func TestScoredFor_TheLexicalLegNoLongerAdmits(t *testing.T) {
	idx, err := recall.NewVectorIndex("test-model", []string{"dentist"}, [][]float32{{0, 1}})
	if err != nil {
		t.Fatalf("NewVectorIndex: %v", err)
	}

	svc := &RecallService{
		index: NewIndex(idx),
		lex:   admissionLexical{ids: []string{"dentist"}},
		units: admissionUnits{},
		embed: admissionEmbed{},
	}

	got, _, err := svc.ForText(context.Background(), "cuando tengo gym")
	if err != nil {
		t.Fatalf("ScoredFor: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("recall answered with %d unit(s) on a lexical match alone: %+v — the lexical "+
			"leg ranks admitted results, it does not admit them", len(got), got)
	}
}

// TestScoredFor_AnAdmittedUnitStillAnswers guards the fix from becoming a
// mute: the floor must reject the far thing WITHOUT rejecting the near
// one, and only both assertions together say that.
func TestScoredFor_AnAdmittedUnitStillAnswers(t *testing.T) {
	idx, err := recall.NewVectorIndex("test-model", []string{"dentist"}, [][]float32{{1, 0}})
	if err != nil {
		t.Fatalf("NewVectorIndex: %v", err)
	}

	svc := &RecallService{
		index: NewIndex(idx),
		lex:   admissionLexical{ids: []string{"dentist"}},
		units: admissionUnits{},
		embed: admissionEmbed{},
	}

	got, _, err := svc.ForText(context.Background(), "cuando tengo dentista")
	if err != nil {
		t.Fatalf("ScoredFor: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("recall answered with %d unit(s) for an identical vector; the floor rejects "+
			"what it should keep", len(got))
	}
}
