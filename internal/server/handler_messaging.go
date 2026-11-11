package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
	"github.com/u007/ocode/internal/messaging"
)

func (h *Handler) handleMessagingAccounts(w http.ResponseWriter, r *http.Request) {
	mgr := h.messaging
	if mgr == nil {
		http.Error(w, "messaging not initialized", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		accs, err := mgr.Store().ListAccounts()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out := make([]map[string]interface{}, 0, len(accs))
		for _, a := range accs {
			out = append(out, map[string]interface{}{
				"id":       a.ID,
				"provider": a.Provider,
				"label":    a.Label,
				"status":   mgr.Status(a.ID),
				"error":    a.Error,
			})
		}
		writeJSON(w, http.StatusOK, out)
	case http.MethodPost:
		var in struct {
			Provider    string `json:"provider"`
			Label       string `json:"label"`
			Credentials string `json:"credentials"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		id := strings.ToLower(in.Provider) + "-" + randomID()
		acc := messaging.Account{
			ID:          id,
			Provider:    messaging.Provider(in.Provider),
			Label:       in.Label,
			Credentials: in.Credentials,
			Status:      "disconnected",
		}
		if err := mgr.Store().UpsertAccount(acc); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"id": id})
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			// support /api/messaging/accounts/{id}
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			if len(parts) > 0 {
				id = parts[len(parts)-1]
			}
		}
		mgr.StopAccount(id)
		if err := mgr.Store().DeleteAccount(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) handleMessagingAccountAction(w http.ResponseWriter, r *http.Request) {
	mgr := h.messaging
	if mgr == nil {
		http.Error(w, "messaging not initialized", http.StatusServiceUnavailable)
		return
	}
	// path: /api/messaging/accounts/{id}/{action}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	id := parts[3]
	// DELETE /api/messaging/accounts/{id} (no action segment) lands here too.
	if len(parts) == 4 && r.Method == http.MethodDelete {
		mgr.StopAccount(id)
		if err := mgr.Store().DeleteAccount(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if len(parts) < 5 {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	action := parts[4]
	acc, err := mgr.Store().GetAccount(id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	switch action {
	case "start":
		if err := mgr.StartAccount(r.Context(), acc); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "connected"})
	case "stop":
		mgr.StopAccount(id)
		writeJSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
	}
}

func (h *Handler) handleMessagingChats(w http.ResponseWriter, r *http.Request) {
	mgr := h.messaging
	if mgr == nil {
		http.Error(w, "messaging not initialized", http.StatusServiceUnavailable)
		return
	}
	accID := r.URL.Query().Get("account_id")
	chats, err := mgr.Store().ListChats(accID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, chats)
}

func (h *Handler) handleMessagingMessages(w http.ResponseWriter, r *http.Request) {
	mgr := h.messaging
	if mgr == nil {
		http.Error(w, "messaging not initialized", http.StatusServiceUnavailable)
		return
	}
	accID := r.URL.Query().Get("account_id")
	chatID := r.URL.Query().Get("chat_id")
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		limit = n
	}
	msgs, err := mgr.Store().ListMessages(accID, chatID, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

func (h *Handler) handleMessagingSearch(w http.ResponseWriter, r *http.Request) {
	mgr := h.messaging
	if mgr == nil {
		http.Error(w, "messaging not initialized", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query().Get("q")
	accID := r.URL.Query().Get("account_id")
	msgs, err := mgr.Store().SearchMessages(q, accID, 50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

func (h *Handler) handleMessagingSend(w http.ResponseWriter, r *http.Request) {
	mgr := h.messaging
	if mgr == nil {
		http.Error(w, "messaging not initialized", http.StatusServiceUnavailable)
		return
	}
	var in struct {
		AccountID string `json:"account_id"`
		ChatID    string `json:"chat_id"`
		Text      string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	mid, err := mgr.Send(r.Context(), in.AccountID, in.ChatID, in.Text)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": mid})
}

func randomID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func (h *Handler) handleMessagingAccountQR(w http.ResponseWriter, r *http.Request) {
	mgr := h.messaging
	if mgr == nil {
		http.Error(w, "messaging not initialized", http.StatusServiceUnavailable)
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 5 {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	id := parts[3]
	status := mgr.Status(id)
	qr := ""
	if strings.HasPrefix(status, "awaiting_qr:") {
		code := strings.TrimPrefix(status, "awaiting_qr:")
		if png, err := qrcode.Encode(code, qrcode.Medium, 256); err == nil {
			qr = "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": status, "qr": qr})
}

func (h *Handler) handleMessagingSync(w http.ResponseWriter, r *http.Request) {
	mgr := h.messaging
	if mgr == nil {
		http.Error(w, "messaging not initialized", http.StatusServiceUnavailable)
		return
	}
	var in struct {
		URL string `json:"url"`
		Dir string `json:"dir"` // push or pull
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	if in.URL != "" {
		u, err := url.Parse(in.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			http.Error(w, "invalid sync url", http.StatusBadRequest)
			return
		}
	}
	if in.Dir == "pull" {
		err := mgr.PullFromRemote(r.Context(), in.URL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	cur, err := mgr.PushToRemote(r.Context(), in.URL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"cursor": cur, "status": "ok"})
}

func (h *Handler) StartMessagingAccounts() {
	mgr := h.messaging
	if mgr == nil || mgr.Store() == nil {
		return
	}
	accs, err := mgr.Store().ListAccounts()
	if err != nil {
		return
	}
	for _, a := range accs {
		_ = mgr.StartAccount(context.Background(), a)
	}
}
