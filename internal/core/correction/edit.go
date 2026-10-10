package correction

import (
	"time"

	"github.com/rengo/nooma/internal/core/unit"
)

// Field names the one column a correction can write — doc 02 §5 step 4,
// design D3.
type Field string

const (
	FieldContent Field = "content"
	FieldEventAt Field = "event_at"
	FieldDueAt   Field = "due_at"
)

// AllFields returns every Field the plan can produce. Its own completeness
// table lives in edit_test.go: every member here must have exactly one
// Edit constructor and accessor pair covering it.
func AllFields() []Field {
	return []Field{FieldContent, FieldEventAt, FieldDueAt}
}

// Edit is one field and its new value. Its state is unexported, and it
// exposes three accessors named after the three ports.UnitRepo methods
// PlanEdit's caller dispatches to (design D5's dispatchEdits) — a struct
// with Content string beside At time.Time and a Field tag is the shape
// that writes the wrong column; here, a crossed wiring is a name mismatch
// a reader sees, and a false accessor return is a bug an L1 test catches
// (design D3).
type Edit struct {
	field   Field
	content string
	eventAt time.Time
	dueAt   time.Time
}

// NewContentEdit builds an Edit that writes content.
func NewContentEdit(s string) Edit {
	return Edit{field: FieldContent, content: s}
}

// NewEventAtEdit builds an Edit that writes event_at.
func NewEventAtEdit(t time.Time) Edit {
	return Edit{field: FieldEventAt, eventAt: t}
}

// NewDueAtEdit builds an Edit that writes due_at.
func NewDueAtEdit(t time.Time) Edit {
	return Edit{field: FieldDueAt, dueAt: t}
}

// Field reports which column e writes.
func (e Edit) Field() Field { return e.field }

// Content reports e's new content and whether e writes content at all —
// true only when e.Field() == FieldContent.
func (e Edit) Content() (string, bool) {
	return e.content, e.field == FieldContent
}

// EventAt reports e's new event_at and whether e writes event_at at all —
// true only when e.Field() == FieldEventAt.
func (e Edit) EventAt() (time.Time, bool) {
	return e.eventAt, e.field == FieldEventAt
}

// DueAt reports e's new due_at and whether e writes due_at at all — true
// only when e.Field() == FieldDueAt.
func (e Edit) DueAt() (time.Time, bool) {
	return e.dueAt, e.field == FieldDueAt
}

// Changes returns the edits of plan that would alter u: a content edit
// whose text is u's own, or a date edit whose instant is u's own, writes
// nothing and is not a change. A repeated correction finds its work done;
// it must not be recorded, signalled or reported as one.
func Changes(plan []Edit, u unit.Unit) []Edit {
	var out []Edit
	for _, e := range plan {
		same := false
		if v, ok := e.Content(); ok {
			same = v == u.Content
		} else if v, ok := e.EventAt(); ok {
			same = u.EventAt != nil && u.EventAt.Equal(v)
		} else if v, ok := e.DueAt(); ok {
			same = u.DueAt != nil && u.DueAt.Equal(v)
		}
		if !same {
			out = append(out, e)
		}
	}
	return out
}
