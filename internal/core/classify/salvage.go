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
// truncatedAfter reports whether the object was not read to a clean close:
// a non-object payload, a payload cut before its first value, or a payload
// cut partway through a later member all report true. fields never holds a
// partial value for an incomplete member — a member is either read in full
// or not present at all.
func Salvage(raw []byte) (fields map[string]json.RawMessage, truncatedAfter bool) {
	fields = make(map[string]json.RawMessage)

	dec := json.NewDecoder(bytes.NewReader(raw))

	tok, err := dec.Token()
	if err != nil {
		return fields, true
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return fields, true
	}

	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return fields, true
		}
		key, ok := keyTok.(string)
		if !ok {
			return fields, true
		}

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
