package brain

import (
	"context"
	"fmt"
	"math"
	"slices"
	"sync"

	"github.com/rengo/nooma/internal/ports"
)

// scriptedEmbedder is a test-local ports.EmbeddingProvider whose answer is
// scripted per input text, so a test can hand-build every vector and every
// cosine is exact (m4e design §6, FX-D / FX-D2). It differs from
// fakeprovider's embedding fake on purpose: that one derives a vector from a
// hash and can neither fail for one text nor return a NaN, a zero or a
// short vector, which are the cases the retired shield's screen exists for.
//
// A text with no script is an error, never a default vector: an unscripted
// call means the fixture and the code disagree about what gets embedded,
// and that has to be loud.
type scriptedEmbedder struct {
	mu      sync.Mutex
	model   string
	vectors map[string][]float32
	errs    map[string]error
	cancels map[string]context.CancelFunc
	calls   []string
}

func newScriptedEmbedder(model string) *scriptedEmbedder {
	return &scriptedEmbedder{
		model:   model,
		vectors: map[string][]float32{},
		errs:    map[string]error{},
		cancels: map[string]context.CancelFunc{},
	}
}

// Vector scripts text to v. v is returned as given: NaN, zero, empty and
// wrong-length vectors are scripted exactly like a good one.
func (s *scriptedEmbedder) Vector(text string, v ...float32) *scriptedEmbedder {
	s.vectors[text] = v
	return s
}

// Fail scripts text to fail with err.
func (s *scriptedEmbedder) Fail(text string, err error) *scriptedEmbedder {
	s.errs[text] = err
	return s
}

// CancelOn scripts text to call cancel when it is embedded, and to fail the
// way a provider fails when its context was cancelled mid-call. Other texts
// are answered as scripted: a provider is free to ignore a context, and the
// phase under test must not depend on a later call failing to notice the
// cancellation.
func (s *scriptedEmbedder) CancelOn(text string, cancel context.CancelFunc) *scriptedEmbedder {
	s.cancels[text] = cancel
	return s
}

// Embed implements ports.EmbeddingProvider.
func (s *scriptedEmbedder) Embed(ctx context.Context, req ports.EmbedRequest) (ports.EmbedResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.calls = append(s.calls, req.Text)
	if cancel, ok := s.cancels[req.Text]; ok {
		cancel()
		return ports.EmbedResponse{}, ctx.Err()
	}
	if err, ok := s.errs[req.Text]; ok {
		return ports.EmbedResponse{}, err
	}
	v, ok := s.vectors[req.Text]
	if !ok {
		return ports.EmbedResponse{}, fmt.Errorf("scriptedEmbedder: no vector scripted for %q", req.Text)
	}
	return ports.EmbedResponse{Vector: slices.Clone(v), Model: s.model}, nil
}

// EmbedCalls is how many times Embed was called, scripted or not.
func (s *scriptedEmbedder) EmbedCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

// Embedded returns the texts embedded, in call order.
func (s *scriptedEmbedder) Embedded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.calls)
}

// axis returns a dim-length vector that is 1 on index i and 0 elsewhere.
func axis(dim, i int) []float32 {
	v := make([]float32, dim)
	v[i] = 1
	return v
}

// mix returns a dim-length vector with weight a on axis i and weight b on
// axis j. With a^2 + b^2 = 1 it is a unit vector, and its dot product with
// axis(dim, i) is exactly float32(a).
func mix(dim, i int, a float64, j int, b float64) []float32 {
	v := make([]float32, dim)
	v[i] = float32(a)
	v[j] = float32(b)
	return v
}

// cosAt returns the weight on the second axis that makes mix(.., a, .., b)
// a unit vector.
func cosAt(a float64) float64 { return math.Sqrt(1 - a*a) }
