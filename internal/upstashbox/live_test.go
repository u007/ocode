package upstashbox_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/browse/cdp"
	"github.com/u007/ocode/internal/upstashbox"
)

// This file is a live end-to-end test against a real Upstash Box. It creates
// an ephemeral box, drives its browser through the cdp attach manager, and
// deletes the box. It runs only when UPSTASH_BOX_API_KEY is set.

const (
	liveKeyEnv       = "UPSTASH_BOX_API_KEY"
	liveLabel        = "ocode-livetest"
	liveTTLSeconds   = 600
	liveTotalBudget  = 4 * time.Minute
	liveCloseWait    = 30 * time.Second
	liveCleanupWait  = 30 * time.Second
	liveTitleWait    = 60 * time.Second
	liveBlockWait    = 20 * time.Second
	livePollEvery    = 250 * time.Millisecond
	liveExampleURL   = "https://example.com"
	liveExampleTitle = "Example Domain"
	liveDevToolsURL  = "http://127.0.0.1:9222/json/version"
	liveMetadataURL  = "http://169.254.169.254/"
	// liveJSONMarker appears in the body text only when the DevTools JSON loaded.
	liveJSONMarker = "webSocketDebuggerUrl"
	liveTabOne     = "tab:livetest-1"
	liveTabTwo     = "tab:livetest-2"
)

// liveSink discards every frame and event. The title watcher probes only a
// target that has a sink, so the title can be read only through a sink-backed
// attach. Nothing reads the frames.
type liveSink struct{}

func (liveSink) Frame(uint32, uint32, []byte)   {}
func (liveSink) Console(cdp.ConsoleEvent)       {}
func (liveSink) Network(cdp.NetworkEvent)       {}
func (liveSink) Performance(map[string]float64) {}
func (liveSink) Error(string)                   {}

// liveEvents records what a manager reports. Its callbacks run on the
// manager's goroutines, so every access is locked.
type liveEvents struct {
	mu        sync.Mutex
	titles    []cdp.TitleEvent
	navErrors []cdp.NavEvent
}

func (e *liveEvents) onTitle(ev cdp.TitleEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.titles = append(e.titles, ev)
}

func (e *liveEvents) onNav(ev cdp.NavEvent) {
	if ev.Error == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.navErrors = append(e.navErrors, ev)
}

// titlesFor returns the titles reported for one state key, in order.
func (e *liveEvents) titlesFor(key string) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []string
	for _, ev := range e.titles {
		if ev.StateKey == key {
			out = append(out, ev.Title)
		}
	}
	return out
}

// navErrorCount returns how many failed navigations were reported for one key.
func (e *liveEvents) navErrorCount(key string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, ev := range e.navErrors {
		if ev.StateKey == key {
			n++
		}
	}
	return n
}

// liveDialer returns the attach-mode dial for one CDP URL. The URL's token is
// used only here, never printed.
func liveDialer(u upstashbox.CDPURL) func(context.Context) (*cdp.Conn, error) {
	return func(ctx context.Context) (*cdp.Conn, error) {
		return cdp.DialWebSocket(ctx, u.RevealRaw())
	}
}

// newLiveManager builds an attach-mode manager that reports into events.
func newLiveManager(dial func(context.Context) (*cdp.Conn, error), events *liveEvents) *cdp.Manager {
	return cdp.NewAttachManager(cdp.ManagerOptions{
		Log:       log.New(io.Discard, "", 0),
		EmitTitle: events.onTitle,
		EmitNav:   events.onNav,
	}, dial)
}

// liveCloseManager closes a manager with a bounded context. Closing twice is
// harmless, so the cleanup and the explicit step may both call it.
func liveCloseManager(t *testing.T, m *cdp.Manager) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), liveCloseWait)
	defer cancel()
	if err := m.Close(ctx); err != nil {
		t.Errorf("close browser manager: %v", err)
	}
}

// liveDeleteBox deletes a box with its own fresh context, so it still runs
// after the test context has ended. A 404 is tolerated: step 8 already deleted
// the box on the success path.
func liveDeleteBox(t *testing.T, client *upstashbox.Client, id string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), liveCleanupWait)
	defer cancel()
	err := client.Delete(ctx, id)
	if err == nil {
		t.Logf("cleanup: deleted box %s", id)
		return
	}
	var apiErr *upstashbox.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
		t.Logf("cleanup: box %s already gone (404)", id)
		return
	}
	t.Errorf("cleanup: delete box %s: %v", id, err)
}

// waitUntil polls cond until it holds or timeout passes, and reports whether it held.
func waitUntil(ctx context.Context, timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return true
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return false
		}
		time.Sleep(livePollEvery)
	}
}

// liveTokenOf returns the token query value of a raw CDP URL. A parse error
// is not printed, because url.Parse's message would repeat the URL.
func liveTokenOf(t *testing.T, raw string) string {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("step 4: the CDP URL does not parse (details withheld: it may carry the token)")
	}
	token := parsed.Query().Get("token")
	if token == "" {
		t.Fatalf("step 4: the CDP URL carries no token query value")
	}
	return token
}

// liveNavigateAndAwaitTitle navigates a tab to example.com and waits for the
// expected page title on the given key.
func liveNavigateAndAwaitTitle(ctx context.Context, t *testing.T, tab *cdp.Target, events *liveEvents, key string) {
	t.Helper()
	if err := tab.Navigate(ctx, liveExampleURL); err != nil {
		t.Fatalf("navigate %s to %s: %v", key, liveExampleURL, err)
	}
	ok := waitUntil(ctx, liveTitleWait, func() bool {
		for _, title := range events.titlesFor(key) {
			if title == liveExampleTitle {
				return true
			}
		}
		return false
	})
	if !ok {
		t.Fatalf("%s: page title never became %q within %s; titles seen: %q",
			key, liveExampleTitle, liveTitleWait, events.titlesFor(key))
	}
}

func TestLiveBoxBrowser(t *testing.T) {
	key := strings.TrimSpace(os.Getenv(liveKeyEnv))
	if key == "" {
		t.Skipf("%s is not set; the live Upstash Box test is skipped", liveKeyEnv)
	}
	client, err := upstashbox.New(key)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), liveTotalBudget)
	defer cancel()

	// Step 1: create an ephemeral, browser-enabled box.
	start := time.Now()
	name := fmt.Sprintf("ocode-livetest-%d", time.Now().Unix())
	box, err := client.Create(ctx, upstashbox.CreateOptions{
		Name:       name,
		Size:       upstashbox.SizeSmall,
		Browser:    true,
		Ephemeral:  true,
		TTLSeconds: liveTTLSeconds,
		Labels:     []string{liveLabel},
	})
	// Create can return a box together with an error, so the cleanup is
	// registered before the error is checked.
	if box != nil && box.ID != "" {
		boxID := box.ID
		t.Cleanup(func() { liveDeleteBox(t, client, boxID) })
	}
	if err != nil {
		t.Fatalf("step 1 Create: %v", err)
	}
	if box == nil || box.ID == "" {
		t.Fatalf("step 1 Create: no error, but no box id returned")
	}
	id := box.ID
	t.Logf("step 1 Create ok in %s: id=%s status=%s", time.Since(start).Round(time.Millisecond), id, box.Status)

	// Step 2: the box reads back as ephemeral with an expiry, and List finds it.
	start = time.Now()
	got, err := client.Get(ctx, id)
	if err != nil {
		t.Fatalf("step 2 Get: %v", err)
	}
	if !got.Ephemeral {
		t.Fatalf("step 2 Get: Ephemeral = false, want true")
	}
	if got.ExpiresAt.IsZero() {
		t.Fatalf("step 2 Get: ExpiresAt is zero, want a non-zero expiry")
	}
	if !got.Browser {
		t.Fatalf("step 2 Get: Browser = false, want true")
	}
	t.Logf("step 2 Get ok in %s: status=%s ephemeral=%v browser=%v expires_at=%s",
		time.Since(start).Round(time.Millisecond), got.Status, got.Ephemeral, got.Browser, got.ExpiresAt.Format(time.RFC3339))

	boxes, err := client.List(ctx, liveLabel)
	if err != nil {
		t.Fatalf("step 2 List(%q): %v", liveLabel, err)
	}
	listed := false
	for _, b := range boxes {
		if b.ID == id {
			listed = true
			break
		}
	}
	if !listed {
		t.Fatalf("step 2 List(%q): box %s not in the %d returned boxes", liveLabel, id, len(boxes))
	}
	t.Logf("step 2 List ok: %d box(es) with label %q, includes %s", len(boxes), liveLabel, id)

	// Step 3: an ephemeral box cannot be paused, so the API answers 400.
	err = client.Pause(ctx, id)
	var pauseErr *upstashbox.APIError
	if !errors.As(err, &pauseErr) {
		t.Fatalf("step 3 Pause: err = %v, want *upstashbox.APIError", err)
	}
	if pauseErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("step 3 Pause: HTTP %d (%s), want 400", pauseErr.StatusCode, pauseErr.Message)
	}
	t.Logf("step 3 Pause refused as expected: HTTP %d: %s", pauseErr.StatusCode, pauseErr.Message)

	// Step 4: the CDP URL is redacted in every printed form.
	cdpURL, err := client.CDPURL(ctx, id)
	if err != nil {
		t.Fatalf("step 4 CDPURL: %v", err)
	}
	token := liveTokenOf(t, cdpURL.RevealRaw())
	printed := map[string]string{
		"String()":   cdpURL.String(),
		"GoString()": cdpURL.GoString(),
		"%v":         fmt.Sprintf("%v", cdpURL),
		"%+v":        fmt.Sprintf("%+v", cdpURL),
		"%#v":        fmt.Sprintf("%#v", cdpURL),
		"%s":         fmt.Sprintf("%s", cdpURL),
	}
	for form, s := range printed {
		if strings.Contains(s, token) {
			t.Fatalf("step 4 CDPURL: the %s form carries the token", form)
		}
	}
	t.Logf("step 4 CDPURL ok: %d printed forms checked, none carries the token", len(printed))

	// Step 5: attach a browser manager and load example.com.
	events := &liveEvents{}
	m1 := newLiveManager(liveDialer(cdpURL), events)
	t.Cleanup(func() { liveCloseManager(t, m1) })

	start = time.Now()
	tab1, err := m1.Attach(ctx, liveTabOne, liveSink{})
	if err != nil {
		t.Fatalf("step 5 Attach: %v", err)
	}
	t.Logf("step 5 Attach ok in %s (includes the browser's first start)", time.Since(start).Round(time.Millisecond))

	start = time.Now()
	liveNavigateAndAwaitTitle(ctx, t, tab1, events, liveTabOne)
	t.Logf("step 5 title is %q after %s", liveExampleTitle, time.Since(start).Round(time.Millisecond))

	// Step 6: the box must not load the DevTools JSON or the metadata endpoint.
	for _, blocked := range []string{liveDevToolsURL, liveMetadataURL} {
		before := events.navErrorCount(liveTabOne)
		if err := tab1.Navigate(ctx, blocked); err != nil {
			t.Logf("step 6 Navigate to a blocked target returned an error (counts as not loaded): %v", err)
		}
		failed := waitUntil(ctx, liveBlockWait, func() bool {
			return events.navErrorCount(liveTabOne) > before
		})
		body, _, err := tab1.PageText(ctx)
		if err != nil {
			t.Fatalf("step 6 PageText after navigating to a blocked target: %v", err)
		}
		if strings.Contains(body, liveJSONMarker) {
			t.Fatalf("step 6: navigating to a blocked target loaded the DevTools JSON (body contains %q)", liveJSONMarker)
		}
		pageURL, _ := tab1.URL(ctx)
		readyState, _ := tab1.ReadyState(ctx)
		t.Logf("step 6 %s stayed off the content: failure event=%v page url=%q readyState=%q body chars=%d",
			blocked, failed, pageURL, readyState, len(body))
	}

	// Step 7: closing the manager must leave the box's browser running.
	liveCloseManager(t, m1)
	t.Logf("step 7 first manager closed")

	cdpURL2, err := client.CDPURL(ctx, id)
	if err != nil {
		t.Fatalf("step 7 CDPURL (second): %v", err)
	}
	events2 := &liveEvents{}
	m2 := newLiveManager(liveDialer(cdpURL2), events2)
	t.Cleanup(func() { liveCloseManager(t, m2) })

	tab2, err := m2.Attach(ctx, liveTabTwo, liveSink{})
	if err != nil {
		t.Fatalf("step 7 second Attach: %v", err)
	}
	liveNavigateAndAwaitTitle(ctx, t, tab2, events2, liveTabTwo)
	t.Logf("step 7 second manager loaded example.com after the first Close")
	liveCloseManager(t, m2)

	// Step 8: delete the box. The API soft-deletes, so Get still succeeds and
	// reports the deleted status, and List no longer returns the box.
	if err := client.Delete(ctx, id); err != nil {
		t.Fatalf("step 8 Delete: %v", err)
	}
	deleted, err := client.Get(ctx, id)
	if err != nil {
		t.Fatalf("step 8 Get after Delete: %v, want a readable box with status %q", err, upstashbox.StatusDeleted)
	}
	if deleted.Status != upstashbox.StatusDeleted {
		t.Fatalf("step 8 Get after Delete: Status = %q, want %q", deleted.Status, upstashbox.StatusDeleted)
	}
	afterDelete, err := client.List(ctx, liveLabel)
	if err != nil {
		t.Fatalf("step 8 List(%q) after Delete: %v", liveLabel, err)
	}
	for _, b := range afterDelete {
		if b.ID == id {
			t.Fatalf("step 8 List(%q) after Delete: deleted box %s is still listed", liveLabel, id)
		}
	}
	t.Logf("step 8 Get after Delete: status=%s; List(%q) omits the box", deleted.Status, liveLabel)
}

// livePrivateDeniedCIDRs are the ranges a box must not reach. They cover the
// RFC 1918 private ranges, link-local (cloud metadata) and loopback.
var livePrivateDeniedCIDRs = []string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"169.254.0.0/16",
	"127.0.0.0/8",
}

// TestLiveBoxNetworkPolicy creates an ephemeral browser box whose network
// policy denies the private ranges, then loads example.com through the attach
// manager.
//
// Observed 2026-11-21: custom mode with DeniedCIDRs alone is accepted by the
// API, but the browser then loads no page (navigation shows a chrome-error
// page). Custom mode with AllowedDomains ["example.com"] plus the same
// DeniedCIDRs loads example.com. The policy here therefore carries the domain
// allowlist as well.
func TestLiveBoxNetworkPolicy(t *testing.T) {
	key := strings.TrimSpace(os.Getenv(liveKeyEnv))
	if key == "" {
		t.Skipf("%s is not set; the live Upstash Box network policy test is skipped", liveKeyEnv)
	}
	client, err := upstashbox.New(key)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), liveTotalBudget)
	defer cancel()

	name := fmt.Sprintf("ocode-livepolicy-%d", time.Now().Unix())
	box, err := client.Create(ctx, upstashbox.CreateOptions{
		Name:       name,
		Size:       upstashbox.SizeSmall,
		Browser:    true,
		Ephemeral:  true,
		TTLSeconds: liveTTLSeconds,
		Labels:     []string{liveLabel},
		NetworkPolicy: &upstashbox.NetworkPolicy{
			Mode:           upstashbox.NetworkCustom,
			AllowedDomains: []string{"example.com"},
			DeniedCIDRs:    livePrivateDeniedCIDRs,
		},
	})
	if box != nil && box.ID != "" {
		boxID := box.ID
		t.Cleanup(func() { liveDeleteBox(t, client, boxID) })
	}
	if err != nil {
		t.Fatalf("Create with private-range deny policy: %v", err)
	}
	if box == nil || box.ID == "" {
		t.Fatalf("Create: no error, but no box id returned")
	}
	id := box.ID
	t.Logf("Create ok: id=%s status=%s", id, box.Status)

	cdpURL, err := client.CDPURL(ctx, id)
	if err != nil {
		t.Fatalf("CDPURL: %v", err)
	}
	events := &liveEvents{}
	m := newLiveManager(liveDialer(cdpURL), events)
	t.Cleanup(func() { liveCloseManager(t, m) })

	const tabKey = "tab:livepolicy-1"
	tab, err := m.Attach(ctx, tabKey, liveSink{})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	liveNavigateAndAwaitTitle(ctx, t, tab, events, tabKey)
	t.Logf("example.com loaded under the private-range deny policy")
}

// liveLoginBudget covers two boxes, each with its own browser start.
const liveLoginBudget = 8 * time.Minute

// The login-state fixtures are test values. The test checks their presence
// and never logs them.
const (
	liveLoginCookie     = "ocode_live_login"
	liveLoginSession    = "ocode_live_session"
	liveLoginLSKey      = "ocode_live_ls"
	liveLoginLSValue    = "live-login-value-1"
	liveLoginLSNewValue = "live-login-value-2"
	liveLoginOrigin     = "https://example.com"
	liveLoginMarker     = "window.__ocode_live_marker"
)

// liveCreateLoginBox creates an ephemeral browser box and registers its cleanup.
func liveCreateLoginBox(ctx context.Context, t *testing.T, client *upstashbox.Client, name string) string {
	t.Helper()
	box, err := client.Create(ctx, upstashbox.CreateOptions{
		Name:       name,
		Size:       upstashbox.SizeSmall,
		Browser:    true,
		Ephemeral:  true,
		TTLSeconds: liveTTLSeconds,
		Labels:     []string{liveLabel},
	})
	if box != nil && box.ID != "" {
		boxID := box.ID
		t.Cleanup(func() { liveDeleteBox(t, client, boxID) })
	}
	if err != nil {
		t.Fatalf("Create %s: %v", name, err)
	}
	if box == nil || box.ID == "" {
		t.Fatalf("Create %s: no error, but no box id returned", name)
	}
	return box.ID
}

// liveProbe evaluates a boolean in the tab. Any failure reads as false, so a
// waitUntil keeps polling while the page is between documents.
func liveProbe(ctx context.Context, tab *cdp.Target, expression string) bool {
	raw, err := tab.EvaluateValue(ctx, expression)
	if err != nil {
		return false
	}
	var ok bool
	return json.Unmarshal(raw, &ok) == nil && ok
}

// liveCheck evaluates a boolean in the tab and fails the test on an error.
func liveCheck(ctx context.Context, t *testing.T, tab *cdp.Target, expression string) bool {
	t.Helper()
	raw, err := tab.EvaluateValue(ctx, expression)
	if err != nil {
		t.Fatalf("evaluate %q: %v", expression, err)
	}
	var ok bool
	if err := json.Unmarshal(raw, &ok); err != nil {
		t.Fatalf("evaluate %q: result is not a boolean", expression)
	}
	return ok
}

// TestLiveBoxLoginStateRoundTrip saves a login state from box A (cookies and
// localStorage of example.com), deletes box A, and restores the state into box
// B. It checks that the cookies and the localStorage item are present in box
// B, and that the page's own value survives a reload.
func TestLiveBoxLoginStateRoundTrip(t *testing.T) {
	key := strings.TrimSpace(os.Getenv(liveKeyEnv))
	if key == "" {
		t.Skipf("%s is not set; the live Upstash Box login-state test is skipped", liveKeyEnv)
	}
	client, err := upstashbox.New(key)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), liveLoginBudget)
	defer cancel()

	// Box A: set a persistent cookie, a session cookie and a localStorage item
	// on example.com, then read them back through the manager.
	idA := liveCreateLoginBox(ctx, t, client, fmt.Sprintf("ocode-livelogin-a-%d", time.Now().Unix()))
	cdpA, err := client.CDPURL(ctx, idA)
	if err != nil {
		t.Fatalf("box A CDPURL: %v", err)
	}
	evA := &liveEvents{}
	mA := newLiveManager(liveDialer(cdpA), evA)
	t.Cleanup(func() { liveCloseManager(t, mA) })
	const keyA = "tab:livelogin-a"
	tabA, err := mA.Attach(ctx, keyA, liveSink{})
	if err != nil {
		t.Fatalf("box A Attach: %v", err)
	}
	liveNavigateAndAwaitTitle(ctx, t, tabA, evA, keyA)
	setState := fmt.Sprintf(`document.cookie = "%s=%s; max-age=3600; path=/";
document.cookie = "%s=%s; path=/";
localStorage.setItem("%s", "%s");
true;`, liveLoginCookie, liveLoginLSValue, liveLoginSession, liveLoginLSValue, liveLoginLSKey, liveLoginLSValue)
	if _, err := tabA.EvaluateValue(ctx, setState); err != nil {
		t.Fatalf("box A: set the login state: %v", err)
	}

	cookiesA, err := mA.StorageCookies(ctx)
	if err != nil {
		t.Fatalf("box A StorageCookies: %v", err)
	}
	snapA, err := mA.LocalStorageSnapshot(ctx)
	if err != nil {
		t.Fatalf("box A LocalStorageSnapshot: %v", err)
	}
	persistentOK, sessionOK := false, false
	for _, raw := range cookiesA {
		var c struct {
			Name    string  `json:"name"`
			Value   string  `json:"value"`
			Expires float64 `json:"expires"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatalf("box A: a cookie object does not decode: %v", err)
		}
		if c.Name == liveLoginCookie && c.Value == liveLoginLSValue {
			persistentOK = c.Expires > 0
		}
		if c.Name == liveLoginSession && c.Value == liveLoginLSValue {
			sessionOK = c.Expires == -1
		}
	}
	if !persistentOK {
		t.Fatalf("box A: the max-age cookie is missing or not persistent (%d cookies read)", len(cookiesA))
	}
	if !sessionOK {
		t.Fatalf("box A: the session cookie is missing or does not have expires -1 (%d cookies read)", len(cookiesA))
	}
	if snapA[liveLoginOrigin][liveLoginLSKey] != liveLoginLSValue {
		t.Fatalf("box A: the localStorage item is missing from the snapshot")
	}
	for origin, items := range snapA {
		if _, ok := items[loginStateSentinelForLive]; ok {
			t.Fatalf("box A: the snapshot for %s carries the reserved sentinel key", origin)
		}
	}
	t.Logf("box A: read %d cookies and %d origin(s); persistent cookie kept, session cookie expires -1", len(cookiesA), len(snapA))

	// Delete box A. The state is held in this test, not in the box.
	liveCloseManager(t, mA)
	if err := client.Delete(ctx, idA); err != nil {
		t.Fatalf("delete box A: %v", err)
	}
	t.Logf("box A deleted")

	// Box B: restore the state, then load example.com.
	idB := liveCreateLoginBox(ctx, t, client, fmt.Sprintf("ocode-livelogin-b-%d", time.Now().Unix()))
	cdpB, err := client.CDPURL(ctx, idB)
	if err != nil {
		t.Fatalf("box B CDPURL: %v", err)
	}
	evB := &liveEvents{}
	mB := newLiveManager(liveDialer(cdpB), evB)
	t.Cleanup(func() { liveCloseManager(t, mB) })
	if rejected, err := mB.SetStorageCookies(ctx, cookiesA); err != nil || rejected != 0 {
		t.Fatalf("box B SetStorageCookies: rejected %d, err %v", rejected, err)
	}
	if skipped, err := mB.SetLocalStorageSeed(ctx, snapA); err != nil || len(skipped) != 0 {
		t.Fatalf("box B SetLocalStorageSeed: skipped %v, err %v", skipped, err)
	}
	const keyB = "tab:livelogin-b"
	tabB, err := mB.Attach(ctx, keyB, liveSink{})
	if err != nil {
		t.Fatalf("box B Attach: %v", err)
	}
	liveNavigateAndAwaitTitle(ctx, t, tabB, evB, keyB)

	cookieDoc, err := tabB.EvaluateValue(ctx, "document.cookie")
	if err != nil {
		t.Fatalf("box B: read document.cookie: %v", err)
	}
	var docCookies string
	if err := json.Unmarshal(cookieDoc, &docCookies); err != nil {
		t.Fatalf("box B: document.cookie is not a string")
	}
	if !strings.Contains(docCookies, liveLoginCookie+"="+liveLoginLSValue) {
		t.Fatalf("box B: the persistent cookie is not in the page")
	}
	if !strings.Contains(docCookies, liveLoginSession+"=") {
		t.Fatalf("box B: the session cookie is not in the page")
	}
	if !liveCheck(ctx, t, tabB, fmt.Sprintf(`localStorage.getItem("%s") === "%s"`, liveLoginLSKey, liveLoginLSValue)) {
		t.Fatalf("box B: the localStorage item was not restored")
	}
	// The restore writes no key into the page. The origin is reported back by a
	// binding, and the Manager then stops seeding it. Wait for that before the
	// reloads below, so a reload is a real test of whether the seed comes back.
	if !liveCheck(ctx, t, tabB, fmt.Sprintf(`localStorage.getItem("%s") === null`, loginStateSentinelForLive)) {
		t.Fatalf("box B: the page holds the reserved key %s; the restore must not write page storage", loginStateSentinelForLive)
	}
	waitForSeedApplied(ctx, t, mB, liveLoginOrigin)
	t.Logf("box B: cookies present, localStorage restored, no page-visible key, origin reported applied")

	// The page changes its own value, then reloads. The restore must not run
	// again, so the page's value survives. The marker is set before the reload
	// and gone after it, which shows the document really was replaced.
	if _, err := tabB.EvaluateValue(ctx, fmt.Sprintf(`localStorage.setItem("%s", "%s"); %s = 1; true;`,
		liveLoginLSKey, liveLoginLSNewValue, liveLoginMarker)); err != nil {
		t.Fatalf("box B: set the page's own value: %v", err)
	}
	if err := tabB.Reload(ctx); err != nil {
		t.Fatalf("box B reload: %v", err)
	}
	reloaded := waitUntil(ctx, liveTitleWait, func() bool {
		return liveProbe(ctx, tabB, fmt.Sprintf(`%s === undefined && document.readyState === "complete"`, liveLoginMarker))
	})
	if !reloaded {
		t.Fatalf("box B: the page did not finish reloading within %s", liveTitleWait)
	}
	if !liveCheck(ctx, t, tabB, fmt.Sprintf(`localStorage.getItem("%s") === "%s"`, liveLoginLSKey, liveLoginLSNewValue)) {
		t.Fatalf("box B: the page's own value was overwritten on the second load")
	}
	t.Logf("box B: the page's own value survived a reload; the restore ran once")

	// The page clears its storage and reloads. The origin was already applied, so
	// the seed must not come back.
	if _, err := tabB.EvaluateValue(ctx, fmt.Sprintf(`localStorage.clear(); %s = 1; true;`, liveLoginMarker)); err != nil {
		t.Fatalf("box B: clear localStorage: %v", err)
	}
	if err := tabB.Reload(ctx); err != nil {
		t.Fatalf("box B reload after clear: %v", err)
	}
	reloaded = waitUntil(ctx, liveTitleWait, func() bool {
		return liveProbe(ctx, tabB, fmt.Sprintf(`%s === undefined && document.readyState === "complete"`, liveLoginMarker))
	})
	if !reloaded {
		t.Fatalf("box B: the page did not finish the reload after clear within %s", liveTitleWait)
	}
	if !liveCheck(ctx, t, tabB, fmt.Sprintf(`localStorage.getItem("%s") === null`, liveLoginLSKey)) {
		t.Fatalf("box B: the seed was re-applied after the page cleared its storage")
	}
	t.Logf("box B: after the page cleared its storage and reloaded, the seed was not re-applied")
}

// waitForSeedApplied waits until the Manager no longer holds origin's seed. That
// happens once a document of the origin applied it and reported back.
func waitForSeedApplied(ctx context.Context, t *testing.T, m *cdp.Manager, origin string) {
	t.Helper()
	applied := waitUntil(ctx, liveTitleWait, func() bool {
		for _, o := range m.LoginSeedUnapplied() {
			if o == origin {
				return false
			}
		}
		return true
	})
	if !applied {
		t.Fatalf("box B: the seed for %s was not reported applied within %s", origin, liveTitleWait)
	}
}

// loginStateSentinelForLive is the reserved localStorage key that marks a
// restored origin. It matches the cdp package's constant, which is unexported.
const loginStateSentinelForLive = "__ocode_ls_restored"
