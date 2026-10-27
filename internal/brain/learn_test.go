package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/relation"
	"github.com/rengo/nooma/internal/ports"
)

var learnTestNow = time.Date(2026, 10, 28, 3, 0, 0, 0, time.UTC)

// verdictSignals answers RelationVerdicts with six typed same_topic
// rejections of Nooma's relations: enough to raise the bar.
type verdictSignals struct{ ports.SignalRepo }

func (verdictSignals) RelationVerdicts(context.Context) ([]ports.Signal, error) {
	typ := "same_topic"
	var out []ports.Signal
	for i := range 6 {
		out = append(out, ports.Signal{ID: fmt.Sprintf("s-%d", i), Type: ports.SignalRelationReject, RelationType: &typ,
			Context: json.RawMessage(`{"created_by":"consolidation"}`), OccurredAt: learnTestNow.Add(-time.Duration(i+1) * time.Hour)})
	}
	return out, nil
}

// pinStore reports same_topic's row with origin on read and refuses every
// write as pinned, counting the writes attempted.
type pinStore struct {
	ports.RelationRepo
	origin ports.ThresholdOrigin
	writes int
}

func (p *pinStore) LearnedThresholds(context.Context) ([]ports.LearnedThreshold, error) {
	return []ports.LearnedThreshold{{RelationType: "same_topic", Origin: p.origin,
		Thresholds: relation.Thresholds{Persist: 0.30, Surface: 0.50}}}, nil
}

func (p *pinStore) LearnThresholds(context.Context, string, relation.Thresholds, time.Time) error {
	p.writes++
	return ports.ErrThresholdsPinned
}

// TestLearn_APinnedRowIsNeitherWrittenNorRecorded (#408 review): the brain's
// two pin guards, each alone. A row read as the user's is never sent to the
// store. A row read as learned but pinned before the write (the store
// refuses it) is skipped as no change: no error that would abort the pass,
// and no decision_log row, because nothing changed (I12).
func TestLearn_APinnedRowIsNeitherWrittenNorRecorded(t *testing.T) {
	for _, tc := range []struct {
		name       string
		origin     ports.ThresholdOrigin
		wantWrites int
	}{
		{"read as the user's: never sent", ports.ThresholdOriginUser, 0},
		{"pinned between read and write: skipped", ports.ThresholdOriginLearned, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &pinStore{origin: tc.origin}
			log := &recordingLog{}
			r := consolidateRunner{rels: store, signals: verdictSignals{}, log: log, ids: &countingIDs{}}
			if err := r.learn(context.Background(), learnTestNow); err != nil {
				t.Fatalf("learn = %v, want the pinned type skipped without error", err)
			}
			if store.writes != tc.wantWrites {
				t.Errorf("%d writes attempted, want %d", store.writes, tc.wantWrites)
			}
			if len(log.actions) != 0 {
				t.Errorf("rows %v recorded for a type that did not change", log.actions)
			}
		})
	}
}
