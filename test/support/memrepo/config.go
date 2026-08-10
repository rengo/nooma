package memrepo

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// Config is an in-memory ports.ConfigRepo (plus
// repocontract.ConfigHarness's SeedConfig). The zero value is not usable —
// call NewConfig. Two instances share no state, matching memrepo.Units's
// own isolation rule.
type Config struct {
	mu sync.Mutex
	// row is nil until first written — either by RecordConsolidationRun
	// (the port's own, only write, spec R2.6) or by SeedConfig (the
	// harness's direct-store hook, repocontract.ConfigHarness). Mirrors
	// design §3.4's "no migration seeds it" rule at the fake's own
	// zero-value level.
	row *ports.VaultConfig
}

// Assert at compile time, following internal/store/sqlite/unitrepo.go:33's
// precedent.
var _ ports.ConfigRepo = (*Config)(nil)

// NewConfig returns an empty, ready-to-use in-memory ports.ConfigRepo.
// Every call returns an independent instance.
func NewConfig() *Config {
	return &Config{}
}

// Load implements ports.ConfigRepo. Returns an all-nil VaultConfig when no
// row has been written yet; otherwise returns the stored row verbatim —
// including any corrupt value it holds (spec R2.4).
func (c *Config) Load(_ context.Context) (ports.VaultConfig, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.row == nil {
		return ports.VaultConfig{}, nil
	}
	return *c.row, nil
}

// RecordConsolidationRun implements ports.ConfigRepo. Lazily creates the
// row when absent, leaving every other field nil at this fake's own
// zero-value level (design §3.4: the real store leaves every other column
// to the migration's SQL DEFAULT, which this fake has none of to read —
// PR 6's sqlite implementation proves that half). When the row already
// exists, only ConsolidationLastRunAt changes.
func (c *Config) RecordConsolidationRun(_ context.Context, at time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.row == nil {
		c.row = &ports.VaultConfig{}
	}
	c.row.ConsolidationLastRunAt = &at
	return nil
}

// SeedConfig implements repocontract.ConfigHarness. Replaces the row
// wholesale — a fixture hook bypassing ports.ConfigRepo entirely, since
// RecordConsolidationRun cannot set any field but ConsolidationLastRunAt.
func (c *Config) SeedConfig(_ *testing.T, cfg ports.VaultConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.row = &cfg
}

// RowExists implements repocontract.ConfigHarness. Reports whether c.row
// has been written yet, independent of what Load returns — Load's
// zero-value VaultConfig is indistinguishable from an all-NULL stored row,
// which is exactly why repocontract.ConfigHarness needs this as a separate
// capability (see its doc comment on RowExists).
func (c *Config) RowExists(_ *testing.T) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.row != nil
}
