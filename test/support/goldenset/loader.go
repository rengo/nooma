package goldenset

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

// DecodeStrict decodes data as JSON into v, rejecting any field present in
// the document that v does not declare (design §10, spec R10.3), and then,
// when v implements Validator, rejecting a document missing one of that
// type's "Required: yes" fields (four-lens pre-PR review, WARNING finding
// 4; CRITICAL finding 2) — DecodeStrict([]byte("{}"), &ClassifyExample{})
// used to return nil before this check existed. This is the single decoder
// configuration Load applies to a real, on-disk case file — exported so any
// other caller validating an in-memory JSON document (a format.md's fenced
// example, extracted by ExtractJSONFence, rather than a file with its own
// path) goes through the exact same rules, instead of a second,
// independently-configured decoder that could quietly drift from Load's.
// One consequence: a format.md's fenced example gutted down to any subset
// of its fields — not just `{}` — now fails the same way a real case file
// would.
func DecodeStrict(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if validator, ok := v.(Validator); ok {
		if err := validator.Validate(); err != nil {
			return fmt.Errorf("required-field validation failed: %w", err)
		}
	}
	return nil
}

// Load reads path and decodes it as JSON into v via DecodeStrict — the
// mechanism that turns an added, undocumented field into a decode error
// instead of one that is silently ignored.
func Load(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return DecodeStrict(data, v)
}
