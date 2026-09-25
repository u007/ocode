package server

import (
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/lsp"
)

func TestFilterLSPStatusesByRootCanonicalizesAndRewritesDisplayRoot(t *testing.T) {
	root := t.TempDir()
	canonical := canonicalLSPProjectRoot(root)
	got := filterLSPStatusesByRoot([]LSPStatus{
		{Cmd: "gopls", Root: canonical},
		{Cmd: "clangd", Root: filepath.Join(root, "..", "other")},
	}, root)
	if len(got) != 1 {
		t.Fatalf("filtered LSP status count = %d, want 1", len(got))
	}
	if got[0].Cmd != "gopls" {
		t.Fatalf("filtered server = %q, want gopls", got[0].Cmd)
	}
	if got[0].Root != root {
		t.Fatalf("filtered display root = %q, want %q", got[0].Root, root)
	}
}

func TestLSPManagerForReusesCanonicalRoot(t *testing.T) {
	h := NewHandler()
	root := t.TempDir()
	alias := filepath.Join(filepath.Dir(root), filepath.Base(root)+string(filepath.Separator)+".")

	first := h.lspManagerFor(root)
	second := h.lspManagerFor(alias)
	if first != second {
		t.Fatalf("equivalent roots created separate managers: %p and %p", first, second)
	}
	first.Close()
}

func TestHandleSessionStatusCreatesProjectLSPManager(t *testing.T) {
	h := NewHandler()
	root := t.TempDir()
	const sessionID = "ses-cold-lsp-status"

	h.sessions.Register(sessionID, root)
	rec := httptest.NewRecorder()
	h.HandleSessionStatus(rec, httptest.NewRequest("GET", "/api/sessions/"+sessionID+"/status", nil), sessionID)
	if rec.Code != 200 {
		t.Fatalf("status code = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	h.lspMu.Lock()
	if len(h.lspMgrs) != 1 {
		h.lspMu.Unlock()
		t.Fatalf("LSP manager count = %d, want 1 for a cold session status request", len(h.lspMgrs))
	}
	wantRoot := canonicalLSPProjectRoot(root)
	managers := make([]*lsp.Manager, 0, len(h.lspMgrs))
	for key, mgr := range h.lspMgrs {
		if filepath.Clean(key) != filepath.Clean(wantRoot) {
			h.lspMu.Unlock()
			mgr.Close()
			t.Fatalf("LSP manager key = %q, want %q", key, wantRoot)
		}
		if filepath.Clean(mgr.Root()) != filepath.Clean(wantRoot) {
			h.lspMu.Unlock()
			mgr.Close()
			t.Fatalf("LSP manager root = %q, want %q", mgr.Root(), wantRoot)
		}
		managers = append(managers, mgr)
	}
	h.lspMu.Unlock()
	for _, mgr := range managers {
		mgr.Close()
	}
}
