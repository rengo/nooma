package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/store/sqlite"
	"github.com/rengo/nooma/internal/store/sqlite/sqlitetest"
	"github.com/rengo/nooma/internal/store/vaultlock"
)

// fakeOllama answers /api/embed with a two-number vector under the model the
// request names and counts the calls; it answers 500 to an input containing
// failOn ("" fails nothing).
func fakeOllama(t *testing.T, calls *atomic.Int64, failOn string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var req struct {
			Model string `json:"model"`
			Input string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if failOn != "" && strings.Contains(req.Input, failOn) {
			http.Error(w, "model not loaded", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": req.Model, "embeddings": [][]float32{{float32(len(req.Input)), 1}}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// reindexVaultAt is a temporary vault whose embedding task is bound to an
// ollama provider at endpoint naming model-a, holding three live units: u-a
// embedded under model-a, u-b under model-b, u-bare under none, and u-bare
// missing from the word index.
func reindexVaultAt(t *testing.T, endpoint string) string {
	t.Helper()
	vault, _, _ := doctorVault(t, func(db *sqlite.Vault) {
		seedUnits(t, db, map[string]string{"u-a": "model-a", "u-b": "model-b"}, "u-a", "u-b", "u-bare")
		sqlitetest.Exec(t, db, `INSERT INTO units_fts(units_fts, rowid, content) SELECT 'delete', rowid, content FROM units WHERE id = 'u-bare'`)
	})
	writeYML(t, vault, "providers:\n  local:\n    type: ollama\n    model: model-a\n    endpoint: "+endpoint+"\ntasks:\n  embedding:\n    provider: local\n")
	return vault
}

// reindexState reads what a run left: the ids under each model and the word
// index's gaps.
func reindexState(t *testing.T, vault string) (modelA, modelB int, gaps sqlite.SearchIndexGaps) {
	t.Helper()
	db := openVault(t, vault)
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	a, err := sqlite.NewEmbeddingRepo(db).LoadIndex(ctx, "model-a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := sqlite.NewEmbeddingRepo(db).LoadIndex(ctx, "model-b")
	if err != nil {
		t.Fatal(err)
	}
	if gaps, err = db.SearchIndexGaps(ctx); err != nil {
		t.Fatal(err)
	}
	return len(a.IDs), len(b.IDs), gaps
}

func wantLines(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

// TestReindex_EmbedsBesideTheOldModelAndRebuildsTheWordIndex: through the
// real wiring and a fake provider, reindex embeds the two units lacking
// model-a, keeps u-b's model-b vector, rebuilds the word index, and says so;
// run again, it calls nothing and says there is nothing to embed.
func TestReindex_EmbedsBesideTheOldModelAndRebuildsTheWordIndex(t *testing.T) {
	var calls atomic.Int64
	vault := reindexVaultAt(t, fakeOllama(t, &calls, "").URL)

	var out bytes.Buffer
	if err := reindex(context.Background(), []string{"--yes", vault}, strings.NewReader(""), &out, io.Discard); err != nil {
		t.Fatalf("reindex: %v\n%s", err, out.String())
	}
	if calls.Load() != 2 {
		t.Errorf("the provider was called %d times, want 2 (u-b and u-bare)", calls.Load())
	}
	wantLines(t, out.String(), "2 live units lack a model-a vector", "word index rebuilt",
		"2 of 2 done under model-a", "embedded 2, skipped 1 already embedded, failed 0", "every live unit has a model-a vector")
	if a, b, gaps := reindexState(t, vault); a != 3 || b != 1 || gaps != (sqlite.SearchIndexGaps{}) {
		t.Errorf("after reindex: %d model-a, %d model-b vectors, word index gaps %+v; want 3, 1 kept, none", a, b, gaps)
	}

	out.Reset()
	if err := reindex(context.Background(), []string{vault}, strings.NewReader(""), &out, io.Discard); err != nil {
		t.Fatalf("second reindex: %v", err)
	}
	if calls.Load() != 2 {
		t.Errorf("a second run called the provider %d more times, want none", calls.Load()-2)
	}
	wantLines(t, out.String(), "nothing to embed: every live unit already has a model-a vector")
}

// TestReindex_AsksBeforeCallingTheProvider (ADR-0003's confirmation): with
// something to embed and no --yes, reindex asks; a no, or no answer at all,
// changes nothing and calls nothing; a yes goes on.
func TestReindex_AsksBeforeCallingTheProvider(t *testing.T) {
	var calls atomic.Int64
	vault := reindexVaultAt(t, fakeOllama(t, &calls, "").URL)

	for _, answer := range []string{"n\n", ""} {
		var out bytes.Buffer
		if err := reindex(context.Background(), []string{vault}, strings.NewReader(answer), &out, io.Discard); err != nil {
			t.Fatalf("reindex answered %q: %v", answer, err)
		}
		wantLines(t, out.String(), "Continue? [y/N]", "nothing was changed", "--yes")
		if a, _, gaps := reindexState(t, vault); calls.Load() != 0 || a != 1 || gaps.Missing != 1 {
			t.Errorf("answered %q: %d calls, %d model-a vectors, gaps %+v; want nothing changed", answer, calls.Load(), a, gaps)
		}
	}

	var out bytes.Buffer
	if err := reindex(context.Background(), []string{vault}, strings.NewReader("yes\n"), &out, io.Discard); err != nil {
		t.Fatalf("reindex answered yes: %v", err)
	}
	if a, _, _ := reindexState(t, vault); calls.Load() != 2 || a != 3 {
		t.Errorf("answered yes: %d calls, %d model-a vectors; want 2 and 3", calls.Load(), a)
	}
}

// TestReindex_RefusesWhileServeHoldsTheVault: reindex writes, so it takes the
// vault lock; a running serve holds it, and reindex refuses before any
// provider call, naming the steps.
func TestReindex_RefusesWhileServeHoldsTheVault(t *testing.T) {
	var calls atomic.Int64
	vault := reindexVaultAt(t, fakeOllama(t, &calls, "").URL)
	lock, err := vaultlock.Acquire(vault)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Release() }()

	err = reindex(context.Background(), []string{"--yes", vault}, strings.NewReader(""), &bytes.Buffer{}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "in use") || !strings.Contains(err.Error(), "stop nooma serve, run nooma reindex, and start it again") {
		t.Errorf("reindex beside a held lock = %v, want a refusal naming the holder and the steps", err)
	}
	if calls.Load() != 0 {
		t.Errorf("the provider was called %d times beside a held lock, want 0", calls.Load())
	}
}

// TestReindex_WithNoEmbeddingModelRebuildsOnlyTheWordIndex: no embedding task
// bound, so there is no model to embed for; the word index is still rebuilt.
func TestReindex_WithNoEmbeddingModelRebuildsOnlyTheWordIndex(t *testing.T) {
	vault, _, _ := doctorVault(t, func(db *sqlite.Vault) {
		seedUnits(t, db, nil, "u-1", "u-2")
		sqlitetest.Exec(t, db, `INSERT INTO units_fts(units_fts, rowid, content) SELECT 'delete', rowid, content FROM units WHERE id = 'u-2'`)
	})
	var out bytes.Buffer
	if err := reindex(context.Background(), []string{vault}, strings.NewReader(""), &out, io.Discard); err != nil {
		t.Fatalf("reindex: %v", err)
	}
	wantLines(t, out.String(), "word index rebuilt", "no embedding model is configured")
	if _, _, gaps := reindexState(t, vault); gaps != (sqlite.SearchIndexGaps{}) {
		t.Errorf("word index gaps after reindex = %+v, want none", gaps)
	}
}

// TestReindex_AFailingProviderKeepsTheWorkAndSaysToRunAgain: a provider that
// refuses every unit stops the run after three; the word index is rebuilt
// anyway, the summary says what is left, and the exit is an error naming the
// rerun.
func TestReindex_AFailingProviderKeepsTheWorkAndSaysToRunAgain(t *testing.T) {
	var calls atomic.Int64
	vault, _, _ := doctorVault(t, func(db *sqlite.Vault) {
		seedUnits(t, db, map[string]string{"u-1": "model-a"}, "u-1", "u-2", "u-3", "u-4", "u-5")
		sqlitetest.Exec(t, db, `INSERT INTO units_fts(units_fts, rowid, content) SELECT 'delete', rowid, content FROM units WHERE id = 'u-2'`)
	})
	writeYML(t, vault, "providers:\n  local:\n    type: ollama\n    model: model-a\n    endpoint: "+fakeOllama(t, &calls, "note").URL+"\ntasks:\n  embedding:\n    provider: local\n")

	var out bytes.Buffer
	err := reindex(context.Background(), []string{"--yes", vault}, strings.NewReader(""), &out, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "run nooma reindex again") || !strings.Contains(err.Error(), "unavailable") {
		t.Errorf("reindex over a failing provider = %v, want an error naming the provider and the rerun", err)
	}
	if calls.Load() != 3 {
		t.Errorf("the provider was called %d times, want 3: a run stops at three refusals in a row", calls.Load())
	}
	wantLines(t, out.String(), "word index rebuilt", "embedded 0, skipped 1 already embedded, failed 3", "4 live units still lack a model-a vector")
	if _, _, gaps := reindexState(t, vault); gaps != (sqlite.SearchIndexGaps{}) {
		t.Errorf("word index gaps = %+v, want it rebuilt before the provider failed", gaps)
	}
}

// TestDoctor_FAILsNameTheShippedCommand (ADR-0059 point 6): neither FAIL says
// reindex is missing any more.
func TestDoctor_FAILsNameTheShippedCommand(t *testing.T) {
	for _, err := range []error{
		vaultCoverageError("model-a", 2, nil),
		searchIndexError(sqlite.SearchIndexGaps{Missing: 1}),
	} {
		if err == nil || strings.Contains(err.Error(), "not in this version") || !strings.Contains(err.Error(), reindexFix) {
			t.Errorf("doctor FAIL = %v, want it to name %q and nothing missing", err, reindexFix)
		}
	}
	// One unit is one unit (seen on a throwaway vault: "1 units are missing").
	for err, want := range map[error]string{
		vaultCoverageError("model-a", 1, nil):                "1 live unit has no model-a vector",
		searchIndexError(sqlite.SearchIndexGaps{Missing: 1}): "1 unit is missing from the word index",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("doctor FAIL = %q, want %q", err, want)
		}
	}
}

// TestReindex_AnInterruptedRunSaysSo: a run cancelled between units (Ctrl-C)
// keeps what it stored and says it was interrupted, not "context canceled".
func TestReindex_AnInterruptedRunSaysSo(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancel()
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "model-a", "embeddings": [][]float32{{1, 1}}})
	}))
	t.Cleanup(srv.Close)
	vault := reindexVaultAt(t, srv.URL)
	var out bytes.Buffer
	err := reindex(ctx, []string{"--yes", vault}, strings.NewReader(""), &out, io.Discard)
	if err == nil || err.Error() != "reindex interrupted; what was stored is kept, run nooma reindex again to continue" {
		t.Errorf("an interrupted reindex = %v, want it to say it was interrupted and how to go on", err)
	}
	wantLines(t, out.String(), "2 live units still lack a model-a vector")
}

// TestReindex_ARefusedUnitIsReportedAndTheRunFails: one unit refused, the
// others embedded; the summary names it and the exit is an error.
func TestReindex_ARefusedUnitIsReportedAndTheRunFails(t *testing.T) {
	var calls atomic.Int64
	vault := reindexVaultAt(t, fakeOllama(t, &calls, "u-bare").URL)
	var out bytes.Buffer
	err := reindex(context.Background(), []string{"--yes", vault}, strings.NewReader(""), &out, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "1 unit could not be embedded; run nooma reindex again") {
		t.Errorf("reindex with one refusal = %v, want an error naming it and the rerun", err)
	}
	wantLines(t, out.String(), "embedded 1, skipped 1 already embedded, failed 1", "last failure: unit u-bare", "1 live unit still lacks")
}

// TestReindex_SaysItsProgressEvery25Units (ADR-0003: "with progress"): a line
// at each 25 units and at the end, none between.
func TestReindex_SaysItsProgressEvery25Units(t *testing.T) {
	var calls atomic.Int64
	ids := make([]string, 27)
	for i := range ids {
		ids[i] = fmt.Sprintf("u-%02d", i)
	}
	vault, _, _ := doctorVault(t, func(db *sqlite.Vault) { seedUnits(t, db, nil, ids...) })
	writeYML(t, vault, "providers:\n  local:\n    type: ollama\n    model: model-a\n    endpoint: "+fakeOllama(t, &calls, "").URL+"\ntasks:\n  embedding:\n    provider: local\n")
	var out bytes.Buffer
	if err := reindex(context.Background(), []string{"--yes", vault}, strings.NewReader(""), &out, io.Discard); err != nil {
		t.Fatalf("reindex: %v", err)
	}
	wantLines(t, out.String(), "reindex: 25 of 27 done under model-a\nreindex: 27 of 27 done under model-a\n")
}

// TestReindex_UnitsLeftAfterACleanRunAreAnError: a unit that appears behind
// the run's cursor (written by another connection mid-run) is left; the run
// had no refusal and no error, and it still exits non-zero, so a run that
// stops short is never silent.
func TestReindex_UnitsLeftAfterACleanRunAreAnError(t *testing.T) {
	vault := reindexVaultAt(t, "")
	other := openVault(t, vault)
	var once sync.Once
	var lateErr error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() {
			lateErr = sqlite.NewUnitRepo(other).Create(context.Background(), unit.Unit{ID: "a-late", Type: unit.TypeKnowledge, Content: "late",
				Status: unit.StatusPool, Weight: 0.5, WeightDecayRate: 0.1, CreatedAt: doctorSeedAt, UpdatedAt: doctorSeedAt, LastTouchedAt: doctorSeedAt})
		})
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "model-a", "embeddings": [][]float32{{1, 1}}})
	}))
	t.Cleanup(srv.Close)
	writeYML(t, vault, "providers:\n  local:\n    type: ollama\n    model: model-a\n    endpoint: "+srv.URL+"\ntasks:\n  embedding:\n    provider: local\n")

	var out bytes.Buffer
	err := reindex(context.Background(), []string{"--yes", vault}, strings.NewReader(""), &out, io.Discard)
	if lateErr != nil {
		t.Fatalf("writing the late unit: %v", lateErr)
	}
	if err == nil || !strings.Contains(err.Error(), "1 live unit still lacks a model-a vector") {
		t.Errorf("reindex leaving a unit = %v, want an error naming what is left", err)
	}
	wantLines(t, out.String(), "embedded 2, skipped 1 already embedded, failed 0")
}
