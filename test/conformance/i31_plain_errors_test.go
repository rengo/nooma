package conformance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// failingLLM answers every completion with a fixed error.
type failingLLM struct{ err error }

func (f failingLLM) Complete(context.Context, ports.LLMRequest) (ports.LLMResponse, error) {
	return ports.LLMResponse{}, f.err
}

// sayingLLM answers every completion with fixed text.
type sayingLLM struct{ text string }

func (f sayingLLM) Complete(context.Context, ports.LLMRequest) (ports.LLMResponse, error) {
	return ports.LLMResponse{Text: f.text, Model: "scripted"}, nil
}

func i31Service(t *testing.T, llm ports.LLMProvider, units *memrepo.Units) *brain.CaptureService {
	t.Helper()
	ctx := context.Background()
	embeddings := memrepo.NewEmbeddings()
	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatalf("embeddings.LoadIndex: %v", err)
	}
	now := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)
	return brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, units, embeddings, memrepo.NewLexical(), memrepo.NewRelations(), memrepo.NewDecisionLog(), llm, llm, llm, fakeprovider.NewEmbeddingFake(embedFakeModel), brain.NewIndex(idx), memrepo.NewSignals(), memrepo.NewTriggers(), memrepo.NewTimers(), 0.5, nil)
}

// TestI31_AFailedCaptureNamesItsClassAndSavesNothing: an answer the decoder
// cannot use, or a provider that fails, ends the capture with an error
// brain.Describe recognises — a class a person can act on — and the vault
// holds no unit for it (doc 02 §5.1, "reported as a failed classification").
func TestI31_AFailedCaptureNamesItsClassAndSavesNothing(t *testing.T) {
	tests := []struct {
		name     string
		llm      func(*testing.T) ports.LLMProvider
		wantCode string
	}{
		{"an answer with no fields", func(t *testing.T) ports.LLMProvider {
			return fakeprovider.New(t, testdataLLMCasesDir(t), "classify-empty-response")
		}, "model_output_unusable"},
		{"an answer with no type", func(t *testing.T) ports.LLMProvider {
			return fakeprovider.New(t, testdataLLMCasesDir(t), "classify-unknown-enum-value")
		}, "model_output_unusable"},
		{"a type with no content", func(*testing.T) ports.LLMProvider {
			return sayingLLM{`{"type":"task","weight":0.6,"decay_rate":0.1}`}
		}, "model_output_unusable"},
		{"a missing key", func(*testing.T) ports.LLMProvider {
			return failingLLM{ports.KeyMissing("openai", "OPENAI_API_KEY")}
		}, "provider_key_missing"},
		{"a rejected key", func(*testing.T) ports.LLMProvider {
			return failingLLM{ports.StatusFailure("openai", 401)}
		}, "provider_key_rejected"},
		{"a rate limit", func(*testing.T) ports.LLMProvider {
			return failingLLM{ports.StatusFailure("openai", 429)}
		}, "provider_rate_limited"},
		{"an unreachable provider", func(*testing.T) ports.LLMProvider {
			return failingLLM{ports.TransportFailure("openai", errors.New("connection refused"))}
		}, "provider_unreachable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			units := memrepo.NewUnits()
			svc := i31Service(t, tt.llm(t), units)

			_, err := svc.Capture(context.Background(), brain.CaptureInput{Text: "buy lilies", Channel: "chat"})
			if err == nil {
				t.Fatal("Capture error = nil, want a failure")
			}
			f, ok := brain.Describe(err)
			if !ok || f.Code != tt.wantCode {
				t.Errorf("Describe(%v) = %+v, %v; want code %q", err, f, ok, tt.wantCode)
			}
			if got := units.Count(); got != 0 {
				t.Errorf("units.Count() = %d, want 0 — nothing is saved on a failed capture", got)
			}
		})
	}
}
