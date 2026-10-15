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
//   - A plan that rewrites the body and brings no such title clears the unit's
//     title: it described a body that is gone. A date-only plan leaves it alone.
//   - A title the model wrote without being shown the unit (the chat path, where
//     the referent is found after the answer) names the utterance, not the
//     unit, and is never taken.
//
// A title that equals the unit's is no edit, and a unit with no title is not
// cleared.
func WithTitle(plan []Edit, c classify.Classification, target unit.Unit, sawUnit bool) []Edit {
	if sawUnit && c.Title != nil {
		if *c.Title == target.Title {
			return plan
		}
		return append(append([]Edit(nil), plan...), NewTitleEdit(*c.Title))
	}
	for _, e := range plan {
		if _, ok := e.Content(); ok && target.Title != "" {
			return append(append([]Edit(nil), plan...), NewTitleEdit(""))
		}
	}
	return plan
}
