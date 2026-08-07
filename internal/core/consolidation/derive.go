package consolidation

import (
	"github.com/rengo/nooma/internal/core/selfmodel"
)

// DeriveTopicKey renders doc 02 §10's derived belief key format (spec
// R4.6): "derived/{facet}/{key}".
//
// Stub (RED, task 4.16): returns "" so the exact-string assertion fails
// first.
func DeriveTopicKey(f selfmodel.Facet, key string) string {
	return ""
}
