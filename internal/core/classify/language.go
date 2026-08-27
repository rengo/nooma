package classify

// Language is the language a captured message was written in, as the
// classifier reads it — doc 02 §5 step 1, decided in
// docs/adr/0022-reply-language.md.
//
// **It is the languages Nooma can speak, not the languages a person can
// write.** That asymmetry is the whole reason this is a closed vocabulary
// rather than a BCP-47 string. A fixed sentence exists in this repository
// or it does not; a classification naming a language Nooma holds no
// sentences in is a value nothing downstream can act on, and an open
// vocabulary would let one arrive looking actionable. Widening this list
// means writing the sentences first — which is exactly the order that
// keeps the vocabulary honest.
//
// A message in a language not on this list still captures, still recalls
// and still gets answered. It is answered in the fallback language, and
// nothing about it degrades: see ADR-0022's own cost section.
type Language string

// The two members of the Language vocabulary. English is first because it
// is the fallback (Fallback below), not because it is preferred.
const (
	LanguageEN Language = "en"
	LanguageES Language = "es"
)

// AllLanguages returns a fresh slice holding every Language vocabulary
// member, in the order the constants above declare them — a function, not
// an exported var, for the same mutability reason AllKinds is one. It is
// also the closed vocabulary the decoder matches "language" against.
func AllLanguages() []Language {
	return []Language{LanguageEN, LanguageES}
}

// Fallback is what a caller renders in when the classification named no
// language at all — the model omitted the field, or named one outside the
// vocabulary above and it degraded to null (I14).
//
// **It is a function of nothing, and that is deliberate.** The obvious
// alternative is a configured default, which would make the fallback a
// per-vault setting and hand every caller a reason to read configuration
// in order to render a sentence. ADR-0022 chose the message over the
// setting; a fallback that reintroduced the setting through the back door
// would undo that on exactly the path where the classifier already
// failed.
//
// English rather than a coin flip because this repository is written in
// English (CLAUDE.md), so English is the one language every fixed sentence
// is guaranteed to exist in. A fallback pointing anywhere else could name
// a language whose table has a hole in it.
func Fallback() Language { return LanguageEN }

// Or returns the language to render in: l when the classification named
// one, Fallback otherwise. Callers hold a *Language because every
// classify field is nilable (I14), and this is the one line that turns
// that into something a renderer can switch on totally.
//
// A method on the pointer rather than a free function taking one, so the
// call reads as the question it answers — `c.Language.Or()` — and so a
// caller cannot get the argument order wrong. A nil receiver is the
// expected case here, not an error: it is what an absent field looks
// like.
func (l *Language) Or() Language {
	if l == nil {
		return Fallback()
	}
	return *l
}
