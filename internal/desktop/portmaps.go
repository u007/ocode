package desktop

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/u007/ocode/internal/projects"
	"github.com/u007/ocode/internal/remote"
)

// portMapsHandler serves the desktop-only /api/desktop/portmaps* endpoints
// for a connected remote workspace, backing the web SPA's Ports panel. These
// paths are NEVER proxied to the remote server: RemoteProxy only forwards
// the generic "/api/" catch-all, but net/http's ServeMux resolves the more
// specific patterns registered here first, so each handler re-checks
// localToken itself — RemoteProxy's own check never runs for these paths.
type portMapsHandler struct {
	fm    *remote.ForwardManager
	store *projects.Store // nil when the project store couldn't be opened (best-effort; see startRemoteServer)
	ref   projects.ProjectRef
	// localToken mirrors RemoteProxy's own defense-in-depth check (loopback
	// binding is the primary barrier); "" (test-only) skips the check.
	localToken string
}

func (h *portMapsHandler) authorized(r *http.Request) bool {
	if h.localToken == "" {
		return true
	}
	tok := r.URL.Query().Get("token")
	if tok == "" {
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			tok = strings.TrimPrefix(auth, "Bearer ")
		}
	}
	return tok == h.localToken
}

type portMapView struct {
	RemotePort int  `json:"remote_port"`
	LocalPort  int  `json:"local_port"`
	Enabled    bool `json:"enabled"`
	Live       bool `json:"live"`
}

func writePortMapsJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// register wires every /api/desktop/portmaps* route onto mux.
func (h *portMapsHandler) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/desktop/portmaps", h.list)
	mux.HandleFunc("POST /api/desktop/portmaps", h.add)
	mux.HandleFunc("DELETE /api/desktop/portmaps/{port}", h.remove)
	mux.HandleFunc("POST /api/desktop/portmaps/{port}/enable", h.setEnabled(true))
	mux.HandleFunc("POST /api/desktop/portmaps/{port}/disable", h.setEnabled(false))
}

func (h *portMapsHandler) views() []portMapView {
	if h.store == nil {
		return []portMapView{}
	}
	maps, err := h.store.PortMaps(h.ref)
	if err != nil {
		return []portMapView{}
	}
	out := make([]portMapView, len(maps))
	for i, m := range maps {
		out[i] = portMapView{RemotePort: m.RemotePort, LocalPort: m.LocalPort, Enabled: m.Enabled, Live: h.fm.IsLive(m.RemotePort)}
	}
	return out
}

func (h *portMapsHandler) list(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writePortMapsJSON(w, h.views())
}

func (h *portMapsHandler) add(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if h.store == nil {
		http.Error(w, "port maps unavailable: project store not open", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		RemotePort int `json:"remote_port"`
		LocalPort  int `json:"local_port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if body.LocalPort == 0 {
		body.LocalPort = body.RemotePort
	}
	if body.RemotePort <= 0 || body.RemotePort > 65535 || body.LocalPort <= 0 || body.LocalPort > 65535 {
		http.Error(w, "invalid port", http.StatusBadRequest)
		return
	}
	if err := h.store.AddPortMap(h.ref, body.RemotePort, body.LocalPort); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := h.fm.Start(remote.ProjectPortMap{RemotePort: body.RemotePort, LocalPort: body.LocalPort, Enabled: true}); err != nil {
		http.Error(w, "saved, but failed to open now: "+err.Error(), http.StatusBadGateway)
		return
	}
	writePortMapsJSON(w, h.views())
}

func (h *portMapsHandler) portFromPath(w http.ResponseWriter, r *http.Request) (int, bool) {
	port, err := strconv.Atoi(r.PathValue("port"))
	if err != nil || port <= 0 || port > 65535 {
		http.Error(w, "invalid port", http.StatusBadRequest)
		return 0, false
	}
	return port, true
}

func (h *portMapsHandler) remove(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if h.store == nil {
		http.Error(w, "port maps unavailable: project store not open", http.StatusServiceUnavailable)
		return
	}
	port, ok := h.portFromPath(w, r)
	if !ok {
		return
	}
	_ = h.fm.Stop(port)
	if err := h.store.RemovePortMap(h.ref, port); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writePortMapsJSON(w, h.views())
}

func (h *portMapsHandler) setEnabled(enabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.authorized(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if h.store == nil {
			http.Error(w, "port maps unavailable: project store not open", http.StatusServiceUnavailable)
			return
		}
		port, ok := h.portFromPath(w, r)
		if !ok {
			return
		}
		if err := h.store.SetPortMapEnabled(h.ref, port, enabled); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if !enabled {
			_ = h.fm.Stop(port)
		} else {
			maps, err := h.store.PortMaps(h.ref)
			if err == nil {
				for _, m := range maps {
					if m.RemotePort == port {
						if serr := h.fm.Start(remote.ProjectPortMap{RemotePort: m.RemotePort, LocalPort: m.LocalPort, Enabled: true}); serr != nil {
							http.Error(w, "enabled, but failed to open now: "+serr.Error(), http.StatusBadGateway)
							return
						}
						break
					}
				}
			}
		}
		writePortMapsJSON(w, h.views())
	}
}
