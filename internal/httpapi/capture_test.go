package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// embedFakeModel matches test/conformance's own constant name and value —
// this package needs its own copy because Go test helpers are not shared
// across packages without an exported support package, and this file's
// wiring is otherwise a direct copy of test/conformance's own
// brain.NewCaptureService pattern (i04_timer_never_a_unit_test.go).
const embedFakeModel = "conformance-embed-fake-v1"

// fixedClock and counterIDs mirror test/conformance's own fakes
// (capture_clock_test.go): a deterministic ports.Clock and ports.IDGen, so
// this package's handler tests need no real time or random source
// (CLAUDE.md non-negotiable #3, docs/06-harness.md §2).
type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type counterIDs struct{ n int }

func (g *counterIDs) New() string {
	g.n++
	return fmt.Sprintf("id-%d", g.n)
}

// testdataLLMCasesDir resolves testdata/llm/cases from the repo root,
// regardless of the working directory `go test` runs from — the same
// resolution test/conformance's own repoRootFromCaller performs, restated
// here because internal/httpapi/*_test.go sits two directories below the
// repo root, not three.
func testdataLLMCasesDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed to report this test file's path")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "testdata", "llm", "cases")
}

// newTestCaptureService builds a *brain.CaptureService over memrepo and a
// fakeprovider script — the same wiring test/conformance/i04_timer_never_a_unit_test.go
// uses, restated here because internal/httpapi may not import test/conformance
// (docs/06-harness.md §1's dependency rule runs adapter -> brain -> core,
// never adapter -> test).
func newTestCaptureService(t *testing.T, now time.Time, llmCase string) *brain.CaptureService {
	t.Helper()
	ctx := context.Background()

	units := memrepo.NewUnits()
	decisions := memrepo.NewDecisionLog()
	embeddings := memrepo.NewEmbeddings()
	lexical := memrepo.NewLexical()
	relations := memrepo.NewRelations()
	llm := fakeprovider.New(t, testdataLLMCasesDir(t), llmCase)
	embed := fakeprovider.NewEmbeddingFake(embedFakeModel)

	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
	}

	return brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, units, embeddings, lexical, relations, decisions, llm, llm, embed, brain.NewIndex(idx), memrepo.NewSignals())
}

func postCapture(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/capture", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestCaptureHandler_StoresAnOrdinaryCapture is R2.1's own L2 handler test:
// an ordinary capture through POST /capture reflects a successfully-stored
// outcome.
func TestCaptureHandler_StoresAnOrdinaryCapture(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	svc := newTestCaptureService(t, now, "classify-pick-up-dry-cleaning")
	h := Handler(Deps{Version: "test", Capture: svc})

	rec := postCapture(t, h, `{"text": "Pick up the dry cleaning on Friday"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding response: %v; body = %s", err, rec.Body.String())
	}
	if got["outcome"] != string(brain.OutcomeStored) {
		t.Errorf("outcome = %v, want %q", got["outcome"], brain.OutcomeStored)
	}
	if uid, _ := got["unit_id"].(string); uid == "" {
		t.Errorf("unit_id is empty, want a persisted unit's id")
	}
}

// TestCaptureHandler_UnwiredCaptureServiceReturns503 pins the fix for the
// nil-dependency gap the coordinator caught: cmd/nooma/serve.go leaves
// Deps.Capture nil until 13d wires providers/repos/Index/services into it.
// An authenticated POST /capture reaching a build in that transitional state
// must not panic the process (a live, trivial denial of service against
// anyone building main in that window) — it must answer honestly that this
// endpoint is not wired in this build. 503, not 500 (nothing went wrong) and
// not 404 (the route exists), with no more detail than that.
func TestCaptureHandler_UnwiredCaptureServiceReturns503(t *testing.T) {
	t.Parallel()

	h := Handler(Deps{Version: "test"}) // Capture is nil, deliberately.

	rec := postCapture(t, h, `{"text": "irrelevant — Capture is nil, this must never reach it"}`)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding response: %v; body = %s", err, rec.Body.String())
	}
	if _, ok := got["error"]; !ok {
		t.Errorf("response has no error field: %s", rec.Body.String())
	}
}

// TestCaptureHandler_TimerRefusalSurfacesPlainWordsVerbatim is R2.2's own L2
// test: a timer-classified message's refusal is distinguishable from every
// other outcome and carries the refusal's plain-words message verbatim —
// never merely a status code standing in for it.
func TestCaptureHandler_TimerRefusalSurfacesPlainWordsVerbatim(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	svc := newTestCaptureService(t, now, "classify-timer-set-a-timer")
	h := Handler(Deps{Version: "test", Capture: svc})

	rec := postCapture(t, h, `{"text": "set a timer for 10 minutes"}`)

	// A deferred refusal is Q3a's honest answer, not an error — it must
	// never surface as a 4xx/5xx (design D10: "a deferred timer is NOT an
	// error status").
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (a refusal is not an error); body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding response: %v; body = %s", err, rec.Body.String())
	}
	if got["outcome"] != string(brain.OutcomeDeferred) {
		t.Errorf("outcome = %v, want %q", got["outcome"], brain.OutcomeDeferred)
	}

	const wantMessage = "timers and recurring reminders aren't wired up yet — nothing was scheduled, and this capture was not stored."
	if got["message"] != wantMessage {
		t.Errorf("message = %q, want the refusal's plain-words message verbatim: %q", got["message"], wantMessage)
	}
}

// TestAllCaptureOutcomesHaveAStatusMapping is R2.1's own completeness test:
// the route's status mapping is a total switch over brain.AllCaptureOutcomes(),
// and this test fails loudly if a member is ever added with no mapping —
// renderCaptureResult's own switch carries no default clause, so a new,
// unhandled outcome falls through to the zero status this test catches.
func TestAllCaptureOutcomesHaveAStatusMapping(t *testing.T) {
	t.Parallel()

	checked := 0

	for _, outcome := range brain.AllCaptureOutcomes() {
		result := brain.CaptureResult{Outcome: outcome}

		// Outcomes that carry a required pointer field need it non-nil, the
		// same way captureRunner's own production code always sets it for
		// that outcome (result.go's own field comments).
		switch outcome {
		case brain.OutcomeDeferred:
			result.Deferred = &brain.Deferred{Message: "placeholder"}
		case brain.OutcomeCorrected, brain.OutcomeAsked:
			result.Correction = &brain.Correction{}
		}

		status, body := renderCaptureResult(result)
		if status == 0 {
			t.Errorf("outcome %q has no status mapping (renderCaptureResult returned status 0) — the total switch is missing a case", outcome)
			continue
		}
		// R2.1's own MUST: no two distinct outcomes are indistinguishable at
		// this boundary. The status code alone does not have to be unique
		// (design D10: several outcomes legitimately share 200) — the body's
		// own "outcome" discriminator is what a caller actually switches on.
		if body.Outcome != string(outcome) {
			t.Errorf("outcome %q rendered a body whose own Outcome field reads %q — the discriminator must name the outcome it renders", outcome, body.Outcome)
		}
		checked++
	}

	if checked != len(brain.AllCaptureOutcomes()) {
		t.Fatalf("checked %d outcomes, want %d — brain.AllCaptureOutcomes() and this test's coverage have drifted apart", checked, len(brain.AllCaptureOutcomes()))
	}
}
