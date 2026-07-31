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

// Embed implements ports.EmbeddingProvider.
func (f *Fake) Embed(_ context.Context, req ports.EmbedRequest) (ports.EmbedResponse, error) {
	return ports.EmbedResponse{
		Vector: deterministicVector(req.Text),
		Model:  f.embedModel,
	}, nil
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
