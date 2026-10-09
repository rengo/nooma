package ui

import (
	"testing"

	"github.com/rengo/nooma/internal/ports"
)

// Every action the vocabulary names has a plain title, so no row on the
// activity page leads with a code. The map also titles the two
// target_unknown actions, which are recorded but not listed by
// ports.AllDecisionActions().
func TestActivityTitle_EveryActionHasOne(t *testing.T) {
	all := ports.AllDecisionActions()
	for _, a := range all {
		if _, ok := activityTitles[a]; !ok {
			t.Errorf("action %q has no title in activityTitles", a)
		}
	}
	if got := activityTitle("capture.discarded"); got != "capture.discarded" {
		t.Errorf("a retired action titled %q, want its code", got)
	}
}
