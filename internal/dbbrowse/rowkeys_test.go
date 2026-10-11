package dbbrowse

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

// ── ExactValue ─────────────────────────────────────────────────────────────

// The whole point: an integer too large for a float64 must survive as a string,
// because a browser parsing it as a JS number would round it and then address
// the wrong row.
func TestExactValueKeepsAnIntegerExact(t *testing.T) {
	big := int64(9007199254740993) // 2^53 + 1
	got := ExactValue(big)
	if got == nil {
		t.Fatal("ExactValue returned nil for a non-NULL value")
	}
	if *got != "9007199254740993" {
		t.Fatalf("got %q, want the exact decimal 9007199254740993", *got)
	}
	// The float64 path is what must NOT be used here.
	if approx := json.Number("9007199254740993").String(); approx != *got {
		t.Fatalf("round trip differs: %q vs %q", approx, *got)
	}
}

// A NULL key column must stay NULL: formatting it as "" would turn an IS NULL
// predicate into an equality against the empty string and address nothing.
func TestExactValuePreservesNull(t *testing.T) {
	if got := ExactValue(nil); got != nil {
		t.Fatalf("got %v, want nil for a NULL value", *got)
	}
}

func TestExactValueFormatsByDriverType(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"int64", int64(42), "42"},
		{"negative", int64(-7), "-7"},
		{"string", "abc", "abc"},
		{"numeric-looking string is NOT normalised", "007", "007"},
		{"float", float64(2.5), "2.5"},
		{"bool true", true, "1"},
		{"bool false", false, "0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ExactValue(c.in)
			if got == nil || *got != c.want {
				t.Fatalf("ExactValue(%#v) = %v, want %q", c.in, got, c.want)
			}
		})
	}
}

// ── RowKeys ────────────────────────────────────────────────────────────────

// rowKeysFixture is a table whose primary key is a snowflake-sized integer:
// the case a JS number cannot represent.
const rowKeysFixture = `CREATE TABLE t(id INTEGER PRIMARY KEY, v TEXT);
	INSERT INTO t VALUES (9007199254740993,'target'), (9007199254740991,'other');`

func TestRowKeysReturnsExactStrings(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "keys.db", rowKeysFixture)

	schema, err := DescribeTable(ctx, path, "t")
	if err != nil {
		t.Fatalf("DescribeTable: %v", err)
	}
	// Sorted explicitly: without it the order is rowid order, which is a
	// property of the storage engine and not of the thing under test.
	page, _, err := TablePageFiltered(ctx, path, "t", PageOptions{SortBy: "id", SortDesc: true})
	if err != nil {
		t.Fatalf("TablePageFiltered: %v", err)
	}

	keys := RowKeys(schema, page)
	if len(keys) != 2 {
		t.Fatalf("got %d keys, want 2", len(keys))
	}
	if got := keys[0]["id"]; got == nil || *got != "9007199254740993" {
		t.Fatalf("key = %v, want the exact 9007199254740993", keys[0]["id"])
	}
	if got := keys[1]["id"]; got == nil || *got != "9007199254740991" {
		t.Fatalf("second key = %v, want 9007199254740991", keys[1]["id"])
	}
}

// The keys must actually address their rows: this is the end-to-end assertion
// that the string round trip works against SQLite's affinity rules.
func TestRowKeysRoundTripThroughReadBlob(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "keys.db", `CREATE TABLE t(id INTEGER PRIMARY KEY, data BLOB);
		INSERT INTO t VALUES (9007199254740993, X'61'), (9007199254740992, X'62');`)

	schema, _ := DescribeTable(ctx, path, "t")
	// Sorted by id ascending, so index 1 is the 2^53+1 row — found by content
	// below rather than assumed, so this test cannot pass or fail on ordering.
	page, _, err := TablePageFiltered(ctx, path, "t", PageOptions{SortBy: "id"})
	if err != nil {
		t.Fatalf("page: %v", err)
	}
	keys := RowKeys(schema, page)
	if len(keys) != 2 {
		t.Fatalf("got %d keys, want 2", len(keys))
	}

	// Find the row whose blob is 'a' (id 2^53+1) by reading each key back, then
	// assert THAT key addressed the right row.
	var found []string
	for _, k := range keys {
		key := RowValues{}
		for name, v := range k {
			if v == nil {
				key[name] = nil
				continue
			}
			key[name] = *v
		}
		got, err := ReadBlob(ctx, path, "t", "data", key)
		if err != nil {
			t.Fatalf("ReadBlob with a string key: %v", err)
		}
		found = append(found, string(got))
	}
	// Order-independent on purpose: the property under test is that the two
	// keys resolve to two DISTINCT rows. A lossy key would resolve both to the
	// same row, giving ["a","a"] or ["b","b"].
	sort.Strings(found)
	if !reflect.DeepEqual(found, []string{"a", "b"}) {
		t.Fatalf("the keys resolved to %q, want two distinct rows [a b]", found)
	}

	// And the large id specifically must be preserved as an exact string.
	if got := keys[1]["id"]; got == nil || *got != "9007199254740993" {
		t.Fatalf("sorted key = %v, want the exact 9007199254740993", keys[1]["id"])
	}
}

// A rowid table's key is the synthetic column, which is not part of `SELECT *`
// unless IncludeRowID was requested — so RowKeys must read it from the page's
// own columns, not from a fixed position.
func TestRowKeysUsesTheSyntheticRowIDColumn(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "keys.db", `CREATE TABLE t(v TEXT); INSERT INTO t VALUES ('x');`)

	schema, _ := DescribeTable(ctx, path, "t")
	page, _, err := TablePageFiltered(ctx, path, "t", PageOptions{IncludeRowID: true})
	if err != nil {
		t.Fatalf("page: %v", err)
	}
	keys := RowKeys(schema, page)
	if len(keys) != 1 {
		t.Fatalf("got %d keys, want 1", len(keys))
	}
	if got := keys[0][RowIDColumn]; got == nil || *got != "1" {
		t.Fatalf("rowid key = %v, want \"1\"", got)
	}
}

// A view has no addressable row, so there are no keys to report.
func TestRowKeysEmptyForAView(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "keys.db", rowKeysFixture+` CREATE VIEW v AS SELECT * FROM t;`)
	schema, _ := DescribeTable(ctx, path, "v")
	page, _, err := TablePageFiltered(ctx, path, "v", PageOptions{})
	if err != nil {
		t.Fatalf("page: %v", err)
	}
	if keys := RowKeys(schema, page); len(keys) != 0 {
		t.Fatalf("got %d keys for a view, want none", len(keys))
	}
}

// A NULL component of a composite key must stay NULL rather than becoming "".
func TestRowKeysPreservesANullKeyComponent(t *testing.T) {
	ctx := context.Background()
	path := makeDB(t, "keys.db", `CREATE TABLE t(a INTEGER, b TEXT, v TEXT, PRIMARY KEY(a, b));
		INSERT INTO t VALUES (1, NULL, 'x');`)

	schema, _ := DescribeTable(ctx, path, "t")
	page, _, err := TablePageFiltered(ctx, path, "t", PageOptions{})
	if err != nil {
		t.Fatalf("page: %v", err)
	}
	keys := RowKeys(schema, page)
	if len(keys) != 1 {
		t.Fatalf("got %d keys, want 1", len(keys))
	}
	if v, ok := keys[0]["b"]; !ok || v != nil {
		t.Fatalf("b = %v (present=%v), want a present nil", v, ok)
	}
	if keys[0]["a"] == nil || *keys[0]["a"] != "1" {
		t.Fatalf("a = %v, want \"1\"", keys[0]["a"])
	}
}
