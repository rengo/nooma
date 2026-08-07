package consolidation

import (
	"time"

	"github.com/rengo/nooma/internal/core/weight"
)

// Reweight applies doc 02 §6.6's post-connection adjustment. See
// design.md §4.5/§6.6 for the full contract; implemented in the next
// commit.
func Reweight(states map[string]weight.Current, newEdges []weight.Edge, now time.Time) (boosts []weight.Boost, corrupted []string) {
	return nil, nil
}
