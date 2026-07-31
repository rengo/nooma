package fakeprovider_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
)

// The EmbeddingProvider compile-time assertion, deferred here from
// fakeprovider_test.go (task 5.1) until Embed exists — task 5.5's own red.
var _ ports.EmbeddingProvider = (*fakeprovider.Fake)(nil)

func TestFakeEmbeddingProvider_DeterministicByModel(t *testing.T) {
	a := fakeprovider.NewEmbeddingFake("model-a")
	b := fakeprovider.NewEmbeddingFake("model-b")

	text := "remember to buy descaling solution"

	got1, err := a.Embed(context.Background(), ports.EmbedRequest{Text: text})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	got2, err := a.Embed(context.Background(), ports.EmbedRequest{Text: text})
	if err != nil {
		t.Fatalf("Embed (second call, same fake, same text): %v", err)
	}
	if !reflect.DeepEqual(got1.Vector, got2.Vector) {
		t.Fatalf("same text against the same configured model returned different vectors: %v vs %v", got1.Vector, got2.Vector)
	}
	if len(got1.Vector) == 0 {
		t.Fatalf("Embed returned an empty vector")
	}

	got3, err := b.Embed(context.Background(), ports.EmbedRequest{Text: text})
	if err != nil {
		t.Fatalf("Embed (model-b, same text): %v", err)
	}
	if got1.Model == got3.Model {
		t.Fatalf("two fakes built with different model names reported the same Model: %q", got1.Model)
	}
	if got1.Model != "model-a" || got3.Model != "model-b" {
		t.Fatalf("Model = %q / %q, want model-a / model-b", got1.Model, got3.Model)
	}
}
