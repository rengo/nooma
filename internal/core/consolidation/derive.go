package consolidation

import (
	"github.com/rengo/nooma/internal/core/selfmodel"
)

// DeriveTopicKey renders doc 02 §10's derived belief key format (spec
// R4.6): "derived/{facet}/{key}".
func DeriveTopicKey(f selfmodel.Facet, key string) string {
	return "derived/" + string(f) + "/" + key
}
