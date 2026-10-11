package dbconnect

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrNoRowMatched is the optimistic-concurrency signal for an update or delete:
// the key matched nothing, so the row changed or was deleted since it was read.
var ErrNoRowMatched = errors.New("no row matched the key; it may have changed or been deleted")

// ErrManyRowsMatched means the key matched more than one row, so it does not
// identify a single row. The change is refused.
var ErrManyRowsMatched = errors.New("key matched more than one row; refusing to change it")

// ApplyRowChange runs one insert, update or delete in its own transaction and
// returns the affected-row count. An insert must affect one row. An update or
// delete must match exactly one row, and anything else rolls back: that is the
// row-level "changed underneath" check.
func ApplyRowChange(ctx context.Context, db *sql.DB, op, table string, key, values map[string]any) (int64, error) {
	cols, err := TableColumns(ctx, db, table)
	if err != nil {
		return 0, err
	}
	if len(cols) == 0 {
		return 0, fmt.Errorf("%w: %s", ErrNoSuchTable, table)
	}
	var stmt writeSQL
	switch op {
	case "insert":
		stmt, err = BuildInsertSQL(table, cols, values)
	case "update":
		stmt, err = BuildUpdateSQL(table, cols, key, values)
	case "delete":
		stmt, err = BuildDeleteSQL(table, cols, key)
	default:
		return 0, fmt.Errorf("%w: op must be insert, update or delete", ErrInvalidInput)
	}
	if err != nil {
		return 0, err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin row tx: %w", err)
	}
	// intentionally not logged: after a successful Commit this Rollback reports
	// sql.ErrTxDone, which means nothing is left to undo.
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, stmt.stmt, stmt.args...)
	if err != nil {
		return 0, fmt.Errorf("row %s: %w", op, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("row %s rows affected: %w", op, err)
	}
	if op == "insert" {
		if n != 1 {
			return 0, fmt.Errorf("insert affected %d rows, want 1", n)
		}
	} else {
		switch {
		case n == 0:
			return 0, ErrNoRowMatched
		case n > 1:
			return 0, ErrManyRowsMatched
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit row %s: %w", op, err)
	}
	return n, nil
}
