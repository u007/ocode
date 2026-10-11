package dbconnect

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5/stdlib"
)

// ExecWrite runs one confirmed statement in a read-write transaction and commits
// it. Call it only after the user has confirmed the write; there is no undo.
//
// The statement is described before it runs. An unnamed Parse + Describe does not
// execute anything, and it refuses a multi-statement string at Parse. A
// statement that returns no columns is then executed with Exec and reports the
// affected-row count. A statement that returns columns (INSERT … RETURNING) runs
// through the same row scanner as a read, and rows_affected is the number of rows
// it returned: every row a RETURNING statement changes comes back.
func ExecWrite(ctx context.Context, db *sql.DB, query string) (*QueryResult, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("write conn: %w", err)
	}
	// intentionally not logged: a failed close of a finished connection leaves
	// nothing for the caller to act on.
	defer func() { _ = conn.Close() }()

	width, err := describeWidth(ctx, conn, query)
	if err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin write tx: %w", err)
	}
	// intentionally not logged: after a successful Commit this Rollback reports
	// sql.ErrTxDone, which means nothing is left to undo.
	defer func() { _ = tx.Rollback() }()

	if width == 0 {
		res, err := tx.ExecContext(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("write: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("write rows affected: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit write: %w", err)
		}
		return &QueryResult{Columns: []string{}, Rows: [][]any{}, RowsAffected: &n}, nil
	}

	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}
	defer rows.Close()
	shape, err := resultColumns(rows)
	if err != nil {
		return nil, err
	}
	res := &QueryResult{Columns: shape.cols, Rows: [][]any{}}
	var count int64
	for rows.Next() {
		count++
		if len(res.Rows) == maxQueryRows {
			res.Truncated = true
			continue
		}
		vals, err := scanRow(rows, shape)
		if err != nil {
			return nil, err
		}
		res.Rows = append(res.Rows, vals)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("write rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("write close rows: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit write: %w", err)
	}
	res.RowsAffected = &count
	return res, nil
}

// describeWidth reports how many result columns query would return, without
// executing it. It runs on the pinned connection before the transaction opens.
func describeWidth(ctx context.Context, conn *sql.Conn, query string) (int, error) {
	width := 0
	err := conn.Raw(func(dc any) error {
		sc, ok := dc.(*stdlib.Conn)
		if !ok {
			return fmt.Errorf("unexpected driver connection %T", dc)
		}
		desc, err := sc.Conn().PgConn().Prepare(ctx, "", query, nil)
		if err != nil {
			return err
		}
		width = len(desc.Fields)
		return nil
	})
	return width, err
}
