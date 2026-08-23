// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// vendorMarkers are the names a channel port must never learn. The list is
// short on purpose: it names the vendors this repository has actually
// decided on (ADR-0006) rather than every messaging product, because a
// list nobody can complete is a list nobody maintains.
var vendorMarkers = []string{"telegram", "whatsapp", "slack", "discord"}

// TestChannelPortNamesNoVendor is R0's experiment, made checkable.
//
// docs/02-cognitive-core.md:653 claims: "Provenance is the caller's fact,
// never the brain's. Which channel a capture arrived through … travels
// inward as data. Nothing in the decision layer names a channel, so
// nothing has to be revisited when one is added."
//
// This chain adds the first real channel since that was written. If the
// claim is true, internal/ports can carry a channel contract without any
// identifier in it naming a vendor. If it is false, the boundary is in the
// wrong place and this test is where that shows.
//
// **It parses rather than greps, and the difference is load-bearing here.**
// ports.Channel's own doc comment legitimately names Telegram as the first
// implementation — that is documentation, not a leak. A byte scan cannot
// tell a doc comment from a type name, so it would either fail on correct
// code or be weakened until it proved nothing. This walks identifiers.
func TestChannelPortNamesNoVendor(t *testing.T) {
	root := repoRootFromCaller(t)
	dir := filepath.Join(root, "internal", "ports")

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", dir, err)
	}
	if len(pkgs) == 0 {
		t.Fatal("parsed zero packages under internal/ports — nothing to check yet")
	}

	scanned := 0
	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			scanned++
			ast.Inspect(file, func(n ast.Node) bool {
				ident, ok := n.(*ast.Ident)
				if !ok {
					return true
				}
				lower := strings.ToLower(ident.Name)
				for _, marker := range vendorMarkers {
					if strings.Contains(lower, marker) {
						rel, _ := filepath.Rel(root, path)
						t.Errorf("%s declares the identifier %s: internal/ports names no vendor, or doc 02's own claim that nothing above the adapter does is false",
							rel, ident.Name)
					}
				}
				return true
			})
		}
	}
	if scanned == 0 {
		t.Fatal("scanned zero files — nothing was checked")
	}
}

// TestBrainNamesNoChannel is the same experiment one layer in, and it is
// the half that would actually break: internal/brain is where a channel's
// reply rendering would land if someone put it in the wrong place.
//
// Its subject is the decision layer's independence, so it covers
// internal/brain and internal/core together — the two doc 02:653 speaks
// for. internal/channels is deliberately absent: naming Telegram is that
// package's whole job.
func TestBrainNamesNoChannel(t *testing.T) {
	root := repoRootFromCaller(t)

	for _, rel := range []string{
		filepath.Join("internal", "brain"),
		filepath.Join("internal", "core"),
	} {
		t.Run(rel, func(t *testing.T) {
			scanned := scanTreeForVendorIdentifiers(t, root, filepath.Join(root, rel))
			if scanned == 0 {
				t.Fatalf("scanned zero files under %s — nothing was checked", rel)
			}
		})
	}
}

// scanTreeForVendorIdentifiers walks every .go file under dir and reports
// how many it read, failing t for each vendor-named identifier.
func scanTreeForVendorIdentifiers(t *testing.T, root, dir string) int {
	t.Helper()

	fset := token.NewFileSet()
	scanned := 0

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		scanned++

		ast.Inspect(file, func(n ast.Node) bool {
			ident, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			lower := strings.ToLower(ident.Name)
			for _, marker := range vendorMarkers {
				if strings.Contains(lower, marker) {
					rel, _ := filepath.Rel(root, path)
					t.Errorf("%s declares the identifier %s: the decision layer names no channel (doc 02:653)", rel, ident.Name)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", dir, err)
	}
	return scanned
}
