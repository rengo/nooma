package voice

import (
	"fmt"
	"strings"

	"github.com/rengo/nooma/internal/core/phrase"
)

// instructions frame each intent for the model.
var instructions = map[Intent]string{
	RecallFound: "The user asked something and the code found the items below. Answer their message with them.",
	RecallNone:  "The user asked something and the code found nothing. Say so honestly and briefly, and suggest nothing.",
}

// BuildPrompt asks the model to word draft from f. It is pure. The rules come
// first and the facts last, so a provider's prefix cache can reuse the rules.
func BuildPrompt(f Facts, draft string) string {
	var b strings.Builder
	b.WriteString("You are Nooma, a person's own memory, writing one chat message to its owner.\n")
	b.WriteString("The code already decided what to say. You only word it, warmly and naturally.\n\n")
	b.WriteString("Rules\n")
	b.WriteString("  Write in " + phrase.For(f.Language).LanguageName + ", in the user's own variety and register: voseo when they use voseo (\"Tenés\"), tú when they use tú.\n")
	b.WriteString("  Never write a date, a day, a time, a number or an item yourself. Write the slots below and the code fills them in:\n")
	b.WriteString("    {1} is item 1's own words, {1.when} its day and time with their own preposition, {count} how many items there are.\n")
	b.WriteString("  Write each item's slots once each, {n} and {n.when} side by side in either order, with nothing of another item between them,\n")
	b.WriteString("    and part two items with a comma, \"y\"/\"and\" or a new sentence. Put no article or preposition right before a {n.when}.\n")
	b.WriteString("  Add no item, place, person or detail of your own: use plain words, and the words of the draft and the message.\n")
	b.WriteString("  One or two short sentences. With three or more items, one short line per item.\n")
	b.WriteString("  No emojis, no markdown, no greeting, no sign-off, no remarks or offers of your own.\n")
	b.WriteString("  Do not ask a question unless the draft asks it.\n")
	b.WriteString("  Keep every text listed under Keep exactly as written.\n")
	b.WriteString("  Reply with the message itself and nothing else.\n\n")
	b.WriteString("Intent\n  " + instructions[f.Intent] + "\n\n")
	if f.Message != "" {
		b.WriteString("The user's message (for its tone; it is not a fact)\n  " + fmt.Sprintf("%q", f.Message) + "\n\n")
	}
	b.WriteString("Facts\n")
	if len(f.Items) == 0 {
		b.WriteString("  (none)\n")
	}
	for i, it := range f.Items {
		fmt.Fprintf(&b, "  {%d} = %q", i+1, it.Text)
		if it.Type != "" {
			b.WriteString(" (" + it.Type + ")")
		}
		if it.At != nil {
			fmt.Fprintf(&b, "; {%d.when} = %q", i+1, when(f, it))
		}
		b.WriteString("\n")
	}
	if len(f.Items) > 0 {
		fmt.Fprintf(&b, "  {count} = %d\n", len(f.Items))
	}
	if len(f.Keep) > 0 {
		b.WriteString("\nKeep\n")
		for _, k := range f.Keep {
			b.WriteString("  " + k + "\n")
		}
	}
	b.WriteString("\nDraft (what the code would send)\n" + draft + "\n")
	return b.String()
}
