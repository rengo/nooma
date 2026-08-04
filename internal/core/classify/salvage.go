package classify

import (
	"bytes"
	"encoding/json"
)

// Salvage reads a JSON object's top-level members one at a time and
// returns every member it completed before the stream ended or went
// malformed — design D1: the truncation-tolerant mechanism I14 requires,
// not json.Unmarshal, which fails a truncated document wholesale.
//
// Salvage first locates the earliest '{' in raw and reads the object
// starting there, discarding everything before it unread (docs
// 02-cognitive-core.md §5.1). This is what lets a response wrapped in a
// markdown code fence — "```json\n{...}\n```", the shape a live OpenAI
// model actually returned in production even after being told not to
// (prompt.go's own instruction: "no prose, no code fence") — decode
// exactly as the same object would decode bare.
//
// Salvage never inspects the discarded prefix for meaning: it does not
// parse markdown, and it does not search past a failed first '{' for a
// later, better one. A stray '{' inside ordinary prose ("here is the JSON:
// {see below}") is picked as the start; reading fails immediately there,
// and that reports the same "nothing could be salvaged" outcome a response
// with no brace at all reports — never a classification silently built
// from the wrong span. Picking the wrong brace on purpose would be worse
// than failing outright, so this is a deliberate floor, not a gap: a
// preamble that happens to contain an earlier, independently well-formed
// `{...}` fragment before the real object (as opposed to one that merely
// contains a stray '{') is a known, accepted limit of scanning for the
// first brace rather than judging which one is "real" — unobserved in any
// recording this corpus carries, and not worth the guessing machinery a
// fix would need.
//
// truncatedAfter reports whether Salvage did not read a complete object to
// a clean close: no '{' anywhere in raw, a '{' that does not open a
// decodable object, or an object that was opened and cut short — a
// non-object payload, a payload cut before its first value, or a payload
// cut partway through a later member all report true. It answers one
// question — "is what came back trustworthy as a whole object" — never
// "was this response truncated by the transport specifically"; that
// narrower fact is not recoverable from the bytes alone once a preamble is
// tolerated. It only reaches a caller's decision when at least one field
// WAS salvaged: Decode's ErrNoFieldsSalvaged floor already covers the
// zero-field case on its own, so the flag's honest scope is exactly where
// missingReason (below) still reads it — telling a required field the
// model never emitted apart from one a genuine cutoff removed. fields
// never holds a partial value for an incomplete member — a member is
// either read in full or not present at all.
func Salvage(raw []byte) (fields map[string]json.RawMessage, truncatedAfter bool) {
	fields = make(map[string]json.RawMessage)

	start := bytes.IndexByte(raw, '{')
	if start < 0 {
		return fields, true
	}

	dec := json.NewDecoder(bytes.NewReader(raw[start:]))

	// Consume the opening '{' itself. raw[start] == '{' by the IndexByte
	// search above, so this call can neither error nor return anything but
	// that delimiter — design D11 point 3's "no unreachable arm" — and
	// nothing here branches on its result.
	_, _ = dec.Token()

	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return fields, true
		}
		// An object key token is a string by encoding/json's own grammar:
		// Token errors before ever returning anything else in this
		// position, so this assertion cannot panic. Asserting with an
		// "ok" check here would add a branch no input can take —
		// design D11 point 3's "no unreachable arm".
		key := keyTok.(string)

		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return fields, true
		}
		fields[key] = value
	}

	// Consume the closing '}'. If the stream ended before it arrived,
	// dec.More() above already returned false on EOF (it cannot tell "no
	// more elements" apart from "ran out of bytes") — this is what
	// distinguishes the two.
	if _, err := dec.Token(); err != nil {
		return fields, true
	}

	return fields, false
}
