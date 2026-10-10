package dbconnect

import (
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
)

// resultShape is the column names of a result and which columns are bytea.
type resultShape struct {
	cols  []string
	bytea []bool
}

// resultColumns describes an open result. Names are never nil: a statement with
// no result columns must serialize as [], not null.
func resultColumns(rows *sql.Rows) (resultShape, error) {
	cols, err := rows.Columns()
	if err != nil {
		return resultShape{}, fmt.Errorf("query columns: %w", err)
	}
	types, err := rows.ColumnTypes()
	if err != nil {
		return resultShape{}, fmt.Errorf("query column types: %w", err)
	}
	shape := resultShape{cols: []string{}, bytea: make([]bool, len(types))}
	shape.cols = append(shape.cols, cols...)
	for i, ct := range types {
		shape.bytea[i] = strings.EqualFold(ct.DatabaseTypeName(), "BYTEA")
	}
	return shape, nil
}

// scanRow reads the current row of rows into JSON-safe values. Read and write
// results both go through here, so the same column type serializes the same way
// in a SELECT and in an INSERT … RETURNING.
func scanRow(rows *sql.Rows, shape resultShape) ([]any, error) {
	vals := make([]any, len(shape.cols))
	ptrs := make([]any, len(shape.cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return nil, fmt.Errorf("query scan: %w", err)
	}
	for i, v := range vals {
		switch x := v.(type) {
		case []byte:
			if shape.bytea[i] {
				vals[i] = byteaText(x)
			} else {
				vals[i] = string(x)
			}
		case int64:
			vals[i] = jsonSafeInt(x)
		}
	}
	return vals, nil
}

// byteaText renders bytea in Postgres's own text form, \x followed by hex. Raw
// bytes are not valid UTF-8 and would be replaced by U+FFFD in the JSON.
func byteaText(b []byte) string {
	return `\x` + hex.EncodeToString(b)
}
