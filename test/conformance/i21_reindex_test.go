// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

var reindexAt = time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)

// reindexEmbedder answers every Embed with a vector under model and records
// the texts it was asked for. A text in failOn is refused; with stopAfter set,
// the call after that many cancels the run, as Ctrl-C does.
type reindexEmbedder struct {
	model     string
	asked     []string
	failOn    map[string]bool
	stopAfter int
	cancel    context.CancelFunc
}

func (e *reindexEmbedder) Embed(ctx context.Context, req ports.EmbedRequest) (ports.EmbedResponse, error) {
	if e.stopAfter > 0 && len(e.asked) == e.stopAfter {
		e.cancel()
		return ports.EmbedResponse{}, ctx.Err()
	}
	e.asked = append(e.asked, req.Text)
	if e.failOn[req.Text] {
		return ports.EmbedResponse{}, errors.New("the provider refused this text")
	}
	return ports.EmbedResponse{Vector: []float32{float32(len(e.asked)), 1}, Model: e.model}, nil
}

// reindexVault holds five live units, each with a model-old vector, u-done
// with a model-new one too, and an archived unit with no vector at all.
type reindexVault struct {
	units  *memrepo.Units
	embeds *memrepo.Embeddings
}

func newReindexVault(t *testing.T) reindexVault {
	t.Helper()
	ctx := context.Background()
	v := reindexVault{units: memrepo.NewUnits(), embeds: memrepo.NewEmbeddings()}
	put := func(id, model string) {
		if err := v.embeds.Put(ctx, ports.Embedding{UnitID: id, Model: model, Vector: []float32{1, 0}, At: reindexAt}); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"u-1", "u-2", "u-3", "u-4", "u-done", "u-archived"} {
		status := unit.StatusPool
		if id == "u-archived" {
			status = unit.StatusArchived
		}
		if err := v.units.Create(ctx, unit.Unit{ID: id, Type: unit.TypeKnowledge, Content: "text of " + id, Status: status,
			CreatedAt: reindexAt, UpdatedAt: reindexAt, LastTouchedAt: reindexAt}); err != nil {
			t.Fatal(err)
		}
		if status == unit.StatusPool {
			v.embeds.EnsureUnit(t, id)
			put(id, "model-old")
		}
	}
	put("u-done", "model-new")
	return v
}

func (v reindexVault) run(ctx context.Context, embed ports.EmbeddingProvider, progress func(done, total int)) (brain.ReindexResult, error) {
	return brain.NewReindexService(fixedClock{now: reindexAt}, v.units, v.embeds, embed).Reindex(ctx, "model-new", progress)
}

func (v reindexVault) indexed(t *testing.T, model string) []string {
	t.Helper()
	idx, err := v.embeds.LoadIndex(context.Background(), model)
	if err != nil {
		t.Fatal(err)
	}
	ids := slices.Clone(idx.IDs)
	slices.Sort(ids)
	return ids
}

var allLive = []string{"u-1", "u-2", "u-3", "u-4", "u-done"}

// TestI21_ReindexAddsTheModelsVectorsBesideTheOld (ADR-0058 point 4): each
// live unit lacking the configured model's vector is embedded from its
// content, once, by id; the old model's vectors all stay; a unit already
// embedded and an archived unit are never asked for; progress reaches the
// total; and a second run finds nothing to do and calls no provider.
func TestI21_ReindexAddsTheModelsVectorsBesideTheOld(t *testing.T) {
	v := newReindexVault(t)
	embed := &reindexEmbedder{model: "model-new"}
	var last [2]int
	res, err := v.run(context.Background(), embed, func(done, total int) { last = [2]int{done, total} })
	if err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	if res.Missing != 4 || res.Embedded != 4 || res.Failed != 0 {
		t.Errorf("result = %+v, want 4 missing and 4 embedded", res)
	}
	if got := strings.Join(embed.asked, ","); got != "text of u-1,text of u-2,text of u-3,text of u-4" {
		t.Errorf("asked to embed %q, want each missing live unit's content once, by id", got)
	}
	if got := v.indexed(t, "model-new"); !slices.Equal(got, allLive) {
		t.Errorf("model-new index = %v, want every live unit %v", got, allLive)
	}
	if got := v.indexed(t, "model-old"); !slices.Equal(got, allLive) {
		t.Errorf("model-old index = %v, want it whole %v: nothing is taken away", got, allLive)
	}
	if last != [2]int{4, 4} {
		t.Errorf("last progress = %v, want 4 of 4", last)
	}

	again := &reindexEmbedder{model: "model-new"}
	if res, err := v.run(context.Background(), again, nil); err != nil || res.Missing != 0 || res.Embedded != 0 || len(again.asked) != 0 {
		t.Errorf("a second run = %+v, %v asking %v; want nothing to do and no call", res, err, again.asked)
	}
}

// TestI21_AnInterruptedReindexResumesWithoutRepaying: a run stopped after two
// units keeps both vectors, and the next run asks only for the other two.
func TestI21_AnInterruptedReindexResumesWithoutRepaying(t *testing.T) {
	v := newReindexVault(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := &reindexEmbedder{model: "model-new", stopAfter: 2, cancel: cancel}
	res, err := v.run(ctx, first, nil)
	if !errors.Is(err, context.Canceled) || res.Embedded != 2 || res.Failed != 0 {
		t.Fatalf("the interrupted run = %+v, %v; want 2 embedded, none failed, and the cancellation", res, err)
	}

	second := &reindexEmbedder{model: "model-new"}
	res, err = v.run(context.Background(), second, nil)
	if err != nil || res.Missing != 2 || res.Embedded != 2 {
		t.Fatalf("the resumed run = %+v, %v; want the 2 left", res, err)
	}
	if got := strings.Join(second.asked, ","); got != "text of u-3,text of u-4" {
		t.Errorf("the resumed run asked for %q, want only u-3 and u-4: nothing paid twice", got)
	}
	if got := v.indexed(t, "model-new"); !slices.Equal(got, allLive) {
		t.Errorf("model-new index after both runs = %v, want %v", got, allLive)
	}
}

// TestI21_AReindexPassesARefusedUnitAndARerunRetriesItAlone: a unit the
// provider refuses is counted with its reason, the run goes on past it, and
// the next run asks for that unit only.
func TestI21_AReindexPassesARefusedUnitAndARerunRetriesItAlone(t *testing.T) {
	v := newReindexVault(t)
	embed := &reindexEmbedder{model: "model-new", failOn: map[string]bool{"text of u-2": true}}
	res, err := v.run(context.Background(), embed, nil)
	if err != nil || res.Embedded != 3 || res.Failed != 1 || res.LastFailure == nil || !strings.Contains(res.LastFailure.Error(), "u-2") {
		t.Fatalf("result = %+v, %v; want 3 embedded and 1 failed naming u-2", res, err)
	}
	retry := &reindexEmbedder{model: "model-new"}
	if res, err := v.run(context.Background(), retry, nil); err != nil || res.Embedded != 1 || strings.Join(retry.asked, ",") != "text of u-2" {
		t.Errorf("the rerun = %+v, %v asking %v; want only u-2", res, err, retry.asked)
	}
}

// TestI21_AReindexStopsWhenTheProviderRefusesThreeInARow: a provider that is
// down is not called once per unit; what was stored before stays.
func TestI21_AReindexStopsWhenTheProviderRefusesThreeInARow(t *testing.T) {
	v := newReindexVault(t)
	embed := &reindexEmbedder{model: "model-new", failOn: map[string]bool{"text of u-2": true, "text of u-3": true, "text of u-4": true}}
	// u-1 is embedded, then three refusals in a row stop the run.
	res, err := v.run(context.Background(), embed, nil)
	if !errors.Is(err, brain.ErrReindexProviderUnavailable) || res.Embedded != 1 || res.Failed != 3 {
		t.Fatalf("result = %+v, %v; want 1 embedded, 3 failed and the provider named unavailable", res, err)
	}
	if got := v.indexed(t, "model-new"); !slices.Equal(got, []string{"u-1", "u-done"}) {
		t.Errorf("model-new index = %v, want u-1 kept beside u-done", got)
	}

	streakBroken := newReindexVault(t)
	alternating := &reindexEmbedder{model: "model-new", failOn: map[string]bool{"text of u-1": true, "text of u-2": true, "text of u-4": true}}
	if res, err := streakBroken.run(context.Background(), alternating, nil); err != nil || res.Embedded != 1 || res.Failed != 3 {
		t.Errorf("two refusals, a success, a refusal = %+v, %v; want the run to finish: the streak was broken", res, err)
	}
}

// TestI21_AReindexNeverStoresAnotherModelsVector: a provider answering under
// another model would mix two models' vectors (I21); the run stops and stores
// nothing under the configured one.
func TestI21_AReindexNeverStoresAnotherModelsVector(t *testing.T) {
	v := newReindexVault(t)
	res, err := v.run(context.Background(), &reindexEmbedder{model: "model-other"}, nil)
	if err == nil || !strings.Contains(err.Error(), "model-other") || res.Embedded != 0 {
		t.Errorf("Reindex = %+v, %v; want a refusal naming model-other and nothing stored", res, err)
	}
	if got := v.indexed(t, "model-new"); !slices.Equal(got, []string{"u-done"}) {
		t.Errorf("model-new index = %v, want only u-done, as before", got)
	}
	if got := v.indexed(t, "model-other"); len(got) != 0 {
		t.Errorf("model-other index = %v, want nothing stored under it either", got)
	}
}
