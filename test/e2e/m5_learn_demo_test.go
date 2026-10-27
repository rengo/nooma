//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/relation"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/store/sqlite"
)

// TestM5Demo_TheCompiledLearnPassRaisesTheBar is M5's demo through the
// compiled binary (docs/05-build-plan.md §M5, I48): a vault whose user
// rejected 6 of the last 8 same_topic links Nooma made, `nooma consolidate
// --phase=learn`, and the type's bar is 0.35 with one decision_log row saying
// why. It is what proves cmd/nooma hands the learn slot the real signal store:
// wired with none, the pass would run and learn nothing.
func TestM5Demo_TheCompiledLearnPassRaisesTheBar(t *testing.T) {
	llm := mockConsolidateLLM(t)
	home, work := t.TempDir(), t.TempDir()
	vault := initVault(t, home, work, "pablo.nooma")
	writeConfig(t, vault, consolidateConfig(llm.URL))

	ctx := context.Background()
	db, err := sqlite.Open(ctx, vaultDBPath(t, vault))
	if err != nil {
		t.Fatal(err)
	}
	signals := sqlite.NewSignalRepo(db)
	judged := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	relType, kind := "same_topic", ports.TargetKindRelation
	for i := range 8 {
		typ, valence := ports.SignalRelationReject, ports.ValenceNegative
		if i >= 6 {
			typ, valence = ports.SignalRelationConfirm, ports.ValencePositive
		}
		target := fmt.Sprintf("rel-%d", i)
		if err := signals.Record(ctx, ports.Signal{ID: fmt.Sprintf("sig-%d", i), Type: typ, Valence: valence,
			TargetKind: &kind, TargetID: &target, RelationType: &relType,
			Context:    json.RawMessage(`{"created_by":"consolidation","confidence":0.4}`),
			OccurredAt: judged.Add(time.Duration(i) * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	_ = db.Close()

	for run := range 2 {
		if stdout, stderr, err := nooma(t, home, work, "consolidate", "--phase=learn", vault); err != nil {
			t.Fatalf("consolidate --phase=learn (run %d): %v\nstdout: %s\nstderr: %s", run+1, err, stdout, stderr)
		}
	}

	db, err = sqlite.Open(ctx, vaultDBPath(t, vault))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	row, err := sqlite.NewRelationRepo(db).ThresholdsFor(ctx, relType)
	if err != nil {
		t.Fatal(err)
	}
	if got := relation.Resolve(row); got != (relation.Thresholds{Persist: 0.35, Surface: 0.50}) {
		t.Errorf("same_topic bar = %+v, want 0.35/0.50", got)
	}
	var learned []ports.Decision
	for _, d := range readDecisionLog(t, vault) {
		if d.Action == ports.ActionLearnRelationThreshold {
			learned = append(learned, d)
		}
	}
	if len(learned) != 1 || !strings.Contains(learned[0].Rationale, "6 of the last 8") {
		t.Errorf("learn rows = %+v, want one, saying 6 of the last 8, after two runs", learned)
	}
}
