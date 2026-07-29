package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// repoRootFromCaller mirrors test/conformance's helper of the same name
// (store_api_test.go) — same package depth (test/<pkg>/file.go), same
// derivation.
func repoRootFromCaller(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed to report this test file's path")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// runCoverageScript runs scripts/core-coverage.sh in test mode (a
// pre-built profile path as $1, so it never shells out to `go test`) and
// reports its combined output and exit code.
func runCoverageScript(t *testing.T, profilePath string) (output string, exitCode int) {
	t.Helper()

	scriptPath := filepath.Join(repoRootFromCaller(t), "scripts", "core-coverage.sh")
	cmd := exec.Command("sh", scriptPath, profilePath)
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

func writeProfile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "coverage.out")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture profile: %v", err)
	}
	return path
}

// TestCoreCoverageFloor is core-coverage.sh's regression test, docs/06-
// harness.md §6/§8 point 6 made real: without it, the FAIL branches of the
// floor logic have no way to ever run for real (see doc.go). Table cases
// cover the floor's boundary (89.9%/90%/90.1%), its two zero-total shapes
// (vacuous vs. real-but-uncovered), the smallest real case (1-of-1), and
// the defect this test package exists to guard against: duplicate ranges
// across coverage fragments from sibling packages sharing code under one
// -coverpkg scope (see scripts/core-coverage.sh's header comment).
func TestCoreCoverageFloor(t *testing.T) {
	cases := []struct {
		name         string
		profile      string
		wantExit     int
		wantContains []string
	}{
		{
			name: "exactly 90 percent passes",
			profile: "mode: set\n" +
				"f.go:1.1,1.2 90 1\n" +
				"f.go:2.1,2.2 10 0\n",
			wantExit:     0,
			wantContains: []string{"OK:", "90% (90/100)"},
		},
		{
			name: "89.9 percent fails — the floor never rounds up",
			profile: "mode: set\n" +
				"f.go:1.1,1.2 899 1\n" +
				"f.go:2.1,2.2 101 0\n",
			wantExit:     1,
			wantContains: []string{"FAIL:", "89% (899/1000)"},
		},
		{
			name: "90.1 percent passes",
			profile: "mode: set\n" +
				"f.go:1.1,1.2 901 1\n" +
				"f.go:2.1,2.2 99 0\n",
			wantExit:     0,
			wantContains: []string{"OK:", "90% (901/1000)"},
		},
		{
			name: "1-of-1 statement covered passes",
			profile: "mode: set\n" +
				"f.go:1.1,1.2 1 1\n",
			wantExit:     0,
			wantContains: []string{"OK:", "100% (1/1)"},
		},
		{
			name:         "header-only profile is a vacuous pass, distinct message",
			profile:      "mode: set\n",
			wantExit:     0,
			wantContains: []string{"no statements yet", "armed but vacuous"},
		},
		{
			name: "real statements all at count 0 fail at 0 percent, not vacuous",
			profile: "mode: set\n" +
				"f.go:1.1,1.2 25 0\n" +
				"f.go:2.1,2.2 25 0\n",
			wantExit:     1,
			wantContains: []string{"FAIL:", "0% (0/50)"},
		},
		{
			// Real go tool output (see scripts/core-coverage.sh's header
			// comment): two sibling packages (a, b) both import a shared
			// helper with two branches; a's tests exercise the "positive"
			// branch, b's exercise "non-positive", and a third,
			// never-called function is uncovered by either. -coverpkg
			// scope makes `go test` emit one profile fragment per test
			// binary (a, b, and shared's own no-test run), so every
			// shared.go range appears 3 times and a.go/b.go's ranges
			// appear once per fragment too — 16 data lines, 6 unique
			// ranges. `go tool cover -func` merges this to 83.3% (5 of 6
			// ranges ever covered by ANY fragment). A flat per-line sum
			// over the same 16 lines gives 6/16 = 37% — the exact defect
			// this package exists to catch a regression of.
			name: "duplicate ranges across fragments merge, they do not flat-sum",
			profile: "mode: set\n" +
				"a/a.go:5.24,5.53 1 1\n" +
				"b/b.go:5.24,5.53 1 0\n" +
				"shared/shared.go:6.29,7.11 1 1\n" +
				"shared/shared.go:7.11,9.3 1 1\n" +
				"shared/shared.go:10.2,10.23 1 0\n" +
				"shared/shared.go:15.22,17.2 1 0\n" +
				"a/a.go:5.24,5.53 1 0\n" +
				"b/b.go:5.24,5.53 1 1\n" +
				"shared/shared.go:6.29,7.11 1 1\n" +
				"shared/shared.go:7.11,9.3 1 0\n" +
				"shared/shared.go:10.2,10.23 1 1\n" +
				"shared/shared.go:15.22,17.2 1 0\n" +
				"shared/shared.go:6.29,7.11 1 0\n" +
				"shared/shared.go:7.11,9.3 1 0\n" +
				"shared/shared.go:10.2,10.23 1 0\n" +
				"shared/shared.go:15.22,17.2 1 0\n",
			wantExit: 1,
			// The dedup-correct merged answer is 5/6 (83%). 6/16 (37%,
			// what an un-deduplicated flat sum would report) must NOT
			// appear — that is the regression this case exists to catch.
			wantContains: []string{"FAIL:", "83% (5/6)"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			profilePath := writeProfile(t, tc.profile)
			output, exitCode := runCoverageScript(t, profilePath)

			if exitCode != tc.wantExit {
				t.Errorf("exit code = %d, want %d\noutput:\n%s", exitCode, tc.wantExit, output)
			}
			for _, want := range tc.wantContains {
				if !strings.Contains(output, want) {
					t.Errorf("output does not contain %q\noutput:\n%s", want, output)
				}
			}
			if strings.Contains(output, "6/16") {
				t.Errorf("output contains the un-deduplicated flat-sum answer (6/16) instead of the merged one — dedup regressed\noutput:\n%s", output)
			}
		})
	}
}
