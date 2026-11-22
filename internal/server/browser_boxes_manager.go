package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/boxsession"
	"github.com/u007/ocode/internal/browse/cdp"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/crashguard"
	"github.com/u007/ocode/internal/paths"
	"github.com/u007/ocode/internal/upstashbox"
)

// This file wires the Upstash Box browser backend into the web/desktop server:
// the adapter from one box's *cdp.Manager to browserSurfaces, the browserBoxes
// the browser tool consumes, and the Handler's box manager with its lifecycle.

// upstashBoxJournalFile is the journal's name inside the global data directory.
const upstashBoxJournalFile = "upstash-boxes.json"

const (
	// boxLoadCap bounds the wait for a navigated box page to reach readyState
	// "complete". It mirrors the browse package's agentLoadCap, which is unexported.
	boxLoadCap = 10 * time.Second
	// boxLoadPoll is the readyState poll interval while a box page loads.
	boxLoadPoll = 100 * time.Millisecond
	// boxDiscardTimeout bounds the CloseTarget that discards a blank box tab whose
	// first navigation failed. It mirrors the browse package's agentCleanupTimeout.
	boxDiscardTimeout = 5 * time.Second
	// boxCloseTimeout bounds the box teardown a session close runs.
	boxCloseTimeout = 15 * time.Second
	// boxShutdownCap bounds the box teardown at server shutdown.
	boxShutdownCap = 10 * time.Second
)

// boxChrome is the part of a box's *cdp.Manager the surfaces use.
type boxChrome interface {
	EnsureHeadless(ctx context.Context, stateKey string) (*cdp.Target, bool, error)
	OpenHeadless(ctx context.Context) (*cdp.Target, string, error)
	CloseTarget(ctx context.Context, stateKey string) error
}

var _ boxChrome = (*cdp.Manager)(nil)

// boxPage is the page operations a newly attached box target needs for its
// stored-url load. *cdp.Target satisfies it; tests use a fake.
type boxPage interface {
	Navigate(ctx context.Context, url string) error
	ReadyState(ctx context.Context) (string, error)
}

var _ boxPage = (*cdp.Target)(nil)

// boxSurfaces adapts one box's Chrome to the browser tool's browserSurfaces. It
// mirrors browse.Server's AgentTarget, OpenAgentTab and CloseAgentTab.
type boxSurfaces struct {
	chrome boxChrome
}

// Surface returns the box's target for stateKey. A target this call attaches is
// loaded with url when url is http(s), and discarded again if that load fails.
func (s boxSurfaces) Surface(ctx context.Context, stateKey, url string) (browserSurface, string, error) {
	t, created, err := s.chrome.EnsureHeadless(ctx, stateKey)
	if err != nil {
		return nil, "", err
	}
	if !created || !(strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")) {
		return cdpAgentSurface{Target: t}, "", nil
	}
	note, err := s.loadAttached(ctx, stateKey, url, t)
	if err != nil {
		return nil, "", err
	}
	return cdpAgentSurface{Target: t}, note, nil
}

// loadAttached navigates a target this call attached to url and waits for the
// load. A load that fails, or is cut off by ctx, discards the target. A load that
// is still running at boxLoadCap is not an error: the note says so.
func (s boxSurfaces) loadAttached(ctx context.Context, stateKey, url string, p boxPage) (string, error) {
	if err := p.Navigate(ctx, url); err != nil {
		return "", s.discard(ctx, stateKey, fmt.Errorf("browse: load stored url into %s: %w", stateKey, err))
	}
	loaded, err := waitForBoxLoad(ctx, p)
	if err != nil {
		return "", s.discard(ctx, stateKey, fmt.Errorf("browse: wait for %s to load %s: %w", stateKey, url, err))
	}
	if !loaded {
		return fmt.Sprintf("the page did not finish loading within %s; take a screenshot to check it", boxLoadCap), nil
	}
	return "", nil
}

// discard closes a box target this call attached and returns the failure that
// made it necessary. The close runs on a fresh bounded context: the caller's ctx
// is often the reason the load failed.
func (s boxSurfaces) discard(ctx context.Context, stateKey string, loadErr error) error {
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), boxDiscardTimeout)
	defer cancel()
	if err := s.chrome.CloseTarget(cctx, stateKey); err != nil {
		return errors.Join(loadErr, fmt.Errorf("browse: discard blank tab %s: %w", stateKey, err))
	}
	return loadErr
}

// waitForBoxLoad polls the top document's readyState until it is "complete". At
// boxLoadCap it reports false with no error. A cancelled ctx ends the wait with
// ctx's error.
func waitForBoxLoad(ctx context.Context, p boxPage) (bool, error) {
	capCtx, cancel := context.WithTimeout(ctx, boxLoadCap)
	defer cancel()
	for {
		state, err := p.ReadyState(capCtx)
		if err != nil {
			if capCtx.Err() != nil && ctx.Err() == nil {
				return false, nil
			}
			return false, err
		}
		if state == "complete" {
			return true, nil
		}
		select {
		case <-capCtx.Done():
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			return false, nil
		case <-time.After(boxLoadPoll):
		}
	}
}

// Open opens a new headless tab on the box, navigated to url when non-empty.
func (s boxSurfaces) Open(ctx context.Context, url string) (string, error) {
	t, stateKey, err := s.chrome.OpenHeadless(ctx)
	if err != nil {
		return "", err
	}
	if url != "" {
		if err := t.Navigate(ctx, url); err != nil {
			return stateKey, fmt.Errorf("browse: open %s at %s: %w", stateKey, url, err)
		}
	}
	return stateKey, nil
}

// Close closes the box's tab stateKey.
func (s boxSurfaces) Close(ctx context.Context, stateKey string) error {
	return s.chrome.CloseTarget(ctx, stateKey)
}

// handlerBoxes is the browserBoxes the browser tool uses for a session with an
// Upstash box. It is cheap to build: the box manager is resolved lazily inside each
// call, so constructing a session never does box IO.
type handlerBoxes struct {
	h *Handler
}

// Start creates the session's box, or reports the one already running.
func (b handlerBoxes) Start(ctx context.Context, sessionID string, retain bool) (browserBoxStatus, error) {
	mgr, err := b.h.boxManager()
	if err != nil {
		return browserBoxStatus{}, err
	}
	st, err := mgr.Start(ctx, sessionID, boxsession.StartOptions{Retain: retain})
	if err != nil {
		return browserBoxStatus{}, err
	}
	return browserBoxStatus{
		BoxID:           st.BoxID,
		Kind:            st.Kind,
		ExpiresAt:       st.ExpiresAt,
		LoginState:      st.LoginState,
		LoginStateNotes: st.LoginStateNotes,
		Reused:          st.Reused,
		Resumed:         st.Resumed,
	}, nil
}

// Terminate ends the session's box and reports its kind. The kind is read from the
// live status before the teardown, because the manager does not report it after: an
// ephemeral box is deleted and a retained box is paused. With no manager built in
// this process no box can be running, so the call reports ErrNotStarted without
// building one.
func (b handlerBoxes) Terminate(ctx context.Context, sessionID string) (string, error) {
	mgr := b.h.boxManagerIfBuilt()
	if mgr == nil {
		return "", boxsession.ErrNotStarted
	}
	kind := liveBoxKind(mgr, sessionID)
	return kind, mgr.Terminate(ctx, sessionID)
}

// liveBoxKind returns the kind of the box sessionID runs, or "" when it runs none.
func liveBoxKind(mgr *boxsession.Manager, sessionID string) string {
	for _, st := range mgr.Statuses() {
		if st.SessionID == sessionID {
			return st.Kind
		}
	}
	return ""
}

// Surfaces returns the running box's tab surfaces and the box's id. It never creates
// a box, and its errors (ErrNotStarted, ErrExpired) reach the caller unchanged: never
// a fallback. The boxsession Manager's Browser call does not return the box's id, so
// the id is read from Statuses before and after it. A box replaced in between would
// pair the new browser with the old id, so that case is an error.
func (b handlerBoxes) Surfaces(ctx context.Context, sessionID string) (browserSurfaces, string, error) {
	mgr := b.h.boxManagerIfBuilt()
	if mgr == nil {
		return nil, "", boxsession.ErrNotStarted
	}
	before, err := boxIDOf(mgr, sessionID)
	if err != nil {
		// Statuses lists only live boxes. A session whose retained box was paused, or whose
		// ephemeral box expired, has no live row, and only Browser reports that marker. It
		// leaves the marker in place, and it fails for every case boxIDOf does.
		if _, berr := mgr.Browser(sessionID); berr != nil {
			return nil, "", berr
		}
		return nil, "", err
	}
	chrome, err := mgr.Browser(sessionID)
	if err != nil {
		return nil, "", err
	}
	after, err := boxIDOf(mgr, sessionID)
	if err != nil {
		return nil, "", err
	}
	if after != before {
		return nil, "", fmt.Errorf("browser: the session's upstash box changed while its browser was read (%s -> %s); retry", before, after)
	}
	return boxSurfaces{chrome: chrome}, after, nil
}

// boxIDOf returns the id of the running box of sessionID. A session with no running
// box is boxsession.ErrNotStarted, as Browser reports it.
func boxIDOf(mgr *boxsession.Manager, sessionID string) (string, error) {
	for _, st := range mgr.Statuses() {
		if st.SessionID == sessionID {
			return st.BoxID, nil
		}
	}
	return "", boxsession.ErrNotStarted
}

// DefaultBackend is the backend a call uses when it omits backend. An unreadable
// setting is returned as an error: falling back to either backend would send the
// call somewhere the user did not choose.
func (b handlerBoxes) DefaultBackend() (string, error) {
	cfg, err := config.LoadUpstashBoxConfig()
	if err != nil {
		return "", fmt.Errorf("read the Upstash Box settings: %w", err)
	}
	return cfg.Backend(), nil
}

// upstashBoxJournalPath is the journal file in the global data directory.
func upstashBoxJournalPath() (string, error) {
	dir, err := paths.GlobalDataDir()
	if err != nil {
		return "", fmt.Errorf("upstash boxes: resolve the data directory: %w", err)
	}
	return filepath.Join(dir, upstashBoxJournalFile), nil
}

// upstashDefaultBoxAccount resolves the default account for a box start. Its
// errors are the user-facing reasons a start cannot run.
func upstashDefaultBoxAccount() (boxsession.Account, error) {
	acc, ok, err := config.LoadDefaultUpstashBoxAccount()
	if err != nil {
		return boxsession.Account{}, fmt.Errorf("read the Upstash Box settings: %w", err)
	}
	if !ok {
		return boxsession.Account{}, errors.New("no Upstash Box account is configured; add one in Settings")
	}
	key, ok := auth.UpstashBoxAPIKey(acc.ID)
	if !ok {
		return boxsession.Account{}, errors.New("the Upstash Box account has no API key")
	}
	return boxsession.Account{
		ID:      acc.ID,
		APIKey:  key,
		Size:    acc.Size,
		Idle:    time.Duration(acc.IdleMinutes) * time.Minute,
		HardCap: time.Duration(acc.HardCapMinutes) * time.Minute,
		Retain:  time.Duration(acc.RetainDays) * 24 * time.Hour,
	}, nil
}

// upstashBoxAPIKeyFor returns an account's stored key for the orphan sweep, which
// may need an account that is no longer the default.
func upstashBoxAPIKeyFor(accountID string) (string, error) {
	key, ok := auth.UpstashBoxAPIKey(accountID)
	if !ok {
		return "", fmt.Errorf("the Upstash Box account %s has no API key", accountID)
	}
	return key, nil
}

// upstashBoxSDKClient builds the box API client for one key.
func upstashBoxSDKClient(apiKey string) (boxsession.BoxAPI, error) {
	c, err := upstashbox.New(apiKey)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// installationLabelLocked returns the installation label, creating it once. The
// caller holds h.boxMu.
func (h *Handler) installationLabelLocked() (string, error) {
	if h.boxLabelCached != "" {
		return h.boxLabelCached, nil
	}
	dir, err := paths.GlobalDataDir()
	if err != nil {
		return "", fmt.Errorf("upstash boxes: resolve the data directory: %w", err)
	}
	label, err := boxsession.InstallationLabel(dir)
	if err != nil {
		return "", err
	}
	h.boxLabelCached = label
	return label, nil
}

// boxInstallationLabel returns the installation label, creating it once.
func (h *Handler) boxInstallationLabel() (string, error) {
	h.boxMu.Lock()
	defer h.boxMu.Unlock()
	return h.installationLabelLocked()
}

// boxManagerDeps returns the box manager's collaborators. The login-state store
// comes from upstashLoginStoreFor, the accessor the login-state routes use, so the
// box and the settings UI read and write one directory. A store that cannot be
// resolved is an error: a box must never start with login states silently off.
func (h *Handler) boxManagerDeps(journal *boxsession.Journal, label string) (boxsession.Deps, error) {
	loginStore, err := upstashLoginStoreFor()
	if err != nil {
		return boxsession.Deps{}, fmt.Errorf("upstash boxes: login states: %w", err)
	}
	return boxsession.Deps{
		Account:       upstashDefaultBoxAccount,
		APIKeyFor:     upstashBoxAPIKeyFor,
		NewAPI:        upstashBoxSDKClient,
		Dial:          cdp.DialWebSocket,
		Busy:          h.boxBusy,
		Journal:       journal,
		Label:         label,
		LoginStates:   loginStore,
		LoginStateFor: h.boxLoginStateFor,
	}, nil
}

// boxManager returns the box manager, building it on first use. Building needs no
// account: the account is resolved only when a box starts. Building starts the
// manager's reaper, which is stopped by shutdownBoxes.
func (h *Handler) boxManager() (*boxsession.Manager, error) {
	h.boxMu.Lock()
	defer h.boxMu.Unlock()
	if h.boxes != nil {
		return h.boxes, nil
	}
	journalPath, err := upstashBoxJournalPath()
	if err != nil {
		return nil, err
	}
	label, err := h.installationLabelLocked()
	if err != nil {
		return nil, err
	}
	deps, err := h.boxManagerDeps(boxsession.NewJournal(journalPath), label)
	if err != nil {
		return nil, err
	}
	mgr := boxsession.New(deps)
	ctx, cancel := context.WithCancel(context.Background())
	h.boxes = mgr
	h.boxStop = cancel
	crashguard.Go(func() { mgr.Run(ctx) })
	return mgr, nil
}

// boxManagerIfBuilt returns the box manager only when one exists. Paths that
// act on existing boxes use it, so they never build a manager.
func (h *Handler) boxManagerIfBuilt() *boxsession.Manager {
	h.boxMu.Lock()
	defer h.boxMu.Unlock()
	return h.boxes
}

// resumeBoxSweepIfJournaled starts the box manager at server start when the
// journal holds rows, so an earlier process's orphaned boxes are swept without a
// session starting a box first. An absent or empty journal starts nothing.
func (h *Handler) resumeBoxSweepIfJournaled() {
	path, err := upstashBoxJournalPath()
	if err != nil {
		log.Printf("server: upstash box sweep: %v", err)
		return
	}
	fi, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return
	case err != nil:
		log.Printf("server: upstash box sweep: stat journal: %v", err)
		return
	case fi.Size() == 0:
		return
	}
	if _, err := h.boxManager(); err != nil {
		log.Printf("server: upstash box sweep: %v", err)
	}
}

// boxLoginStateFor returns the login state assigned to a session's project, or ""
// for none. An unknown session, a session with no project root and an unreadable
// Upstash settings file are errors: a start never falls back to a clean browser
// without saying so.
func (h *Handler) boxLoginStateFor(sessionID string) (string, error) {
	entry := h.sessions.Lookup(sessionID)
	if entry == nil {
		return "", fmt.Errorf("resolve the login state: session %s is not known to this server", sessionID)
	}
	if entry.ProjectRoot == "" {
		return "", fmt.Errorf("resolve the login state: session %s has no project root", sessionID)
	}
	cfg, err := config.LoadUpstashBoxConfig()
	if err != nil {
		return "", fmt.Errorf("read the Upstash Box settings: %w", err)
	}
	return cfg.LoginStateFor(entry.ProjectRoot), nil
}

// boxBusy reports whether a session has work the box must not be reaped under:
// an active or starting turn, or a pending permission or question ask. A lock
// that cannot be taken means a turn holds the session, so it is busy. Never blocks.
func (h *Handler) boxBusy(sessionID string) bool {
	h.cancelMu.Lock()
	inFlight := h.turnInFlight[sessionID] > 0
	h.cancelMu.Unlock()
	if inFlight {
		return true
	}
	if h.sessions.IsTurnActive(sessionID) {
		return true
	}
	as := h.lookupAgentSession(sessionID)
	if as == nil {
		return false
	}
	if !as.mu.TryLock() {
		return true
	}
	defer as.mu.Unlock()
	return h.sessionHasOutstandingAsk(as)
}

// terminateSessionBox deletes a closed session's box in the background, bounded by
// boxCloseTimeout. The Upstash delete can take seconds, and the close request must
// not wait for it. Having no box is not a failure: ErrNotStarted means none, and
// ErrExpired means the reaper already removed it. The returned channel is closed
// when the teardown ends (at once when there is no manager); the close request
// itself never waits on it.
func (h *Handler) terminateSessionBox(sessionID string) <-chan struct{} {
	done := make(chan struct{})
	mgr := h.boxManagerIfBuilt()
	if mgr == nil {
		close(done)
		return done
	}
	h.boxCloses.Add(1)
	crashguard.Go(func() {
		defer h.boxCloses.Done()
		defer close(done)
		ctx, cancel := context.WithTimeout(context.Background(), boxCloseTimeout)
		defer cancel()
		err := mgr.Terminate(ctx, sessionID)
		// ErrPaused is a retained box the idle reaper already paused: the close has nothing to do.
		if err != nil && !errors.Is(err, boxsession.ErrNotStarted) && !errors.Is(err, boxsession.ErrExpired) && !errors.Is(err, boxsession.ErrPaused) {
			log.Printf("server: close session %s: terminate its Upstash box: %v", sessionID, err)
		}
	})
	return done
}

// shutdownBoxes stops the reaper, then tears down every box this process owns: an
// ephemeral box is deleted, and a retained box is paused after its final login-state
// save. It runs after the agent sessions have stopped, so no turn can start a box
// after it. The teardown has its own capped slice of ctx, so an unreachable Upstash
// cannot eat the whole shutdown budget. An ephemeral box left behind expires at its
// ttl; a retained box left running is paused by the next ocode start's sweep.
func (h *Handler) shutdownBoxes(ctx context.Context) {
	h.boxMu.Lock()
	mgr := h.boxes
	stop := h.boxStop
	h.boxMu.Unlock()
	if stop != nil {
		stop()
	}
	if mgr == nil {
		return
	}
	if ctx.Err() != nil {
		log.Printf("server: shutdown skipped the Upstash box teardown: %v", ctx.Err())
		return
	}
	bctx, cancel := context.WithTimeout(ctx, boxShutdownCap)
	defer cancel()
	mgr.Shutdown(bctx)
}

// rekeyBoxSession moves a session's box to its new id on /reset-id.
func (h *Handler) rekeyBoxSession(oldID, newID string) {
	mgr := h.boxManagerIfBuilt()
	if mgr == nil {
		return
	}
	if err := mgr.Rekey(oldID, newID); err != nil {
		log.Printf("server: reset session id %s: move its Upstash box: %v", oldID, err)
	}
}

// upstashBoxHTTPStatus is the status an upstream box error maps to. A refusal of a
// box that another process or a resume owns is a 409. A 4xx from Upstash passes
// through, except 401 and 403, which the web client reads as an expired ocode login
// and so map to 502. Anything else is a 502.
func upstashBoxHTTPStatus(err error) int {
	if errors.Is(err, errRetainedBoxResume) || errors.Is(err, boxsession.ErrRetainedBusy) {
		return http.StatusConflict
	}
	var apiErr *upstashbox.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode >= http.StatusBadRequest && apiErr.StatusCode < http.StatusInternalServerError {
		if apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden {
			return http.StatusBadGateway
		}
		return apiErr.StatusCode
	}
	return http.StatusBadGateway
}

// upstashBrowserMetadataKey is the session-metadata key that gives a session the
// Upstash box backend. It is written once, at session creation, and never
// changed afterwards, so the browser tool's definition stays byte-stable for the
// session's life.
const upstashBrowserMetadataKey = "upstash_browser"

// upstashBrowserAtCreation reports whether a new session gets the box backend: it
// does iff at least one Upstash Box account exists at that moment. An unreadable
// config is an error, because the flag is permanent and a guess would stick.
func upstashBrowserAtCreation() (bool, error) {
	cfg, err := config.LoadUpstashBoxConfig()
	if err != nil {
		return false, fmt.Errorf("read the Upstash Box settings: %w", err)
	}
	return len(cfg.Accounts) > 0, nil
}

// upstashBrowserFromMetadata reads the creation flag. An absent key is false. A
// value that is not a bool is an error, so a corrupt flag fails the session's
// build instead of silently dropping its boxes.
func upstashBrowserFromMetadata(md map[string]any) (bool, error) {
	raw, ok := md[upstashBrowserMetadataKey]
	if !ok {
		return false, nil
	}
	on, isBool := raw.(bool)
	if !isBool {
		return false, fmt.Errorf("%s is %T, want bool", upstashBrowserMetadataKey, raw)
	}
	return on, nil
}
