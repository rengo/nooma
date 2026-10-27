package telegram

import (
	"strings"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/phrase"
)

// entity is one of a Telegram message's entities, narrowed to what tells a
// bot command apart from text that only starts with a slash.
type entity struct {
	Type   string `json:"type"`
	Offset int    `json:"offset"`
	Length int    `json:"length"`
}

// commandReply answers a message that opens with a bot command, without the
// model: /start with the greeting, any other command by saying it is not
// one Nooma knows. ok is false for every other message, which classify
// reads. The command is Telegram's own entity at offset 0, so a slash in
// text is never mistaken for one; "/start@bot" is /start addressed by name.
func commandReply(text string, entities []entity, languageCode string) (string, bool) {
	if len(entities) == 0 || entities[0].Type != "bot_command" || entities[0].Offset != 0 {
		return "", false
	}
	runes := []rune(text)
	name := string(runes[:min(entities[0].Length, len(runes))])
	name, _, _ = strings.Cut(name, "@")
	say := phrase.For(languageOf(languageCode))
	if name == "/start" {
		return say.Greeting, true
	}
	return say.UnknownCommand, true
}

// languageOf is the language Nooma speaks to a sender whose Telegram client
// reports code, an IETF tag such as "es-AR": English for an English one,
// and Spanish, the vault owner's, for every other or for none.
func languageOf(code string) classify.Language {
	primary, _, _ := strings.Cut(strings.ToLower(code), "-")
	if primary == string(classify.LanguageEN) {
		return classify.LanguageEN
	}
	return classify.LanguageES
}
