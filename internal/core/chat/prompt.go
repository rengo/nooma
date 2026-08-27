// Package chat builds the one prompt the `chat` task sends — doc 02 §5
// step 1's "two of the thirteen are not memory" bullet, decided in
// docs/adr/0021-conversation-boundary.md.
//
// It is the conversational half of capture and nothing else. A message
// classified `chitchat` had nothing in it worth keeping; what it still had
// was a person waiting for an answer, and this is what that answer is built
// from. Nothing here persists, embeds, recalls or judges.
package chat

import "strings"

// BuildPrompt renders the prompt for one chitchat reply. It is pure: same
// argument, same string.
//
// **It takes the raw message, never normalized_content.** The classifier's
// normalization is a paraphrase written for storage, and answering a
// paraphrase costs the one property this whole path exists to buy: a reply
// in the sender's own language. The normalization may already be in
// another language than the message; the message never is.
//
// **It carries no vault content — no beliefs, no recall, no candidates.**
// That is a decision, not an omission (ADR-0021). A chitchat is by
// classification the message that had nothing to keep in it; running the
// expensive half of the pipeline for the kind that was routed out of the
// pipeline would pay recall's cost to decorate a greeting. When M4 or M5
// wants a conversation that remembers, it adds a parameter here and the
// caller fills it — the same shape classify.BuildPrompt's beliefs
// parameter already holds open.
//
// There is no now parameter for the same reason there is no vault content:
// nothing in a greeting resolves against a date. A conversation that needs
// to say "good morning" correctly is a different prompt than this one, and
// it can take the instant then, from its caller, the way every other
// instant in internal/core arrives.
func BuildPrompt(message string) string {
	var b strings.Builder

	b.WriteString("You are Nooma, a person's own memory. You are talking to its owner.\n")
	b.WriteString("This message had nothing in it to remember, so there is nothing to look up:\n")
	b.WriteString("just answer it.\n\n")

	b.WriteString("Rules\n")
	// **The language rule is the load-bearing line.** Nooma has no locale
	// setting anywhere — not in nooma.yml, not on a unit, not per user —
	// so every fixed sentence the binary can say is English by the
	// accident of who typed it. This path is the one place that does not
	// have to be: the model is holding a message written in some language
	// and can simply answer in it.
	b.WriteString("  Answer in the same language the message is written in.\n")
	// **Do not let it invent a capability.** The classifier decides which
	// messages reach this prompt, and it will be wrong sometimes — an
	// out_of_scope request read as small talk arrives here, where a model
	// with no tools will happily promise to check the weather. Nooma
	// captures, recalls, corrects and reminds; that list is short enough
	// to state, and stating it is cheaper than an apology later.
	b.WriteString("  You can capture things, recall them, correct them and set reminders.\n")
	b.WriteString("  You cannot browse, search the web, run code, or reach any other service.\n")
	b.WriteString("  If the message asks for something you cannot do, say so plainly.\n")
	b.WriteString("  Answer in one or two sentences. This is read on a phone, in a chat.\n")
	b.WriteString("  Reply with the answer itself — no preamble, no quoting the message back.\n\n")

	b.WriteString("Message\n")
	b.WriteString("  " + message + "\n")

	return b.String()
}
