package cdp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Browser storage for attach mode: the cookies of the shared browser context
// and the localStorage of each open origin. A login state is saved from these
// and restored into a fresh browser by SetStorageCookies and SetLocalStorageSeed.
// None of this runs in launch mode.

// attachStorageTimeout bounds one storage call, or one tab's localStorage read,
// so a tab stuck mid-navigation cannot stall a whole snapshot.
const attachStorageTimeout = 10 * time.Second

// loginCallTimeout bounds one login-state CDP call: a registration, a removal
// or a binding. It is a variable only so a test can shorten it.
var loginCallTimeout = attachStorageTimeout

// maxLoginScriptBytes is the size limit for one origin's restore script. The
// same measure (loginScriptSize) decides whether an origin is captured from a
// tab and whether its seed is skipped on restore, so the two cannot disagree.
const maxLoginScriptBytes = 1 << 20

// loginGenReserve is room kept in that measure for the generation number a
// restore script carries, so the size does not depend on the generation.
const loginGenReserve = 20

// legacyLoginSentinel is the key an earlier build wrote into page storage to
// mark an origin restored. Nothing writes it now. The snapshot drops it from
// states saved by that build, and never reports it.
const legacyLoginSentinel = "__ocode_ls_restored"

// loginSeedWorld is the isolated world the restore script runs in. The page
// cannot read the script or the binding below. It can read the values the
// script writes, because they are in its own localStorage.
const loginSeedWorld = "ocode_login_state"

// loginSeedBinding is the binding the restore script calls once it has applied
// an origin. It exists only in loginSeedWorld, so page scripts cannot call it.
const loginSeedBinding = "ocodeLoginSeedApplied"

var (
	errNotAttachMode          = errors.New("cdp: storage API requires an attach-mode Manager")
	errNotConnected           = errors.New("cdp: attach: browser is not connected")
	errManagerClosed          = errors.New("cdp: attach: manager is closed")
	errNoCookieAccepted       = errors.New("cdp: attach: no login-state cookie was accepted")
	errReplacedWithoutRestore = errors.New("cdp: attach: browser context was replaced without a restore")
)

// storageCookieParamFields are the CookieParam fields that Storage.setCookies
// accepts (Chrome DevTools Protocol). Storage.getCookies returns more than
// these (size, session, partitionKeyOpaque), and those are dropped.
var storageCookieParamFields = map[string]bool{
	"name": true, "value": true, "url": true, "domain": true, "path": true,
	"secure": true, "httpOnly": true, "sameSite": true, "expires": true,
	"priority": true, "sourceScheme": true, "sourcePort": true, "partitionKey": true,
}

// loginSeedRestoreFn runs in the isolated world on every document. It applies
// the entry for this origin and then reports the origin to the Manager, which
// stops seeding it. The restored values are visible to the page, as its own
// localStorage. Two tabs that load one origin in the same instant may both
// apply the same seed before either report arrives. Applying the same values
// twice is harmless, and that case is accepted.
const loginSeedRestoreFn = `function (entries) {
  if (window.top !== window) return;
  const origin = location.origin;
  const entry = entries.find((e) => e.origin === origin);
  if (!entry) return;
  try {
    for (const [key, value] of entry.items) localStorage.setItem(key, value);
  } catch (e) {
    // An opaque origin (a sandboxed or data: document) throws a SecurityError on
    // any localStorage access, so there is nothing to restore there. Only that
    // exception is swallowed. Any other failure is real and is rethrown.
    if (e && e.name === "SecurityError") return;
    throw e;
  }
  globalThis.` + loginSeedBinding + `(JSON.stringify({ origin: origin, gen: entry.gen }));
}`

// loginSnapshotScript returns the page's origin and localStorage as pairs, or
// null for a page that is not http(s). Pairs, not an object, so a key such as
// "__proto__" survives.
const loginSnapshotScript = `(() => {
  const p = location.protocol;
  if (p !== "http:" && p !== "https:") return null;
  const items = [];
  for (let i = 0; i < localStorage.length; i++) {
    const key = localStorage.key(i);
    items.push([key, localStorage.getItem(key)]);
  }
  return { origin: location.origin, items: items };
})()`

// loginSeedEntry is one origin's seed as it is sent into the page. Gen is the
// seed generation the script was built for, echoed back in its report.
type loginSeedEntry struct {
	Origin string      `json:"origin"`
	Gen    uint64      `json:"gen"`
	Items  [][2]string `json:"items"`
}

// loginSnapshot is one tab's localStorage as the snapshot script returns it.
type loginSnapshot struct {
	Origin string      `json:"origin"`
	Items  [][2]string `json:"items"`
}

// loginTab is one tab's login-state registrations: the identifier of the
// restore script registered for each origin, and how to reach the tab to
// remove them.
type loginTab struct {
	conn      *Conn
	sessionID string
	key       string
	ids       map[string]string // origin -> script identifier
}

// loginSeedState is attach mode's localStorage restore. mu serializes every
// seed change with the per-tab hooks and with the reports from restore
// scripts. The zero value is ready to use.
//
//   - gen is the committed seed generation. A report counts only when it
//     carries this generation, so a script from a replaced seed cannot mark an
//     origin of the new seed applied.
//   - sources holds the restore script of every origin a new tab gets: the
//     committed seed, minus origins already applied.
//   - applied holds the origins a document already applied in this browser.
//     It survives a seed change and is reset only when the browser context is
//     replaced, because the new browser has restored nothing.
//   - tabs holds exactly the registrations the browser holds for the tabs of
//     the current connection.
type loginSeedState struct {
	mu      sync.Mutex
	gen     uint64
	sources map[string]string
	applied map[string]bool
	tabs    map[string]*loginTab // by targetID
}

// connDone reports whether the connection has ended.
func connDone(c *Conn) bool {
	select {
	case <-c.Done():
		return true
	default:
		return false
	}
}

// logLogin writes a login-state diagnostic. It carries names and counts, never
// a cookie or storage value.
func (m *Manager) logLogin(format string, args ...any) {
	if m.opts.Log != nil {
		m.opts.Log.Printf(format, args...)
	}
}

// SetContextRestore sets the hook that restores a replacement browser context.
// When a browser that lost the shared context is attached again, the new
// context is filled before any tab is created in it: fn's cookies are set, and
// its seed replaces the localStorage seed. If fn or the apply fails, the attach
// fails with that error. With no hook, the replacement is created empty and
// the Manager refuses storage reads from then on, so an empty jar is never
// saved as real. fn must not call back into this Manager. The setter is a no-op
// outside attach mode.
func (m *Manager) SetContextRestore(fn func(ctx context.Context) (cookies []json.RawMessage, seed map[string]map[string]string, err error)) {
	if !m.attachMode {
		return
	}
	m.mu.Lock()
	m.contextRestore = fn
	m.mu.Unlock()
}

// storageCookieParam keeps only the CookieParam fields Storage.setCookies
// accepts. A session cookie's expires of -1 is dropped, so the cookie is set
// again as a session cookie rather than as one that expired long ago.
func storageCookieParam(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, errors.New("not a JSON object")
	}
	out := make(map[string]json.RawMessage, len(obj))
	for k, v := range obj {
		if storageCookieParamFields[k] {
			out[k] = v
		}
	}
	if exp, ok := out["expires"]; ok {
		var secs float64
		if err := json.Unmarshal(exp, &secs); err != nil {
			return nil, errors.New("expires is not a number")
		}
		if secs == -1 {
			delete(out, "expires")
		}
	}
	return out, nil
}

// prepareStorageCookies reduces raw cookies to CookieParam objects. It returns
// the cookies to send, and how many were invalid: not a usable object, or an
// expires that is not a number. A cookie whose expiry is already past at now is
// neither sent nor counted invalid. It is a saved cookie that simply expired,
// and nothing is wrong with it. Each drop gets a note with its position and
// never its value.
func prepareStorageCookies(raw []json.RawMessage, now float64) (params []map[string]json.RawMessage, invalid int, notes []string) {
	for i, r := range raw {
		p, err := storageCookieParam(r)
		if err != nil {
			invalid++
			notes = append(notes, fmt.Sprintf("cookie %d rejected: %v", i, err))
			continue
		}
		if exp, ok := p["expires"]; ok {
			var secs float64
			_ = json.Unmarshal(exp, &secs) // storageCookieParam checked it is a number
			if secs < now {
				notes = append(notes, fmt.Sprintf("cookie %d skipped: already expired", i))
				continue
			}
		}
		params = append(params, p)
	}
	return params, invalid, notes
}

// nothingToSend is the result when no cookie reaches the browser. An expired
// jar is not an error. A jar with invalid cookies and nothing usable is refused,
// and the caller fails on it.
func nothingToSend(invalid int) (int, error) {
	if invalid > 0 {
		return invalid, fmt.Errorf("%w (%d rejected)", errNoCookieAccepted, invalid)
	}
	return 0, nil
}

// cookieName is a cookie's name for a log line. It is never a value.
func cookieName(p map[string]json.RawMessage) string {
	var name string
	_ = json.Unmarshal(p["name"], &name)
	return name
}

// StorageCookies returns every cookie of the shared browser context, each as
// the raw object Storage.getCookies returned. A Manager that never had a shared
// context returns an empty slice. Once it had one, the read needs a live
// connection: after a redial that has not yet re-established the context, the
// read re-establishes it first, and while disconnected it is an error, so an
// empty list can never stand for a browser that has lost its cookies.
func (m *Manager) StorageCookies(ctx context.Context) ([]json.RawMessage, error) {
	if !m.attachMode {
		return nil, errNotAttachMode
	}
	conn, bc, err := m.storageContext(ctx)
	if err != nil {
		return nil, err
	}
	if conn == nil {
		return []json.RawMessage{}, nil
	}
	var res struct {
		Cookies []json.RawMessage `json:"cookies"`
	}
	if err := conn.Call(ctx, "", "Storage.getCookies", map[string]string{"browserContextId": bc}, &res); err != nil {
		return nil, fmt.Errorf("cdp: attach: read browser cookies: %w", err)
	}
	if res.Cookies == nil {
		return []json.RawMessage{}, nil
	}
	return res.Cookies, nil
}

// storageContext returns the connection and shared context a storage read must
// use. A Manager that never had a shared context returns nil and no error. A
// read after a re-dial re-establishes the context on the current connection.
func (m *Manager) storageContext(ctx context.Context) (*Conn, string, error) {
	m.mu.Lock()
	had, closed, lost := m.attachHad, m.closed, m.contextLost
	conn, owner, bc := m.conn, m.sharedBCOwner, m.sharedBC
	m.mu.Unlock()
	if !had {
		return nil, "", nil
	}
	if closed {
		return nil, "", errManagerClosed
	}
	if lost {
		return nil, "", errReplacedWithoutRestore
	}
	if conn == nil || connDone(conn) {
		return nil, "", errNotConnected
	}
	if owner == conn && bc != "" {
		return conn, bc, nil
	}
	m.attachMu.Lock()
	defer m.attachMu.Unlock()
	bc, err := m.browserContextFor(ctx, conn, "")
	if err != nil {
		return nil, "", fmt.Errorf("cdp: attach: shared browser context: %w", err)
	}
	m.mu.Lock()
	lost = m.contextLost
	m.mu.Unlock()
	if lost {
		return nil, "", errReplacedWithoutRestore
	}
	return conn, bc, nil
}

// SetStorageCookies sets cookies in the shared browser context, creating that
// context if it does not exist yet. Each input is a Storage.getCookies object,
// reduced to the CookieParam fields. Cookies whose expiry is already past are
// dropped before any call. The batch is sent first. If the browser refuses it,
// each cookie is sent alone, so one bad cookie does not sink the rest.
//
// It returns how many inputs were rejected: invalid ones, and ones the browser
// refused. An expired cookie is filtered out and is not counted. The error is
// non-nil only when the context could not be created, or when cookies were
// given and none was accepted. Empty input and an all-expired jar are no-ops.
func (m *Manager) SetStorageCookies(ctx context.Context, cookies []json.RawMessage) (rejected int, err error) {
	if !m.attachMode {
		return 0, errNotAttachMode
	}
	if len(cookies) == 0 {
		return 0, nil
	}
	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return 0, errManagerClosed
	}
	params, invalid, notes := prepareStorageCookies(cookies, nowSeconds())
	m.logNotes(notes)
	if len(params) == 0 {
		// Nothing to send, so the browser is not touched.
		return nothingToSend(invalid)
	}
	if err := m.ensureChrome(ctx); err != nil {
		return 0, err
	}
	m.mu.Lock()
	conn := m.conn
	m.mu.Unlock()
	if conn == nil {
		return 0, errNotConnected
	}
	// browserContextFor requires attachMu.
	m.attachMu.Lock()
	bc, err := m.browserContextFor(ctx, conn, "")
	m.attachMu.Unlock()
	if err != nil {
		return 0, fmt.Errorf("cdp: attach: shared browser context: %w", err)
	}
	rejected, err = m.sendPreparedCookies(ctx, conn, bc, params, invalid)
	if err != nil && !errors.Is(err, errNoCookieAccepted) {
		return rejected, fmt.Errorf("cdp: attach: set browser cookies: %w", err)
	}
	return rejected, err
}

// nowSeconds is the current time in the unit Storage cookies use for expires.
func nowSeconds() float64 {
	return float64(time.Now().Unix())
}

// logNotes writes the notes from prepareStorageCookies.
func (m *Manager) logNotes(notes []string) {
	for _, n := range notes {
		m.logLogin("browse: attach: login-state %s", n)
	}
}

// sendPreparedCookies sends the usable params to bc. invalid counts the inputs
// that were not usable. It returns the rejected count, invalid plus refused by
// the browser, and errNoCookieAccepted when cookies were given and none was
// accepted.
func (m *Manager) sendPreparedCookies(ctx context.Context, conn *Conn, bc string, params []map[string]json.RawMessage, invalid int) (int, error) {
	if len(params) == 0 {
		return nothingToSend(invalid)
	}
	refused, err := m.sendStorageCookies(ctx, conn, bc, params)
	rejected := invalid + refused
	if rejected > 0 {
		m.logLogin("browse: attach: %d login-state cookies rejected", rejected)
	}
	if err != nil {
		return rejected, err
	}
	if refused == len(params) {
		return rejected, fmt.Errorf("%w (%d rejected)", errNoCookieAccepted, rejected)
	}
	return rejected, nil
}

// sendStorageCookies sends params to bc and returns how many the browser
// refused. A refused batch sets none of its cookies, so each is then sent
// alone. A connection that ends stops the call and is returned as an error,
// because every later cookie would fail the same way.
func (m *Manager) sendStorageCookies(ctx context.Context, conn *Conn, bc string, params []map[string]json.RawMessage) (int, error) {
	if err := setStorageCookiesCall(ctx, conn, bc, params); err == nil {
		return 0, nil
	} else if errors.Is(err, ErrConnClosed) {
		return len(params), err
	}
	refused := 0
	for _, p := range params {
		if err := setStorageCookiesCall(ctx, conn, bc, []map[string]json.RawMessage{p}); err != nil {
			if errors.Is(err, ErrConnClosed) {
				return len(params), err
			}
			refused++
			m.logLogin("browse: attach: browser refused login-state cookie %q", cookieName(p))
		}
	}
	return refused, nil
}

func setStorageCookiesCall(ctx context.Context, conn *Conn, bc string, params []map[string]json.RawMessage) error {
	return conn.Call(ctx, "", "Storage.setCookies", map[string]any{
		"browserContextId": bc,
		"cookies":          params,
	}, nil)
}

// LocalStorageSnapshot returns the localStorage of every open http(s) tab,
// merged by origin. Tabs of one origin share storage, so the last tab read
// wins a key that differs between them (tabs are read in stateKey order). The
// legacy sentinel key is dropped. Every origin that was read is in the result,
// even with no items, so a caller can save an origin that was cleared. An
// origin whose items total more than maxLoginScriptBytes is left out and
// reported, by name and size only. A tab whose read fails is skipped and
// reported in the error, which joins every such failure. The map is still
// returned, so the caller decides whether a partial snapshot is usable.
func (m *Manager) LocalStorageSnapshot(ctx context.Context) (map[string]map[string]string, error) {
	if !m.attachMode {
		return nil, errNotAttachMode
	}
	if _, _, err := m.storageContext(ctx); err != nil {
		return nil, err
	}
	out := make(map[string]map[string]string)
	var errs []error
	for _, t := range m.attachedTabs() {
		tctx, cancel := context.WithTimeout(ctx, attachStorageTimeout)
		snap, err := t.localStorageSnapshot(tctx)
		cancel()
		if err != nil {
			errs = append(errs, fmt.Errorf("login state: tab %s: %w", t.stateKey, err))
			continue
		}
		if snap == nil {
			continue
		}
		if out[snap.Origin] == nil {
			out[snap.Origin] = make(map[string]string)
		}
		for _, kv := range snap.Items {
			if kv[0] == legacyLoginSentinel {
				continue
			}
			out[snap.Origin][kv[0]] = kv[1]
		}
	}
	origins := sortedStringKeys(out)
	for _, origin := range origins {
		size, err := loginScriptSize(origin, out[origin])
		if err != nil {
			errs = append(errs, err)
			delete(out, origin)
			continue
		}
		if size > maxLoginScriptBytes {
			delete(out, origin)
			errs = append(errs, fmt.Errorf("login state: origin %s is %d bytes, over the %d byte limit; not captured", origin, size, maxLoginScriptBytes))
		}
	}
	return out, errors.Join(errs...)
}

// SetLocalStorageSeed makes every tab restore seed's localStorage the first
// time it loads each origin, through a restore script in an isolated world. It
// applies to every open tab now and to every tab created later. A second call
// replaces the first, and a nil or empty seed clears it.
//
// An origin already applied in this browser is not seeded again. An origin
// whose script would exceed maxLoginScriptBytes is skipped, and returned by
// name in skipped. The rest of the seed still applies.
//
// Consistency on failure. The change is made in this order: every script the
// Manager holds is removed, the new scripts are registered on every tab, and
// only then is the new source committed. If any step fails the call returns the
// error and the committed source is unchanged, so a tab created later gets the
// old seed. The Manager's record of registrations still matches the browser:
// tabs that lost a script no longer hold it, and tabs that gained one hold it
// and are recorded. Calling SetLocalStorageSeed again with the same seed
// converges every tab on it.
func (m *Manager) SetLocalStorageSeed(ctx context.Context, seed map[string]map[string]string) (skipped []string, err error) {
	if !m.attachMode {
		return nil, errNotAttachMode
	}
	s := &m.login
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.gen + 1
	sources, skipped, err := buildLoginSources(seed, s.applied, next)
	if err != nil {
		return nil, err
	}
	identities := m.loginIdentities(s)
	for targetID, tab := range s.tabs {
		for origin, id := range tab.ids {
			if err := removeLoginSeedScript(ctx, tab.conn, tab.sessionID, id); err != nil {
				return nil, fmt.Errorf("cdp: attach: remove login-state seed from tab %s: %w", tab.key, err)
			}
			delete(tab.ids, origin)
		}
		delete(s.tabs, targetID)
	}
	for targetID, tab := range identities {
		if err := s.registerTabLocked(ctx, sources, targetID, tab); err != nil {
			return nil, fmt.Errorf("cdp: attach: login-state seed on tab %s: %w", tab.key, err)
		}
	}
	s.gen = next
	s.sources = sources
	return skipped, nil
}

// registerLoginSeed registers the committed seed on a tab that is being
// created. The tab is still in m.pending, not m.targets. With no seed it sends
// nothing. A failure leaves nothing recorded for the tab, and the caller
// closes it.
func (m *Manager) registerLoginSeed(ctx context.Context, conn *Conn, sessionID, targetID, key string) error {
	s := &m.login
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sources) == 0 {
		return nil
	}
	tab := &loginTab{conn: conn, sessionID: sessionID, key: key}
	if err := s.registerTabLocked(ctx, s.sources, targetID, tab); err != nil {
		delete(s.tabs, targetID)
		return err
	}
	return nil
}

// registerTabLocked registers one restore script per origin of sources on tab,
// and records every identifier it gets, so a later change can remove them. A
// failure keeps the identifiers already recorded. The caller holds s.mu.
func (s *loginSeedState) registerTabLocked(ctx context.Context, sources map[string]string, targetID string, tab *loginTab) error {
	if len(sources) == 0 {
		return nil
	}
	if s.tabs == nil {
		s.tabs = make(map[string]*loginTab)
	}
	tab.ids = make(map[string]string, len(sources))
	s.tabs[targetID] = tab
	if err := addLoginBinding(ctx, tab.conn, tab.sessionID); err != nil {
		return err
	}
	for _, origin := range sortedStringKeys(sources) {
		id, err := addLoginSeedScript(ctx, tab.conn, tab.sessionID, sources[origin])
		if err != nil {
			return err
		}
		tab.ids[origin] = id
	}
	return nil
}

// loginIdentities returns the tabs a seed change registers on: the tabs of the
// current connection, live or pending. A record for any other tab is dropped
// here, with no call, because its connection is gone and so are its scripts.
// The caller holds s.mu.
func (m *Manager) loginIdentities(s *loginSeedState) map[string]*loginTab {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur := m.conn
	out := make(map[string]*loginTab)
	if cur == nil {
		s.tabs = make(map[string]*loginTab)
		return out
	}
	for _, t := range m.targets {
		if t.conn == cur {
			out[t.targetID] = &loginTab{conn: t.conn, sessionID: t.sessionID, key: t.stateKey}
		}
	}
	for targetID, tab := range s.tabs {
		if _, live := out[targetID]; live {
			continue
		}
		if _, pending := m.pending[targetID]; pending && tab.conn == cur {
			out[targetID] = tab
			continue
		}
		delete(s.tabs, targetID)
	}
	return out
}

// loginOriginApplied handles one report from a tab's restore script. It is the
// only path that marks an origin applied. The report counts only from the
// current connection, for the committed generation, and for an origin that is
// still unapplied. The origin's script is then removed from every tab, so no
// later document of that origin is seeded. A removal that fails is logged by
// origin name and retried at the next seed change.
func (m *Manager) loginOriginApplied(t *Target, payload string) {
	var rep struct {
		Origin string `json:"origin"`
		Gen    uint64 `json:"gen"`
	}
	if err := json.Unmarshal([]byte(payload), &rep); err != nil {
		return
	}
	s := &m.login
	s.mu.Lock()
	defer s.mu.Unlock()
	if rep.Gen != s.gen {
		return
	}
	if _, ok := s.sources[rep.Origin]; !ok {
		return
	}
	m.mu.Lock()
	current := t.conn != nil && m.conn == t.conn
	m.mu.Unlock()
	if !current {
		return
	}
	if s.applied == nil {
		s.applied = make(map[string]bool)
	}
	s.applied[rep.Origin] = true
	delete(s.sources, rep.Origin)
	for _, tab := range s.tabs {
		id, ok := tab.ids[rep.Origin]
		if !ok {
			continue
		}
		if err := removeLoginSeedScript(context.Background(), tab.conn, tab.sessionID, id); err != nil {
			m.logLogin("browse: attach: could not remove the login-state seed for %s from tab %s; the next seed change retries: %v", rep.Origin, tab.key, err)
			continue
		}
		delete(tab.ids, rep.Origin)
	}
}

// LoginSeedUnapplied returns the origins the committed seed still holds for
// documents not yet loaded, sorted. It carries origin names only. The live
// login-state test polls it to learn that an origin was applied and its script
// removed.
func (m *Manager) LoginSeedUnapplied() []string {
	s := &m.login
	s.mu.Lock()
	defer s.mu.Unlock()
	return sortedStringKeys(s.sources)
}

// resetLoginForNewBrowser commits seed for a browser context that was just
// created in place of a lost one. The old tabs' scripts are dropped without a
// call, because their connection is gone. Applied origins are forgotten, since
// the new browser has restored nothing.
func (m *Manager) resetLoginForNewBrowser(seed map[string]map[string]string) ([]string, error) {
	s := &m.login
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.gen + 1
	sources, skipped, err := buildLoginSources(seed, nil, next)
	if err != nil {
		return nil, err
	}
	s.tabs = make(map[string]*loginTab)
	s.applied = make(map[string]bool)
	s.gen = next
	s.sources = sources
	return skipped, nil
}

// buildLoginSources is the one place a seed becomes restore scripts. It returns
// a script for each origin that has items, is not applied, and fits the size
// limit, and it returns the origins that did not fit, by name. It changes no
// state.
func buildLoginSources(seed map[string]map[string]string, applied map[string]bool, gen uint64) (map[string]string, []string, error) {
	sources := make(map[string]string)
	var skipped []string
	for _, origin := range sortedOrigins(seed) {
		if applied[origin] {
			continue
		}
		items := seed[origin]
		size, err := loginScriptSize(origin, items)
		if err != nil {
			return nil, nil, err
		}
		if size > maxLoginScriptBytes {
			skipped = append(skipped, origin)
			continue
		}
		src, err := loginSourceFor(origin, items, gen)
		if err != nil {
			return nil, nil, err
		}
		sources[origin] = src
	}
	return sources, skipped, nil
}

// sortedOrigins returns the origins of seed that have items, sorted.
func sortedOrigins(seed map[string]map[string]string) []string {
	origins := make([]string, 0, len(seed))
	for origin, items := range seed {
		if len(items) > 0 {
			origins = append(origins, origin)
		}
	}
	sort.Strings(origins)
	return origins
}

// sortedStringKeys returns the keys of m, sorted.
func sortedStringKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// loginSourceFor builds one origin's restore script for generation gen. The
// seed is marshalled to JSON and passed as an argument, so no value is ever
// spliced into code.
func loginSourceFor(origin string, items map[string]string, gen uint64) (string, error) {
	keys := sortedStringKeys(items)
	pairs := make([][2]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, [2]string{k, items[k]})
	}
	data, err := json.Marshal([]loginSeedEntry{{Origin: origin, Gen: gen, Items: pairs}})
	if err != nil {
		return "", fmt.Errorf("cdp: attach: encode login-state seed: %w", err)
	}
	return "(" + loginSeedRestoreFn + ")(" + string(data) + ");", nil
}

// loginScriptSize is the size an origin is measured at: the length of its
// restore script, plus loginGenReserve. It is used for capture and for restore.
func loginScriptSize(origin string, items map[string]string) (int, error) {
	src, err := loginSourceFor(origin, items, 0)
	if err != nil {
		return 0, err
	}
	return len(src) + loginGenReserve, nil
}

// addLoginBinding installs the binding the restore script reports through, in
// the isolated world only. It is idempotent, so it is sent before every
// registration on a tab.
func addLoginBinding(ctx context.Context, conn *Conn, sessionID string) error {
	cctx, cancel := context.WithTimeout(ctx, loginCallTimeout)
	defer cancel()
	return conn.Call(cctx, sessionID, "Runtime.addBinding", map[string]string{
		"name":                 loginSeedBinding,
		"executionContextName": loginSeedWorld,
	}, nil)
}

func addLoginSeedScript(ctx context.Context, conn *Conn, sessionID, source string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, loginCallTimeout)
	defer cancel()
	var res struct {
		Identifier string `json:"identifier"`
	}
	if err := conn.Call(cctx, sessionID, "Page.addScriptToEvaluateOnNewDocument", map[string]any{
		"source":    source,
		"worldName": loginSeedWorld,
	}, &res); err != nil {
		return "", err
	}
	// Without an identifier the script could never be removed on replacement.
	if res.Identifier == "" {
		return "", errors.New("browser returned no script identifier for the login-state seed")
	}
	return res.Identifier, nil
}

func removeLoginSeedScript(ctx context.Context, conn *Conn, sessionID, identifier string) error {
	cctx, cancel := context.WithTimeout(ctx, loginCallTimeout)
	defer cancel()
	return conn.Call(cctx, sessionID, "Page.removeScriptToEvaluateOnNewDocument",
		map[string]string{"identifier": identifier}, nil)
}

// handleLoginBinding reads the restore script's reports from one tab. Each one
// goes to the Manager, which validates it.
func (t *Target) handleLoginBinding(ch <-chan json.RawMessage) {
	for raw := range ch {
		var ev struct {
			Name    string `json:"name"`
			Payload string `json:"payload"`
		}
		if json.Unmarshal(raw, &ev) != nil || ev.Name != loginSeedBinding {
			continue
		}
		if t.manager != nil && t.manager.attachMode {
			t.manager.loginOriginApplied(t, ev.Payload)
		}
	}
}

// attachedTabs returns the live tabs of the current connection, in stateKey
// order.
func (m *Manager) attachedTabs() []*Target {
	m.mu.Lock()
	cur := m.conn
	tabs := make([]*Target, 0, len(m.targets))
	for _, t := range m.targets {
		if cur != nil && t.conn == cur {
			tabs = append(tabs, t)
		}
	}
	m.mu.Unlock()
	sort.Slice(tabs, func(i, j int) bool { return tabs[i].stateKey < tabs[j].stateKey })
	return tabs
}

// localStorageSnapshot reads one tab's origin and localStorage. It returns nil
// for a tab whose page is not http(s).
func (t *Target) localStorageSnapshot(ctx context.Context) (*loginSnapshot, error) {
	res, err := t.evaluateIn(ctx, 0, loginSnapshotScript, true)
	if err != nil {
		return nil, err
	}
	if len(res.Result.Value) == 0 {
		return nil, errors.New("localStorage read returned no value")
	}
	var snap *loginSnapshot
	if err := json.Unmarshal(res.Result.Value, &snap); err != nil {
		return nil, fmt.Errorf("decode localStorage: %w", err)
	}
	return snap, nil
}

// EvaluateValue runs expression in the tab's main frame and returns its JSON
// value. A script exception is an error. The live login-state test uses it to
// set and read page state.
func (t *Target) EvaluateValue(ctx context.Context, expression string) (json.RawMessage, error) {
	res, err := t.evaluateIn(ctx, 0, expression, true)
	if err != nil {
		return nil, err
	}
	return res.Result.Value, nil
}
