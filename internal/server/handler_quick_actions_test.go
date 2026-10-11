package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

const quickActionsPath = "/api/config/ocode/quick-actions"

func quickActionsGet(t *testing.T, h *Handler) config.QuickActionsConfig {
	t.Helper()
	w := httptest.NewRecorder()
	h.HandleGetQuickActionsConfig(w, httptest.NewRequest(http.MethodGet, quickActionsPath, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET = %d body=%s", w.Code, w.Body.String())
	}
	var resp config.QuickActionsConfig
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	return resp
}

func quickActionsPut(t *testing.T, h *Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.HandleSetQuickActionsConfig(w, httptest.NewRequest(http.MethodPut, quickActionsPath, strings.NewReader(body)))
	return w
}

// A fresh install has never touched settings, so the key is absent. GET must
// still report the three shipped pills, byte-identical to the Go seed, in the
// shipped order — nothing is persisted as a side effect of asking.
func TestHandleGetQuickActionsSeedsWhenAbsent(t *testing.T) {
	h := testConfigHandler(t)
	resp := quickActionsGet(t, h)
	want := config.SeedQuickActions()
	if len(resp.Chips) != len(want.Chips) {
		t.Fatalf("seeded %d chips, want %d: %+v", len(resp.Chips), len(want.Chips), resp.Chips)
	}
	for i := range want.Chips {
		if resp.Chips[i] != want.Chips[i] {
			t.Errorf("chip %d = %+v, want %+v", i, resp.Chips[i], want.Chips[i])
		}
	}
}

// Order IS the user's sort order, so the round trip must preserve it exactly,
// not sort by id and not drop a field on the way through.
func TestHandleSetQuickActionsRoundTrips(t *testing.T) {
	h := testConfigHandler(t)
	body := `{"chips":[
		{"id":"tests","label":"Run tests","icon":"flask-conical","message":"run the test suite","mode":"fill"},
		{"id":"recap","label":"Recap","icon":"file-text","message":"/recap","mode":"send","seed":"recap"}
	]}`
	if w := quickActionsPut(t, h, body); w.Code != http.StatusOK {
		t.Fatalf("PUT = %d body=%s", w.Code, w.Body.String())
	}

	want := []config.QuickActionChip{
		{ID: "tests", Label: "Run tests", Icon: "flask-conical", Message: "run the test suite", Mode: config.QuickActionModeFill},
		{ID: "recap", Label: "Recap", Icon: "file-text", Message: "/recap", Mode: config.QuickActionModeSend, Seed: config.QuickActionSeedRecap},
	}
	got := quickActionsGet(t, h)
	if len(got.Chips) != len(want) {
		t.Fatalf("round trip returned %d chips, want %d: %+v", len(got.Chips), len(want), got.Chips)
	}
	for i := range want {
		if got.Chips[i] != want[i] {
			t.Errorf("chip %d = %+v, want %+v (order or field lost)", i, got.Chips[i], want[i])
		}
	}
}

// An omitted mode normalizes to send and an omitted icon to the default, so a
// minimal client payload is accepted and still round-trips fully resolved.
func TestHandleSetQuickActionsNormalizesOmittedModeAndIcon(t *testing.T) {
	h := testConfigHandler(t)
	if w := quickActionsPut(t, h, `{"chips":[{"id":"a","label":"A","message":"m"}]}`); w.Code != http.StatusOK {
		t.Fatalf("PUT = %d body=%s", w.Code, w.Body.String())
	}
	got := quickActionsGet(t, h)
	if len(got.Chips) != 1 {
		t.Fatalf("chips = %+v", got.Chips)
	}
	if got.Chips[0].Mode != config.QuickActionModeSend || got.Chips[0].Icon != config.QuickActionDefaultIcon {
		t.Fatalf("chip not normalized: %+v", got.Chips[0])
	}
}

func itoaTest(i int) string { return strconv.Itoa(i) }

// Review Focus #4: two settings panes each add one and land on 21. The 21st
// must be EXPLAINED, never silently truncated — a silent truncate loses the
// user's chip with no error at all.
func TestHandleSetQuickActionsRejectsOverCapWithAReadableMessage(t *testing.T) {
	h := testConfigHandler(t)
	var sb strings.Builder
	sb.WriteString(`{"chips":[`)
	for i := 0; i < config.QuickActionsMaxChips+1; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"id":"c`)
		sb.WriteString(string(rune('a' + i%26)))
		sb.WriteString(itoaTest(i))
		sb.WriteString(`","label":"C","icon":"zap","message":"m","mode":"send"}`)
	}
	sb.WriteString(`]}`)
	w := quickActionsPut(t, h, sb.String())
	if w.Code != http.StatusBadRequest {
		t.Fatalf("PUT %d chips = %d, want 400 body=%s", config.QuickActionsMaxChips+1, w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "at most 20") {
		t.Fatalf("error body %q does not explain the cap", w.Body.String())
	}
	// The rejected save must not have replaced the previous state.
	if got := quickActionsGet(t, h); len(got.Chips) != len(config.SeedQuickActions().Chips) {
		t.Fatalf("rejected PUT mutated stored config: %+v", got.Chips)
	}
}

// Exactly at the cap is legal — only >20 is rejected. Pinning the boundary
// stops a future "off by one" from silently rejecting a full legitimate strip.
func TestHandleSetQuickActionsAcceptsExactlyTheCap(t *testing.T) {
	h := testConfigHandler(t)
	var sb strings.Builder
	sb.WriteString(`{"chips":[`)
	for i := 0; i < config.QuickActionsMaxChips; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"id":"c`)
		sb.WriteString(string(rune('a' + i%26)))
		sb.WriteString(itoaTest(i))
		sb.WriteString(`","label":"C","icon":"zap","message":"m","mode":"send"}`)
	}
	sb.WriteString(`]}`)
	if w := quickActionsPut(t, h, sb.String()); w.Code != http.StatusOK {
		t.Fatalf("PUT %d chips = %d, want 200 body=%s", config.QuickActionsMaxChips, w.Code, w.Body.String())
	}
	if got := quickActionsGet(t, h); len(got.Chips) != config.QuickActionsMaxChips {
		t.Fatalf("stored %d chips, want %d", len(got.Chips), config.QuickActionsMaxChips)
	}
}

func TestHandleSetQuickActionsRejectsInvalidBody(t *testing.T) {
	for name, body := range map[string]string{
		"bad json":        `{`,
		"bad icon":        `{"chips":[{"id":"a","label":"A","icon":"nope","message":"m","mode":"send"}]}`,
		"bad mode":        `{"chips":[{"id":"a","label":"A","icon":"zap","message":"m","mode":"sideways"}]}`,
		"bad seed":        `{"chips":[{"id":"a","label":"A","icon":"zap","message":"m","mode":"send","seed":"teleport"}]}`,
		"dup id":          `{"chips":[{"id":"a","label":"A","icon":"zap","message":"m","mode":"send"},{"id":"a","label":"B","icon":"zap","message":"m","mode":"send"}]}`,
		"reserved dup":    `{"chips":[{"id":"recap","label":"Recap","icon":"file-text","message":"/recap","mode":"send","seed":"recap"},{"id":"recap","label":"Mine","icon":"zap","message":"m","mode":"send"}]}`,
		"blank msg":       `{"chips":[{"id":"a","label":"A","icon":"zap","message":"   ","mode":"send"}]}`,
		"blank label":     `{"chips":[{"id":"a","label":"\t","icon":"zap","message":"m","mode":"send"}]}`,
		"empty id":        `{"chips":[{"id":"","label":"A","icon":"zap","message":"m","mode":"send"}]}`,
		"unknown f":       `{"chips":[{"id":"a","label":"A","icon":"zap","message":"m","mode":"send","colour":"red"}]}`,
		"unknown top f":   `{"chip":[{"id":"a","label":"A","icon":"zap","message":"m","mode":"send"}]}`,
		"chip is not obj": `{"chips":["nope"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			h := testConfigHandler(t)
			w := quickActionsPut(t, h, body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("PUT %s = %d, want 400 body=%s", name, w.Code, w.Body.String())
			}
		})
	}
}

// "I deleted every chip" is a legitimate state and means no strip. This also
// pins the nil-vs-empty distinction: a non-nil EMPTY list must NOT fall back to
// the seeds, or a user who removed every pill gets them all back.
func TestHandleSetQuickActionsAcceptsEmptyList(t *testing.T) {
	h := testConfigHandler(t)
	if w := quickActionsPut(t, h, `{"chips":[]}`); w.Code != http.StatusOK {
		t.Fatalf("PUT empty = %d body=%s", w.Code, w.Body.String())
	}
	got := quickActionsGet(t, h)
	if len(got.Chips) != 0 {
		t.Fatalf("empty save resurrected %d chips: %+v", len(got.Chips), got.Chips)
	}
	if got.Chips == nil {
		t.Fatal("empty save stored a nil slice; that re-arms the seed path on the next GET")
	}
}

// A persisted empty strip must survive a fresh Handler over the same HOME, so
// "no strip" is durable rather than a per-process illusion.
func TestHandleGetQuickActionsKeepsPersistedEmptyStrip(t *testing.T) {
	h := testConfigHandler(t)
	if w := quickActionsPut(t, h, `{"chips":[]}`); w.Code != http.StatusOK {
		t.Fatalf("PUT empty = %d body=%s", w.Code, w.Body.String())
	}
	// Reload from disk (the same isolated HOME the PUT wrote) into a fresh
	// handler. Deliberately NOT testConfigHandler: it calls t.Setenv with a NEW
	// temp HOME, which would read an empty config and pass vacuously.
	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	h2 := NewHandler()
	h2.mu.Lock()
	h2.cfg = loaded
	h2.mu.Unlock()
	if got := quickActionsGet(t, h2); len(got.Chips) != 0 {
		t.Fatalf("reloaded strip has %d chips, want 0: %+v", len(got.Chips), got.Chips)
	}
}
