package repocontract

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/selfmodel"
	"github.com/rengo/nooma/internal/ports"
)

// The belief-status half of the ports.SelfModelRepo contract (m4e design
// §3.2, §6 fixture FX-B): the two reads RetiredBeliefs and BeliefByID, the
// two user writes SetStatus and EditContent, and the two guards that make a
// retired belief and the user's own text unreachable by derive.
//
// Every suite here seeds the same FX-B fixture before it asserts that
// "nothing else changed", and compares full rows rather than counts from
// zero: each seeded belief carries non-default confidence and timestamps, so
// a column written by mistake is visible. Timestamps are whole seconds,
// because SQLite stores seconds and memrepo keeps nanoseconds.

// Seeded belief ids, named for the facet and origin they exercise.
const (
	idGoalDerived   = "b-goal-derived"
	idGoalUser      = "b-goal-user"
	idGoalRetired   = "b-goal-retired"
	idValueSeed     = "b-value-seed"
	idValueDerived  = "b-value-derived"
	idIdentityDeriv = "b-identity-derived"
)

// fxbBase is the whole-second instant every FX-B timestamp is offset from.
var fxbBase = time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

// fxbBelief builds one FX-B row. The offset makes confidence and the three
// timestamps distinct per belief and different from any default.
func fxbBelief(id string, facet selfmodel.Facet, origin selfmodel.Origin, status selfmodel.Status, confidence float64, offsetMinutes int) ports.Belief {
	step := time.Duration(offsetMinutes) * time.Minute
	return ports.Belief{
		ID:               id,
		Facet:            facet,
		TopicKey:         "key/" + id,
		Content:          "content of " + id,
		Confidence:       confidence,
		Origin:           origin,
		SourceUnitID:     nil,
		Status:           status,
		LastReinforcedAt: fxbBase.Add(step + 7*time.Second),
		CreatedAt:        fxbBase.Add(-48*time.Hour + step + 3*time.Second),
		UpdatedAt:        fxbBase.Add(-24*time.Hour + step + 5*time.Second),
	}
}

// seedFXB stores the FX-B fixture and returns it keyed by id: two beliefs
// in each touched facet (goal, value), one populated untouched facet
// (identity), two empty facets (social, preference), and a retired belief
// in a touched facet. The value-facet derived belief has multi-line
// content.
func seedFXB(t *testing.T, repo ports.SelfModelRepo) map[string]ports.Belief {
	t.Helper()

	valueDerived := fxbBelief(idValueDerived, selfmodel.FacetValue, selfmodel.OriginDerived, selfmodel.StatusActive, 0.67, 40)
	valueDerived.Content = "line one\nline two"

	seeded := []ports.Belief{
		fxbBelief(idGoalDerived, selfmodel.FacetGoal, selfmodel.OriginDerived, selfmodel.StatusActive, 0.71, 10),
		fxbBelief(idGoalUser, selfmodel.FacetGoal, selfmodel.OriginUserStated, selfmodel.StatusActive, 0.82, 20),
		fxbBelief(idGoalRetired, selfmodel.FacetGoal, selfmodel.OriginDerived, selfmodel.StatusRetired, 0.63, 30),
		fxbBelief(idValueSeed, selfmodel.FacetValue, selfmodel.OriginSeed, selfmodel.StatusActive, 0.58, 35),
		valueDerived,
		fxbBelief(idIdentityDeriv, selfmodel.FacetIdentity, selfmodel.OriginDerived, selfmodel.StatusActive, 0.74, 50),
	}
	out := make(map[string]ports.Belief, len(seeded))
	for _, b := range seeded {
		if err := repo.UpsertByTopicKey(context.Background(), b); err != nil {
			t.Fatalf("seed %s: UpsertByTopicKey: %v", b.ID, err)
		}
		out[b.ID] = b
	}
	return out
}

// beliefColumnsDiffering names every column on which want and got differ.
func beliefColumnsDiffering(want, got ports.Belief) []string {
	var cols []string
	add := func(name string, same bool) {
		if !same {
			cols = append(cols, name)
		}
	}
	add("id", want.ID == got.ID)
	add("facet", want.Facet == got.Facet)
	add("topic_key", want.TopicKey == got.TopicKey)
	add("content", want.Content == got.Content)
	add("confidence", want.Confidence == got.Confidence)
	add("origin", want.Origin == got.Origin)
	add("status", want.Status == got.Status)
	add("last_reinforced_at", want.LastReinforcedAt.Equal(got.LastReinforcedAt))
	add("created_at", want.CreatedAt.Equal(got.CreatedAt))
	add("updated_at", want.UpdatedAt.Equal(got.UpdatedAt))
	switch {
	case want.SourceUnitID == nil && got.SourceUnitID == nil:
	case want.SourceUnitID == nil || got.SourceUnitID == nil:
		cols = append(cols, "source_unit_id")
	default:
		add("source_unit_id", *want.SourceUnitID == *got.SourceUnitID)
	}
	return cols
}

// requireBelief reads id back through BeliefByID and fails unless every
// column equals want.
func requireBelief(t *testing.T, repo ports.SelfModelRepo, label string, want ports.Belief) {
	t.Helper()

	got, err := repo.BeliefByID(context.Background(), want.ID)
	if err != nil {
		t.Fatalf("%s: BeliefByID(%q): %v", label, want.ID, err)
	}
	if cols := beliefColumnsDiffering(want, got); len(cols) != 0 {
		t.Errorf("%s: belief %q differs in columns %v\n got: %+v\nwant: %+v", label, want.ID, cols, got, want)
	}
}

// requireOthersUnchanged fails unless every seeded belief other than the
// ones named in except still equals its seeded row — the "nothing else
// moved" half of every write test.
func requireOthersUnchanged(t *testing.T, repo ports.SelfModelRepo, label string, seeded map[string]ports.Belief, except ...string) {
	t.Helper()

	skip := make(map[string]bool, len(except))
	for _, id := range except {
		skip[id] = true
	}
	for id, want := range seeded {
		if skip[id] {
			continue
		}
		requireBelief(t, repo, label+" (bystander)", want)
	}
}

func beliefIDs(beliefs []ports.Belief) []string {
	ids := make([]string, 0, len(beliefs))
	for _, b := range beliefs {
		ids = append(ids, b.ID)
	}
	sort.Strings(ids)
	return ids
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// RunRetiredBeliefs runs the ports.SelfModelRepo.RetiredBeliefs contract.
// newRepo must return a repository with no belief already stored in it.
func RunRetiredBeliefs(t *testing.T, newRepo func(t *testing.T) ports.SelfModelRepo) {
	t.Helper()

	t.Run("returns exactly the retired beliefs, every facet, and partitions with ActiveBeliefs", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		seeded := seedFXB(t, repo)

		// A second retired belief in a different facet: RetiredBeliefs carries
		// no facet filter.
		valueRetired := fxbBelief("b-value-retired", selfmodel.FacetValue, selfmodel.OriginUserStated, selfmodel.StatusRetired, 0.55, 60)
		if err := repo.UpsertByTopicKey(ctx, valueRetired); err != nil {
			t.Fatalf("seed %s: %v", valueRetired.ID, err)
		}
		seeded[valueRetired.ID] = valueRetired

		retired, err := repo.RetiredBeliefs(ctx)
		if err != nil {
			t.Fatalf("RetiredBeliefs: %v", err)
		}
		if got, want := beliefIDs(retired), []string{idGoalRetired, "b-value-retired"}; !equalStrings(got, want) {
			t.Fatalf("RetiredBeliefs ids = %v, want %v", got, want)
		}
		for _, b := range retired {
			if b.Status != selfmodel.StatusRetired {
				t.Errorf("RetiredBeliefs returned %s with status %q, want retired", b.ID, b.Status)
			}
			if cols := beliefColumnsDiffering(seeded[b.ID], b); len(cols) != 0 {
				t.Errorf("RetiredBeliefs row %s differs from what was stored in columns %v", b.ID, cols)
			}
		}

		active, err := repo.ActiveBeliefs(ctx)
		if err != nil {
			t.Fatalf("ActiveBeliefs: %v", err)
		}
		all := append(beliefIDs(active), beliefIDs(retired)...)
		sort.Strings(all)
		wantAll := make([]string, 0, len(seeded))
		for id := range seeded {
			wantAll = append(wantAll, id)
		}
		sort.Strings(wantAll)
		if !equalStrings(all, wantAll) {
			t.Errorf("active + retired ids = %v, want every seeded belief exactly once: %v", all, wantAll)
		}
	})

	t.Run("no retired belief stored returns none, even with active beliefs present", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		if err := repo.UpsertByTopicKey(ctx, fxbBelief("only-active", selfmodel.FacetGoal, selfmodel.OriginDerived, selfmodel.StatusActive, 0.6, 1)); err != nil {
			t.Fatalf("seed: %v", err)
		}

		got, err := repo.RetiredBeliefs(ctx)
		if err != nil {
			t.Fatalf("RetiredBeliefs: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("RetiredBeliefs() = %v, want none (the only stored belief is active)", got)
		}
	})
}

// RunBeliefByID runs the ports.SelfModelRepo.BeliefByID contract.
func RunBeliefByID(t *testing.T, newRepo func(t *testing.T) ports.SelfModelRepo) {
	t.Helper()

	t.Run("returns every column of an active and of a retired belief", func(t *testing.T) {
		repo := newRepo(t)
		seeded := seedFXB(t, repo)

		for id, want := range seeded {
			requireBelief(t, repo, "BeliefByID "+id, want)
		}
	})

	t.Run("an unknown id is ErrBeliefNotFound", func(t *testing.T) {
		repo := newRepo(t)
		seedFXB(t, repo)

		got, err := repo.BeliefByID(context.Background(), "no-such-belief")
		if !errors.Is(err, ports.ErrBeliefNotFound) {
			t.Fatalf("BeliefByID(unknown) error = %v, want ports.ErrBeliefNotFound", err)
		}
		if got.ID != "" {
			t.Errorf("BeliefByID(unknown) returned %+v alongside the error, want the zero Belief", got)
		}
	})
}

// RunSetStatus runs the ports.SelfModelRepo.SetStatus contract.
func RunSetStatus(t *testing.T, newRepo func(t *testing.T) ports.SelfModelRepo) {
	t.Helper()

	retireAt := fxbBase.Add(3 * time.Hour)

	t.Run("retires an active belief: status and updated_at move, nothing else does", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		seeded := seedFXB(t, repo)

		if err := repo.SetStatus(ctx, idGoalDerived, selfmodel.StatusActive, selfmodel.StatusRetired, retireAt); err != nil {
			t.Fatalf("SetStatus: %v", err)
		}

		want := seeded[idGoalDerived]
		want.Status = selfmodel.StatusRetired
		want.UpdatedAt = retireAt
		requireBelief(t, repo, "retired belief", want)
		requireOthersUnchanged(t, repo, "after SetStatus", seeded, idGoalDerived)

		active, err := repo.ActiveBeliefs(ctx)
		if err != nil {
			t.Fatalf("ActiveBeliefs: %v", err)
		}
		for _, b := range active {
			if b.ID == idGoalDerived {
				t.Errorf("ActiveBeliefs still returns %s after it was retired", idGoalDerived)
			}
		}
		retired, err := repo.RetiredBeliefs(ctx)
		if err != nil {
			t.Fatalf("RetiredBeliefs: %v", err)
		}
		if got, wantIDs := beliefIDs(retired), []string{idGoalDerived, idGoalRetired}; !equalStrings(got, wantIDs) {
			t.Errorf("RetiredBeliefs ids = %v, want %v", got, wantIDs)
		}
	})

	t.Run("retiring twice is a conflict and the second call changes nothing", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		seeded := seedFXB(t, repo)

		if err := repo.SetStatus(ctx, idGoalDerived, selfmodel.StatusActive, selfmodel.StatusRetired, retireAt); err != nil {
			t.Fatalf("first SetStatus: %v", err)
		}
		afterFirst := seeded[idGoalDerived]
		afterFirst.Status = selfmodel.StatusRetired
		afterFirst.UpdatedAt = retireAt

		err := repo.SetStatus(ctx, idGoalDerived, selfmodel.StatusActive, selfmodel.StatusRetired, retireAt.Add(time.Hour))
		if !errors.Is(err, ports.ErrBeliefStatusConflict) {
			t.Fatalf("second SetStatus error = %v, want ports.ErrBeliefStatusConflict", err)
		}
		requireBelief(t, repo, "after the conflicting call", afterFirst)
		requireOthersUnchanged(t, repo, "after the conflicting call", seeded, idGoalDerived)
	})

	t.Run("a from that does not match is a conflict, not a validation", func(t *testing.T) {
		repo := newRepo(t)
		seeded := seedFXB(t, repo)

		// The belief is active; the caller believes it is retired.
		err := repo.SetStatus(context.Background(), idGoalDerived, selfmodel.StatusRetired, selfmodel.StatusActive, retireAt)
		if !errors.Is(err, ports.ErrBeliefStatusConflict) {
			t.Fatalf("SetStatus(from=retired) on an active belief error = %v, want ports.ErrBeliefStatusConflict", err)
		}
		requireBelief(t, repo, "mismatched from", seeded[idGoalDerived])
	})

	t.Run("un-retire (retired -> active) is allowed at the store level: the user's-word-wins rule lives on derive's paths, not here", func(t *testing.T) {
		repo := newRepo(t)
		seeded := seedFXB(t, repo)

		// Design OR-1 reserves this transition for a future user-initiated
		// caller. The store must not refuse it; from is a precondition and
		// to is written as given, provided both are known statuses.
		if err := repo.SetStatus(context.Background(), idGoalRetired, selfmodel.StatusRetired, selfmodel.StatusActive, retireAt); err != nil {
			t.Fatalf("SetStatus(retired -> active): %v", err)
		}
		want := seeded[idGoalRetired]
		want.Status = selfmodel.StatusActive
		want.UpdatedAt = retireAt
		requireBelief(t, repo, "retired -> active", want)
		requireOthersUnchanged(t, repo, "after retired -> active", seeded, idGoalRetired)
	})

	t.Run("from == to is accepted like UnitRepo.SetStatus: status stays, updated_at moves", func(t *testing.T) {
		repo := newRepo(t)
		seeded := seedFXB(t, repo)

		if err := repo.SetStatus(context.Background(), idGoalDerived, selfmodel.StatusActive, selfmodel.StatusActive, retireAt); err != nil {
			t.Fatalf("SetStatus(active -> active): %v", err)
		}
		want := seeded[idGoalDerived]
		want.UpdatedAt = retireAt
		requireBelief(t, repo, "active -> active", want)
		requireOthersUnchanged(t, repo, "after active -> active", seeded, idGoalDerived)
	})

	t.Run("a from or to outside AllStatuses is ErrBeliefStatusInvalid and writes nothing", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			from, to selfmodel.Status
		}{
			{"unknown to", selfmodel.StatusActive, selfmodel.Status("archived")},
			{"empty to", selfmodel.StatusActive, selfmodel.Status("")},
			{"unknown from", selfmodel.Status("archived"), selfmodel.StatusRetired},
			{"empty from", selfmodel.Status(""), selfmodel.StatusRetired},
			{"both unknown", selfmodel.Status("x"), selfmodel.Status("y")},
		} {
			t.Run(tc.name, func(t *testing.T) {
				repo := newRepo(t)
				seeded := seedFXB(t, repo)

				err := repo.SetStatus(context.Background(), idGoalDerived, tc.from, tc.to, retireAt)
				if !errors.Is(err, ports.ErrBeliefStatusInvalid) {
					t.Fatalf("SetStatus(%q -> %q) error = %v, want ports.ErrBeliefStatusInvalid", tc.from, tc.to, err)
				}
				requireOthersUnchanged(t, repo, "after an invalid status", seeded)
			})
		}

		t.Run("validation runs before the id lookup", func(t *testing.T) {
			repo := newRepo(t)
			err := repo.SetStatus(context.Background(), "no-such-belief", selfmodel.StatusActive, selfmodel.Status("archived"), retireAt)
			if !errors.Is(err, ports.ErrBeliefStatusInvalid) {
				t.Fatalf("SetStatus(unknown id, invalid to) error = %v, want ports.ErrBeliefStatusInvalid", err)
			}
		})
	})

	t.Run("an unknown id is ErrBeliefNotFound and writes nothing", func(t *testing.T) {
		repo := newRepo(t)
		seeded := seedFXB(t, repo)

		err := repo.SetStatus(context.Background(), "no-such-belief", selfmodel.StatusActive, selfmodel.StatusRetired, retireAt)
		if !errors.Is(err, ports.ErrBeliefNotFound) {
			t.Fatalf("SetStatus(unknown) error = %v, want ports.ErrBeliefNotFound", err)
		}
		requireOthersUnchanged(t, repo, "after SetStatus(unknown)", seeded)
	})
}

// RunEditContent runs the ports.SelfModelRepo.EditContent contract.
func RunEditContent(t *testing.T, newRepo func(t *testing.T) ports.SelfModelRepo) {
	t.Helper()

	editAt := fxbBase.Add(5 * time.Hour)
	const edited = "the user's own words\nsecond line"

	t.Run("edits content, marks user_stated, bumps updated_at, and moves nothing else", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		seeded := seedFXB(t, repo)

		target := seeded[idGoalDerived]
		if err := repo.EditContent(ctx, target.ID, target.Content, edited, editAt); err != nil {
			t.Fatalf("EditContent: %v", err)
		}

		want := target
		want.Content = edited
		want.Origin = selfmodel.OriginUserStated
		want.UpdatedAt = editAt
		requireBelief(t, repo, "edited belief", want)
		requireOthersUnchanged(t, repo, "after EditContent", seeded, target.ID)
	})

	t.Run("a seed belief and an already user_stated belief both end up user_stated", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()
		seeded := seedFXB(t, repo)

		for _, id := range []string{idValueSeed, idGoalUser} {
			target := seeded[id]
			if err := repo.EditContent(ctx, id, target.Content, edited+" "+id, editAt); err != nil {
				t.Fatalf("EditContent %s: %v", id, err)
			}
			want := target
			want.Content = edited + " " + id
			want.Origin = selfmodel.OriginUserStated
			want.UpdatedAt = editAt
			requireBelief(t, repo, "edited "+id, want)
		}
		requireOthersUnchanged(t, repo, "after two edits", seeded, idValueSeed, idGoalUser)
	})

	t.Run("to equal to from still claims: user_stated, content byte-identical, updated_at bumped", func(t *testing.T) {
		repo := newRepo(t)
		seeded := seedFXB(t, repo)

		for _, id := range []string{idGoalDerived, idValueSeed} {
			target := seeded[id]
			if err := repo.EditContent(context.Background(), id, target.Content, target.Content, editAt); err != nil {
				t.Fatalf("EditContent(to == from) %s: %v", id, err)
			}
			want := target
			want.Origin = selfmodel.OriginUserStated
			want.UpdatedAt = editAt
			requireBelief(t, repo, "claimed "+id, want)
		}
		requireOthersUnchanged(t, repo, "after two claims", seeded, idGoalDerived, idValueSeed)
	})

	t.Run("a stale from is a conflict and nothing changes", func(t *testing.T) {
		repo := newRepo(t)
		seeded := seedFXB(t, repo)

		err := repo.EditContent(context.Background(), idGoalDerived, "content as someone else last saw it", edited, editAt)
		if !errors.Is(err, ports.ErrBeliefStatusConflict) {
			t.Fatalf("EditContent(stale from) error = %v, want ports.ErrBeliefStatusConflict", err)
		}
		requireOthersUnchanged(t, repo, "after a stale-from edit", seeded)
	})

	t.Run("a retired belief is a conflict and nothing changes", func(t *testing.T) {
		repo := newRepo(t)
		seeded := seedFXB(t, repo)

		retired := seeded[idGoalRetired]
		err := repo.EditContent(context.Background(), retired.ID, retired.Content, edited, editAt)
		if !errors.Is(err, ports.ErrBeliefStatusConflict) {
			t.Fatalf("EditContent(retired) error = %v, want ports.ErrBeliefStatusConflict", err)
		}
		requireOthersUnchanged(t, repo, "after editing a retired belief", seeded)
	})

	t.Run("an unknown id is ErrBeliefNotFound and writes nothing", func(t *testing.T) {
		repo := newRepo(t)
		seeded := seedFXB(t, repo)

		err := repo.EditContent(context.Background(), "no-such-belief", "anything", edited, editAt)
		if !errors.Is(err, ports.ErrBeliefNotFound) {
			t.Fatalf("EditContent(unknown) error = %v, want ports.ErrBeliefNotFound", err)
		}
		requireOthersUnchanged(t, repo, "after EditContent(unknown)", seeded)
	})
}

// RunBeliefWriteGuards runs the store-level guards on the two derive
// writes (m4e design G1): neither may touch a retired belief, and the
// upsert may overwrite a row only while it is active AND derived.
func RunBeliefWriteGuards(t *testing.T, newRepo func(t *testing.T) ports.SelfModelRepo) {
	t.Helper()

	// attempt builds the write derive would make over an existing belief's
	// topic_key: a fresh id, fresh content, active and derived.
	attempt := func(over ports.Belief) ports.Belief {
		b := fxbBelief("b-attempt-"+over.ID, over.Facet, selfmodel.OriginDerived, selfmodel.StatusActive, 0.99, 90)
		b.TopicKey = over.TopicKey
		b.Content = "what derive wanted to write"
		return b
	}

	for _, tc := range []struct {
		name string
		id   string
	}{
		{"a retired key", idGoalRetired},
		{"a user_stated key", idGoalUser},
		{"a seed key", idValueSeed},
	} {
		t.Run("upsert over "+tc.name+" is ErrBeliefProtected and the row is byte-identical", func(t *testing.T) {
			repo := newRepo(t)
			seeded := seedFXB(t, repo)

			b := attempt(seeded[tc.id])
			err := repo.UpsertByTopicKey(context.Background(), b)
			if !errors.Is(err, ports.ErrBeliefProtected) {
				t.Fatalf("UpsertByTopicKey over %s error = %v, want ports.ErrBeliefProtected", tc.name, err)
			}
			requireOthersUnchanged(t, repo, "after a refused upsert", seeded)
			if _, err := repo.BeliefByID(context.Background(), b.ID); !errors.Is(err, ports.ErrBeliefNotFound) {
				t.Errorf("BeliefByID(%q) error = %v, want ErrBeliefNotFound: a refused upsert must not leave a second row", b.ID, err)
			}
		})
	}

	t.Run("an insert whose id is already held under another topic_key is refused and changes nothing", func(t *testing.T) {
		repo := newRepo(t)
		seeded := seedFXB(t, repo)

		// SQLite refuses this with a primary-key error; the in-memory fake
		// must refuse it too instead of silently replacing the other row.
		clash := fxbBelief(idGoalDerived, selfmodel.FacetGoal, selfmodel.OriginDerived, selfmodel.StatusActive, 0.5, 91)
		clash.TopicKey = "a-topic-key-nobody-holds"
		clash.Content = "what derive wanted to write"
		if err := repo.UpsertByTopicKey(context.Background(), clash); err == nil {
			t.Fatal("UpsertByTopicKey with an id held under another topic_key returned nil, want an error")
		}
		requireOthersUnchanged(t, repo, "after the refused insert", seeded)
	})

	t.Run("upsert over an active derived key still overwrites in place (m2c R2.1)", func(t *testing.T) {
		repo := newRepo(t)
		seeded := seedFXB(t, repo)

		old := seeded[idIdentityDeriv]
		b := attempt(old)
		if err := repo.UpsertByTopicKey(context.Background(), b); err != nil {
			t.Fatalf("UpsertByTopicKey over an active derived key: %v", err)
		}

		got, err := repo.BeliefByID(context.Background(), old.ID)
		if err != nil {
			t.Fatalf("BeliefByID(%q): %v", old.ID, err)
		}
		// Identity is kept; the write's own values replace the row's. (created_at
		// is deliberately not asserted: it is outside m2c R2.1's text.)
		if got.Content != b.Content || got.Confidence != b.Confidence ||
			got.Origin != selfmodel.OriginDerived || got.Status != selfmodel.StatusActive ||
			!got.LastReinforcedAt.Equal(b.LastReinforcedAt) || !got.UpdatedAt.Equal(b.UpdatedAt) {
			t.Errorf("overwritten row = %+v, want the new write's values on the old identity %q", got, old.ID)
		}
		if _, err := repo.BeliefByID(context.Background(), b.ID); !errors.Is(err, ports.ErrBeliefNotFound) {
			t.Errorf("BeliefByID(%q) error = %v, want ErrBeliefNotFound: the overwrite keeps the first row's id", b.ID, err)
		}
		requireOthersUnchanged(t, repo, "after an overwrite", seeded, old.ID)
	})

	t.Run("reinforcing a retired belief is ErrBeliefStatusConflict and changes nothing", func(t *testing.T) {
		repo := newRepo(t)
		seeded := seedFXB(t, repo)

		err := repo.ReinforceByID(context.Background(), idGoalRetired, 0.99, fxbBase.Add(9*time.Hour))
		if !errors.Is(err, ports.ErrBeliefStatusConflict) {
			t.Fatalf("ReinforceByID(retired) error = %v, want ports.ErrBeliefStatusConflict", err)
		}
		requireOthersUnchanged(t, repo, "after reinforcing a retired belief", seeded)
	})

	t.Run("reinforcing an unknown id is ErrBeliefNotFound, distinct from the retired case", func(t *testing.T) {
		repo := newRepo(t)
		seeded := seedFXB(t, repo)

		err := repo.ReinforceByID(context.Background(), "no-such-belief", 0.99, fxbBase.Add(9*time.Hour))
		if !errors.Is(err, ports.ErrBeliefNotFound) {
			t.Fatalf("ReinforceByID(unknown) error = %v, want ports.ErrBeliefNotFound", err)
		}
		requireOthersUnchanged(t, repo, "after reinforcing an unknown id", seeded)
	})

	t.Run("a user_stated belief can still be reinforced: only confidence and last_reinforced_at move", func(t *testing.T) {
		repo := newRepo(t)
		seeded := seedFXB(t, repo)

		at := fxbBase.Add(9 * time.Hour)
		if err := repo.ReinforceByID(context.Background(), idGoalUser, 0.91, at); err != nil {
			t.Fatalf("ReinforceByID(user_stated): %v", err)
		}
		want := seeded[idGoalUser]
		want.Confidence = 0.91
		want.LastReinforcedAt = at
		requireBelief(t, repo, "reinforced user_stated belief", want)
		requireOthersUnchanged(t, repo, "after reinforcing", seeded, idGoalUser)
	})
}
