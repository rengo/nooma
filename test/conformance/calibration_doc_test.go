// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"go/constant"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The three literals that locate doc 02's calibration table, plus the floor
// that stops this gate from passing vacuously.
const (
	calibrationDocPath = "docs/02-cognitive-core.md"
	calibrationHeading = "## 13. Calibration"

	// calibrationMinSymbols is the number of §13 rows that named a core
	// constant when this gate was written (2026-08-07, HEAD c68d5ad). It is a
	// floor, not an equality: m2c and m2d are expected to add rows, and adding
	// one must not fail this test. What the floor catches is the opposite —
	// the table, the heading, or the row grammar changing shape underneath the
	// parser, which would otherwise turn this whole gate green by checking
	// nothing at all.
	calibrationMinSymbols = 21
)

var (
	// calibrationQualifiedRef matches a fully package-qualified reference to a
	// core symbol as §13 writes it: `internal/core/consolidation.StrengthenGain`.
	calibrationQualifiedRef = regexp.MustCompile(`internal/core/([a-z0-9/]+)\.([A-Z][A-Za-z0-9_]*)`)

	// calibrationResolverRef matches the bare, unqualified companion names §13
	// writes after a "+" on the same row: `ResolveWeightThreshold`. They carry
	// no package, so they are resolved against the package of the qualified
	// symbol on their own row.
	calibrationResolverRef = regexp.MustCompile("`(Resolve[A-Za-z0-9_]*)`")

	// calibrationLeadingNumber matches the number a Default cell must begin
	// with. Anchored at the start on purpose — see the "It reads the number,
	// it does not search for one" note on the test below.
	calibrationLeadingNumber = regexp.MustCompile(`^-?\d+(?:\.\d+)?`)
)

// calibrationKnob is one §13 row that names a core symbol: the constant, the
// number the document says it holds, and any resolver named alongside it.
type calibrationKnob struct {
	pkgRel    string // e.g. "internal/core/consolidation"
	constName string // e.g. "StrengthenGain"
	docValue  string // the literal as §13 writes it, e.g. "0.10"
	resolvers []string
	row       string // the raw row, for error messages
}

// TestHarness_CalibrationTableMatchesConstants makes docs/02-cognitive-core.md
// §13 executable: every row of the calibration table that names a constant
// under internal/core/ must name one that exists, is a constant, and holds
// exactly the number the row documents.
//
// # Why this is a gate and not a convention
//
// docs/06-harness.md §7 already carries the rule — "every number in the doc 02
// §13 table is a named constant in exactly one place". It is written down, it
// is correct, and it was violated four consecutive times inside
// m2b-consolidation-core, twice with the instruction explicit in the
// implementer's brief. Every one of those violations passed CI. A convention
// that four attempts in a row fail to apply is not a weak convention; it is a
// missing gate, and this is the gate. It exists before m2c adds the next batch
// of calibratables rather than after.
//
// # What it checks
//
// For each §13 row whose Knob cell contains `internal/core/<pkg>.<Symbol>`:
//
//  1. the package type-checks and exports that symbol;
//  2. the symbol is a *constant*, not a var or a func — §7's rule is that a
//     calibratable is a named constant in one place, and a var is a place two
//     goroutines can disagree about;
//  3. its value equals the number the Default cell leads with, compared as
//     exact go/constant values rather than as float64, so 0.10 and 0.1 are the
//     same number and 0.1 and 0.100000000000000005 are not;
//  4. any `ResolveXxx` companion the same row names exists in that same
//     package as a function.
//
// # What it deliberately does NOT check
//
// Being precise here matters more than usual: the defect class this repository
// keeps producing is not wrong code, it is a description wider than the code it
// describes. So, explicitly —
//
//   - **The reverse direction is not covered.** A constant that lives under
//     internal/core/ and is absent from §13 does not fail this test. Most core
//     constants are not calibratables (ResurfaceMaxHops' loop bound is, the
//     phase count is not), and no mechanical rule separates the two, so the
//     table remains the authority on what is calibratable.
//   - **Rows naming no core symbol are skipped entirely**, and there are many:
//     `Quiet hours`, `RRF k`, `recall_top_k`, `Push threshold`. They are
//     unimplemented, not unpinned, and this gate will start covering each one
//     on the day its row names the constant that implements it. That is the
//     intended growth path, and it is also why calibrationMinSymbols is a floor.
//   - **It does not check which schema home a knob reads at runtime.**
//     goal_stagnation_days has two (config.goal_stagnation_days and
//     calibration's own key) and m2c picks one; this gate pins the Go constant
//     against the document, nothing more.
//   - **It does not pin the constant against the migration DEFAULT.** That is a
//     different edge of the same triangle, and the *_ddl_test.go files already
//     own it. Together the two say: doc 02 <-> Go constant <-> schema default.
//     Neither subsumes the other, and a knob with no SQL column (StrengthenGain,
//     ResurfaceMaxHops) has only this one.
//
// # It reads the number, it does not search for one
//
// calibrationLeadingNumber is anchored at the start of the Default cell, so the
// cell must *begin* with its number. An unanchored search would happily read
// "was 30 under ruling 9" and pin 30, or read the 7 in "7±2" and be right by
// luck. Anchoring converts an entire class of silent mispinning into a loud
// parse failure, at the cost of one editorial constraint on §13: the Default
// column leads with the value, and the prose follows after an em dash. Doc 02's
// belief-merge row was reworded in this commit to satisfy it (0.85 unchanged).
//
// # Vacuous-pass guards
//
// A missing document, a missing §13, an empty table, a row that names a symbol
// but no readable number, and a symbol count below the floor are all failures,
// not skips. This test has no path on which it succeeds without having compared
// at least calibrationMinSymbols numbers.
//
// L2 rather than L1: it reads two documents and a package tree off disk, and
// depguard denies os to internal/core/** with no $test selector.
func TestHarness_CalibrationTableMatchesConstants(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	knobs := parseCalibrationTable(t, repoRoot)

	if len(knobs) < calibrationMinSymbols {
		t.Fatalf("doc 02 §13 yielded %d rows naming an internal/core constant, but %d were "+
			"present when this gate was written. A row may legitimately have been removed — "+
			"if so, lower calibrationMinSymbols in the same commit that removes it. Far more "+
			"likely the table's shape changed and the parser now reads nothing, which would "+
			"make this gate pass while checking nothing.",
			len(knobs), calibrationMinSymbols)
	}

	fset := token.NewFileSet()
	modulePath := moduleImportPath(t, repoRoot)
	imp := newModuleImporter(fset, repoRoot, modulePath)

	for _, knob := range knobs {
		t.Run(knob.constName, func(t *testing.T) {
			pkg, err := imp.Import(modulePath + "/" + knob.pkgRel)
			if err != nil {
				t.Fatalf("doc 02 §13 names %s.%s, but %s does not type-check: %v\nrow: %s",
					knob.pkgRel, knob.constName, knob.pkgRel, err, knob.row)
			}

			obj := pkg.Scope().Lookup(knob.constName)
			if obj == nil {
				t.Fatalf("doc 02 §13 names %s.%s, but %s exports no such symbol. Doc 02 "+
					"governs: either the constant was renamed and the row must follow, or "+
					"the row is aspirational and does not belong in the table yet.\nrow: %s",
					knob.pkgRel, knob.constName, knob.pkgRel, knob.row)
			}

			konst, ok := obj.(*types.Const)
			if !ok {
				t.Fatalf("doc 02 §13 names %s.%s as a calibratable, but it is a %T, not a "+
					"constant. docs/06-harness.md §7 requires every §13 number to be a named "+
					"constant in exactly one place.\nrow: %s",
					knob.pkgRel, knob.constName, obj, knob.row)
			}

			want := calibrationLiteral(t, knob)
			if !constant.Compare(konst.Val(), token.EQL, want) {
				t.Errorf("%s.%s = %s, but doc 02 §13 documents %s.\n"+
					"docs/02-cognitive-core.md governs behavior: fix the constant, or amend "+
					"§13 and its ADR in the same PR. Two numbers for one knob is the drift "+
					"this gate exists to stop.\nrow: %s",
					knob.pkgRel, knob.constName, konst.Val(), knob.docValue, knob.row)
			}

			for _, resolver := range knob.resolvers {
				fn, found := pkg.Scope().Lookup(resolver).(*types.Func)
				if !found || fn == nil {
					t.Errorf("doc 02 §13 names %s alongside %s, but %s exports no such "+
						"function. A resolver named in the table and absent from the code is "+
						"a claim wider than the code.\nrow: %s",
						resolver, knob.constName, knob.pkgRel, knob.row)
				}
			}
		})
	}
}

// parseCalibrationTable reads §13 out of doc 02 and returns one calibrationKnob
// per row that names a core symbol, sorted by symbol for a stable subtest order.
func parseCalibrationTable(t *testing.T, repoRoot string) []calibrationKnob {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(repoRoot, calibrationDocPath))
	if err != nil {
		t.Fatalf("read %s: %v — this gate cannot pass without the document it enforces",
			calibrationDocPath, err)
	}

	section, ok := calibrationSection(string(data))
	if !ok {
		t.Fatalf("%s has no %q heading. The section this gate reads is gone or renamed, "+
			"which is a failure and not a skip.", calibrationDocPath, calibrationHeading)
	}

	var knobs []calibrationKnob
	seen := make(map[string]string)

	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}

		cells := splitMarkdownRow(line)
		if len(cells) < 2 {
			continue
		}
		knobCell, defaultCell := cells[0], cells[1]

		refs := calibrationQualifiedRef.FindAllStringSubmatch(knobCell, -1)
		if len(refs) == 0 {
			continue // header, separator, or a knob with no constant yet
		}
		if len(refs) > 1 {
			t.Errorf("doc 02 §13 row names %d core constants; this gate reads one number "+
				"per row and cannot tell which of them the Default column belongs to. Split "+
				"the row.\nrow: %s", len(refs), line)
			continue
		}

		knob := calibrationKnob{
			pkgRel:    "internal/core/" + refs[0][1],
			constName: refs[0][2],
			docValue:  calibrationLeadingNumber.FindString(defaultCell),
			row:       line,
		}
		for _, m := range calibrationResolverRef.FindAllStringSubmatch(knobCell, -1) {
			knob.resolvers = append(knob.resolvers, m[1])
		}

		if knob.docValue == "" {
			t.Errorf("doc 02 §13 names %s.%s but its Default column does not begin with a "+
				"number (%q). §13's Default column leads with the value; prose follows after "+
				"an em dash.\nrow: %s", knob.pkgRel, knob.constName, defaultCell, line)
			continue
		}

		key := knob.pkgRel + "." + knob.constName
		if prior, dup := seen[key]; dup {
			t.Errorf("doc 02 §13 names %s on two rows, so one of them is unenforced by "+
				"construction — a knob has one home.\nfirst:  %s\nsecond: %s", key, prior, line)
			continue
		}
		seen[key] = line

		knobs = append(knobs, knob)
	}

	sort.Slice(knobs, func(i, j int) bool {
		return knobs[i].pkgRel+knobs[i].constName < knobs[j].pkgRel+knobs[j].constName
	})
	return knobs
}

// calibrationSection returns the text between §13's heading and the next
// top-level heading, or ok=false if the heading is absent.
func calibrationSection(doc string) (string, bool) {
	start := strings.Index(doc, calibrationHeading)
	if start < 0 {
		return "", false
	}

	rest := doc[start+len(calibrationHeading):]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		return rest[:end], true
	}
	return rest, true
}

// splitMarkdownRow splits a pipe-delimited table row into its trimmed cells,
// dropping the empty fragments the leading and trailing pipes produce.
func splitMarkdownRow(line string) []string {
	parts := strings.Split(strings.Trim(line, "|"), "|")
	cells := make([]string, 0, len(parts))
	for _, p := range parts {
		cells = append(cells, strings.TrimSpace(p))
	}
	return cells
}

// calibrationLiteral turns the documented literal into an exact go/constant
// value. INT and FLOAT are dispatched by shape because go/constant rejects "2"
// as a FLOAT literal and "2.0" as an INT one; the resulting values compare
// across kinds, so 2 and 2.0 remain the same number.
func calibrationLiteral(t *testing.T, knob calibrationKnob) constant.Value {
	t.Helper()

	kind := token.INT
	if strings.ContainsAny(knob.docValue, ".eE") {
		kind = token.FLOAT
	}

	v := constant.MakeFromLiteral(knob.docValue, kind, 0)
	if v.Kind() == constant.Unknown {
		t.Fatalf("doc 02 §13 documents %s.%s as %q, which is not a Go numeric literal.\nrow: %s",
			knob.pkgRel, knob.constName, knob.docValue, knob.row)
	}
	return v
}
