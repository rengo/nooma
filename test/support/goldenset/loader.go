package goldenset

import (
	"bytes"
	"encoding/json"
	"os"
)

// DecodeStrict decodes data as JSON into v, rejecting any field present in
// the document that v does not declare (design §10, spec R10.3). This is
// the single decoder configuration Load applies to a real, on-disk case
// file — exported so any other caller validating an in-memory JSON document
// (a format.md's fenced example, extracted by ExtractJSONFence, rather than
// a file with its own path) goes through the exact same
// json.Decoder.DisallowUnknownFields rule, instead of a second,
// independently-configured decoder that could quietly drift from Load's.
func DecodeStrict(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
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
