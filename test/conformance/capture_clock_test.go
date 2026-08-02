// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// testdataLLMCasesDir returns testdata/llm/cases, resolved from the repo
// root rather than a relative path — repoRootFromCaller (store_api_test.go)
// is what keeps this stable regardless of the working directory `go test`
// is invoked from.
func testdataLLMCasesDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRootFromCaller(t), "testdata", "llm", "cases")
}

// countingClock is a ports.Clock that fails the test the instant Now() is
// called a second time during one Capture call — spec R4.1 and design D4
// Layer 1's own stated property: one capture sees exactly one instant.
//
// A t.Fatalf rather than a deferred count comparison, because the point is
// to catch the SECOND read as it happens: by the time Capture returns, a
// captureRunner that reached for a fresh instant mid-operation may already
// have used it to build an inconsistent unit, and a count checked only
// afterward would report the symptom, not the moment it went wrong.
type countingClock struct {
	t   *testing.T
	now time.Time
	n   int
}

func (c *countingClock) Now() time.Time {
	c.t.Helper()
	c.n++
	if c.n > 1 {
		c.t.Fatalf("ports.Clock.Now() called a second time during one Capture "+
			"call (call #%d) — captureRunner must receive now as a parameter, "+
			"never read the clock itself (spec R4.1, design D4 Layer 1)", c.n)
	}
	return c.now
}

// counterIDs is a deterministic ports.IDGen: "id-1", "id-2", ... in call
// order. A real UUID generator would work too, but a predictable one lets a
// test assert on the exact ID a capture produced without reading it back.
type counterIDs struct{ n int }

func (g *counterIDs) New() string {
	g.n++
	return fmt.Sprintf("id-%d", g.n)
}

// TestCapture_ReadsClockExactlyOnce is spec R4.1's own scenario, over
// design D4 Layer 1's clockless-worker shape: CaptureService is the only
// place in internal/brain holding a ports.Clock, and captureRunner —
// everything Capture delegates to — has no way to obtain one.
//
// This test does not assert anything about the persisted unit's fields;
// that is TestCapture_OrdinaryClassificationPersistsAUnit's job
// (capture_pipeline_test.go, spec R4.2). This one asserts a narrower,
// structural property, and asserts it even though the pipeline underneath
// happens to be the same one R4.2 exercises.
func TestCapture_ReadsClockExactlyOnce(t *testing.T) {
	clock := &countingClock{t: t, now: time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)}
	llm := fakeprovider.New(t, testdataLLMCasesDir(t), "classify-pick-up-dry-cleaning")
	embed := fakeprovider.NewEmbeddingFake(embedFakeModel)
	svc := brain.NewCaptureService(clock, &counterIDs{}, memrepo.NewUnits(), memrepo.NewEmbeddings(), memrepo.NewDecisionLog(), llm, embed)

	_, err := svc.Capture(context.Background(), brain.CaptureInput{
		Text:    "Pick up the dry cleaning on Friday",
		Channel: "chat",
	})
	if err != nil {
		t.Fatalf("Capture error = %v, want nil", err)
	}
	if clock.n != 1 {
		t.Fatalf("clock.Now() called %d times, want exactly 1", clock.n)
	}
}
