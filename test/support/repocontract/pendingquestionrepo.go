package repocontract

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// PendingQuestionHarness is what a ports.PendingQuestionRepo implementation
// must offer the contract so the suite can put questions in front of it.
//
// EnsureRelation exists for the same reason RelationHarness.EnsureUnit does
// (C11): a real store's pending_questions.relation_id is checked against a
// real relations row at INSERT time (design §3.1's own WHERE EXISTS guard,
// chosen over a foreign key), and RelationQuestion's own join needs a real
// relation with real endpoint content to read back. relType, the two
// endpoint unit ids and their content are all recorded so Unasked/Open's
// join can be proven, not only Create's existence check.
type PendingQuestionHarness interface {
	ports.PendingQuestionRepo

	// EnsureRelation makes relationID a valid Create target and gives
	// Unasked/Open's join something real to read: relType and both
	// endpoints' unit id + content.
	EnsureRelation(t *testing.T, relationID, relType, fromUnitID, toUnitID, fromContent, toContent string)

	// ForgetRelation removes relationID from the store's own
	// relation-existence set (over the fake) or deletes the underlying
	// relations row (over the real store), so R8's mitigation — a question
	// whose relation is gone is skipped by Unasked/Open, not failed on —
	// can be exercised without depending on RejectRelation's own delete
	// path.
	ForgetRelation(t *testing.T, relationID string)
}

// pendingQuestionNow is a fixed instant fixtures in this suite share —
// none of these methods reads a clock (design D5's rule, applied to this
// port too).
var pendingQuestionNow = time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)

// RunPendingQuestionRepo runs the ports.PendingQuestionRepo contract against
// a fresh implementation built by newRepo for every subtest. newRepo must
// return one holding no pending question and no relation.
func RunPendingQuestionRepo(t *testing.T, newRepo func(t *testing.T) PendingQuestionHarness) {
	t.Helper()

	t.Run("Create then Unasked round trip", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		repo.EnsureRelation(t, "rel-1", "same_topic", "unit-a", "unit-b", "content a", "content b")

		q := ports.PendingQuestion{ID: "q-1", Kind: ports.QuestionKindRelation, RelationID: "rel-1", CreatedAt: pendingQuestionNow}
		if err := repo.Create(ctx, q); err != nil {
			t.Fatalf("Create: %v", err)
		}

		got, err := repo.Unasked(ctx)
		if err != nil {
			t.Fatalf("Unasked: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("Unasked() returned %d rows, want 1: %v", len(got), got)
		}
		want := ports.RelationQuestion{
			ID: "q-1", RelationID: "rel-1", RelationType: "same_topic",
			FromUnitID: "unit-a", ToUnitID: "unit-b",
			FromContent: "content a", ToContent: "content b",
			CreatedAt: pendingQuestionNow,
		}
		if got[0] != want {
			t.Errorf("Unasked()[0] = %+v, want %+v", got[0], want)
		}
	})

	t.Run("Create against an unknown relation_id returns ErrRelationNotFound and inserts nothing (N1)", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		q := ports.PendingQuestion{ID: "q-orphan", Kind: ports.QuestionKindRelation, RelationID: "no-such-relation", CreatedAt: pendingQuestionNow}
		err := repo.Create(ctx, q)
		if err == nil {
			t.Fatal("Create(unknown relation_id) returned nil, want ErrRelationNotFound")
		}

		got, err := repo.Unasked(ctx)
		if err != nil {
			t.Fatalf("Unasked: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("Unasked() = %v after a refused Create, want none — nothing must have been inserted", got)
		}
	})

	t.Run("MarkAsked moves a row from Unasked into Open", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		repo.EnsureRelation(t, "rel-1", "same_topic", "unit-a", "unit-b", "content a", "content b")
		if err := repo.Create(ctx, ports.PendingQuestion{ID: "q-1", Kind: ports.QuestionKindRelation, RelationID: "rel-1", CreatedAt: pendingQuestionNow}); err != nil {
			t.Fatalf("Create: %v", err)
		}

		askedAt := pendingQuestionNow.Add(1 * time.Hour)
		if err := repo.MarkAsked(ctx, "q-1", askedAt); err != nil {
			t.Fatalf("MarkAsked: %v", err)
		}

		unasked, err := repo.Unasked(ctx)
		if err != nil {
			t.Fatalf("Unasked: %v", err)
		}
		if len(unasked) != 0 {
			t.Errorf("Unasked() = %v after MarkAsked, want none", unasked)
		}

		open, err := repo.Open(ctx)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if len(open) != 1 {
			t.Fatalf("Open() returned %d rows, want 1: %v", len(open), open)
		}
		if open[0].AskedAt == nil || !open[0].AskedAt.Equal(askedAt) {
			t.Errorf("Open()[0].AskedAt = %v, want %v", open[0].AskedAt, askedAt)
		}
	})

	t.Run("MarkAsked twice fails the second time (asked_at IS NULL precondition)", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		repo.EnsureRelation(t, "rel-1", "same_topic", "unit-a", "unit-b", "a", "b")
		if err := repo.Create(ctx, ports.PendingQuestion{ID: "q-1", Kind: ports.QuestionKindRelation, RelationID: "rel-1", CreatedAt: pendingQuestionNow}); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := repo.MarkAsked(ctx, "q-1", pendingQuestionNow); err != nil {
			t.Fatalf("first MarkAsked: %v", err)
		}
		if err := repo.MarkAsked(ctx, "q-1", pendingQuestionNow.Add(time.Hour)); err == nil {
			t.Fatal("second MarkAsked returned nil, want ErrQuestionStatusConflict")
		}
	})

	t.Run("Open orders most recently asked first", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		repo.EnsureRelation(t, "rel-old", "same_topic", "unit-a", "unit-b", "a", "b")
		repo.EnsureRelation(t, "rel-new", "same_topic", "unit-c", "unit-d", "c", "d")

		for _, q := range []ports.PendingQuestion{
			{ID: "q-old", Kind: ports.QuestionKindRelation, RelationID: "rel-old", CreatedAt: pendingQuestionNow},
			{ID: "q-new", Kind: ports.QuestionKindRelation, RelationID: "rel-new", CreatedAt: pendingQuestionNow},
		} {
			if err := repo.Create(ctx, q); err != nil {
				t.Fatalf("Create(%s): %v", q.ID, err)
			}
		}
		if err := repo.MarkAsked(ctx, "q-old", pendingQuestionNow); err != nil {
			t.Fatalf("MarkAsked(q-old): %v", err)
		}
		if err := repo.MarkAsked(ctx, "q-new", pendingQuestionNow.Add(5*time.Minute)); err != nil {
			t.Fatalf("MarkAsked(q-new): %v", err)
		}

		open, err := repo.Open(ctx)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if len(open) != 2 || open[0].ID != "q-new" || open[1].ID != "q-old" {
			t.Fatalf("Open() = %v, want [q-new q-old] (most recently asked first)", open)
		}
	})

	for _, tc := range []struct {
		name    string
		resolve func(repo ports.PendingQuestionRepo, ctx context.Context, id string, at time.Time) error
	}{
		{"Confirm", func(r ports.PendingQuestionRepo, ctx context.Context, id string, at time.Time) error {
			return r.Confirm(ctx, id, at)
		}},
		{"Reject", func(r ports.PendingQuestionRepo, ctx context.Context, id string, at time.Time) error {
			return r.Reject(ctx, id, at)
		}},
		{"Expire", func(r ports.PendingQuestionRepo, ctx context.Context, id string, at time.Time) error {
			return r.Expire(ctx, id, at)
		}},
	} {
		t.Run(tc.name+" closes an open question and removes it from Open", func(t *testing.T) {
			repo := newRepo(t)
			ctx := context.Background()
			repo.EnsureRelation(t, "rel-1", "same_topic", "unit-a", "unit-b", "a", "b")
			if err := repo.Create(ctx, ports.PendingQuestion{ID: "q-1", Kind: ports.QuestionKindRelation, RelationID: "rel-1", CreatedAt: pendingQuestionNow}); err != nil {
				t.Fatalf("Create: %v", err)
			}
			if err := repo.MarkAsked(ctx, "q-1", pendingQuestionNow); err != nil {
				t.Fatalf("MarkAsked: %v", err)
			}

			if err := tc.resolve(repo, ctx, "q-1", pendingQuestionNow.Add(time.Hour)); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}

			open, err := repo.Open(ctx)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			if len(open) != 0 {
				t.Errorf("Open() = %v after %s, want none", open, tc.name)
			}
		})

		t.Run(tc.name+" twice fails the second time (resolved_at IS NULL precondition)", func(t *testing.T) {
			repo := newRepo(t)
			ctx := context.Background()
			repo.EnsureRelation(t, "rel-1", "same_topic", "unit-a", "unit-b", "a", "b")
			if err := repo.Create(ctx, ports.PendingQuestion{ID: "q-1", Kind: ports.QuestionKindRelation, RelationID: "rel-1", CreatedAt: pendingQuestionNow}); err != nil {
				t.Fatalf("Create: %v", err)
			}
			if err := repo.MarkAsked(ctx, "q-1", pendingQuestionNow); err != nil {
				t.Fatalf("MarkAsked: %v", err)
			}
			if err := tc.resolve(repo, ctx, "q-1", pendingQuestionNow.Add(time.Hour)); err != nil {
				t.Fatalf("first %s: %v", tc.name, err)
			}
			if err := tc.resolve(repo, ctx, "q-1", pendingQuestionNow.Add(2*time.Hour)); err == nil {
				t.Fatalf("second %s returned nil, want ErrQuestionStatusConflict", tc.name)
			}
		})
	}

	t.Run("Confirm/Reject/Expire on an unknown id returns ErrQuestionNotFound", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		if err := repo.Confirm(ctx, "no-such-question", pendingQuestionNow); err == nil {
			t.Error("Confirm(unknown) returned nil, want ErrQuestionNotFound")
		}
		if err := repo.Reject(ctx, "no-such-question", pendingQuestionNow); err == nil {
			t.Error("Reject(unknown) returned nil, want ErrQuestionNotFound")
		}
		if err := repo.Expire(ctx, "no-such-question", pendingQuestionNow); err == nil {
			t.Error("Expire(unknown) returned nil, want ErrQuestionNotFound")
		}
	})

	t.Run("a question whose relation is gone is skipped by Unasked and Open, not failed on (R8)", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		repo.EnsureRelation(t, "rel-gone", "same_topic", "unit-a", "unit-b", "a", "b")
		if err := repo.Create(ctx, ports.PendingQuestion{ID: "q-1", Kind: ports.QuestionKindRelation, RelationID: "rel-gone", CreatedAt: pendingQuestionNow}); err != nil {
			t.Fatalf("Create: %v", err)
		}

		repo.ForgetRelation(t, "rel-gone")

		unasked, err := repo.Unasked(ctx)
		if err != nil {
			t.Fatalf("Unasked: %v", err)
		}
		if len(unasked) != 0 {
			t.Errorf("Unasked() = %v for a question whose relation is gone, want none", unasked)
		}

		if err := repo.MarkAsked(ctx, "q-1", pendingQuestionNow); err != nil {
			t.Fatalf("MarkAsked: %v", err)
		}
		open, err := repo.Open(ctx)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if len(open) != 0 {
			t.Errorf("Open() = %v for a question whose relation is gone, want none", open)
		}
	})

	t.Run("declares no Delete/Remove/Purge/Drop/Destroy-prefixed method (I03)", func(t *testing.T) {
		repoType := reflect.TypeOf((*ports.PendingQuestionRepo)(nil)).Elem()
		for i := 0; i < repoType.NumMethod(); i++ {
			name := repoType.Method(i).Name
			for _, prefix := range []string{"Delete", "Remove", "Purge", "Drop", "Destroy"} {
				if strings.HasPrefix(name, prefix) {
					t.Errorf("ports.PendingQuestionRepo declares %s — a pending question is a state machine, never a removal (I03)", name)
				}
			}
		}
	})
}
