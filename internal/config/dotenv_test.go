package config

import (
	"strings"
	"testing"
)

// TestParseEnvFileAcceptsTheDocumentedSubset covers every shape design D2 admits.
// The subset is small on purpose: the `.env` format has no specification, and
// every library that tries to be generous invents its own tolerance. A malformed
// line silently skipped becomes a missing credential, which becomes a provider
// error three layers away from the typo that caused it.
func TestParseEnvFileAcceptsTheDocumentedSubset(t *testing.T) {
	t.Parallel()

	const document = `# a comment
ANTHROPIC_API_KEY=sk-plain

  # an indented comment
TELEGRAM_BOT_TOKEN="123:quoted-double"
NOOMA_AUTH_TOKEN='quoted-single'
EMPTY=
_LEADING_UNDERSCORE=ok
WITH_DIGITS_9=ok
`

	got, err := ParseEnvFile(strings.NewReader(document))
	if err != nil {
		t.Fatalf("ParseEnvFile rejected the documented subset: %v", err)
	}

	want := map[string]string{
		"ANTHROPIC_API_KEY":   "sk-plain",
		"TELEGRAM_BOT_TOKEN":  "123:quoted-double",
		"NOOMA_AUTH_TOKEN":    "quoted-single",
		"EMPTY":               "",
		"_LEADING_UNDERSCORE": "ok",
		"WITH_DIGITS_9":       "ok",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d assignments, want %d: %v", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %q, want %q", k, got[k], v)
		}
	}
}

// TestParseEnvFileRejectsEverythingElse is the substance of D2. Each case is a
// shape some other `.env` library accepts; here each one is an error naming the
// line, because the alternative is a credential that silently is not there.
func TestParseEnvFileRejectsEverythingElse(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		document string
		wantLine string
	}{
		{"no equals sign", "ANTHROPIC_API_KEY sk-plain\n", "line 1"},
		{"export prefix", "export ANTHROPIC_API_KEY=sk-plain\n", "line 1"},
		{"key starting with a digit", "9LIVES=ok\n", "line 1"},
		{"key with a dash", "MY-KEY=ok\n", "line 1"},
		{"key with a space", "MY KEY=ok\n", "line 1"},
		{"empty key", "=orphan\n", "line 1"},
		{"unterminated double quote", "K=\"unterminated\n", "line 1"},
		{"unterminated single quote", "K='unterminated\n", "line 1"},
		{"duplicate key in one file", "K=first\nK=second\n", "line 2"},
		{"bare hash after a value", "K=value # trailing comment\n", "line 1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseEnvFile(strings.NewReader(tc.document))
			if err == nil {
				t.Fatalf("ParseEnvFile accepted %s: %v", tc.name, got)
			}
			if !strings.Contains(err.Error(), tc.wantLine) {
				t.Errorf("error does not name %s, so the user cannot find it:\n%v", tc.wantLine, err)
			}
			if got != nil {
				t.Error("ParseEnvFile returned assignments alongside an error")
			}
		})
	}
}

// TestParseEnvFileRejectsBareHashExplicitly earns its own test because it is the
// one shape D2 could plausibly have swallowed instead of rejecting: most `.env`
// tooling strips a trailing comment, so `K=value # note` would silently become
// "value" in some readers and "value # note" in others. Ambiguous input with two
// defensible readings is exactly what this parser refuses.
func TestParseEnvFileRejectsBareHashExplicitly(t *testing.T) {
	t.Parallel()

	if _, err := ParseEnvFile(strings.NewReader("K=value # note\n")); err == nil {
		t.Fatal("a bare # after a value was accepted; its meaning is ambiguous between two conventions")
	}

	// Quoted, it is unambiguous: the hash is part of the value.
	got, err := ParseEnvFile(strings.NewReader(`K="value # note"` + "\n"))
	if err != nil {
		t.Fatalf("a quoted hash should be part of the value: %v", err)
	}
	if got["K"] != "value # note" {
		t.Errorf("K: got %q, want the hash kept inside the quotes", got["K"])
	}
}

// TestApplyEnvDoesNotOverwriteTheProcessEnvironment is spec R4.3, and the
// direction matters: an operator overriding a value for one run — a container, a
// systemd unit, a one-off shell — must not be silently overridden by a file on
// disk. The file fills gaps; it does not win arguments.
func TestApplyEnvDoesNotOverwriteTheProcessEnvironment(t *testing.T) {
	t.Parallel()

	assignments := map[string]string{
		"ONLY_IN_FILE": "from-file",
		"IN_BOTH":      "from-file",
	}

	environment := map[string]string{
		"IN_BOTH":     "from-operator",
		"ONLY_IN_ENV": "from-operator",
	}

	set := map[string]string{}
	lookup := func(k string) (string, bool) { v, ok := environment[k]; return v, ok }
	setenv := func(k, v string) error { set[k] = v; environment[k] = v; return nil }

	if err := ApplyEnv(assignments, lookup, setenv); err != nil {
		t.Fatalf("ApplyEnv: %v", err)
	}

	if got := environment["ONLY_IN_FILE"]; got != "from-file" {
		t.Errorf("ONLY_IN_FILE: got %q, want the file's value for a variable the environment lacked", got)
	}
	if got := environment["IN_BOTH"]; got != "from-operator" {
		t.Errorf("IN_BOTH: got %q — the process environment must win over the file", got)
	}
	if _, touched := set["IN_BOTH"]; touched {
		t.Error("ApplyEnv called setenv for a variable that was already set")
	}
	if got := environment["ONLY_IN_ENV"]; got != "from-operator" {
		t.Errorf("ONLY_IN_ENV: got %q, want it untouched", got)
	}
}

// TestApplyEnvSetsAnEmptyFileValue guards a case that reads as an edge and is
// not: `EMPTY=` in the file is a deliberate empty value, not an absent one, and
// a variable the environment does not define at all is a gap the file may fill —
// including with emptiness.
func TestApplyEnvSetsAnEmptyFileValue(t *testing.T) {
	t.Parallel()

	environment := map[string]string{}
	lookup := func(k string) (string, bool) { v, ok := environment[k]; return v, ok }
	setenv := func(k, v string) error { environment[k] = v; return nil }

	if err := ApplyEnv(map[string]string{"EMPTY": ""}, lookup, setenv); err != nil {
		t.Fatalf("ApplyEnv: %v", err)
	}
	if v, ok := environment["EMPTY"]; !ok || v != "" {
		t.Errorf(`EMPTY: got (%q, %v), want ("", true)`, v, ok)
	}
}

// TestApplyEnvRespectsAnEmptyProcessValue is the mirror image, and the trap: a
// variable set to the empty string IS set. Treating empty as absent would let a
// file quietly override an operator who deliberately blanked a value.
func TestApplyEnvRespectsAnEmptyProcessValue(t *testing.T) {
	t.Parallel()

	environment := map[string]string{"BLANKED": ""}
	lookup := func(k string) (string, bool) { v, ok := environment[k]; return v, ok }
	setenv := func(k, v string) error { environment[k] = v; return nil }

	if err := ApplyEnv(map[string]string{"BLANKED": "from-file"}, lookup, setenv); err != nil {
		t.Fatalf("ApplyEnv: %v", err)
	}
	if got := environment["BLANKED"]; got != "" {
		t.Errorf("BLANKED: got %q — an empty value is still a set value", got)
	}
}
