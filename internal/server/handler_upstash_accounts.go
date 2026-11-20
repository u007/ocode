package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
)

// Upstash Box account management. Non-secret fields are persisted through
// config.MutateUpstashBoxGuarded (a load-modify-write of the upstash_box
// section). Each API key is stored in auth.json under auth.UpstashBoxCredentialID.
// A response carries only the masked key; the raw key never leaves the server
// or reaches a log.
//
// Ordering for every change that writes a key: all validation, including the
// duplicate-name check, runs before any write. The key write happens inside the
// config lock, and if the config save then fails the key is restored before the
// lock is released.

// upstashBodyMaxBytes caps every Upstash request body. The largest legitimate
// body is one account, a few hundred bytes.
const upstashBodyMaxBytes = 64 << 10

// upstashListMaxPage caps one page of GET /api/upstash/accounts.
const upstashListMaxPage = 200

// upstashAccountRequest is the create and update body. Pointers distinguish an
// absent field from a zero value, so 0 and "" are validated, not defaulted.
type upstashAccountRequest struct {
	Name           *string `json:"name"`
	APIKey         *string `json:"api_key"`
	Size           *string `json:"size"`
	IdleMinutes    *int    `json:"idle_minutes"`
	HardCapMinutes *int    `json:"hard_cap_minutes"`
	RetainDays     *int    `json:"retain_days"`
}

// upstashSettingsRequest is the body of PUT /api/upstash/settings.
type upstashSettingsRequest struct {
	DefaultBackend   *string `json:"default_backend"`
	DefaultAccountID *string `json:"default_account_id"`
}

// upstashAccountView is one account as the API returns it: no raw key.
type upstashAccountView struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	KeyMasked      string `json:"key_masked"`
	Size           string `json:"size"`
	IdleMinutes    int    `json:"idle_minutes"`
	HardCapMinutes int    `json:"hard_cap_minutes"`
	RetainDays     int    `json:"retain_days"`
	IsDefault      bool   `json:"is_default"`
}

func upstashAccountViewOf(acc config.UpstashBoxAccount, key string, isDefault bool) upstashAccountView {
	return upstashAccountView{
		ID:             acc.ID,
		Name:           acc.Name,
		KeyMasked:      maskConnectCredential(auth.Credential{Key: key}),
		Size:           acc.Size,
		IdleMinutes:    acc.IdleMinutes,
		HardCapMinutes: acc.HardCapMinutes,
		RetainDays:     acc.RetainDays,
		IsDefault:      isDefault,
	}
}

// upstashAccountKey returns the stored key for masking. A missing key yields
// "", which masks to the bare placeholder and never reveals anything.
func upstashAccountKey(id string) string {
	key, _ := auth.UpstashBoxAPIKey(id)
	return key
}

// newUpstashBoxAccountID mints an opaque account id from crypto/rand.
func newUpstashBoxAccountID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate account id: %w", err)
	}
	return "ub_" + hex.EncodeToString(b[:]), nil
}

// upstashErrStatus maps a config error to an HTTP status.
func upstashErrStatus(err error) int {
	switch {
	case errors.Is(err, config.ErrUpstashBoxNotFound):
		return http.StatusNotFound
	case errors.Is(err, config.ErrUpstashBoxInvalid):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

// readUpstashBody decodes a JSON request body capped at upstashBodyMaxBytes.
// It writes the response and returns false on failure: 413 for an oversized
// body, 400 for anything unreadable or malformed. Unknown top-level fields are
// ignored, as in readBodyJSON, which the other JSON handlers use.
func readUpstashBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, upstashBodyMaxBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("request body is larger than the %d KB limit", upstashBodyMaxBytes>>10))
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	if err := json.Unmarshal(body, dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

// HandleUpstashAccountsList lists accounts sorted by name then id. limit
// (1..upstashListMaxPage, default and maximum upstashListMaxPage) and offset
// (>= 0) page the list; total counts all accounts.
func (h *Handler) HandleUpstashAccountsList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := upstashListMaxPage
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		if n < limit {
			limit = n
		}
	}
	offset := 0
	if raw := q.Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "offset must be a non-negative integer")
			return
		}
		offset = n
	}

	c, err := config.LoadUpstashBoxConfig()
	if err != nil {
		log.Printf("upstash accounts: load config for list: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to load config: "+err.Error())
		return
	}
	total := len(c.Accounts)
	// Compare against the remaining count rather than computing start+limit,
	// which overflows for a huge offset or limit.
	start := offset
	if start > total {
		start = total
	}
	n := limit
	if remaining := total - start; n > remaining {
		n = remaining
	}
	views := make([]upstashAccountView, 0, n)
	for _, acc := range c.Accounts[start : start+n] {
		views = append(views, upstashAccountViewOf(acc, upstashAccountKey(acc.ID), acc.ID == c.DefaultAccountID))
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"accounts":        views,
		"total":           total,
		"default_backend": c.Backend(),
	})
}

// HandleUpstashAccountCreate adds an account. Every check runs first. The
// duplicate-name check runs before the key is written, so a refused create
// writes nothing.
func (h *Handler) HandleUpstashAccountCreate(w http.ResponseWriter, r *http.Request) {
	var req upstashAccountRequest
	if !readUpstashBody(w, r, &req) {
		return
	}
	name := strings.TrimSpace(upstashDerefString(req.Name))
	key := strings.TrimSpace(upstashDerefString(req.APIKey))
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if key == "" {
		writeError(w, http.StatusBadRequest, "api_key is required")
		return
	}
	if err := config.ValidateUpstashAPIKey(key); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := newUpstashBoxAccountID()
	if err != nil {
		log.Printf("upstash accounts: create %q: %v", name, err)
		writeError(w, http.StatusInternalServerError, "failed to create account id")
		return
	}
	acc := config.UpstashBoxAccount{
		ID:             id,
		Name:           name,
		Size:           config.UpstashBoxDefaultSize,
		IdleMinutes:    config.UpstashBoxDefaultIdleMinutes,
		HardCapMinutes: config.UpstashBoxDefaultHardCapMinutes,
		RetainDays:     config.UpstashBoxDefaultRetainDays,
	}
	applyUpstashAccountFields(&acc, req)
	if err := acc.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	isDefault := false
	err = config.MutateUpstashBoxGuarded(func(c *config.UpstashBoxConfig) (func(), error) {
		// Refusals first: a duplicate name or a bad field writes nothing.
		if err := c.AddAccount(acc); err != nil {
			return nil, err
		}
		if err := auth.SetUpstashBoxAPIKey(id, key); err != nil {
			return nil, fmt.Errorf("store api key in auth.json: %w", err)
		}
		def, ok := c.DefaultAccount()
		isDefault = ok && def.ID == id
		return func() {
			if rmErr := auth.RemoveUpstashBoxAPIKey(id); rmErr != nil {
				log.Printf("upstash accounts: create %q: roll back api key %s: %v", name, id, rmErr)
			}
		}, nil
	})
	if err != nil {
		log.Printf("upstash accounts: create %q: %v", name, err)
		writeError(w, upstashErrStatus(err), err.Error())
		return
	}
	h.refreshUpstashBoxSnapshot()
	writeJSON(w, http.StatusCreated, upstashAccountViewOf(acc, key, isDefault))
}

// HandleUpstashAccountUpdate changes an account's fields. An absent or empty
// api_key keeps the stored key. A new key is validated before anything is
// written, then stored under the config lock and restored if the config save fails.
func (h *Handler) HandleUpstashAccountUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req upstashAccountRequest
	if !readUpstashBody(w, r, &req) {
		return
	}
	newKey := strings.TrimSpace(upstashDerefString(req.APIKey))
	if newKey != "" {
		if err := config.ValidateUpstashAPIKey(newKey); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	var updated config.UpstashBoxAccount
	err := config.MutateUpstashBoxGuarded(func(c *config.UpstashBoxConfig) (func(), error) {
		cur, ok := c.FindAccount(id)
		if !ok {
			return nil, config.ErrUpstashBoxNotFound
		}
		updated = cur
		if req.Name != nil {
			updated.Name = strings.TrimSpace(*req.Name)
		}
		applyUpstashAccountFields(&updated, req)
		// UpdateAccount runs the field checks and the duplicate-name check. It
		// writes nothing, so a refusal here leaves the stored key untouched.
		if err := c.UpdateAccount(updated); err != nil {
			return nil, err
		}
		if newKey == "" {
			return nil, nil
		}
		prevKey, hadPrevKey := auth.UpstashBoxAPIKey(id)
		if err := auth.SetUpstashBoxAPIKey(id, newKey); err != nil {
			return nil, fmt.Errorf("store api key in auth.json: %w", err)
		}
		return func() { restoreUpstashKey(id, prevKey, hadPrevKey) }, nil
	})
	if err != nil {
		log.Printf("upstash accounts: update %s: %v", id, err)
		writeError(w, upstashErrStatus(err), err.Error())
		return
	}

	h.refreshUpstashBoxSnapshot()
	key := upstashAccountKey(id)
	if newKey != "" {
		key = newKey
	}
	cfg, loadErr := config.LoadUpstashBoxConfig()
	isDefault := loadErr == nil && cfg.DefaultAccountID == id
	writeJSON(w, http.StatusOK, upstashAccountViewOf(updated, key, isDefault))
}

// HandleUpstashAccountDelete removes an account and its key in one guarded
// change. If the key removal fails the account stays, and if the config save
// fails the key is restored, so the two never disagree.
func (h *Handler) HandleUpstashAccountDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := config.MutateUpstashBoxGuarded(func(c *config.UpstashBoxConfig) (func(), error) {
		prevKey, hadPrevKey := auth.UpstashBoxAPIKey(id)
		if err := c.RemoveAccount(id); err != nil {
			return nil, err
		}
		if err := auth.RemoveUpstashBoxAPIKey(id); err != nil {
			return nil, fmt.Errorf("remove api key from auth.json: %w", err)
		}
		return func() { restoreUpstashKey(id, prevKey, hadPrevKey) }, nil
	})
	if err != nil {
		log.Printf("upstash accounts: delete %s: %v", id, err)
		writeError(w, upstashErrStatus(err), err.Error())
		return
	}
	h.refreshUpstashBoxSnapshot()
	w.WriteHeader(http.StatusNoContent)
}

// HandleUpstashSettingsUpdate sets the default backend and/or default account.
func (h *Handler) HandleUpstashSettingsUpdate(w http.ResponseWriter, r *http.Request) {
	var req upstashSettingsRequest
	if !readUpstashBody(w, r, &req) {
		return
	}
	var out config.UpstashBoxConfig
	err := config.MutateUpstashBox(func(c *config.UpstashBoxConfig) error {
		if err := c.SetSettings(req.DefaultBackend, req.DefaultAccountID); err != nil {
			return err
		}
		out = *c
		return nil
	})
	if err != nil {
		log.Printf("upstash accounts: update settings: %v", err)
		writeError(w, upstashErrStatus(err), err.Error())
		return
	}
	h.refreshUpstashBoxSnapshot()
	writeJSON(w, http.StatusOK, map[string]string{
		"default_backend":    out.Backend(),
		"default_account_id": out.DefaultAccountID,
	})
}

// applyUpstashAccountFields copies each non-nil request field onto acc. Name is
// handled by the caller (create requires it, update treats it as optional).
func applyUpstashAccountFields(acc *config.UpstashBoxAccount, req upstashAccountRequest) {
	if req.Size != nil {
		acc.Size = *req.Size
	}
	if req.IdleMinutes != nil {
		acc.IdleMinutes = *req.IdleMinutes
	}
	if req.HardCapMinutes != nil {
		acc.HardCapMinutes = *req.HardCapMinutes
	}
	if req.RetainDays != nil {
		acc.RetainDays = *req.RetainDays
	}
}

// refreshUpstashBoxSnapshot copies the just-saved upstash_box section into the
// handler's in-memory config (the same way HandleSetSyncURLConfig updates
// h.cfg.Ocode.SyncURL). The disk read happens outside h.mu; the lock is held
// only for the assignment.
func (h *Handler) refreshUpstashBoxSnapshot() {
	snap, err := config.LoadUpstashBoxConfig()
	if err != nil {
		log.Printf("upstash accounts: reload saved section into memory: %v", err)
		return
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.UpstashBox = snap
	}
	h.mu.Unlock()
}

// restoreUpstashKey puts an account's previous key back after a failed change.
// It runs under the config lock (see config.MutateUpstashBoxGuarded). It logs
// rather than returning an error, because the caller is already reporting one.
func restoreUpstashKey(id, prev string, hadPrev bool) {
	var err error
	if hadPrev {
		err = auth.SetUpstashBoxAPIKey(id, prev)
	} else {
		err = auth.RemoveUpstashBoxAPIKey(id)
	}
	if err != nil {
		log.Printf("upstash accounts: restore previous api key for %s in auth.json: %v", id, err)
	}
}

func upstashDerefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
