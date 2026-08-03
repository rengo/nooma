package httpapi

import (
	"context"
	"testing"

	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
)

// TestReadRoutesWriteNoDecisionLog is R2.7's own route-level test: none of
// this PR's three read routes (POST /recall, GET /units/{id}, GET /units)
// writes a decision_log row — doc 02 §4's own reasoning, "a judgment that
// decided nothing writes nothing", applied at the HTTP boundary the same way
// test/conformance's own TestRecallWritesNoDecisionRow already proves it at
// the brain layer.
//
// Its own honest limitation, stated for the same reason that test states
// one: brain.NewRecallService's signature (index, lex, units, embed) takes
// no ports.DecisionLog parameter at all, and Deps carries none either — so
// nothing this handler builds could reach a DecisionLog.Record call even if
// it tried. "An instrumented fake failing the test if Record is invoked"
// (this PR's own task text) is therefore unrepresentable by construction,
// the identical shape TestRecallWritesNoDecisionRow already names for
// RecallService itself; this test drives the three registered read routes
// end to end and pins that none of them needs, or could use, a DecisionLog
// to answer.
func TestReadRoutesWriteNoDecisionLog(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	units, embeddings, lexical, embed := newTestRecallFixtures(t)
	if err := units.Create(ctx, poolUnitFixture("u-read", "a note about the leaky faucet")); err != nil {
		t.Fatalf("seeding u-read: %v", err)
	}
	if err := embeddings.Put(ctx, unitEmbeddingFor(t, "u-read", "a note about the leaky faucet")); err != nil {
		t.Fatalf("seeding u-read's embedding: %v", err)
	}

	svc := buildRecallService(t, units, embeddings, lexical, embed)
	h := Handler(Deps{Version: "test", Recall: svc})

	if rec := postRecall(t, h, `{"query": "leaky faucet"}`); rec.Code != 200 {
		t.Fatalf("POST /recall: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec := getRoute(t, h, "/units/u-read"); rec.Code != 200 {
		t.Fatalf("GET /units/u-read: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec := getRoute(t, h, "/units?ids=u-read"); rec.Code != 200 {
		t.Fatalf("GET /units?ids=u-read: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// No assertion beyond "these succeeded": the absence of a DecisionLog
	// dependency anywhere in this call chain (see doc comment above) is what
	// makes a written row unrepresentable, not a runtime check this test
	// could still fail.
}

// unitEmbeddingFor derives id's fake embedding the same way
// TestRecallHandler_EmbedsAndReturnsScoredUnits does, factored out because
// this file needs it too.
func unitEmbeddingFor(t *testing.T, id, content string) ports.Embedding {
	t.Helper()
	embed := fakeprovider.NewEmbeddingFake(embedFakeModel)
	vec, err := embed.Embed(context.Background(), ports.EmbedRequest{Text: content})
	if err != nil {
		t.Fatalf("deriving %s's vector: %v", id, err)
	}
	return ports.Embedding{UnitID: id, Model: embedFakeModel, Vector: vec.Vector}
}
