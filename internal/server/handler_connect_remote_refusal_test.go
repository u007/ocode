package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/projects"
)

// Credentials are PER MACHINE. A Connectors view for a remote (SSH/WSL) project
// must show the host's credentials, never this machine's — a user looking at a
// remote project and seeing their local API key (or, worse, saving one there
// without knowing) is the exact failure the whole host-threading convention
// exists to prevent.
//
// The invariant is enforced by an invisible coupling: `handleConnectList` has
// NO remote awareness at all. It reads the process-local auth store and ignores
// `?host=`, `?project=` and everything else. The ONLY thing keeping a remote
// project's Connectors view off the local store is the web client rewriting the
// path to `/api/remote/{host}/api/...` (fetchJSON's `prefixed` line in
// web/src/api/client.ts). Nothing in Go enforces it.
//
// These tests pin both halves so that coupling cannot be broken silently:
//
//   - the local endpoint answers from the local store even when a host is named,
//     so the client's rewrite is the load-bearing part; and
//   - the remote-prefixed path REFUSES rather than degrading to local state.

// remoteLeakCanary is the tail-4 canary that appears in a masked local key.
// maskConnectCredential never shows more than the last 4 characters, so the
// canary proves a response was served from THIS machine's store without
// reproducing the secret.
const remoteLeakCanary = "9f3a"

const remoteLeakKey = "sk-local-only-canary-" + remoteLeakCanary

// localConnectBody runs the local connect list and returns the raw body.
func localConnectBody(t *testing.T, h *Handler, query string) (string, int) {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/auth/connect"+query, nil)
	h.handleConnectList(w, r)
	return w.Body.String(), w.Code
}

func TestConnectCredentialsForARemoteHostNeverComeFromTheLocalStore(t *testing.T) {
	h := NewHandler()
	preserveConnectCredential(t, "deepseek")
	if err := auth.Set("deepseek", auth.Credential{Kind: auth.KindAPIKey, Key: remoteLeakKey}); err != nil {
		t.Fatal(err)
	}

	// Control: the local endpoint does serve this machine's credential. Without
	// this, the negative assertions below would pass vacuously — a provider list
	// with no credentials at all also "contains no canary".
	t.Run("control: the local endpoint serves the local credential", func(t *testing.T) {
		body, code := localConnectBody(t, h, "")
		if code != http.StatusOK {
			t.Fatalf("list: %d %s", code, body)
		}
		if !strings.Contains(body, remoteLeakCanary) {
			t.Fatalf("control failed — local list has no masked key, so the leak checks below prove nothing: %s", body)
		}
	})

	// handleConnectList is machine-global by design: naming a host changes
	// nothing, because the client never sends one. This is the coupling: if
	// someone later adds `?host=` support to the local handler, this test is
	// where they will look, and it must be a deliberate change rather than an
	// accident that starts answering remote questions from the local store.
	t.Run("the local endpoint ignores a host parameter entirely", func(t *testing.T) {
		body, code := localConnectBody(t, h, "?host=james@box")
		if code != http.StatusOK {
			t.Fatalf("list with host: %d %s", code, body)
		}
		if !strings.Contains(body, remoteLeakCanary) {
			t.Fatalf("expected the LOCAL mask regardless of ?host=, got: %s", body)
		}
	})

	// The security half: the host-scoped path must refuse. It may not answer 200
	// with this machine's provider list, and it may not leak the canary.
	t.Run("the host-scoped path refuses instead of falling back to local state", func(t *testing.T) {
		h2 := NewHandler()
		h2.SetWorkDir(t.TempDir())
		store, err := projects.NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
		if err != nil {
			t.Fatalf("projects store: %v", err)
		}
		h2.projects = store
		// A registry whose factory always errors: this test must never reach a
		// connect, because admission denies an unsaved host first. If it does
		// reach one, the factory error surfaces instead of a silent success.
		h2.remoteHosts = newTestRegistry(nil)

		w := httptest.NewRecorder()
		// No saved project for this host, so admission denies before any connect.
		r := httptest.NewRequest("GET", "/api/remote/james@box/api/auth/connect", nil)
		setPathValues(r, map[string]string{"host": "james@box", "rest": "auth/connect"})
		h2.HandleRemoteProxy(w, r)

		if w.Code == http.StatusOK {
			t.Fatalf("host-scoped connect answered 200; body: %s", w.Body.String())
		}
		if strings.Contains(w.Body.String(), remoteLeakCanary) {
			t.Fatalf("host-scoped connect leaked a LOCAL credential: %s", w.Body.String())
		}
		// Assert the refusal's SHAPE, not its prose: two admission branches exist
		// (with and without a project path) and each words its own message, so
		// pinning the string would break on a harmless reword. What matters is
		// that it is a 403 carrying a JSON error and nothing else.
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 admission refusal, got %d: %s", w.Code, w.Body.String())
		}
		var errBody struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &errBody); err != nil || errBody.Error == "" {
			t.Fatalf("expected a JSON error envelope, got %s (unmarshal err=%v)", w.Body.String(), err)
		}
	})
}
