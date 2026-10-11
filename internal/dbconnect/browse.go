package dbconnect

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ErrInvalidInput marks a request the caller got wrong: an unknown table, column,
// sort, or key shape. The handler maps it to 400.
var ErrInvalidInput = errors.New("invalid input")

// Column is one column of a public table. Type is the catalog's format_type
// output (for example "numeric(10,2)"); it is the cast applied to every text
// parameter bound to this column. PK is the 1-based position in the primary key,
// or 0 when the column is not part of it.
type Column struct {
	Name string `json:"name"`
	Type string `json:"type"`
	PK   int    `json:"pk"`
}

// TableColumns returns the columns of public.table in table order. An empty
// result means the table does not exist. PK is 1-based: pg_index.indkey is an
// int2vector whose subscripts start at 0, and array_position returns the
// subscript, so the key position is one more than it returns.
func TableColumns(ctx context.Context, db *sql.DB, table string) ([]Column, error) {
	rows, err := db.QueryContext(ctx, `
SELECT a.attname, format_type(a.atttypid, a.atttypmod),
       COALESCE(array_position(i.indkey::int2[], a.attnum) + 1, 0)
FROM pg_attribute a
JOIN pg_class c ON c.oid = a.attrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
LEFT JOIN pg_index i ON i.indrelid = c.oid AND i.indisprimary
WHERE n.nspname = 'public' AND c.relname = $1 AND a.attnum > 0 AND NOT a.attisdropped
ORDER BY a.attnum`, table)
	if err != nil {
		return nil, fmt.Errorf("table columns: %w", err)
	}
	defer rows.Close()
	cols := []Column{}
	for rows.Next() {
		var c Column
		if err := rows.Scan(&c.Name, &c.Type, &c.PK); err != nil {
			return nil, fmt.Errorf("table columns: scan: %w", err)
		}
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("table columns: rows: %w", err)
	}
	return cols, nil
}

// primaryKey returns the primary-key columns in key order. A table without a
// primary key returns nil.
func primaryKey(cols []Column) []Column {
	var pk []Column
	for _, c := range cols {
		if c.PK > 0 {
			pk = append(pk, c)
		}
	}
	sort.Slice(pk, func(i, j int) bool { return pk[i].PK < pk[j].PK })
	return pk
}

// PrimaryKey returns the names of the primary-key columns, in key order.
func PrimaryKey(cols []Column) []string {
	names := []string{}
	for _, c := range primaryKey(cols) {
		names = append(names, c.Name)
	}
	return names
}

// quoteIdent double-quotes an identifier, doubling any embedded quote. Callers
// must first check the name against the table's own columns.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// columnByName returns the catalog column with this name, or ErrInvalidInput.
func columnByName(cols []Column, name string) (Column, error) {
	for _, c := range cols {
		if c.Name == name {
			return c, nil
		}
	}
	return Column{}, fmt.Errorf("%w: no column %q", ErrInvalidInput, name)
}

// BuildBrowseSQL builds one page of a table. sortCol names a column to order by
// (empty orders by primary key, or not at all when there is none); desc flips
// the direction. filter is the only free-form text: it goes on its own line
// inside parentheses, so a trailing "--" comments out nothing that follows, and
// it runs read-only at the server (see QueryReadOnly). The result is a
// parameter-free statement: the page size and offset are ints.
func BuildBrowseSQL(table string, cols []Column, sortCol string, desc bool, filter string, limit, offset int) (string, error) {
	if limit < 1 || offset < 0 {
		return "", fmt.Errorf("%w: limit must be at least 1 and offset at least 0", ErrInvalidInput)
	}
	var b strings.Builder
	b.WriteString("SELECT * FROM ")
	b.WriteString(quoteIdent(table))
	if f := strings.TrimSpace(filter); f != "" {
		b.WriteString(" WHERE (\n")
		b.WriteString(f)
		b.WriteString("\n)")
	}
	switch {
	case sortCol != "":
		if _, err := columnByName(cols, sortCol); err != nil {
			return "", err
		}
		b.WriteString(" ORDER BY ")
		b.WriteString(quoteIdent(sortCol))
		if desc {
			b.WriteString(" DESC")
		} else {
			b.WriteString(" ASC")
		}
	default:
		if pk := primaryKey(cols); len(pk) > 0 {
			b.WriteString(" ORDER BY ")
			for i, c := range pk {
				if i > 0 {
					b.WriteString(", ")
				}
				b.WriteString(quoteIdent(c.Name))
			}
		}
	}
	b.WriteString(" LIMIT ")
	b.WriteString(strconv.Itoa(limit))
	b.WriteString(" OFFSET ")
	b.WriteString(strconv.Itoa(offset))
	return b.String(), nil
}

// paramText turns one JSON value from a row edit into the text form Postgres
// reads for the column's type. json.Number keeps the exact digits the client
// sent, so a bigint key is never rounded on the way in.
func paramText(v any) (any, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case string:
		return x, nil
	case bool:
		return strconv.FormatBool(x), nil
	case json.Number:
		return x.String(), nil
	case map[string]any, []any:
		b, err := json.Marshal(x)
		if err != nil {
			return nil, fmt.Errorf("%w: value is not JSON: %v", ErrInvalidInput, err)
		}
		return string(b), nil
	default:
		return nil, fmt.Errorf("%w: unsupported value type %T", ErrInvalidInput, v)
	}
}

// writeSQL is a statement with its bound arguments, in placeholder order.
type writeSQL struct {
	stmt string
	args []any
}

// sortedKeys returns the map's keys in sorted order, so generated SQL is
// deterministic.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// BuildInsertSQL builds INSERT INTO table (cols) VALUES ($n::type, …). Every
// value is a bound text parameter cast to its column's catalog type.
func BuildInsertSQL(table string, cols []Column, values map[string]any) (writeSQL, error) {
	if len(values) == 0 {
		return writeSQL{}, fmt.Errorf("%w: insert needs at least one column", ErrInvalidInput)
	}
	var names, marks []string
	var args []any
	for _, name := range sortedKeys(values) {
		col, err := columnByName(cols, name)
		if err != nil {
			return writeSQL{}, err
		}
		arg, err := paramText(values[name])
		if err != nil {
			return writeSQL{}, err
		}
		args = append(args, arg)
		names = append(names, quoteIdent(col.Name))
		marks = append(marks, fmt.Sprintf("$%d::%s", len(args), col.Type))
	}
	stmt := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		quoteIdent(table), strings.Join(names, ", "), strings.Join(marks, ", "))
	return writeSQL{stmt: stmt, args: args}, nil
}

// keyWhere builds WHERE "k1" = $n::t AND …, over the table's primary key only.
// It refuses a table with no primary key, and a key that does not name every
// primary-key column exactly.
func keyWhere(cols []Column, key map[string]any, args []any) (string, []any, error) {
	pk := primaryKey(cols)
	if len(pk) == 0 {
		return "", nil, fmt.Errorf("%w: table has no primary key; its rows are read-only", ErrInvalidInput)
	}
	if len(key) != len(pk) {
		return "", nil, fmt.Errorf("%w: key must name exactly the primary-key columns", ErrInvalidInput)
	}
	var parts []string
	for _, c := range pk {
		v, ok := key[c.Name]
		if !ok {
			return "", nil, fmt.Errorf("%w: key is missing column %q", ErrInvalidInput, c.Name)
		}
		arg, err := paramText(v)
		if err != nil {
			return "", nil, err
		}
		if arg == nil {
			return "", nil, fmt.Errorf("%w: key column %q cannot be NULL", ErrInvalidInput, c.Name)
		}
		args = append(args, arg)
		parts = append(parts, fmt.Sprintf("%s = $%d::%s", quoteIdent(c.Name), len(args), c.Type))
	}
	return strings.Join(parts, " AND "), args, nil
}

// BuildUpdateSQL builds UPDATE table SET col = $n::t, … WHERE <primary key>.
// Values are bound; the key is the original row's primary key.
func BuildUpdateSQL(table string, cols []Column, key, values map[string]any) (writeSQL, error) {
	if len(values) == 0 {
		return writeSQL{}, fmt.Errorf("%w: update needs at least one column", ErrInvalidInput)
	}
	var sets []string
	var args []any
	for _, name := range sortedKeys(values) {
		col, err := columnByName(cols, name)
		if err != nil {
			return writeSQL{}, err
		}
		arg, err := paramText(values[name])
		if err != nil {
			return writeSQL{}, err
		}
		args = append(args, arg)
		sets = append(sets, fmt.Sprintf("%s = $%d::%s", quoteIdent(col.Name), len(args), col.Type))
	}
	where, args, err := keyWhere(cols, key, args)
	if err != nil {
		return writeSQL{}, err
	}
	stmt := fmt.Sprintf("UPDATE %s SET %s WHERE %s", quoteIdent(table), strings.Join(sets, ", "), where)
	return writeSQL{stmt: stmt, args: args}, nil
}

// BuildDeleteSQL builds DELETE FROM table WHERE <primary key>.
func BuildDeleteSQL(table string, cols []Column, key map[string]any) (writeSQL, error) {
	where, args, err := keyWhere(cols, key, nil)
	if err != nil {
		return writeSQL{}, err
	}
	return writeSQL{stmt: fmt.Sprintf("DELETE FROM %s WHERE %s", quoteIdent(table), where), args: args}, nil
}

// ErrNoSuchTable means the requested public table does not exist. The handler
// maps it to 404.
var ErrNoSuchTable = errors.New("no such table")

// BrowseResult is one page of a table: its catalog columns, the rows in column
// order, the primary key, and whether another page follows.
type BrowseResult struct {
	Columns    []Column `json:"columns"`
	Rows       [][]any  `json:"rows"`
	HasMore    bool     `json:"has_more"`
	PrimaryKey []string `json:"primary_key"`
}

// BrowsePage reads one page of public.table. It fetches one extra row to learn
// whether another page exists, and reads through QueryReadOnly, so a filter
// cannot write.
func BrowsePage(ctx context.Context, db *sql.DB, table, sortCol string, desc bool, filter string, limit, offset int) (BrowseResult, error) {
	cols, err := TableColumns(ctx, db, table)
	if err != nil {
		return BrowseResult{}, err
	}
	if len(cols) == 0 {
		return BrowseResult{}, fmt.Errorf("%w: %s", ErrNoSuchTable, table)
	}
	stmt, err := BuildBrowseSQL(table, cols, sortCol, desc, filter, limit+1, offset)
	if err != nil {
		return BrowseResult{}, err
	}
	res, err := QueryReadOnly(ctx, db, stmt)
	if err != nil {
		return BrowseResult{}, err
	}
	page := BrowseResult{Columns: cols, Rows: res.Rows, PrimaryKey: PrimaryKey(cols)}
	if len(page.Rows) > limit {
		page.HasMore = true
		page.Rows = page.Rows[:limit]
	}
	return page, nil
}
