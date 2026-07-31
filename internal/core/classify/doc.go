// Package classify decodes a provider's raw capture response into a
// per-field-optional Classification — docs/02-cognitive-core.md §5 step 1,
// I14. Decoding is a salvaging read, not json.Unmarshal: a malformed or
// truncated field degrades to its absent value, the rest of the
// classification survives (design D1).
//
// See docs/06-harness.md §1 for the dependency rule.
package classify
