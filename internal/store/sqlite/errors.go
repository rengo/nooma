package sqlite

import "fmt"

// VersionError reports a vault whose schema is newer than this binary
// supports (an old binary opening a newer vault). It is declared here
// ahead of its first use because the migration runner that returns it —
// having modified nothing, no transaction opened, nothing written (design
// §5.3, spec R3.6) — lands in PR 3 of this same change
// (openspec/changes/complete-harness). Open does not return it yet.
type VersionError struct {
	VaultVersion  int
	BinaryVersion int
}

func (e *VersionError) Error() string {
	return fmt.Sprintf(
		"vault is at schema version %d, this binary supports up to %d — upgrade nooma to open this vault (nothing was modified)",
		e.VaultVersion, e.BinaryVersion,
	)
}
