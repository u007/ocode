package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/boxsession"
	"github.com/u007/ocode/internal/browse/cdp"
	"github.com/u007/ocode/internal/tool"
	"github.com/u007/ocode/internal/upstashbox"
)

// This file is a live end-to-end test of the browser tool's upstash backend. It
// drives the real browserTool through the real handlerBoxes adapter and a real
// boxsession.Manager against a real Upstash box, then deletes the box. It runs
// only when UPSTASH_BOX_API_KEY is set. The key and every URL (a CDP URL carries
// a token) are scrubbed from anything this test prints.

const (
	liveBoxKeyEnv      = "UPSTASH_BOX_API_KEY"
	liveBoxAccount     = "live"
	liveBoxLabel       = "ocode-livetest"
	liveBoxSession     = "ses_live_e2e"
	liveBoxProjectRoot = "/work/live"
	liveBoxIdle        = 5 * time.Minute
	liveBoxHardCap     = 10 * time.Minute
	liveBoxTotal       = 4 * time.Minute
	liveBoxCleanup     = 30 * time.Second
	// liveBoxBodyText is a sentence in example.com's body. The body no longer repeats
	// the title "Example Domain", so the title is not a usable check.
	liveBoxBodyText = "This domain is for use in documentation examples"
)

var (
	liveScrubURL   = regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://[^\s"'<>]+`)
	liveScrubToken = regexp.MustCompile(`(?i)token=[^\s&"']+`)
)

// liveScrub removes the API key, every URL and every token value from s.
func liveScrub(key, s string) string {
	if key != "" {
		s = strings.ReplaceAll(s, key, "<key>")
	}
	s = liveScrubURL.ReplaceAllString(s, "<url>")
	return liveScrubToken.ReplaceAllString(s, "token=<redacted>")
}

// liveCall runs one browser action under ctx with the JSON args given.
func liveCall(ctx context.Context, b *browserTool, args string) (string, error) {
	return b.ExecuteCtx(ctx, json.RawMessage(args))
}

func TestLiveBrowserToolOnBox(t *testing.T) {
	key := strings.TrimSpace(os.Getenv(liveBoxKeyEnv))
	if key == "" {
		t.Skipf("%s is not set; the live Upstash browser box test is skipped", liveBoxKeyEnv)
	}
	scrub := func(s string) string { return liveScrub(key, s) }

	// Isolate HOME so NewHandler reads no real user config.
	boxTestHome(t)

	ctx, cancel := context.WithTimeout(context.Background(), liveBoxTotal)
	defer cancel()

	mgr := boxsession.New(boxsession.Deps{
		Account: func() (boxsession.Account, error) {
			return boxsession.Account{
				ID:      liveBoxAccount,
				APIKey:  key,
				Size:    upstashbox.SizeSmall,
				Idle:    liveBoxIdle,
				HardCap: liveBoxHardCap,
			}, nil
		},
		APIKeyFor: func(string) (string, error) { return key, nil },
		NewAPI:    upstashBoxSDKClient,
		Dial:      cdp.DialWebSocket,
		Busy:      func(string) bool { return false },
		Journal:   boxsession.NewJournal(filepath.Join(t.TempDir(), "boxes.json")),
		Label:     liveBoxLabel,
	})
	// Registered before any box is started, so the box is deleted on every path,
	// including a failed step. The teardown has its own context.
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), liveBoxCleanup)
		defer ccancel()
		mgr.Shutdown(cctx)
		if n := len(mgr.Statuses()); n != 0 {
			t.Errorf("cleanup: %d box(es) still running after Shutdown", n)
		}
	})

	h := NewHandler()
	h.boxes = mgr
	b := newBrowserTool(liveBoxProjectRoot, 1568, nil, nil).withBoxes(handlerBoxes{h: h})
	sctx := tool.WithTodoScope(ctx, tool.TodoScope{ProjectRoot: liveBoxProjectRoot, SessionID: liveBoxSession})

	// Step 1: navigate before start is refused with the call-start error, and no box exists.
	start := time.Now()
	_, err := liveCall(sctx, b, `{"action":"navigate","backend":"upstash","url":"https://example.com"}`)
	if err == nil {
		t.Fatal("step 1 navigate before start: no error")
	}
	if !strings.Contains(err.Error(), "action=start") {
		t.Fatalf("step 1 navigate before start: error %q does not tell the model to call start", scrub(err.Error()))
	}
	if n := len(mgr.Statuses()); n != 0 {
		t.Fatalf("step 1: %d box(es) exist after a refused navigate, want 0", n)
	}
	t.Logf("step 1 ok in %s: navigate before start refused with the call-start error; no box", time.Since(start).Round(time.Millisecond))

	// Step 2: start creates the box and reports its expiry, without leaking a URL or token.
	start = time.Now()
	out, err := liveCall(sctx, b, `{"action":"start","backend":"upstash"}`)
	if err != nil {
		t.Fatalf("step 2 start: %s", scrub(err.Error()))
	}
	if !strings.Contains(strings.ToLower(out), "expires") {
		t.Fatalf("step 2 start: result %q does not mention an expiry", scrub(out))
	}
	if strings.Contains(out, "wss://") || strings.Contains(out, "token") {
		t.Fatalf("step 2 start: result leaks a CDP URL or token: %q", scrub(out))
	}
	statuses := mgr.Statuses()
	if len(statuses) != 1 {
		t.Fatalf("step 2 start: %d box(es) running, want 1", len(statuses))
	}
	boxID := statuses[0].BoxID
	if boxID == "" {
		t.Fatal("step 2 start: the running box has no id")
	}
	if !strings.Contains(out, boxID) {
		t.Fatalf("step 2 start: result %q does not name box %s", scrub(out), boxID)
	}
	t.Logf("step 2 start ok in %s: box %s, result %q", time.Since(start).Round(time.Millisecond), boxID, scrub(out))

	// Step 3: a second start reports the running box and creates no other.
	start = time.Now()
	out, err = liveCall(sctx, b, `{"action":"start","backend":"upstash"}`)
	if err != nil {
		t.Fatalf("step 3 second start: %s", scrub(err.Error()))
	}
	if !strings.Contains(out, "already running") {
		t.Fatalf("step 3 second start: result %q does not report the box already running", scrub(out))
	}
	statuses = mgr.Statuses()
	if len(statuses) != 1 || statuses[0].BoxID != boxID {
		t.Fatalf("step 3 second start: statuses = %d box(es), want exactly box %s", len(statuses), boxID)
	}
	t.Logf("step 3 second start ok in %s: already running, still one box %s", time.Since(start).Round(time.Millisecond), boxID)

	// Step 4: open a tab on example.com, then read its text.
	start = time.Now()
	out, err = liveCall(sctx, b, `{"action":"open","backend":"upstash","url":"https://example.com"}`)
	if err != nil {
		t.Fatalf("step 4 open: %s", scrub(err.Error()))
	}
	t.Logf("step 4 open ok in %s: %q", time.Since(start).Round(time.Millisecond), scrub(out))
	start = time.Now()
	out, err = liveCall(sctx, b, `{"action":"text","backend":"upstash"}`)
	if err != nil {
		t.Fatalf("step 4 text: %s", scrub(err.Error()))
	}
	if !strings.Contains(out, liveBoxBodyText) {
		t.Fatalf("step 4 text: page text does not contain %q; got %d chars: %q", liveBoxBodyText, len(out), scrub(out))
	}
	t.Logf("step 4 text ok in %s: page text contains %q", time.Since(start).Round(time.Millisecond), liveBoxBodyText)

	// Step 5: navigation to the box's own network is refused before any tab is attached.
	start = time.Now()
	for _, u := range []string{"http://127.0.0.1:9222/json/version", "http://169.254.169.254/"} {
		args := `{"action":"navigate","backend":"upstash","url":"` + u + `"}`
		out, err := liveCall(sctx, b, args)
		if err == nil {
			t.Fatalf("step 5 navigate to %s: no refusal (result %q)", u, scrub(out))
		}
		t.Logf("step 5 navigate to %s refused: %s", u, scrub(err.Error()))
	}
	t.Logf("step 5 ok in %s: both internal addresses refused", time.Since(start).Round(time.Millisecond))

	// Step 6: snapshot is non-empty, and tabs lists the tab step 4 opened.
	opened := b.openedIn(browserBackendUpstash)
	if len(opened) != 1 {
		t.Fatalf("step 6: the agent's opened upstash tabs = %v, want exactly one", opened)
	}
	start = time.Now()
	out, err = liveCall(sctx, b, `{"action":"snapshot","backend":"upstash"}`)
	if err != nil {
		t.Fatalf("step 6 snapshot: %s", scrub(err.Error()))
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("step 6 snapshot: empty result")
	}
	t.Logf("step 6 snapshot ok in %s: %d chars", time.Since(start).Round(time.Millisecond), len(out))
	out, err = liveCall(sctx, b, `{"action":"tabs","backend":"upstash"}`)
	if err != nil {
		t.Fatalf("step 6 tabs: %s", scrub(err.Error()))
	}
	if !strings.Contains(out, opened[0]) {
		t.Fatalf("step 6 tabs: result %q does not list tab %s", scrub(out), opened[0])
	}
	t.Logf("step 6 tabs ok: lists %s", opened[0])

	// Step 7: an upload on the box is refused with the uploads message, before any file is read.
	start = time.Now()
	_, err = liveCall(sctx, b, `{"action":"upload","backend":"upstash","target":"@e1","files":["/work/live/upload.txt"]}`)
	if err == nil || !errors.Is(err, errBrowserBoxUploads) {
		t.Fatalf("step 7 upload: err = %s, want the uploads refusal", scrub(fmt.Sprint(err)))
	}
	t.Logf("step 7 ok in %s: upload refused with the uploads message", time.Since(start).Round(time.Millisecond))

	// Step 8: terminate deletes the box; the next call reports the call-start error.
	start = time.Now()
	out, err = liveCall(sctx, b, `{"action":"terminate","backend":"upstash"}`)
	if err != nil {
		t.Fatalf("step 8 terminate: %s", scrub(err.Error()))
	}
	if n := len(mgr.Statuses()); n != 0 {
		t.Fatalf("step 8 terminate: %d box(es) still running, want 0", n)
	}
	t.Logf("step 8 terminate ok in %s: %q", time.Since(start).Round(time.Millisecond), scrub(out))
	_, err = liveCall(sctx, b, `{"action":"text","backend":"upstash"}`)
	if err == nil {
		t.Fatal("step 8 text after terminate: no error")
	}
	if !strings.Contains(err.Error(), "action=start") {
		t.Fatalf("step 8 text after terminate: error %q does not tell the model to call start", scrub(err.Error()))
	}
	t.Logf("step 8 text after terminate refused with the call-start error")

	// Step 9: a sub-agent may not start a box. The refusal comes before any box call.
	sub, ok := b.ForAgent("general").(*browserTool)
	if !ok {
		t.Fatal("step 9: ForAgent did not return a *browserTool")
	}
	startArgs := json.RawMessage(`{"action":"start","backend":"upstash"}`)
	if r := sub.Refusal(startArgs); r == "" {
		t.Fatal("step 9: a sub-agent start has an empty refusal")
	} else {
		t.Logf("step 9 sub-agent start refusal: %s", scrub(r))
	}
	if _, err := sub.ExecuteCtx(sctx, startArgs); err == nil {
		t.Fatal("step 9: a sub-agent start was not refused")
	}
	if n := len(mgr.Statuses()); n != 0 {
		t.Fatalf("step 9: %d box(es) exist after a refused sub-agent start, want 0", n)
	}
	t.Logf("step 9 ok: sub-agent start refused, no box created")
}

const (
	liveLoginName     = "livetest"
	liveLoginSessionA = "ses_live_login_a"
	liveLoginSessionB = "ses_live_login_b"
	liveLoginOrigin   = "https://example.com"
	// liveLoginCookie and liveLoginKey name what the test plants. Only the names are
	// asserted or printed; the values stay inside the page and the saved file.
	liveLoginCookie = "ocode_login_live"
	liveLoginKey    = "ocode_login_live_ls"
	liveLoginValue  = "login-state-live-value"
)

// TestLiveBrowserToolLoginState drives a login state through a real Upstash box:
// a cookie and a localStorage item set in one box are saved on terminate, restored
// into a second box, and a second session cannot start the same state while the
// first holds it.
func TestLiveBrowserToolLoginState(t *testing.T) {
	key := strings.TrimSpace(os.Getenv(liveBoxKeyEnv))
	if key == "" {
		t.Skipf("%s is not set; the live Upstash login-state test is skipped", liveBoxKeyEnv)
	}
	scrub := func(s string) string { return liveScrub(key, s) }

	boxTestHome(t)
	ctx, cancel := context.WithTimeout(context.Background(), liveBoxTotal)
	defer cancel()

	journalPath := filepath.Join(t.TempDir(), "boxes.json")
	store := boxsession.NewLoginStateStore(filepath.Join(t.TempDir(), "login-states"))
	lookup := func(string) (string, error) { return liveLoginName, nil }
	deps := func() boxsession.Deps {
		return boxsession.Deps{
			Account: func() (boxsession.Account, error) {
				return boxsession.Account{
					ID:      liveBoxAccount,
					APIKey:  key,
					Size:    upstashbox.SizeSmall,
					Idle:    liveBoxIdle,
					HardCap: liveBoxHardCap,
				}, nil
			},
			APIKeyFor:     func(string) (string, error) { return key, nil },
			NewAPI:        upstashBoxSDKClient,
			Dial:          cdp.DialWebSocket,
			Busy:          func(string) bool { return false },
			Journal:       boxsession.NewJournal(journalPath),
			Label:         liveBoxLabel,
			LoginStates:   store,
			LoginStateFor: lookup,
		}
	}
	mgr := boxsession.New(deps())
	other := boxsession.New(deps())
	// Both managers are shut down on every path. Shutdown deletes whatever each still
	// owns, so a failed step does not leave a billing box behind.
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), liveBoxCleanup)
		defer ccancel()
		other.Shutdown(cctx)
		mgr.Shutdown(cctx)
		if n := len(mgr.Statuses()) + len(other.Statuses()); n != 0 {
			t.Errorf("cleanup: %d box(es) still running after Shutdown", n)
		}
	})

	h := NewHandler()
	h.boxes = mgr
	b := newBrowserTool(liveBoxProjectRoot, 1568, nil, nil).withBoxes(handlerBoxes{h: h})
	sctx := tool.WithTodoScope(ctx, tool.TodoScope{ProjectRoot: liveBoxProjectRoot, SessionID: liveLoginSessionA})

	// evalOnOpenedTab runs one expression in the page of the agent's upstash tab.
	evalOnOpenedTab := func(expr string) string {
		t.Helper()
		opened := b.openedIn(browserBackendUpstash)
		if len(opened) == 0 {
			t.Fatal("no upstash tab is open")
		}
		cm, err := mgr.Browser(liveLoginSessionA)
		if err != nil {
			t.Fatalf("Browser: %v", err)
		}
		// openedIn holds tab ids; the cdp state key is "tab:" + id.
		tgt, ok := cm.LookupTarget("tab:" + opened[len(opened)-1])
		if !ok {
			t.Fatal("the opened tab has no attached target")
		}
		raw, err := tgt.EvaluateValue(ctx, expr)
		if err != nil {
			t.Fatalf("evaluate on the tab: %s", scrub(err.Error()))
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatalf("evaluate result is not a string: %s", scrub(string(raw)))
		}
		return s
	}

	// Step 1: start, open example.com, and plant a cookie and a localStorage item.
	start := time.Now()
	out, err := liveCall(sctx, b, `{"action":"start","backend":"upstash"}`)
	if err != nil {
		t.Fatalf("step 1 start: %s", scrub(err.Error()))
	}
	if !strings.Contains(out, `Login state "`+liveLoginName+`" is restored`) {
		t.Fatalf("step 1 start: a new box with a login state does not say so: %q", scrub(out))
	}
	if _, err := liveCall(sctx, b, `{"action":"open","backend":"upstash","url":"`+liveLoginOrigin+`"}`); err != nil {
		t.Fatalf("step 1 open: %s", scrub(err.Error()))
	}
	plant := fmt.Sprintf(`(() => { document.cookie = %q + "=" + %q + "; max-age=3600; path=/"; localStorage.setItem(%q, %q); return "planted"; })()`,
		liveLoginCookie, liveLoginValue, liveLoginKey, liveLoginValue)
	if got := evalOnOpenedTab(plant); got != "planted" {
		t.Fatalf("step 1 plant: page returned %q, want planted", got)
	}
	t.Logf("step 1 ok in %s: box started, example.com open, cookie and localStorage item planted", time.Since(start).Round(time.Millisecond))

	// Step 2: terminate saves the state. The file holds the cookie and the origin, by name and count.
	start = time.Now()
	if _, err := liveCall(sctx, b, `{"action":"terminate","backend":"upstash"}`); err != nil {
		t.Fatalf("step 2 terminate: %s", scrub(err.Error()))
	}
	saved, found, err := store.Load(liveLoginName)
	if err != nil || !found {
		t.Fatalf("step 2: the login state file = found %v, err %v; want it saved on terminate", found, err)
	}
	names := make([]string, 0, len(saved.Cookies))
	for _, raw := range saved.Cookies {
		var c struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatalf("step 2: a saved cookie is not JSON")
		}
		names = append(names, c.Name)
	}
	if len(saved.Cookies) < 1 || !slices.Contains(names, liveLoginCookie) {
		t.Fatalf("step 2: saved cookies = %d (names %v), want the planted cookie", len(saved.Cookies), names)
	}
	items, ok := saved.LocalStorage[liveLoginOrigin]
	if !ok {
		t.Fatalf("step 2: saved origins = %d, want %s", len(saved.LocalStorage), liveLoginOrigin)
	}
	if _, ok := items[liveLoginKey]; !ok {
		t.Fatalf("step 2: the saved %s origin has %d item(s) without the planted key", liveLoginOrigin, len(items))
	}
	t.Logf("step 2 ok in %s: terminate saved %d cookie(s) and %d origin(s); planted names present", time.Since(start).Round(time.Millisecond), len(saved.Cookies), len(saved.LocalStorage))

	// Step 3: a new box restores the state. The cookie is set, and the localStorage item
	// is restored on the first visit to the origin.
	start = time.Now()
	out, err = liveCall(sctx, b, `{"action":"start","backend":"upstash"}`)
	if err != nil {
		t.Fatalf("step 3 start: %s", scrub(err.Error()))
	}
	if !strings.Contains(out, `Login state "`+liveLoginName+`" is restored`) {
		t.Fatalf("step 3 start: result does not mention the login state: %q", scrub(out))
	}
	if _, err := liveCall(sctx, b, `{"action":"open","backend":"upstash","url":"`+liveLoginOrigin+`"}`); err != nil {
		t.Fatalf("step 3 open: %s", scrub(err.Error()))
	}
	cookieStr := evalOnOpenedTab(`document.cookie`)
	if !strings.Contains(cookieStr, liveLoginCookie+"=") {
		t.Fatalf("step 3: document.cookie does not carry the restored cookie name %q", liveLoginCookie)
	}
	if got := evalOnOpenedTab(fmt.Sprintf(`localStorage.getItem(%q) || ""`, liveLoginKey)); got != liveLoginValue {
		t.Fatalf("step 3: the restored localStorage item does not match what was saved")
	}
	t.Logf("step 3 ok in %s: new box restored the cookie and the localStorage item", time.Since(start).Round(time.Millisecond))

	// Step 4: a second manager on the same store refuses the state while the first holds it.
	start = time.Now()
	_, err = other.Start(ctx, liveLoginSessionB, boxsession.StartOptions{})
	inUse, ok := err.(boxsession.ErrLoginStateInUse)
	if !ok {
		t.Fatalf("step 4: second start = %v, want ErrLoginStateInUse", err)
	}
	if inUse.SessionID != liveLoginSessionA {
		t.Fatalf("step 4: the refusal names session %q, want %q", inUse.SessionID, liveLoginSessionA)
	}
	if n := len(other.Statuses()); n != 0 {
		t.Fatalf("step 4: the refused start left %d box(es) on the second manager", n)
	}
	entries, err := boxsession.NewJournal(journalPath).List()
	if err != nil || len(entries) != 1 {
		t.Fatalf("step 4: journal rows = %d (err %v), want the first box only", len(entries), err)
	}
	t.Logf("step 4 ok in %s: second session refused, naming %s; no second box", time.Since(start).Round(time.Millisecond), inUse.SessionID)

	// Step 5: terminate releases the state and leaves no box running.
	start = time.Now()
	if _, err := liveCall(sctx, b, `{"action":"terminate","backend":"upstash"}`); err != nil {
		t.Fatalf("step 5 terminate: %s", scrub(err.Error()))
	}
	if n := len(mgr.Statuses()); n != 0 {
		t.Fatalf("step 5: %d box(es) still running, want 0", n)
	}
	if sid, held, err := store.InUseBy(liveLoginName); err != nil || held {
		t.Fatalf("step 5: login state still held by %q (err %v), want released", sid, err)
	}
	t.Logf("step 5 ok in %s: terminated, no box running, login state released", time.Since(start).Round(time.Millisecond))
}

const (
	liveRetainSession = "ses_live_retain"
	liveRetainOrigin  = "https://example.com/"
	// liveRetainCookie is the cookie the page sets. Only its name is asserted or logged.
	liveRetainCookie = "retain_probe"
	// liveRetainTotal bounds the whole retained test: two pauses and resumes need more
	// time than the ephemeral tests' liveBoxTotal.
	liveRetainTotal = 8 * time.Minute
	// liveRetainStatusWait bounds the wait for a box to report paused after terminate.
	liveRetainStatusWait = 90 * time.Second
)

// waitBoxStatus polls the box's status until it is want, or the wait runs out. A pause
// can report a transitional status for a moment, so the first read is not final.
func waitBoxStatus(ctx context.Context, client *upstashbox.Client, id, want string, wait time.Duration) (string, error) {
	deadline := time.Now().Add(wait)
	for {
		box, err := client.Get(ctx, id)
		if err != nil {
			return "", err
		}
		if box.Status == want || time.Now().After(deadline) {
			return box.Status, nil
		}
		select {
		case <-ctx.Done():
			return box.Status, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// TestLiveBrowserToolRetainedBox drives a retained box through a real Upstash box.
// A start with retain creates the box. Terminate pauses it and keeps it, so the box
// still exists and reports paused. A start with retain resumes that same box, and the
// cookie the page set before the pause is still there. Every box and journal row the
// test made is deleted in cleanup, whatever step failed.
func TestLiveBrowserToolRetainedBox(t *testing.T) {
	key := strings.TrimSpace(os.Getenv(liveBoxKeyEnv))
	if key == "" {
		t.Skipf("%s is not set; the live Upstash retained-box test is skipped", liveBoxKeyEnv)
	}
	scrub := func(s string) string { return liveScrub(key, s) }

	boxTestHome(t)
	ctx, cancel := context.WithTimeout(context.Background(), liveRetainTotal)
	defer cancel()

	// A login-state name unique to this run, so a leftover box from an earlier run is never reused.
	loginName := fmt.Sprintf("retain-%d", time.Now().UnixNano())
	journalPath := filepath.Join(t.TempDir(), "boxes.json")
	journal := boxsession.NewJournal(journalPath)
	store := boxsession.NewLoginStateStore(filepath.Join(t.TempDir(), "login-states"))
	lookup := func(string) (string, error) { return loginName, nil }
	mgr := boxsession.New(boxsession.Deps{
		Account: func() (boxsession.Account, error) {
			return boxsession.Account{
				ID:      liveBoxAccount,
				APIKey:  key,
				Size:    upstashbox.SizeSmall,
				Idle:    liveBoxIdle,
				HardCap: liveBoxHardCap,
				Retain:  24 * time.Hour,
			}, nil
		},
		APIKeyFor:     func(string) (string, error) { return key, nil },
		NewAPI:        upstashBoxSDKClient,
		Dial:          cdp.DialWebSocket,
		Busy:          func(string) bool { return false },
		Journal:       journal,
		Label:         liveBoxLabel,
		LoginStates:   store,
		LoginStateFor: lookup,
	})

	// Cleanup runs last-in first-out. The delete is registered first, so it runs after
	// the manager's Shutdown, which pauses a retained box and keeps it. It walks the
	// journal rather than one remembered id, so a start that created a box and then
	// failed is deleted too.
	t.Cleanup(func() {
		rows, err := journal.List()
		if err != nil {
			t.Errorf("cleanup: read the journal: %s", scrub(err.Error()))
			return
		}
		if len(rows) == 0 {
			return
		}
		client, err := upstashbox.New(key)
		if err != nil {
			t.Errorf("cleanup: build the Upstash client: %s", scrub(err.Error()))
			return
		}
		cctx, ccancel := context.WithTimeout(context.Background(), liveBoxCleanup)
		defer ccancel()
		for _, row := range rows {
			if err := client.Delete(cctx, row.BoxID); err != nil {
				t.Errorf("cleanup: delete box %s: %s", row.BoxID, scrub(err.Error()))
				continue
			}
			if err := journal.Remove(row.BoxID); err != nil {
				t.Errorf("cleanup: remove journal row for box %s: %s", row.BoxID, scrub(err.Error()))
			}
		}
	})
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), liveBoxCleanup)
		defer ccancel()
		mgr.Shutdown(cctx)
	})

	h := NewHandler()
	h.boxes = mgr
	b := newBrowserTool(liveBoxProjectRoot, 1568, nil, nil).withBoxes(handlerBoxes{h: h})
	sctx := tool.WithTodoScope(ctx, tool.TodoScope{ProjectRoot: liveBoxProjectRoot, SessionID: liveRetainSession})

	// evalOnOpenedTab runs one expression in the page of the agent's upstash tab, as
	// TestLiveBrowserToolLoginState does: the tool has no eval action.
	evalOnOpenedTab := func(expr string) string {
		t.Helper()
		opened := b.openedIn(browserBackendUpstash)
		if len(opened) == 0 {
			t.Fatal("no upstash tab is open")
		}
		cm, err := mgr.Browser(liveRetainSession)
		if err != nil {
			t.Fatalf("Browser: %s", scrub(err.Error()))
		}
		tgt, ok := cm.LookupTarget("tab:" + opened[len(opened)-1])
		if !ok {
			t.Fatal("the opened tab has no attached target")
		}
		raw, err := tgt.EvaluateValue(ctx, expr)
		if err != nil {
			t.Fatalf("evaluate on the tab: %s", scrub(err.Error()))
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatalf("evaluate result is not a string: %s", scrub(string(raw)))
		}
		return s
	}

	// liveBoxID returns the id and kind of the session's live box, or empty values.
	liveBoxID := func() (string, string) {
		for _, st := range mgr.Statuses() {
			if st.SessionID == liveRetainSession {
				return st.BoxID, st.Kind
			}
		}
		return "", ""
	}

	// Step 1: start with retain creates a retained box, open the page, and set a cookie.
	start := time.Now()
	out, err := liveCall(sctx, b, `{"action":"start","backend":"upstash","retain":true}`)
	if err != nil {
		t.Fatalf("step 1 start: %s", scrub(err.Error()))
	}
	if !strings.Contains(out, "created retained box") || !strings.Contains(out, `for login state "`+loginName+`"`) {
		t.Fatalf("step 1 start: result does not say a retained box was created: %q", scrub(out))
	}
	boxID, kind := liveBoxID()
	if boxID == "" || kind != boxsession.KindRetained {
		t.Fatalf("step 1 start: live box = %q kind %q, want a retained box", boxID, kind)
	}
	if _, err := liveCall(sctx, b, `{"action":"open","backend":"upstash","url":"`+liveRetainOrigin+`"}`); err != nil {
		t.Fatalf("step 1 open: %s", scrub(err.Error()))
	}
	plant := fmt.Sprintf(`(() => { document.cookie = %q + "=1; max-age=86400; path=/"; return "planted"; })()`, liveRetainCookie)
	if got := evalOnOpenedTab(plant); got != "planted" {
		t.Fatalf("step 1 plant: page returned %q, want planted", got)
	}
	t.Logf("step 1 ok in %s: retained box created, page open, cookie planted", time.Since(start).Round(time.Millisecond))

	// Step 2: terminate pauses the box. The box still exists, reports paused, and the
	// journal row is kept and marked paused.
	start = time.Now()
	out, err = liveCall(sctx, b, `{"action":"terminate","backend":"upstash"}`)
	if err != nil {
		t.Fatalf("step 2 terminate: %s", scrub(err.Error()))
	}
	if !strings.Contains(out, "paused the retained browser box") {
		t.Fatalf("step 2 terminate: result does not say the box was paused: %q", scrub(out))
	}
	if n := len(mgr.Statuses()); n != 0 {
		t.Fatalf("step 2: %d box(es) still live after terminate, want 0", n)
	}
	client, err := upstashbox.New(key)
	if err != nil {
		t.Fatalf("step 2: build the Upstash client: %s", scrub(err.Error()))
	}
	status, err := waitBoxStatus(ctx, client, boxID, upstashbox.StatusPaused, liveRetainStatusWait)
	if err != nil {
		t.Fatalf("step 2: read box %s after terminate: %s (the box must still exist)", boxID, scrub(err.Error()))
	}
	if status != upstashbox.StatusPaused {
		t.Fatalf("step 2: box %s status = %q after terminate, want %q (it must be paused, not deleted)", boxID, status, upstashbox.StatusPaused)
	}
	rows, err := journal.List()
	if err != nil || len(rows) != 1 || rows[0].BoxID != boxID || !rows[0].Paused {
		t.Fatalf("step 2: journal = %+v, %v; want the retained row kept and marked paused", rows, err)
	}
	t.Logf("step 2 ok in %s: terminate paused box, still present, row kept", time.Since(start).Round(time.Millisecond))

	// Step 3: start with retain resumes the same box, and the cookie is still set.
	start = time.Now()
	out, err = liveCall(sctx, b, `{"action":"start","backend":"upstash","retain":true}`)
	if err != nil {
		t.Fatalf("step 3 start: %s", scrub(err.Error()))
	}
	if !strings.Contains(out, "resumed retained box "+boxID) {
		t.Fatalf("step 3 start: result does not say box %s was resumed: %q", boxID, scrub(out))
	}
	if resumedID, kind := liveBoxID(); resumedID != boxID || kind != boxsession.KindRetained {
		t.Fatalf("step 3 start: live box = %q kind %q, want the same retained box %s", resumedID, kind, boxID)
	}
	if _, err := liveCall(sctx, b, `{"action":"open","backend":"upstash","url":"`+liveRetainOrigin+`"}`); err != nil {
		t.Fatalf("step 3 open: %s", scrub(err.Error()))
	}
	if cookies := evalOnOpenedTab(`document.cookie`); !strings.Contains(cookies, liveRetainCookie+"=1") {
		t.Fatalf("step 3: document.cookie does not carry the cookie %q set before the pause", liveRetainCookie)
	}
	t.Logf("step 3 ok in %s: start resumed the same box and the cookie survived the pause", time.Since(start).Round(time.Millisecond))
}
