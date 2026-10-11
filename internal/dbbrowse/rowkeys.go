package dbbrowse

import (
	"strconv"
)

// This file exists because of a precision trap that no client-side fix can
// reach: a row key is delivered to the browser as JSON, and `JSON.parse` turns
// every integer into a float64. Past 2^53 two adjacent ids collapse into the
// same number, so an edit or delete built from the grid's cells can silently
// address the NEIGHBOURING row.
//
// Snowflake-style ids (and any user-assigned 64-bit id) routinely exceed 2^53,
// so this is not hypothetical. The fix is to hand the client the key as an exact
// DECIMAL STRING, which it returns unchanged; SQLite's type affinity converts a
// numeric string back to an integer for the comparison, so no server-side
// coercion is needed (verified in rowkeys_test.go against a real database).

// ExactValue renders one driver value as an exact string, or nil for SQL NULL.
//
// NULL cannot become "": the key predicate for a nil value is `IS NULL`, while
// an empty string is an equality, so flattening the two would address nothing.
func ExactValue(v any) *string {
	switch t := v.(type) {
	case nil:
		return nil
	case int64:
		s := strconv.FormatInt(t, 10)
		return &s
	case float64:
		// -1 precision and 'g' keep the shortest representation that round-trips.
		s := strconv.FormatFloat(t, 'g', -1, 64)
		return &s
	case string:
		s := t
		return &s
	case bool:
		// SQLite has no boolean type; the driver surfaces booleans as 0/1.
		s := "0"
		if t {
			s = "1"
		}
		return &s
	default:
		// A driver type this function cannot express exactly (a BLOB key
		// component, say). Reporting it would mean inventing a representation
		// that might not match the stored value, so the caller is told there is
		// no exact key instead.
		return nil
	}
}

// RowKeys returns one exact key per row of a page, index-aligned with
// page.Rows. A nil entry means that row has no expressible key and the caller
// must fall back to its previous (cell-derived) behaviour — never a partial key,
// which could address a different row.
//
// The keys are built from the page's OWN columns, so a table addressed by its
// synthetic rowid column works without the caller knowing which case it is in.
func RowKeys(schema TableSchema, page ResultSet) []map[string]*string {
	keyCols, ok := schema.KeyColumns()
	if !ok {
		return nil
	}
	// A key column that is not in the projection cannot be read. That happens
	// when IncludeRowID was not requested for a rowid table, in which case the
	// caller has no key to offer.
	index := make(map[string]int, len(page.Columns))
	for i, c := range page.Columns {
		index[c.Name] = i
	}
	for _, name := range keyCols {
		if _, present := index[name]; !present {
			return nil
		}
	}

	out := make([]map[string]*string, len(page.Rows))
	for ri, row := range page.Rows {
		key := make(map[string]*string, len(keyCols))
		expressible := true
		for _, name := range keyCols {
			ci := index[name]
			if ci >= len(row) {
				expressible = false
				break
			}
			// A non-nil value that ExactValue refuses (an unexpressible type)
			// makes the WHOLE row's key unusable: a partial key is worse than
			// none, because it could match a different row.
			v := row[ci]
			s := ExactValue(v)
			if s == nil && v != nil {
				expressible = false
				break
			}
			key[name] = s
		}
		if expressible {
			out[ri] = key
		}
	}
	return out
}
