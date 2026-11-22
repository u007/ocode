package server

import (
	"errors"
	"log"
	"net/http"

	"github.com/u007/ocode/internal/boxsession"
	"github.com/u007/ocode/internal/config"
)

// Login states for the Upstash Box browser: named cookie and site-storage sets
// kept on this machine (internal/boxsession/loginstate.go). The list shows
// counts only. The assignment maps a project root to one login state. The model
// cannot choose a login state; only these user-facing routes change one.

// upstashLoginStoreFor returns the login-state store. A test replaces it to
// point at a temp directory.
var upstashLoginStoreFor = func() (*boxsession.LoginStateStore, error) {
	dir, err := boxsession.DefaultLoginStateDir()
	if err != nil {
		return nil, err
	}
	return boxsession.NewLoginStateStore(dir), nil
}

// upstashLoginStateView is one saved login state as GET /api/upstash/login-states
// returns it. It carries counts only: never cookie or storage content.
type upstashLoginStateView struct {
	Name    string `json:"name"`
	SavedAt string `json:"saved_at"`
	Cookies int    `json:"cookies"`
	Origins int    `json:"origins"`
	Bytes   int64  `json:"bytes"`
	InUseBy string `json:"in_use_by"`
	// Corrupt marks a file that cannot be read. Its counts are zero, and the UI
	// offers only Delete for it.
	Corrupt bool `json:"corrupt"`
}

// upstashLoginStateRequest is the body of PUT /api/upstash/login-state.
type upstashLoginStateRequest struct {
	Project string `json:"project"`
	Name    string `json:"name"`
}

// upstashLoginStateAssignment is the body of GET and PUT /api/upstash/login-state.
type upstashLoginStateAssignment struct {
	Project string `json:"project"`
	Name    string `json:"name"`
}

// upstashLoginStateInUse is the 409 body of DELETE /api/upstash/login-states/{name}.
type upstashLoginStateInUse struct {
	Error   string `json:"error"`
	InUseBy string `json:"in_use_by"`
}

// upstashLoginStore resolves the store, writing the error response on failure.
func upstashLoginStore(w http.ResponseWriter) (*boxsession.LoginStateStore, bool) {
	store, err := upstashLoginStoreFor()
	if err != nil {
		log.Printf("upstash login states: resolve store: %v", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	return store, true
}

// upstashLoginProject validates the project param against the server's project
// roots, the same trust boundary as session resolution and /api/files/*. It
// writes the error response and returns false when the project is not allowed.
func (h *Handler) upstashLoginProject(w http.ResponseWriter, project string) bool {
	if project == "" {
		writeError(w, http.StatusBadRequest, "project is required")
		return false
	}
	for _, root := range h.allowedProjectRoots() {
		if root == project {
			return true
		}
	}
	writeError(w, http.StatusForbidden, "project is not registered with this server")
	return false
}

// HandleUpstashLoginStatesList serves GET /api/upstash/login-states. The array
// is never null. A file that cannot be read is listed with corrupt true, so it can
// be deleted.
func (h *Handler) HandleUpstashLoginStatesList(w http.ResponseWriter, r *http.Request) {
	store, ok := upstashLoginStore(w)
	if !ok {
		return
	}
	infos, err := store.List()
	if err != nil {
		log.Printf("upstash login states: list: %v", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	views := make([]upstashLoginStateView, 0, len(infos))
	for _, in := range infos {
		holder, inUse, err := store.InUseBy(in.Name)
		if err != nil {
			log.Printf("upstash login states: holder of %q: %v", in.Name, err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		v := upstashLoginStateView{
			Name:    in.Name,
			SavedAt: rfc3339OrEmpty(in.SavedAt),
			Cookies: in.Cookies,
			Origins: in.Origins,
			Bytes:   in.Bytes,
			Corrupt: in.Corrupt,
		}
		if inUse {
			v.InUseBy = holder
		}
		views = append(views, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"login_states": views})
}

// HandleUpstashLoginStateDelete serves DELETE /api/upstash/login-states/{name}.
// It refuses a state a live session holds (409, with the holder), then removes
// the name from every project assignment.
func (h *Handler) HandleUpstashLoginStateDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := boxsession.ValidLoginStateName(name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	store, ok := upstashLoginStore(w)
	if !ok {
		return
	}
	if err := store.Delete(name); err != nil {
		var inUse boxsession.ErrLoginStateInUse
		if errors.As(err, &inUse) {
			log.Printf("upstash login states: delete refused: %v", err)
			writeJSON(w, http.StatusConflict, upstashLoginStateInUse{Error: err.Error(), InUseBy: inUse.SessionID})
			return
		}
		log.Printf("upstash login states: delete %q: %v", name, err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The state file is gone. Clearing the assignments after it is safe to
	// retry: a second delete of a missing state succeeds and clears the same names.
	if err := config.MutateUpstashBox(func(c *config.UpstashBoxConfig) error {
		c.ClearLoginStateAssignments(name)
		return nil
	}); err != nil {
		log.Printf("upstash login states: clear assignments of %q: %v", name, err)
		writeError(w, http.StatusInternalServerError,
			"the login state was deleted but its project assignments could not be cleared: "+err.Error())
		return
	}
	h.refreshUpstashBoxSnapshot()
	w.WriteHeader(http.StatusNoContent)
}

// HandleUpstashLoginStateGet serves GET /api/upstash/login-state?project=<root>.
// name is "" when no login state is assigned.
func (h *Handler) HandleUpstashLoginStateGet(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	if !h.upstashLoginProject(w, project) {
		return
	}
	cfg, err := config.LoadUpstashBoxConfig()
	if err != nil {
		log.Printf("upstash login states: load config: %v", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, upstashLoginStateAssignment{Project: project, Name: cfg.LoginStateFor(project)})
}

// HandleUpstashLoginStateSet serves PUT /api/upstash/login-state. An empty name
// or "none" clears the project's assignment. A name that is not saved yet is
// accepted: it is created on its first save.
func (h *Handler) HandleUpstashLoginStateSet(w http.ResponseWriter, r *http.Request) {
	var req upstashLoginStateRequest
	if !readUpstashBody(w, r, &req) {
		return
	}
	if !h.upstashLoginProject(w, req.Project) {
		return
	}
	if req.Name != "" && req.Name != config.LoginStateNone {
		if err := boxsession.ValidLoginStateName(req.Name); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	var stored string
	err := config.MutateUpstashBox(func(c *config.UpstashBoxConfig) error {
		if err := c.SetLoginState(req.Project, req.Name); err != nil {
			return err
		}
		stored = c.LoginStateFor(req.Project)
		return nil
	})
	if err != nil {
		log.Printf("upstash login states: set assignment: %v", err)
		writeError(w, upstashErrStatus(err), err.Error())
		return
	}
	h.refreshUpstashBoxSnapshot()
	writeJSON(w, http.StatusOK, upstashLoginStateAssignment{Project: req.Project, Name: stored})
}
