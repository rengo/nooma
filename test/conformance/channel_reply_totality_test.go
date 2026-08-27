// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/channels"
	"github.com/rengo/nooma/internal/core/prospection"
)

// TestChannelReplyIsTotalOverEveryCaptureOutcome is R5.1.
//
// A channel answers a person. An outcome with no rendering would answer
// them with an empty message — the one failure mode where the user cannot
// tell a bug from being ignored.
//
// It iterates brain.AllCaptureOutcomes() rather than listing today's
// seven, which is the difference between a test that catches an eighth
// outcome and one that silently stops covering the vocabulary. The length
// check catches the other direction: a member removed without this test
// noticing.
//
// internal/httpapi's TestAllCaptureOutcomesHaveAStatusMapping is the shape
// this follows; this is the third surface to carry it, after HTTP and the
// CLI.
func TestChannelReplyIsTotalOverEveryCaptureOutcome(t *testing.T) {
	outcomes := brain.AllCaptureOutcomes()
	if len(outcomes) != 8 {
		t.Fatalf("brain.AllCaptureOutcomes() has %d members, want 8 — this test's own claim to be exhaustive is what changed", len(outcomes))
	}

	seen := map[string]brain.CaptureOutcome{}

	for _, outcome := range outcomes {
		result := brain.CaptureResult{Outcome: outcome}

		// Outcomes carrying a required pointer need it non-nil, the same
		// way captureRunner always sets it for that outcome.
		switch outcome {
		case brain.OutcomeArmed:
			result.Armed = &brain.Armed{What: prospection.ArmTimer, ID: "t-1"}
		case brain.OutcomeArmRefused:
			result.ArmRefused = &brain.ArmRefused{Why: prospection.RefusalNoDate, Message: "no time was given"}
		case brain.OutcomeCorrected, brain.OutcomeAsked:
			result.Correction = &brain.Correction{UnitID: "u-1"}
		case brain.OutcomeStored:
			result.UnitID = "u-1"
		case brain.OutcomeConversed:
			// The model's sentence. Its empty case is a real state of
			// this outcome, and TestChannelReply_ConversedWithNoAnswer
			// below covers it — here it would only collide with nothing.
			result.Reply = "All good, I'm here."
		}

		reply := channels.RenderReply(result)

		if strings.TrimSpace(reply) == "" {
			t.Errorf("outcome %q renders an empty reply — a person cannot tell that from being ignored", outcome)
			continue
		}
		if other, taken := seen[reply]; taken {
			t.Errorf("outcomes %q and %q render the identical reply %q — two different things happened and the person is told the same sentence", outcome, other, reply)
		}
		seen[reply] = outcome
	}
}

// TestChannelReplyCarriesWhatTheOutcomeNames: a total mapping that said
// "ok" seven different ways would pass the test above. These are the facts
// a person actually needs back.
func TestChannelReplyCarriesWhatTheOutcomeNames(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result brain.CaptureResult
		wants  []string
	}{
		{
			name:   "an armed timer names when it will fire",
			result: brain.CaptureResult{Outcome: brain.OutcomeArmed, Armed: &brain.Armed{What: prospection.ArmTimer, ID: "t-1"}},
			wants:  []string{"timer"},
		},
		{
			name: "a refused arming says which thing was missing",
			result: brain.CaptureResult{
				Outcome:    brain.OutcomeArmRefused,
				ArmRefused: &brain.ArmRefused{Why: prospection.RefusalNoDate, Message: "no time was given, and guessing one is worse"},
			},
			wants: []string{"no time was given"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Case-insensitive: what is asserted is that the fact
			// reaches the reader, not how the sentence capitalises it.
			reply := strings.ToLower(channels.RenderReply(tc.result))
			for _, want := range tc.wants {
				if !strings.Contains(reply, strings.ToLower(want)) {
					t.Errorf("reply %q does not carry %q", reply, want)
				}
			}
		})
	}
}

// TestChannelReply_ConversedWithNoAnswer covers the one state the totality
// test above cannot: OutcomeConversed with an empty Reply, which is what a
// chat-task outage produces (brain.CaptureResult.Reply's own contract,
// ADR-0021).
//
// The totality test hands every outcome a populated result, because its
// question is whether the switch is exhaustive. This one's question is
// different: an outcome with a documented empty-field state has two
// renderings, and the empty one is the one that would silently become the
// empty string if the branch were ever dropped — the exact failure the
// switch's missing default clause exists to make visible for unknown
// outcomes, and cannot make visible for a known one.
func TestChannelReply_ConversedWithNoAnswer(t *testing.T) {
	degraded := channels.RenderReply(brain.CaptureResult{Outcome: brain.OutcomeConversed})
	if strings.TrimSpace(degraded) == "" {
		t.Fatal("a conversed capture whose chat task did not answer renders an empty reply — the person cannot tell that from being ignored")
	}

	refusal := channels.RenderReply(brain.CaptureResult{Outcome: brain.OutcomeOutOfScope})
	if degraded == refusal {
		t.Errorf("a chat outage and an out-of-scope request render the identical reply %q — one is temporary and one is permanent, and the person is told the same thing", degraded)
	}
}
