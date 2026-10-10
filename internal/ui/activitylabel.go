package ui

import "github.com/rengo/nooma/internal/ports"

// activityTitles is each decision_log action in plain words: what the
// activity page leads a row with. The action code stays on the row, secondary,
// for whoever needs the exact bucket. TestActivityTitle_EveryActionHasOne keeps
// this total over ports.AllDecisionActions(): a new action without a title
// fails there rather than surfacing its code as the headline.
var activityTitles = map[ports.DecisionAction]string{
	ports.ActionCaptureClassify:           "Saved",
	ports.ActionCaptureUnparseable:        "Could not read a message",
	ports.ActionCaptureUnclassifiable:     "Could not tell what a message was",
	ports.ActionCaptureConversed:          "Replied in conversation",
	ports.ActionCaptureOutOfScope:         "Set aside an out-of-scope message",
	ports.ActionCaptureUnitCreated:        "Entry created",
	ports.ActionCaptureEmbeddingFailed:    "Saved, but not indexed for search",
	ports.ActionCaptureDedupFailed:        "Could not check for a duplicate",
	ports.ActionCaptureChatFailed:         "Could not reply",
	ports.ActionCapturePersonRefAmbiguous: "Unsure which person was meant",
	ports.ActionCaptureArmedTimer:         "Timer set",
	ports.ActionCaptureArmedTrigger:       "Reminder set",
	ports.ActionCaptureArmedRecurring:     "Recurring reminder set",
	ports.ActionCaptureArmRefused:         "Reminder not set",
	ports.ActionCheckTriggerExpired:       "Reminder expired unsent",
	ports.ActionCheckTimerFired:           "Timer went off",
	ports.ActionCheckTimerCancelled:       "Timer cancelled",
	ports.ActionCheckConflictSkipped:      "Skipped a conflicting update",
	ports.ActionCheckTriggerFired:         "Reminder due",
	ports.ActionCheckTriggerDelivered:     "Reminder sent",
	ports.ActionCheckDeliveryFailed:       "Message not delivered",
	ports.ActionCheckDigestSent:           "Morning digest sent",
	ports.ActionCheckDigestHeld:           "Morning digest held back",
	ports.ActionCheckFocusUnavailable:     "Focus unavailable",
	ports.ActionCheckTimerRephraseFailed:  "Could not reword a timer",
	ports.ActionCaptureCheckInResolved:    "Check-in answered",
	ports.ActionCaptureCheckInUnmatched:   "Reply matched no check-in",
	ports.ActionCaptureDedupJudged:        "Checked for a duplicate",
	ports.ActionRelationPersisted:         "Linked to a related entry",
	ports.ActionRelationDiscarded:         "Possible link dropped",
	ports.ActionRelationDuplicateRecorded: "Marked as a duplicate",
	ports.ActionRelationTargetUnknown:     "Link to an unknown entry dropped",

	ports.ActionCorrectionApplied:           "Corrected",
	ports.ActionCorrectionAmbiguous:         "Correction not applied",
	ports.ActionCorrectionReminderMoved:     "Reminder moved",
	ports.ActionCorrectionReminderArmed:     "Reminder set by a correction",
	ports.ActionCorrectionReminderCancelled: "Reminder cancelled by a correction",

	ports.ActionExpireIncompleteTransitioned:    "Unfinished entry set aside",
	ports.ActionArchiveArchived:                 "Archived",
	ports.ActionArchiveConflictSkipped:          "Archiving skipped",
	ports.ActionStrengthenApplied:               "Strengthened",
	ports.ActionConnectRelationPersisted:        "Connected overnight",
	ports.ActionConnectTargetUnknown:            "Overnight link dropped",
	ports.ActionDeriveBeliefCreated:             "New belief",
	ports.ActionDeriveBeliefReinforced:          "Belief reinforced",
	ports.ActionDeriveBeliefSkipped:             "Belief not derived",
	ports.ActionDeriveRetiredEmbedFailed:        "Could not compare with retired beliefs",
	ports.ActionReweightBoostApplied:            "Weight raised",
	ports.ActionPatternEvalStagnationFound:      "Stalled goal noticed",
	ports.ActionPatternEvalLoadHypothesisOpened: "Recurring load noticed",
	ports.ActionConnectQuestionCreated:          "Question queued",
	ports.ActionCheckDigestQuestionAsked:        "Question asked",
	ports.ActionCheckQuestionExpired:            "Question expired",

	ports.ActionCaptureRelationCheckInResolved:  "Link question answered",
	ports.ActionCaptureRelationCheckInUnmatched: "Reply matched no link question",
	ports.ActionBeliefEdited:                    "Belief edited",
	ports.ActionBeliefRetired:                   "Belief retired",
}

// activityTitle is a row's headline. A vault can hold actions the vocabulary
// no longer names (capture.discarded, before ADR-0021); those show their code.
func activityTitle(a ports.DecisionAction) string {
	if t, ok := activityTitles[a]; ok {
		return t
	}
	return string(a)
}

// fieldLabel names a changed column the way the pages label it. A column
// with no plain name shows as stored.
func fieldLabel(name string) string {
	switch name {
	case "content":
		return "Text"
	case "event_at":
		return "Event"
	case "due_at":
		return "Due"
	case "fire_at":
		return "Fires at"
	}
	return name
}
