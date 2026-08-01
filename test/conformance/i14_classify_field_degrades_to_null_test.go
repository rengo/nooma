// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/test/support/goldenset"
)

// corpusZone is fixed rather than the machine's: a date-only value in a case
// parses to midnight in the location Decode is handed, and a test reading the
// OS zone would pass in one timezone and fail in another.
var corpusZone = time.FixedZone("corpus-03", -3*60*60)

// corpusNow is the instant every Decode call in this file receives. Only its
// location matters (design D2, conflict C7).
var corpusNow = time.Date(2026, 8, 1, 12, 0, 0, 0, corpusZone)

// degradationShapes maps a case-id prefix to the Reason that case exists to
// prove. testdata/classify/format.md's "What makes a good case" names exactly
// these three malformed shapes, and a case file announces which one it is in
// its own name — so the corpus stays readable as a directory listing, and a
// case cannot quietly stop testing what its name claims.
var degradationShapes = map[string]classify.Reason{
	"degraded-truncated-response": classify.ReasonTruncated,
	"degraded-wrong-typed-field":  classify.ReasonWrongType,
	"degraded-unknown-enum-value": classify.ReasonUnknownEnum,
}

// TestI14_ClassifyFieldDegradesToNull is I14's proof over real recorded
// responses — doc 02 §5: "a malformed field degrades to null [...] it never
// brings down the whole classification".
//
// Distinct from 7a.4's L1 tables, which drive Decode over inline literals.
// Those carry the coverage numerator and cover shapes exhaustively; this one
// proves the same invariant holds for bytes a provider actually produced,
// recorded in testdata/llm/. A hand-written literal can accidentally be
// easier than reality — the recordings cannot.
//
// L2 rather than L1 because it reads the corpus off disk, and depguard denies
// os to internal/core/** with no $test selector (design D11), so this cannot
// live inside internal/core/classify even as a _test.go file.
func TestI14_ClassifyFieldDegradesToNull(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	casesDir := filepath.Join(repoRoot, "testdata", "classify", "cases")

	entries, err := os.ReadDir(casesDir)
	if err != nil {
		t.Fatalf("reading %s: %v", casesDir, err)
	}

	seenShapes := map[classify.Reason]bool{}
	backed := 0

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		var example goldenset.ClassifyExample
		path := filepath.Join(casesDir, entry.Name())
		if err := goldenset.Load(path, &example); err != nil {
			t.Fatalf("loading %s: %v", path, err)
		}

		// Only cases naming an llm/ recording carry a raw response to
		// decode. A clean case states what classify must produce; it has no
		// wire bytes of its own, and this test is about the wire.
		if example.LLMCaseID == "" {
			continue
		}
		backed++

		t.Run(entry.Name(), func(t *testing.T) {
			wantReason, named := degradationShapes[example.ID]
			if !named {
				t.Fatalf("case %q names an llm recording but no degradation shape — every "+
					"backed case must declare which of format.md's three malformed shapes it "+
					"proves, by its id", example.ID)
			}

			raw := loadRecordedResponse(t, repoRoot, example.LLMCaseID)

			c, err := classify.Decode(raw, corpusNow)
			if err != nil {
				t.Fatalf("Decode returned %v — a malformed field must degrade, not bring "+
					"down the whole classification (I14)", err)
			}

			if len(c.Degradations) == 0 {
				t.Fatalf("the recorded response decoded with no degradations at all; this "+
					"case exists to prove %q is detected, so either the recording is no "+
					"longer malformed or the decoder stopped noticing", wantReason)
			}

			var got []string
			matched := false
			for _, d := range c.Degradations {
				got = append(got, string(d.Reason))
				if d.Reason == wantReason {
					matched = true
				}
			}
			if !matched {
				t.Errorf("degraded with %v, want at least one %q — the case id names that shape",
					got, wantReason)
			}
			seenShapes[wantReason] = true

			// I14's other half, and the one worth the corpus: the rest of
			// the classification survived. Every backed case's recording is
			// malformed in exactly one place, so type and normalized_content
			// must still arrive — except where the defect IS the type.
			if c.NormalizedContent == nil {
				t.Error("normalized_content did not survive — one malformed field brought " +
					"down a field it has no relationship to (I14)")
			}
			if wantReason != classify.ReasonUnknownEnum && c.Kind == nil {
				t.Error("type did not survive a defect in another field (I14)")
			}
		})
	}

	// The corpus must actually contain all three shapes. Without this,
	// deleting a case file would quietly shrink what I14 is proven against
	// and every remaining subtest would still pass.
	if backed == 0 {
		t.Fatal("no case names an llm recording — I14 has nothing to prove itself against, " +
			"and this test would have passed vacuously")
	}
	for _, want := range []classify.Reason{
		classify.ReasonTruncated, classify.ReasonWrongType, classify.ReasonUnknownEnum,
	} {
		if !seenShapes[want] {
			t.Errorf("no case in the corpus proves %q; format.md requires one per malformed "+
				"shape", want)
		}
	}
}

// TestClassifyCorpusCoversEveryTaxonomyValue mechanizes what
// testdata/classify/format.md calls a review-time concern.
//
// format.md says enum membership is "a review-time concern, not a mechanized
// one", and that was true when nothing in Go held the taxonomy. classify.Kind
// now does, and docs/06-harness.md §6's own precedence rule applies: if a rule
// can be an automated gate, it is a gate. Spec R1.5 requires at least one case
// per taxonomy value including timer, recurring_reminder, chitchat and
// out_of_scope — a reviewer counting thirteen values across seventeen files by
// eye is exactly the check that silently stops happening.
//
// It also catches the reverse: a case whose type is not a taxonomy value at
// all, which the loader deliberately does not reject.
func TestClassifyCorpusCoversEveryTaxonomyValue(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	casesDir := filepath.Join(repoRoot, "testdata", "classify", "cases")

	entries, err := os.ReadDir(casesDir)
	if err != nil {
		t.Fatalf("reading %s: %v", casesDir, err)
	}

	covered := map[classify.Kind]bool{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		var example goldenset.ClassifyExample
		path := filepath.Join(casesDir, entry.Name())
		if err := goldenset.Load(path, &example); err != nil {
			t.Fatalf("loading %s: %v", path, err)
		}

		kind := classify.Kind(example.Expected.Type)
		if !isTaxonomyValue(kind) {
			// The unknown-enum case is required to carry one, and says so
			// in its name. Anything else is a typo the loader lets through.
			if !strings.Contains(example.ID, "unknown-enum") {
				t.Errorf("%s: type %q is not a taxonomy value (doc 02 §5). Only a case whose "+
					"id says unknown-enum may carry one", entry.Name(), example.Expected.Type)
			}
			continue
		}
		covered[kind] = true
	}

	for _, k := range classify.AllKinds() {
		if !covered[k] {
			t.Errorf("no case in testdata/classify/cases/ exercises type %q — spec R1.5 "+
				"requires every one of the thirteen, and the corpus is shared with nooma "+
				"doctor's structured-JSON gate (ADR-0002), which needs a provider proven "+
				"capable of producing all of them", k)
		}
	}
}

func isTaxonomyValue(k classify.Kind) bool {
	for _, known := range classify.AllKinds() {
		if k == known {
			return true
		}
	}
	return false
}

// loadRecordedResponse returns the raw response text of a testdata/llm/ case,
// failing if the reference dangles. format.md notes that llm_case_id resolving
// to a real file is not checked by the loader; for the cases this test drives,
// it is checked here.
func loadRecordedResponse(t *testing.T, repoRoot, llmCaseID string) string {
	t.Helper()

	path := filepath.Join(repoRoot, "testdata", "llm", "cases", llmCaseID+".json")
	var recording goldenset.LLMExample
	if err := goldenset.Load(path, &recording); err != nil {
		t.Fatalf("loading the recording %q named by llm_case_id: %v — the reference dangles, "+
			"which the goldenset loader does not check", llmCaseID, err)
	}
	if recording.Response == "" {
		t.Fatalf("recording %q has no response; a case proving a malformed response needs "+
			"one (an Error-only recording proves a transport failure, a different thing)",
			llmCaseID)
	}
	return recording.Response
}
