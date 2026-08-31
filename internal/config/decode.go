package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	yaml "github.com/goccy/go-yaml"
)

// Decode reads one nooma.yml and returns it, or an error naming what is wrong
// with it. It never returns a partially populated config alongside an error
// (spec R3.3): the caller gets a usable value or nothing.
//
// Two rejection properties matter more than the parsing, and they come from two
// independent mechanisms in the decoder — worth stating because it would be easy
// to assume one option buys both:
//
//   - **Unknown keys** are rejected by yaml.Strict() (spec R3.2). Without it a
//     mistyped key is silently ignored, and the user's next experience is fixing
//     a typo, restarting, and observing no change at all —
//     docs/01-architecture.md sells this configuration on "nothing hidden in
//     defaults", and a permissive decoder breaks that promise in its most
//     expensive form.
//   - **Duplicate keys** are rejected by the decoder's default behavior, gated
//     by a separate flag that only an explicit opt-out disables. Strict() has
//     nothing to do with it. That is precisely why decode_test.go asserts it:
//     no code in this package enforces it, so a change of decoder or of options
//     could drop it with nothing failing.
//
// Applying defaults is deliberately not Decode's job — see ApplyDefaults.
func Decode(r io.Reader) (*Config, error) {
	doc, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("nooma.yml: %w", err)
	}

	var cfg Config

	dec := yaml.NewDecoder(bytes.NewReader(doc), yaml.Strict())
	if err := dec.Decode(&cfg); err != nil {
		// An empty file is a valid vault configuration: every key of spec R3.4
		// is absent-allowed and everything else is optional at decode time, so
		// the document that says nothing decodes to the config that chose
		// nothing. The decoder reports that as EOF, which is not a failure.
		if errors.Is(err, io.EOF) {
			return &cfg, nil
		}
		if retired := retiredKeyIn(doc); retired != nil {
			return nil, fmt.Errorf("nooma.yml: `%s` is no longer a configuration key. %s", retired.key, retired.explanation)
		}
		return nil, fmt.Errorf("nooma.yml: %w", err)
	}

	return &cfg, nil
}

// retiredKey is a top-level key that a nooma.yml may still carry because an
// older docs/01-architecture.md documented it, together with what its owner
// needs to hear.
type retiredKey struct {
	key         string
	explanation string
}

// retiredKeys is ordered rather than a map so the message a given document
// produces never depends on iteration order.
var retiredKeys = []retiredKey{
	{
		key: "schedules",
		explanation: "Nooma consolidates at 03:00 local and runs its proactive check every " +
			"5 minutes; neither is configurable, and nothing here parses a cron expression " +
			"(ADR-0025). Delete the block to start. To stop the nightly pass entirely, set " +
			"`consolidation_enabled = 0` in the vault's config table.",
	},
}

// retiredKeyIn re-reads the document permissively to tell a retired key from a
// mistyped one.
//
// It runs on the error path only, and that placement is the whole design. A
// retired key and a typo are both "unknown field" to the strict decoder, and
// they need opposite messages: one says "you misspelled this", the other says
// "this setting no longer exists, and here is what happens instead". Matching on
// the strict decoder's own error text would tie that distinction to a
// dependency's wording; re-reading the document does not.
//
// A document that cannot be parsed even permissively — a duplicate key, broken
// YAML — yields nothing, and the caller reports the decoder's own error, which
// is the accurate one in that case.
func retiredKeyIn(doc []byte) *retiredKey {
	var top map[string]any
	if err := yaml.Unmarshal(doc, &top); err != nil {
		return nil
	}
	for i := range retiredKeys {
		if _, ok := top[retiredKeys[i].key]; ok {
			return &retiredKeys[i]
		}
	}
	return nil
}
