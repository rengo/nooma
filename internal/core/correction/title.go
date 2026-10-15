package correction

import (
	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/unit"
)

// WithTitle is plan and, when the correction changes what the unit's title says,
// the edit that keeps the two in step (spec R12, doc 02 §5 step 4).
//
//   - A valid title the model answered, having been shown this unit (sawUnit), is
//     written when it differs from the unit's. A correction prompt asks for a
//     fresh one whenever the day or time moves, so "Reunión del jueves" can
//     become "Reunión del viernes".
//   - Otherwise a plan that really changes the unit (a moved date, or a rewritten
//     body) clears its title: it names a day or a body that is gone, and the unit
//     then reads its content, which follows the new date. This covers a date-only
//     edit with no fresh title and the chat path, where the referent is found
//     after the model answered, so a title it wrote names the utterance and is
//     never taken.
//
// A title that equals the unit's is no edit, a unit with no title is not cleared,
// and a correction that changes nothing (a date the unit already holds) clears
// nothing.
func WithTitle(plan []Edit, c classify.Classification, target unit.Unit, sawUnit bool) []Edit {
	if sawUnit && c.Title != nil {
		if *c.Title == target.Title {
			return plan
		}
		return append(append([]Edit(nil), plan...), NewTitleEdit(*c.Title))
	}
	if target.Title != "" && len(Changes(plan, target)) > 0 {
		return append(append([]Edit(nil), plan...), NewTitleEdit(""))
	}
	return plan
}
