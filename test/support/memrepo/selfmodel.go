package memrepo

import (
	"context"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// SelfModel is an in-memory ports.SelfModelRepo. The zero value is not
// usable — call NewSelfModel.
//
// TODO(PR3 GREEN): this is the RED-commit stub — every method is a
// zero-value no-op, deliberately failing repocontract.RunActiveBeliefs et
// al. for the right reason (nothing is stored) rather than for a compile
// error. The GREEN commit in this same PR replaces every body below.
type SelfModel struct{}

// Assert at compile time, following internal/store/sqlite/unitrepo.go:33's
// precedent.
var _ ports.SelfModelRepo = (*SelfModel)(nil)

// NewSelfModel returns an empty, ready-to-use in-memory
// ports.SelfModelRepo. Every call returns an independent instance.
func NewSelfModel() *SelfModel {
	return &SelfModel{}
}

// ActiveBeliefs implements ports.SelfModelRepo. RED-commit stub: always
// empty.
func (s *SelfModel) ActiveBeliefs(_ context.Context) ([]ports.Belief, error) {
	return nil, nil
}

// UpsertByTopicKey implements ports.SelfModelRepo. RED-commit stub: a
// no-op.
func (s *SelfModel) UpsertByTopicKey(_ context.Context, _ ports.Belief) error {
	return nil
}

// ReinforceByID implements ports.SelfModelRepo. RED-commit stub: always
// reports not-found.
func (s *SelfModel) ReinforceByID(_ context.Context, _ string, _ float64, _ time.Time) error {
	return ports.ErrBeliefNotFound
}
