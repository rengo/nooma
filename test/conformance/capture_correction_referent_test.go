// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// TestCapture_CorrectionExplicitReferentWinsWithoutRecall is spec R1.5,
// design D7: an explicit CaptureInput.ReferentID wins wherever it is set,
// and recall does not run at all when it does — proven here by an
// embedding-call count of zero, since RecallService.ScoredFor is the only
// path this pipeline has left that would call ports.EmbeddingProvider.Embed
// for a correction (the ordinary embedAndStore step never runs for a
// correction-classified capture either, R1.1).
func TestCapture_CorrectionExplicitReferentWinsWithoutRecall(t *testing.T) {
	t.Run("explicit id resolves directly and edits the target", func(t *testing.T) {
		ctx := context.Background()
		now := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)

		units := memrepo.NewUnits()
		originalEventAt := time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)
		if err := units.Create(ctx, unit.Unit{
			ID: "target-1", Type: unit.TypeTask, Status: unit.StatusPool,
			Content: "Dentist appointment", EventAt: &originalEventAt,
			Source: "chat", CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seeding target-1: %v", err)
		}

		decisions := memrepo.NewDecisionLog()
		embeddings := memrepo.NewEmbeddings()
		lexical := memrepo.NewLexical()
		relations := memrepo.NewRelations()
		llm := fakeprovider.New(t, testdataLLMCasesDir(t), "classify-correction-dentist-date")
		embed := fakeprovider.NewEmbeddingFake(embedFakeModel)

		idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
		if err != nil {
			t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
		}
		svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, units, embeddings, lexical, relations, decisions, llm, llm, embed, brain.NewIndex(idx), memrepo.NewSignals())

		result, err := svc.Capture(ctx, brain.CaptureInput{
			Text:       "irrelevant — the fake replays by case id, not prompt text",
			Channel:    "chat",
			ReferentID: "target-1",
		})
		if err != nil {
			t.Fatalf("Capture error = %v, want nil", err)
		}
		if result.Outcome != brain.OutcomeCorrected {
			t.Fatalf("Outcome = %q, want %q", result.Outcome, brain.OutcomeCorrected)
		}
		if result.Correction == nil || result.Correction.UnitID != "target-1" {
			t.Fatalf("Correction = %+v, want UnitID %q", result.Correction, "target-1")
		}
		if got := embed.EmbedCalls(); got != 0 {
			t.Errorf("EmbedCalls() = %d, want 0 — an explicit referent must never run recall (R1.5)", got)
		}

		got, err := units.ByID(ctx, "target-1")
		if err != nil {
			t.Fatalf("units.ByID: %v", err)
		}
		wantEventAt := time.Date(2026, 8, 15, 9, 0, 0, 0, time.FixedZone("-03:00", -3*3600))
		if got.EventAt == nil || !got.EventAt.Equal(wantEventAt) {
			t.Errorf("EventAt = %v, want %v", got.EventAt, wantEventAt)
		}
	})

	t.Run("an unknown explicit id errors and edits nothing", func(t *testing.T) {
		ctx := context.Background()
		now := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)

		units := memrepo.NewUnits()
		if err := units.Create(ctx, unit.Unit{
			ID: "unrelated-unit", Type: unit.TypeTask, Status: unit.StatusPool,
			Content: "Untouched content", Source: "chat", CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seeding unrelated-unit: %v", err)
		}

		decisions := memrepo.NewDecisionLog()
		embeddings := memrepo.NewEmbeddings()
		lexical := memrepo.NewLexical()
		relations := memrepo.NewRelations()
		llm := fakeprovider.New(t, testdataLLMCasesDir(t), "classify-correction-dentist-date")
		embed := fakeprovider.NewEmbeddingFake(embedFakeModel)

		idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
		if err != nil {
			t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
		}
		svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, units, embeddings, lexical, relations, decisions, llm, llm, embed, brain.NewIndex(idx), memrepo.NewSignals())

		_, err = svc.Capture(ctx, brain.CaptureInput{
			Text:       "irrelevant — the fake replays by case id, not prompt text",
			Channel:    "chat",
			ReferentID: "does-not-exist",
		})
		if err == nil {
			t.Fatal("Capture error = nil, want an error — an unknown explicit referent must never fall back to recall (R1.5)")
		}

		got, err := units.ByID(ctx, "unrelated-unit")
		if err != nil {
			t.Fatalf("units.ByID: %v", err)
		}
		if got.Content != "Untouched content" {
			t.Errorf("Content = %q, want unchanged — a failed explicit-referent resolution must edit nothing", got.Content)
		}
	})
}

// TestCapture_CorrectionChatPathReferentResolution is spec R1.6, design
// D2/D7/D9: when no explicit id is given, capture runs
// RecallService.ScoredFor(ctx, in.Text) — the raw text — gated by
// correction.Referent at ReferentMargin.
func TestCapture_CorrectionChatPathReferentResolution(t *testing.T) {
	t.Run("a single strong match resolves and edits the target", func(t *testing.T) {
		ctx := context.Background()
		now := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
		const rawText = "no, it's the 15th, not the 14th"

		units := memrepo.NewUnits()
		embeddings := memrepo.NewEmbeddings()
		lexical := memrepo.NewLexical()

		setupEmbed := fakeprovider.NewEmbeddingFake(embedFakeModel)
		matchVector, err := setupEmbed.Embed(ctx, ports.EmbedRequest{Text: rawText})
		if err != nil {
			t.Fatalf("deriving the match vector: %v", err)
		}

		originalEventAt := time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)
		if err := units.Create(ctx, unit.Unit{
			ID: "dentist-real", Type: unit.TypeTask, Status: unit.StatusPool,
			Content: "Dentist appointment on the 14th", EventAt: &originalEventAt,
			Source: "chat", CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seeding dentist-real: %v", err)
		}
		if err := embeddings.Put(ctx, ports.Embedding{UnitID: "dentist-real", Model: embedFakeModel, Vector: matchVector.Vector, At: now}); err != nil {
			t.Fatalf("seeding dentist-real's embedding: %v", err)
		}
		lexical.SeedLexical(t, "dentist-real", rawText)

		idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
		if err != nil {
			t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
		}
		decisions := memrepo.NewDecisionLog()
		relations := memrepo.NewRelations()
		llm := fakeprovider.New(t, testdataLLMCasesDir(t), "classify-correction-dentist-date")
		embed := fakeprovider.NewEmbeddingFake(embedFakeModel)
		svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, units, embeddings, lexical, relations, decisions, llm, llm, embed, brain.NewIndex(idx), memrepo.NewSignals())

		result, err := svc.Capture(ctx, brain.CaptureInput{Text: rawText, Channel: "chat"})
		if err != nil {
			t.Fatalf("Capture error = %v, want nil", err)
		}
		if result.Outcome != brain.OutcomeCorrected {
			t.Fatalf("Outcome = %q, want %q", result.Outcome, brain.OutcomeCorrected)
		}
		if result.Correction == nil || result.Correction.UnitID != "dentist-real" {
			t.Fatalf("Correction = %+v, want UnitID %q", result.Correction, "dentist-real")
		}

		got, err := units.ByID(ctx, "dentist-real")
		if err != nil {
			t.Fatalf("units.ByID: %v", err)
		}
		wantEventAt := time.Date(2026, 8, 15, 9, 0, 0, 0, time.FixedZone("-03:00", -3*3600))
		if got.EventAt == nil || !got.EventAt.Equal(wantEventAt) {
			t.Errorf("EventAt = %v, want %v", got.EventAt, wantEventAt)
		}
		if got.Content != "Dentist appointment on the 14th" {
			t.Errorf("Content = %q, want unchanged — dates win over content (R1.8)", got.Content)
		}

		// I03's correction half (12g.6): an UPDATE, not a write — the unit
		// count is unchanged and the id survives.
		if got := units.Count(); got != 1 {
			t.Errorf("units.Count() = %d, want 1 — a correction never creates a unit (R1.1, R1.11)", got)
		}
	})

	// R1.6's own Scenario: two units whose recall scores fall within the
	// margin of each other leave both untouched and ask instead of
	// guessing.
	t.Run("two units within margin ask, editing nothing", func(t *testing.T) {
		ctx := context.Background()
		now := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
		const rawText = "correct dentist appointment date again"

		units := memrepo.NewUnits()
		embeddings := memrepo.NewEmbeddings()
		lexical := memrepo.NewLexical()

		setupEmbed := fakeprovider.NewEmbeddingFake(embedFakeModel)
		matchVector, err := setupEmbed.Embed(ctx, ports.EmbedRequest{Text: rawText})
		if err != nil {
			t.Fatalf("deriving the match vector: %v", err)
		}

		seed := func(id, content string) {
			t.Helper()
			if err := units.Create(ctx, unit.Unit{
				ID: id, Type: unit.TypeTask, Status: unit.StatusPool,
				Content: content, Source: "chat", CreatedAt: now, UpdatedAt: now,
			}); err != nil {
				t.Fatalf("seeding %q: %v", id, err)
			}
			if err := embeddings.Put(ctx, ports.Embedding{UnitID: id, Model: embedFakeModel, Vector: matchVector.Vector, At: now}); err != nil {
				t.Fatalf("seeding %q's embedding: %v", id, err)
			}
			lexical.SeedLexical(t, id, rawText)
		}
		seed("dentist-1", "Dentist appointment, first vault entry")
		seed("dentist-2", "Dentist appointment, second vault entry")

		idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
		if err != nil {
			t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
		}
		decisions := memrepo.NewDecisionLog()
		relations := memrepo.NewRelations()
		llm := fakeprovider.New(t, testdataLLMCasesDir(t), "classify-correction-dentist-date")
		embed := fakeprovider.NewEmbeddingFake(embedFakeModel)
		svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, units, embeddings, lexical, relations, decisions, llm, llm, embed, brain.NewIndex(idx), memrepo.NewSignals())

		result, err := svc.Capture(ctx, brain.CaptureInput{Text: rawText, Channel: "chat"})
		if err != nil {
			t.Fatalf("Capture error = %v, want nil — an ambiguous referent asks, it does not fail (R1.6)", err)
		}
		if result.Outcome != brain.OutcomeAsked {
			t.Fatalf("Outcome = %q, want %q", result.Outcome, brain.OutcomeAsked)
		}
		if result.Correction == nil || !result.Correction.Ambiguous {
			t.Fatalf("Correction = %+v, want Ambiguous = true", result.Correction)
		}

		for _, tc := range []struct{ id, want string }{
			{"dentist-1", "Dentist appointment, first vault entry"},
			{"dentist-2", "Dentist appointment, second vault entry"},
		} {
			got, err := units.ByID(ctx, tc.id)
			if err != nil {
				t.Fatalf("units.ByID(%q): %v", tc.id, err)
			}
			if got.Content != tc.want {
				t.Errorf("%s Content = %q, want unchanged %q — an ambiguous referent must edit nothing", tc.id, got.Content, tc.want)
			}
		}

		rows, err := decisions.Since(ctx, now.Add(-time.Hour), -1)
		if err != nil {
			t.Fatalf("decisions.Since: %v", err)
		}
		if len(rows) != 1 || rows[0].Action != ports.ActionCorrectionAmbiguous {
			t.Fatalf("decision_log rows = %+v, want exactly one %q row", rows, ports.ActionCorrectionAmbiguous)
		}
	})

	// This is 12b's own ordering debt (Conflicts, tasks.md): correction.Referent
	// is pure and has no notion of unit status, so nothing before this link
	// could prove the gate runs over LIVE candidates only, with the ratio
	// recomputed over the survivors after an archived top scorer is dropped
	// — not merely that a non-live unit is excluded from the final result.
	// This corpus makes an archived unit the strongest dual-leg match and a
	// pool unit a weaker single-leg match: an unfiltered ratio would pick
	// the archived unit (a referent nothing can correct); the correct,
	// live-filtered ratio has exactly one surviving candidate and picks it
	// instead — the pick itself changes, not merely the result's membership.
	t.Run("dropping the archived top scorer changes the pick (I02)", func(t *testing.T) {
		ctx := context.Background()
		now := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
		const rawText = "correct dentist appointment date to fifteenth"

		units := memrepo.NewUnits()
		embeddings := memrepo.NewEmbeddings()
		lexical := memrepo.NewLexical()

		setupEmbed := fakeprovider.NewEmbeddingFake(embedFakeModel)
		matchVector, err := setupEmbed.Embed(ctx, ports.EmbedRequest{Text: rawText})
		if err != nil {
			t.Fatalf("deriving the match vector: %v", err)
		}

		originalArchivedEventAt := time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)
		if err := units.Create(ctx, unit.Unit{
			ID: "archived-A", Type: unit.TypeTask, Status: unit.StatusArchived,
			Content: "Archived dentist note", EventAt: &originalArchivedEventAt,
			Source: "chat", CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seeding archived-A: %v", err)
		}
		// archived-A matches BOTH legs: the exact vector, and every one of
		// rawText's own tokens lexically — the strongest possible fused
		// score.
		if err := embeddings.Put(ctx, ports.Embedding{UnitID: "archived-A", Model: embedFakeModel, Vector: matchVector.Vector, At: now}); err != nil {
			t.Fatalf("seeding archived-A's embedding: %v", err)
		}
		lexical.SeedLexical(t, "archived-A", rawText)

		originalLiveEventAt := time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)
		if err := units.Create(ctx, unit.Unit{
			ID: "live-B", Type: unit.TypeTask, Status: unit.StatusPool,
			Content: "Dentist appointment on the 14th", EventAt: &originalLiveEventAt,
			Source: "chat", CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seeding live-B: %v", err)
		}
		// live-B matches the lexical leg only, and only two of rawText's six
		// tokens — a weaker single-leg match, never seeded into the vector
		// index at all.
		lexical.SeedLexical(t, "live-B", "dentist appointment")

		idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
		if err != nil {
			t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
		}
		decisions := memrepo.NewDecisionLog()
		relations := memrepo.NewRelations()
		llm := fakeprovider.New(t, testdataLLMCasesDir(t), "classify-correction-dentist-date")
		embed := fakeprovider.NewEmbeddingFake(embedFakeModel)
		svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, units, embeddings, lexical, relations, decisions, llm, llm, embed, brain.NewIndex(idx), memrepo.NewSignals())

		result, err := svc.Capture(ctx, brain.CaptureInput{Text: rawText, Channel: "chat"})
		if err != nil {
			t.Fatalf("Capture error = %v, want nil", err)
		}
		if result.Outcome != brain.OutcomeCorrected {
			t.Fatalf("Outcome = %q, want %q", result.Outcome, brain.OutcomeCorrected)
		}
		if result.Correction == nil || result.Correction.UnitID != "live-B" {
			t.Fatalf("Correction = %+v, want UnitID %q — the archived unit outscores live-B before the live filter, and must never be picked", result.Correction, "live-B")
		}

		gotLive, err := units.ByID(ctx, "live-B")
		if err != nil {
			t.Fatalf("units.ByID(live-B): %v", err)
		}
		wantEventAt := time.Date(2026, 8, 15, 9, 0, 0, 0, time.FixedZone("-03:00", -3*3600))
		if gotLive.EventAt == nil || !gotLive.EventAt.Equal(wantEventAt) {
			t.Errorf("live-B EventAt = %v, want %v", gotLive.EventAt, wantEventAt)
		}

		gotArchived, err := units.ByID(ctx, "archived-A")
		if err != nil {
			t.Fatalf("units.ByID(archived-A): %v", err)
		}
		if gotArchived.Content != "Archived dentist note" || gotArchived.EventAt == nil || !gotArchived.EventAt.Equal(originalArchivedEventAt) {
			t.Errorf("archived-A = %+v, want byte-identical — the strongest raw scorer must never be corrected once it is not live", gotArchived)
		}
	})
}
