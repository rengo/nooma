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

// goalSignals answers GoalVerdicts with five ignored goal check-ins: enough
// to lengthen the interval.
type goalSignals struct{ ports.SignalRepo }

func (goalSignals) GoalVerdicts(context.Context) ([]ports.Signal, error) {
	var out []ports.Signal
	for i := range 5 {
		out = append(out, ports.Signal{ID: fmt.Sprintf("g-%d", i), Type: ports.SignalNudgeIgnored,
			Context: json.RawMessage(`{"belief_id":"b-run"}`), OccurredAt: learnTestNow.Add(-time.Duration(i+1) * 24 * time.Hour)})
	}
	return out, nil
}

// pinnedCadence refuses every learner write as pinned, counting them.
type pinnedCadence struct {
	ports.ConfigRepo
	writes int
}

func (p *pinnedCadence) LearnGoalStagnationDays(context.Context, int, time.Time) error {
	p.writes++
	return ports.ErrGoalStagnationPinned
}

// TestLearnGoalCadence_APinIsNeitherWrittenNorRecorded (the shape of #408's
// pin-guard test): read as pinned, nothing is sent to the store; pinned
// between the read and the write, the refusal is no change, with no error and
// no row.
func TestLearnGoalCadence_APinIsNeitherWrittenNorRecorded(t *testing.T) {
	for _, tc := range []struct {
		name       string
		pinned     bool
		wantWrites int
	}{
		{"read as pinned: never sent", true, 0},
		{"pinned between read and write: skipped", false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &pinnedCadence{}
			log := &recordingLog{}
			days := 21
			r := consolidateRunner{cfg: store, signals: goalSignals{}, log: log, ids: &countingIDs{}}
			err := r.learnGoalCadence(context.Background(), ports.VaultConfig{GoalStagnationDays: &days, GoalStagnationPinned: tc.pinned}, learnTestNow)
			if err != nil {
				t.Fatalf("learnGoalCadence = %v, want the pinned value skipped without error", err)
			}
			if store.writes != tc.wantWrites || len(log.actions) != 0 {
				t.Errorf("%d writes, rows %v; want %d writes and no row", store.writes, log.actions, tc.wantWrites)
			}
		})
	}
}
