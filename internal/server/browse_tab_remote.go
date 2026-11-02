package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/u007/ocode/internal/browse"
)

// maxBrowseRevokeBody caps the revoke body the remote API proxy reads to find the
// panel. A revoke names one state key, so anything larger is refused, not cut short.
const maxBrowseRevokeBody = 1 << 16

// remoteTabTunnel is the tunnel resolver of a forwarding tab for projectHost: the
// host's browse origin, reached over the workspace tunnel exactly as the prefix
// proxy reaches it. It never dials.
func (s *Server) remoteTabTunnel(projectHost string) browse.TunnelFunc {
	return func(w http.ResponseWriter, r *http.Request) (string, bool) {
		return s.handler.connectedBrowseTunnel(w, projectHost)
	}
}

// mintRemoteGrant mints stateKey's one-time grant on the project host's own browse
// origin. It goes through the host's API proxy, the path the SPA's
// /api/remote/{host}/api/browse/grant call takes: admission, connect and the host's
// bearer token. The caller gets the grant alone. The host's token never leaves the
// proxy, and a host's error reply is relayed with its status.
func (s *Server) mintRemoteGrant(w http.ResponseWriter, r *http.Request, host, stateKey string) (string, bool) {
	body, err := json.Marshal(map[string]string{"state_key": stateKey})
	if err != nil {
		log.Printf("tab origin: encode remote grant request for %s: %v", stateKey, err)
		writeError(w, http.StatusInternalServerError, "tab origin: encode grant request: "+err.Error())
		return "", false
	}
	status, reply, err := s.remoteBrowseCall(r, host, "grant", body)
	if err != nil {
		log.Printf("tab origin: remote grant for %s on %s: %v", stateKey, host, err)
		writeError(w, http.StatusBadGateway, "tab origin: remote grant: "+err.Error())
		return "", false
	}
	if status != http.StatusOK {
		writeError(w, status, fmt.Sprintf("tab origin: remote grant: %s", bytes.TrimSpace(reply)))
		return "", false
	}
	var out struct {
		Grant string `json:"grant"`
	}
	if err := json.Unmarshal(reply, &out); err != nil {
		log.Printf("tab origin: decode remote grant reply from %s: %v", host, err)
		writeError(w, http.StatusBadGateway, "tab origin: remote grant reply is not JSON")
		return "", false
	}
	if out.Grant == "" {
		writeError(w, http.StatusBadGateway, "tab origin: remote grant reply has no grant")
		return "", false
	}
	return out.Grant, true
}

// remoteBrowseCall sends one browse API call to host's /api/browse/{action} through
// the host's API proxy, the way the SPA's /api/remote/{host}/api/browse/{action} call
// does. It returns the host's status and body. Admission and the connect are the
// proxy's own, so a caller that must not dial checks the connection first.
func (s *Server) remoteBrowseCall(r *http.Request, host, action string, body []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, "/api/remote/"+host+"/api/browse/"+action, bytes.NewReader(body))
	if err != nil {
		return 0, nil, fmt.Errorf("build remote browse %s request: %w", action, err)
	}
	req.SetPathValue("host", host)
	req.SetPathValue("rest", "browse/"+action)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", requestOrigin(r))
	out := newBufferedResponse()
	s.handler.HandleRemoteProxy(out, req)
	return out.statusCode(), out.body.Bytes(), nil
}

// revokeRemoteBrowse tells a remote host to revoke stateKey's browse session, as the
// SPA's own revoke call does. It never dials: a host that is not connected is logged
// and skipped, since its browse session lives in that host's memory.
func (s *Server) revokeRemoteBrowse(r *http.Request, host, stateKey string) {
	project, ok := s.handler.firstSavedProjectForHost(host)
	if !ok {
		log.Printf("browse revoke: remote host %s is not a saved project; its session for %s is left", host, stateKey)
		return
	}
	if s.handler.remoteHosts == nil {
		log.Printf("browse revoke: remote connections not available; %s's session on %s is left", stateKey, host)
		return
	}
	if _, connected := s.handler.remoteHosts.connectedWorkspace(host, project.RemotePort); !connected {
		log.Printf("browse revoke: remote host %s is not connected; %s's session there is left", host, stateKey)
		return
	}
	body, err := json.Marshal(map[string]string{"state_key": stateKey})
	if err != nil {
		log.Printf("browse revoke: encode remote revoke for %s: %v", stateKey, err)
		return
	}
	status, reply, err := s.remoteBrowseCall(r, host, "revoke", body)
	if err != nil {
		log.Printf("browse revoke: remote revoke for %s on %s: %v", stateKey, host, err)
		return
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		log.Printf("browse revoke: remote revoke for %s on %s: status %d: %s", stateKey, host, status, bytes.TrimSpace(reply))
	}
}

// handleRemoteAPI is the /api/remote/{host}/api/{rest...} proxy. A browse revoke for
// a remote panel is the SPA's call to the host, not to this machine. The panel's tab
// origins here forward to that host, so they are closed too, or they would outlive
// the panel. The revoke then goes on to the host as before.
func (s *Server) handleRemoteAPI(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("rest") == "browse/revoke" && r.Method == http.MethodPost {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBrowseRevokeBody))
		if err != nil {
			writeError(w, http.StatusRequestEntityTooLarge, "browse revoke: body too large or unreadable: "+err.Error())
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		var req struct {
			StateKey string `json:"state_key"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, http.StatusBadRequest, "browse revoke: bad request body: "+err.Error())
			return
		}
		if req.StateKey == "" {
			writeError(w, http.StatusBadRequest, "browse revoke: state_key is required")
			return
		}
		s.browse.Revoke(req.StateKey)
		s.forgetTabOrigins(req.StateKey)
	}
	s.handler.HandleRemoteProxy(w, r)
}

// bufferedResponse is an in-process reply. A proxy writes it, and the caller reads
// the status and body back.
type bufferedResponse struct {
	header http.Header
	code   int
	body   bytes.Buffer
}

func newBufferedResponse() *bufferedResponse {
	return &bufferedResponse{header: http.Header{}}
}

func (b *bufferedResponse) Header() http.Header { return b.header }

func (b *bufferedResponse) WriteHeader(code int) {
	if b.code == 0 {
		b.code = code
	}
}

func (b *bufferedResponse) Write(p []byte) (int, error) {
	b.WriteHeader(http.StatusOK)
	return b.body.Write(p)
}

// statusCode is the status the proxy wrote, or 200 when it wrote none, as net/http
// sends for a reply with no explicit status.
func (b *bufferedResponse) statusCode() int {
	if b.code == 0 {
		return http.StatusOK
	}
	return b.code
}
