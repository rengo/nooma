package sqlite

import (
	"errors"
	"net/url"
	"runtime"
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
	dsn, err := buildDSN("/tmp/vault.db", posixStyle)
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
// asserts on the PARSED URI's components, never on the raw DSN string —
// a string-shaped assertion is exactly the defect class this test exists
// to catch (see the _txlock history above): a plausible-looking DSN string
// can still be silently wrong about which file it opens.
//
// It is still not enough on its own, and the Windows breakage proved it:
// every case here passed while no vault could be opened on Windows at all,
// because a URI can be correct by the RFC and still name a file this
// driver's VFS cannot reach. The behavioral half lives in
// dsn_windows_style_integration_test.go, which opens the file.
//
// This table is deliberately split by pathStyle rather than guessing the
// style from each path's own shape (see pathStyle's doc comment in dsn.go
// for why bidirectional shape-guessing is the residual defect this table
// exists to close). The POSIX/Windows-shaped cases below are run against
// EACH style explicitly, so the table proves both:
//   - that a style recognizes its own absolute shape, and
//   - that a style does NOT recognize the other style's shape as absolute
//     — "C:/data/vault.db" and `C:\data\vault.db` are valid *relative*
//     POSIX paths (a backslash is an ordinary filename byte, and a
//     directory can be named "C:"; measured on this repo's own runner:
//     `mkdir -p 'C:'/data && echo ok > 'C:'/data/vault.db` works) and MUST
//     be rejected, not silently rewritten to an absolute path outside the
//     caller's intended directory.
func TestBuildDSNAbsolutePathHandling(t *testing.T) {
	tests := []struct {
		name     string
		style    pathStyle
		path     string
		wantErr  bool
		wantPath string
	}{
		{
			name:     "posix: leading slash is absolute",
			style:    posixStyle,
			path:     "/home/pablo/vault.db",
			wantPath: "/home/pablo/vault.db",
		},
		{
			name:    "posix: bare relative path is rejected, not silently misrouted",
			style:   posixStyle,
			path:    "relvault.db",
			wantErr: true,
		},
		{
			name:    "posix: dot-relative path is rejected, not silently misrouted",
			style:   posixStyle,
			path:    "./data/vault.db",
			wantErr: true,
		},
		{
			// Regression coverage: watched RED against the pre-fix buildDSN,
			// which guessed absoluteness from the string's shape regardless
			// of style and accepted this as absolute.
			name:    "posix: forward-slash windows-drive-shaped path is relative, not absolute",
			style:   posixStyle,
			path:    "C:/data/vault.db",
			wantErr: true,
		},
		{
			// Same regression, backslash variant.
			name:    "posix: backslash windows-drive-shaped path is relative, not absolute",
			style:   posixStyle,
			path:    `C:\data\vault.db`,
			wantErr: true,
		},
		{
			name:     "posix: path containing a space, '?' and '#' is preserved exactly",
			style:    posixStyle,
			path:     "/home/pa blo/my?weird#vault.db",
			wantPath: "/home/pa blo/my?weird#vault.db",
		},
		{
			name:  "windows: backslash drive-letter path becomes a URI this driver can open",
			style: windowsStyle,
			path:  `C:\Users\pablo\vault.db`,
			// Forward slashes, and NO leading "/" ahead of the drive
			// letter — the opaque "file:C:/..." form, not RFC 8089's
			// "file:///C:/...". setURIPath's doc comment owns that
			// decision; TestBuildDSNWindowsStyleOpensTheFileItNames is
			// what proves it opens a real file. Runnable on Linux — this
			// does not depend on the host OS's own path semantics,
			// because style is injected, never guessed from the runner.
			wantPath: "C:/Users/pablo/vault.db",
		},
		{
			name:     "windows: forward-slash drive-letter path becomes a URI this driver can open",
			style:    windowsStyle,
			path:     "C:/Users/pablo/vault.db",
			wantPath: "C:/Users/pablo/vault.db",
		},
		{
			name:    "windows: bare relative filename is rejected, not silently misrouted",
			style:   windowsStyle,
			path:    "vault.db",
			wantErr: true,
		},
		{
			name:    "windows: driveless relative path is rejected, not silently misrouted",
			style:   windowsStyle,
			path:    `data\vault.db`,
			wantErr: true,
		},
		{
			// The regression guard on setURIPath's own escaping: the
			// opaque component is written verbatim by url.URL.String(),
			// so unlike the posix case above, net/url is NOT what keeps
			// these characters from truncating the DSN.
			name:     "windows: path containing a space, '?' and '#' is preserved exactly",
			style:    windowsStyle,
			path:     `C:\Users\pablo\my vault #1?share.db`,
			wantPath: "C:/Users/pablo/my vault #1?share.db",
		},
		{
			// A literal '%' must survive as a literal '%', not be read
			// back as the start of an escape sequence. Same reason.
			name:     "windows: path containing a literal '%' survives the round trip",
			style:    windowsStyle,
			path:     `C:\Users\pablo\100%\vault.db`,
			wantPath: "C:/Users/pablo/100%/vault.db",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn, err := buildDSN(tt.path, tt.style)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("buildDSN(%q, %v) = %q, nil, want a non-nil error", tt.path, tt.style, dsn)
				}
				if !errors.Is(err, ErrRelativeDBPath) {
					t.Errorf("buildDSN(%q, %v) error = %v, want it to wrap ErrRelativeDBPath", tt.path, tt.style, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("buildDSN(%q, %v) = _, %v, want nil error", tt.path, tt.style, err)
			}

			u, perr := url.Parse(dsn)
			if perr != nil {
				t.Fatalf("url.Parse(buildDSN(%q, %v)) = _, %v, want a valid URI", tt.path, tt.style, perr)
			}
			if u.Host != "" {
				t.Errorf("buildDSN(%q, %v) produced a URI with host %q, want an empty host (a non-empty host means the path was misrouted into the URI authority)", tt.path, tt.style, u.Host)
			}

			// The two styles carry the path in different URI components,
			// by construction (setURIPath): posixStyle in Path, where
			// net/url escapes it, and windowsStyle in Opaque, escaped by
			// setURIPath itself. Asserting that exactly one of them is
			// populated is what keeps a future edit from quietly filling
			// both, or the wrong one.
			got, gotComponent := u.Path, "path"
			if tt.style == windowsStyle {
				if u.Path != "" {
					t.Errorf("buildDSN(%q, %v) produced a URI with path %q, want the windows form to leave path empty and carry the vault path in the opaque component", tt.path, tt.style, u.Path)
				}
				decoded, uerr := url.PathUnescape(u.Opaque)
				if uerr != nil {
					t.Fatalf("url.PathUnescape(%q) = _, %v, want the opaque component to be validly escaped — SQLite percent-decodes it before opening", u.Opaque, uerr)
				}
				got, gotComponent = decoded, "opaque"
			} else if u.Opaque != "" {
				t.Errorf("buildDSN(%q, %v) produced a URI with opaque %q, want the posix form to carry the vault path in the path component", tt.path, tt.style, u.Opaque)
			}

			if got != tt.wantPath {
				t.Errorf("buildDSN(%q, %v) produced a URI whose %s component resolves to %q, want %q", tt.path, tt.style, gotComponent, got, tt.wantPath)
			}
		})
	}
}

// TestPathStyleForGOOS asserts the one place production resolves pathStyle
// from the real environment (pathStyle's doc comment in dsn.go): windows
// maps to windowsStyle, and every other GOOS maps to posixStyle. This is
// checked against the CONSTANT runtime.GOOS this test binary was actually
// built for, not a simulated value — pathStyleForGOOS takes no parameter on
// purpose (nothing else in this package should call it with an assumed
// GOOS), so this test proves the mapping rule, not a specific branch.
func TestPathStyleForGOOS(t *testing.T) {
	got := pathStyleForGOOS()

	want := posixStyle
	if runtime.GOOS == "windows" {
		want = windowsStyle
	}

	if got != want {
		t.Errorf("pathStyleForGOOS() = %v, want %v for runtime.GOOS = %q", got, want, runtime.GOOS)
	}
}
