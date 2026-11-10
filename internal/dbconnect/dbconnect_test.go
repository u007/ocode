package dbconnect

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

func TestParseURLValid(t *testing.T) {
	_, err := ParseURL("postgres://user:pwd@localhost/db")
	if err != nil {
		t.Errorf("expected ok: %v", err)
	}
}

func TestParseURLBadScheme(t *testing.T) {
	_, err := ParseURL("oracle://localhost/db")
	if err == nil {
		t.Fatal("expected error for bad scheme")
	}
}

func TestParseMalformedURL(t *testing.T) {
	_, err := ParseURL("postgres://[bad url")
	if err == nil {
		t.Fatal("expected error for malformed url")
	}
}

func TestParseURLRefusesLocalFileParameters(t *testing.T) {
	// A connection URL may come from a project's .env. These parameters make
	// pgx read a service definition, a password file or TLS material from the
	// server's filesystem, so they are refused rather than handed to pgx.
	for _, raw := range []string{
		"postgres://u:p@host/db?servicefile=/tmp/pg_service.conf",
		"postgres://u:p@host/db?service=prod",
		"postgres://u:p@host/db?passfile=/etc/passwd",
		"postgres://u:p@host/db?PASSFILE=/etc/passwd",
		// pgx also reads these from the local filesystem.
		"postgres://u:p@host/db?sslkey=/tmp/client.key",
		"postgres://u:p@host/db?sslcert=/tmp/client.crt",
		"postgres://u:p@host/db?sslrootcert=/tmp/root.crt",
		// The key is percent-decoded before the comparison, exactly as pgx does.
		"postgres://u:p@host/db?pass%66ile=/etc/passwd",
		// '#' is DATA to pgx and a fragment to net/url, so it must not be
		// usable to hide the parameters above from this check.
		"postgres://u:p@host/db?x=1#&passfile=/tmp/x",
		"postgres://u:p@host/db?x=1#&service=s",
		"postgres://u:p@host/db?x=1#&servicefile=/tmp/pg_service.conf",
		"postgres://u:p@host/db#frag",
	} {
		if _, err := ParseURL(raw); err == nil {
			t.Errorf("ParseURL(%q) = ok, want a refusal", raw)
		}
	}
	for _, raw := range []string{
		"postgres://u:p@host/db?sslmode=disable",
		"postgres://u:p@host/db?connect_timeout=5&application_name=x",
		// A parameter name that merely CONTAINS a refused one is fine.
		"postgres://u:p@host/db?myservicefile=x",
	} {
		if _, err := ParseURL(raw); err != nil {
			t.Errorf("ParseURL(%q) = %v, want ok", raw, err)
		}
	}
}

func TestHasServerSideEffect(t *testing.T) {
	flagged := []string{
		"SELECT pg_terminate_backend(1)",
		"select pg_cancel_backend (2)",
		"NOTIFY chan",
		"LISTEN chan",
		"SELECT pg_notify('chan', 'x')",
		"SELECT pg_try_advisory_xact_lock(1, 2)",
		"SELECT pg_sleep_for('1 second')",
		"SELECT lo_import('/etc/passwd')",
		"RESET ROLE",
		"SET LOCAL ROLE app",
		"SET session_authorization TO 'app'",
	}
	for _, q := range flagged {
		if !HasServerSideEffect(q) {
			t.Errorf("%q: not flagged, want flagged", q)
		}
	}
	plain := []string{
		"SELECT id, role FROM users",
		"SELECT notes, lo_status FROM orders",
		"SELECT setting FROM config WHERE name = 'x'",
		"SELECT count(*) FROM pg_stat_activity",
	}
	for _, q := range plain {
		if HasServerSideEffect(q) {
			t.Errorf("%q: flagged, want a plain read", q)
		}
	}
}

func TestParseURLMySQLValid(t *testing.T) {
	_, err := ParseURL("mysql://user:pwd@localhost/db")
	if err != nil {
		t.Errorf("expected ok: %v", err)
	}
}

func TestParseURLMySQLDangerousParams(t *testing.T) {
	for _, raw := range []string{
		"mysql://u:p@host/db?allowAllFiles=true",
		"mysql://u:p@host/db?multiStatements=true",
		"mysql://u:p@host/db?interpolateParams=true",
		"mysql://u:p@host/db?allowCleartextPasswords=true",
		"mysql://u:p@host/db?allowNativePasswords=true",
		"mysql://u:p@host/db?allowFallbackToPlaintext=true",
		"mysql://u:p@host/db?allowOldPasswords=true",
		"mysql://u:p@host/db?localinfile=true",
		"mysql://u:p@host/db?tls=skip-verify",
	} {
		if _, err := ParseURL(raw); err == nil {
			t.Errorf("expected error for %s", raw)
		}
	}
}

func TestMySQLDSN(t *testing.T) {
	got, err := mysqlDSN("mysql://u:p@host:3307/db?tls=true")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "u:p@tcp(host:3307)/db") {
		t.Fatalf("dsn = %q, want host:3307", got)
	}
	if !strings.Contains(got, "tls=true") {
		t.Fatalf("dsn = %q, want tls param", got)
	}
	// default port
	got2, err := mysqlDSN("mysql://u:p@host/db")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got2, "host:3306") {
		t.Fatalf("dsn = %q, want default 3306", got2)
	}
}

func TestParseURLMySQLTLSWeakenedRefused(t *testing.T) {
	// Each value turns TLS off, downgrades it to plaintext, or skips verification.
	// Case, surrounding whitespace and percent-encoding must not hide them.
	for _, val := range []string{
		"skip-verify",
		"SKIP-VERIFY",
		"Skip-Verify",
		"false",
		"FALSE",
		"False",
		"0",
		"f",
		"F",
		"preferred",
		"PREFERRED",
		"Preferred",
		"%66alse",
		"%20false",
		"skip%2Dverify",
	} {
		raw := "mysql://u:p@host/db?tls=" + val
		if _, err := ParseURL(raw); err == nil {
			t.Errorf("ParseURL(%q) = ok, want a refusal", raw)
		}
	}
}

func TestParseURLMySQLTLSAllowed(t *testing.T) {
	for _, val := range []string{"true", "TRUE", "1", "t", "custom-ca"} {
		raw := "mysql://u:p@host/db?tls=" + val
		if _, err := ParseURL(raw); err != nil {
			t.Errorf("ParseURL(%q) = %v, want ok", raw, err)
		}
	}
}

func TestHasServerSideEffectForMySQL(t *testing.T) {
	flagged := []string{
		"SELECT * FROM t INTO OUTFILE '/tmp/x'",
		"select id into outfile '/var/lib/mysql/x.txt' from t",
		"SELECT 1 INTO DUMPFILE '/tmp/x'",
		"SELECT GET_LOCK('job', 10)",
		"SELECT get_lock ('job', 10)",
		"SELECT RELEASE_LOCK('job')",
		"SELECT SLEEP(5)",
		"select sleep (5)",
		"SELECT BENCHMARK(1000000, MD5('a'))",
		"SELECT LOAD_FILE('/etc/passwd')",
		"LOAD DATA INFILE '/tmp/x' INTO TABLE t",
		"load data local infile '/tmp/x' into table t",
		// The PostgreSQL patterns still apply under MySQL: the check only adds.
		"SELECT pg_sleep(1)",
	}
	for _, q := range flagged {
		if !HasServerSideEffectFor("mysql", q) {
			t.Errorf("mysql %q: not flagged, want flagged", q)
		}
	}
	plain := []string{
		"SELECT id, sleep_count FROM t",
		"SELECT benchmark_score FROM t",
		"SELECT get_lock_count FROM t",
		"SELECT outfile_name, dumpfile_name FROM t",
		"SELECT loaded_data FROM t",
		"SELECT id FROM t WHERE status = 'active'",
	}
	for _, q := range plain {
		if HasServerSideEffectFor("mysql", q) {
			t.Errorf("mysql %q: flagged, want a plain read", q)
		}
	}
}

func TestHasServerSideEffectForPostgresUnchanged(t *testing.T) {
	// MySQL-only constructs are not flagged for PostgreSQL, so its behavior
	// stays exactly what HasServerSideEffect has always done.
	notFlagged := []string{
		"SELECT sleep(1)",
		"SELECT GET_LOCK('a', 1)",
		"SELECT 1 INTO OUTFILE '/tmp/x'",
		"LOAD DATA INFILE '/tmp/x' INTO TABLE t",
	}
	for _, scheme := range []string{"postgres", "postgresql"} {
		for _, q := range notFlagged {
			if HasServerSideEffectFor(scheme, q) {
				t.Errorf("%s %q: flagged, want unchanged (not flagged)", scheme, q)
			}
		}
		if !HasServerSideEffectFor(scheme, "SELECT pg_sleep(1)") {
			t.Errorf("%s: pg_sleep not flagged", scheme)
		}
	}
	if HasServerSideEffectFor("postgres", "SELECT pg_sleep(1)") != HasServerSideEffect("SELECT pg_sleep(1)") {
		t.Error("HasServerSideEffectFor(postgres) diverges from HasServerSideEffect")
	}
}

func TestListTablesSQLPerDriver(t *testing.T) {
	// sql.Open does not dial, so these handles are created offline.
	my, err := sql.Open("mysql", "u:p@tcp(127.0.0.1:1)/db")
	if err != nil {
		t.Fatal(err)
	}
	defer my.Close()
	cfg, err := pgx.ParseConfig("postgres://u:p@127.0.0.1:1/db")
	if err != nil {
		t.Fatal(err)
	}
	pg := stdlib.OpenDB(*cfg)
	defer pg.Close()

	if !isMySQLDB(my) {
		t.Error("mysql handle not detected as mysql")
	}
	if isMySQLDB(pg) {
		t.Error("postgres handle detected as mysql")
	}

	if got := listTablesSQL(pg); got != listTablesPostgresSQL {
		t.Errorf("postgres query = %q, want the pg_tables query", got)
	}
	if !strings.Contains(listTablesPostgresSQL, "$1") || !strings.Contains(listTablesPostgresSQL, "pg_tables") {
		t.Errorf("postgres query changed: %q", listTablesPostgresSQL)
	}

	got := listTablesSQL(my)
	if got != listTablesMySQLSQL {
		t.Errorf("mysql query = %q, want the information_schema query", got)
	}
	for _, want := range []string{"information_schema.tables", "table_schema = DATABASE()", "table_type = 'BASE TABLE'", "ORDER BY table_name", "LIMIT ? OFFSET ?"} {
		if !strings.Contains(got, want) {
			t.Errorf("mysql query %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "$") || strings.Contains(got, "pg_tables") {
		t.Errorf("mysql query %q still uses postgres syntax", got)
	}
}

func TestHasServerSideEffectDBPerDriver(t *testing.T) {
	my, err := sql.Open("mysql", "u:p@tcp(127.0.0.1:1)/db")
	if err != nil {
		t.Fatal(err)
	}
	defer my.Close()
	cfg, err := pgx.ParseConfig("postgres://u:p@127.0.0.1:1/db")
	if err != nil {
		t.Fatal(err)
	}
	pg := stdlib.OpenDB(*cfg)
	defer pg.Close()

	// SELECT SLEEP(1) is flagged only for a MySQL handle.
	if !HasServerSideEffectDB(my, "SELECT SLEEP(1)") {
		t.Error("mysql handle: SLEEP( not flagged")
	}
	if HasServerSideEffectDB(pg, "SELECT SLEEP(1)") {
		t.Error("postgres handle: SLEEP( flagged, want unchanged")
	}
	// pg_sleep is flagged for both.
	for name, db := range map[string]*sql.DB{"mysql": my, "postgres": pg} {
		if !HasServerSideEffectDB(db, "SELECT pg_sleep(1)") {
			t.Errorf("%s handle: pg_sleep not flagged", name)
		}
	}
}
