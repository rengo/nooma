package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ncruces/go-sqlite3"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
)

// TriggerRepo is the SQLite implementation of ports.TriggerRepo — design
// D6's "answered twice" standing rule: the same repocontract.RunTriggerRepo
// suite that ran against test/support/memrepo's fake at L2 runs against
// this type at L3, over a real migrated vault. It adds no migration: the
// triggers table has existed since 0001_core_tables.sql.
type TriggerRepo struct {
	db *sql.DB
}

// NewTriggerRepo returns a ports.TriggerRepo backed by v's already-migrated
// vault.
func NewTriggerRepo(v *Vault) *TriggerRepo {
	return &TriggerRepo{db: v.db}
}

var _ ports.TriggerRepo = (*TriggerRepo)(nil)

// anchorJSON is prospection.Anchor's storage encoding, and it exists
// because the core type carries no JSON tags (recurrence.go:20-23) while
// the column comment says {month, day} lowercase (0001_core_tables.sql:56).
// Go's default marshalling would write {"Month":9,"Day":4}.
//
// Adding tags to a core type for a storage concern is the wrong direction:
// the encoding is this package's decision, so it lives in this package.
// Month is an int here rather than a time.Month so the JSON is a number by
// construction and not by time.Month's marshalling behaviour.
type anchorJSON struct {
	Month int `json:"month"`
	Day   int `json:"day"`
}

// payloadJSON is ports.TriggerPayload's storage encoding, declared here for
// anchorJSON's reason and pinned to migration 0001:48's own column comment,
// "JSON (action, rationale, lead_days…)". lead_days is the key doc 02 §7
// names when it says a recurring trigger's re-arm propagates it — the one
// key in this object that anything reads back.
type payloadJSON struct {
	Action    string `json:"action"`
	Rationale string `json:"rationale"`
	LeadDays  int    `json:"lead_days"`
}

// Create implements ports.TriggerRepo. The armed literal is written here
// rather than left to the column default: ports.Trigger carries no Status
// field, so this statement is the only place that decides a new trigger is
// armed, and a default changed in a later migration must not be able to
// change that quietly.
func (r *TriggerRepo) Create(ctx context.Context, t ports.Trigger) error {
	payload, err := json.Marshal(payloadJSON{
		Action:    t.Payload.ActionText,
		Rationale: t.Payload.Rationale,
		LeadDays:  t.Payload.LeadDays,
	})
	if err != nil {
		return fmt.Errorf("marshal trigger %q payload: %w", t.ID, err)
	}

	anchor, err := marshalAnchor(t.RecurrenceAnchor)
	if err != nil {
		return fmt.Errorf("marshal trigger %q recurrence_anchor: %w", t.ID, err)
	}

	_, err = r.db.ExecContext(ctx,
		`INSERT INTO triggers
			(id, unit_id, kind, status, interrupt_level, payload, fire_at,
			 recurrence_rule, recurrence_anchor, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, stringPtrToNull(t.UnitID), string(t.Kind), string(ports.TriggerStatusArmed),
		floatPtrToNull(t.InterruptLevel), string(payload), timePtrToNull(t.FireAt),
		rulePtrToNull(t.RecurrenceRule), anchor, formatUnitTime(t.CreatedAt),
	)
	if err != nil {
		if errors.Is(err, sqlite3.CONSTRAINT_PRIMARYKEY) {
			return ports.ErrTriggerExists
		}
		return fmt.Errorf("insert trigger %q: %w", t.ID, err)
	}
	return nil
}

// Due implements ports.TriggerRepo. The predicate matches
// idx_triggers_status_fire (0001:59), and fire_at IS NOT NULL is part of
// it rather than a defensive guard — a pattern_based trigger legitimately
// has none.
func (r *TriggerRepo) Due(ctx context.Context, at time.Time) ([]ports.DueTrigger, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, unit_id, fire_at, interrupt_level, recurrence_rule, recurrence_anchor
		 FROM triggers
		 WHERE status = ? AND fire_at IS NOT NULL AND fire_at <= ?
		 ORDER BY fire_at, id`,
		string(ports.TriggerStatusArmed), formatUnitTime(at),
	)
	if err != nil {
		return nil, fmt.Errorf("select due triggers: %w", err)
	}
	defer rows.Close() //nolint:errcheck // read-only query, nothing left to clean up on error

	due := make([]ports.DueTrigger, 0)
	for rows.Next() {
		d, err := scanDueTrigger(rows)
		if err != nil {
			return nil, err
		}
		due = append(due, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("select due triggers: %w", err)
	}
	return due, nil
}

// Fire implements ports.TriggerRepo. status and fired_at move in one
// statement, so a fired row without a fired_at is unrepresentable.
// surfaced_at is untouched: "NULL = pending delivery" (0001:52) is what a
// fired-but-undelivered trigger is, and closing it is m3d's.
func (r *TriggerRepo) Fire(ctx context.Context, id string, at time.Time) error {
	return r.transition(ctx, id, ports.TriggerStatusFired, &at)
}

// Expire implements ports.TriggerRepo. triggers carries no expired_at
// column, so nothing but the status is written — and writing fired_at here
// would record a firing that never happened, which is exactly what I15
// exists to prevent.
func (r *TriggerRepo) Expire(ctx context.Context, id string) error {
	return r.transition(ctx, id, ports.TriggerStatusExpired, nil)
}

// Surface, Undelivered, Delivered and Resolve are the red step's stubs.
func (r *TriggerRepo) Surface(context.Context, string, time.Time) error { return nil }

func (r *TriggerRepo) Undelivered(context.Context) ([]ports.DueTrigger, error) { return nil, nil }

func (r *TriggerRepo) Delivered(context.Context) ([]ports.DueTrigger, error) { return nil, nil }

func (r *TriggerRepo) Resolve(context.Context, string, ports.TriggerResolution, time.Time) error {
	return nil
}

// transition moves id out of armed, optionally stamping fired_at.
//
// Two statements, and both are load-bearing. The SELECT distinguishes "no
// such trigger" from "no longer armed", which one UPDATE alone cannot do —
// zero rows affected means both. The UPDATE's own WHERE carries the armed
// precondition, and that is what decides a race: two scans over one due
// trigger both pass the SELECT, and exactly one wins the UPDATE.
// UnitRepo.SetStatus is built the same way, for the same two reasons.
func (r *TriggerRepo) transition(ctx context.Context, id string, to ports.TriggerStatus, firedAt *time.Time) error {
	var current string
	err := r.db.QueryRowContext(ctx, `SELECT status FROM triggers WHERE id = ?`, id).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.ErrTriggerNotFound
	}
	if err != nil {
		return fmt.Errorf("read trigger %q status: %w", id, err)
	}
	if current != string(ports.TriggerStatusArmed) {
		return ports.ErrTriggerStatusConflict
	}

	var res sql.Result
	if firedAt == nil {
		res, err = r.db.ExecContext(ctx,
			`UPDATE triggers SET status = ? WHERE id = ? AND status = ?`,
			string(to), id, string(ports.TriggerStatusArmed))
	} else {
		res, err = r.db.ExecContext(ctx,
			`UPDATE triggers SET status = ?, fired_at = ? WHERE id = ? AND status = ?`,
			string(to), formatUnitTime(*firedAt), id, string(ports.TriggerStatusArmed))
	}
	if err != nil {
		return fmt.Errorf("update trigger %q status: %w", id, err)
	}
	return requireRowAffected(res, ports.ErrTriggerStatusConflict)
}

// scanDueTrigger reads one row of Due's projection.
func scanDueTrigger(rows *sql.Rows) (ports.DueTrigger, error) {
	var (
		d                              ports.DueTrigger
		unitID                         sql.NullString
		fireAt                         string
		interruptLevel                 any
		recurrenceRule, recurrenceAnch sql.NullString
	)

	if err := rows.Scan(&d.ID, &unitID, &fireAt, &interruptLevel, &recurrenceRule, &recurrenceAnch); err != nil {
		return ports.DueTrigger{}, fmt.Errorf("scan due trigger: %w", err)
	}

	var err error
	if d.FireAt, err = time.Parse(unitTimeLayout, fireAt); err != nil {
		return ports.DueTrigger{}, fmt.Errorf("trigger %q: fire_at: %w", d.ID, err)
	}
	d.UnitID = nullStringToPtr(unitID)

	if d.InterruptLevel, err = interruptLevelFrom(d.ID, interruptLevel); err != nil {
		return ports.DueTrigger{}, err
	}

	if recurrenceRule.Valid {
		rule := prospection.Rule(recurrenceRule.String)
		d.RecurrenceRule = &rule
	}
	if recurrenceAnch.Valid {
		var stored anchorJSON
		if err := json.Unmarshal([]byte(recurrenceAnch.String), &stored); err != nil {
			return ports.DueTrigger{}, fmt.Errorf("trigger %q: recurrence_anchor: %w", d.ID, err)
		}
		d.RecurrenceAnchor = &prospection.Anchor{Month: time.Month(stored.Month), Day: stored.Day}
	}

	return d, nil
}

// interruptLevelFrom converts the driver's own value for triggers.
// interrupt_level into the *float64 the port returns.
//
// The column is scanned as an untyped value rather than into a
// sql.NullFloat64 for one reason: SQLite's dynamic typing permits a REAL
// column to hold non-numeric TEXT, and a NullFloat64 scan would fail with
// database/sql's own conversion message, naming neither the column nor the
// row. There is no value to be made of such a cell, so this aborts with a
// named error rather than inventing a level or dropping the trigger —
// persistBoosts' own ruling, applied here.
//
// What it does NOT do is judge the number. A level outside [0,1] is
// returned verbatim: clamping 1.7 to 1.0 would manufacture a push out of a
// corrupt number, and refusing the row would suppress a nudge over a field
// that only chooses a lane. prospection.ResolveInterrupt degrades it, and
// the decision_log row records both what was stored and what was made of
// it.
func interruptLevelFrom(id string, v any) (*float64, error) {
	switch level := v.(type) {
	case nil:
		return nil, nil
	case float64:
		return &level, nil
	case int64:
		asFloat := float64(level)
		return &asFloat, nil
	default:
		return nil, fmt.Errorf("trigger %q: interrupt_level holds %T (%v), which is not a number", id, v, v)
	}
}

// marshalAnchor renders a possibly-nil *prospection.Anchor into the SQL
// NULL/TEXT the recurrence_anchor column expects.
func marshalAnchor(a *prospection.Anchor) (any, error) {
	if a == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(anchorJSON{Month: int(a.Month), Day: a.Day})
	if err != nil {
		return nil, err
	}
	return string(encoded), nil
}

// rulePtrToNull converts a possibly-nil *prospection.Rule into the SQL
// NULL/TEXT the recurrence_rule column expects.
func rulePtrToNull(r *prospection.Rule) any {
	if r == nil {
		return nil
	}
	return string(*r)
}
