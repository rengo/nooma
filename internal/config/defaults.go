package config

import (
	"fmt"
	"net"
	"sort"
	"strings"
)

// The four documented defaults of spec R3.4, and the only keys a nooma.yml may
// omit. They are constants so that changing one is a visible edit here rather
// than a literal buried in a function, and so a test can assert against the same
// value docs/01-architecture.md shows a user.
const (
	DefaultBind         = "127.0.0.1"
	DefaultHTTPPort     = 7777
	DefaultUI           = true
	DefaultDatabasePath = "./nooma.db"
)

// ApplyDefaults fills in the four absent-allowed keys and leaves every value the
// document set alone. It is idempotent.
//
// It is a separate step from Decode, not a pre-populated struct, so that absence
// stays observable up to the moment it is resolved. Pre-populating would make
// `ui: false` indistinguishable from an omitted `ui` — the difference between
// reporting the user's configuration and reporting our own guesses back at them.
func (c *Config) ApplyDefaults() {
	if c.Server.Bind == nil {
		bind := DefaultBind
		c.Server.Bind = &bind
	}
	if c.Server.HTTPPort == nil {
		port := DefaultHTTPPort
		c.Server.HTTPPort = &port
	}
	if c.Server.UI == nil {
		ui := DefaultUI
		c.Server.UI = &ui
	}
	if c.Database.Path == nil {
		path := DefaultDatabasePath
		c.Database.Path = &path
	}
}

// Summary renders the effective configuration for `nooma status` and
// `nooma doctor` (spec R12.1).
//
// It reports the *names* of the environment variables holding credentials and
// never their values (spec R4.2). Today that holds by construction, because no
// field in this schema can contain a secret — which is exactly why the test for
// it exists: the day somebody resolves a name to its value to make this output
// friendlier, a secret starts appearing in terminals, logs and pasted bug
// reports, and the test is what stops it.
//
// Call ApplyDefaults first. Summary reports what the binary will actually do, so
// an unresolved pointer here would be a bug in the caller rather than something
// to paper over with a fallback.
func (c *Config) Summary() string {
	var b strings.Builder

	bind := *c.Server.Bind
	exposure := "loopback"
	if !isLoopbackHost(bind) {
		exposure = "exposed"
	}
	fmt.Fprintf(&b, "bind:      %s:%d (%s)\n", bind, *c.Server.HTTPPort, exposure)
	fmt.Fprintf(&b, "ui:        %v\n", *c.Server.UI)
	fmt.Fprintf(&b, "database:  %s\n", *c.Database.Path)

	if c.Server.AuthTokenEnv != "" {
		fmt.Fprintf(&b, "auth token: from $%s\n", c.Server.AuthTokenEnv)
	} else {
		fmt.Fprintf(&b, "auth token: not configured\n")
	}

	if names := sortedKeys(c.Providers); len(names) > 0 {
		fmt.Fprintf(&b, "providers: %s\n", strings.Join(names, ", "))
		for _, name := range names {
			p := c.Providers[name]
			line := fmt.Sprintf("  %s: type=%s", name, p.Type)
			if p.Model != "" {
				line += " model=" + p.Model
			}
			if p.APIKeyEnv != "" {
				line += " key=$" + p.APIKeyEnv
			}
			fmt.Fprintln(&b, line)
		}
	}

	if names := sortedKeys(c.Tasks); len(names) > 0 {
		fmt.Fprintf(&b, "tasks:     %s\n", strings.Join(names, ", "))
	}

	if c.Channels.Telegram.Enabled {
		fmt.Fprintf(&b, "telegram:  enabled, %d allowed chat id(s), token from $%s\n",
			len(c.Channels.Telegram.AllowedChatIDs), c.Channels.Telegram.BotTokenEnv)
	} else {
		fmt.Fprintln(&b, "telegram:  disabled")
	}

	return b.String()
}

// isLoopbackHost decides ADR-0007's exposure question by parsing, never by
// string comparison. The full truth table and its adversarial cases belong to
// the binding decision in slice 10; this is the reporting half, and it must agree
// with that one — a summary that called an exposed bind "loopback" would be worse
// than no summary.
//
// The literal "localhost" is a deliberate special case with no DNS lookup: a
// resolution inside a security-relevant decision is slow at startup and
// untestable offline. Anything else that is not an IP fails safe as exposed.
func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

func sortedKeys[V any](m map[string]V) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
