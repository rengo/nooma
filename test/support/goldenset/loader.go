package goldenset

import (
	"encoding/json"
	"os"
)

// Load reads path, decodes it as JSON into v, and rejects any field
// present in the document that v does not declare (design §10, spec
// R10.3) — the mechanism that turns an added, undocumented field into a
// decode error instead of one that is silently ignored.
func Load(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck // read-only close; the file was only ever read from, and the decode below already reports the failure that matters

	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
