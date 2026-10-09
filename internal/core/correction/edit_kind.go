package correction

import "github.com/rengo/nooma/internal/core/classify"

// IsEdit reports whether a text the model classified as k can carry an
// edit to a unit named explicitly as its referent — doc 02 §5 step 4, I28.
//
// A capture naming its referent is a correction whatever its type, but not
// every type carries a change: a question (recall), a remark (chitchat), a
// refusal (out_of_scope) or a timer request says nothing about the unit's
// value, and PlanEdit's content fallback would otherwise overwrite the
// unit's body with the question itself. Those four are exactly the kinds
// that are neither memory nor a correction, so the rule is derived from
// classify.Kind.UnitType rather than listed: a kind added later lands on
// the side its own memory mapping puts it, and TestIsEdit's sweep fails
// until its table says so too.
func IsEdit(k classify.Kind) bool {
	_, memory := k.UnitType()
	return memory || k == classify.KindCorrection
}
