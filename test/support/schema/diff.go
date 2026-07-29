package schema

import "sort"

// DifferenceKind names which of design §6.5's three disagreement shapes a
// Difference reports.
type DifferenceKind string

const (
	// DiffMissingFromSchema: declared in doc 03 but absent from the golden.
	DiffMissingFromSchema DifferenceKind = "missing_from_schema"
	// DiffUndeclaredInDoc: present in the golden but not declared in doc 03
	// — this is the assertion that forces the FTS trigger DDL into doc 03
	// (proposal §4.2, R9.2).
	DiffUndeclaredInDoc DifferenceKind = "undeclared_in_doc"
	// DiffColumnMismatch: a table or virtual_table present on both sides
	// whose column sets differ.
	DiffColumnMismatch DifferenceKind = "column_mismatch"
	// DiffDuplicateInDoc: docs/03-data-model.md declares the same (Kind,
	// Name) object more than once. Reported explicitly instead of silently
	// keeping only the last declaration and discarding all evidence that
	// the two disagreed (four-lens pre-PR review finding 2) — a duplicate
	// column-set comparison against golden is skipped for that key, because
	// which of the doc's own conflicting declarations to compare would be
	// an arbitrary choice.
	DiffDuplicateInDoc DifferenceKind = "duplicate_in_doc"
)

// Difference is one disagreement Diff found between docs/03-data-model.md's
// declared objects and the schema golden (design §6.5). OnlyInDoc and
// OnlyInSchema are populated only for DiffColumnMismatch.
type Difference struct {
	DiffKind     DifferenceKind
	Kind         Kind
	Name         string
	OnlyInDoc    []string
	OnlyInSchema []string
}

// objectKey identifies an Object by (Kind, Name) — the pair design §6.5
// says must match ("every object declared in doc 03 exists in the golden,
// with the same kind").
type objectKey struct {
	Kind Kind
	Name string
}

// Diff compares doc (ParseMarkdown's output) against golden (ParseGolden's
// output), asserting design §6.5's three comparisons plus one guard against
// ambiguous input (four-lens pre-PR review finding 2):
//
//   - doc declares no (Kind, Name) object more than once (DiffDuplicateInDoc
//     otherwise) — a doc-authoring mistake that would otherwise let one
//     declaration silently overwrite the other in docByKey below, hiding
//     the very disagreement this gate exists to surface;
//   - every object declared in doc 03 exists in the golden, with the same
//     kind (DiffMissingFromSchema otherwise);
//   - every object in the golden is declared in doc 03 (DiffUndeclaredInDoc
//     otherwise);
//   - for table and virtual_table objects present on both sides, the
//     column SETS are equal (DiffColumnMismatch otherwise, both directions).
//     Skipped for a duplicate key: comparing one arbitrary survivor's
//     columns would only add a second, confusing report about the same
//     underlying problem DiffDuplicateInDoc already names.
//
// It deliberately never compares types, NOT NULL, defaults, FK clauses,
// index columns, index predicates or trigger bodies (design §6.5's
// explicit non-assertion list) — all of that is ddl.golden's job, reviewed
// as a diff. Diff never mutates doc or golden. The returned slice is
// sorted deterministically (DiffKind, then kind rank, then name) so a
// caller's rendered report never depends on Go's randomized map iteration
// order.
func Diff(doc, golden []Object) []Difference {
	docByKey := make(map[objectKey]Object, len(doc))
	duplicateKeys := make(map[objectKey]bool)
	for _, o := range doc {
		key := objectKey{o.Kind, o.Name}
		if _, seen := docByKey[key]; seen {
			duplicateKeys[key] = true
		}
		docByKey[key] = o
	}
	goldenByKey := make(map[objectKey]Object, len(golden))
	for _, o := range golden {
		goldenByKey[objectKey{o.Kind, o.Name}] = o
	}

	var diffs []Difference

	for key := range duplicateKeys {
		diffs = append(diffs, Difference{DiffKind: DiffDuplicateInDoc, Kind: key.Kind, Name: key.Name})
	}
	for _, o := range doc {
		if _, ok := goldenByKey[objectKey{o.Kind, o.Name}]; !ok {
			diffs = append(diffs, Difference{DiffKind: DiffMissingFromSchema, Kind: o.Kind, Name: o.Name})
		}
	}
	for _, o := range golden {
		if _, ok := docByKey[objectKey{o.Kind, o.Name}]; !ok {
			diffs = append(diffs, Difference{DiffKind: DiffUndeclaredInDoc, Kind: o.Kind, Name: o.Name})
		}
	}
	for key, docObj := range docByKey {
		if docObj.Kind != KindTable && docObj.Kind != KindVirtualTable {
			continue
		}
		if duplicateKeys[key] {
			continue // already reported as DiffDuplicateInDoc — comparing
			// one arbitrary survivor's columns against golden would add a
			// second, confusing report about the same underlying problem.
		}
		goldenObj, ok := goldenByKey[key]
		if !ok {
			continue // already reported as DiffMissingFromSchema above
		}
		onlyInDoc, onlyInSchema := columnSetDiff(docObj.Columns, goldenObj.Columns)
		if len(onlyInDoc) > 0 || len(onlyInSchema) > 0 {
			diffs = append(diffs, Difference{
				DiffKind:     DiffColumnMismatch,
				Kind:         docObj.Kind,
				Name:         docObj.Name,
				OnlyInDoc:    onlyInDoc,
				OnlyInSchema: onlyInSchema,
			})
		}
	}

	sortDifferences(diffs)
	return diffs
}

// columnSetDiff returns the set difference in both directions, each sorted
// for a deterministic report.
func columnSetDiff(docCols, goldenCols []string) (onlyInDoc, onlyInSchema []string) {
	inGolden := make(map[string]bool, len(goldenCols))
	for _, c := range goldenCols {
		inGolden[c] = true
	}
	inDoc := make(map[string]bool, len(docCols))
	for _, c := range docCols {
		inDoc[c] = true
	}
	for _, c := range docCols {
		if !inGolden[c] {
			onlyInDoc = append(onlyInDoc, c)
		}
	}
	for _, c := range goldenCols {
		if !inDoc[c] {
			onlyInSchema = append(onlyInSchema, c)
		}
	}
	sort.Strings(onlyInDoc)
	sort.Strings(onlyInSchema)
	return onlyInDoc, onlyInSchema
}

// sortDifferences orders diffs by (DiffKind precedence, kind rank, name) so
// Diff's output never depends on map iteration order.
func sortDifferences(diffs []Difference) {
	sort.Slice(diffs, func(i, j int) bool {
		if diffs[i].DiffKind != diffs[j].DiffKind {
			return diffKindOrder(diffs[i].DiffKind) < diffKindOrder(diffs[j].DiffKind)
		}
		if diffs[i].Kind != diffs[j].Kind {
			return Rank(diffs[i].Kind) < Rank(diffs[j].Kind)
		}
		return diffs[i].Name < diffs[j].Name
	})
}

func diffKindOrder(k DifferenceKind) int {
	switch k {
	case DiffDuplicateInDoc:
		return 0
	case DiffMissingFromSchema:
		return 1
	case DiffUndeclaredInDoc:
		return 2
	case DiffColumnMismatch:
		return 3
	default:
		return 4
	}
}
