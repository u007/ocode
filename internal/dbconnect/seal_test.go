package dbconnect

import (
	"context"
	"strings"
	"testing"
	"time"
)

const testPGURL = "postgres://app:s3cret-pw@db.internal:5432/app?sslmode=disable"

func TestSealOpenURLRoundTrip(t *testing.T) {
	sealed, err := SealURL(testPGURL, "master-pw")
	if err != nil {
		t.Fatalf("SealURL: %v", err)
	}
	got, err := OpenURL(sealed, "master-pw")
	if err != nil {
		t.Fatalf("OpenURL: %v", err)
	}
	if got != testPGURL {
		t.Fatalf("round trip = %q, want %q", got, testPGURL)
	}
}

func TestSealURLDoesNotLeakPlaintext(t *testing.T) {
	sealed, err := SealURL(testPGURL, "master-pw")
	if err != nil {
		t.Fatalf("SealURL: %v", err)
	}
	for _, secret := range []string{testPGURL, "s3cret-pw", "db.internal"} {
		if strings.Contains(sealed, secret) {
			t.Fatalf("sealed envelope contains plaintext %q", secret)
		}
	}
}

func TestSealURLFreshSaltPerCall(t *testing.T) {
	a, err := SealURL(testPGURL, "master-pw")
	if err != nil {
		t.Fatalf("SealURL: %v", err)
	}
	b, err := SealURL(testPGURL, "master-pw")
	if err != nil {
		t.Fatalf("SealURL: %v", err)
	}
	if a == b {
		t.Fatal("two seals of the same URL are identical; salt/nonce is not fresh")
	}
}

func TestOpenURLWrongPasswordFails(t *testing.T) {
	sealed, err := SealURL(testPGURL, "master-pw")
	if err != nil {
		t.Fatalf("SealURL: %v", err)
	}
	if _, err := OpenURL(sealed, "wrong-pw"); err == nil {
		t.Fatal("OpenURL with wrong password succeeded")
	}
}

func TestOpenURLRejectsMalformedEnvelope(t *testing.T) {
	if _, err := OpenURL("not-an-envelope", "master-pw"); err == nil {
		t.Fatal("OpenURL accepted a malformed envelope")
	}
}

func TestSealURLRejectsBadURLAndEmptyPassword(t *testing.T) {
	if _, err := SealURL("mysql://localhost/db", "master-pw"); err == nil {
		t.Fatal("SealURL accepted a non-postgres URL")
	}
	if _, err := SealURL(testPGURL, ""); err == nil {
		t.Fatal("SealURL accepted an empty master password")
	}
}

// TestOpenRegistersPgxDriver guards the driver name: pgx/v5/stdlib registers
// "pgx", so sql.Open("postgres", …) fails with "unknown driver" before any
// network I/O. The port is closed on purpose, so Open must fail on the dial,
// not on the driver lookup.
func TestOpenRegistersPgxDriver(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := Open(ctx, "postgres://app:pw@127.0.0.1:1/app?sslmode=disable&connect_timeout=1")
	if err == nil {
		t.Fatal("Open succeeded against a closed port")
	}
	if strings.Contains(err.Error(), "unknown driver") {
		t.Fatalf("Open used an unregistered driver: %v", err)
	}
}
