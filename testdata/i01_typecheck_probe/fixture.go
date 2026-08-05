// Package i01typecheckprobe is a permanent regression fixture for
// TestI01_FocusIsNeverAPersistedStatus's type-checked "no exported function
// returns or embeds a unit.Status" check
// (test/conformance/i01_focus_never_persisted_test.go).
//
// It lives under testdata/ so `go build ./...`, `go vet ./...`,
// golangci-lint's own package walk, and the cross-compile matrix all skip it
// by Go's own testdata convention (docs/06-harness.md §5, the same
// convention testdata/{recall,classify,llm} already rely on) — it is never
// part of the real build. TestI01TypecheckFixture_KnownShapes parses and
// type-checks it directly, by path, the same way the structural subtest
// parses and type-checks internal/core/focus itself, and pins which of its
// exported functions must be flagged and which must not.
//
// Every shape here is deliberately synthetic and exists only so a future
// refactor of typeCanYieldWithoutConversion cannot silently regress on the
// exact shapes Judgment Day round 3 found missing (the entire category of
// defined slice/map/pointer/channel/func/interface types over unit.Status),
// on cycle termination, or on the one shape that must stay unflagged forever
// (a defined type declared without `=`).
package i01typecheckprobe

import "github.com/rengo/nooma/internal/core/unit"

// ReturnsDirect is the base case a type-checked pass has always had to get
// right: a direct unit.Status result.
func ReturnsDirect() unit.Status { var z unit.Status; return z }

// CaughtSlice, CaughtMap, CaughtPointer, CaughtChan, CaughtFunc and
// CaughtInterface are exactly Judgment Day round 3's finding: a defined type
// whose underlying kind is a container, function, or interface, over
// unit.Status — no wrapper struct needed to extract the real type, and the
// go/ast-text check that preceded this one indexed none of them.
type (
	CaughtSlice     []unit.Status
	CaughtMap       map[string]unit.Status
	CaughtPointer   *unit.Status
	CaughtChan      chan unit.Status
	CaughtFunc      func() unit.Status
	CaughtInterface interface{ Get() unit.Status }
)

func ReturnsCaughtSlice() CaughtSlice         { return nil }
func ReturnsCaughtMap() CaughtMap             { return nil }
func ReturnsCaughtPointer() CaughtPointer     { return nil }
func ReturnsCaughtChan() CaughtChan           { return nil }
func ReturnsCaughtFunc() CaughtFunc           { return nil }
func ReturnsCaughtInterface() CaughtInterface { return nil }

// CaughtStructField wraps one of the above in a struct field, proving the
// container/func/interface categories are still caught when they are not
// the return type directly but a field of one.
type CaughtStructField struct {
	S CaughtSlice
	M CaughtMap
	F CaughtFunc
	I CaughtInterface
}

func ReturnsCaughtStructField() CaughtStructField { return CaughtStructField{} }

// NotFlagged is Go's own distinct-type rule: a defined type WITHOUT `=`
// needs an explicit conversion to or from unit.Status, so it must never be
// flagged even though its underlying type is identical.
type NotFlagged unit.Status

func ReturnsNotFlagged() NotFlagged { var z NotFlagged; return z }

// aliasedWrapper and AliasOfWrapper pin a real, empirically-found gap in
// this check's first draft: Go 1.22+ can materialize `type X = Y` as its
// own *types.Alias node instead of resolving X directly to Y's own type
// object. types.Identical already accounts for that when Y IS unit.Status
// directly (an alias of unit.Status itself was caught from the very first
// version), but an alias of something ELSE that merely embeds unit.Status —
// a wrapper struct, here — was not: without calling types.Unalias before
// dispatching on the type's own kind, t stayed a *types.Alias no switch
// case ever unwrapped, and ReturnsAliasOfWrapper passed the check green
// during this round's own probing. Kept here so that exact regression
// cannot silently return.
type aliasedWrapper struct{ S unit.Status }

type AliasOfWrapper = aliasedWrapper

func ReturnsAliasOfWrapper() AliasOfWrapper { return AliasOfWrapper{} }

// selfReferential has a pointer to its own type and no unit.Status anywhere
// in it — the check must terminate and report false, not loop forever.
type selfReferential struct {
	Next *selfReferential
}

func ReturnsSelfReferentialNoStatus() selfReferential { return selfReferential{} }

// mutuallyA and mutuallyB reference each other AND mutuallyB carries a real
// unit.Status field — the check must still terminate (the cycle does not
// prevent completion) and must still report true (the cycle does not hide
// the real field either).
type mutuallyA struct {
	B *mutuallyB
}

type mutuallyB struct {
	A *mutuallyA
	S unit.Status
}

func ReturnsMutuallyRecursiveWithStatus() mutuallyA { return mutuallyA{} }

// GenericWrapper is a generic container; instantiating it with unit.Status
// (directly, or via a local alias of unit.Status) must be caught the same as
// a non-generic wrapper — go/types substitutes the type argument into the
// instantiated struct's own field types, so no special-casing is needed for
// this beyond the ordinary struct-field walk.
type GenericWrapper[T any] struct {
	V T
}

type localAliasOfStatus = unit.Status

func ReturnsGenericDirect() GenericWrapper[unit.Status] { return GenericWrapper[unit.Status]{} }
func ReturnsGenericViaAlias() GenericWrapper[localAliasOfStatus] {
	return GenericWrapper[localAliasOfStatus]{}
}

// ReturnsSecondOfTwo is a multiple-return-value shape where only the second
// result carries unit.Status — the check must inspect every result, not only
// the first.
func ReturnsSecondOfTwo() (int, unit.Status) { var z unit.Status; return 0, z }
