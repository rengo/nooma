//go:build integration

package sqlite

import (
	"context"
	"database/sql"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/repocontract"
)

// triggerFixtureTime is a fixed, whole-second UTC instant. The suite below
// exercises no clock behaviour of its own — every instant it needs is an
// offset from this one.
var triggerFixtureTime = time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)

// TestTriggerRepo_Contract runs the same repocontract.RunTriggerRepo suite
// PR 1 ran against the in-memory fake at L2, now against a real temporary
// SQLite vault at L3 — design D6's "answered twice" standing rule.
func TestTriggerRepo_Contract(t *testing.T) {
	repocontract.RunTriggerRepo(t, func(t *testing.T) repocontract.TriggerHarness {
		v := openTestVault(t)
		return triggerHarness{TriggerRepo: NewTriggerRepo(v), v: v}
	})
}

// triggerHarness is the whole reason repocontract.TriggerHarness exists:
// triggers.unit_id REFERENCES units(id) (migration 0001:43) and the vault
// opens with foreign_keys=on, so creating a trigger for a unit that does
// not exist is a constraint violation here and a no-op in the fake.
//
// It inserts directly rather than going through UnitRepo — the fixture must
// not depend on another port's behaviour to set up this one's.
// embeddingHarness is the same shape, for the same reason.
type triggerHarness struct {
	*TriggerRepo
	v *Vault
}

func (h triggerHarness) EnsureUnit(t *testing.T, id string) {
	t.Helper()

	_, err := h.v.db.ExecContext(context.Background(), `
INSERT INTO units (id, type, content, status, weight, weight_decay_rate,
                   last_touched_at, source, created_at, updated_at)
VALUES (?, 'task', 'fixture', 'pool', 1.0, 0.01, ?, 'chat', ?, ?)
ON CONFLICT(id) DO NOTHING`,
		id, triggerFixtureTime.Format(unitTimeLayout),
		triggerFixtureTime.Format(unitTimeLayout),
		triggerFixtureTime.Format(unitTimeLayout))
	if err != nil {
		t.Fatalf("seeding unit %s: %v", id, err)
	}
}

// TestTriggerRepo_NonFiniteInterruptLevelStorageIsPinned records what
// SQLite actually does with NaN and ±Inf in a REAL column, rather than
// leaving it to be inferred from a doc nobody re-reads (design §3.4).
//
// Every expectation here was read from this vault, not chosen in advance,
// and that is the point of the test: if a driver or SQLite upgrade changes
// any of these answers, this test is where the change surfaces, loudly,
// instead of quietly changing what a degraded interrupt level means.
//
// The finding worth stating in prose, because it is not obvious: **NaN is
// stored as SQL NULL**, so a NaN interrupt level and a degraded one are
// indistinguishable once written. That is not a defect — NULL is exactly
// what "no claim was made" means for this column — but it means NaN can
// never reach ResolveInterrupt as a number, while ±Inf can.
func TestTriggerRepo_NonFiniteInterruptLevelStorageIsPinned(t *testing.T) {
	for _, tc := range []struct {
		name       string
		written    float64
		wantTypeof string
		wantRead   *float64
	}{
		{"NaN is stored as NULL and reads back as no level at all", math.NaN(), "null", nil},
		{"+Inf survives as a REAL and reads back verbatim", math.Inf(1), "real", ptrTo(math.Inf(1))},
		{"-Inf survives as a REAL and reads back verbatim", math.Inf(-1), "real", ptrTo(math.Inf(-1))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := openTestVault(t)
			insertRawTrigger(t, v, "trg-raw", tc.written)

			if got := rawTypeof(t, v, "interrupt_level", "trg-raw"); got != tc.wantTypeof {
				t.Errorf("typeof(interrupt_level): got %q, want %q", got, tc.wantTypeof)
			}

			due, err := NewTriggerRepo(v).Due(context.Background(), triggerFixtureTime)
			if err != nil {
				t.Fatalf("Due: %v", err)
			}
			if len(due) != 1 {
				t.Fatalf("Due: got %d triggers, want 1", len(due))
			}

			switch {
			case tc.wantRead == nil && due[0].InterruptLevel != nil:
				t.Fatalf("InterruptLevel: got %v, want nil", *due[0].InterruptLevel)
			case tc.wantRead != nil && due[0].InterruptLevel == nil:
				t.Fatalf("InterruptLevel: got nil, want %v", *tc.wantRead)
			case tc.wantRead != nil && *due[0].InterruptLevel != *tc.wantRead:
				t.Fatalf("InterruptLevel: got %v, want %v", *due[0].InterruptLevel, *tc.wantRead)
			}
		})
	}
}

// TestTriggerRepo_OutOfRangeInterruptLevelIsReturnedVerbatim proves the
// repository neither clamps nor refuses a level outside [0,1] (design
// §3.4): clamping 1.7 to 1.0 would manufacture a push out of a corrupt
// number, and refusing the row would suppress a nudge the user asked for
// over a field that only chooses a lane. Degrading it is core's job, and
// an auditor can still tell a corrupt row from a clean one.
func TestTriggerRepo_OutOfRangeInterruptLevelIsReturnedVerbatim(t *testing.T) {
	v := openTestVault(t)
	ctx := context.Background()
	repo := NewTriggerRepo(v)
	seedFixtureUnit(t, v, "unit-trg-out-of-range")

	level := 1.7
	trg := fixtureStoreTrigger("trg-out-of-range")
	trg.InterruptLevel = &level
	if err := repo.Create(ctx, trg); err != nil {
		t.Fatalf("Create: %v", err)
	}

	due, err := repo.Due(ctx, triggerFixtureTime)
	if err != nil {
		t.Fatalf("Due: %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("Due: got %d triggers, want 1", len(due))
	}
	if due[0].InterruptLevel == nil || *due[0].InterruptLevel != 1.7 {
		t.Fatalf("InterruptLevel: got %v, want 1.7 verbatim", due[0].InterruptLevel)
	}
}

// TestTriggerRepo_NonNumericInterruptLevelAborts proves the one case the
// repository does not tolerate. SQLite's dynamic typing permits a REAL
// column to hold non-numeric TEXT; no value can be made of it, so Due
// fails with an error naming the column and the row, rather than
// inventing a level or silently dropping the trigger. persistBoosts' own
// ruling applies verbatim: no spec line covers this, so inventing
// tolerance here would be deciding design from an implementation seat.
func TestTriggerRepo_NonNumericInterruptLevelAborts(t *testing.T) {
	v := openTestVault(t)
	insertRawTrigger(t, v, "trg-corrupt", "not a number")

	// Pinned too: the value stayed TEXT rather than being coerced by the
	// column's REAL affinity, which is why this case exists at all.
	if got := rawTypeof(t, v, "interrupt_level", "trg-corrupt"); got != "text" {
		t.Fatalf("typeof(interrupt_level): got %q, want %q — the fixture no longer writes what it claims to", got, "text")
	}

	due, err := NewTriggerRepo(v).Due(context.Background(), triggerFixtureTime)
	if err == nil {
		t.Fatalf("Due: got %d triggers and no error, want an error naming interrupt_level", len(due))
	}
	if due != nil {
		t.Errorf("Due: got a non-nil slice alongside the error, want nil")
	}
	for _, want := range []string{"interrupt_level", "trg-corrupt"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Due error %q does not name %q", err, want)
		}
	}
}

// TestTriggerRepo_RecurrenceAnchorIsStoredWithLowercaseKeys asserts the
// stored bytes, not the round-tripped struct — the whole point of the case
// (design §3.4). prospection.Anchor carries no JSON tags, so Go's default
// marshalling would write {"Month":9,"Day":4} while the column comment
// says {month, day}. json.Unmarshal is case-insensitive on read, so a test
// that only round-tripped the struct would pass against either encoding
// and prove nothing.
func TestTriggerRepo_RecurrenceAnchorIsStoredWithLowercaseKeys(t *testing.T) {
	v := openTestVault(t)
	ctx := context.Background()
	repo := NewTriggerRepo(v)
	seedFixtureUnit(t, v, "unit-trg-anchor")

	rule := prospection.RuleYearly
	anchor := prospection.Anchor{Month: time.September, Day: 4}
	trg := fixtureStoreTrigger("trg-anchor")
	trg.RecurrenceRule = &rule
	trg.RecurrenceAnchor = &anchor
	if err := repo.Create(ctx, trg); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var stored string
	if err := v.db.QueryRowContext(ctx,
		`SELECT recurrence_anchor FROM triggers WHERE id = ?`, "trg-anchor").Scan(&stored); err != nil {
		t.Fatalf("select recurrence_anchor: %v", err)
	}
	if want := `{"month":9,"day":4}`; stored != want {
		t.Fatalf("stored recurrence_anchor: got %s, want %s", stored, want)
	}

	var storedRule string
	if err := v.db.QueryRowContext(ctx,
		`SELECT recurrence_rule FROM triggers WHERE id = ?`, "trg-anchor").Scan(&storedRule); err != nil {
		t.Fatalf("select recurrence_rule: %v", err)
	}
	if storedRule != string(prospection.RuleYearly) {
		t.Fatalf("stored recurrence_rule: got %q, want %q", storedRule, prospection.RuleYearly)
	}
}

// TestTriggerRepo_PayloadIsStoredWithTheColumnCommentsKeys pins
// triggers.payload's stored keys to migration 0001:48's own comment,
// "JSON (action, rationale, lead_days…)" — the same reasoning as the
// anchor above, and the same failure mode a struct-only round trip would
// miss. lead_days is the key doc 02 §7 names when it says a recurring
// trigger's re-arm propagates it.
func TestTriggerRepo_PayloadIsStoredWithTheColumnCommentsKeys(t *testing.T) {
	v := openTestVault(t)
	ctx := context.Background()
	seedFixtureUnit(t, v, "unit-trg-payload")

	if err := NewTriggerRepo(v).Create(ctx, fixtureStoreTrigger("trg-payload")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var stored string
	if err := v.db.QueryRowContext(ctx,
		`SELECT payload FROM triggers WHERE id = ?`, "trg-payload").Scan(&stored); err != nil {
		t.Fatalf("select payload: %v", err)
	}
	want := `{"action":"renew the passport","rationale":"it expires in three months","lead_days":7}`
	if stored != want {
		t.Fatalf("stored payload: got %s, want %s", stored, want)
	}
}

// TestTriggerRepo_FireWritesFiredAtAndLeavesSurfacedAtNull is where the
// status column is asserted directly — the observation RunTriggerRepo's
// doc comment says the port-level contract cannot make, because neither
// port declares an any-status read.
//
// surfaced_at staying NULL is not an omission: "NULL = pending delivery"
// (migration 0001:52) is what a fired-but-undelivered trigger is, and
// closing it is m3d's.
func TestTriggerRepo_FireWritesFiredAtAndLeavesSurfacedAtNull(t *testing.T) {
	v := openTestVault(t)
	ctx := context.Background()
	repo := NewTriggerRepo(v)
	seedFixtureUnit(t, v, "unit-trg-fired")

	if err := repo.Create(ctx, fixtureStoreTrigger("trg-fired")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	firedAt := triggerFixtureTime.Add(90 * time.Second)
	if err := repo.Fire(ctx, "trg-fired", firedAt); err != nil {
		t.Fatalf("Fire: %v", err)
	}

	var (
		status     string
		firedAtCol sql.NullString
		surfacedAt sql.NullString
	)
	if err := v.db.QueryRowContext(ctx,
		`SELECT status, fired_at, surfaced_at FROM triggers WHERE id = ?`, "trg-fired").
		Scan(&status, &firedAtCol, &surfacedAt); err != nil {
		t.Fatalf("select trigger: %v", err)
	}

	if status != string(ports.TriggerStatusFired) {
		t.Errorf("status: got %q, want %q", status, ports.TriggerStatusFired)
	}
	if !firedAtCol.Valid {
		t.Error("fired_at: got NULL, want the instant Fire was given — a fired row without one is unrepresentable")
	} else if want := firedAt.UTC().Format(unitTimeLayout); firedAtCol.String != want {
		t.Errorf("fired_at: got %q, want %q", firedAtCol.String, want)
	}
	if surfacedAt.Valid {
		t.Errorf("surfaced_at: got %q, want NULL — delivery is m3d's", surfacedAt.String)
	}
}

// TestTriggerRepo_ExpireWritesOnlyTheStatus proves Expire invents no
// timestamp: triggers carries no expired_at column, and writing fired_at
// for an expired trigger would record a firing that never happened (I15's
// whole point).
func TestTriggerRepo_ExpireWritesOnlyTheStatus(t *testing.T) {
	v := openTestVault(t)
	ctx := context.Background()
	repo := NewTriggerRepo(v)
	seedFixtureUnit(t, v, "unit-trg-expired")

	if err := repo.Create(ctx, fixtureStoreTrigger("trg-expired")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Expire(ctx, "trg-expired"); err != nil {
		t.Fatalf("Expire: %v", err)
	}

	var (
		status                  string
		firedAt, surfacedAt     sql.NullString
		respondedAt, resolution sql.NullString
	)
	if err := v.db.QueryRowContext(ctx,
		`SELECT status, fired_at, surfaced_at, responded_at, resolution FROM triggers WHERE id = ?`,
		"trg-expired").Scan(&status, &firedAt, &surfacedAt, &respondedAt, &resolution); err != nil {
		t.Fatalf("select trigger: %v", err)
	}

	if status != string(ports.TriggerStatusExpired) {
		t.Errorf("status: got %q, want %q", status, ports.TriggerStatusExpired)
	}
	for _, col := range []struct {
		name string
		got  sql.NullString
	}{
		{"fired_at", firedAt},
		{"surfaced_at", surfacedAt},
		{"responded_at", respondedAt},
		{"resolution", resolution},
	} {
		if col.got.Valid {
			t.Errorf("%s: got %q, want NULL — Expire writes the status and nothing else", col.name, col.got.String)
		}
	}
}

// TestTriggerRepo_CreateWritesTheArmedStatusItself proves the row lands
// armed because this repository says so, not because the column happens to
// default that way: ports.Trigger carries no Status field, so a default
// silently changed in a later migration would otherwise change what
// arming means with no test failing.
func TestTriggerRepo_CreateWritesTheArmedStatusItself(t *testing.T) {
	v := openTestVault(t)
	ctx := context.Background()
	seedFixtureUnit(t, v, "unit-trg-armed")

	if err := NewTriggerRepo(v).Create(ctx, fixtureStoreTrigger("trg-armed")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var status string
	if err := v.db.QueryRowContext(ctx,
		`SELECT status FROM triggers WHERE id = ?`, "trg-armed").Scan(&status); err != nil {
		t.Fatalf("select status: %v", err)
	}
	if status != string(ports.TriggerStatusArmed) {
		t.Fatalf("status: got %q, want %q", status, ports.TriggerStatusArmed)
	}
}

// fixtureStoreTrigger is one armed, time_based, one-shot trigger due at
// triggerFixtureTime.
func fixtureStoreTrigger(id string) ports.Trigger {
	unitID := "unit-" + id
	level := 0.42
	fireAt := triggerFixtureTime
	return ports.Trigger{
		ID:             id,
		UnitID:         &unitID,
		Kind:           ports.TriggerKindTimeBased,
		InterruptLevel: &level,
		Payload: ports.TriggerPayload{
			ActionText: "renew the passport",
			Rationale:  "it expires in three months",
			LeadDays:   7,
		},
		FireAt:    &fireAt,
		CreatedAt: triggerFixtureTime.Add(-24 * time.Hour),
	}
}

// seedFixtureUnit is triggerHarness.EnsureUnit for the cases that build a
// repository directly instead of going through the contract suite.
func seedFixtureUnit(t *testing.T, v *Vault, id string) {
	t.Helper()
	triggerHarness{v: v}.EnsureUnit(t, id)
}

// insertRawTrigger writes one armed, time_based trigger straight through
// SQL, bypassing the repository. These fixtures exist to store values
// Create could never produce — that is the only reason to reach past the
// port under test.
func insertRawTrigger(t *testing.T, v *Vault, id string, interruptLevel any) {
	t.Helper()

	_, err := v.db.ExecContext(context.Background(),
		`INSERT INTO triggers (id, kind, status, interrupt_level, payload, fire_at, created_at)
		 VALUES (?, ?, ?, ?, '{}', ?, ?)`,
		id, string(ports.TriggerKindTimeBased), string(ports.TriggerStatusArmed), interruptLevel,
		triggerFixtureTime.UTC().Format(unitTimeLayout),
		triggerFixtureTime.Add(-24*time.Hour).UTC().Format(unitTimeLayout),
	)
	if err != nil {
		t.Fatalf("raw insert trigger %q: %v", id, err)
	}
}

// rawTypeof returns SQLite's own typeof() for one trigger column, so a
// storage-format claim is read from the engine rather than assumed.
func rawTypeof(t *testing.T, v *Vault, column, id string) string {
	t.Helper()

	var typ string
	if err := v.db.QueryRowContext(context.Background(),
		`SELECT typeof(`+column+`) FROM triggers WHERE id = ?`, id).Scan(&typ); err != nil {
		t.Fatalf("select typeof(%s): %v", column, err)
	}
	return typ
}

// ptrTo is the fixtures' one-line address-of helper.
func ptrTo[T any](v T) *T { return &v }

// TestTriggerRepo_DeliveryContract runs the same delivery suite the
// in-memory fake answers at L2, over a real migrated vault.
func TestTriggerRepo_DeliveryContract(t *testing.T) {
	repocontract.RunTriggerDelivery(t, func(t *testing.T) repocontract.TriggerHarness {
		v := openTestVault(t)
		return triggerHarness{TriggerRepo: NewTriggerRepo(v), v: v}
	})
}

// TestTriggerRepo_ResolutionColumnHoldsOnlyVocabularyMembers is the
// constraint the schema does not carry, for the fourth vocabulary.
//
// triggers.resolution is plain TEXT with no CHECK, so a mistyped literal
// anywhere in the write path persists happily. Every L2 case above stays
// green through exactly that mutation — the fake has no column to hold a
// wrong value in — and only a read of what SQLite actually stored fails.
func TestTriggerRepo_ResolutionColumnHoldsOnlyVocabularyMembers(t *testing.T) {
	v := openTestVault(t)
	ctx := context.Background()
	repo := NewTriggerRepo(v)
	seedFixtureUnit(t, v, "unit-trg-resolved")

	if err := repo.Create(ctx, fixtureStoreTrigger("trg-resolved")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	for _, step := range []func() error{
		func() error { return repo.Fire(ctx, "trg-resolved", triggerFixtureTime) },
		func() error { return repo.Surface(ctx, "trg-resolved", triggerFixtureTime) },
		func() error {
			return repo.Resolve(ctx, "trg-resolved", ports.ResolutionSelfHealed, triggerFixtureTime)
		},
	} {
		if err := step(); err != nil {
			t.Fatalf("transition: %v", err)
		}
	}

	var stored, respondedAt sql.NullString
	if err := v.db.QueryRowContext(ctx,
		`SELECT resolution, responded_at FROM triggers WHERE id = ?`, "trg-resolved").
		Scan(&stored, &respondedAt); err != nil {
		t.Fatalf("select resolution: %v", err)
	}

	vocabulary := map[string]bool{}
	for _, r := range ports.AllTriggerResolutions() {
		vocabulary[string(r)] = true
	}
	if !stored.Valid || !vocabulary[stored.String] {
		t.Fatalf("resolution = %v, which is not a member of ports.AllTriggerResolutions() — the column has no CHECK constraint, so this test is the constraint", stored)
	}
	if !respondedAt.Valid {
		t.Error("responded_at is NULL on a resolved trigger — an answer without an instant is unrepresentable by design")
	}
}
