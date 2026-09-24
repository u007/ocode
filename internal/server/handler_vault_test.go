package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/vault"
)

func newVaultTestHandler(t *testing.T) *Handler {
	t.Helper()
	h := NewHandler()
	h.SetVault(vault.New(filepath.Join(t.TempDir(), "vault.json")))
	return h
}

// vaultCall invokes a vault handler directly with an optional JSON body.
func vaultCall(t *testing.T, fn func(w http.ResponseWriter, r *http.Request), method, target, body, id string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, rdr)
	if id != "" {
		req.SetPathValue("id", id)
	}
	rec := httptest.NewRecorder()
	fn(rec, req)
	return rec
}

func vaultStatus(t *testing.T, h *Handler, surface string) (exists, unlocked bool) {
	t.Helper()
	rec := vaultCall(t, h.HandleVaultStatus, "GET", "/api/vault/status?surface="+url.QueryEscape(surface), "", "")
	if rec.Code != 200 {
		t.Fatalf("status code = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var st struct {
		Exists   bool `json:"exists"`
		Unlocked bool `json:"unlocked"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	return st.Exists, st.Unlocked
}

func TestVaultInitStatusAndLocked403(t *testing.T) {
	h := newVaultTestHandler(t)

	if exists, unlocked := vaultStatus(t, h, "settings"); exists || unlocked {
		t.Fatalf("pre-init status = (%v,%v), want (false,false)", exists, unlocked)
	}

	if rec := vaultCall(t, h.HandleVaultList, "GET", "/api/vault/items?surface=settings", "", ""); rec.Code != 403 {
		t.Fatalf("locked list = %d, want 403 (%s)", rec.Code, rec.Body.String())
	}

	rec := vaultCall(t, h.HandleVaultInit, "POST", "/api/vault/init", `{"master":"m","surface":"settings"}`, "")
	if rec.Code != 200 {
		t.Fatalf("init = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if exists, unlocked := vaultStatus(t, h, "settings"); !exists || !unlocked {
		t.Fatalf("post-init status = (%v,%v), want (true,true)", exists, unlocked)
	}

	if rec := vaultCall(t, h.HandleVaultInit, "POST", "/api/vault/init", `{"master":"m","surface":"settings"}`, ""); rec.Code != 409 {
		t.Fatalf("second init = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
}

func TestVaultUnlockWrongMaster401(t *testing.T) {
	h := newVaultTestHandler(t)
	if rec := vaultCall(t, h.HandleVaultInit, "POST", "/api/vault/init", `{"master":"right","surface":"settings"}`, ""); rec.Code != 200 {
		t.Fatalf("init = %d, want 200", rec.Code)
	}
	// Drop every grant (and the in-memory data key) so the next unlock starts
	// from a locked vault.
	if rec := vaultCall(t, h.HandleVaultLock, "POST", "/api/vault/lock", "", ""); rec.Code != 204 {
		t.Fatalf("lock = %d, want 204", rec.Code)
	}

	rec := vaultCall(t, h.HandleVaultUnlock, "POST", "/api/vault/unlock", `{"master":"wrong","surface":"settings"}`, "")
	if rec.Code != 401 {
		t.Fatalf("unlock(wrong) = %d, want 401 (%s)", rec.Code, rec.Body.String())
	}
	if _, unlocked := vaultStatus(t, h, "settings"); unlocked {
		t.Fatal("surface unlocked after a failed unlock")
	}
}

func TestVaultCRUDAndReveal(t *testing.T) {
	h := newVaultTestHandler(t)
	if rec := vaultCall(t, h.HandleVaultInit, "POST", "/api/vault/init", `{"master":"m","surface":"settings"}`, ""); rec.Code != 200 {
		t.Fatalf("init = %d, want 200", rec.Code)
	}

	rec := vaultCall(t, h.HandleVaultCreate, "POST", "/api/vault/items?surface=settings",
		`{"site":"Example","username":"alice","password":"pw"}`, "")
	if rec.Code != 200 {
		t.Fatalf("create = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var created vault.Item
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	if created.ID == "" || created.Site != "Example" {
		t.Fatalf("created = %+v, want an id and site Example", created)
	}

	rev := vaultCall(t, h.HandleVaultReveal, "GET", "/api/vault/items/"+created.ID+"/reveal?surface=settings", "", created.ID)
	if rev.Code != 200 {
		t.Fatalf("reveal = %d, want 200 (%s)", rev.Code, rev.Body.String())
	}
	var revealed vault.Item
	if err := json.Unmarshal(rev.Body.Bytes(), &revealed); err != nil {
		t.Fatalf("decode revealed: %v", err)
	}
	if revealed.Password != "pw" {
		t.Fatalf("revealed password = %q, want pw", revealed.Password)
	}

	if del := vaultCall(t, h.HandleVaultDelete, "DELETE", "/api/vault/items/"+created.ID+"?surface=settings", "", created.ID); del.Code != 204 {
		t.Fatalf("delete = %d, want 204 (%s)", del.Code, del.Body.String())
	}
}

func TestVaultLockAllClearsSurfaces(t *testing.T) {
	h := newVaultTestHandler(t)
	if rec := vaultCall(t, h.HandleVaultInit, "POST", "/api/vault/init", `{"master":"m","surface":"settings"}`, ""); rec.Code != 200 {
		t.Fatalf("init = %d, want 200", rec.Code)
	}
	if rec := vaultCall(t, h.HandleVaultUnlock, "POST", "/api/vault/unlock", `{"master":"m","surface":"other"}`, ""); rec.Code != 200 {
		t.Fatalf("unlock(other) = %d, want 200", rec.Code)
	}
	for _, s := range []string{"settings", "other"} {
		if _, unlocked := vaultStatus(t, h, s); !unlocked {
			t.Fatalf("surface %q not unlocked before lock-all", s)
		}
	}

	if rec := vaultCall(t, h.HandleVaultLock, "POST", "/api/vault/lock", "", ""); rec.Code != 204 {
		t.Fatalf("lock-all = %d, want 204", rec.Code)
	}
	for _, s := range []string{"settings", "other"} {
		if h.vaultGranted(s) {
			t.Errorf("grant for %q survived lock-all", s)
		}
		if _, unlocked := vaultStatus(t, h, s); unlocked {
			t.Errorf("surface %q still unlocked after lock-all", s)
		}
	}
}

func TestVaultLockSurface(t *testing.T) {
	h := newVaultTestHandler(t)
	if rec := vaultCall(t, h.HandleVaultInit, "POST", "/api/vault/init", `{"master":"m","surface":"settings"}`, ""); rec.Code != 200 {
		t.Fatalf("init = %d, want 200", rec.Code)
	}
	if rec := vaultCall(t, h.HandleVaultUnlock, "POST", "/api/vault/unlock", `{"master":"m","surface":"other"}`, ""); rec.Code != 200 {
		t.Fatalf("unlock(other) = %d, want 200", rec.Code)
	}

	h.VaultLockSurface("settings")

	if h.vaultGranted("settings") {
		t.Error("grant for settings survived VaultLockSurface(settings)")
	}
	if !h.vaultGranted("other") {
		t.Error("VaultLockSurface(settings) dropped the other surface's grant")
	}
}

// TestVaultUngrantedSurfaceDenied pins the per-surface grant: an ungranted
// surface must be refused even while the vault itself is unlocked for another
// surface (otherwise unlock would be a global switch).
func TestVaultUngrantedSurfaceDenied(t *testing.T) {
	h := newVaultTestHandler(t)
	if rec := vaultCall(t, h.HandleVaultInit, "POST", "/api/vault/init", `{"master":"m","surface":"settings"}`, ""); rec.Code != 200 {
		t.Fatalf("init = %d, want 200", rec.Code)
	}
	if rec := vaultCall(t, h.HandleVaultUnlock, "POST", "/api/vault/unlock", `{"master":"m","surface":"other"}`, ""); rec.Code != 200 {
		t.Fatalf("unlock(other) = %d, want 200", rec.Code)
	}

	if _, unlocked := vaultStatus(t, h, "other"); !unlocked {
		t.Fatal("other surface should be unlocked")
	}
	if rec := vaultCall(t, h.HandleVaultList, "GET", "/api/vault/items?surface=third", "", ""); rec.Code != 403 {
		t.Fatalf("ungranted surface list = %d, want 403 (%s)", rec.Code, rec.Body.String())
	}
	if rec := vaultCall(t, h.HandleVaultList, "GET", "/api/vault/items?surface=settings", "", ""); rec.Code != 200 {
		t.Fatalf("granted surface list = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}

	// A surface-scoped lock drops exactly that surface even though the vault
	// stays unlocked for the other one.
	h.VaultLockSurface("settings")
	if rec := vaultCall(t, h.HandleVaultList, "GET", "/api/vault/items?surface=settings", "", ""); rec.Code != 403 {
		t.Fatalf("locked surface list = %d, want 403 (%s)", rec.Code, rec.Body.String())
	}
	if rec := vaultCall(t, h.HandleVaultList, "GET", "/api/vault/items?surface=other", "", ""); rec.Code != 200 {
		t.Fatalf("other surface list after lock = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
}

func TestVaultGenerate(t *testing.T) {
	h := newVaultTestHandler(t)
	rec := vaultCall(t, h.HandleVaultGenerate, "POST", "/api/vault/generate", `{"length":24,"upper":true,"digits":true,"symbols":true}`, "")
	if rec.Code != 200 {
		t.Fatalf("generate = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var out struct {
		Password string `json:"password"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode generate: %v", err)
	}
	if len(out.Password) != 24 {
		t.Fatalf("password = %q (len %d), want length 24", out.Password, len(out.Password))
	}
}

func TestVaultListRejectsMalformedPagination(t *testing.T) {
	h := newVaultTestHandler(t)
	if rec := vaultCall(t, h.HandleVaultInit, "POST", "/api/vault/init", `{"master":"m","surface":"settings"}`, ""); rec.Code != 200 {
		t.Fatalf("init = %d, want 200", rec.Code)
	}

	cases := []string{
		"limit=abc",
		"limit=-1",
		"limit=1001",
		"offset=abc",
		"sort=username",
	}
	for _, q := range cases {
		rec := vaultCall(t, h.HandleVaultList, "GET", "/api/vault/items?surface=settings&"+q, "", "")
		if rec.Code != 400 {
			t.Errorf("list ?%s = %d, want 400 (%s)", q, rec.Code, rec.Body.String())
		}
	}
}

func TestVaultRevealUnknownId404(t *testing.T) {
	h := newVaultTestHandler(t)
	if rec := vaultCall(t, h.HandleVaultInit, "POST", "/api/vault/init", `{"master":"m","surface":"settings"}`, ""); rec.Code != 200 {
		t.Fatalf("init = %d, want 200", rec.Code)
	}
	rec := vaultCall(t, h.HandleVaultReveal, "GET", "/api/vault/items/nope/reveal?surface=settings", "", "nope")
	if rec.Code != 404 {
		t.Fatalf("reveal(unknown) = %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
}
