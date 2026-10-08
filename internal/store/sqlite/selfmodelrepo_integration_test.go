//go:build integration

package sqlite

import (
	"context"
	"testing"

	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/repocontract"
)

// TestSelfModelRepo_ActiveBeliefs, TestSelfModelRepo_UpsertByTopicKey and
// TestSelfModelRepo_ReinforceByID run the same repocontract suites
// test/support/memrepo/selfmodel.go already answers at L2 (PR 3), now
// against a real temporary SQLite vault at L3 — design D6's "answered
// twice" standing rule.
func TestSelfModelRepo_ActiveBeliefs(t *testing.T) {
	repocontract.RunActiveBeliefs(t, func(t *testing.T) ports.SelfModelRepo {
		return NewSelfModelRepo(openTestVault(t))
	})
}

func TestSelfModelRepo_UpsertByTopicKey(t *testing.T) {
	repocontract.RunUpsertByTopicKey(t, func(t *testing.T) ports.SelfModelRepo {
		return NewSelfModelRepo(openTestVault(t))
	})
}

func TestSelfModelRepo_ReinforceByID(t *testing.T) {
	repocontract.RunReinforceByID(t, func(t *testing.T) ports.SelfModelRepo {
		return NewSelfModelRepo(openTestVault(t))
	})
}

func TestSelfModelRepo_RetiredBeliefs(t *testing.T) {
	repocontract.RunRetiredBeliefs(t, func(t *testing.T) ports.SelfModelRepo {
		return NewSelfModelRepo(openTestVault(t))
	})
}

func TestSelfModelRepo_BeliefByID(t *testing.T) {
	repocontract.RunBeliefByID(t, func(t *testing.T) ports.SelfModelRepo {
		return NewSelfModelRepo(openTestVault(t))
	})
}

func TestSelfModelRepo_SetStatus(t *testing.T) {
	repocontract.RunSetStatus(t, func(t *testing.T) ports.SelfModelRepo {
		return NewSelfModelRepo(openTestVault(t))
	})
}

func TestSelfModelRepo_EditContent(t *testing.T) {
	repocontract.RunEditContent(t, func(t *testing.T) ports.SelfModelRepo {
		return NewSelfModelRepo(openTestVault(t))
	})
}

func TestSelfModelRepo_WriteGuards(t *testing.T) {
	repocontract.RunBeliefWriteGuards(t, func(t *testing.T) ports.SelfModelRepo {
		return NewSelfModelRepo(openTestVault(t))
	})
}

// TestSelfModelRepo_DoUpdateWhereReportsChanges is design RK-1's probe, the
// first test written for the upsert guard: the guard maps "zero rows
// affected" to ports.ErrBeliefProtected, which is sound only if SQLite (and
// this driver) report 0 changes when an ON CONFLICT ... DO UPDATE ... WHERE
// clause is false, and 1 when it is true. It talks to the vault directly,
// below the repo, so it pins the engine's behaviour and not the guard.
func TestSelfModelRepo_DoUpdateWhereReportsChanges(t *testing.T) {
	v := openTestVault(t)
	ctx := context.Background()

	const insert = `INSERT INTO self_beliefs (id, facet, topic_key, content, confidence, origin,
		status, last_reinforced_at, created_at, updated_at)
		VALUES (?, 'goal', 'probe/key', ?, 0.5, 'derived', 'active', ?, ?, ?)`
	const at = "2026-08-01T12:00:00Z"
	if _, err := v.db.ExecContext(ctx, insert, "probe-1", "original", at, at, at); err != nil {
		t.Fatalf("seed probe row: %v", err)
	}

	upsert := func(whereClause string) int64 {
		t.Helper()
		res, err := v.db.ExecContext(ctx, insert+` ON CONFLICT (topic_key) DO UPDATE SET content = excluded.content
			WHERE `+whereClause, "probe-2", "overwritten", at, at, at)
		if err != nil {
			t.Fatalf("upsert WHERE %s: %v", whereClause, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			t.Fatalf("RowsAffected: %v", err)
		}
		return n
	}
	content := func() string {
		t.Helper()
		var c string
		if err := v.db.QueryRowContext(ctx, `SELECT content FROM self_beliefs WHERE topic_key = 'probe/key'`).Scan(&c); err != nil {
			t.Fatalf("read probe row: %v", err)
		}
		return c
	}

	if n := upsert("self_beliefs.status = 'retired'"); n != 0 {
		t.Errorf("DO UPDATE with a false WHERE reported %d rows affected, want 0 — the guard's whole premise (design RK-1)", n)
	}
	if got := content(); got != "original" {
		t.Errorf("content after a false-WHERE upsert = %q, want %q", got, "original")
	}
	if n := upsert("self_beliefs.status = 'active'"); n != 1 {
		t.Errorf("DO UPDATE with a true WHERE reported %d rows affected, want 1", n)
	}
	if got := content(); got != "overwritten" {
		t.Errorf("content after a true-WHERE upsert = %q, want %q", got, "overwritten")
	}
}
