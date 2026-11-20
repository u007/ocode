package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
)

const upstashTestSecret = "ubx-SECRET-test-key-0123456789abcdef"

// upstashTestServer wires the Upstash handlers on a mux with the same patterns
// as Server.registerRoutes, at a fresh isolated home. The auth cache is reloaded
// from that home so earlier tests' keys cannot leak in.
//
// Cleanup order matters: the auth cache reload and the credential-version
// restore are registered BEFORE setHomeTree, so they run AFTER HOME is restored
// (LIFO). Otherwise the cache would stay loaded from a deleted temp dir and a
// later test's auth.Set would write that stale map to the package-wide auth.json.
func upstashTestServer(t *testing.T) (*http.ServeMux, *Handler) {
	t.Helper()
	prevVersion := auth.CredentialVersion()
	t.Cleanup(func() {
		auth.SetCredentialVersionForTest(prevVersion)
		_ = auth.LoadStore()
	})
	setHomeTree(t, t.TempDir())
	if err := auth.LoadStore(); err != nil {
		t.Fatalf("auth.LoadStore: %v", err)
	}

	h := NewHandler()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/upstash/accounts", h.HandleUpstashAccountsList)
	mux.HandleFunc("POST /api/upstash/accounts", h.HandleUpstashAccountCreate)
	mux.HandleFunc("PUT /api/upstash/accounts/{id}", h.HandleUpstashAccountUpdate)
	mux.HandleFunc("DELETE /api/upstash/accounts/{id}", h.HandleUpstashAccountDelete)
	mux.HandleFunc("PUT /api/upstash/settings", h.HandleUpstashSettingsUpdate)
	return mux, h
}

func upstashDo(t *testing.T, mux http.Handler, method, path string, body any) (int, []byte) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func upstashDecode(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return out
}

func upstashCreate(t *testing.T, mux http.Handler, name, key string) string {
	t.Helper()
	code, raw := upstashDo(t, mux, http.MethodPost, "/api/upstash/accounts", map[string]any{
		"name": name, "api_key": key,
	})
	if code != http.StatusCreated {
		t.Fatalf("create %q: %d %s", name, code, raw)
	}
	return upstashDecode(t, raw)["id"].(string)
}

func upstashAuthFile(t *testing.T) string {
	t.Helper()
	path, err := auth.AuthPath()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read auth.json: %v", err)
	}
	return string(raw)
}

func upstashConfigFile(t *testing.T) string {
	t.Helper()
	path, err := config.ActiveOcodeConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ocodeconfig.json: %v", err)
	}
	return string(raw)
}

func TestUpstashAccountsRoundTrip(t *testing.T) {
	mux, _ := upstashTestServer(t)

	code, raw := upstashDo(t, mux, http.MethodPost, "/api/upstash/accounts", map[string]any{
		"name": "work", "api_key": upstashTestSecret, "size": "medium",
		"idle_minutes": 30, "hard_cap_minutes": 600, "retain_days": 7,
	})
	if code != http.StatusCreated {
		t.Fatalf("create: %d %s", code, raw)
	}
	if strings.Contains(string(raw), upstashTestSecret) {
		t.Fatalf("create response leaked the raw key: %s", raw)
	}
	created := upstashDecode(t, raw)
	id := created["id"].(string)
	if created["key_masked"] != "••••••••••••cdef" {
		t.Fatalf("key_masked = %v", created["key_masked"])
	}
	if created["is_default"] != true {
		t.Fatalf("first account must be default, got %v", created["is_default"])
	}
	if created["size"] != "medium" || created["idle_minutes"] != float64(30) ||
		created["hard_cap_minutes"] != float64(600) || created["retain_days"] != float64(7) {
		t.Fatalf("explicit fields not stored: %v", created)
	}

	code, raw = upstashDo(t, mux, http.MethodGet, "/api/upstash/accounts", nil)
	if code != http.StatusOK {
		t.Fatalf("list: %d %s", code, raw)
	}
	if strings.Contains(string(raw), upstashTestSecret) {
		t.Fatalf("list response leaked the raw key: %s", raw)
	}
	list := upstashDecode(t, raw)
	accounts := list["accounts"].([]any)
	if len(accounts) != 1 || list["total"] != float64(1) || list["default_backend"] != "local" {
		t.Fatalf("list = %v", list)
	}

	code, raw = upstashDo(t, mux, http.MethodPut, "/api/upstash/accounts/"+id, map[string]any{
		"name": "work renamed", "size": "large",
	})
	if code != http.StatusOK {
		t.Fatalf("update: %d %s", code, raw)
	}
	updated := upstashDecode(t, raw)
	if updated["name"] != "work renamed" || updated["size"] != "large" {
		t.Fatalf("update response = %v", updated)
	}
	if got, ok := auth.UpstashBoxAPIKey(id); !ok || got != upstashTestSecret {
		t.Fatalf("update without api_key must keep the stored key")
	}

	code, raw = upstashDo(t, mux, http.MethodDelete, "/api/upstash/accounts/"+id, nil)
	if code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", code, raw)
	}
	if _, ok := auth.UpstashBoxAPIKey(id); ok {
		t.Fatal("delete must remove the key from auth.json")
	}
	if strings.Contains(upstashAuthFile(t), id) {
		t.Fatal("auth.json still references the deleted account")
	}
	code, raw = upstashDo(t, mux, http.MethodGet, "/api/upstash/accounts", nil)
	if list := upstashDecode(t, raw); code != http.StatusOK || list["total"] != float64(0) {
		t.Fatalf("after delete list = %d %v", code, list)
	}
}

func TestUpstashKeyInAuthJSONNeverInOcodeConfig(t *testing.T) {
	mux, _ := upstashTestServer(t)
	upstashCreate(t, mux, "secret holder", upstashTestSecret)

	if !strings.Contains(upstashAuthFile(t), upstashTestSecret) {
		t.Fatal("key was not written to auth.json")
	}
	cfgBytes := upstashConfigFile(t)
	if strings.Contains(cfgBytes, upstashTestSecret) {
		t.Fatalf("API key leaked into ocodeconfig.json:\n%s", cfgBytes)
	}
	if !strings.Contains(cfgBytes, "secret holder") {
		t.Fatalf("non-secret fields missing from ocodeconfig.json:\n%s", cfgBytes)
	}
}

func TestUpstashUpdateKeyReplacementAndKeepOnEmpty(t *testing.T) {
	mux, _ := upstashTestServer(t)
	id := upstashCreate(t, mux, "rotate", upstashTestSecret)

	for _, body := range []map[string]any{
		{"name": "rotate", "api_key": ""},
		{"name": "rotate"},
	} {
		code, raw := upstashDo(t, mux, http.MethodPut, "/api/upstash/accounts/"+id, body)
		if code != http.StatusOK {
			t.Fatalf("update %v: %d %s", body, code, raw)
		}
		if got, ok := auth.UpstashBoxAPIKey(id); !ok || got != upstashTestSecret {
			t.Fatalf("update %v dropped or changed the key", body)
		}
	}

	const newKey = "ubx-ROTATED-key-0123456789abcdef"
	code, raw := upstashDo(t, mux, http.MethodPut, "/api/upstash/accounts/"+id, map[string]any{
		"api_key": newKey,
	})
	if code != http.StatusOK {
		t.Fatalf("rotate key: %d %s", code, raw)
	}
	if strings.Contains(string(raw), newKey) {
		t.Fatalf("update response leaked the new key: %s", raw)
	}
	if got, _ := auth.UpstashBoxAPIKey(id); got != newKey {
		t.Fatalf("key not rotated, got %q", got)
	}
}

func TestUpstashValidationRejections(t *testing.T) {
	mux, _ := upstashTestServer(t)
	upstashCreate(t, mux, "existing", upstashTestSecret)

	cases := []struct {
		name string
		body map[string]any
	}{
		{"hard cap 4321", map[string]any{"name": "a", "api_key": "k-1234567890abcdefgh", "hard_cap_minutes": 4321}},
		{"hard cap zero", map[string]any{"name": "a", "api_key": "k-1234567890abcdefgh", "hard_cap_minutes": 0}},
		{"idle zero", map[string]any{"name": "a", "api_key": "k-1234567890abcdefgh", "idle_minutes": 0}},
		{"bad size", map[string]any{"name": "a", "api_key": "k-1234567890abcdefgh", "size": "xl"}},
		{"retain zero", map[string]any{"name": "a", "api_key": "k-1234567890abcdefgh", "retain_days": 0}},
		{"empty name", map[string]any{"name": "  ", "api_key": "k-1234567890abcdefgh"}},
		{"missing api_key", map[string]any{"name": "a"}},
		{"duplicate name", map[string]any{"name": "existing", "api_key": "k-1234567890abcdefgh"}},
		{"duplicate name other case", map[string]any{"name": "EXISTING", "api_key": "k-1234567890abcdefgh"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, raw := upstashDo(t, mux, http.MethodPost, "/api/upstash/accounts", tc.body)
			if code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", code, raw)
			}
			if _, ok := upstashDecode(t, raw)["error"]; !ok {
				t.Fatalf("400 body has no error field: %s", raw)
			}
		})
	}

	// Rejected creates must not leave orphan keys behind: only the one
	// existing account's key may be in auth.json.
	if n := strings.Count(upstashAuthFile(t), `"upstash-box:`); n != 1 {
		t.Fatalf("auth.json has %d upstash keys after rejected creates, want 1", n)
	}

	// Boundary: exactly 3 days is accepted.
	code, raw := upstashDo(t, mux, http.MethodPost, "/api/upstash/accounts", map[string]any{
		"name": "max cap", "api_key": "k-1234567890abcdefgh", "hard_cap_minutes": 4320,
	})
	if code != http.StatusCreated {
		t.Fatalf("hard cap 4320: %d %s", code, raw)
	}
}

func TestUpstashDefaultReassignedOnDelete(t *testing.T) {
	mux, _ := upstashTestServer(t)
	beta := upstashCreate(t, mux, "Beta", "k-beta-1234567890abcdef")
	alpha := upstashCreate(t, mux, "alpha", "k-alpha-1234567890abcdef")

	_, raw := upstashDo(t, mux, http.MethodGet, "/api/upstash/accounts", nil)
	for _, a := range upstashDecode(t, raw)["accounts"].([]any) {
		m := a.(map[string]any)
		if (m["id"] == beta) != (m["is_default"] == true) {
			t.Fatalf("only the first-created account is default, got %v", m)
		}
	}

	if code, raw := upstashDo(t, mux, http.MethodDelete, "/api/upstash/accounts/"+beta, nil); code != http.StatusNoContent {
		t.Fatalf("delete default: %d %s", code, raw)
	}
	acc, ok, err := config.LoadDefaultUpstashBoxAccount()
	if err != nil || !ok || acc.ID != alpha {
		t.Fatalf("default after deleting it = %+v ok=%v err=%v, want %s", acc, ok, err, alpha)
	}

	if code, raw := upstashDo(t, mux, http.MethodDelete, "/api/upstash/accounts/"+alpha, nil); code != http.StatusNoContent {
		t.Fatalf("delete last: %d %s", code, raw)
	}
	_, raw = upstashDo(t, mux, http.MethodGet, "/api/upstash/accounts", nil)
	if got := upstashDecode(t, raw)["default_backend"]; got != "local" {
		t.Fatalf("default_backend after deleting last account = %v, want local", got)
	}
}

func TestUpstashSettingsRefusals(t *testing.T) {
	mux, _ := upstashTestServer(t)

	code, raw := upstashDo(t, mux, http.MethodPut, "/api/upstash/settings", map[string]any{"default_backend": "upstash"})
	if code != http.StatusBadRequest {
		t.Fatalf("upstash with no accounts: %d %s, want 400", code, raw)
	}
	id := upstashCreate(t, mux, "box", "k-box-1234567890abcdefgh")

	code, raw = upstashDo(t, mux, http.MethodPut, "/api/upstash/settings", map[string]any{"default_account_id": "ub_nope"})
	if code != http.StatusBadRequest {
		t.Fatalf("unknown default account: %d %s, want 400", code, raw)
	}
	code, raw = upstashDo(t, mux, http.MethodPut, "/api/upstash/settings", map[string]any{"default_backend": "gpu"})
	if code != http.StatusBadRequest {
		t.Fatalf("unknown backend: %d %s, want 400", code, raw)
	}

	code, raw = upstashDo(t, mux, http.MethodPut, "/api/upstash/settings", map[string]any{
		"default_backend": "upstash", "default_account_id": id,
	})
	if code != http.StatusOK {
		t.Fatalf("valid settings: %d %s", code, raw)
	}
	got := upstashDecode(t, raw)
	if got["default_backend"] != "upstash" || got["default_account_id"] != id {
		t.Fatalf("settings response = %v", got)
	}
}

func TestUpstashListSortingAndPaging(t *testing.T) {
	mux, _ := upstashTestServer(t)
	upstashCreate(t, mux, "charlie", "k-charlie-1234567890abcd")
	upstashCreate(t, mux, "Alpha", "k-alpha-1234567890abcdef")
	upstashCreate(t, mux, "bravo", "k-bravo-1234567890abcdef")

	names := func(path string) []string {
		t.Helper()
		code, raw := upstashDo(t, mux, http.MethodGet, path, nil)
		if code != http.StatusOK {
			t.Fatalf("GET %s: %d %s", path, code, raw)
		}
		var out []string
		for _, a := range upstashDecode(t, raw)["accounts"].([]any) {
			out = append(out, a.(map[string]any)["name"].(string))
		}
		return out
	}

	all := names("/api/upstash/accounts")
	if strings.Join(all, ",") != "Alpha,bravo,charlie" {
		t.Fatalf("sort order = %v, want Alpha,bravo,charlie", all)
	}
	if got := names("/api/upstash/accounts?limit=2"); strings.Join(got, ",") != "Alpha,bravo" {
		t.Fatalf("limit=2 = %v", got)
	}
	if got := names("/api/upstash/accounts?limit=2&offset=2"); strings.Join(got, ",") != "charlie" {
		t.Fatalf("offset=2 limit=2 = %v", got)
	}
	if got := names("/api/upstash/accounts?offset=9"); len(got) != 0 {
		t.Fatalf("offset past end = %v, want empty", got)
	}
	_, raw := upstashDo(t, mux, http.MethodGet, "/api/upstash/accounts?limit=1&offset=1", nil)
	if total := upstashDecode(t, raw)["total"]; total != float64(3) {
		t.Fatalf("total with paging = %v, want 3", total)
	}
	for _, bad := range []string{"?limit=0", "?limit=x", "?offset=-1"} {
		if code, _ := upstashDo(t, mux, http.MethodGet, "/api/upstash/accounts"+bad, nil); code != http.StatusBadRequest {
			t.Fatalf("GET %s = %d, want 400", bad, code)
		}
	}
}

func TestUpstashUnknownIDIsNotFound(t *testing.T) {
	mux, _ := upstashTestServer(t)
	if code, _ := upstashDo(t, mux, http.MethodPut, "/api/upstash/accounts/ub_nope", map[string]any{"name": "x"}); code != http.StatusNotFound {
		t.Fatalf("PUT unknown = %d, want 404", code)
	}
	if code, _ := upstashDo(t, mux, http.MethodDelete, "/api/upstash/accounts/ub_nope", nil); code != http.StatusNotFound {
		t.Fatalf("DELETE unknown = %d, want 404", code)
	}
}

// The handler loads its config at construction. Another session then saves an
// unrelated field. Saving an Upstash account afterwards must keep that field.
func TestUpstashSaveDoesNotEraseConcurrentConfigWrite(t *testing.T) {
	mux, _ := upstashTestServer(t)

	if err := config.SaveEditor("vim"); err != nil {
		t.Fatalf("other session SaveEditor: %v", err)
	}
	upstashCreate(t, mux, "after-edit", upstashTestSecret)

	reloaded, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Editor != "vim" {
		t.Fatalf("unrelated editor setting erased by an Upstash save: got %q", reloaded.Editor)
	}
	if len(reloaded.UpstashBox.Accounts) != 1 {
		t.Fatalf("upstash account not persisted: %+v", reloaded.UpstashBox)
	}
}

// An Upstash key shares the credential store with the LLM providers. It must
// not show up as a configured provider in the model picker.
func TestUpstashKeysAreNotConfiguredProviders(t *testing.T) {
	mux, _ := upstashTestServer(t)
	upstashCreate(t, mux, "picker", upstashTestSecret)
	if !strings.Contains(upstashAuthFile(t), `"upstash-box:`) {
		t.Fatal("precondition: an upstash key must be in auth.json")
	}
	for id := range NewHandler().configuredProviderIDs() {
		if auth.IsUpstashBoxCredentialID(id) {
			t.Fatalf("upstash key %q listed as a configured provider", id)
		}
	}
}

// The handler's in-memory config must mirror the saved section after each
// change, so no later snapshot-based save can drop it.
func TestUpstashHandlerMirrorsSectionIntoInMemoryConfig(t *testing.T) {
	mux, h := upstashTestServer(t)
	id := upstashCreate(t, mux, "mirror", upstashTestSecret)

	h.mu.Lock()
	got := h.cfg.Ocode.UpstashBox.Accounts
	h.mu.Unlock()
	if len(got) != 1 || got[0].ID != id {
		t.Fatalf("in-memory upstash_box after create = %+v, want the new account", got)
	}

	if code, raw := upstashDo(t, mux, http.MethodDelete, "/api/upstash/accounts/"+id, nil); code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", code, raw)
	}
	h.mu.Lock()
	got = h.cfg.Ocode.UpstashBox.Accounts
	h.mu.Unlock()
	if len(got) != 0 {
		t.Fatalf("in-memory upstash_box after delete = %+v, want empty", got)
	}
}

// Paging must never overflow or panic, whatever the query asks for.
func TestUpstashListPagingBoundsAndOverflow(t *testing.T) {
	mux, _ := upstashTestServer(t)
	upstashCreate(t, mux, "one", "k-one-1234567890abcdefgh")
	upstashCreate(t, mux, "two", "k-two-1234567890abcdefgh")

	okCases := []struct {
		query string
		count int
	}{
		{"?offset=1&limit=9223372036854775807", 1},
		{"?offset=9223372036854775807&limit=9223372036854775807", 0},
		{"?offset=9223372036854775807", 0},
		{"?limit=1", 1},
		{"?offset=2", 0},
	}
	for _, tc := range okCases {
		code, raw := upstashDo(t, mux, http.MethodGet, "/api/upstash/accounts"+tc.query, nil)
		if code != http.StatusOK {
			t.Fatalf("GET %s = %d %s, want 200", tc.query, code, raw)
		}
		got := upstashDecode(t, raw)
		if n := len(got["accounts"].([]any)); n != tc.count {
			t.Fatalf("GET %s returned %d accounts, want %d", tc.query, n, tc.count)
		}
		if got["total"] != float64(2) {
			t.Fatalf("GET %s total = %v, want 2", tc.query, got["total"])
		}
	}

	badCases := []string{
		"?limit=0", "?limit=-1", "?limit=9223372036854775808", "?limit=abc",
		"?offset=-1", "?offset=-9223372036854775808", "?offset=x",
	}
	for _, q := range badCases {
		if code, raw := upstashDo(t, mux, http.MethodGet, "/api/upstash/accounts"+q, nil); code != http.StatusBadRequest {
			t.Fatalf("GET %s = %d %s, want 400", q, code, raw)
		}
	}
}

// A page is capped at 200 even when the caller asks for more, and the default
// page is the same cap.
func TestUpstashListPageCappedAt200(t *testing.T) {
	mux, _ := upstashTestServer(t)
	// Seed 210 accounts directly through the config layer: this test is about
	// paging, not about key storage, so no keys are written.
	if err := config.MutateUpstashBox(func(c *config.UpstashBoxConfig) error {
		for i := 0; i < 210; i++ {
			acc := config.UpstashBoxAccount{
				ID: fmt.Sprintf("ub_%03d", i), Name: fmt.Sprintf("box-%03d", i),
				Size: config.UpstashBoxDefaultSize, IdleMinutes: 15, HardCapMinutes: 120, RetainDays: 14,
			}
			if err := c.AddAccount(acc); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"", "?limit=500", "?limit=201"} {
		code, raw := upstashDo(t, mux, http.MethodGet, "/api/upstash/accounts"+q, nil)
		if code != http.StatusOK {
			t.Fatalf("GET %q = %d %s", q, code, raw)
		}
		got := upstashDecode(t, raw)
		if n := len(got["accounts"].([]any)); n != 200 {
			t.Fatalf("GET %q returned %d accounts, want 200", q, n)
		}
		if got["total"] != float64(210) {
			t.Fatalf("GET %q total = %v, want 210", q, got["total"])
		}
	}
}

// Every bound is enforced on create and update with a 400 and a message.
func TestUpstashBoundsTable(t *testing.T) {
	mux, _ := upstashTestServer(t)
	id := upstashCreate(t, mux, "base", "k-base-1234567890abcdefgh") // idle 15, cap 120, retain 14

	ok := func(extra map[string]any) map[string]any {
		body := map[string]any{"name": "t", "api_key": "k-test-1234567890abcdefgh"}
		for k, v := range extra {
			body[k] = v
		}
		return body
	}
	createCases := []struct {
		name   string
		body   map[string]any
		wantOK bool
	}{
		{"idle 4320 with cap 4320 accepted", ok(map[string]any{"idle_minutes": 4320, "hard_cap_minutes": 4320}), true},
		{"idle 4321 rejected", ok(map[string]any{"idle_minutes": 4321, "hard_cap_minutes": 4320}), false},
		{"idle above cap rejected", ok(map[string]any{"idle_minutes": 200, "hard_cap_minutes": 120}), false},
		{"idle equal to cap accepted", ok(map[string]any{"idle_minutes": 120, "hard_cap_minutes": 120}), true},
		{"retain 365 accepted", ok(map[string]any{"retain_days": 365}), true},
		{"retain 366 rejected", ok(map[string]any{"retain_days": 366}), false},
		{"api_key 512 bytes accepted", map[string]any{"name": "k512", "api_key": strings.Repeat("a", 512)}, true},
		{"api_key 513 bytes rejected", map[string]any{"name": "k513", "api_key": strings.Repeat("a", 513)}, false},
		{"api_key with inner space rejected", map[string]any{"name": "sp", "api_key": "abc def-1234567890"}, false},
		{"api_key with newline rejected", map[string]any{"name": "nl", "api_key": "abc\ndef-1234567890"}, false},
		{"api_key with tab rejected", map[string]any{"name": "tab", "api_key": "abc\tdef-1234567890"}, false},
		{"whitespace-only name rejected", map[string]any{"name": "   ", "api_key": "k-test-1234567890abcdefgh"}, false},
	}
	for i, tc := range createCases {
		t.Run("create "+tc.name, func(t *testing.T) {
			// Each case gets its own name so an accepted case cannot collide with
			// a later one through the duplicate-name rule.
			body := map[string]any{}
			for k, v := range tc.body {
				body[k] = v
			}
			if name, _ := body["name"].(string); strings.TrimSpace(name) != "" {
				body["name"] = fmt.Sprintf("bound-%d", i)
			}
			code, raw := upstashDo(t, mux, http.MethodPost, "/api/upstash/accounts", body)
			if tc.wantOK {
				if code != http.StatusCreated {
					t.Fatalf("status = %d, want 201: %s", code, raw)
				}
				return
			}
			if code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", code, raw)
			}
			if msg, _ := upstashDecode(t, raw)["error"].(string); msg == "" || strings.Contains(msg, "invalid upstash box settings") {
				t.Fatalf("400 message is not a clear reason: %q", msg)
			}
		})
	}

	// Update: the bounds are checked against the RESULTING values, so a change
	// to one field is judged against the stored value of the other.
	updateCases := []struct {
		name   string
		body   map[string]any
		wantOK bool
	}{
		{"raise idle above stored cap rejected", map[string]any{"idle_minutes": 200}, false},
		{"lower cap below stored idle rejected", map[string]any{"hard_cap_minutes": 10}, false},
		{"lower cap to idle accepted", map[string]any{"hard_cap_minutes": 15}, true},
		{"retain 0 rejected", map[string]any{"retain_days": 0}, false},
		{"idle 4321 rejected", map[string]any{"idle_minutes": 4321}, false},
		{"new key with space rejected", map[string]any{"api_key": "bad key-1234567890"}, false},
		{"new key over 512 bytes rejected", map[string]any{"api_key": strings.Repeat("b", 513)}, false},
	}
	for _, tc := range updateCases {
		t.Run("update "+tc.name, func(t *testing.T) {
			code, raw := upstashDo(t, mux, http.MethodPut, "/api/upstash/accounts/"+id, tc.body)
			if tc.wantOK {
				if code != http.StatusOK {
					t.Fatalf("status = %d, want 200: %s", code, raw)
				}
				return
			}
			if code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", code, raw)
			}
		})
	}
}

// A PUT that collides with another account's name is refused before any key is
// written, so the stored key of the account being edited is unchanged.
func TestUpstashUpdateDuplicateNameKeepsStoredKey(t *testing.T) {
	mux, _ := upstashTestServer(t)
	upstashCreate(t, mux, "alpha", "k-alpha-1234567890abcdefgh")
	betaID := upstashCreate(t, mux, "beta", "k-beta-original-1234567890")

	code, raw := upstashDo(t, mux, http.MethodPut, "/api/upstash/accounts/"+betaID, map[string]any{
		"name": "ALPHA", "api_key": "k-beta-rotated-1234567890",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("duplicate rename = %d %s, want 400", code, raw)
	}
	if got, _ := auth.UpstashBoxAPIKey(betaID); got != "k-beta-original-1234567890" {
		t.Fatalf("refused update changed the stored key to %q", got)
	}
	if strings.Contains(upstashAuthFile(t), "k-beta-rotated-1234567890") {
		t.Fatal("refused update wrote its new key to auth.json")
	}
}

// injectUpstashSaveFailure makes the next ocodeconfig write fail, so a test can
// exercise the rollback path without relying on file permissions.
func injectUpstashSaveFailure(t *testing.T) {
	t.Helper()
	restore := config.SetUpstashBoxSaveForTest(func(*config.OcodeConfig) error {
		return errors.New("injected config write failure")
	})
	t.Cleanup(restore)
}

func TestUpstashCreateConfigWriteFailureLeavesNoKey(t *testing.T) {
	mux, _ := upstashTestServer(t)
	injectUpstashSaveFailure(t)

	code, raw := upstashDo(t, mux, http.MethodPost, "/api/upstash/accounts", map[string]any{
		"name": "doomed", "api_key": upstashTestSecret,
	})
	if code != http.StatusInternalServerError {
		t.Fatalf("create with failing config write = %d %s, want 500", code, raw)
	}
	if strings.Contains(upstashAuthFile(t), `"upstash-box:`) {
		t.Fatal("failed create left an upstash key in auth.json")
	}
	if strings.Contains(string(raw), upstashTestSecret) {
		t.Fatal("error response leaked the key")
	}
}

func TestUpstashUpdateConfigWriteFailureRestoresKey(t *testing.T) {
	mux, _ := upstashTestServer(t)
	id := upstashCreate(t, mux, "stable", "k-stable-1234567890abcdefgh")
	injectUpstashSaveFailure(t)

	code, raw := upstashDo(t, mux, http.MethodPut, "/api/upstash/accounts/"+id, map[string]any{
		"name": "stable renamed", "api_key": "k-rotated-1234567890abcdefgh",
	})
	if code != http.StatusInternalServerError {
		t.Fatalf("update with failing config write = %d %s, want 500", code, raw)
	}
	if got, _ := auth.UpstashBoxAPIKey(id); got != "k-stable-1234567890abcdefgh" {
		t.Fatalf("failed update left key %q, want the previous key restored", got)
	}
}

func TestUpstashDeleteConfigWriteFailureKeepsAccountAndKey(t *testing.T) {
	mux, _ := upstashTestServer(t)
	id := upstashCreate(t, mux, "keeper", "k-keeper-1234567890abcdefgh")
	injectUpstashSaveFailure(t)

	if code, raw := upstashDo(t, mux, http.MethodDelete, "/api/upstash/accounts/"+id, nil); code != http.StatusInternalServerError {
		t.Fatalf("delete with failing config write = %d %s, want 500", code, raw)
	}
	if got, _ := auth.UpstashBoxAPIKey(id); got != "k-keeper-1234567890abcdefgh" {
		t.Fatalf("failed delete lost the key: %q", got)
	}
	cfg, err := config.LoadUpstashBoxConfig()
	if err != nil || len(cfg.Accounts) != 1 {
		t.Fatalf("failed delete removed the account: %+v err=%v", cfg, err)
	}
}

// Request bodies are capped at 64 KB on every route that reads one.
func TestUpstashBodySizeLimit(t *testing.T) {
	mux, _ := upstashTestServer(t)
	huge := strings.Repeat("x", 70<<10)

	routes := []struct{ method, path string }{
		{http.MethodPost, "/api/upstash/accounts"},
		{http.MethodPut, "/api/upstash/accounts/ub_none"},
		{http.MethodPut, "/api/upstash/settings"},
	}
	for _, rt := range routes {
		code, raw := upstashDo(t, mux, rt.method, rt.path, map[string]any{"name": huge, "api_key": "k-1234567890abcdefgh"})
		if code != http.StatusRequestEntityTooLarge {
			t.Fatalf("%s %s with a 70 KB body = %d %s, want 413", rt.method, rt.path, code, raw)
		}
	}

	code, raw := upstashDo(t, mux, http.MethodPost, "/api/upstash/accounts", "not an object")
	if code != http.StatusBadRequest {
		t.Fatalf("malformed body = %d %s, want 400", code, raw)
	}
}
