package consolidation

import (
	"strings"

	"github.com/rengo/nooma/internal/core/unit"
)

// DeriveSource is one unit as the derivation judge sees it: the id (for a
// caller that needs to trace a proposed belief back to its source unit,
// design.md §10.2), its Type (doc 02 §1's nine-member vocabulary), and its
// Content — the text the judge actually reads.
type DeriveSource struct {
	UnitID  string
	Type    unit.Type
	Content string
}

// BuildDerivePrompt renders doc 02 §6.5's derivation prompt (spec R5.6,
// design.md §10.2). It is pure: same arguments, same string — no clock, no
// randomness, and slice order is preserved rather than resorted, so the
// output is deterministic across repeated calls with the same inputs.
//
// existing carries every active self-belief SelfModelRepo.ActiveBeliefs
// returns (brain's own read, this function only renders it): dedup defense
// 1, doc 02 §6.5's named gap this PR closes. Rendering every one lets the
// judge decide "this already exists" before proposing a new belief, rather
// than the second, embedding-based defense (MergeProposals) doing all the
// work alone. When existing is empty — a fresh vault, or one with no
// beliefs derived yet — the prompt still sends and states that plainly
// (spec R5.6's second MUST): the absence of beliefs is itself informative
// to the judge, not a degenerate case worth hiding by omitting the
// section.
func BuildDerivePrompt(us []DeriveSource, existing []Belief) string {
	var b strings.Builder

	b.WriteString("You derive self-beliefs about the user from a set of memory units.\n")
	b.WriteString("Answer with one JSON object and nothing else — no prose, no code fence.\n\n")

	writeExistingBeliefs(&b, existing)

	b.WriteString("Units to derive from\n")
	for _, s := range us {
		b.WriteString("  [" + string(s.Type) + "] " + s.Content + "\n")
	}
	b.WriteString("\n")

	b.WriteString("For each new belief worth proposing, decide facet, topic_key and content.\n")
	b.WriteString("Do not propose a belief that already exists above — reinforcing an existing\n")
	b.WriteString("belief happens automatically when what you derive is a near-duplicate of one\n")
	b.WriteString("already listed; only propose a genuinely new belief.\n")

	return b.String()
}

// writeExistingBeliefs renders dedup defense 1's own section: every active
// belief already known about the user, or — when existing is empty — one
// sentence stating that plainly. Unlike classify.BuildPrompt's writeBeliefs
// (which omits its section entirely when there is nothing to render, since
// a heading with no content under it misleads a classification judge into
// thinking a section was skipped by accident), the derive prompt's empty
// case is itself a fact the judge needs: "no existing beliefs" tells the
// judge every candidate it derives is necessarily new, not "the caller
// forgot this section" (spec R5.6's second MUST).
func writeExistingBeliefs(b *strings.Builder, existing []Belief) {
	b.WriteString("Existing self-beliefs\n")
	if len(existing) == 0 {
		b.WriteString("  There are no existing self-beliefs for this user yet.\n\n")
		return
	}
	for _, belief := range existing {
		b.WriteString("  [" + string(belief.Facet) + "] " + belief.TopicKey + ": " + belief.Content + "\n")
	}
	b.WriteString("\n")
}
