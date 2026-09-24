package server

import (
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/u007/ocode/internal/vault"
)

// Password vault HTTP surface.
//
// A random 32-byte data key protects the items; the master password only ever
// unwraps that key in memory. "Unlock" is tracked per client-supplied
// *surface* id (e.g. "settings", or a browse panel's state key) so closing a
// browse panel can revoke just its own access. The surface is UX state, not a
// security boundary — the auth token is. Vault request bodies (master
// passwords, secrets) are never logged.

// SetVault swaps the handler's vault. It is the explicit boot/test seam: tests
// point it at a temp file so no test touches the real data dir.
func (h *Handler) SetVault(v *vault.Vault) {
	h.vault = v
}

// VaultLockSurface drops the unlock grant for one surface (called when a browse
// panel closes). An empty surface is ignored.
func (h *Handler) VaultLockSurface(surface string) {
	if surface == "" {
		return
	}
	h.vaultMu.Lock()
	delete(h.vaultGrants, surface)
	h.vaultMu.Unlock()
}

// vaultRef returns the configured vault, or nil when the data dir could not be
// resolved at boot.
func (h *Handler) vaultRef() *vault.Vault {
	return h.vault
}

// vaultGranted reports whether a non-empty surface currently holds a grant.
func (h *Handler) vaultGranted(surface string) bool {
	if surface == "" {
		return false
	}
	h.vaultMu.Lock()
	defer h.vaultMu.Unlock()
	return h.vaultGrants[surface]
}

// vaultGrant marks a surface as unlocked.
func (h *Handler) vaultGrant(surface string) {
	if surface == "" {
		return
	}
	h.vaultMu.Lock()
	if h.vaultGrants == nil {
		h.vaultGrants = make(map[string]bool)
	}
	h.vaultGrants[surface] = true
	h.vaultMu.Unlock()
}

// vaultLockAll clears every surface grant.
func (h *Handler) vaultLockAll() {
	h.vaultMu.Lock()
	clear(h.vaultGrants)
	h.vaultMu.Unlock()
}

// vaultAnyGrant reports whether any surface is currently unlocked.
func (h *Handler) vaultAnyGrant() bool {
	h.vaultMu.Lock()
	defer h.vaultMu.Unlock()
	return len(h.vaultGrants) > 0
}

// vaultStore returns the vault only when the surface holds a grant. An empty
// surface never matches (it would otherwise be a blanket grant).
func (h *Handler) vaultStore(surface string) *vault.Vault {
	if h.vault == nil || !h.vaultGranted(surface) {
		return nil
	}
	return h.vault
}

// vaultForSurface resolves a grant-checked vault or writes the failure
// response: 403 when the surface is not unlocked, 500 when the vault is
// unavailable. It returns nil when a response was written.
func (h *Handler) vaultForSurface(w http.ResponseWriter, surface string) *vault.Vault {
	if v := h.vaultStore(surface); v != nil {
		return v
	}
	if h.vaultRef() == nil {
		writeError(w, http.StatusInternalServerError, "vault unavailable")
	} else {
		writeError(w, http.StatusForbidden, "vault is locked")
	}
	return nil
}

// vaultErr logs the failed operation and maps a vault sentinel to the HTTP
// status the client should see.
func (h *Handler) vaultErr(w http.ResponseWriter, op string, err error) {
	log.Printf("vault: %s: %v", op, err)
	switch {
	case errors.Is(err, vault.ErrWrongMaster):
		writeError(w, http.StatusUnauthorized, "invalid master password")
	case errors.Is(err, vault.ErrLocked):
		writeError(w, http.StatusForbidden, "vault is locked")
	case errors.Is(err, vault.ErrNotFound):
		writeError(w, http.StatusNotFound, "vault item not found")
	case errors.Is(err, vault.ErrExists):
		writeError(w, http.StatusConflict, "vault already exists")
	case errors.Is(err, vault.ErrNoVault):
		writeError(w, http.StatusNotFound, "vault does not exist")
	default:
		writeError(w, http.StatusInternalServerError, "vault operation failed")
	}
}

// atoiDefault parses s, returning def when s is empty. It reports false for a
// present-but-non-numeric value so callers can reject rather than silently
// coerce.
func atoiDefault(s string, def int) (int, bool) {
	if s == "" {
		return def, true
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

// decodeOptionalVaultBody accepts an empty body (io.EOF) as all-zero values.
func decodeOptionalVaultBody(r *http.Request, v any) error {
	if err := readBodyJSON(r, v); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// HandleVaultStatus reports whether a vault exists and whether this surface is
// currently unlocked.
func (h *Handler) HandleVaultStatus(w http.ResponseWriter, r *http.Request) {
	v := h.vaultRef()
	if v == nil {
		writeError(w, http.StatusInternalServerError, "vault unavailable")
		return
	}
	surface := r.URL.Query().Get("surface")
	writeJSON(w, http.StatusOK, map[string]bool{
		"exists":   v.Exists(),
		"unlocked": v.Unlocked() && h.vaultGranted(surface),
	})
}

// HandleVaultInit creates a new vault and grants the calling surface.
func (h *Handler) HandleVaultInit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Master  string `json:"master"`
		Surface string `json:"surface"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		log.Printf("vault: vault init: decode request: %v", err)
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	if req.Master == "" || req.Surface == "" {
		writeError(w, http.StatusBadRequest, "master and surface are required")
		return
	}
	v := h.vaultRef()
	if v == nil {
		writeError(w, http.StatusInternalServerError, "vault unavailable")
		return
	}
	if err := v.Init(req.Master); err != nil {
		h.vaultErr(w, "vault init", err)
		return
	}
	h.vaultGrant(req.Surface)
	writeJSON(w, http.StatusOK, map[string]bool{"unlocked": true})
}

// HandleVaultUnlock unlocks the vault with a master password and grants the
// calling surface.
func (h *Handler) HandleVaultUnlock(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Master  string `json:"master"`
		Surface string `json:"surface"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		log.Printf("vault: vault unlock: decode request: %v", err)
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	if req.Master == "" || req.Surface == "" {
		writeError(w, http.StatusBadRequest, "master and surface are required")
		return
	}
	v := h.vaultRef()
	if v == nil {
		writeError(w, http.StatusInternalServerError, "vault unavailable")
		return
	}
	if err := v.Unlock(req.Master); err != nil {
		h.vaultErr(w, "vault unlock", err)
		return
	}
	h.vaultGrant(req.Surface)
	writeJSON(w, http.StatusOK, map[string]bool{"unlocked": true})
}

// HandleVaultLock drops one surface's grant, or every grant (and the in-memory
// data key) when no surface is supplied.
func (h *Handler) HandleVaultLock(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Surface string `json:"surface"`
	}
	if err := decodeOptionalVaultBody(r, &req); err != nil {
		log.Printf("vault: vault lock: decode request: %v", err)
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	if req.Surface == "" {
		h.vaultLockAll()
		if v := h.vaultRef(); v != nil {
			v.Lock()
		}
	} else {
		h.VaultLockSurface(req.Surface)
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleVaultList returns item metadata for an unlocked surface.
func (h *Handler) HandleVaultList(w http.ResponseWriter, r *http.Request) {
	v := h.vaultForSurface(w, r.URL.Query().Get("surface"))
	if v == nil {
		return
	}
	q := r.URL.Query()

	sortKey := q.Get("sort")
	if sortKey != "" && sortKey != "site" {
		writeError(w, http.StatusBadRequest, "invalid sort")
		return
	}
	limit, ok := atoiDefault(q.Get("limit"), 200)
	if !ok || limit < 0 || limit > 1000 {
		writeError(w, http.StatusBadRequest, "invalid limit")
		return
	}
	offset, ok := atoiDefault(q.Get("offset"), 0)
	if !ok || offset < 0 {
		writeError(w, http.StatusBadRequest, "invalid offset")
		return
	}

	items, err := v.List(sortKey, limit, offset)
	if err != nil {
		h.vaultErr(w, "vault list", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": v.Count()})
}

// HandleVaultReveal returns one full item, including its password.
func (h *Handler) HandleVaultReveal(w http.ResponseWriter, r *http.Request) {
	v := h.vaultForSurface(w, r.URL.Query().Get("surface"))
	if v == nil {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing item id")
		return
	}
	it, err := v.Reveal(id)
	if err != nil {
		h.vaultErr(w, "vault reveal", err)
		return
	}
	writeJSON(w, http.StatusOK, it)
}

// HandleVaultCreate adds an item with a server-generated id.
func (h *Handler) HandleVaultCreate(w http.ResponseWriter, r *http.Request) {
	v := h.vaultForSurface(w, r.URL.Query().Get("surface"))
	if v == nil {
		return
	}
	var it vault.Item
	if err := readBodyJSON(r, &it); err != nil {
		log.Printf("vault: vault create: decode request: %v", err)
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	// Never trust client-supplied identity or timestamps.
	it.ID = ""
	it.Created = ""
	it.Updated = ""
	created, err := v.Create(it)
	if err != nil {
		h.vaultErr(w, "vault create", err)
		return
	}
	writeJSON(w, http.StatusOK, created)
}

// HandleVaultUpdate replaces an existing item.
func (h *Handler) HandleVaultUpdate(w http.ResponseWriter, r *http.Request) {
	v := h.vaultForSurface(w, r.URL.Query().Get("surface"))
	if v == nil {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing item id")
		return
	}
	var it vault.Item
	if err := readBodyJSON(r, &it); err != nil {
		log.Printf("vault: vault update: decode request: %v", err)
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	it.ID = ""
	it.Created = ""
	it.Updated = ""
	updated, err := v.Update(id, it)
	if err != nil {
		h.vaultErr(w, "vault update", err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// HandleVaultDelete removes an item.
func (h *Handler) HandleVaultDelete(w http.ResponseWriter, r *http.Request) {
	v := h.vaultForSurface(w, r.URL.Query().Get("surface"))
	if v == nil {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing item id")
		return
	}
	if err := v.Delete(id); err != nil {
		h.vaultErr(w, "vault delete", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleVaultMatch returns items that plausibly match a URL.
func (h *Handler) HandleVaultMatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL     string `json:"url"`
		Surface string `json:"surface"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		log.Printf("vault: vault match: decode request: %v", err)
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	v := h.vaultForSurface(w, req.Surface)
	if v == nil {
		return
	}
	items, err := v.MatchForURL(req.URL)
	if err != nil {
		h.vaultErr(w, "vault match", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// HandleVaultChangeMaster re-wraps the data key under a new master password.
func (h *Handler) HandleVaultChangeMaster(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		log.Printf("vault: vault change master: decode request: %v", err)
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	if req.Old == "" || req.New == "" {
		writeError(w, http.StatusBadRequest, "old and new master are required")
		return
	}
	if !h.vaultAnyGrant() {
		writeError(w, http.StatusForbidden, "vault is locked")
		return
	}
	v := h.vaultRef()
	if v == nil {
		writeError(w, http.StatusInternalServerError, "vault unavailable")
		return
	}
	if err := v.ChangeMaster(req.Old, req.New); err != nil {
		h.vaultErr(w, "vault change master", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleVaultGenerate returns a random password. It needs no unlock: it never
// touches the vault.
func (h *Handler) HandleVaultGenerate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Length  int  `json:"length"`
		Upper   bool `json:"upper"`
		Digits  bool `json:"digits"`
		Symbols bool `json:"symbols"`
	}
	if err := decodeOptionalVaultBody(r, &req); err != nil {
		log.Printf("vault: vault generate: decode request: %v", err)
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	pw := vault.GeneratePassword(vault.GenOptions{
		Length:  req.Length,
		Upper:   req.Upper,
		Digits:  req.Digits,
		Symbols: req.Symbols,
	})
	writeJSON(w, http.StatusOK, map[string]string{"password": pw})
}
