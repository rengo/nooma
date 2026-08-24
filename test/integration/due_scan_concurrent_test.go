//go:build integration

package integration

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/store/sqlite"
)

// TestDueScan_ConcurrentPassesTransitionExactlyOnce is R5.4's real claim,
// and it lives at L3 because nowhere else can make it.
//
// The guard being tested is not a Go check. The armed precondition lives in
// the UPDATE's own WHERE clause and nowhere else — ports.DueTrigger carries
// no Status field at all, which makes the Go-side check unwritable rather
// than merely discouraged. A Go-side check would re-read the stale value
// the loop already holds and cannot fail on the violation it exists to
// prevent.
//
// So the thing under test is SQLite's own behaviour under the vault's
// _txlock=immediate DSN: two passes race on one statement, exactly one
// wins, and the loser gets zero rows affected and the conflict sentinel.
// **Running this same body against memrepo would prove nothing** — a fake's
// mutex would serialize the two calls and the test would pass without the
// SQL predicate existing at all. That is the "correct guard entered from
// underneath" failure this test is pinned to L3 to avoid.
//
// Both tables are raced, because "it works for triggers" is not evidence
// about the other loop's arm. The trigger fixture is stale rather than
// merely due: a delivering trigger writes nothing in this change, so two
// passes would both correctly do nothing and the race would be unobserved.
func TestDueScan_ConcurrentPassesTransitionExactlyOnce(t *testing.T) {
	ctx := context.Background()
	fireAt := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	now := fireAt.Add(time.Duration(prospection.TriggerStalenessHours+1) * time.Hour)

	dbPath := filepath.Join(t.TempDir(), "vault.db")
	v, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open(%q): %v", dbPath, err)
	}

	triggers := sqlite.NewTriggerRepo(v)
	timers := sqlite.NewTimerRepo(v)
	decisions := sqlite.NewDecisionLog(v)

	at := fireAt
	if err := triggers.Create(ctx, ports.Trigger{
		ID: "trg-contended", Kind: ports.TriggerKindTimeBased, FireAt: &at, CreatedAt: fireAt.Add(-72 * time.Hour),
	}); err != nil {
		t.Fatalf("Create trigger: %v", err)
	}
	// The timer is stale at this instant too (its window is tighter), so
	// both rows have exactly one legal transition and both races are real.
	if err := timers.Create(ctx, ports.Timer{
		ID: "tmr-contended", FireAt: fireAt, CreatedAt: fireAt.Add(-72 * time.Hour),
	}); err != nil {
		t.Fatalf("Create timer: %v", err)
	}

	// Two scans, each with its own id generator so a decision_log
	// primary-key collision cannot masquerade as the race.
	//
	// The reads are held at a barrier, and that is deliberate. Launching
	// two goroutines and hoping the scheduler interleaves them produces a
	// test that passes for the wrong reason most runs — the first scan
	// finishes, the second finds nothing due, no conflict happens, and the
	// assertions still hold. A race test that depends on luck is a flaky
	// test on its way to being deleted.
	//
	// So the barrier forces both passes to complete their reads before
	// either writes, which is exactly the interleaving the guard exists
	// for. It orders the READS only: both UPDATEs then run genuinely
	// concurrently against real SQLite, and which one wins is still the
	// engine's decision, not this test's.
	triggerReads := newBarrier(2)
	timerReads := newBarrier(2)
	svcA := brain.NewCheckService(fixedClock{now: now},
		barrierTriggers{TriggerRepo: triggers, barrier: triggerReads},
		barrierTimers{TimerRepo: timers, barrier: timerReads},
		&prefixedIDs{prefix: "a"}, decisions, nil)
	svcB := brain.NewCheckService(fixedClock{now: now},
		barrierTriggers{TriggerRepo: triggers, barrier: triggerReads},
		barrierTimers{TimerRepo: timers, barrier: timerReads},
		&prefixedIDs{prefix: "b"}, decisions, nil)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	start := make(chan struct{})
	for i, svc := range []*brain.CheckService{svcA, svcB} {
		wg.Add(1)
		go func(i int, svc *brain.CheckService) {
			defer wg.Done()
			<-start
			_, errs[i] = svc.Check(ctx, brain.CheckRequest{})
		}(i, svc)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("scan %d: %v — a lost race is recorded and skipped, never fatal to the pass", i, err)
		}
	}

	if err := v.Close(); err != nil {
		t.Fatalf("closing the vault before the raw read: %v", err)
	}
	raw, err := sql.Open("sqlite3", pragmaDSN(dbPath))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })

	// Exactly one transition per row, whichever pass won it.
	assertStatus(t, raw, "triggers", "trg-contended", string(ports.TriggerStatusExpired))
	assertStatus(t, raw, "timers", "tmr-contended", string(ports.TimerStatusCancelled))

	// And exactly one row each in the audit trail: one effect, one skip,
	// per contended row. Two effects would mean the guard never fired; two
	// skips would mean neither pass did the work.
	assertActionCount(t, raw, string(ports.ActionCheckTriggerExpired), 1)
	assertActionCount(t, raw, string(ports.ActionCheckTimerCancelled), 1)
	assertActionCount(t, raw, string(ports.ActionCheckConflictSkipped), 2)
}

// barrier releases every waiter once n of them have arrived, or after
// barrierTimeout, whichever comes first.
//
// The timeout is not defensive padding. Before the conflict arm exists, the
// pass that loses the trigger race aborts and never reaches the timer read,
// so a barrier that waited forever would hang the red step instead of
// failing it — and a red step that hangs teaches nobody anything. With the
// timeout, the red step fails on its assertions, which is what a red step
// is for. Once the arm lands, no waiter ever reaches the timeout.
type barrier struct {
	mu       sync.Mutex
	waiting  int
	n        int
	released chan struct{}
}

const barrierTimeout = 10 * time.Second

func newBarrier(n int) *barrier {
	return &barrier{n: n, released: make(chan struct{})}
}

func (b *barrier) arrive() {
	b.mu.Lock()
	b.waiting++
	if b.waiting == b.n {
		close(b.released)
	}
	released := b.released
	b.mu.Unlock()

	select {
	case <-released:
	case <-time.After(barrierTimeout):
	}
}

// barrierTriggers is the real repository with one added behaviour: Due does
// not return until every scan has finished reading. Every other method is
// the real one, so the UPDATE under test is untouched.
type barrierTriggers struct {
	ports.TriggerRepo
	barrier *barrier
}

func (r barrierTriggers) Due(ctx context.Context, at time.Time) ([]ports.DueTrigger, error) {
	due, err := r.TriggerRepo.Due(ctx, at)
	r.barrier.arrive()
	return due, err
}

// barrierTimers is barrierTriggers for the other table.
type barrierTimers struct {
	ports.TimerRepo
	barrier *barrier
}

func (r barrierTimers) Due(ctx context.Context, at time.Time) ([]ports.DueTimer, error) {
	due, err := r.TimerRepo.Due(ctx, at)
	r.barrier.arrive()
	return due, err
}

// prefixedIDs is an IDGen whose ids cannot collide with another instance's.
type prefixedIDs struct {
	prefix string
	mu     sync.Mutex
	n      int
}

func (g *prefixedIDs) New() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.n++
	return g.prefix + "-" + time.Duration(g.n).String()
}

func assertStatus(t *testing.T, raw *sql.DB, table, id, want string) {
	t.Helper()

	var got string
	if err := raw.QueryRowContext(context.Background(),
		`SELECT status FROM `+table+` WHERE id = ?`, id).Scan(&got); err != nil {
		t.Fatalf("select %s.status for %q: %v", table, id, err)
	}
	if got != want {
		t.Errorf("%s.status for %q = %q, want %q", table, id, got, want)
	}
}

func assertActionCount(t *testing.T, raw *sql.DB, action string, want int) {
	t.Helper()

	var got int
	if err := raw.QueryRowContext(context.Background(),
		`SELECT count(*) FROM decision_log WHERE action = ?`, action).Scan(&got); err != nil {
		t.Fatalf("count decision_log rows for %q: %v", action, err)
	}
	if got != want {
		t.Errorf("%d %q rows, want %d", got, action, want)
	}
}
