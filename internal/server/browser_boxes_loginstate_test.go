package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/boxsession"
	"github.com/u007/ocode/internal/browse/cdp"
	"github.com/u007/ocode/internal/config"
)

// newLoginBoxManager builds a box manager over api with login states on: the
// store is the given one, and every session's login state is lookup's answer.
func newLoginBoxManager(t *testing.T, api *fakeBoxAPI, label string, store *boxsession.LoginStateStore, lookup func(string) (string, error)) *boxsession.Manager {
	t.Helper()
	path, err := upstashBoxJournalPath()
	if err != nil {
		t.Fatal(err)
	}
	return boxsession.New(boxsession.Deps{
		Account: func() (boxsession.Account, error) {
			return boxsession.Account{ID: testBoxAccount, APIKey: "ubx-test-key", Idle: time.Hour, HardCap: 2 * time.Hour}, nil
		},
		APIKeyFor: func(string) (string, error) { return "ubx-test-key", nil },
		NewAPI:    func(string) (boxsession.BoxAPI, error) { return api, nil },
		Dial: func(context.Context, string) (*cdp.Conn, error) {
			return nil, errors.New("tests do not dial a browser")
		},
		Busy:          func(string) bool { return false },
		Journal:       boxsession.NewJournal(path),
		Label:         label,
		LoginStates:   store,
		LoginStateFor: lookup,
	})
}

// TestBoxLoginStateForReadsTheSessionsProject: a session's box uses the login state
// assigned to its own project. An unassigned project means none, and an unknown
// session is an error, never a silent none.
func TestBoxLoginStateForReadsTheSessionsProject(t *testing.T) {
	boxTestHome(t)
	h := NewHandler()
	assigned := t.TempDir()
	plain := t.TempDir()
	h.sessions.Register("ses_assigned", assigned)
	h.sessions.Register("ses_plain", plain)
	if err := config.MutateUpstashBox(func(c *config.UpstashBoxConfig) error {
		return c.SetLoginState(assigned, "work")
	}); err != nil {
		t.Fatal(err)
	}

	if got, err := h.boxLoginStateFor("ses_assigned"); err != nil || got != "work" {
		t.Fatalf("assigned project = %q, %v; want work", got, err)
	}
	if got, err := h.boxLoginStateFor("ses_plain"); err != nil || got != "" {
		t.Fatalf("unassigned project = %q, %v; want none", got, err)
	}
	if _, err := h.boxLoginStateFor("ses_unknown"); err == nil {
		t.Fatal("an unknown session resolved a login state without an error")
	}
}

// TestBoxLoginStateForRefusesUnreadableSettings: an unreadable settings file is an
// error, so a start cannot quietly run with the wrong login state.
func TestBoxLoginStateForRefusesUnreadableSettings(t *testing.T) {
	boxTestHome(t)
	h := NewHandler()
	root := t.TempDir()
	h.sessions.Register("ses_one", root)
	path, err := config.ActiveOcodeConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := h.boxLoginStateFor("ses_one")
	if err == nil {
		t.Fatalf("unreadable settings: login state = %q with no error, want an error", got)
	}
}

// TestBoxManagerDepsShareTheLoginStateStore: the box manager gets the store the
// login-state routes use, and the session resolver, so one directory serves both.
func TestBoxManagerDepsShareTheLoginStateStore(t *testing.T) {
	boxTestHome(t)
	store := boxsession.NewLoginStateStore(filepath.Join(t.TempDir(), "login-states"))
	prev := upstashLoginStoreFor
	upstashLoginStoreFor = func() (*boxsession.LoginStateStore, error) { return store, nil }
	t.Cleanup(func() { upstashLoginStoreFor = prev })

	h := NewHandler()
	root := t.TempDir()
	h.sessions.Register("ses_one", root)
	if err := config.MutateUpstashBox(func(c *config.UpstashBoxConfig) error {
		return c.SetLoginState(root, "work")
	}); err != nil {
		t.Fatal(err)
	}
	deps, err := h.boxManagerDeps(boxsession.NewJournal(filepath.Join(t.TempDir(), "j.json")), "ocode-aaaaaaaa")
	if err != nil {
		t.Fatalf("boxManagerDeps: %v", err)
	}
	if deps.LoginStates != store {
		t.Fatal("the box manager's login-state store is not the routes' store")
	}
	if deps.LoginStateFor == nil {
		t.Fatal("the box manager has no login-state resolver")
	}
	if got, err := deps.LoginStateFor("ses_one"); err != nil || got != "work" {
		t.Fatalf("resolver = %q, %v; want work", got, err)
	}
}

// TestBoxManagerDepsFailWhenTheStoreCannotResolve: a store that cannot be built is
// an error, so the box manager is never built with login states off.
func TestBoxManagerDepsFailWhenTheStoreCannotResolve(t *testing.T) {
	boxTestHome(t)
	prev := upstashLoginStoreFor
	upstashLoginStoreFor = func() (*boxsession.LoginStateStore, error) {
		return nil, errors.New("no data directory")
	}
	t.Cleanup(func() { upstashLoginStoreFor = prev })

	_, err := NewHandler().boxManagerDeps(boxsession.NewJournal(filepath.Join(t.TempDir(), "j.json")), "ocode-aaaaaaaa")
	if err == nil || !strings.Contains(err.Error(), "no data directory") {
		t.Fatalf("boxManagerDeps = %v, want the store error", err)
	}
}

// TestBrowserBoxes_StartNamesARestoredLoginState: a new box with a login state says
// so in the start result. A reused box does not repeat it.
func TestBrowserBoxes_StartNamesARestoredLoginState(t *testing.T) {
	box := &fakeBoxes{status: browserBoxStatus{BoxID: "box-1", LoginState: "work"}}
	b, _, _, _ := boxSetup(t, box)
	ctx := boxSession("sess-1")

	out, err := b.ExecuteCtx(ctx, json.RawMessage(`{"action":"start","backend":"upstash"}`))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	want := `Login state "work" is restored: cookies now, site storage on first visit to each site.`
	if !strings.Contains(out, want) {
		t.Fatalf("start result lacks the login-state sentence %q: %s", want, out)
	}

	box.status.Reused = true
	out, err = b.ExecuteCtx(ctx, json.RawMessage(`{"action":"start","backend":"upstash"}`))
	if err != nil || strings.Contains(out, "is restored") {
		t.Fatalf("reused start = %q, %v; want no restore sentence", out, err)
	}
}

// TestBrowserBoxes_StartWithoutLoginStateSaysNothing: a box with no login state
// gets no login-state sentence.
func TestBrowserBoxes_StartWithoutLoginStateSaysNothing(t *testing.T) {
	box := &fakeBoxes{status: browserBoxStatus{BoxID: "box-1"}}
	b, _, _, _ := boxSetup(t, box)
	out, err := b.ExecuteCtx(boxSession("sess-1"), json.RawMessage(`{"action":"start","backend":"upstash"}`))
	if err != nil || strings.Contains(out, "Login state") {
		t.Fatalf("start = %q, %v; want no login-state sentence", out, err)
	}
}

// TestUpstashBoxesListCarriesTheLoginState: a running box of this installation shows
// the login state it was started with, and every other box shows the empty string.
func TestUpstashBoxesListCarriesTheLoginState(t *testing.T) {
	mux, h := upstashTestServer(t)
	mux.HandleFunc("GET /api/upstash/boxes", h.HandleUpstashBoxesList)
	addTestBoxAccount(t, testBoxAccount, "main")

	label, err := h.boxInstallationLabel()
	if err != nil {
		t.Fatal(err)
	}
	store := boxsession.NewLoginStateStore(filepath.Join(t.TempDir(), "login-states"))
	api := &fakeBoxAPI{}
	h.boxes = newLoginBoxManager(t, api, label, store, func(string) (string, error) { return "work", nil })
	if _, err := h.boxes.Start(context.Background(), "ses_one", boxsession.StartOptions{}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	fake := &fakeUpstashBoxAPI{boxes: []map[string]any{
		wireBox("box-1", now.Add(-time.Hour), true, []string{label}),
		wireBox("box-other", now.Add(-2*time.Hour), false, []string{"someone-else"}),
	}}
	wireUpstashBoxClient(t, h, fake)

	code, raw := upstashDo(t, mux, http.MethodGet, "/api/upstash/boxes", nil)
	if code != http.StatusOK {
		t.Fatalf("list status = %d (%s)", code, raw)
	}
	_, body := listedBoxes(t, raw)
	got := map[string]any{}
	for _, b := range body["boxes"].([]any) {
		m := b.(map[string]any)
		v, ok := m["login_state"]
		if !ok {
			t.Fatalf("box %v has no login_state field", m["id"])
		}
		got[m["id"].(string)] = v
	}
	if got["box-1"] != "work" || got["box-other"] != "" {
		t.Fatalf("login_state values = %v, want box-1=work and box-other empty", got)
	}
}

// TestBrowserBoxes_StartReportsLoginStateNotes: what the restore could not apply
// is stated in the start result, after the restore sentence.
func TestBrowserBoxes_StartReportsLoginStateNotes(t *testing.T) {
	box := &fakeBoxes{status: browserBoxStatus{
		BoxID:           "box-1",
		LoginState:      "work",
		LoginStateNotes: []string{"3 cookies were rejected by the browser", "site storage for https://big.test was too large and was not restored"},
	}}
	b, _, _, _ := boxSetup(t, box)
	out, err := b.ExecuteCtx(boxSession("sess-1"), json.RawMessage(`{"action":"start","backend":"upstash"}`))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	for _, want := range []string{
		"Login state note: 3 cookies were rejected by the browser.",
		"Login state note: site storage for https://big.test was too large and was not restored.",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("start result lacks %q: %s", want, out)
		}
	}
}
