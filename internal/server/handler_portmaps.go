package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/u007/ocode/internal/projects"
	"github.com/u007/ocode/internal/remote"
	"github.com/u007/ocode/internal/tool"
)

// portMapView is the wire shape the web/desktop Port forwards panel renders —
// the project-scoped counterpart of internal/desktop's portMapView. Live is
// the current session's forward state; Enabled is what's persisted.
type portMapView struct {
	RemotePort int  `json:"remote_port"`
	LocalPort  int  `json:"local_port"`
	Enabled    bool `json:"enabled"`
	Live       bool `json:"live"`
}

// portMapRegistry owns one remote.ForwardManager per remote project, keyed by
// the same canonical identity the projects store keys PortMaps on (the
// portless target string plus the verbatim remote path — see
// remoteWorkFor/remoteProjectEntry). Managers live under the server's process
// supervisor, so every `ssh -N -L` child they open dies with the server.
//
// Forwards are per-project because the panel is scoped to the active project;
// a manager is created lazily on first use and kept for the process lifetime.
type portMapRegistry struct {
	sup *tool.ProcessSupervisor

	mu    sync.Mutex
	byKey map[string]*portMapEntry
}

type portMapEntry struct {
	fm *remote.ForwardManager
	// autoStartOnce runs the persisted-enabled forwards exactly once per
	// project per process, on the first list (the panel's mount probe). Any
	// later edits go through the explicit add/enable handlers.
	autoStartOnce sync.Once
}

func newPortMapRegistry(sup *tool.ProcessSupervisor) *portMapRegistry {
	return &portMapRegistry{sup: sup, byKey: make(map[string]*portMapEntry)}
}

// portMapKey is the registry's identity for a remote project. It deliberately
// matches the store's ProjectRef{Host: target.String(), Path: path} so a
// project can never have two managers (or two PortMaps lists).
func portMapKey(target remote.Target, path string) string {
	return target.String() + "\x00" + path
}

func (reg *portMapRegistry) entry(target remote.Target, path string) *portMapEntry {
	key := portMapKey(target, path)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if e, ok := reg.byKey[key]; ok {
		return e
	}
	e := &portMapEntry{fm: remote.NewForwardManager(reg.sup, target)}
	reg.byKey[key] = e
	return e
}

func portMapRef(rw remoteWork) projects.ProjectRef {
	return projects.ProjectRef{Host: rw.Target.String(), Path: rw.Path}
}

// portMapTarget resolves the request's ?host= + ?project= pair into a
// registered SSH remote project and its forward manager. It writes the HTTP
// error itself and returns ok=false on any failure, so every caller can early
// return. WSL targets are rejected: WSL2 shares the Windows loopback, so an
// extra forward would be a no-op at best (the same restriction
// internal/remotecli applies by only wiring its PortMapHook for SSH).
func (h *Handler) portMapTarget(w http.ResponseWriter, r *http.Request) (remoteWork, *portMapEntry, bool) {
	if h.projects == nil {
		writeError(w, http.StatusServiceUnavailable, "port forwards unavailable: project store not open")
		return remoteWork{}, nil, false
	}
	if h.portMaps == nil {
		writeError(w, http.StatusServiceUnavailable, "port forwards unavailable")
		return remoteWork{}, nil, false
	}
	rw, err := h.remoteWorkFor(hostParam(r), strings.TrimSpace(r.URL.Query().Get("project")))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return remoteWork{}, nil, false
	}
	if rw.Target.Kind != remote.KindSSH {
		writeError(w, http.StatusBadRequest, "port forwards are only available for SSH remote projects (WSL shares localhost)")
		return remoteWork{}, nil, false
	}
	return rw, h.portMaps.entry(rw.Target, rw.Path), true
}

// portMapViews snapshots the persisted forwards for ref, decorating each with
// its current live state. A store read failure degrades to an empty list, so
// the panel renders "no forwards" rather than an error.
func (h *Handler) portMapViews(ref projects.ProjectRef, fm *remote.ForwardManager) []portMapView {
	maps, err := h.projects.PortMaps(ref)
	if err != nil {
		return []portMapView{}
	}
	out := make([]portMapView, len(maps))
	for i, m := range maps {
		out[i] = portMapView{RemotePort: m.RemotePort, LocalPort: m.LocalPort, Enabled: m.Enabled, Live: fm.IsLive(m.RemotePort)}
	}
	return out
}

// autoStartPortMaps opens every persisted, enabled forward for ref. Best-effort
// per forward (a failure is logged, not fatal) so one unreachable tunnel cannot
// hide the rest of the panel. Idempotent: ForwardManager.Start is a no-op when
// the forward is already live.
func (h *Handler) autoStartPortMaps(ref projects.ProjectRef, fm *remote.ForwardManager) {
	maps, err := h.projects.PortMaps(ref)
	if err != nil {
		log.Printf("port forwards: load %s:%s: %v", ref.Host, ref.Path, err)
		return
	}
	for _, m := range maps {
		if !m.Enabled {
			continue
		}
		if err := fm.Start(remote.ProjectPortMap{RemotePort: m.RemotePort, LocalPort: m.LocalPort, Enabled: true}); err != nil {
			log.Printf("port forwards: auto-start remote:%d for %s:%s: %v", m.RemotePort, ref.Host, ref.Path, err)
		}
	}
}

// HandleListPortMaps serves GET /api/portmaps?host=&project=. The first list
// for a project is also the auto-start trigger, so persisted forwards come
// back when the panel mounts after a server restart.
func (h *Handler) HandleListPortMaps(w http.ResponseWriter, r *http.Request) {
	rw, entry, ok := h.portMapTarget(w, r)
	if !ok {
		return
	}
	ref := portMapRef(rw)
	entry.autoStartOnce.Do(func() { h.autoStartPortMaps(ref, entry.fm) })
	writeJSON(w, http.StatusOK, h.portMapViews(ref, entry.fm))
}

// HandleAddPortMap serves POST /api/portmaps?host=&project= with body
// {"remote_port":N,"local_port":M} (local defaults to remote). The forward is
// persisted before the live open is attempted, so a failed open leaves a saved
// row the user can retry — same contract as the desktop handler.
func (h *Handler) HandleAddPortMap(w http.ResponseWriter, r *http.Request) {
	rw, entry, ok := h.portMapTarget(w, r)
	if !ok {
		return
	}
	var body struct {
		RemotePort int `json:"remote_port"`
		LocalPort  int `json:"local_port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	if body.LocalPort == 0 {
		body.LocalPort = body.RemotePort
	}
	if body.RemotePort <= 0 || body.RemotePort > 65535 || body.LocalPort <= 0 || body.LocalPort > 65535 {
		writeError(w, http.StatusBadRequest, "invalid port")
		return
	}
	ref := portMapRef(rw)
	if err := h.projects.AddPortMap(ref, body.RemotePort, body.LocalPort); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := entry.fm.Start(remote.ProjectPortMap{RemotePort: body.RemotePort, LocalPort: body.LocalPort, Enabled: true}); err != nil {
		writeError(w, http.StatusBadGateway, "saved, but failed to open now: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, h.portMapViews(ref, entry.fm))
}

// HandleRemovePortMap serves DELETE /api/portmaps/{port}?host=&project=. The
// tunnel is stopped before the persisted entry is dropped; an unknown port is a
// 404 from the store.
func (h *Handler) HandleRemovePortMap(w http.ResponseWriter, r *http.Request) {
	rw, entry, ok := h.portMapTarget(w, r)
	if !ok {
		return
	}
	port, ok := portMapPathPort(w, r)
	if !ok {
		return
	}
	_ = entry.fm.Stop(port)
	ref := portMapRef(rw)
	if err := h.projects.RemovePortMap(ref, port); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, h.portMapViews(ref, entry.fm))
}

// HandleEnablePortMap / HandleDisablePortMap serve
// POST /api/portmaps/{port}/enable|disable?host=&project=.
func (h *Handler) HandleEnablePortMap(w http.ResponseWriter, r *http.Request) {
	h.setPortMapEnabled(w, r, true)
}

func (h *Handler) HandleDisablePortMap(w http.ResponseWriter, r *http.Request) {
	h.setPortMapEnabled(w, r, false)
}

func (h *Handler) setPortMapEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	rw, entry, ok := h.portMapTarget(w, r)
	if !ok {
		return
	}
	port, ok := portMapPathPort(w, r)
	if !ok {
		return
	}
	ref := portMapRef(rw)
	if err := h.projects.SetPortMapEnabled(ref, port, enabled); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if !enabled {
		_ = entry.fm.Stop(port)
	} else {
		maps, err := h.projects.PortMaps(ref)
		if err == nil {
			for _, m := range maps {
				if m.RemotePort != port {
					continue
				}
				if serr := entry.fm.Start(remote.ProjectPortMap{RemotePort: m.RemotePort, LocalPort: m.LocalPort, Enabled: true}); serr != nil {
					writeError(w, http.StatusBadGateway, "enabled, but failed to open now: "+serr.Error())
					return
				}
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, h.portMapViews(ref, entry.fm))
}

// portMapPathPort reads and validates the {port} path value.
func portMapPathPort(w http.ResponseWriter, r *http.Request) (int, bool) {
	port, err := strconv.Atoi(r.PathValue("port"))
	if err != nil || port <= 0 || port > 65535 {
		writeError(w, http.StatusBadRequest, "invalid port")
		return 0, false
	}
	return port, true
}
