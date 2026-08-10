package memrepo

import (
	"context"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// Config is an in-memory ports.ConfigRepo (plus
// repocontract.ConfigHarness's SeedConfig). The zero value is not usable —
// call NewConfig.
//
// TODO(PR3 GREEN): this is the RED-commit stub — Load always reports an
// absent row and RecordConsolidationRun is a no-op, deliberately failing
// repocontract.RunConfigRepoLoad/RunRecordConsolidationRun for the right
// reason (nothing is stored) rather than for a compile error. The GREEN
// commit in this same PR replaces every body below.
type Config struct{}

// Assert at compile time, following internal/store/sqlite/unitrepo.go:33's
// precedent.
var _ ports.ConfigRepo = (*Config)(nil)

// NewConfig returns an empty, ready-to-use in-memory ports.ConfigRepo.
// Every call returns an independent instance.
func NewConfig() *Config {
	return &Config{}
}

// Load implements ports.ConfigRepo. RED-commit stub: always reports an
// absent row.
func (c *Config) Load(_ context.Context) (ports.VaultConfig, error) {
	return ports.VaultConfig{}, nil
}

// RecordConsolidationRun implements ports.ConfigRepo. RED-commit stub: a
// no-op.
func (c *Config) RecordConsolidationRun(_ context.Context, _ time.Time) error {
	return nil
}

// SeedConfig implements repocontract.ConfigHarness. RED-commit stub: a
// no-op.
func (c *Config) SeedConfig(_ *testing.T, _ ports.VaultConfig) {}
