package classify

// The six orthogonal resolution fields — docs/02-cognitive-core.md §5 step 1
// at 02:120-123, which calls them out explicitly: "These are orthogonal
// fields, not types". A capture can carry one alongside any Kind ("I practiced
// yesterday" is nudge_outcome: engaged *and* a knowledge unit), and each
// degrades independently of the others and of Kind (spec R1.2, I14).
//
// Each vocabulary is closed and exposes only AllX(). There is deliberately no
// ParseX: decodeEnum[T ~string](raw, all) serves Kind and all six of these
// (design D11 point 2), so seven parse functions would be seven sets of arms
// where one loop already suffices.

// NudgeOutcome records how a nudge landed — doc 02 §5: engaged | declined.
type NudgeOutcome string

const (
	NudgeOutcomeEngaged  NudgeOutcome = "engaged"
	NudgeOutcomeDeclined NudgeOutcome = "declined"
)

// AllNudgeOutcomes returns a fresh slice of the NudgeOutcome vocabulary, in
// doc 02's declared order — the closed set decodeEnum matches against.
func AllNudgeOutcomes() []NudgeOutcome {
	return []NudgeOutcome{NudgeOutcomeEngaged, NudgeOutcomeDeclined}
}

// RelationOutcome records a user's verdict on a proposed relation — doc 02 §5:
// confirmed | rejected. Distinct from StateOutcome, which shares "confirmed"
// but denies rather than rejects.
type RelationOutcome string

const (
	RelationOutcomeConfirmed RelationOutcome = "confirmed"
	RelationOutcomeRejected  RelationOutcome = "rejected"
)

// AllRelationOutcomes returns a fresh slice of the RelationOutcome vocabulary,
// in doc 02's declared order.
func AllRelationOutcomes() []RelationOutcome {
	return []RelationOutcome{RelationOutcomeConfirmed, RelationOutcomeRejected}
}

// StateOutcome records a user's verdict on an inferred state — doc 02 §5:
// confirmed | denied.
type StateOutcome string

const (
	StateOutcomeConfirmed StateOutcome = "confirmed"
	StateOutcomeDenied    StateOutcome = "denied"
)

// AllStateOutcomes returns a fresh slice of the StateOutcome vocabulary, in
// doc 02's declared order.
func AllStateOutcomes() []StateOutcome {
	return []StateOutcome{StateOutcomeConfirmed, StateOutcomeDenied}
}

// TaskCheckinOutcome records how a task check-in resolved — doc 02 §5:
// done | snooze | drop.
type TaskCheckinOutcome string

const (
	TaskCheckinOutcomeDone   TaskCheckinOutcome = "done"
	TaskCheckinOutcomeSnooze TaskCheckinOutcome = "snooze"
	TaskCheckinOutcomeDrop   TaskCheckinOutcome = "drop"
)

// AllTaskCheckinOutcomes returns a fresh slice of the TaskCheckinOutcome
// vocabulary, in doc 02's declared order.
func AllTaskCheckinOutcomes() []TaskCheckinOutcome {
	return []TaskCheckinOutcome{
		TaskCheckinOutcomeDone, TaskCheckinOutcomeSnooze, TaskCheckinOutcomeDrop,
	}
}

// ListOp records an operation on a list unit — doc 02 §5:
// append | delete | mark_done | remove.
type ListOp string

const (
	ListOpAppend   ListOp = "append"
	ListOpDelete   ListOp = "delete"
	ListOpMarkDone ListOp = "mark_done"
	ListOpRemove   ListOp = "remove"
)

// AllListOps returns a fresh slice of the ListOp vocabulary, in doc 02's
// declared order.
func AllListOps() []ListOp {
	return []ListOp{ListOpAppend, ListOpDelete, ListOpMarkDone, ListOpRemove}
}

// PersonRefStatus records how a person reference resolved — doc 02 §5:
// resolved | new | ambiguous. classify returns "ambiguous" exactly as it
// returns any other value; deciding what to do about it belongs to
// internal/brain (spec R1.4, Q3a).
type PersonRefStatus string

const (
	PersonRefStatusResolved  PersonRefStatus = "resolved"
	PersonRefStatusNew       PersonRefStatus = "new"
	PersonRefStatusAmbiguous PersonRefStatus = "ambiguous"
)

// AllPersonRefStatuses returns a fresh slice of the PersonRefStatus
// vocabulary, in doc 02's declared order.
func AllPersonRefStatuses() []PersonRefStatus {
	return []PersonRefStatus{
		PersonRefStatusResolved, PersonRefStatusNew, PersonRefStatusAmbiguous,
	}
}
