package sqlite

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

// TestBuildDSN asserts buildDSN requests exactly the PRAGMA set design D3
// specifies, AND that "_txlock" is sent as its own top-level query
// parameter rather than folded into a "_pragma" value. "_txlock" is not a
// PRAGMA — see buildDSN's doc comment — so a "_pragma" value containing it
// would be silently ignored by SQLite while a naive string-shaped
// assertion still passed. That is exactly the bug this test once encoded
// and could not catch; see TestOpenTxlockIsImmediate in this package's
// txlock_integration_test.go (build tag integration) for the behavioral
// proof this test cannot provide.
//
// This L1 test, together with the L3 TestOpenTxlockIsImmediate, is what
// proves _txlock is requested and honoured end-to-end. It is NOT, on its
// own, proof that foreign_keys and busy_timeout are honoured — see the L3
// TestOpenPRAGMAsReadBack in pragma_integration_test.go for that; this
// test only proves buildDSN requests the right PRAGMA set, composed with
// the driver's own verified behavior (ground truth: "_pragma= parameters
// are executed at sqlite3_open time on every connection") as partial,
// string-level evidence.
//
// The two conditions below are checked independently (each its own
// subtest, t.Errorf rather than t.Fatalf) rather than chained behind a
// single t.Fatalf: a first failing t.Fatalf calls runtime.Goexit and
// leaves the test right there, so a second, unrelated check placed after
// it is never reached and silently "passes" by never running at all — the
// prior shape of this test had exactly that dead assertion.
func TestBuildDSN(t *testing.T) {
	dsn, err := buildDSN("/tmp/vault.db")
	if err != nil {
		t.Fatalf("buildDSN(%q) = _, %v, want nil error", "/tmp/vault.db", err)
	}

	t.Run("requests every PRAGMA design D3 specifies", func(t *testing.T) {
		// Presence, not order, is what is behaviorally load-bearing: every
		// _pragma directive runs once, before any statement the caller
		// issues, at sqlite3_open time (ground truth: driver.go). Ordering
		// among them was previously asserted here too, but that tested an
		// incidental string layout with no behavior riding on it — dropped
		// per review.
		for _, want := range []string{
			"_pragma=busy_timeout%285000%29",
			"_pragma=journal_mode%28wal%29",
			"_pragma=foreign_keys%28on%29",
		} {
			if !strings.Contains(dsn, want) {
				t.Errorf("buildDSN(...) = %q, want it to contain %q", dsn, want)
			}
		}
	})

	t.Run("_txlock is its own top-level parameter, never folded into _pragma", func(t *testing.T) {
		if !strings.Contains(dsn, "_txlock=immediate") {
			t.Errorf("buildDSN(...) = %q, want it to contain the top-level parameter %q", dsn, "_txlock=immediate")
		}
		if strings.Contains(dsn, "_pragma=_txlock") {
			t.Errorf("buildDSN(...) = %q, want _txlock as its own parameter, not folded into a _pragma value (_txlock is not a PRAGMA — SQLite silently ignores it there)", dsn)
		}
	})
}

// TestBuildDSNAbsolutePathHandling covers the BLOCKER this test guards
// against: url.URL{Path: p}.String() treats a path that does not start
// with "/" as ambiguous with the URI authority component, silently
// routing the path's first segment into the host instead of failing. It
// asserts on the PARSED URI's Host and Path fields, never on the raw DSN
// string — a string-shaped assertion is exactly the defect class this
// test exists to catch (see the _txlock history above): a plausible-
// looking DSN string can still be silently wrong about which file it
// opens.
func TestBuildDSNAbsolutePathHandling(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		wantErr  bool
		wantPath string
	}{
		{
			name:     "posix absolute path",
			path:     "/home/pablo/vault.db",
			wantPath: "/home/pablo/vault.db",
		},
		{
			name:    "relative path is rejected, not silently misrouted",
			path:    "relvault.db",
			wantErr: true,
		},
		{
			name:    "dot-relative path is rejected, not silently misrouted",
			path:    "./data/vault.db",
			wantErr: true,
		},
		{
			name: "windows drive-letter absolute path becomes a valid file-absolute URI",
			path: `C:\Users\pablo\vault.db`,
			// RFC 8089's "file-absolute" form: forward slashes, and a
			// leading "/" ahead of the drive letter. Runnable on Linux —
			// this does not depend on the host OS's own path semantics.
			wantPath: "/C:/Users/pablo/vault.db",
		},
		{
			name:     "path containing a space, '?' and '#'",
			path:     "/tmp/my vault #1?share/nooma.db",
			wantPath: "/tmp/my vault #1?share/nooma.db",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn, err := buildDSN(tt.path)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("buildDSN(%q) = %q, nil, want a non-nil error", tt.path, dsn)
				}
				if !errors.Is(err, ErrRelativeDBPath) {
					t.Errorf("buildDSN(%q) error = %v, want it to wrap ErrRelativeDBPath", tt.path, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("buildDSN(%q) = _, %v, want nil error", tt.path, err)
			}

			u, perr := url.Parse(dsn)
			if perr != nil {
				t.Fatalf("url.Parse(buildDSN(%q)) = _, %v, want a valid URI", tt.path, perr)
			}
			if u.Host != "" {
				t.Errorf("buildDSN(%q) produced a URI with host %q, want an empty host (a non-empty host means the path was misrouted into the URI authority)", tt.path, u.Host)
			}
			if u.Path != tt.wantPath {
				t.Errorf("buildDSN(%q) produced a URI with path %q, want %q", tt.path, u.Path, tt.wantPath)
			}
		})
	}
}
