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
	b.WriteString("  Say only the facts below. Do not add, guess or change any item, date, time or number, and drop none.\n")
	b.WriteString("  Write every time as given (08:30 or 8:30, 24-hour) and every number in digits. Name a day with the words given for it.\n")
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
		fmt.Fprintf(&b, "  %d. %s\n", i+1, itemLine(f, it))
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

// itemLine names an item for the prompt: its words, its type, and its day as
// the code counted it, so the model copies a weekday rather than computing one.
func itemLine(f Facts, it Item) string {
	line := it.Text
	if it.Type != "" {
		line += " (" + it.Type + ")"
	}
	if it.At == nil {
		return line
	}
	v, set := vocabularyFor(f.Language), phrase.For(f.Language)
	t := it.At.In(f.Now.Location())
	line += " — " + fmt.Sprintf(v.longDate, v.weekdays[t.Weekday()], t.Day(), set.MonthNames[t.Month()-1])
	if t.Year() != f.Now.Year() {
		line += fmt.Sprintf(v.yearSuffix, t.Year())
	}
	if !it.DayOnly {
		line += fmt.Sprintf(", %02d:%02d", t.Hour(), t.Minute())
	}
	if label, ok := v.labels[dayOffset(f.Now, t)]; ok {
		line += " — " + label
	}
	return line
}
