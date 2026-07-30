package config

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// ParseEnvFile reads a vault's `.env` and returns its assignments, or an error
// naming the line that is wrong.
//
// The accepted subset is deliberately narrow (design D2), and the reason is not
// minimalism for its own sake. The `.env` format has no specification: every
// library invents its own tolerance for `export` prefixes, interpolation,
// multi-line values and lines it simply skips. A permissive parser turns a
// malformed line into a missing credential, and a missing credential surfaces as
// a provider error three layers away from the typo that caused it. This project
// has recorded twelve separate defects of exactly that shape.
//
// Accepted:
//
//	KEY=VALUE          KEY matches [A-Za-z_][A-Za-z0-9_]*
//	KEY="VALUE"        double quotes, stripped
//	KEY='VALUE'        single quotes, stripped
//	KEY=               a deliberate empty value
//	# comment          first non-space character is #
//	                   a blank line
//
// Everything else is an error, including a bare `#` after an unquoted value:
// stripping it and keeping it are both defensible conventions, so the input is
// ambiguous and gets rejected rather than guessed at. Quote it and the hash is
// unambiguously part of the value.
//
// Growing this subset later is additive and safe. Starting permissive is not
// reversible, because by then someone's `.env` depends on the tolerance.
func ParseEnvFile(r io.Reader) (map[string]string, error) {
	assignments := make(map[string]string)

	scanner := bufio.NewScanner(r)
	for line := 1; scanner.Scan(); line++ {
		text := scanner.Text()
		trimmed := strings.TrimSpace(text)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		key, value, found := strings.Cut(trimmed, "=")
		if !found {
			return nil, fmt.Errorf(".env line %d: no %q — every line must be KEY=VALUE, a # comment, or blank: %q", line, "=", text)
		}

		key = strings.TrimSpace(key)
		if err := validEnvKey(key); err != nil {
			return nil, fmt.Errorf(".env line %d: %w: %q", line, err, text)
		}
		if _, duplicate := assignments[key]; duplicate {
			return nil, fmt.Errorf(".env line %d: %s is assigned twice in this file; which one wins is not something to guess", line, key)
		}

		value, err := envValue(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf(".env line %d: %w: %q", line, err, text)
		}

		assignments[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf(".env: %w", err)
	}

	return assignments, nil
}

func validEnvKey(key string) error {
	if key == "" {
		return fmt.Errorf("the name before %q is empty", "=")
	}
	for i, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
		case i > 0 && r >= '0' && r <= '9':
		default:
			return fmt.Errorf("%q is not a valid variable name (letters, digits and _ only, not starting with a digit)", key)
		}
	}
	return nil
}

// envValue strips one matching pair of quotes, or rejects the value.
func envValue(raw string) (string, error) {
	if len(raw) >= 2 {
		first, last := raw[0], raw[len(raw)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return raw[1 : len(raw)-1], nil
		}
	}
	if strings.HasPrefix(raw, `"`) || strings.HasPrefix(raw, "'") {
		return "", fmt.Errorf("the quote opening this value is never closed")
	}
	if strings.Contains(raw, "#") {
		return "", fmt.Errorf("an unquoted %q is ambiguous — some tools treat it as a comment and some as part of the value; quote the value to keep it", "#")
	}
	return raw, nil
}

// ApplyEnv writes the file's assignments into the environment, and **never
// overwrites a variable that is already set** (spec R4.3).
//
// The direction is the requirement. An operator overriding a value for one run —
// a container, a systemd unit, a one-off shell — must not be silently overridden
// by a file on disk. The file fills gaps; it does not win arguments.
//
// A variable set to the empty string IS set. Treating empty as absent would let
// the file quietly override an operator who deliberately blanked a value, which
// is the same defect in a costume.
//
// lookup and setenv are injected for the same reason D7 injects the rest of the
// environment: precedence is the behavior under test, and a test that had to
// mutate the real process environment could not run in parallel with any other.
func ApplyEnv(assignments map[string]string, lookup func(string) (string, bool), setenv func(string, string) error) error {
	for _, key := range sortedKeys(assignments) {
		if _, alreadySet := lookup(key); alreadySet {
			continue
		}
		if err := setenv(key, assignments[key]); err != nil {
			return fmt.Errorf("setting %s from .env: %w", key, err)
		}
	}
	return nil
}
