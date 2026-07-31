package unit

import "fmt"

// Type is the unit's persisted kind — doc 02 §1 names exactly nine values.
// Like Status, it is a defined string type (design D1's pattern, repeated
// per D4): it prints as itself in an error, a log line and a decision_log
// row, and binds to the units.type TEXT column with no conversion table.
//
// Type is not doc 02 §5.1's classification taxonomy: that is a different,
// thirteen-member vocabulary living in core/classify (Phase B), with a
// partial mapping onto this one (design D4). "chitchat" and the other
// classify-only outcomes are not — and must never become — Type values.
type Type string

// The nine members of the Type vocabulary, doc 02 §1.
const (
	TypeTask          Type = "task"
	TypeMentalLoad    Type = "mental_load"
	TypeEvent         Type = "event"
	TypeKnowledge     Type = "knowledge"
	TypeProcedural    Type = "procedural"
	TypeEmotional     Type = "emotional"
	TypeList          Type = "list"
	TypeStructuredRef Type = "structured_ref"
	TypeInsight       Type = "insight"
)

// AllTypes returns a fresh slice holding the nine Type vocabulary members,
// in the order the constants above declare them. A function, not an
// exported var, for the same reason AllStatuses is (design D1).
func AllTypes() []Type {
	return []Type{
		TypeTask, TypeMentalLoad, TypeEvent, TypeKnowledge, TypeProcedural,
		TypeEmotional, TypeList, TypeStructuredRef, TypeInsight,
	}
}

// ErrUnknownType is returned by ParseType when s does not name a member of
// AllTypes().
var ErrUnknownType = fmt.Errorf("unknown unit type")

// ParseType is the sole entry point from untrusted text into the Type
// vocabulary (design D1's pattern, repeated per D4). It returns
// ErrUnknownType, naming the rejected value, for anything that is not one
// of AllTypes()'s members.
func ParseType(s string) (Type, error) {
	for _, want := range AllTypes() {
		if s == string(want) {
			return want, nil
		}
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownType, s)
}
