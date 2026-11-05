package channels

import (
	"fmt"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/phrase"
)

// EchoesTranscript reports whether the reply to a voice note leads with
// what was heard (ADR-0062 point 4, narrowed by ADR-0066): exactly when the
// reading did nothing the user can see acted on and the transcript explains
// an odd answer — an out-of-scope request, an ask, a which-timer question —
// so a mishearing shows. A chitchat is answered as typed text is, with no
// echo, and so is everything else: the reply already names what was
// understood.
//
// Decided over the typed outcome, never over the reply's words.
func EchoesTranscript(result brain.CaptureResult) bool {
	switch result.Outcome {
	case brain.OutcomeOutOfScope, brain.OutcomeAsked:
		return true
	case brain.OutcomeTimers:
		return result.Timers != nil && result.Timers.Which
	}
	return false
}

// voiceReply is reply to a voice note heard as transcript.
func voiceReply(result brain.CaptureResult, transcript, reply string) string {
	if !EchoesTranscript(result) {
		return reply
	}
	return fmt.Sprintf(phrase.For(result.Language).VoiceHeard, transcript) + "\n" + reply
}
