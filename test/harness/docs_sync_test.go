package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runDocsSyncScript runs scripts/docs-sync.sh with the given labels JSON
// (its one positional argument) and changed-file list (piped on stdin, one
// path per line — the same shape `git diff --name-only` produces), and
// reports its combined output and exit code. When noJQ is true, the
// subprocess runs with a PATH that cannot resolve jq, exercising the
// script's own "jq is required" guard.
func runDocsSyncScript(t *testing.T, labelsJSON string, changedFiles []string, noJQ bool) (output string, exitCode int) {
	t.Helper()

	scriptPath := filepath.Join(repoRootFromCaller(t), "scripts", "docs-sync.sh")
	cmd := exec.Command("sh", scriptPath, labelsJSON)
	cmd.Stdin = strings.NewReader(strings.Join(changedFiles, "\n") + "\n")
	if noJQ {
		// A PATH resolving every external command the script needs
		// (grep, sed, cat) EXCEPT jq — proves the script fails loudly
		// with its own message instead of a raw "jq: not found" from the
		// shell, regardless of where this machine's jq actually lives.
		cmd.Env = []string{"PATH=" + pathWithoutJQ(t)}
	}

	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("running %s: %v\noutput:\n%s", scriptPath, err, out)
	}
	return string(out), exitErr.ExitCode()
}

// pathWithoutJQ builds a directory of symlinks to every external command
// scripts/docs-sync.sh needs besides jq, and returns it as a standalone
// PATH value — so a subprocess using it can run grep/sed/cat but never
// resolve jq, wherever it actually lives on this machine.
func pathWithoutJQ(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	for _, tool := range []string{"grep", "sed", "cat"} {
		real, err := exec.LookPath(tool)
		if err != nil {
			t.Fatalf("LookPath(%q): %v", tool, err)
		}
		if err := os.Symlink(real, filepath.Join(dir, tool)); err != nil {
			t.Fatalf("symlink %s: %v", tool, err)
		}
	}
	return dir
}

// TestDocsSync is scripts/docs-sync.sh's regression test — the logic
// docs-sync.yml delegates to it, extracted specifically so it is testable
// without GitHub Actions (see doc.go and the script's own header comment).
func TestDocsSync(t *testing.T) {
	cases := []struct {
		name         string
		labelsJSON   string
		changed      []string
		wantExit     int
		wantContains string
	}{
		{
			name:         "core changed, no doc-02 change, no label — fails",
			labelsJSON:   "[]",
			changed:      []string{"internal/core/unit/foo.go"},
			wantExit:     1,
			wantContains: "FAIL: this PR changes internal/core/** but not docs/02-cognitive-core.md.",
		},
		{
			name:         "core and doc-02 both changed — passes",
			labelsJSON:   "[]",
			changed:      []string{"internal/core/unit/foo.go", "docs/02-cognitive-core.md"},
			wantExit:     0,
			wantContains: "OK: internal/core/** changed and docs/02-cognitive-core.md changed with it.",
		},
		{
			name:         "doc-02 changed alone — passes, nothing to gate",
			labelsJSON:   "[]",
			changed:      []string{"docs/02-cognitive-core.md"},
			wantExit:     0,
			wantContains: "OK: this PR does not touch internal/core/**",
		},
		{
			name:         "neither changed — passes",
			labelsJSON:   "[]",
			changed:      []string{"README.md"},
			wantExit:     0,
			wantContains: "OK: this PR does not touch internal/core/**",
		},
		{
			name:         "internal/core_helpers/ must not match ^internal/core/",
			labelsJSON:   "[]",
			changed:      []string{"internal/core_helpers/x.go"},
			wantExit:     0,
			wantContains: "OK: this PR does not touch internal/core/**",
		},
		{
			name:         "real no-spec-change label bypasses",
			labelsJSON:   `["no-spec-change"]`,
			changed:      []string{"internal/core/unit/foo.go"},
			wantExit:     0,
			wantContains: "OK: internal/core/** changed with no doc-02 change, and the PR carries",
		},
		{
			// The crafted-label bypass this test guards against: toJSON()
			// escapes an embedded quote as \", so a label literally named
			// `foo"no-spec-change` renders as ["foo\"no-spec-change"] —
			// whose raw text still contains "no-spec-change" contiguously.
			// A substring grep over that raw text is fooled; jq -e
			// 'index("no-spec-change")' operates on the decoded array and
			// is not, because the decoded label is
			// `foo"no-spec-change`, not `no-spec-change`.
			name:         "crafted label foo-quote-no-spec-change does not bypass",
			labelsJSON:   `["foo\"no-spec-change"]`,
			changed:      []string{"internal/core/unit/foo.go"},
			wantExit:     1,
			wantContains: "FAIL: this PR changes internal/core/** but not docs/02-cognitive-core.md.",
		},
		{
			name:         "simple label collisions do not bypass either",
			labelsJSON:   `["not-no-spec-change","no-spec-change-extra","xno-spec-change"]`,
			changed:      []string{"internal/core/unit/foo.go"},
			wantExit:     1,
			wantContains: "FAIL: this PR changes internal/core/** but not docs/02-cognitive-core.md.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			output, exitCode := runDocsSyncScript(t, tc.labelsJSON, tc.changed, false)

			if exitCode != tc.wantExit {
				t.Errorf("exit code = %d, want %d\noutput:\n%s", exitCode, tc.wantExit, output)
			}
			if !strings.Contains(output, tc.wantContains) {
				t.Errorf("output does not contain %q\noutput:\n%s", tc.wantContains, output)
			}
		})
	}
}

// TestDocsSyncFailsLoudlyWithoutJQ proves the script does not silently fall
// back to the substring-grep it replaced when jq is unavailable — it fails
// with an explicit, actionable message instead.
func TestDocsSyncFailsLoudlyWithoutJQ(t *testing.T) {
	output, exitCode := runDocsSyncScript(t, "[]", []string{"internal/core/unit/foo.go"}, true)

	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1\noutput:\n%s", exitCode, output)
	}
	if !strings.Contains(output, "FAIL: this gate needs jq") {
		t.Errorf("output does not report the missing-jq failure in the script's own voice\noutput:\n%s", output)
	}
}
