package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// These cover the paths that need no database: parameter validation and the
// locked-connection refusal. The SQL itself is covered by the dbconnect builder
// tests, and the live Postgres checks were run by hand.

func TestDBConnectRowsRefusesLockedConnection(t *testing.T) {
	h := &Handler{}
	rec := httptest.NewRecorder()
	h.HandleDBConnectRows(rec, httptest.NewRequest(http.MethodGet,
		"/api/dbconnect/rows?surface=s1&connection=prod&table=t&limit=10", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("rows while locked = %d, want 403 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestDBConnectRowsValidatesParameters(t *testing.T) {
	h := &Handler{}
	cases := map[string]string{
		"missing table":   "surface=s1&connection=prod&limit=10",
		"missing surface": "connection=prod&table=t&limit=10",
		"limit above max": "surface=s1&connection=prod&table=t&limit=501",
		"limit zero":      "surface=s1&connection=prod&table=t&limit=0",
		"bad dir":         "surface=s1&connection=prod&table=t&limit=10&dir=sideways",
	}
	for name, query := range cases {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.HandleDBConnectRows(rec, httptest.NewRequest(http.MethodGet, "/api/dbconnect/rows?"+query, nil))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%s = %d, want 400 (body=%s)", name, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestDBConnectRowRefusesLockedConnection(t *testing.T) {
	h := &Handler{}
	body := `{"surface":"s1","connection":"prod","op":"delete","table":"t","key":{"id":"7"}}`
	rec := httptest.NewRecorder()
	h.HandleDBConnectRow(rec, httptest.NewRequest(http.MethodPost, "/api/dbconnect/row", strings.NewReader(body)))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("row while locked = %d, want 403 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestDBConnectRowRejectsMalformedRequests(t *testing.T) {
	h := &Handler{}
	cases := map[string]string{
		"not json":        `{"surface":`,
		"missing surface": `{"connection":"prod","op":"insert","table":"t"}`,
		"missing table":   `{"surface":"s1","connection":"prod","op":"insert"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.HandleDBConnectRow(rec, httptest.NewRequest(http.MethodPost, "/api/dbconnect/row", bytes.NewReader([]byte(body))))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%s = %d, want 400 (body=%s)", name, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestParseDBDirection(t *testing.T) {
	cases := []struct {
		raw      string
		wantDesc bool
		wantOK   bool
	}{
		{"", false, true},
		{"asc", false, true},
		{"desc", true, true},
		{"DESC", false, false},
		{"up", false, false},
	}
	for _, c := range cases {
		desc, ok := parseDBDirection(c.raw)
		if desc != c.wantDesc || ok != c.wantOK {
			t.Fatalf("parseDBDirection(%q) = (%v,%v), want (%v,%v)", c.raw, desc, ok, c.wantDesc, c.wantOK)
		}
	}
}
