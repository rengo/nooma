// Package correction decides two things a correction needs, doc 02 §5 step
// 4 made executable: which unit a correction targets when the caller gave
// no explicit id (Referent, design D2), and what a correction writes
// (PlanEdit, design D3, lands in PR 12c). Both are pure: no LLM, no I/O, no
// clock — a package for the two decisions, not one function each.
//
// See docs/06-harness.md §1 for the dependency rule.
package correction
