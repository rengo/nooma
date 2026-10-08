package brain

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

// actBase is the instant every activity fixture hangs off. Every time here is
// a whole second (FX-A): SQLite stores seconds and memrepo nanoseconds, so a
// sub-second fixture could pass here and fail there.
var actBase = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

// actCountingLog counts reads, so a test can prove a request read nothing.
type actCountingLog struct {
	ports.DecisionLog
	reads int
}

func (c *actCountingLog) Before(ctx context.Context, b *ports.DecisionCursor, prefix string, limit int) ([]ports.DecisionRow, error) {
	c.reads++
	return c.DecisionLog.Before(ctx, b, prefix, limit)
}

func actRecord(t *testing.T, log ports.DecisionLog, id string, action ports.DecisionAction, at time.Time, ctxJSON string) {
	t.Helper()
	if at.Nanosecond() != 0 {
		t.Fatalf("fixture %s has a sub-second time %v: FX-A allows whole seconds only", id, at)
	}
	d := ports.Decision{ID: id, Action: action, Rationale: "why " + id, OccurredAt: at}
	if ctxJSON != "" {
		d.Context = []byte(ctxJSON)
	}
	if err := log.Record(context.Background(), d); err != nil {
		t.Fatalf("Record %s: %v", id, err)
	}
}

// actWalk reads every page from the newest, returning the ids in order and
// the number of pages. It fails a page above ActivityPageSize.
func actWalk(t *testing.T, svc *ActivityService, kind string) (ids []string, pages int) {
	t.Helper()
	var cursor *ports.DecisionCursor
	for pages < 20 {
		page, err := svc.Page(context.Background(), kind, cursor)
		if err != nil {
			t.Fatalf("Page %d: %v", pages, err)
		}
		if len(page.Rows) > ActivityPageSize {
			t.Fatalf("page %d holds %d rows, want at most %d", pages, len(page.Rows), ActivityPageSize)
		}
		pages++
		for _, r := range page.Rows {
			ids = append(ids, r.ID)
		}
		if page.Next == nil {
			return ids, pages
		}
		cursor = page.Next
	}
	t.Fatal("the walk did not end after 20 pages")
	return nil, 0
}

func actHead(ids []string) []string {
	return ids[:min(len(ids), 3)]
}

// A pass-sized tie group: 2 pages and a bit, one instant, ids in descending
// write order so neither id order nor a reversed Since can pass by accident.
func TestActivityPage_TieGroupLargerThanAPageIsWalkedExactlyOnce(t *testing.T) {
	log := memrepo.NewDecisionLog()
	n := 2*ActivityPageSize + 10
	var want []string
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("id-%03d", n-i)
		actRecord(t, log, id, ports.ActionCaptureUnitCreated, actBase, "")
		want = append([]string{id}, want...)
	}

	got, pages := actWalk(t, NewActivityService(log), "")

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("walk = %d rows, want %d in reverse write order (first of each: %v / %v)", len(got), len(want), actHead(got), actHead(want))
	}
	if pages != 3 {
		t.Errorf("pages = %d, want 3", pages)
	}
}

func TestActivityPage_OlderLinkOnlyWhenMoreRowsExist(t *testing.T) {
	cases := []struct {
		name     string
		rows     int
		wantNext bool
	}{
		{"exactly one page", ActivityPageSize, false},
		{"one row over", ActivityPageSize + 1, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			log := memrepo.NewDecisionLog()
			for i := 0; i < c.rows; i++ {
				actRecord(t, log, fmt.Sprintf("r-%03d", i), ports.ActionCaptureUnitCreated, actBase.Add(time.Duration(i)*time.Second), "")
			}
			page, err := NewActivityService(log).Page(context.Background(), "", nil)
			if err != nil {
				t.Fatalf("Page: %v", err)
			}
			if len(page.Rows) != ActivityPageSize {
				t.Fatalf("rows = %d, want %d", len(page.Rows), ActivityPageSize)
			}
			if (page.Next != nil) != c.wantNext {
				t.Fatalf("Next = %v, want present=%v", page.Next, c.wantNext)
			}
			if !c.wantNext {
				return
			}
			// The extra row only signals "more": it is not shown, and the
			// cursor is the last row that IS shown.
			last := page.Rows[len(page.Rows)-1]
			if last.ID != "r-001" {
				t.Errorf("last shown row = %s, want r-001 (the 51st, r-000, is the extra)", last.ID)
			}
			for _, r := range page.Rows {
				if r.ID == "r-000" {
					t.Error("the 51st row is shown on page one")
				}
			}
			want := ports.DecisionCursor{OccurredAt: last.OccurredAt, Seq: last.Seq}
			if *page.Next != want {
				t.Errorf("Next = %+v, want the last shown row's own cursor %+v", *page.Next, want)
			}
		})
	}
}

// Page one must hold the NEWEST rows (a reversed Since would hold the oldest).
func TestActivityPage_FirstPageHoldsTheNewest(t *testing.T) {
	log := memrepo.NewDecisionLog()
	total := 2*ActivityPageSize + 20
	for i := 0; i < total; i++ {
		actRecord(t, log, fmt.Sprintf("r-%03d", i), ports.ActionCaptureUnitCreated, actBase.Add(time.Duration(i)*time.Second), "")
	}
	page, err := NewActivityService(log).Page(context.Background(), "", nil)
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if len(page.Rows) != ActivityPageSize {
		t.Fatalf("rows = %d, want %d", len(page.Rows), ActivityPageSize)
	}
	if got, want := page.Rows[0].ID, fmt.Sprintf("r-%03d", total-1); got != want {
		t.Errorf("first row = %s, want the newest %s", got, want)
	}
	if got, want := page.Rows[len(page.Rows)-1].ID, fmt.Sprintf("r-%03d", total-ActivityPageSize); got != want {
		t.Errorf("last row of page one = %s, want %s", got, want)
	}
}

func TestActivityPage_UnknownKindReadsNothing(t *testing.T) {
	log := &actCountingLog{DecisionLog: memrepo.NewDecisionLog()}
	svc := NewActivityService(log)

	for _, kind := range []string{"bogus", "check.", "che", "Check", " "} {
		_, err := svc.Page(context.Background(), kind, nil)
		if !errors.Is(err, ErrUnknownActivityKind) {
			t.Errorf("Page(kind %q) error = %v, want ErrUnknownActivityKind", kind, err)
		}
	}
	if log.reads != 0 {
		t.Errorf("an unknown kind caused %d read(s), want 0", log.reads)
	}
}

func TestActivityPage_KindNarrowsToOneFamily(t *testing.T) {
	log := memrepo.NewDecisionLog()
	actRecord(t, log, "d-check", ports.ActionCheckTimerFired, actBase, "")
	actRecord(t, log, "d-checkin", ports.ActionCaptureCheckInResolved, actBase.Add(time.Second), "")
	actRecord(t, log, "d-check2", ports.ActionCheckDigestSent, actBase.Add(2*time.Second), "")

	got, _ := actWalk(t, NewActivityService(log), "check")

	if want := []string{"d-check2", "d-check"}; !reflect.DeepEqual(got, want) {
		t.Errorf("kind=check = %v, want %v (capture.checkin.* is not a check.* row)", got, want)
	}
}

func actSingle(t *testing.T, action ports.DecisionAction, ctxJSON string) ActivityRow {
	t.Helper()
	log := memrepo.NewDecisionLog()
	actRecord(t, log, "d-1", action, actBase, ctxJSON)
	page, err := NewActivityService(log).Page(context.Background(), "", nil)
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if len(page.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(page.Rows))
	}
	return page.Rows[0]
}

func TestActivityPage_CorrectionShowsPreviousAndNext(t *testing.T) {
	row := actSingle(t, ports.ActionCorrectionApplied,
		`{"unit_id":"u1","fields":["event_at"],"previous":{"event_at":"2026-09-01T10:00:00Z"},"next":{"event_at":"2026-09-02T10:00:00Z"},"referent":{"source":"explicit"}}`)

	want := []ChangedField{{Name: "event_at", Previous: "2026-09-01T10:00:00Z", Next: "2026-09-02T10:00:00Z"}}
	if !reflect.DeepEqual(row.Change, want) {
		t.Errorf("Change = %+v, want %+v", row.Change, want)
	}
}

// Whatever the context is, the page renders: a row that cannot be read as a
// change shows its rationale only (R6).
func TestActivityPage_MalformedContextStillRenders(t *testing.T) {
	for name, ctxJSON := range map[string]string{
		"not json":          `{"previous": `,
		"no previous":       `{"next":{"a":1}}`,
		"previous a string": `{"previous":"x","next":{"a":1}}`,
		"next null":         `{"previous":{"a":1},"next":null}`,
		"an array":          `[1,2]`,
		"empty":             ``,
	} {
		t.Run(name, func(t *testing.T) {
			row := actSingle(t, ports.ActionCorrectionApplied, ctxJSON)
			if row.Change != nil {
				t.Errorf("Change = %+v, want nil", row.Change)
			}
			if row.Rationale == "" {
				t.Error("the row lost its rationale")
			}
		})
	}
}

// The decoder is keyed to the shape, not to correction.applied: a
// config.updated-shaped row (m4e2-admin) renders with no change here. A
// number never renders with a trailing .0.
func TestActivityPage_ConfigUpdatedShapeRenders(t *testing.T) {
	row := actSingle(t, ports.ActionBeliefEdited,
		`{"fields":["goal_stagnation_days","consolidation_enabled","weight_threshold"],`+
			`"previous":{"goal_stagnation_days":21,"consolidation_enabled":true,"weight_threshold":0.5},`+
			`"next":{"goal_stagnation_days":28.0,"consolidation_enabled":false,"weight_threshold":0.6}}`)

	want := []ChangedField{
		{Name: "goal_stagnation_days", Previous: "21", Next: "28"},
		{Name: "consolidation_enabled", Previous: "true", Next: "false"},
		{Name: "weight_threshold", Previous: "0.5", Next: "0.6"},
	}
	if !reflect.DeepEqual(row.Change, want) {
		t.Errorf("Change = %+v, want %+v", row.Change, want)
	}
}

func TestActivityPage_ValueKindsAndFieldFallback(t *testing.T) {
	// No "fields": the sorted union of both objects' keys; null is "(none)";
	// a key present on one side only still shows.
	row := actSingle(t, ports.ActionCorrectionApplied,
		`{"previous":{"b":null,"a":0.05},"next":{"b":"text","c":[1,2]}}`)

	want := []ChangedField{
		{Name: "a", Previous: "0.05", Next: "(none)"},
		{Name: "b", Previous: "(none)", Next: "text"},
		{Name: "c", Previous: "(none)", Next: "[1,2]"},
	}
	if !reflect.DeepEqual(row.Change, want) {
		t.Errorf("Change = %+v, want %+v", row.Change, want)
	}

	// "fields" that does not key both objects is ignored for the union.
	row = actSingle(t, ports.ActionCorrectionApplied,
		`{"fields":["zzz"],"previous":{"a":1},"next":{"a":2}}`)
	if want := []ChangedField{{Name: "a", Previous: "1", Next: "2"}}; !reflect.DeepEqual(row.Change, want) {
		t.Errorf("Change = %+v, want %+v", row.Change, want)
	}
}

func TestActivityFamilies_DerivedFromVocabulary(t *testing.T) {
	synthetic := []ports.DecisionAction{"zzz.one", "check.x", "zzz.two", "check.y.z"}
	if got, want := ActivityFamilies(synthetic), []string{"check", "zzz"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ActivityFamilies(synthetic) = %v, want %v", got, want)
	}

	real := ActivityFamilies(ports.AllDecisionActions())
	if want := []string{"belief", "capture", "check", "consolidate", "correction", "relation"}; !reflect.DeepEqual(real, want) {
		t.Errorf("ActivityFamilies(real) = %v, want %v", real, want)
	}
	if again := ActivityFamilies(ports.AllDecisionActions()); !reflect.DeepEqual(real, again) {
		t.Errorf("order is not stable: %v then %v", real, again)
	}

	// A service built over a vocabulary with family zzz accepts ?kind=zzz and
	// still refuses a family that vocabulary lacks.
	log := &actCountingLog{DecisionLog: memrepo.NewDecisionLog()}
	svc := &ActivityService{log: log, actions: synthetic}
	if _, err := svc.Page(context.Background(), "zzz", nil); err != nil {
		t.Errorf("Page(zzz) over the synthetic vocabulary: %v", err)
	}
	if _, err := svc.Page(context.Background(), "capture", nil); !errors.Is(err, ErrUnknownActivityKind) {
		t.Errorf("Page(capture) over the synthetic vocabulary = %v, want ErrUnknownActivityKind", err)
	}
	if got := ActivityFamilies(nil); len(got) != 0 {
		t.Errorf("ActivityFamilies(nil) = %v, want empty", got)
	}
}
