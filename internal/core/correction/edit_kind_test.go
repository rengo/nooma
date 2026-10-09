package correction

import (
	"testing"

	"github.com/rengo/nooma/internal/core/classify"
)

// TestIsEdit sweeps the whole Kind vocabulary (doc 02 §5 step 4, I28): with
// an explicit referent, only a text the model read as a question, a remark,
// a refusal or a timer is not an edit. Every kind that is memory, and
// correction itself, still edits. A kind added to classify.AllKinds lands
// in one of the two sets without anyone remembering this table, so the
// sweep fails until it is listed here too.
func TestIsEdit(t *testing.T) {
	notEdits := map[classify.Kind]bool{
		classify.KindRecall:     true,
		classify.KindChitchat:   true,
		classify.KindOutOfScope: true,
		classify.KindTimer:      true,
	}
	edits := map[classify.Kind]bool{
		classify.KindTask: true, classify.KindMentalLoad: true, classify.KindEvent: true,
		classify.KindKnowledge: true, classify.KindProcedural: true, classify.KindEmotional: true,
		classify.KindCorrection: true, classify.KindRecurringReminder: true, classify.KindList: true,
	}
	for _, k := range classify.AllKinds() {
		if notEdits[k] == edits[k] {
			t.Fatalf("kind %q is in neither or both expected sets — list it in this table", k)
		}
		if got := IsEdit(k); got != edits[k] {
			t.Errorf("IsEdit(%q) = %v, want %v", k, got, edits[k])
		}
	}
}
