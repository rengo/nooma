package httpapi

import (
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/config"
)

func cfg(t *testing.T, document string) *config.Config {
	t.Helper()

	c, err := config.Decode(strings.NewReader(document))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	c.ApplyDefaults()
	return c
}

// TestDecideBinding is ADR-0007 made executable, and the adversarial rows are the
// substance rather than decoration.
//
// The decision is made by parsing, never by string comparison. A prefix or
// substring test that accepted "127.0.0.1.evil" as loopback would disable the
// whole rule silently — the server would bind a public interface believing it was
// local, and nothing anywhere would say so.
//
// The literal "localhost" is a deliberate special case with NO DNS lookup: a
// resolution inside a security-relevant decision is slow at startup and
// untestable offline, which non-negotiable #5 forbids. Anything else that is not
// an IP reads as exposed, which is the safe direction.
func TestDecideBinding(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		document string
		env      map[string]string
		wantErr  bool
	}{
		{name: "the default bind needs no token", document: "{}\n"},
		{name: "explicit loopback", document: "server:\n  bind: 127.0.0.1\n"},
		{name: "anywhere in 127.0.0.0/8", document: "server:\n  bind: 127.9.9.9\n"},
		{name: "IPv6 loopback", document: "server:\n  bind: ::1\n"},
		{name: "the literal localhost", document: "server:\n  bind: localhost\n"},

		{name: "0.0.0.0 without a token", document: "server:\n  bind: 0.0.0.0\n", wantErr: true},
		{name: ":: without a token", document: "server:\n  bind: \"::\"\n", wantErr: true},
		{name: "a LAN address without a token", document: "server:\n  bind: 192.168.1.10\n", wantErr: true},
		{
			name:     "0.0.0.0 with a token variable that is unset",
			document: "server:\n  bind: 0.0.0.0\n  auth_token_env: NOOMA_AUTH_TOKEN\n",
			wantErr:  true,
		},
		{
			name:     "0.0.0.0 with a token variable that is set",
			document: "server:\n  bind: 0.0.0.0\n  auth_token_env: NOOMA_AUTH_TOKEN\n",
			env:      map[string]string{"NOOMA_AUTH_TOKEN": "s3cret"},
		},

		// A hostname that merely looks like loopback. This is the row that a
		// substring check gets wrong, and getting it wrong means binding a public
		// interface while believing otherwise.
		{name: "127.0.0.1.evil is a hostname", document: "server:\n  bind: 127.0.0.1.evil\n", wantErr: true},
		{name: "0127.0.0.1 is not an IP", document: "server:\n  bind: 0127.0.0.1\n", wantErr: true},
		{name: "an empty bind is not loopback", document: "server:\n  bind: \"\"\n", wantErr: true},
		{name: "localhost.attacker.example", document: "server:\n  bind: localhost.attacker.example\n", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			lookup := func(k string) (string, bool) { v, ok := tc.env[k]; return v, ok }
			addr, err := DecideBinding(cfg(t, tc.document), lookup)

			switch {
			case tc.wantErr && err == nil:
				t.Fatalf("DecideBinding accepted %s and returned %q — ADR-0007 requires a token for a non-loopback bind", tc.name, addr)
			case !tc.wantErr && err != nil:
				t.Fatalf("DecideBinding rejected a valid binding: %v", err)
			case tc.wantErr:
				if !strings.Contains(err.Error(), "auth_token_env") {
					t.Errorf("the error does not name what the user must set:\n%v", err)
				}
				if addr != "" {
					t.Errorf("DecideBinding returned an address %q alongside a refusal; nothing may listen on it", addr)
				}
			default:
				if !strings.Contains(addr, ":7777") {
					t.Errorf("DecideBinding returned %q, want the configured port", addr)
				}
			}
		})
	}
}

// TestDecideBindingNeverResolvesDNS pins the "no lookup" half of the rule. A name
// that cannot resolve must still produce an answer, promptly — if this ever
// starts resolving, an offline machine would hang at startup on a decision that
// should be a parse.
func TestDecideBindingNeverResolvesDNS(t *testing.T) {
	t.Parallel()

	_, err := DecideBinding(cfg(t, "server:\n  bind: nothing.invalid\n"), func(string) (string, bool) { return "", false })
	if err == nil {
		t.Fatal("an unresolvable hostname was treated as loopback")
	}
}
