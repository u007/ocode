package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/projects"
)

// Deleting a profile must be refused while a project is still bound to it: the
// binding would otherwise outlive the profile, resolveSessionProfile would keep
// returning the dead name, and the project would silently run on the base
// credentials.
func TestDeleteProfileRefusedWhileProjectBound(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h := NewHandler()
	proj := t.TempDir()
	store, err := projects.NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if err := store.Add(proj); err != nil {
		t.Fatal(err)
	}
	if err := store.SetProfileRef(projects.ProjectRef{Path: proj}, "work"); err != nil {
		t.Fatal(err)
	}
	h.projects = store

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/api/profiles/work", nil)
	r.SetPathValue("name", "work")
	h.handleDeleteProfile(w, r)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body: %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "bound to 1 project") {
		t.Fatalf("body = %q, want the binding explanation", body)
	}
	if got := store.ProfileFor(projects.ProjectRef{Path: proj}); got != "work" {
		t.Fatalf("binding = %q after refused delete, want work", got)
	}
}
