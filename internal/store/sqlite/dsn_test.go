package sqlite

import (
	"strings"
	"testing"
)

// TestBuildDSN asserts buildDSN requests exactly the PRAGMA set design D3
// specifies, in the order D3 requires (busy_timeout first, so the driver's
// own default 1-minute busy handler — dropped the moment any _pragma
// appears — is never left unset).
//
// This is the only place foreign_keys and busy_timeout can be proven sound:
// they are per-connection, Vault exposes no query surface (design D7's
// scope boundary), and a raw connection opened independently of Open()'s
// own DSN observes SQLite's own compiled-in defaults, not what Open()
// requested — see the residual-risk note in pragma_test.go. Composed with
// the driver's own verified behavior (ground truth: "_pragma= parameters
// are executed at sqlite3_open time on every connection"), this is sound
// end-to-end proof of R2.1.
func TestBuildDSN(t *testing.T) {
	dsn := buildDSN("/tmp/vault.db")

	if !strings.HasPrefix(dsn, "file:") {
		t.Fatalf("buildDSN(...) = %q, want a file: URI", dsn)
	}

	order := []string{
		"_pragma=busy_timeout%285000%29",
		"_pragma=journal_mode%28wal%29",
		"_pragma=foreign_keys%28on%29",
		"_pragma=_txlock%28immediate%29",
	}
	last := -1
	for _, want := range order {
		i := strings.Index(dsn, want)
		if i < 0 {
			t.Fatalf("buildDSN(...) = %q, want it to contain %q", dsn, want)
		}
		if i < last {
			t.Fatalf("buildDSN(...) = %q, want %q before the previous pragma (D3 order)", dsn, want)
		}
		last = i
	}
}

// TestBuildDSNEscapesPath asserts the path is escaped via net/url rather
// than concatenated, so a vault path containing a space, '?' or '#' is not
// silently truncated or reinterpreted as a query parameter (design D3).
func TestBuildDSNEscapesPath(t *testing.T) {
	dsn := buildDSN("/tmp/my vault #1/nooma.db")

	if strings.Contains(dsn, " ") {
		t.Errorf("buildDSN(...) = %q, want the space escaped", dsn)
	}
	if strings.Count(dsn, "#") > 0 {
		t.Errorf("buildDSN(...) = %q, want '#' escaped (unescaped '#' starts a URI fragment)", dsn)
	}
}
