package commandctx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupDocsBundle creates a minimal OKF bundle with one active doc and,
// optionally, one doc already marked deprecated.
func setupDocsBundle(t *testing.T, withDeprecated bool) (workDir, deprecatedPath string) {
	t.Helper()
	workDir = t.TempDir()
	docsDir := filepath.Join(workDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatal(err)
	}
	index := "---\nokf_version: \"0.1\"\n---\n"
	if err := os.WriteFile(filepath.Join(docsDir, "index.md"), []byte(index), 0644); err != nil {
		t.Fatal(err)
	}

	active := "---\ntype: concept\ntitle: Active\ndescription: Still current\n---\nBody\n"
	if err := os.WriteFile(filepath.Join(docsDir, "active.md"), []byte(active), 0644); err != nil {
		t.Fatal(err)
	}

	if withDeprecated {
		deprecatedPath = filepath.Join(docsDir, "gone.md")
		gone := "---\ntype: concept\ntitle: Gone\ndescription: Obsolete\nstatus: deprecated\ndeprecated_reason: superseded by active.md\n---\nBody\n"
		if err := os.WriteFile(deprecatedPath, []byte(gone), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return workDir, deprecatedPath
}

// TestDocsCleanupWithoutConfirmDispatchesAudit pins the LLM-driven contract:
// cleanup without --yes never deletes and never assumes a preset deprecation
// list; it returns the context-agent audit prompt instead.
func TestDocsCleanupWithoutConfirmDispatchesAudit(t *testing.T) {
	workDir, deprecatedPath := setupDocsBundle(t, false)

	text, prompt := DocsCleanup(workDir, false)
	if prompt == "" {
		t.Fatalf("expected an audit prompt, got none (text=%q)", text)
	}
	for _, want := range []string{"doc_search", "doc_deprecate", "/docs cleanup --yes"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("audit prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "Do NOT delete") == false && !strings.Contains(prompt, "Never delete") {
		t.Errorf("audit prompt does not forbid deletion:\n%s", prompt)
	}
	if !strings.Contains(text, "Auditing") {
		t.Errorf("text = %q, want an auditing notice", text)
	}
	if deprecatedPath != "" {
		if _, err := os.Stat(deprecatedPath); err != nil {
			t.Errorf("deprecated doc removed without --yes: %v", err)
		}
	}
	// Nothing was deleted, and the audit did not mutate the bundle.
	if _, err := os.Stat(filepath.Join(workDir, "docs", "active.md")); err != nil {
		t.Errorf("active doc removed: %v", err)
	}
}

// TestDocsCleanupConfirmDeletesDeprecated pins the deterministic, human-gated
// deletion step that stays after the LLM audit.
func TestDocsCleanupConfirmDeletesDeprecated(t *testing.T) {
	workDir, deprecatedPath := setupDocsBundle(t, true)

	text, prompt := DocsCleanup(workDir, true)
	if prompt != "" {
		t.Errorf("confirm path must not dispatch a prompt, got %q", prompt)
	}
	if !strings.Contains(text, "Deleted 1") {
		t.Errorf("text = %q, want a deletion count", text)
	}
	if _, err := os.Stat(deprecatedPath); !os.IsNotExist(err) {
		t.Errorf("deprecated doc still on disk (err=%v)", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, "docs", "active.md")); err != nil {
		t.Errorf("active doc removed: %v", err)
	}
}

func TestDocsCleanupConfirmWithNothingDeprecated(t *testing.T) {
	workDir, _ := setupDocsBundle(t, false)

	text, prompt := DocsCleanup(workDir, true)
	if prompt != "" {
		t.Errorf("confirm path must not dispatch a prompt, got %q", prompt)
	}
	if !strings.Contains(text, "No deprecated documents") {
		t.Errorf("text = %q, want the no-op message", text)
	}
}

func TestDocsCleanupWithoutBundle(t *testing.T) {
	text, prompt := DocsCleanup(t.TempDir(), false)
	if prompt != "" {
		t.Errorf("no bundle must not dispatch a prompt, got %q", prompt)
	}
	if !strings.Contains(text, "No OKF knowledge bundle") {
		t.Errorf("text = %q, want the missing-bundle message", text)
	}
}
