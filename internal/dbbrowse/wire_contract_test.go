package dbbrowse

import (
	"context"
	"encoding/json"
	"testing"
)

// assertJSONArrays marshals payload and asserts that the payload is a JSON
// array (when no keys are given) or that every listed top-level key holds one.
//
// It decodes into `any` on purpose: a nil Go slice comes back as nil (JSON
// null) while an empty slice comes back as a []any of length 0, which is
// exactly the distinction the wire contract turns on.
//
// Do NOT assert "no null anywhere in the payload": a SQL NULL cell is
// legitimately null, and so is a DDL-less view.
func assertJSONArrays(t *testing.T, label string, payload any, keys ...string) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("%s: marshal: %v", label, err)
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("%s: unmarshal %s: %v", label, raw, err)
	}
	if len(keys) == 0 {
		if _, isArray := decoded.([]any); !isArray {
			t.Errorf("%s: payload is %#v, want a JSON array (got %s)", label, decoded, raw)
		}
		return
	}
	object, isObject := decoded.(map[string]any)
	if !isObject {
		t.Fatalf("%s: payload is %#v, want a JSON object to check %v (got %s)", label, decoded, keys, raw)
	}
	for _, key := range keys {
		value, present := object[key]
		if !present {
			t.Errorf("%s: key %q missing from %s", label, key, raw)
			continue
		}
		if _, isArray := value.([]any); !isArray {
			t.Errorf("%s: %q is %#v, want a JSON array (got %s)", label, key, value, raw)
		}
	}
}

// TestWireArraysAreNeverNull pins the contract every /api/db/* client depends
// on: a list field is ALWAYS a JSON array, never null. encoding/json renders a
// nil Go slice as null, and the React preview dereferences these fields
// unguarded (`schema.foreign_keys.length`, `result.rows.map`), so a single nil
// slice crashes the whole preview pane instead of showing an empty section.
// This regressed as "null is not an object (evaluating 'e.foreign_keys.length')"
// on every table without a foreign key.
func TestWireArraysAreNeverNull(t *testing.T) {
	ctx := context.Background()

	t.Run("table with no indexes and no foreign keys", func(t *testing.T) {
		// INTEGER PRIMARY KEY is the rowid alias, so SQLite creates no index for
		// it: indexes AND foreign_keys are both legitimately empty here.
		path := makeDB(t, "bare.db", `CREATE TABLE t(x INTEGER PRIMARY KEY)`)
		schema, err := DescribeTable(ctx, path, "t")
		if err != nil {
			t.Fatalf("DescribeTable: %v", err)
		}
		assertJSONArrays(t, "schema", schema, "columns", "indexes", "foreign_keys")
	})

	// The `columns` and index-`columns` initializers are NOT mutation-covered by
	// the cases below, and cannot be: SQLite cannot produce a zero-column table
	// or an index whose pragma_index_info returns no rows, so those two lists
	// are unreachable while empty. They are still allocated so the "arrays are
	// never null" contract holds for the type rather than for today's callers.
	t.Run("every index reports its columns", func(t *testing.T) {
		path := makeDB(t, "idx.db", `CREATE TABLE t(a, b); CREATE INDEX i_t_a ON t(a);`)
		schema, err := DescribeTable(ctx, path, "t")
		if err != nil {
			t.Fatalf("DescribeTable: %v", err)
		}
		if len(schema.Indexes) != 1 {
			t.Fatalf("indexes = %+v, want exactly 1", schema.Indexes)
		}
		assertJSONArrays(t, "index", schema.Indexes[0], "columns")
	})

	t.Run("query returning zero rows", func(t *testing.T) {
		path := makeDB(t, "empty.db", `CREATE TABLE t(a);`)
		result, err := Query(ctx, path, `SELECT a FROM t`, 0)
		if err != nil {
			t.Fatalf("Query: %v", err)
		}
		assertJSONArrays(t, "result", result, "columns", "rows")
	})

	t.Run("table page of a table with no rows", func(t *testing.T) {
		path := makeDB(t, "paged.db", `CREATE TABLE t(a INTEGER PRIMARY KEY);`)
		page, err := TablePageKeyed(ctx, path, "t", 0, 0, false)
		if err != nil {
			t.Fatalf("TablePageKeyed: %v", err)
		}
		assertJSONArrays(t, "page", page, "columns", "rows")
	})

	t.Run("database with no tables", func(t *testing.T) {
		// Create then drop, so the file is a valid database whose sqlite_master
		// is empty (a 0-byte file is not a database at all).
		path := makeDB(t, "void.db", `CREATE TABLE gone(x); DROP TABLE gone;`)
		tables, err := ListTables(ctx, path)
		if err != nil {
			t.Fatalf("ListTables: %v", err)
		}
		assertJSONArrays(t, "tables", tables)
	})
}
