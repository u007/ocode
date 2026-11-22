package cdp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const storageFullCookieJSON = `{"name":"sid","value":"v1","domain":"example.com","path":"/","expires":1900000000.5,"size":5,"httpOnly":true,"secure":true,"session":false,"sameSite":"Lax","priority":"Medium","sourceScheme":"Secure","sourcePort":443,"partitionKey":{"topLevelSite":"https://example.com","hasCrossSiteAncestor":false},"partitionKeyOpaque":false}`

const storageSessionCookieJSON = `{"name":"tmp","value":"v2","domain":"example.com","path":"/","expires":-1,"size":3,"httpOnly":false,"secure":false,"session":true,"priority":"Medium","sourceScheme":"Secure","sourcePort":443}`

// expiredCookieJSON expired in 1970, so it is dropped before it is sent.
const expiredCookieJSON = `{"name":"old","value":"x","domain":"example.com","path":"/","expires":1000}`

// seedAddCalls returns the login-state registrations sent so far, in order.
// The select picker also calls addScriptToEvaluateOnNewDocument, without a
// worldName, so it is filtered out here.
func seedAddCalls(stub *stubChrome) []stubCall {
	var out []stubCall
	for _, c := range stub.CallsFor("Page.addScriptToEvaluateOnNewDocument") {
		if paramOf(c, "worldName") == loginSeedWorld {
			out = append(out, c)
		}
	}
	return out
}

// seedAddCallsOn returns the login-state registrations sent on one session.
func seedAddCallsOn(stub *stubChrome, sessionID string) []stubCall {
	var out []stubCall
	for _, c := range seedAddCalls(stub) {
		if c.SessionID == sessionID {
			out = append(out, c)
		}
	}
	return out
}

// answerSeedIdentifiers makes every login-state registration return a script
// identifier, as a browser does, so a test reaches the behaviour it checks.
func answerSeedIdentifiers(stub *stubChrome) {
	var counter atomic.Int64
	stub.SetResponder(func(method, _ string, params json.RawMessage) (any, string, bool) {
		if method != "Page.addScriptToEvaluateOnNewDocument" || !strings.Contains(string(params), `"worldName"`) {
			return nil, "", false
		}
		return map[string]string{"identifier": fmt.Sprintf("seed-%d", counter.Add(1))}, "", true
	})
}

// redialManager returns an attach Manager whose n-th dial reaches stubs[n].
// Closing a stub drops that connection, and the next dial takes the next stub.
func redialManager(t *testing.T, stubs ...*stubChrome) *Manager {
	t.Helper()
	var dials atomic.Int64
	m := NewAttachManager(ManagerOptions{Log: log.New(io.Discard, "", 0)}, func(context.Context) (*Conn, error) {
		i := int(dials.Add(1)) - 1
		if i >= len(stubs) {
			return nil, errors.New("no stub left to dial")
		}
		return stubs[i].Conn(), nil
	})
	m.resolveHost = testResolver
	t.Cleanup(func() { _ = m.Close(context.Background()) })
	return m
}

// waitDisconnected waits until the Manager has noticed that its connection ended.
func waitDisconnected(t *testing.T, m *Manager) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		conn := m.conn
		m.mu.Unlock()
		if conn == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the manager never noticed the dropped connection")
}

func TestStorageAPIsRefuseNonAttachManager(t *testing.T) {
	m := NewManager(ManagerOptions{})
	ctx := context.Background()
	seed := map[string]map[string]string{"https://example.com": {"k": "v"}}

	if _, err := m.StorageCookies(ctx); !errors.Is(err, errNotAttachMode) {
		t.Errorf("StorageCookies: err = %v, want errNotAttachMode", err)
	}
	cookie := []json.RawMessage{json.RawMessage(storageFullCookieJSON)}
	if _, err := m.SetStorageCookies(ctx, cookie); !errors.Is(err, errNotAttachMode) {
		t.Errorf("SetStorageCookies: err = %v, want errNotAttachMode", err)
	}
	if _, err := m.SetStorageCookies(ctx, nil); !errors.Is(err, errNotAttachMode) {
		t.Errorf("SetStorageCookies(nil): err = %v, want errNotAttachMode (mode check comes first)", err)
	}
	if _, err := m.LocalStorageSnapshot(ctx); !errors.Is(err, errNotAttachMode) {
		t.Errorf("LocalStorageSnapshot: err = %v, want errNotAttachMode", err)
	}
	if _, err := m.SetLocalStorageSeed(ctx, seed); !errors.Is(err, errNotAttachMode) {
		t.Errorf("SetLocalStorageSeed: err = %v, want errNotAttachMode", err)
	}
	if _, err := m.SetLocalStorageSeed(ctx, nil); !errors.Is(err, errNotAttachMode) {
		t.Errorf("SetLocalStorageSeed(nil): err = %v, want errNotAttachMode", err)
	}
}

// SetContextRestore is a no-op outside attach mode: the hook is never kept,
// and the storage reads still refuse the Manager.
func TestSetContextRestoreOutsideAttachIsNoOp(t *testing.T) {
	m := NewManager(ManagerOptions{})
	m.SetContextRestore(func(context.Context) ([]json.RawMessage, map[string]map[string]string, error) {
		t.Error("restore hook ran on a non-attach Manager")
		return nil, nil, nil
	})
	if _, err := m.StorageCookies(context.Background()); !errors.Is(err, errNotAttachMode) {
		t.Fatalf("StorageCookies: err = %v, want errNotAttachMode", err)
	}
}

func TestStorageCookiesWithoutContextIsEmpty(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := attachTestManager(t, stub)
	ctx := context.Background()

	// Never connected, and never had a shared context: an empty jar is the
	// truth, and no browser was reached, so there is nothing to dial.
	got, err := m.StorageCookies(ctx)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("never-connected read = %v, %v; want an empty slice and no error", got, err)
	}
	if n := len(stub.Calls()); n != 0 {
		t.Fatalf("never-connected read sent %d commands; want none (it must not dial)", n)
	}

	// Connected, but no tab was ever opened: still no shared context.
	if err := m.ensureChrome(ctx); err != nil {
		t.Fatalf("ensureChrome: %v", err)
	}
	got, err = m.StorageCookies(ctx)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("no-context read = %v, %v; want an empty slice and no error", got, err)
	}
	if n := len(stub.CallsFor("Storage.getCookies")); n != 0 {
		t.Fatalf("no-context read sent Storage.getCookies %d times; want 0", n)
	}
}

func TestStorageCookiesReturnsEachObjectVerbatim(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := attachTestManager(t, stub)
	ctx := context.Background()

	tab, _, err := m.EnsureHeadless(ctx, "tab:cookies")
	if err != nil {
		t.Fatalf("EnsureHeadless: %v", err)
	}
	stub.SetResult("Storage.getCookies", map[string]any{
		"cookies": []json.RawMessage{json.RawMessage(storageFullCookieJSON), json.RawMessage(storageSessionCookieJSON)},
	})
	got, err := m.StorageCookies(ctx)
	if err != nil {
		t.Fatalf("StorageCookies: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("StorageCookies returned %d objects, want 2", len(got))
	}
	if string(got[0]) != storageFullCookieJSON || string(got[1]) != storageSessionCookieJSON {
		t.Fatalf("cookie objects were not returned verbatim:\n%s\n%s", got[0], got[1])
	}
	calls := stub.CallsFor("Storage.getCookies")
	if len(calls) != 1 || paramOf(calls[0], "browserContextId") != tab.browserContextID {
		t.Fatalf("Storage.getCookies calls = %v; want one call for the shared context %q", calls, tab.browserContextID)
	}
}

func TestSetStorageCookiesStripsToCookieParam(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := attachTestManager(t, stub)
	ctx := context.Background()

	// No tab yet: SetStorageCookies creates the shared context itself.
	cookies := []json.RawMessage{json.RawMessage(storageFullCookieJSON), json.RawMessage(storageSessionCookieJSON)}
	if rejected, err := m.SetStorageCookies(ctx, cookies); err != nil || rejected != 0 {
		t.Fatalf("SetStorageCookies = %d, %v; want 0 rejected and no error", rejected, err)
	}
	if n := len(stub.CallsFor("Target.createBrowserContext")); n != 1 {
		t.Fatalf("createBrowserContext sent %d times, want 1", n)
	}
	sets := stub.CallsFor("Storage.setCookies")
	if len(sets) != 1 {
		t.Fatalf("Storage.setCookies sent %d times, want 1", len(sets))
	}
	bc := paramOf(sets[0], "browserContextId")
	if bc == "" {
		t.Fatal("Storage.setCookies carried no browserContextId")
	}
	// A tab opened afterwards must land in the context the cookies went into.
	tab, _, err := m.EnsureHeadless(ctx, "tab:after")
	if err != nil {
		t.Fatalf("EnsureHeadless: %v", err)
	}
	if tab.browserContextID != bc {
		t.Fatalf("tab context %q, cookies set in %q", tab.browserContextID, bc)
	}

	var p struct {
		Cookies []map[string]json.RawMessage `json:"cookies"`
	}
	if err := json.Unmarshal(sets[0].Params, &p); err != nil {
		t.Fatalf("decode Storage.setCookies params: %v", err)
	}
	if len(p.Cookies) != 2 {
		t.Fatalf("Storage.setCookies got %d cookies, want 2", len(p.Cookies))
	}
	persistent, session := p.Cookies[0], p.Cookies[1]
	for _, dropped := range []string{"size", "session", "partitionKeyOpaque"} {
		if _, ok := persistent[dropped]; ok {
			t.Errorf("persistent cookie kept %q, which CookieParam does not accept", dropped)
		}
	}
	for _, kept := range []string{"name", "value", "domain", "path", "secure", "httpOnly", "sameSite", "expires", "priority", "sourceScheme", "sourcePort", "partitionKey"} {
		if _, ok := persistent[kept]; !ok {
			t.Errorf("persistent cookie lost %q", kept)
		}
	}
	if string(persistent["expires"]) != "1900000000.5" {
		t.Errorf("persistent cookie expires = %s, want the original 1900000000.5", persistent["expires"])
	}
	if _, ok := session["expires"]; ok {
		t.Errorf("session cookie kept expires %s; it must be dropped so the cookie stays a session cookie", session["expires"])
	}
	if string(session["name"]) != `"tmp"` || string(session["value"]) != `"v2"` {
		t.Errorf("session cookie lost its name or value")
	}

	// Empty input is a no-op.
	if rejected, err := m.SetStorageCookies(ctx, nil); err != nil || rejected != 0 {
		t.Fatalf("SetStorageCookies(nil) = %d, %v; want 0 and no error", rejected, err)
	}
	if n := len(stub.CallsFor("Storage.setCookies")); n != 1 {
		t.Fatalf("empty input sent Storage.setCookies; total %d, want 1", n)
	}
	// A cookie that is not an object is refused before anything is sent. It
	// is the only cookie, so every cookie was rejected and that is an error.
	if _, err := m.SetStorageCookies(ctx, []json.RawMessage{json.RawMessage(`"not-an-object"`)}); err == nil {
		t.Fatal("non-object cookie was accepted")
	}
	if n := len(stub.CallsFor("Storage.setCookies")); n != 1 {
		t.Fatalf("a refused cookie still reached Storage.setCookies; total %d, want 1", n)
	}
	// A closed Manager never dials the browser again.
	if err := m.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := m.SetStorageCookies(ctx, cookies); !errors.Is(err, errManagerClosed) {
		t.Fatalf("SetStorageCookies after Close: err = %v, want errManagerClosed", err)
	}
}

// SetStorageCookies reports a partial rejection by count: a refused batch is
// retried one cookie at a time, so only the bad cookie is lost.
func TestSetStorageCookiesRejectsOnlyTheBadOne(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := attachTestManager(t, stub)
	ctx := context.Background()

	stub.SetResponder(func(method, _ string, params json.RawMessage) (any, string, bool) {
		if method != "Storage.setCookies" {
			return nil, "", false
		}
		if strings.Contains(string(params), `"bad"`) {
			return nil, "invalid cookie", true
		}
		return map[string]any{}, "", true
	})
	cookies := []json.RawMessage{
		json.RawMessage(storageSessionCookieJSON),
		json.RawMessage(`{"name":"bad","value":"v","domain":"example.com","path":"/"}`),
		json.RawMessage(`{"name":"keep","value":"k","domain":"example.com","path":"/"}`),
	}
	rejected, err := m.SetStorageCookies(ctx, cookies)
	if err != nil {
		t.Fatalf("SetStorageCookies: %v; a partial rejection is not an error", err)
	}
	if rejected != 1 {
		t.Fatalf("rejected = %d, want 1 (only the bad cookie)", rejected)
	}
	// One refused batch, then one call per cookie.
	if n := len(stub.CallsFor("Storage.setCookies")); n != 4 {
		t.Fatalf("Storage.setCookies sent %d times, want 4 (batch, then one per cookie)", n)
	}
}

// SetStorageCookies drops cookies whose expiry is already past, before any
// call, and counts them as rejected. When nothing is left to send, it returns
// an error and never touches the browser.
func TestSetStorageCookiesDropsExpiredBeforeSending(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := attachTestManager(t, stub)
	ctx := context.Background()

	rejected, err := m.SetStorageCookies(ctx, []json.RawMessage{json.RawMessage(expiredCookieJSON), json.RawMessage(storageSessionCookieJSON)})
	if err != nil || rejected != 0 {
		t.Fatalf("SetStorageCookies = %d, %v; want 0 rejected (an expired cookie is filtered, not refused) and no error", rejected, err)
	}
	sets := stub.CallsFor("Storage.setCookies")
	if len(sets) != 1 {
		t.Fatalf("Storage.setCookies sent %d times, want 1", len(sets))
	}
	var p struct {
		Cookies []map[string]json.RawMessage `json:"cookies"`
	}
	if err := json.Unmarshal(sets[0].Params, &p); err != nil {
		t.Fatalf("decode Storage.setCookies params: %v", err)
	}
	if len(p.Cookies) != 1 || string(p.Cookies[0]["name"]) != `"tmp"` {
		t.Fatalf("Storage.setCookies carried %d cookies; want only the session cookie", len(p.Cookies))
	}

	// Only expired cookies is a saved jar that simply expired: nothing to set,
	// and no error, so a restore never fails on it.
	stub2 := newStubChrome()
	defer stub2.Close()
	m2 := attachTestManager(t, stub2)
	rejected, err = m2.SetStorageCookies(ctx, []json.RawMessage{json.RawMessage(expiredCookieJSON)})
	if err != nil || rejected != 0 {
		t.Fatalf("all-expired SetStorageCookies = %d, %v; want 0 rejected and no error", rejected, err)
	}
	if n := len(stub2.Calls()); n != 0 {
		t.Fatalf("all-expired input sent %d commands; want none", n)
	}
}

func TestLocalStorageSnapshotMergesOriginsAndReportsFailures(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := attachTestManager(t, stub)
	ctx := context.Background()

	objectReply := func(origin string, pairs [][2]string) map[string]any {
		return map[string]any{"result": map[string]any{
			"type":  "object",
			"value": map[string]any{"origin": origin, "items": pairs},
		}}
	}
	nullReply := map[string]any{"result": map[string]any{"type": "object", "subtype": "null", "value": nil}}

	sessions := map[string]string{}
	for _, key := range []string{"tab:a", "tab:b", "tab:c", "tab:d", "tab:e", "tab:f"} {
		tab, _, err := m.EnsureHeadless(ctx, key)
		if err != nil {
			t.Fatalf("EnsureHeadless %s: %v", key, err)
		}
		sessions[key] = tab.sessionID
	}
	replies := map[string]map[string]any{
		sessions["tab:a"]: objectReply("https://example.com", [][2]string{{"k1", "from-a"}, {"k2", "only-a"}}),
		sessions["tab:b"]: objectReply("https://example.com", [][2]string{{"k1", "from-b"}, {legacyLoginSentinel, "1"}}),
		sessions["tab:c"]: objectReply("https://other.example", [][2]string{{"x", "1"}}),
		sessions["tab:e"]: nullReply, // about:blank: not http(s), skipped silently
		sessions["tab:f"]: objectReply("https://sentinel.example", [][2]string{{legacyLoginSentinel, "1"}}),
	}
	stub.SetResponder(func(method, sessionID string, _ json.RawMessage) (any, string, bool) {
		if method != "Runtime.evaluate" {
			return nil, "", false
		}
		if sessionID == sessions["tab:d"] {
			return nil, "Execution context was destroyed.", true
		}
		if reply, ok := replies[sessionID]; ok {
			return reply, "", true
		}
		return nil, "", false
	})

	snap, err := m.LocalStorageSnapshot(ctx)
	if err == nil || !strings.Contains(err.Error(), "tab:d") {
		t.Fatalf("err = %v, want a report naming the failing tab tab:d", err)
	}
	if strings.Contains(err.Error(), "from-") {
		t.Fatalf("snapshot error carries a storage value: %v", err)
	}
	// The partial map is still returned. tab:b is read after tab:a, so its
	// k1 wins. The legacy sentinel is dropped. An origin that held only the
	// sentinel is still reported, as an empty map: it was read, and it is empty.
	want := map[string]map[string]string{
		"https://example.com":      {"k1": "from-b", "k2": "only-a"},
		"https://other.example":    {"x": "1"},
		"https://sentinel.example": {},
	}
	if !reflect.DeepEqual(snap, want) {
		t.Fatalf("snapshot = %v, want %v", snap, want)
	}
}

// An origin read successfully with zero items is reported as an empty map, so
// a caller can save "cleared" rather than losing the origin.
func TestLocalStorageSnapshotKeepsClearedOrigin(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := attachTestManager(t, stub)
	ctx := context.Background()

	tab, _, err := m.EnsureHeadless(ctx, "tab:cleared")
	if err != nil {
		t.Fatalf("EnsureHeadless: %v", err)
	}
	stub.SetResponder(func(method, sessionID string, _ json.RawMessage) (any, string, bool) {
		if method != "Runtime.evaluate" || sessionID != tab.sessionID {
			return nil, "", false
		}
		return map[string]any{"result": map[string]any{"type": "object", "value": map[string]any{
			"origin": "https://cleared.example", "items": [][2]string{},
		}}}, "", true
	})
	snap, err := m.LocalStorageSnapshot(ctx)
	if err != nil {
		t.Fatalf("LocalStorageSnapshot: %v", err)
	}
	items, ok := snap["https://cleared.example"]
	if !ok || len(items) != 0 {
		t.Fatalf("snapshot = %v; a cleared origin must be reported as an empty map", snap)
	}
}

// An origin whose items total over the capture limit is dropped and named in
// the error, with its size, and never with a value.
func TestLocalStorageSnapshotDropsOversizeOrigin(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := attachTestManager(t, stub)
	ctx := context.Background()

	big, _, err := m.EnsureHeadless(ctx, "tab:big")
	if err != nil {
		t.Fatalf("EnsureHeadless big: %v", err)
	}
	small, _, err := m.EnsureHeadless(ctx, "tab:small")
	if err != nil {
		t.Fatalf("EnsureHeadless small: %v", err)
	}
	half := strings.Repeat("s", 600<<10)
	stub.SetResponder(func(method, sessionID string, _ json.RawMessage) (any, string, bool) {
		if method != "Runtime.evaluate" {
			return nil, "", false
		}
		switch sessionID {
		case big.sessionID:
			return map[string]any{"result": map[string]any{"type": "object", "value": map[string]any{
				"origin": "https://big.example", "items": [][2]string{{"a", half}, {"b", half}},
			}}}, "", true
		case small.sessionID:
			return map[string]any{"result": map[string]any{"type": "object", "value": map[string]any{
				"origin": "https://small.example", "items": [][2]string{{"k", "v"}},
			}}}, "", true
		}
		return nil, "", false
	})

	snap, err := m.LocalStorageSnapshot(ctx)
	if err == nil || !strings.Contains(err.Error(), "https://big.example") {
		t.Fatalf("err = %v, want a report naming the dropped origin", err)
	}
	if strings.Contains(err.Error(), "sss") {
		t.Fatalf("the capture error carries a storage value: %v", err)
	}
	if _, ok := snap["https://big.example"]; ok {
		t.Fatal("an origin over the capture limit was kept")
	}
	if snap["https://small.example"]["k"] != "v" {
		t.Fatalf("the small origin was lost: %v", snap)
	}
}

// Attach mode only: a redial that finds the browser still holding the shared
// context reuses it. Nothing is disposed, and the cookie read goes to the same
// context. While disconnected, the reads fail rather than look empty.
func TestRedialReusesSharedContextStillPresent(t *testing.T) {
	stub1 := newStubChrome()
	defer stub1.Close()
	stub2 := newStubChrome()
	defer stub2.Close()
	m := redialManager(t, stub1, stub2)
	ctx := context.Background()

	a, _, err := m.EnsureHeadless(ctx, "tab:a")
	if err != nil {
		t.Fatalf("EnsureHeadless: %v", err)
	}
	bc := a.browserContextID

	// Only the socket dropped: the browser still lists the context.
	stub2.SetResult("Target.getBrowserContexts", map[string]any{"browserContextIds": []string{bc}})
	stub2.SetResult("Storage.getCookies", map[string]any{"cookies": []json.RawMessage{json.RawMessage(storageFullCookieJSON)}})

	stub1.Close()
	waitDisconnected(t, m)

	if _, err := m.StorageCookies(ctx); !errors.Is(err, errNotConnected) {
		t.Fatalf("StorageCookies while disconnected: err = %v, want errNotConnected", err)
	}
	if _, err := m.LocalStorageSnapshot(ctx); !errors.Is(err, errNotConnected) {
		t.Fatalf("LocalStorageSnapshot while disconnected: err = %v, want errNotConnected", err)
	}

	// A redial with no new tab: the next read re-establishes the same context.
	if err := m.ensureChrome(ctx); err != nil {
		t.Fatalf("redial: %v", err)
	}
	got, err := m.StorageCookies(ctx)
	if err != nil || len(got) != 1 {
		t.Fatalf("StorageCookies after redial = %v, %v; want the one cookie", got, err)
	}
	reads := stub2.CallsFor("Storage.getCookies")
	if len(reads) != 1 || paramOf(reads[0], "browserContextId") != bc {
		t.Fatalf("Storage.getCookies calls = %v; want one call for the reused context %q", reads, bc)
	}
	if n := len(stub2.CallsFor("Target.createBrowserContext")); n != 0 {
		t.Fatalf("a reused context was replaced: %d createBrowserContext calls on the new connection", n)
	}
	for name, s := range map[string]*stubChrome{"stub1": stub1, "stub2": stub2} {
		if n := len(s.CallsFor("Target.disposeBrowserContext")); n != 0 {
			t.Fatalf("%s disposed %d browser contexts; a reused context must never be disposed", name, n)
		}
	}
	b, _, err := m.EnsureHeadless(ctx, "tab:b")
	if err != nil {
		t.Fatalf("EnsureHeadless after redial: %v", err)
	}
	if b.browserContextID != bc {
		t.Fatalf("tab after redial in context %q, want the reused %q", b.browserContextID, bc)
	}
}

// When the browser no longer has the shared context, the replacement is
// restored before any tab is created in it: the hook runs first, its cookies
// are applied, and its seed replaces the old source for new tabs.
func TestRedialRestoresReplacementContextBeforeTabs(t *testing.T) {
	stub1 := newStubChrome()
	defer stub1.Close()
	stub2 := newStubChrome()
	defer stub2.Close()
	answerSeedIdentifiers(stub1)
	answerSeedIdentifiers(stub2)
	m := redialManager(t, stub1, stub2)
	ctx := context.Background()

	if _, _, err := m.EnsureHeadless(ctx, "tab:a"); err != nil {
		t.Fatalf("EnsureHeadless: %v", err)
	}
	if _, err := m.SetLocalStorageSeed(ctx, map[string]map[string]string{"https://example.com": {"k": "old"}}); err != nil {
		t.Fatalf("SetLocalStorageSeed: %v", err)
	}

	hookRuns, targetsAtHook := 0, -1
	m.SetContextRestore(func(context.Context) ([]json.RawMessage, map[string]map[string]string, error) {
		hookRuns++
		targetsAtHook = len(stub2.CallsFor("Target.createTarget"))
		return []json.RawMessage{json.RawMessage(storageFullCookieJSON)},
			map[string]map[string]string{"https://example.com": {"k": "restored"}}, nil
	})

	stub1.Close()
	waitDisconnected(t, m)

	b, _, err := m.EnsureHeadless(ctx, "tab:b")
	if err != nil {
		t.Fatalf("attach after the browser was replaced: %v", err)
	}
	if hookRuns != 1 {
		t.Fatalf("restore hook ran %d times, want 1", hookRuns)
	}
	if targetsAtHook != 0 {
		t.Fatalf("restore hook ran after %d tabs were created in the new browser; it must run first", targetsAtHook)
	}
	sets := stub2.CallsFor("Storage.setCookies")
	if len(sets) != 1 || paramOf(sets[0], "browserContextId") != b.browserContextID {
		t.Fatalf("Storage.setCookies calls = %v; want the restored cookies in the new context %q", sets, b.browserContextID)
	}
	adds := seedAddCallsOn(stub2, b.sessionID)
	if len(adds) != 1 || !seedSessionSawSource(adds, b.sessionID, `"restored"`) || seedSessionSawSource(adds, b.sessionID, `"old"`) {
		t.Fatalf("tab in the replaced browser got %d login-state registrations; want the restored seed only", len(adds))
	}
}

// A saved jar whose cookies have all expired is a normal jar after a while. A
// restore from it must succeed: nothing is set, the attach opens, and the seed
// still applies. It must not fail the attach on every restart.
func TestRedialRestoreWithOnlyExpiredCookiesSucceeds(t *testing.T) {
	stub1 := newStubChrome()
	defer stub1.Close()
	stub2 := newStubChrome()
	defer stub2.Close()
	m := redialManager(t, stub1, stub2)
	ctx := context.Background()

	if _, _, err := m.EnsureHeadless(ctx, "tab:a"); err != nil {
		t.Fatalf("EnsureHeadless: %v", err)
	}
	stub1.Close()
	waitDisconnected(t, m)

	m.SetContextRestore(func(context.Context) ([]json.RawMessage, map[string]map[string]string, error) {
		return []json.RawMessage{json.RawMessage(expiredCookieJSON)}, nil, nil
	})
	if _, _, err := m.EnsureHeadless(ctx, "tab:b"); err != nil {
		t.Fatalf("attach from a jar of only expired cookies: %v", err)
	}
	if n := len(stub2.CallsFor("Storage.setCookies")); n != 0 {
		t.Fatalf("Storage.setCookies sent %d times for an expired-only jar, want 0", n)
	}
}

// A restore that fails fails the attach. No tab opens in the unrestored
// context, and the next attach restores again instead of reusing it.
func TestRedialRestoreFailureFailsAttachAndRetries(t *testing.T) {
	stub1 := newStubChrome()
	defer stub1.Close()
	stub2 := newStubChrome()
	defer stub2.Close()
	m := redialManager(t, stub1, stub2)
	ctx := context.Background()

	if _, _, err := m.EnsureHeadless(ctx, "tab:a"); err != nil {
		t.Fatalf("EnsureHeadless: %v", err)
	}
	stub1.Close()
	waitDisconnected(t, m)

	runs := 0
	m.SetContextRestore(func(context.Context) ([]json.RawMessage, map[string]map[string]string, error) {
		runs++
		return nil, nil, errors.New("restore store unavailable")
	})
	if _, _, err := m.EnsureHeadless(ctx, "tab:b"); err == nil || !strings.Contains(err.Error(), "restore store unavailable") {
		t.Fatalf("attach with a failing restore: err = %v, want the restore error", err)
	}
	if n := len(stub2.CallsFor("Target.createTarget")); n != 0 {
		t.Fatalf("a tab was created in an unrestored context (%d createTarget calls)", n)
	}
	if n := len(stub2.CallsFor("Storage.setCookies")); n != 0 {
		t.Fatalf("cookies were applied after a failed restore (%d calls)", n)
	}

	m.SetContextRestore(func(context.Context) ([]json.RawMessage, map[string]map[string]string, error) {
		runs++
		return nil, nil, nil
	})
	if _, _, err := m.EnsureHeadless(ctx, "tab:c"); err != nil {
		t.Fatalf("attach after the restore recovered: %v", err)
	}
	if runs != 2 {
		t.Fatalf("restore hook ran %d times, want 2 (the failed context was not kept)", runs)
	}
}

// With no restore hook, a replaced context is created empty, the attach
// succeeds, and every storage read fails rather than returning an empty jar.
func TestRedialWithoutRestoreHookFailsStorageReads(t *testing.T) {
	stub1 := newStubChrome()
	defer stub1.Close()
	stub2 := newStubChrome()
	defer stub2.Close()
	m := redialManager(t, stub1, stub2)
	ctx := context.Background()

	if _, _, err := m.EnsureHeadless(ctx, "tab:a"); err != nil {
		t.Fatalf("EnsureHeadless: %v", err)
	}
	stub1.Close()
	waitDisconnected(t, m)

	if _, _, err := m.EnsureHeadless(ctx, "tab:b"); err != nil {
		t.Fatalf("attach without a restore hook: %v", err)
	}
	if n := len(stub2.CallsFor("Storage.setCookies")); n != 0 {
		t.Fatalf("cookies were applied without a restore (%d calls)", n)
	}
	if _, err := m.StorageCookies(ctx); !errors.Is(err, errReplacedWithoutRestore) {
		t.Fatalf("StorageCookies: err = %v, want errReplacedWithoutRestore", err)
	}
	if _, err := m.LocalStorageSnapshot(ctx); !errors.Is(err, errReplacedWithoutRestore) {
		t.Fatalf("LocalStorageSnapshot: err = %v, want errReplacedWithoutRestore", err)
	}
}

func TestLoginSeedRegistersOnTabsAndReplaces(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := attachTestManager(t, stub)
	ctx := context.Background()

	var counter atomic.Int64
	stub.SetResponder(func(method, _ string, params json.RawMessage) (any, string, bool) {
		if method != "Page.addScriptToEvaluateOnNewDocument" || !strings.Contains(string(params), `"worldName"`) {
			return nil, "", false
		}
		return map[string]string{"identifier": fmt.Sprintf("seed-%d", counter.Add(1))}, "", true
	})

	// A tab opened before any seed gets no login-state script.
	tab0, _, err := m.OpenHeadless(ctx)
	if err != nil {
		t.Fatalf("OpenHeadless before seed: %v", err)
	}
	if n := len(seedAddCalls(stub)); n != 0 {
		t.Fatalf("a tab with no seed got %d login-state scripts, want 0", n)
	}

	// The first seed reaches the tab that already exists.
	seed1 := map[string]map[string]string{
		"https://example.com": {"k": "v1", "tricky\"key</script> ": "x"},
	}
	if _, err := m.SetLocalStorageSeed(ctx, seed1); err != nil {
		t.Fatalf("SetLocalStorageSeed: %v", err)
	}
	adds := seedAddCalls(stub)
	if len(adds) != 1 || adds[0].SessionID != tab0.sessionID {
		t.Fatalf("after the first seed: %d registrations, want 1 on tab0 (%s)", len(adds), tab0.sessionID)
	}
	tabA, _, err := m.EnsureHeadless(ctx, "tab:a")
	if err != nil {
		t.Fatalf("EnsureHeadless: %v", err)
	}
	if adds = seedAddCalls(stub); len(adds) != 2 || adds[1].SessionID != tabA.sessionID {
		t.Fatalf("a tab created after the seed was not registered (got %d)", len(adds))
	}

	// The script is isolated, top-frame only, carries no page-visible key, and
	// carries the seed as JSON whose values survive exactly, quotes and
	// </script> and U+2028 included.
	src := paramOf(adds[0], "source")
	if !strings.Contains(src, "window.top !== window") {
		t.Fatalf("restore script misses the top-frame check")
	}
	if strings.Contains(src, "__ocode_ls_restored") {
		t.Fatalf("restore script writes the page-visible sentinel key")
	}
	wantOrigin, wantItems := "https://example.com", [][2]string{{"k", "v1"}, {"tricky\"key</script> ", "x"}}
	if got := seedEntriesFromSource(t, src); len(got) != 1 || got[0].Origin != wantOrigin || !reflect.DeepEqual(got[0].Items, wantItems) {
		t.Fatalf("seed in script = %v, want origin %s with items %v", got, wantOrigin, wantItems)
	}

	// A tab opened later, the popup path included, is registered too.
	tabB, _, err := m.OpenHeadless(ctx)
	if err != nil {
		t.Fatalf("OpenHeadless: %v", err)
	}
	if adds = seedAddCalls(stub); len(adds) != 3 || adds[2].SessionID != tabB.sessionID {
		t.Fatalf("a tab opened later was not registered (got %d)", len(adds))
	}
	stub.InjectEvent("", "Target.targetCreated", map[string]any{"targetInfo": map[string]any{
		"targetId":         "popup-target",
		"type":             "page",
		"url":              "",
		"openerId":         tabA.targetID,
		"attached":         false,
		"browserContextId": tabA.browserContextID,
	}})
	deadline := time.Now().Add(5 * time.Second)
	for len(m.TargetKeys()) < 4 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := len(m.TargetKeys()); n != 4 {
		t.Fatalf("popup was not registered as a tab (targets = %d)", n)
	}
	if adds = seedAddCalls(stub); len(adds) != 4 {
		t.Fatalf("popup got %d registrations in total, want 4", len(adds))
	}
	popupSession := adds[3].SessionID

	// A second seed replaces the first. Each old script is removed from the
	// tab it was registered on, and every tab gets the new one.
	seed2 := map[string]map[string]string{"https://example.com": {"k": "v2"}}
	if _, err := m.SetLocalStorageSeed(ctx, seed2); err != nil {
		t.Fatalf("second SetLocalStorageSeed: %v", err)
	}
	removes := stub.CallsFor("Page.removeScriptToEvaluateOnNewDocument")
	if len(removes) != 4 {
		t.Fatalf("second seed removed %d scripts, want 4", len(removes))
	}
	byID := map[string]string{}
	for i, c := range adds {
		byID[fmt.Sprintf("seed-%d", i+1)] = c.SessionID
	}
	for _, c := range removes {
		id := paramOf(c, "identifier")
		if sess, ok := byID[id]; !ok || sess != c.SessionID {
			t.Fatalf("removed script %q on session %q, which did not register it", id, c.SessionID)
		}
	}
	adds = seedAddCalls(stub)
	if len(adds) != 8 {
		t.Fatalf("after replacement: %d registrations, want 8 (4 old + 4 new)", len(adds))
	}
	for _, sess := range []string{tab0.sessionID, tabA.sessionID, tabB.sessionID, popupSession} {
		if !seedSessionSawSource(adds[4:], sess, `"v2"`) {
			t.Fatalf("session %s did not receive the replacement seed", sess)
		}
	}

	// An origin whose script would exceed the limit is skipped and named; the
	// rest of the seed still applies to every tab.
	huge := map[string]map[string]string{
		"https://big.example": {"big": strings.Repeat("x", maxLoginScriptBytes)},
		"https://example.com": {"k": "v3"},
	}
	before := len(seedAddCalls(stub))
	skipped, err := m.SetLocalStorageSeed(ctx, huge)
	if err != nil || !reflect.DeepEqual(skipped, []string{"https://big.example"}) {
		t.Fatalf("oversize origin: skipped = %v, err = %v; want the big origin skipped and no error", skipped, err)
	}
	for _, c := range seedAddCalls(stub)[before:] {
		src := paramOf(c, "source")
		if len(src) > maxLoginScriptBytes || strings.Contains(src, "big.example") {
			t.Fatalf("a script for the skipped origin, or over the limit (%d bytes), was sent", len(src))
		}
	}

	// nil clears the seed: the old scripts are removed and nothing is added.
	removedBefore := len(stub.CallsFor("Page.removeScriptToEvaluateOnNewDocument"))
	addedBefore := len(seedAddCalls(stub))
	if _, err := m.SetLocalStorageSeed(ctx, nil); err != nil {
		t.Fatalf("SetLocalStorageSeed(nil): %v", err)
	}
	if n := len(stub.CallsFor("Page.removeScriptToEvaluateOnNewDocument")) - removedBefore; n != 4 {
		t.Fatalf("nil seed removed %d scripts, want 4", n)
	}
	if n := len(seedAddCalls(stub)) - addedBefore; n != 0 {
		t.Fatalf("nil seed registered %d scripts, want 0", n)
	}
}

// A report from the isolated world marks its origin applied: the Manager
// removes that origin's script from every tab and never seeds it again, while
// other origins keep theirs.
func TestLoginSeedAppliedOriginStopsSeeding(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := attachTestManager(t, stub)
	ctx := context.Background()

	var counter atomic.Int64
	stub.SetResponder(func(method, _ string, params json.RawMessage) (any, string, bool) {
		if method != "Page.addScriptToEvaluateOnNewDocument" || !strings.Contains(string(params), `"worldName"`) {
			return nil, "", false
		}
		return map[string]string{"identifier": fmt.Sprintf("seed-%d", counter.Add(1))}, "", true
	})
	seed := map[string]map[string]string{
		"https://example.com":   {"k": "v"},
		"https://other.example": {"x": "1"},
	}
	if _, err := m.SetLocalStorageSeed(ctx, seed); err != nil {
		t.Fatalf("SetLocalStorageSeed: %v", err)
	}
	tab1, _, err := m.EnsureHeadless(ctx, "tab:1")
	if err != nil {
		t.Fatalf("EnsureHeadless: %v", err)
	}
	// Origins register in sorted order: example.com is seed-1 on this tab.
	if adds := seedAddCallsOn(stub, tab1.sessionID); len(adds) != 2 {
		t.Fatalf("tab1 got %d origin scripts, want 2", len(adds))
	}
	bound := false
	for _, c := range stub.CallsFor("Runtime.addBinding") {
		if c.SessionID == tab1.sessionID && paramOf(c, "name") == loginSeedBinding && paramOf(c, "executionContextName") == loginSeedWorld {
			bound = true
		}
	}
	if !bound {
		t.Fatal("the isolated-world binding was not installed on the tab")
	}
	for _, c := range seedAddCalls(stub) {
		if strings.Contains(paramOf(c, "source"), "__ocode_ls_restored") {
			t.Fatal("a restore script writes the page-visible sentinel key")
		}
	}

	payload := fmt.Sprintf(`{"origin":"https://example.com","gen":%d}`, loginGenOf(m))
	stub.InjectEvent(tab1.sessionID, "Runtime.bindingCalled", map[string]any{"name": loginSeedBinding, "payload": payload, "executionContextId": 7})
	waitForCall(t, stub, "Page.removeScriptToEvaluateOnNewDocument", func(c stubCall) bool {
		return c.SessionID == tab1.sessionID && paramOf(c, "identifier") == "seed-1"
	})

	tab2, _, err := m.EnsureHeadless(ctx, "tab:2")
	if err != nil {
		t.Fatalf("EnsureHeadless: %v", err)
	}
	adds := seedAddCallsOn(stub, tab2.sessionID)
	if len(adds) != 1 || !seedSessionSawSource(adds, tab2.sessionID, "https://other.example") || seedSessionSawSource(adds, tab2.sessionID, "https://example.com") {
		t.Fatalf("a new tab got %d scripts; want only the origin that is not applied yet", len(adds))
	}
	if got := m.LoginSeedUnapplied(); !reflect.DeepEqual(got, []string{"https://other.example"}) {
		t.Fatalf("LoginSeedUnapplied = %v, want only other.example", got)
	}
}

// A report from an older seed generation, or under a binding name the Manager
// did not install, changes nothing. A valid report sent after them on the same
// session shows the earlier ones were handled first.
func TestLoginSeedStaleOrUnknownReportIsIgnored(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := attachTestManager(t, stub)
	ctx := context.Background()

	var counter atomic.Int64
	stub.SetResponder(func(method, _ string, params json.RawMessage) (any, string, bool) {
		if method != "Page.addScriptToEvaluateOnNewDocument" || !strings.Contains(string(params), `"worldName"`) {
			return nil, "", false
		}
		return map[string]string{"identifier": fmt.Sprintf("seed-%d", counter.Add(1))}, "", true
	})
	seed := map[string]map[string]string{
		"https://example.com":   {"k": "v"},
		"https://other.example": {"x": "1"},
	}
	if _, err := m.SetLocalStorageSeed(ctx, seed); err != nil {
		t.Fatalf("SetLocalStorageSeed: %v", err)
	}
	tab1, _, err := m.EnsureHeadless(ctx, "tab:1")
	if err != nil {
		t.Fatalf("EnsureHeadless: %v", err)
	}
	gen := loginGenOf(m)
	stub.InjectEvent(tab1.sessionID, "Runtime.bindingCalled", map[string]any{"name": loginSeedBinding,
		"payload": fmt.Sprintf(`{"origin":"https://example.com","gen":%d}`, gen+1), "executionContextId": 7})
	stub.InjectEvent(tab1.sessionID, "Runtime.bindingCalled", map[string]any{"name": "someOtherBinding",
		"payload": fmt.Sprintf(`{"origin":"https://example.com","gen":%d}`, gen), "executionContextId": 7})
	stub.InjectEvent(tab1.sessionID, "Runtime.bindingCalled", map[string]any{"name": loginSeedBinding,
		"payload": fmt.Sprintf(`{"origin":"https://other.example","gen":%d}`, gen), "executionContextId": 7})

	waitForCall(t, stub, "Page.removeScriptToEvaluateOnNewDocument", func(c stubCall) bool {
		return c.SessionID == tab1.sessionID && paramOf(c, "identifier") == "seed-2"
	})
	for _, c := range stub.CallsFor("Page.removeScriptToEvaluateOnNewDocument") {
		if paramOf(c, "identifier") == "seed-1" {
			t.Fatal("a stale or unknown report removed example.com's seed")
		}
	}
}

// An origin whose script would exceed the limit is skipped and listed, by
// name only; the other origins still register.
func TestLoginSeedSkipsOversizeOrigin(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := attachTestManager(t, stub)
	answerSeedIdentifiers(stub)
	ctx := context.Background()

	tab, _, err := m.EnsureHeadless(ctx, "tab:1")
	if err != nil {
		t.Fatalf("EnsureHeadless: %v", err)
	}
	seed := map[string]map[string]string{
		"https://big.example": {"blob": strings.Repeat("x", maxLoginScriptBytes)},
		"https://example.com": {"k": "v"},
	}
	skipped, err := m.SetLocalStorageSeed(ctx, seed)
	if err != nil {
		t.Fatalf("SetLocalStorageSeed: %v", err)
	}
	if !reflect.DeepEqual(skipped, []string{"https://big.example"}) {
		t.Fatalf("skipped = %v, want [https://big.example]", skipped)
	}
	adds := seedAddCallsOn(stub, tab.sessionID)
	if len(adds) != 1 || !seedSessionSawSource(adds, tab.sessionID, "https://example.com") {
		t.Fatalf("tab got %d scripts; want the example.com script only", len(adds))
	}
	for _, c := range seedAddCalls(stub) {
		if n := len(paramOf(c, "source")); n > maxLoginScriptBytes {
			t.Fatalf("a script of %d bytes was sent; the limit is %d", n, maxLoginScriptBytes)
		}
	}
}

// Each origin is its own script, so two origins that are each under the limit
// are both registered, even when together they would be over it.
func TestLoginSeedChunksPerOrigin(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := attachTestManager(t, stub)
	answerSeedIdentifiers(stub)
	ctx := context.Background()

	tab, _, err := m.EnsureHeadless(ctx, "tab:1")
	if err != nil {
		t.Fatalf("EnsureHeadless: %v", err)
	}
	half := strings.Repeat("h", 600<<10)
	seed := map[string]map[string]string{
		"https://one.example": {"a": half},
		"https://two.example": {"b": half},
	}
	if _, err := m.SetLocalStorageSeed(ctx, seed); err != nil {
		t.Fatalf("SetLocalStorageSeed: %v", err)
	}
	adds := seedAddCallsOn(stub, tab.sessionID)
	if len(adds) != 2 {
		t.Fatalf("tab got %d scripts, want 2 (one per origin)", len(adds))
	}
	for _, c := range adds {
		if n := len(paramOf(c, "source")); n > maxLoginScriptBytes {
			t.Fatalf("a script of %d bytes was sent; the limit is %d", n, maxLoginScriptBytes)
		}
	}
}

// A failed removal leaves the committed seed in place: a tab created after
// the failure gets the old seed, and a retry once removal works converges
// every tab on the new one.
func TestLoginSeedFailedRemovalKeepsCommittedSeed(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := attachTestManager(t, stub)
	ctx := context.Background()

	var failRemove atomic.Bool
	var ids atomic.Int64
	stub.SetResponder(func(method, _ string, params json.RawMessage) (any, string, bool) {
		switch {
		case method == "Page.removeScriptToEvaluateOnNewDocument" && failRemove.Load():
			return nil, "remove refused", true
		case method == "Page.addScriptToEvaluateOnNewDocument" && strings.Contains(string(params), `"worldName"`):
			return map[string]string{"identifier": fmt.Sprintf("seed-%d", ids.Add(1))}, "", true
		}
		return nil, "", false
	})
	tab0, _, err := m.EnsureHeadless(ctx, "tab:0")
	if err != nil {
		t.Fatalf("EnsureHeadless: %v", err)
	}
	if _, err := m.SetLocalStorageSeed(ctx, map[string]map[string]string{"https://example.com": {"k": "first"}}); err != nil {
		t.Fatalf("first SetLocalStorageSeed: %v", err)
	}

	failRemove.Store(true)
	second := map[string]map[string]string{"https://example.com": {"k": "second"}}
	if _, err := m.SetLocalStorageSeed(ctx, second); err == nil || !strings.Contains(err.Error(), "remove refused") {
		t.Fatalf("SetLocalStorageSeed with a failing removal: err = %v, want the removal error", err)
	}
	tab1, _, err := m.EnsureHeadless(ctx, "tab:1")
	if err != nil {
		t.Fatalf("EnsureHeadless: %v", err)
	}
	adds := seedAddCallsOn(stub, tab1.sessionID)
	if len(adds) != 1 || !seedSessionSawSource(adds, tab1.sessionID, `"first"`) || seedSessionSawSource(adds, tab1.sessionID, `"second"`) {
		t.Fatalf("a tab created after the failed change got %d scripts; want the committed first seed only", len(adds))
	}

	failRemove.Store(false)
	if _, err := m.SetLocalStorageSeed(ctx, second); err != nil {
		t.Fatalf("retry SetLocalStorageSeed: %v", err)
	}
	for _, tab := range []*Target{tab0, tab1} {
		calls := seedAddCallsOn(stub, tab.sessionID)
		if last := calls[len(calls)-1]; !strings.Contains(paramOf(last, "source"), `"second"`) {
			t.Fatalf("tab %s did not converge on the second seed", tab.stateKey)
		}
	}
}

// A registration whose reply never comes fails within loginCallTimeout, and
// the login-state lock is released, so a later tab still opens.
func TestLoginSeedRegistrationIsBounded(t *testing.T) {
	old := loginCallTimeout
	loginCallTimeout = 100 * time.Millisecond
	t.Cleanup(func() { loginCallTimeout = old })

	stub := newStubChrome()
	defer stub.Close()
	m := attachTestManager(t, stub)
	answerSeedIdentifiers(stub)
	ctx := context.Background()

	if _, err := m.SetLocalStorageSeed(ctx, map[string]map[string]string{"https://example.com": {"k": "v"}}); err != nil {
		t.Fatalf("SetLocalStorageSeed: %v", err)
	}
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	var hang atomic.Bool
	hang.Store(true)
	stub.SetReplyGate(func(method string, params json.RawMessage) <-chan struct{} {
		if hang.Load() && method == "Page.addScriptToEvaluateOnNewDocument" && strings.Contains(string(params), `"worldName"`) {
			return release
		}
		return nil
	})

	open := func() (error, time.Duration) {
		done := make(chan error, 1)
		start := time.Now()
		go func() {
			_, _, err := m.OpenHeadless(ctx)
			done <- err
		}()
		select {
		case err := <-done:
			return err, time.Since(start)
		case <-time.After(5 * time.Second):
			t.Fatal("a hung login-state registration blocked tab creation")
			return nil, 0
		}
	}
	if err, _ := open(); err == nil {
		t.Fatal("a hung registration did not fail the tab")
	}
	hang.Store(false)
	if err, _ := open(); err != nil {
		t.Fatalf("a tab after the hung one: %v", err)
	}
}

// A tab that cannot take the login-state seed is closed, never opened without
// it, and a failed seed change leaves the existing tabs in place.
func TestLoginSeedFailureClosesNewTab(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := attachTestManager(t, stub)
	ctx := context.Background()

	answerSeedIdentifiers(stub)
	existing, _, err := m.EnsureHeadless(ctx, "tab:existing")
	if err != nil {
		t.Fatalf("EnsureHeadless: %v", err)
	}
	seed := map[string]map[string]string{"https://example.com": {"k": "v"}}
	if _, err := m.SetLocalStorageSeed(ctx, seed); err != nil {
		t.Fatalf("SetLocalStorageSeed: %v", err)
	}

	// From here every registration is refused. A seed change that fails to
	// register is an error, and the existing tab is kept.
	stub.SetResponder(func(method, _ string, params json.RawMessage) (any, string, bool) {
		if method == "Page.addScriptToEvaluateOnNewDocument" && strings.Contains(string(params), `"worldName"`) {
			return nil, "seed refused", true
		}
		return nil, "", false
	})
	if _, err := m.SetLocalStorageSeed(ctx, map[string]map[string]string{"https://example.com": {"k": "v2"}}); err == nil || !strings.Contains(err.Error(), "seed refused") {
		t.Fatalf("SetLocalStorageSeed on an existing tab: err = %v, want the registration error", err)
	}
	if _, ok := m.LookupTarget(existing.stateKey); !ok {
		t.Fatal("the existing tab was dropped by a failed seed")
	}

	// The committed seed is still the first one, and a new tab cannot take it:
	// it is closed, never opened without it.
	_, _, err = m.OpenHeadless(ctx)
	if err == nil || !strings.Contains(err.Error(), "seed refused") {
		t.Fatalf("OpenHeadless with a failing seed: err = %v, want the registration error", err)
	}
	if n := len(stub.CallsFor("Target.closeTarget")); n != 1 {
		t.Fatalf("Target.closeTarget sent %d times, want 1 (the abandoned tab)", n)
	}
	if n := len(m.TargetKeys()); n != 1 {
		t.Fatalf("targets = %d, want 1: only the existing tab", n)
	}
}

// buildLoginSources is the one place a seed becomes per-origin scripts. An
// empty origin gets none, an oversize origin is skipped and named, and an
// origin already applied in this browser is not seeded again.
func TestLoginSeedSourceBounds(t *testing.T) {
	if src, skipped, err := buildLoginSources(nil, nil, 1); err != nil || len(src) != 0 || len(skipped) != 0 {
		t.Fatalf("nil seed = %v, %v, %v; want no sources, no skips and no error", src, skipped, err)
	}
	if src, _, err := buildLoginSources(map[string]map[string]string{"https://example.com": {}}, nil, 1); err != nil || len(src) != 0 {
		t.Fatalf("seed with no items = %v, %v; want no sources and no error", src, err)
	}
	huge := map[string]map[string]string{
		"https://big.example":   {"big": strings.Repeat("x", maxLoginScriptBytes)},
		"https://small.example": {"k": "v"},
	}
	src, skipped, err := buildLoginSources(huge, nil, 1)
	if err != nil || !reflect.DeepEqual(skipped, []string{"https://big.example"}) {
		t.Fatalf("oversize origin: skipped = %v, err = %v; want the big origin skipped", skipped, err)
	}
	if _, ok := src["https://big.example"]; ok || len(src) != 1 {
		t.Fatalf("sources = %v; want only the small origin", keysOf(src))
	}
	if _, skipped, _ := buildLoginSources(huge, map[string]bool{"https://small.example": true}, 1); len(skipped) != 1 {
		t.Fatalf("applied origin changed the skip list: %v", skipped)
	}
	if src, _, _ := buildLoginSources(huge, map[string]bool{"https://small.example": true}, 1); len(src) != 0 {
		t.Fatalf("an applied origin was seeded again: %v", keysOf(src))
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// seedEntriesFromSource decodes the seed argument of a restore script built by
// loginSourceFor, and fails the test if the script does not have that shape.
func seedEntriesFromSource(t *testing.T, src string) []loginSeedEntry {
	t.Helper()
	prefix := "(" + loginSeedRestoreFn + ")("
	if !strings.HasPrefix(src, prefix) || !strings.HasSuffix(src, ");") {
		t.Fatalf("restore script is not the expected call shape")
	}
	var entries []loginSeedEntry
	if err := json.Unmarshal([]byte(src[len(prefix):len(src)-2]), &entries); err != nil {
		t.Fatalf("seed argument is not JSON: %v", err)
	}
	return entries
}

// seedSessionSawSource reports whether a registration on sessionID carried a
// script containing want.
func seedSessionSawSource(calls []stubCall, sessionID, want string) bool {
	for _, c := range calls {
		if c.SessionID == sessionID && strings.Contains(paramOf(c, "source"), want) {
			return true
		}
	}
	return false
}

// loginGenOf returns the committed login-state seed generation.
func loginGenOf(m *Manager) uint64 {
	s := &m.login
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gen
}
