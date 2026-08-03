package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// SignalRepo is the SQLite implementation of ports.SignalRepo, over
// learning_signals (migration 0002:8-19) — design D6's "answered twice"
// standing rule: the same repocontract.RunSignalRepo suite that runs
// against test/support/memrepo's fake at L2 runs against this type at L3,
// over a real migrated vault. It adds no migration: the table, and its
// deliberately FK-less target_id column, already exist.
type SignalRepo struct {
	db *sql.DB
}

// NewSignalRepo returns a ports.SignalRepo backed by v's already-migrated
// vault.
func NewSignalRepo(v *Vault) *SignalRepo {
	return &SignalRepo{db: v.db}
}

var _ ports.SignalRepo = (*SignalRepo)(nil)

// Record implements ports.SignalRepo. A nil/empty Context omits the
// context column from the INSERT rather than binding NULL, the same
// reasoning DecisionLog.Record uses: the column is NOT NULL DEFAULT '{}'
// (migration 0002:17), so leaving it out is what lets the SQL DEFAULT
// supply the value.
//
// target_kind, target_id, decision_action, relation_type and magnitude are
// all nullable columns (migration 0002:12-16), bound as SQL NULL when the
// corresponding pointer is nil. No application-side check stands in for
// target_id's deliberate absence of a foreign key (I13) — a target that
// never existed inserts exactly like one that did.
func (r *SignalRepo) Record(ctx context.Context, s ports.Signal) error {
	var err error
	if len(s.Context) == 0 {
		_, err = r.db.ExecContext(ctx, `
INSERT INTO learning_signals
	(id, signal_type, valence, target_kind, target_id, decision_action, relation_type, magnitude, occurred_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			s.ID, string(s.Type), string(s.Valence), targetKindToNull(s.TargetKind),
			stringPtrToNull(s.TargetID), decisionActionToNull(s.DecisionAction),
			stringPtrToNull(s.RelationType), floatPtrToNull(s.Magnitude),
			s.OccurredAt.UTC().Format(unitTimeLayout),
		)
	} else {
		_, err = r.db.ExecContext(ctx, `
INSERT INTO learning_signals
	(id, signal_type, valence, target_kind, target_id, decision_action, relation_type, magnitude, context, occurred_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			s.ID, string(s.Type), string(s.Valence), targetKindToNull(s.TargetKind),
			stringPtrToNull(s.TargetID), decisionActionToNull(s.DecisionAction),
			stringPtrToNull(s.RelationType), floatPtrToNull(s.Magnitude),
			string(s.Context), s.OccurredAt.UTC().Format(unitTimeLayout),
		)
	}
	if err != nil {
		return fmt.Errorf("recording signal %q: %w", s.ID, err)
	}
	return nil
}

// Since implements ports.SignalRepo. occurred_at > ? is a strict bound,
// matching DecisionLog.Since's own reasoning. ORDER BY occurred_at, id
// gives a deterministic order across rows sharing one occurred_at value —
// the column carries no uniqueness constraint.
func (r *SignalRepo) Since(ctx context.Context, t time.Time, limit int) ([]ports.Signal, error) {
	const q = `
SELECT id, signal_type, valence, target_kind, target_id, decision_action, relation_type, magnitude, context, occurred_at
FROM learning_signals
WHERE occurred_at > ?
ORDER BY occurred_at, id
LIMIT ?`

	rows, err := r.db.QueryContext(ctx, q, t.UTC().Format(unitTimeLayout), limit)
	if err != nil {
		return nil, fmt.Errorf("reading signals since %s: %w", t, err)
	}
	defer func() { _ = rows.Close() }()

	var out []ports.Signal
	for rows.Next() {
		var (
			id, signalType, valence, context, occurredAt string
			targetKind, targetID                         sql.NullString
			decisionAction, relationType                 sql.NullString
			magnitude                                    sql.NullFloat64
		)
		if err := rows.Scan(&id, &signalType, &valence, &targetKind, &targetID,
			&decisionAction, &relationType, &magnitude, &context, &occurredAt); err != nil {
			return nil, fmt.Errorf("scanning a signal row: %w", err)
		}
		at, err := time.Parse(unitTimeLayout, occurredAt)
		if err != nil {
			return nil, fmt.Errorf("signal %q: occurred_at: %w", id, err)
		}
		out = append(out, ports.Signal{
			ID:             id,
			Type:           ports.SignalType(signalType),
			Valence:        ports.Valence(valence),
			TargetKind:     nullStringToTargetKind(targetKind),
			TargetID:       nullStringToPtr(targetID),
			DecisionAction: nullStringToDecisionAction(decisionAction),
			RelationType:   nullStringToPtr(relationType),
			Magnitude:      nullFloatToPtr(magnitude),
			Context:        []byte(context),
			OccurredAt:     at,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading signals since %s: %w", t, err)
	}
	return out, nil
}

// targetKindToNull converts a possibly-nil *ports.TargetKind into the SQL
// NULL/TEXT the target_kind column expects.
func targetKindToNull(k *ports.TargetKind) any {
	if k == nil {
		return nil
	}
	return string(*k)
}

// decisionActionToNull converts a possibly-nil *ports.DecisionAction into
// the SQL NULL/TEXT the decision_action column expects.
func decisionActionToNull(a *ports.DecisionAction) any {
	if a == nil {
		return nil
	}
	return string(*a)
}

// stringPtrToNull converts a possibly-nil *string into the SQL NULL/TEXT
// the target_id and relation_type columns expect.
func stringPtrToNull(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// nullStringToPtr is stringPtrToNull's inverse, used for TargetID and
// RelationType on the read path.
func nullStringToPtr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	v := n.String
	return &v
}

// nullStringToTargetKind is targetKindToNull's inverse.
func nullStringToTargetKind(n sql.NullString) *ports.TargetKind {
	if !n.Valid {
		return nil
	}
	k := ports.TargetKind(n.String)
	return &k
}

// nullStringToDecisionAction is decisionActionToNull's inverse.
func nullStringToDecisionAction(n sql.NullString) *ports.DecisionAction {
	if !n.Valid {
		return nil
	}
	a := ports.DecisionAction(n.String)
	return &a
}

// nullFloatToPtr is floatPtrToNull's inverse, used for Magnitude on the
// read path.
func nullFloatToPtr(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	v := n.Float64
	return &v
}
