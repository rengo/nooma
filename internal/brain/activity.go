package brain

import (
	"context"
	"errors"

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
}

// NewActivityService wires an ActivityService over log, filtering by the
// families of the full action vocabulary.
func NewActivityService(log ports.DecisionLog) *ActivityService {
	return &ActivityService{log: log, actions: ports.AllDecisionActions()}
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
}

// ActivityPage is one page of rows and the cursor of the page after it, nil
// when this is the last page.
type ActivityPage struct {
	Rows []ActivityRow
	Next *ports.DecisionCursor
}

// Page returns one page of activity, newest first. kind is "" for every row
// or one family from ActivityFamilies; before is nil for the newest page.
func (s *ActivityService) Page(ctx context.Context, kind string, before *ports.DecisionCursor) (ActivityPage, error) {
	return ActivityPage{}, nil
}

// ActivityFamilies returns the distinct first dot-segments of actions, in
// order of first appearance.
func ActivityFamilies(actions []ports.DecisionAction) []string {
	return nil
}
