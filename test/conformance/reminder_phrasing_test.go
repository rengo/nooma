package conformance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/reading"
	"github.com/rengo/nooma/test/support/goldenset"
)

// TestReminderPhrasingSetReadsAsExpected replays every recorded phrasing of
// testdata/llm/reminders/ against its hand-written expectation (ADR-0033
// point 5): the recording is rebuilt into its prompt and must hash to the
// prompt_sha256 it was made against, so a prompt change fails here and names
// the cases to record again. A case still awaiting its recording is skipped
// and counted: the merge gate for those is scripts/recordings-complete.sh,
// not this test.
func TestReminderPhrasingSetReadsAsExpected(t *testing.T) {
	dir := filepath.Join(repoRootFromCaller(t), "testdata", "llm", "reminders")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	awaiting, total := 0, 0
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		total++
		var ex goldenset.LLMExample
		if err := goldenset.Load(filepath.Join(dir, entry.Name()), &ex); err != nil {
			t.Errorf("%s: %v", entry.Name(), err)
			continue
		}
		c := ex.Case()
		if want := strings.TrimSuffix(entry.Name(), ".json"); c.ID != want {
			t.Errorf("%s holds id %q: a case's file is named by its id", entry.Name(), c.ID)
		}
		if err := reading.ValidateCase(c); err != nil {
			t.Errorf("%s: %v", c.ID, err)
		}
		t.Run(c.ID, func(t *testing.T) {
			if ex.Record {
				awaiting++
				t.Skip("awaiting recording (nooma doctor --record)")
			}
			if err := reading.Replay(c, ex.Response); err != nil {
				t.Error(err)
			}
		})
	}
	t.Logf("reminder phrasing set: %d case(s), %d awaiting recording", total, awaiting)
}
