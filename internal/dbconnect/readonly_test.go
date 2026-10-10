package dbconnect

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsReadOnlyViolationMatchesSQLSTATE(t *testing.T) {
	readOnly := &pgconn.PgError{Code: "25006", Message: "cannot execute DELETE in a read-only transaction"}
	wrapped := fmt.Errorf("query: %w", readOnly)

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"read-only violation", readOnly, true},
		{"wrapped read-only violation", wrapped, true},
		{"other SQLSTATE", &pgconn.PgError{Code: "42601", Message: "cannot insert multiple commands"}, false},
		{"active tx (25001)", &pgconn.PgError{Code: "25001", Message: "VACUUM cannot run inside a transaction block"}, false},
		{"message text only", errors.New("cannot execute DELETE in a read-only transaction"), false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsReadOnlyViolation(c.err); got != c.want {
				t.Fatalf("IsReadOnlyViolation(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}
