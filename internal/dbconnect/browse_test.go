package dbconnect

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

var testCols = []Column{
	{Name: "id", Type: "bigint", PK: 1},
	{Name: "we\"ird", Type: "text"},
	{Name: "amount", Type: "numeric(10,2)"},
}

var noPKCols = []Column{{Name: "a", Type: "integer"}, {Name: "b", Type: "text"}}

func TestBuildBrowseSQLDefaultsToPrimaryKeyOrder(t *testing.T) {
	got, err := BuildBrowseSQL("t", testCols, "", false, "", 101, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := `SELECT * FROM "t" ORDER BY "id" LIMIT 101 OFFSET 0`
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestBuildBrowseSQLNoPrimaryKeyHasNoOrder(t *testing.T) {
	got, err := BuildBrowseSQL("t", noPKCols, "", false, "", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "ORDER BY") {
		t.Fatalf("no-PK table got an ORDER BY: %q", got)
	}
}

func TestBuildBrowseSQLSortAndDirection(t *testing.T) {
	asc, err := BuildBrowseSQL("t", testCols, "amount", false, "", 10, 20)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(asc, `ORDER BY "amount" ASC LIMIT 10 OFFSET 20`) {
		t.Fatalf("asc = %q", asc)
	}
	desc, err := BuildBrowseSQL("t", testCols, `we"ird`, true, "", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(desc, `ORDER BY "we""ird" DESC`) {
		t.Fatalf("desc with quoted column = %q", desc)
	}
}

func TestBuildBrowseSQLRejectsUnknownSortColumn(t *testing.T) {
	// A column that is not in the table is refused even though it could be quoted.
	_, err := BuildBrowseSQL("t", testCols, "id; DROP TABLE t", false, "", 10, 0)
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unknown sort column err = %v, want ErrInvalidInput", err)
	}
}

func TestBuildBrowseSQLFilterStaysInsideParentheses(t *testing.T) {
	cases := []struct {
		name   string
		filter string
	}{
		{"simple", "amount > 10"},
		{"trailing line comment", "true) --"},
		{"closing paren and new statement", "1=1); DELETE FROM t"},
		{"unterminated block comment", "1=1 /*"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := BuildBrowseSQL("t", testCols, "", false, c.filter, 10, 0)
			if err != nil {
				t.Fatal(err)
			}
			// The filter is on its own line, so a trailing "--" ends at the line break
			// and the closing paren and ORDER BY/LIMIT still belong to the statement.
			if !strings.Contains(got, "WHERE (\n"+c.filter+"\n)") {
				t.Fatalf("filter not isolated: %q", got)
			}
			if !strings.HasSuffix(got, `ORDER BY "id" LIMIT 10 OFFSET 0`) {
				t.Fatalf("clause after filter lost: %q", got)
			}
		})
	}
}

func TestBuildBrowseSQLBlankFilterAddsNoWhere(t *testing.T) {
	got, err := BuildBrowseSQL("t", testCols, "", false, "   ", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "WHERE") {
		t.Fatalf("blank filter added WHERE: %q", got)
	}
}

func TestBuildBrowseSQLRejectsBadPaging(t *testing.T) {
	if _, err := BuildBrowseSQL("t", testCols, "", false, "", 0, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("limit 0 err = %v", err)
	}
	if _, err := BuildBrowseSQL("t", testCols, "", false, "", 10, -1); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("offset -1 err = %v", err)
	}
}

func TestBuildInsertSQLBindsValuesWithCatalogCasts(t *testing.T) {
	got, err := BuildInsertSQL("t", testCols, map[string]any{
		"id":     json.Number("9007199254740993"),
		"amount": "12.34",
		`we"ird`: nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantStmt := `INSERT INTO "t" ("amount", "id", "we""ird") VALUES ($1::numeric(10,2), $2::bigint, $3::text)`
	if got.stmt != wantStmt {
		t.Fatalf("stmt = %q\nwant %q", got.stmt, wantStmt)
	}
	// The bigint stays exact: it is bound as its digits, not a JSON float.
	if want := []any{"12.34", "9007199254740993", nil}; !reflect.DeepEqual(got.args, want) {
		t.Fatalf("args = %#v, want %#v", got.args, want)
	}
}

func TestBuildInsertSQLRejectsUnknownColumn(t *testing.T) {
	_, err := BuildInsertSQL("t", testCols, map[string]any{"nope": "x"})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unknown column err = %v", err)
	}
}

func TestBuildUpdateSQLNumbersKeyParamsAfterSetParams(t *testing.T) {
	got, err := BuildUpdateSQL("t", testCols,
		map[string]any{"id": json.Number("9007199254740993")},
		map[string]any{"amount": "1.00"})
	if err != nil {
		t.Fatal(err)
	}
	wantStmt := `UPDATE "t" SET "amount" = $1::numeric(10,2) WHERE "id" = $2::bigint`
	if got.stmt != wantStmt {
		t.Fatalf("stmt = %q\nwant %q", got.stmt, wantStmt)
	}
	if want := []any{"1.00", "9007199254740993"}; !reflect.DeepEqual(got.args, want) {
		t.Fatalf("args = %#v, want %#v", got.args, want)
	}
}

func TestKeyedStatementsRequireTheExactPrimaryKey(t *testing.T) {
	cases := []struct {
		name string
		run  func() error
	}{
		{"update no primary key", func() error {
			_, err := BuildUpdateSQL("t", noPKCols, map[string]any{"a": json.Number("1")}, map[string]any{"b": "x"})
			return err
		}},
		{"delete no primary key", func() error {
			_, err := BuildDeleteSQL("t", noPKCols, map[string]any{"a": json.Number("1")})
			return err
		}},
		{"partial key", func() error {
			_, err := BuildDeleteSQL("t", []Column{{Name: "a", Type: "int", PK: 1}, {Name: "b", Type: "int", PK: 2}},
				map[string]any{"a": json.Number("1")})
			return err
		}},
		{"null key", func() error {
			_, err := BuildDeleteSQL("t", testCols, map[string]any{"id": nil})
			return err
		}},
		{"key names a non-key column", func() error {
			_, err := BuildDeleteSQL("t", testCols, map[string]any{"amount": "1"})
			return err
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.run(); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("err = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestBuildDeleteSQLUsesPrimaryKey(t *testing.T) {
	got, err := BuildDeleteSQL("t", testCols, map[string]any{"id": json.Number("7")})
	if err != nil {
		t.Fatal(err)
	}
	if got.stmt != `DELETE FROM "t" WHERE "id" = $1::bigint` || !reflect.DeepEqual(got.args, []any{"7"}) {
		t.Fatalf("delete = %q %#v", got.stmt, got.args)
	}
}

func TestParamTextKeepsValuesExact(t *testing.T) {
	cases := []struct {
		in   any
		want any
	}{
		{nil, nil},
		{"text", "text"},
		{true, "true"},
		{json.Number("12345678901234567890"), "12345678901234567890"},
		{map[string]any{"a": json.Number("1")}, `{"a":1}`},
		{[]any{"x", nil}, `["x",null]`},
	}
	for _, c := range cases {
		got, err := paramText(c.in)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Fatalf("paramText(%#v) = %#v, %v; want %#v", c.in, got, err, c.want)
		}
	}
	if _, err := paramText(float64(1)); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("float64 should be refused (use json.Number): %v", err)
	}
}
