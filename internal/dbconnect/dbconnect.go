package dbconnect

// Package dbconnect manages DB connections (pgx for PostgreSQL, dbbrowse reuse for SQLite).
// Connection URLs are sealed with SealURL before they are saved; this package never logs URL or password values.
import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"

	"github.com/u007/ocode/internal/encryption"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

// maxQueryRows caps how many rows QueryReadOnly returns to the caller.
const maxQueryRows = 1000

// maxSafeJSONInt is 2^53-1, the largest integer a JavaScript number holds exactly.
const maxSafeJSONInt = 1<<53 - 1

// jsonSafeInt keeps an int64 as a JSON number when a JavaScript client can hold
// it exactly, and returns its decimal string otherwise. Snowflake-style ids
// exceed 2^53, and a rounded id would address the wrong row.
func jsonSafeInt(n int64) any {
	if n > maxSafeJSONInt || n < -maxSafeJSONInt {
		return strconv.FormatInt(n, 10)
	}
	return n
}

// QueryResult is one read-only query's columns and rows. Truncated is set when
// the row cap cut the result short.
type QueryResult struct {
	Columns   []string `json:"columns"`
	Rows      [][]any  `json:"rows"`
	Truncated bool     `json:"truncated"`
	// RowsAffected is set only for a confirmed write (ExecWrite).
	RowsAffected *int64 `json:"rows_affected,omitempty"`
}

// sqlstateReadOnlyTx is 25006, read_only_sql_transaction: the server refused a
// write inside a READ ONLY transaction.
const sqlstateReadOnlyTx = "25006"

// IsReadOnlyViolation reports whether err is the server refusing a write inside
// a READ ONLY transaction. It matches the SQLSTATE, never the message text.
func IsReadOnlyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == sqlstateReadOnlyTx
}

// serverSideEffectRE matches constructs PostgreSQL accepts inside a READ ONLY
// transaction that still act on the server: they end or cancel other backends,
// take advisory locks, sleep, send notifications, change the role, or touch
// large objects. Function forms require the open paren, so a column that shares
// a function's name is not flagged.
var serverSideEffectRE = regexp.MustCompile(`(?i)\b(?:` +
	`(?:pg_terminate_backend|pg_cancel_backend|pg_reload_conf|pg_notify|pg_sleep\w*|pg_(?:try_)?advisory_\w*|lo_\w+|set_config)\s*\(` +
	`|listen\b|notify\b` +
	`|(?:re)?set\s+(?:local\s+|session\s+)?(?:role|session_authorization|authorization)\b` +
	`)`)

// HasServerSideEffect reports whether query contains a construct that runs in a
// READ ONLY transaction yet still changes server state. The match is static and
// errs toward flagging, so a flagged read only costs one confirmation.
func HasServerSideEffect(query string) bool {
	return serverSideEffectRE.MatchString(query)
}

// Open connects to a DB using the connection URL.
//
// Queries use the extended protocol only (QueryExecModeExec). A Parse message
// carries exactly one statement, so "COMMIT; DELETE …" is rejected by the
// server instead of running the DELETE outside the read-only transaction.
func Open(ctx context.Context, connURL string) (*sql.DB, error) {
	if _, err := ParseURL(connURL); err != nil {
		return nil, err
	}
	cfg, err := pgx.ParseConfig(connURL)
	if err != nil {
		return nil, fmt.Errorf("parse db config: %w", err)
	}
	cfg.DefaultQueryExecMode = pgx.QueryExecModeExec
	db := stdlib.OpenDB(*cfg)
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return db, nil
}

// QueryReadOnly runs one statement inside a READ ONLY transaction and returns
// at most maxQueryRows rows. Writes fail at the server.
func QueryReadOnly(ctx context.Context, db *sql.DB, query string) (*QueryResult, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin read-only tx: %w", err)
	}
	// intentionally not logged: the transaction only ever reads, so a failed
	// rollback leaves nothing behind for the caller to act on.
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()
	shape, err := resultColumns(rows)
	if err != nil {
		return nil, err
	}
	res := &QueryResult{Columns: shape.cols, Rows: [][]any{}}
	for rows.Next() {
		if len(res.Rows) == maxQueryRows {
			res.Truncated = true
			break
		}
		vals, err := scanRow(rows, shape)
		if err != nil {
			return nil, err
		}
		res.Rows = append(res.Rows, vals)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query rows: %w", err)
	}
	return res, nil
}

// SealURL validates a postgres:// URL and encrypts it under the master
// password. Each call draws a fresh salt, so the envelope carries everything
// OpenURL needs besides the password.
func SealURL(connURL, password string) (string, error) {
	if _, err := ParseURL(connURL); err != nil {
		return "", err
	}
	sealed, err := encryption.EncryptJSON([]byte(connURL), password)
	if err != nil {
		return "", fmt.Errorf("seal db url: %w", err)
	}
	return sealed, nil
}

// OpenURL decrypts an envelope from SealURL. A wrong password fails the
// AES-GCM tag check and returns an error; there is no separate verifier.
func OpenURL(sealed, password string) (string, error) {
	plain, err := encryption.DecryptJSON(sealed, password)
	if err != nil {
		return "", fmt.Errorf("open db url: %w", err)
	}
	return string(plain), nil
}

// ParseURL validates a postgres:// URL.
//
// The error text never includes the raw URL: url.Parse's error quotes the
// whole input, password included, and that text reaches API responses.
func ParseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid connection url")
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return nil, fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}
	return u, nil
}

// TablePage is one page of public table names, sorted. HasMore reports whether
// another page follows.
type TablePage struct {
	Tables  []string `json:"tables"`
	HasMore bool     `json:"has_more"`
}

// ListTables returns one page of public table names, sorted by name. It fetches
// one extra row to learn whether another page exists.
func ListTables(ctx context.Context, db *sql.DB, limit, offset int) (TablePage, error) {
	rows, err := db.QueryContext(ctx,
		"SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename LIMIT $1 OFFSET $2",
		limit+1, offset)
	if err != nil {
		return TablePage{}, fmt.Errorf("list tables: %w", err)
	}
	defer rows.Close()
	page := TablePage{Tables: []string{}} // non-nil: an empty schema must serialize as [], not null
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return TablePage{}, fmt.Errorf("list tables: scan: %w", err)
		}
		if len(page.Tables) == limit {
			page.HasMore = true
			break
		}
		page.Tables = append(page.Tables, n)
	}
	if err := rows.Err(); err != nil {
		return TablePage{}, fmt.Errorf("list tables: rows: %w", err)
	}
	return page, nil
}
