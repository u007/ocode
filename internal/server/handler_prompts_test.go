package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// promptsTestHandler isolates HOME and starts from an empty live override
// store, so no override saved by an earlier test leaks into this one.
func promptsTestHandler(t *testing.T) *Handler {
	t.Helper()
	setHomeTree(t, t.TempDir())
	config.SetPromptOverrides(nil)
	t.Cleanup(func() { config.SetPromptOverrides(nil) })
	return &Handler{}
}

func listPromptRows(t *testing.T, h *Handler) []promptOverrideRow {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/config/prompts", nil)
	h.HandleListPromptOverrides(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Prompts []promptOverrideRow `json:"prompts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	return out.Prompts
}

func putPromptOverride(t *testing.T, h *Handler, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/api/config/prompts/"+key, strings.NewReader(body))
	r.SetPathValue("key", key)
	h.HandleSetPromptOverride(w, r)
	return w
}

func TestListPromptOverridesReturnsCatalogSorted(t *testing.T) {
	h := promptsTestHandler(t)

	rows := listPromptRows(t, h)
	if len(rows) != 21 {
		t.Fatalf("got %d prompts, want 21", len(rows))
	}
	if !sort.SliceIsSorted(rows, func(i, j int) bool { return rows[i].Key < rows[j].Key }) {
		t.Errorf("prompts not sorted by key")
	}
	for _, row := range rows {
		if row.Kind == "" {
			t.Errorf("prompt %q has empty kind", row.Key)
		}
		if row.Enabled || row.Text != "" {
			t.Errorf("prompt %q should start with no override, got enabled=%v text=%q", row.Key, row.Enabled, row.Text)
		}
	}
}

func TestListPromptOverridesOmitsBuiltinText(t *testing.T) {
	h := promptsTestHandler(t)

	w := httptest.NewRecorder()
	h.HandleListPromptOverrides(w, httptest.NewRequest(http.MethodGet, "/api/config/prompts", nil))
	if strings.Contains(w.Body.String(), `"builtin"`) {
		t.Errorf("list response carries built-in prompt text; the UI only needs kind")
	}
}

func TestSetPromptOverrideThenListShowsIt(t *testing.T) {
	h := promptsTestHandler(t)

	w := putPromptOverride(t, h, "title", `{"enabled":true,"text":"Keep titles under six words."}`)
	if w.Code != http.StatusOK {
		t.Fatalf("put status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	var put promptOverrideRow
	if err := json.Unmarshal(w.Body.Bytes(), &put); err != nil {
		t.Fatalf("decode put: %v", err)
	}
	if put.Key != "title" || !put.Enabled || put.Text != "Keep titles under six words." {
		t.Errorf("put response = %+v, want enabled title override", put)
	}

	for _, row := range listPromptRows(t, h) {
		if row.Key != "title" {
			continue
		}
		if !row.Enabled || row.Text != "Keep titles under six words." {
			t.Errorf("list row for title = %+v, want enabled override", row)
		}
		return
	}
	t.Fatalf("title missing from list")
}

func TestSetPromptOverrideUnknownKeyIs404(t *testing.T) {
	h := promptsTestHandler(t)

	w := putPromptOverride(t, h, "no_such_prompt", `{"enabled":true,"text":"x"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body=%s", w.Code, w.Body.String())
	}
	if _, ok := config.PromptOverrideFor("no_such_prompt"); ok {
		t.Errorf("unknown key was stored in the live override store")
	}
}

func TestSetPromptOverrideInvalidBodyIs400(t *testing.T) {
	h := promptsTestHandler(t)

	w := putPromptOverride(t, h, "title", `{not json`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
}

func TestSetPromptOverrideDisabledAndBlankClearsIt(t *testing.T) {
	h := promptsTestHandler(t)

	if w := putPromptOverride(t, h, "title", `{"enabled":true,"text":"Be brief."}`); w.Code != http.StatusOK {
		t.Fatalf("seed put status = %d, body=%s", w.Code, w.Body.String())
	}

	w := putPromptOverride(t, h, "title", `{"enabled":false,"text":""}`)
	if w.Code != http.StatusOK {
		t.Fatalf("clear put status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if _, ok := config.PromptOverrideFor("title"); ok {
		t.Errorf("cleared override still present in the live store")
	}
	for _, row := range listPromptRows(t, h) {
		if row.Key == "title" && (row.Enabled || row.Text != "") {
			t.Errorf("list row for title after clear = %+v, want disabled and empty", row)
		}
	}
}

func TestSetPromptOverrideOversizeTextIs400(t *testing.T) {
	h := promptsTestHandler(t)

	body := `{"enabled":true,"text":"` + strings.Repeat("a", config.MaxPromptOverrideBytes+1) + `"}`
	w := putPromptOverride(t, h, "title", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%.200s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "byte limit") {
		t.Errorf("400 body does not name the limit: %s", w.Body.String())
	}
	if _, ok := config.PromptOverrideFor("title"); ok {
		t.Errorf("oversize override reached the live store")
	}
}
