package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ncruces/go-sqlite3"

	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// UnitRepo is the SQLite implementation of ports.UnitRepo — design D6's
// "answered twice" standing rule: the same repocontract.RunUnitRepo suite
// that ran against test/support/memrepo's fake at L2 (PR 3) runs against
// this type at L3, over a real migrated vault. It adds no migration
// (spec R4.4) — the units table already exists (migration 0001).
type UnitRepo struct {
	db *sql.DB
}

// NewUnitRepo returns a ports.UnitRepo backed by v's already-migrated
// vault.
func NewUnitRepo(v *Vault) *UnitRepo {
	return &UnitRepo{db: v.db}
}

var _ ports.UnitRepo = (*UnitRepo)(nil)

// unitTimeLayout is doc 03's TEXT timestamp encoding — ISO-8601 UTC,
// "YYYY-MM-DDTHH:MM:SSZ" (docs/03-data-model.md:11). time.RFC3339 renders
// exactly this shape for a UTC time carrying no fractional seconds, and
// time.Parse with the same layout returns a time.Time in the UTC location
// for a "Z"-suffixed input, so a value written and read back compares
// equal under reflect.DeepEqual.
const unitTimeLayout = time.RFC3339

// Create implements ports.UnitRepo.
func (r *UnitRepo) Create(ctx context.Context, u unit.Unit) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO units
			(id, type, content, status, weight, weight_decay_rate, last_touched_at,
			 structured_data, source, event_at, due_at, confidence, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, string(u.Type), u.Content, string(u.Status), u.Weight, u.WeightDecayRate,
		formatUnitTime(u.LastTouchedAt), rawMessageToNull(u.StructuredData), u.Source,
		timePtrToNull(u.EventAt), timePtrToNull(u.DueAt), floatPtrToNull(u.Confidence),
		formatUnitTime(u.CreatedAt), formatUnitTime(u.UpdatedAt),
	)
	if err != nil {
		if errors.Is(err, sqlite3.CONSTRAINT_PRIMARYKEY) {
			return ports.ErrUnitExists
		}
		return fmt.Errorf("insert unit %q: %w", u.ID, err)
	}
	return nil
}

// ByID implements ports.UnitRepo.
func (r *UnitRepo) ByID(ctx context.Context, id string) (unit.Unit, error) {
	row := r.db.QueryRowContext(ctx, unitSelectColumns+` FROM units WHERE id = ?`, id)
	u, err := scanUnit(row)
	if errors.Is(err, sql.ErrNoRows) {
		return unit.Unit{}, ports.ErrUnitNotFound
	}
	if err != nil {
		return unit.Unit{}, fmt.Errorf("select unit %q: %w", id, err)
	}
	return u, nil
}

// LiveByIDs implements ports.UnitRepo. The SQL filters positively on
// status = 'pool' (spec R4.2) — never a negative exclusion — and the
// result is reordered to match the caller's ids, omitting any id that is
// absent or not live (design D5).
func (r *UnitRepo) LiveByIDs(ctx context.Context, ids []string) ([]unit.Unit, error) {
	if len(ids) == 0 {
		return []unit.Unit{}, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+1)
	for _, id := range ids {
		args = append(args, id)
	}
	args = append(args, string(unit.StatusPool))

	rows, err := r.db.QueryContext(ctx,
		unitSelectColumns+` FROM units WHERE id IN (`+placeholders+`) AND status = ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("select live units: %w", err)
	}
	defer rows.Close() //nolint:errcheck // read-only query, nothing left to clean up on error

	byID := make(map[string]unit.Unit, len(ids))
	for rows.Next() {
		u, err := scanUnit(rows)
		if err != nil {
			return nil, fmt.Errorf("scan live unit: %w", err)
		}
		byID[u.ID] = u
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("select live units: %w", err)
	}

	live := make([]unit.Unit, 0, len(ids))
	for _, id := range ids {
		if u, ok := byID[id]; ok {
			live = append(live, u)
		}
	}
	return live, nil
}

// UpdateContent implements ports.UnitRepo.
func (r *UnitRepo) UpdateContent(ctx context.Context, id, content string, at time.Time) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE units SET content = ?, updated_at = ? WHERE id = ?`,
		content, formatUnitTime(at), id,
	)
	if err != nil {
		return fmt.Errorf("update unit %q content: %w", id, err)
	}
	return requireRowAffected(res, ports.ErrUnitNotFound)
}

// SetStatus implements ports.UnitRepo. from is an optimistic-concurrency
// precondition, not a validation — unit.ValidateTransition already decided
// legality (design D5).
func (r *UnitRepo) SetStatus(ctx context.Context, id string, from, to unit.Status, at time.Time) error {
	var current string
	err := r.db.QueryRowContext(ctx, `SELECT status FROM units WHERE id = ?`, id).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.ErrUnitNotFound
	}
	if err != nil {
		return fmt.Errorf("read unit %q status: %w", id, err)
	}
	if current != string(from) {
		return ports.ErrStatusConflict
	}

	res, err := r.db.ExecContext(ctx,
		`UPDATE units SET status = ?, updated_at = ? WHERE id = ? AND status = ?`,
		string(to), formatUnitTime(at), id, string(from),
	)
	if err != nil {
		return fmt.Errorf("update unit %q status: %w", id, err)
	}
	return requireRowAffected(res, ports.ErrStatusConflict)
}

// unitSelectColumns is shared by ByID and LiveByIDs — one column list, one
// place, so the two read paths and scanUnit cannot drift apart.
const unitSelectColumns = `SELECT id, type, content, status, weight, weight_decay_rate, last_touched_at,
	structured_data, source, event_at, due_at, confidence, created_at, updated_at`

// unitRow is satisfied by both *sql.Row (ByID) and *sql.Rows (LiveByIDs).
type unitRow interface {
	Scan(dest ...any) error
}

// scanUnit reads one units row into a unit.Unit, in unitSelectColumns'
// exact column order.
func scanUnit(row unitRow) (unit.Unit, error) {
	var (
		u                                   unit.Unit
		typ, status                         string
		lastTouchedAt, createdAt, updatedAt string
		structuredData                      sql.NullString
		eventAt, dueAt                      sql.NullString
		confidence                          sql.NullFloat64
	)

	err := row.Scan(
		&u.ID, &typ, &u.Content, &status, &u.Weight, &u.WeightDecayRate, &lastTouchedAt,
		&structuredData, &u.Source, &eventAt, &dueAt, &confidence, &createdAt, &updatedAt,
	)
	if err != nil {
		return unit.Unit{}, err
	}

	u.Type, err = unit.ParseType(typ)
	if err != nil {
		return unit.Unit{}, fmt.Errorf("unit %q: %w", u.ID, err)
	}
	u.Status, err = unit.ParseStatus(status)
	if err != nil {
		return unit.Unit{}, fmt.Errorf("unit %q: %w", u.ID, err)
	}
	if u.LastTouchedAt, err = time.Parse(unitTimeLayout, lastTouchedAt); err != nil {
		return unit.Unit{}, fmt.Errorf("unit %q: last_touched_at: %w", u.ID, err)
	}
	if u.CreatedAt, err = time.Parse(unitTimeLayout, createdAt); err != nil {
		return unit.Unit{}, fmt.Errorf("unit %q: created_at: %w", u.ID, err)
	}
	if u.UpdatedAt, err = time.Parse(unitTimeLayout, updatedAt); err != nil {
		return unit.Unit{}, fmt.Errorf("unit %q: updated_at: %w", u.ID, err)
	}
	if structuredData.Valid {
		u.StructuredData = json.RawMessage(structuredData.String)
	}
	if eventAt.Valid {
		t, err := time.Parse(unitTimeLayout, eventAt.String)
		if err != nil {
			return unit.Unit{}, fmt.Errorf("unit %q: event_at: %w", u.ID, err)
		}
		u.EventAt = &t
	}
	if dueAt.Valid {
		t, err := time.Parse(unitTimeLayout, dueAt.String)
		if err != nil {
			return unit.Unit{}, fmt.Errorf("unit %q: due_at: %w", u.ID, err)
		}
		u.DueAt = &t
	}
	if confidence.Valid {
		u.Confidence = &confidence.Float64
	}

	return u, nil
}

// formatUnitTime renders t in doc 03's TEXT encoding (unitTimeLayout).
func formatUnitTime(t time.Time) string {
	return t.UTC().Format(unitTimeLayout)
}

// rawMessageToNull converts a possibly-nil json.RawMessage into the SQL
// NULL/TEXT the structured_data column expects.
func rawMessageToNull(v json.RawMessage) any {
	if v == nil {
		return nil
	}
	return string(v)
}

// timePtrToNull converts a possibly-nil *time.Time into the SQL NULL/TEXT
// the event_at/due_at columns expect.
func timePtrToNull(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatUnitTime(*t)
}

// floatPtrToNull converts a possibly-nil *float64 into the SQL NULL/REAL
// the confidence column expects.
func floatPtrToNull(f *float64) any {
	if f == nil {
		return nil
	}
	return *f
}

// requireRowAffected returns notFound when res reports zero rows affected,
// nil otherwise — the shared shape UpdateContent and SetStatus both need.
func requireRowAffected(res sql.Result, notFound error) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return notFound
	}
	return nil
}
