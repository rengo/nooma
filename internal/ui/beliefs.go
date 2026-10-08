package ui

import (
	"context"

	"github.com/rengo/nooma/internal/brain"
)

// Beliefs is /ui/beliefs' own entry point into the belief read model and its
// two user writes (m4e design §3.9): ByFacet for the page, Edit and Retire
// for the two POSTs. Edit takes the content and nothing else, so a posted
// facet or confidence has nowhere to go. *brain.BeliefsService satisfies it.
type Beliefs interface {
	ByFacet(ctx context.Context) ([]brain.FacetBeliefs, error)
	Edit(ctx context.Context, id, content string) error
	Retire(ctx context.Context, id string) error
}

// beliefResult is what a belief POST reports back: the outcome marker, the
// message shown to the user, and, for a rejected edit, the belief it was for
// and the text that was submitted, so the form can be shown again with it.
type beliefResult struct {
	Outcome string
	Message string
	ID      string
	Content string
}
