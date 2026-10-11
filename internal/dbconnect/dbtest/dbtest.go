// Package dbtest gives Postgres integration tests a fresh database each. Only
// tests behind the pgintegration build tag import it, so a plain `go test`
// never reaches a server.
package dbtest

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// EnvURL names the admin connection URL. Its own database is used only to
// create and drop the per-test databases. It is documented in .env.example.
const EnvURL = "OCODE_TEST_POSTGRES_URL"

// NewDatabase creates a uniquely named database on the server named by EnvURL
// and returns a URL that connects to it. The database is dropped in t.Cleanup.
// A missing variable fails the test: an integration run must never pass by
// checking nothing.
func NewDatabase(t testing.TB) string {
	t.Helper()
	admin := os.Getenv(EnvURL)
	if admin == "" {
		t.Fatalf("%s is not set; run scripts/test-postgres.sh or point it at an admin postgres:// URL", EnvURL)
	}
	u, err := url.Parse(admin)
	if err != nil {
		// url.Parse errors quote the input, which carries the password, so the message is fixed.
		t.Fatalf("%s is not a valid URL", EnvURL)
	}
	db, err := sql.Open("pgx", admin)
	if err != nil {
		t.Fatalf("open admin connection: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close admin connection: %v", err)
		}
	})

	name := "ocode_t_" + randomSuffix(t)
	if _, err := db.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		// FORCE ends any connection a failed test left open.
		if _, err := db.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)"); err != nil {
			t.Errorf("drop test database %s: %v", name, err)
		}
	})
	u.Path = "/" + name
	return u.String()
}

func randomSuffix(t testing.TB) string {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("random database name: %v", err)
	}
	return hex.EncodeToString(b)
}
