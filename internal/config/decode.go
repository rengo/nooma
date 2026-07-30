package config

import (
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
	var cfg Config

	dec := yaml.NewDecoder(r, yaml.Strict())
	if err := dec.Decode(&cfg); err != nil {
		// An empty file is a valid vault configuration: every key of spec R3.4
		// is absent-allowed and everything else is optional at decode time, so
		// the document that says nothing decodes to the config that chose
		// nothing. The decoder reports that as EOF, which is not a failure.
		if errors.Is(err, io.EOF) {
			return &cfg, nil
		}
		return nil, fmt.Errorf("nooma.yml: %w", err)
	}

	return &cfg, nil
}
