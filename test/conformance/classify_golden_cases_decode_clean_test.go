// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/test/support/goldenset"
)

// TestClassifyGoldenCasesDecodeClean mechanizes one line of
// testdata/classify/format.md's "Not checked" list: that a case's stated
// answer is one the decoder can actually produce.
//
// Every clean case's `expected` block says what classify MUST produce for
// its input, and nothing verified the stated shape was producible. The
// golden-set tests check the format documentation and that the directories
// are not empty; TestI14_ClassifyFieldDegradesToNull reads only the cases
// naming an llm/ recording. A clean case could therefore declare a value
// outside a closed vocabulary and the whole suite stayed green until a live
// provider was asked to satisfy it.
//
// **The two tests partition the directory.** I14 takes the cases WITH an
// llm_case_id — the deliberately malformed ones, whose `expected` mirrors
// what the model said rather than what classify produces, and which exist
// precisely to hold values the decoder rejects. This one takes the rest.
// Neither reads the other's cases, and the count assertion below is what
// keeps a case from falling between them.
//
// # What this does not catch, stated because it is the interesting half
//
// It does not catch a case that answers a question by AVOIDING a field.
// testdata/classify/cases/recurring-reminder-water-plants put a weekly
// recurrence into structured_data as an iCal RRULE for months, because the
// vocabulary was yearly|monthly and had no way to say weekly. That fixture
// passes this test and always would have: structured_data is free-form
// JSON, so nothing about the bytes is illegal. What was wrong with it was a
// judgement — the recurrence belonged in a decoded field — and judgement is
// what format.md's review asks for and gets.
//
// What this DOES catch is the other direction, and it is the one that
// arrives without anybody doing anything wrong: a vocabulary that moves
// under fixtures already written. Remove a member, or write a case by hand
// with a guessed value, and every stale case fails here in milliseconds
// rather than in `nooma doctor` against a paid API.
func TestClassifyGoldenCasesDecodeClean(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	casesDir := filepath.Join(repoRoot, "testdata", "classify", "cases")

	entries, err := os.ReadDir(casesDir)
	if err != nil {
		t.Fatalf("reading %s: %v", casesDir, err)
	}

	checked, skipped, total := 0, 0, 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		var example goldenset.ClassifyExample
		path := filepath.Join(casesDir, entry.Name())
		if err := goldenset.Load(path, &example); err != nil {
			t.Fatalf("loading %s: %v", path, err)
		}
		total++

		// A case naming an llm/ recording is a deliberately malformed one,
		// and its `expected` mirrors the wire bytes rather than classify's
		// output — "type": "grocery" is the point of
		// degraded-unknown-enum-value, not a defect in it. Those belong to
		// TestI14_ClassifyFieldDegradesToNull, which reads exactly the cases
		// this one skips.
		if example.LLMCaseID != "" {
			skipped++
			continue
		}
		checked++

		t.Run(entry.Name(), func(t *testing.T) {
			// Marshalling `expected` back to the wire rather than reading
			// the raw llm/ recording, deliberately: most cases have no
			// recording, and the claim under test belongs to the case file
			// either way. ClassifyExpected's JSON tags ARE the wire names,
			// so this round-trip is the case's own bytes as the decoder
			// would meet them.
			raw, err := json.Marshal(example.Expected)
			if err != nil {
				t.Fatalf("marshalling expected: %v", err)
			}

			c, err := classify.Decode(string(raw), corpusNow)
			if err != nil {
				t.Fatalf("expected does not decode at all: %v\n%s", err, raw)
			}

			for _, d := range c.Degradations {
				t.Errorf("expected.%s degrades as %q — this case states an answer the decoder "+
					"rejects, so it can never be satisfied by any model. Either the value is "+
					"outside its vocabulary, or the vocabulary moved and the case did not.\n%s",
					d.Field, d.Reason, raw)
			}
		})
	}

	// A directory that stopped being read would otherwise report success.
	if checked == 0 {
		t.Fatalf("no clean classify cases found in %s — this test proved nothing", casesDir)
	}
	// The partition has to be total: a case read by neither test is a case
	// nothing checks, which is the state this test exists to end.
	if checked+skipped != total {
		t.Fatalf("checked %d + skipped %d != %d cases — some case is read by neither this "+
			"test nor TestI14_ClassifyFieldDegradesToNull", checked, skipped, total)
	}
}
