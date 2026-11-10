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
	"strings"

	"github.com/u007/ocode/internal/encryption"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/go-sql-driver/mysql"
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
	if errors.As(err, &pgErr) && pgErr.Code == sqlstateReadOnlyTx {
		return true
	}
	var myErr *mysql.MySQLError
	if errors.As(err, &myErr) {
		switch myErr.Number {
		case 1290, 1792, 1836: // read-only transaction violations
			return true
		}
	}
	return false
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

// mysqlServerSideEffectRE matches MySQL constructs that a READ ONLY transaction
// does not stop: they write files on the server, take named locks, or burn CPU
// or time on purpose. Function forms require the open paren, as in
// serverSideEffectRE, so a column that shares a function's name is not flagged.
var mysqlServerSideEffectRE = regexp.MustCompile(`(?i)\b(?:` +
	`into\s+(?:outfile|dumpfile)\b` +
	`|(?:get_lock|release_lock|sleep|benchmark|load_file)\s*\(` +
	`|load\s+data\b` +
	`)`)

// HasServerSideEffectFor is HasServerSideEffect for the driver named by a
// connection URL scheme ("mysql" or "postgres"/"postgresql"). For MySQL it also
// applies mysqlServerSideEffectRE. The PostgreSQL behavior is unchanged.
func HasServerSideEffectFor(scheme, query string) bool {
	if scheme == "mysql" && mysqlServerSideEffectRE.MatchString(query) {
		return true
	}
	return serverSideEffectRE.MatchString(query)
}

// HasServerSideEffectDB is HasServerSideEffectFor for an already-open handle,
// choosing the driver from db itself.
func HasServerSideEffectDB(db *sql.DB, query string) bool {
	if isMySQLDB(db) {
		return HasServerSideEffectFor("mysql", query)
	}
	return HasServerSideEffect(query)
}

// Open connects to a DB using the connection URL. The scheme selects the
// driver: postgres/postgresql -> pgx, mysql -> go-sql-driver/mysql.
func Open(ctx context.Context, connURL string) (*sql.DB, error) {
	u, err := ParseURL(connURL)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "mysql":
		return openMySQL(ctx, connURL)
	default:
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
}

// openMySQL connects to a MySQL database using the mysql driver.
func openMySQL(ctx context.Context, connURL string) (*sql.DB, error) {
	dsn, err := mysqlDSN(connURL)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return db, nil
}

// mysqlDSN converts a mysql:// URL to a go-sql-driver/mysql DSN.
func mysqlDSN(connURL string) (string, error) {
	u, err := url.Parse(connURL)
	if err != nil {
		return "", fmt.Errorf("invalid connection url")
	}
	cfg := mysql.Config{
		User:   u.User.Username(),
		DBName: strings.TrimPrefix(u.Path, "/"),
		Net:    "tcp",
		Addr:   u.Host,
		Params: map[string]string{},
	}
	if p, ok := u.User.Password(); ok {
		cfg.Passwd = p
	}
	if cfg.Addr == "" {
		cfg.Addr = "127.0.0.1:3306"
	} else if !strings.Contains(cfg.Addr, ":") {
		cfg.Addr += ":3306"
	}
	for k, v := range u.Query() {
		if len(v) > 0 {
			cfg.Params[k] = v[0]
		}
	}
	return cfg.FormatDSN(), nil
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

// dbRedirectParams make the client read a file from the server's filesystem or
// redirect the connection through a service definition. A connection URL can
// come from a project's .env (a file that arrives with a cloned repository),
// and a template URL is expanded from it, so these are refused rather than
// handed to pgx. pgx's own documentation (pgconn/config.go) names exactly
// servicefile, passfile, sslkey, sslcert and sslrootcert as the parameters that
// make ParseConfig read local files; servicefile/service can additionally
// redirect the connection, and passfile makes pgx read a file and send its
// contents as the password.
var dbRedirectParams = map[string]bool{
	"service":     true,
	"servicefile": true,
	"passfile":    true,
	"sslkey":      true,
	"sslcert":     true,
	"sslrootcert": true,
}

// mysqlDangerousParams are go-sql-driver/mysql query parameters that read
// local files or enable multi-statement execution, so a repo-supplied URL
// must not be able to turn them on.
var mysqlDangerousParams = map[string]bool{
	"allowallfiles":            true,
	"localinfile":              true,
	"interpolateparams":        true,
	"multistatements":          true,
	"allowcleartextpasswords":  true,
	"allownativepasswords":     true,
	"allowfallbacktoplaintext": true,
	"allowoldpasswords":        true,
}

// mysqlTLSWeakened reports whether a go-sql-driver tls= value turns TLS off or
// lets it fall back to plaintext or skip certificate verification. The driver
// reads "0"/"false" (any case) as off, "preferred" as plaintext fallback and
// "skip-verify" as unverified TLS. Only "true"-style values and registered
// custom TLS config names are left allowed.
func mysqlTLSWeakened(value string) bool {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "skip-verify" || v == "preferred" {
		return true
	}
	if b, err := strconv.ParseBool(v); err == nil && !b {
		return true
	}
	return false
}

// ParseURL validates a postgres:// URL.
//
// A '#' is refused outright. pgx's URI parser is libpq-compatible, not RFC 3986:
// it treats '#' as ordinary query DATA (its own test is named "hash is data not
// fragment"), while net/url ends the query there. Leaving it accepted lets a
// value like "...?x=1#&passfile=/etc/passwd" hide a redirect parameter from the
// check below and still deliver it to pgx. No legitimate postgres URI needs a
// fragment, and a literal '#' in a credential must be percent-encoded anyway.
//
// The error text never includes the raw URL: url.Parse's error quotes the
// whole input, password included, and that text reaches API responses.
func ParseURL(raw string) (*url.URL, error) {
	if strings.ContainsRune(raw, '#') {
		return nil, fmt.Errorf("invalid connection url")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid connection url")
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" && u.Scheme != "mysql" {
		return nil, fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}
	// Parse the query the way pgx does (split on '&', decode the key) instead
	// of via u.Query(), which silently drops ';'-separated pairs.
	for pair := range strings.SplitSeq(u.RawQuery, "&") {
		if pair == "" {
			continue
		}
		name, _, _ := strings.Cut(pair, "=")
		decoded, decErr := url.QueryUnescape(name)
		if decErr != nil {
			// pgx rejects a URI with a bad escape too. Fail closed instead of
			// comparing the still-encoded key, which could hide a redirect
			// parameter behind an escape pgx would still decode.
			return nil, fmt.Errorf("invalid connection url")
		}
		dangerous := dbRedirectParams
		if u.Scheme == "mysql" {
			dangerous = mysqlDangerousParams
		}
		if dangerous[strings.ToLower(decoded)] {
			// Name the parameter only: its value may be a path or a secret.
			return nil, fmt.Errorf("connection parameter %q is not allowed", decoded)
		}
		// Value-level guard for MySQL: never allow TLS to be turned off, downgraded
		// to plaintext, or left unverified.
		if u.Scheme == "mysql" && strings.EqualFold(decoded, "tls") {
			_, val, _ := strings.Cut(pair, "=")
			if v, verr := url.QueryUnescape(val); verr != nil || mysqlTLSWeakened(v) {
				return nil, fmt.Errorf("connection parameter %q value is not allowed", decoded)
			}
		}
	}
	return u, nil
}

// listTablesPostgresSQL and listTablesMySQLSQL each take (limit, offset). The
// MySQL form restricts to base tables so it matches pg_tables, which has no views.
const (
	listTablesPostgresSQL = "SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename LIMIT $1 OFFSET $2"
	listTablesMySQLSQL    = "SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE() AND table_type = 'BASE TABLE' ORDER BY table_name LIMIT ? OFFSET ?"
)

// isMySQLDB reports whether db was opened with the go-sql-driver/mysql driver.
// database/sql exposes the driver through the connector, so no extra state is
// tracked alongside the *sql.DB.
func isMySQLDB(db *sql.DB) bool {
	switch db.Driver().(type) {
	case *mysql.MySQLDriver, mysql.MySQLDriver:
		return true
	}
	return false
}

// listTablesSQL returns the table-listing query for db's driver.
func listTablesSQL(db *sql.DB) string {
	if isMySQLDB(db) {
		return listTablesMySQLSQL
	}
	return listTablesPostgresSQL
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
	rows, err := db.QueryContext(ctx, listTablesSQL(db), limit+1, offset)
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

// SealPassword encrypts a raw secret (e.g. an SSH password) under the master
// password. It does not validate the value as a URL.
func SealPassword(secret, password string) (string, error) {
	if secret == "" {
		return "", fmt.Errorf("secret is required")
	}
	if password == "" {
		return "", fmt.Errorf("master password is required")
	}
	sealed, err := encryption.EncryptJSON([]byte(secret), password)
	if err != nil {
		return "", fmt.Errorf("seal password: %w", err)
	}
	return sealed, nil
}

// OpenPassword decrypts a value from SealPassword.
func OpenPassword(sealed, password string) (string, error) {
	plain, err := encryption.DecryptJSON(sealed, password)
	if err != nil {
		return "", fmt.Errorf("open password: %w", err)
	}
	return string(plain), nil
}
