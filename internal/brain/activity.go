package brain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// ActivityPageSize is the number of decisions one /ui/activity page shows.
// A transport constant, not a doc 02 §13 row: like ports.BrowsePageSize it
// shapes a page and decides nothing the cognitive core governs.
const ActivityPageSize = 50

// ErrUnknownActivityKind is Page's answer to a kind that is not a family of
// the action vocabulary. It is returned before any read.
var ErrUnknownActivityKind = errors.New("activity: unknown kind")

// ActivityService is the glass box's read model (doc 02 §11): what Nooma did,
// newest first, a page at a time. It holds a DecisionLog and writes nothing.
type ActivityService struct {
	log     ports.DecisionLog
	actions []ports.DecisionAction
	units   ActivityUnits
}

// ActivityUnits is the one unit read the activity page makes: the live units
// its rows name, so a row can say what it is about. *sqlite.UnitRepo and
// memrepo's Units satisfy it.
type ActivityUnits interface {
	LiveByIDs(ctx context.Context, ids []string) ([]unit.Unit, error)
}

// NewActivityService wires an ActivityService over log, filtering by the
// families of the full action vocabulary.
func NewActivityService(log ports.DecisionLog) *ActivityService {
	return &ActivityService{log: log, actions: ports.AllDecisionActions()}
}

// WithUnits lets each row carry the live unit it concerns as its Subject. The
// page reads the units it names once, whatever its length. Without it rows
// carry no subject.
func (s *ActivityService) WithUnits(units ActivityUnits) *ActivityService {
	s.units = units
	return s
}

// ActivitySubject is the live unit a row concerns: enough to name it and link
// to it.
type ActivitySubject struct {
	UnitID  string
	Content string
}

// ChangedField is one column of a change-shaped row, with its previous and
// next values already rendered as text.
type ChangedField struct {
	Name     string
	Previous string
	Next     string
}

// ActivityRow is one decision_log row and, when its context is
// change-shaped, the fields it changed.
type ActivityRow struct {
	ports.DecisionRow
	Change []ChangedField
	// Subject is the live unit the row's context names, nil when it names
	// none or that unit is no longer live.
	Subject *ActivitySubject
}

// ActivityPage is one page of rows and the cursor of the page after it, nil
// when this is the last page.
type ActivityPage struct {
	Rows []ActivityRow
	Next *ports.DecisionCursor
}

// Page returns one page of activity, newest first. kind is "" for every row
// or one family from ActivityFamilies, and an unknown kind is refused with
// ErrUnknownActivityKind before anything is read; before is nil for the
// newest page.
//
// It asks for one row more than a page holds. That row only signals that
// older rows exist: it is never shown, and the next cursor is the last row
// that is.
func (s *ActivityService) Page(ctx context.Context, kind string, before *ports.DecisionCursor) (ActivityPage, error) {
	prefix := ""
	if kind != "" {
		if !slices.Contains(ActivityFamilies(s.actions), kind) {
			return ActivityPage{}, fmt.Errorf("%w: %q", ErrUnknownActivityKind, kind)
		}
		prefix = kind + "."
	}

	rows, err := s.log.Before(ctx, before, prefix, ActivityPageSize+1)
	if err != nil {
		return ActivityPage{}, fmt.Errorf("read activity: %w", err)
	}
	more := len(rows) > ActivityPageSize
	if more {
		rows = rows[:ActivityPageSize]
	}

	page := ActivityPage{Rows: make([]ActivityRow, len(rows))}
	for i, r := range rows {
		page.Rows[i] = ActivityRow{DecisionRow: r, Change: decodeChange(r.Context)}
	}
	if err := s.attachSubjects(ctx, page.Rows); err != nil {
		return ActivityPage{}, err
	}
	if more {
		last := rows[len(rows)-1]
		page.Next = &ports.DecisionCursor{OccurredAt: last.OccurredAt, Seq: last.Seq}
	}
	return page, nil
}

// attachSubjects resolves every row's subject unit with one read.
func (s *ActivityService) attachSubjects(ctx context.Context, rows []ActivityRow) error {
	if s.units == nil {
		return nil
	}
	ids := make([]string, len(rows))
	var wanted []string
	for i, r := range rows {
		ids[i] = subjectUnitID(r.Context)
		if ids[i] != "" && !slices.Contains(wanted, ids[i]) {
			wanted = append(wanted, ids[i])
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	live, err := s.units.LiveByIDs(ctx, wanted)
	if err != nil {
		return fmt.Errorf("read activity subjects: %w", err)
	}
	content := make(map[string]string, len(live))
	for _, u := range live {
		content[u.ID] = u.Content
	}
	for i := range rows {
		if c, ok := content[ids[i]]; ok {
			rows[i].Subject = &ActivitySubject{UnitID: ids[i], Content: c}
		}
	}
	return nil
}

// subjectUnitID reads the unit a row's context names: "unit_id", or for a
// relation row its "from_unit_id". Anything else names none.
func subjectUnitID(raw json.RawMessage) string {
	var ref struct {
		UnitID     string `json:"unit_id"`
		FromUnitID string `json:"from_unit_id"`
	}
	if err := json.Unmarshal(raw, &ref); err != nil {
		return ""
	}
	if ref.UnitID != "" {
		return ref.UnitID
	}
	return ref.FromUnitID
}

// ActivityFamilies returns the distinct first dot-segments of actions, sorted.
// The vocabulary is a parameter so the filter follows it: a new family needs
// no edit here.
func ActivityFamilies(actions []ports.DecisionAction) []string {
	var families []string
	for _, a := range actions {
		family, _, _ := strings.Cut(string(a), ".")
		families = append(families, family)
	}
	slices.Sort(families)
	return slices.Compact(families)
}

// decodeChange reads a row's context as a change: a JSON object carrying
// "previous" and "next" objects. It is decided by that shape, never by the
// row's action, so correction.applied, belief.edited and config.updated
// share it. Anything else, malformed JSON included, yields nil and the row
// shows its rationale only.
//
// The columns come from "fields" when that is a string array whose members
// key both objects, otherwise from the sorted union of the objects' keys.
func decodeChange(raw json.RawMessage) []ChangedField {
	var top map[string]any
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil
	}
	previous, okPrev := top["previous"].(map[string]any)
	next, okNext := top["next"].(map[string]any)
	if !okPrev || !okNext {
		return nil
	}

	names := changeColumns(top["fields"], previous, next)
	if len(names) == 0 {
		return nil
	}
	change := make([]ChangedField, len(names))
	for i, name := range names {
		change[i] = ChangedField{Name: name, Previous: renderChangeValue(previous, name), Next: renderChangeValue(next, name)}
	}
	return change
}

func changeColumns(fields any, previous, next map[string]any) []string {
	if list, ok := fields.([]any); ok && len(list) > 0 {
		names := make([]string, 0, len(list))
		for _, f := range list {
			name, isString := f.(string)
			_, inPrev := previous[name]
			_, inNext := next[name]
			if !isString || !inPrev || !inNext {
				names = nil
				break
			}
			names = append(names, name)
		}
		if names != nil {
			return names
		}
	}
	union := map[string]bool{}
	for k := range previous {
		union[k] = true
	}
	for k := range next {
		union[k] = true
	}
	return slices.Sorted(maps.Keys(union))
}

// renderChangeValue renders one column's value by its JSON kind: text as
// text, a number without a trailing ".0" (21, not 21.0), a bool as
// true/false, and an absent key or null as "(none)".
func renderChangeValue(values map[string]any, name string) string {
	switch v := values[name].(type) {
	case nil:
		return "(none)"
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		var b strings.Builder
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(v); err != nil {
			return "(unreadable)"
		}
		return strings.TrimSpace(b.String())
	}
}
