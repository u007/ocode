package dbconnect

import (
	"testing"
)

func TestParseURLValid(t *testing.T) {
	_, err := ParseURL("postgres://user:pwd@localhost/db")
	if err != nil {
		t.Errorf("expected ok: %v", err)
	}
}

func TestParseURLBadScheme(t *testing.T) {
	_, err := ParseURL("mysql://localhost/db")
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
