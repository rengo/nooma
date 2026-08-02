package fakeprovider

import (
	"context"
	"hash/fnv"

	"github.com/rengo/nooma/internal/ports"
)

// fakeEmbeddingDim is the length of every vector NewEmbeddingFake's Embed
// returns. It carries no relationship to a real model's dimension — this
// is a test double, not an embedding model — and len(Vector) is the only
// dimension EmbedResponse ever reports (design D7: no Dim field).
const fakeEmbeddingDim = 8

// NewEmbeddingFake returns a Fake whose Embed reports model — fixed at
// construction (design D7) — and whose vector is derived deterministically
// from the input text: the same text against the same fake always returns
// the same vector, and two fakes built with different model names report
// different Model values for the same text. This is the Phase B seam I21
// needs — a vault holding two models' embeddings, where a vector query
// must filter on the model that produced a vector rather than mixing them.
//
// A Fake built this way carries no Complete script (New builds that half).
// Calling Complete on it is a caller error, not a documented use.
func NewEmbeddingFake(model string) *Fake {
	return &Fake{embedModel: model}
}

// NewEmbeddingFakeWithError returns a Fake whose Embed always fails with
// err — scripted at construction, the same "recorded, not discovered" shape
// New's per-case Error field gives Complete (fakeprovider.go:80-82). This is
// what design D8's degradation path (task 10b.8) needs to prove: a captured
// unit whose embedding write fails must still be persisted, and the test
// asserting that needs a provider that fails on command, never a hand-rolled
// stub bypassing this package's own scripting discipline.
func NewEmbeddingFakeWithError(model string, err error) *Fake {
	return &Fake{embedModel: model, embedErr: err}
}

// Embed implements ports.EmbeddingProvider.
func (f *Fake) Embed(_ context.Context, req ports.EmbedRequest) (ports.EmbedResponse, error) {
	f.embedCalls++
	if f.embedErr != nil {
		return ports.EmbedResponse{}, f.embedErr
	}
	return ports.EmbedResponse{
		Vector: deterministicVector(req.Text),
		Model:  f.embedModel,
	}, nil
}

// EmbedCalls returns how many times Embed was called on f. Task 10b.4's
// "embedded exactly once" property needs a call count, not just a stored
// row: a bug that called Embed twice but only persisted the second result
// would pass a row-count assertion and still fail this one.
func (f *Fake) EmbedCalls() int {
	return f.embedCalls
}

// deterministicVector derives a fixed-length []float32 from text via
// fnv-1a, reseeded per output element — the same text always yields the
// same vector. No semantic meaning is claimed; only determinism.
func deterministicVector(text string) []float32 {
	vec := make([]float32, fakeEmbeddingDim)
	for i := range vec {
		h := fnv.New32a()
		h.Write([]byte{byte(i)})
		h.Write([]byte(text))
		vec[i] = float32(h.Sum32()%10000) / 10000
	}
	return vec
}
