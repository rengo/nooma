package brain_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/core/weight"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

var unitsNow = time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)

// spyUnits wraps memrepo.Units, recording every LiveBrowsePage call and
// answering with a canned page — TestUnitsService_BrowsePassesThrough's own
// fixture. Every other method is promoted from the embedded fake unchanged.
type spyUnits struct {
	*memrepo.Units
	browseCalls int
	gotTypes    []unit.Type
	gotAfter    *ports.BrowseCursor
	page        ports.BrowsePage
}

func (s *spyUnits) LiveBrowsePage(_ context.Context, types []unit.Type, after *ports.BrowseCursor) (ports.BrowsePage, error) {
	s.browseCalls++
	s.gotTypes = types
	s.gotAfter = after
	return s.page, nil
}

// TestUnitsService_BrowsePassesThrough is design §3.3's own wording:
// "Browse ... pass-through". Proven by a spy rather than by comparing
// against a second real UnitRepo.LiveBrowsePage call, so the test fails if
// Browse narrows, reorders or otherwise touches what LiveBrowsePage
// already decided.
func TestUnitsService_BrowsePassesThrough(t *testing.T) {
	ctx := context.Background()
	wantPage := ports.BrowsePage{
		Units: []unit.Unit{{ID: "u-1", Type: unit.TypeTask, Status: unit.StatusPool}},
		Next:  &ports.BrowseCursor{CreatedAt: unitsNow, ID: "u-1"},
	}
	fake := &spyUnits{Units: memrepo.NewUnits(), page: wantPage}
	svc := brain.NewUnitsService(fake, memrepo.NewRelations())

	types := []unit.Type{unit.TypeTask, unit.TypeKnowledge}
	after := &ports.BrowseCursor{CreatedAt: unitsNow, ID: "cursor-unit"}

	got, err := svc.Browse(ctx, types, after)
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	if fake.browseCalls != 1 {
		t.Fatalf("LiveBrowsePage called %d times, want 1", fake.browseCalls)
	}
	if !reflect.DeepEqual(fake.gotTypes, types) {
		t.Fatalf("LiveBrowsePage received types = %v, want %v — Browse must forward types unchanged", fake.gotTypes, types)
	}
	if fake.gotAfter != after {
		t.Fatalf("LiveBrowsePage received a different *BrowseCursor than Browse was given — Browse must forward after unchanged, not copy or rebuild it")
	}
	if !reflect.DeepEqual(got, wantPage) {
		t.Fatalf("Browse returned %+v, want %+v — Browse must return LiveBrowsePage's result unchanged", got, wantPage)
	}
}

func seedUnitsUnit(t *testing.T, units *memrepo.Units, id string, status unit.Status) {
	t.Helper()
	if err := units.Create(context.Background(), unit.Unit{
		ID: id, Type: unit.TypeTask, Status: status, Content: id,
		Weight: 1, LastTouchedAt: unitsNow, CreatedAt: unitsNow, UpdatedAt: unitsNow,
	}); err != nil {
		t.Fatalf("seed unit %s: %v", id, err)
	}
}

// TestUnitsService_DetailNotFoundForNonLive is spec R3's own scenario: "an
// archived unit's detail page is not reachable" — through
// UnitRepo.LiveByIDs's existing positive filter, not a status check Detail
// invents itself.
func TestUnitsService_DetailNotFoundForNonLive(t *testing.T) {
	ctx := context.Background()
	units := memrepo.NewUnits()
	seedUnitsUnit(t, units, "u-archived", unit.StatusArchived)
	seedUnitsUnit(t, units, "u-superseded", unit.StatusSuperseded)
	seedUnitsUnit(t, units, "u-incomplete", unit.StatusIncomplete)

	svc := brain.NewUnitsService(units, memrepo.NewRelations())

	cases := []string{"u-archived", "u-superseded", "u-incomplete", "u-absent"}
	for _, id := range cases {
		detail, found, err := svc.Detail(ctx, id)
		if err != nil {
			t.Fatalf("Detail(%q): %v", id, err)
		}
		if found {
			t.Fatalf("Detail(%q) found = true, want false — status is not pool", id)
		}
		if !reflect.DeepEqual(detail, brain.UnitDetail{}) {
			t.Fatalf("Detail(%q) returned a non-zero UnitDetail on found=false: %+v", id, detail)
		}
	}
}

// TestUnitsService_DetailDropsNonLiveNeighbours is spec R3's own scenario:
// "a relation to a non-live unit is omitted from a live unit's detail
// page" — I02 applied to neighbours, enforced through LiveByIDs and not
// reimplemented as a status check inside Detail.
func TestUnitsService_DetailDropsNonLiveNeighbours(t *testing.T) {
	ctx := context.Background()
	units := memrepo.NewUnits()
	seedUnitsUnit(t, units, "center", unit.StatusPool)
	seedUnitsUnit(t, units, "live-neighbour", unit.StatusPool)
	seedUnitsUnit(t, units, "archived-neighbour", unit.StatusArchived)

	rels := memrepo.NewRelations()
	if err := rels.Upsert(ctx, ports.Relation{
		ID: "rel-live", FromUnitID: "center", ToUnitID: "live-neighbour",
		Type: "same_topic", Strength: 0.5, Confidence: 0.9, CreatedBy: "test", CreatedAt: unitsNow,
	}); err != nil {
		t.Fatalf("seed live relation: %v", err)
	}
	if err := rels.Upsert(ctx, ports.Relation{
		ID: "rel-archived", FromUnitID: "archived-neighbour", ToUnitID: "center",
		Type: "same_topic", Strength: 0.5, Confidence: 0.9, CreatedBy: "test", CreatedAt: unitsNow.Add(time.Second),
	}); err != nil {
		t.Fatalf("seed archived relation: %v", err)
	}

	svc := brain.NewUnitsService(units, rels)

	detail, found, err := svc.Detail(ctx, "center")
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if !found {
		t.Fatalf("Detail(center) found = false, want true — center is pool")
	}
	if detail.Unit.ID != "center" {
		t.Fatalf("Detail(center).Unit.ID = %q, want %q", detail.Unit.ID, "center")
	}
	if len(detail.Relations) != 1 {
		t.Fatalf("len(Relations) = %d, want 1 — the archived neighbour must be dropped: %+v", len(detail.Relations), detail.Relations)
	}
	got := detail.Relations[0]
	if got.RelationID != "rel-live" {
		t.Fatalf("Relations[0].RelationID = %q, want %q — the surviving relation must be the live-neighbour one", got.RelationID, "rel-live")
	}
	if !got.Outgoing {
		t.Fatalf("Relations[0].Outgoing = false, want true — center is rel-live's FromUnitID")
	}
	if got.Other.ID != "live-neighbour" {
		t.Fatalf("Relations[0].Other.ID = %q, want %q", got.Other.ID, "live-neighbour")
	}
}

// unitsWriteGuard wraps memrepo.Units, failing the test on any of
// UnitRepo's six write methods — TestUnitsService_ReadsWriteNothing's own
// fixture, a minimal I27-shaped fake covering only what Browse/Detail could
// reach (PR 2's overflow cut, tasks.md).
type unitsWriteGuard struct {
	*memrepo.Units
	t *testing.T
}

func unitsWriteGuardFail(t *testing.T, method string) {
	t.Helper()
	t.Fatalf("UnitsService called UnitRepo.%s — Browse and Detail are read-only (I27 shape)", method)
}

func (g *unitsWriteGuard) Create(context.Context, unit.Unit) error {
	unitsWriteGuardFail(g.t, "Create")
	return nil
}
func (g *unitsWriteGuard) UpdateContent(context.Context, string, string, time.Time) error {
	unitsWriteGuardFail(g.t, "UpdateContent")
	return nil
}
func (g *unitsWriteGuard) UpdateEventAt(context.Context, string, time.Time, time.Time) error {
	unitsWriteGuardFail(g.t, "UpdateEventAt")
	return nil
}
func (g *unitsWriteGuard) UpdateDueAt(context.Context, string, time.Time, time.Time) error {
	unitsWriteGuardFail(g.t, "UpdateDueAt")
	return nil
}
func (g *unitsWriteGuard) SetStatus(context.Context, string, unit.Status, unit.Status, time.Time) error {
	unitsWriteGuardFail(g.t, "SetStatus")
	return nil
}
func (g *unitsWriteGuard) ApplyBoosts(context.Context, []weight.Boost, time.Time) error {
	unitsWriteGuardFail(g.t, "ApplyBoosts")
	return nil
}

// relationsWriteGuard wraps memrepo.Relations, failing the test on
// RelationRepo's two write methods — Upsert and Delete (I10, port doc
// comment).
type relationsWriteGuard struct {
	*memrepo.Relations
	t *testing.T
}

func (g *relationsWriteGuard) Upsert(context.Context, ports.Relation) error {
	unitsWriteGuardFail(g.t, "RelationRepo.Upsert")
	return nil
}
func (g *relationsWriteGuard) Delete(context.Context, string) error {
	unitsWriteGuardFail(g.t, "RelationRepo.Delete")
	return nil
}

// TestUnitsService_ReadsWriteNothing is I27's shape applied to
// UnitsService: neither Browse nor Detail may call a write method on
// UnitRepo or RelationRepo.
func TestUnitsService_ReadsWriteNothing(t *testing.T) {
	ctx := context.Background()
	realUnits := memrepo.NewUnits()
	seedUnitsUnit(t, realUnits, "center", unit.StatusPool)
	seedUnitsUnit(t, realUnits, "neighbour", unit.StatusPool)

	realRels := memrepo.NewRelations()
	if err := realRels.Upsert(ctx, ports.Relation{
		ID: "rel-1", FromUnitID: "center", ToUnitID: "neighbour",
		Type: "same_topic", Strength: 0.5, Confidence: 0.9, CreatedBy: "test", CreatedAt: unitsNow,
	}); err != nil {
		t.Fatalf("seed relation: %v", err)
	}

	units := &unitsWriteGuard{Units: realUnits, t: t}
	rels := &relationsWriteGuard{Relations: realRels, t: t}
	svc := brain.NewUnitsService(units, rels)

	if _, err := svc.Browse(ctx, nil, nil); err != nil {
		t.Fatalf("Browse: %v", err)
	}
	if _, _, err := svc.Detail(ctx, "center"); err != nil {
		t.Fatalf("Detail: %v", err)
	}
}
