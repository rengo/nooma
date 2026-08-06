// Package xpkg is a second, throwaway package under testdata/, existing only
// so fixture.go can prove Judgment Day round 5's fix resolves a NAMED
// constraint through a *types.Named term regardless of which package
// declares it — go/types represents a *types.Named identically whether it
// comes from the same package as the type parameter using it or a different
// one, so nothing in appendConstraintTerm (test/conformance/
// i01_focus_never_persisted_test.go) special-cases package boundaries; this
// package exists to make that claim checked rather than merely asserted.
//
// It lives under testdata/ for the same reason fixture.go's own package
// does: Go's own testdata convention keeps it out of `go build ./...`, `go
// vet ./...`, golangci-lint's package walk, and the cross-compile matrix,
// while still being real, parseable, type-checked Go source.
package xpkg

import "github.com/rengo/nooma/internal/core/unit"

// Constraint restricts a type parameter to exactly unit.Status, declared in
// this package specifically so fixture.go's own
// ReturnsCrossPackageNamedTypeParam references a *types.Named whose home
// package differs from the type parameter using it.
type Constraint interface{ unit.Status }
