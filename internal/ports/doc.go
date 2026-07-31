// Package ports declares the interfaces the cognitive core and the brain depend on.
//
// Everything the core cannot do for itself — reading time, generating identifiers,
// reaching storage, calling a model, speaking to a channel — enters through a port
// declared here and is implemented by an adapter outside internal/core.
//
// See docs/06-harness.md §1 for the dependency rule and §2 for the clock.
package ports
