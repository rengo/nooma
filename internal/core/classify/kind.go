package classify

import "github.com/rengo/nooma/internal/core/unit"

// Kind is the classification taxonomy — doc 02 §5 step 1's own
// thirteen-member list. It is a distinct vocabulary from unit.Type's nine
// members (unit.Type's own doc comment states this explicitly): six of
// these thirteen — chitchat, out_of_scope, recall, correction, timer,
// recurring_reminder — map to no unit.Type at all (UnitType below).
type Kind string

// The thirteen members of the Kind vocabulary, doc 02 §5 step 1's order.
const (
	KindTask              Kind = "task"
	KindMentalLoad        Kind = "mental_load"
	KindEvent             Kind = "event"
	KindKnowledge         Kind = "knowledge"
	KindProcedural        Kind = "procedural"
	KindEmotional         Kind = "emotional"
	KindChitchat          Kind = "chitchat"
	KindOutOfScope        Kind = "out_of_scope"
	KindRecall            Kind = "recall"
	KindCorrection        Kind = "correction"
	KindTimer             Kind = "timer"
	KindRecurringReminder Kind = "recurring_reminder"
	KindList              Kind = "list"
)

// AllKinds returns a fresh slice holding the thirteen Kind vocabulary
// members, in the order the constants above declare them — the taxonomy
// test's own completeness check iterates this slice (design D11 point 4),
// and it is also the closed vocabulary decodeEnum matches "type" against
// (design D11 point 2).
func AllKinds() []Kind {
	return []Kind{
		KindTask, KindMentalLoad, KindEvent, KindKnowledge, KindProcedural,
		KindEmotional, KindChitchat, KindOutOfScope, KindRecall,
		KindCorrection, KindTimer, KindRecurringReminder, KindList,
	}
}

// UnitType maps k onto unit.Type where a persisted unit exists. It returns
// false for the five Kind values that never persist a unit — chitchat,
// out_of_scope, recall, correction and timer — so the caller
// (classify.ToUnit) cannot forget to check.
//
// **recurring_reminder maps to unit.TypeEvent.** It was in the false list,
// justified by §8's "a timer is NEVER a unit" — a rule about timers,
// applied to a kind that is not one. A birthday is memory; what recurs is
// the nudge, and recurrence is a property of the trigger rather than of
// what is remembered. Nothing new is added to units.type: doc 02 §5's
// "a distinct type from event, not an event with a flag" is about THIS
// vocabulary, which stays distinct because it decides what gets armed.
func (k Kind) UnitType() (unit.Type, bool) {
	switch k {
	case KindTask:
		return unit.TypeTask, true
	case KindMentalLoad:
		return unit.TypeMentalLoad, true
	case KindEvent, KindRecurringReminder:
		return unit.TypeEvent, true
	case KindKnowledge:
		return unit.TypeKnowledge, true
	case KindProcedural:
		return unit.TypeProcedural, true
	case KindEmotional:
		return unit.TypeEmotional, true
	case KindList:
		return unit.TypeList, true
	default:
		return "", false
	}
}
