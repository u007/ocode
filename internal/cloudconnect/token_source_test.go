package cloudconnect

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/encryption"
)

// countingRefresher is a mock hub refresher that blocks the first call until
// released, so concurrent Token() calls must overlap and exercise single-flight.
type countingRefresher struct {
	mu      sync.Mutex
	calls   int
	started chan struct{}
	release chan struct{}
}

func (r *countingRefresher) RefreshToken(_ context.Context, _ string) (string, time.Time, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	select {
	case r.started <- struct{}{}:
	default:
	}
	<-r.release
	return "fresh-access", time.Now().Add(time.Hour), nil
}

func (r *countingRefresher) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func seedHubV2(t *testing.T, root, provider, connectionID string) {
	t.Helper()
	f := &config.StorageConnectorsFileV2{
		Version: "v2",
		Central: config.CentralConnector{
			ConnectorID: "kakiit",
			Mode:        "hub",
			Connections: map[string]config.ConnectionMeta{
				provider: {Provider: provider, ConnectionID: connectionID},
			},
		},
	}
	if err := config.SaveV2(root, f); err != nil {
		t.Fatal(err)
	}
}

// TestTokenSourceSingleFlight pins that ten concurrent hub Token() calls for
// one connection coalesce into a single RefreshToken round-trip.
func TestTokenSourceSingleFlight(t *testing.T) {
	root := t.TempDir()
	seedHubV2(t, root, "dropbox", "conn-1")

	r := &countingRefresher{started: make(chan struct{}, 1), release: make(chan struct{})}
	ts := newHubTokenSource(r, root)

	const n = 10
	var wg sync.WaitGroup
	results := make(chan string, n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tok, _, err := ts.Token(context.Background(), "dropbox")
			if err != nil {
				errs <- err
				return
			}
			results <- tok
		}()
	}

	select {
	case <-r.started:
	case <-time.After(3 * time.Second):
		t.Fatal("RefreshToken was never called")
	}
	// Give the other goroutines time to join the in-flight refresh. The flight
	// stays in the map until release is closed, so later arrivals still join.
	time.Sleep(50 * time.Millisecond)
	close(r.release)

	wg.Wait()
	close(errs)
	close(results)
	for err := range errs {
		t.Fatalf("Token: %v", err)
	}
	for tok := range results {
		if tok != "fresh-access" {
			t.Fatalf("token = %q, want fresh-access", tok)
		}
	}
	if got := r.count(); got != 1 {
		t.Fatalf("RefreshToken called %d times, want 1 (single-flight)", got)
	}
}

// TestLegacyTokenSourceReadsEnvelope pins that local mode reads the access
// token and expiry straight from the v2 local credentials envelope, with no
// hub call.
func TestLegacyTokenSourceReadsEnvelope(t *testing.T) {
	root := t.TempDir()
	key := fixedKey()
	exp := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	creds := map[string]Credentials{
		"dropbox": {AccessToken: "legacy-access", RefreshToken: "legacy-refresh", ExpiresAt: exp},
	}
	plain, err := json.Marshal(creds)
	if err != nil {
		t.Fatal(err)
	}
	env, err := encryption.SealWithKey(plain, key)
	if err != nil {
		t.Fatal(err)
	}
	f := &config.StorageConnectorsFileV2{
		Version:             "v2",
		Central:             config.CentralConnector{ConnectorID: "kakiit", Mode: "local", Connections: map[string]config.ConnectionMeta{}},
		CredentialsEnvelope: env,
	}
	if err := config.SaveV2(root, f); err != nil {
		t.Fatal(err)
	}

	ts := NewLegacyTokenSource(root, key)
	tok, gotExp, err := ts.Token(context.Background(), "dropbox")
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok != "legacy-access" {
		t.Fatalf("token = %q, want legacy-access", tok)
	}
	if !gotExp.Equal(exp) {
		t.Fatalf("expiry = %v, want %v", gotExp, exp)
	}
}

// ctxCheckingRefresher fails if the refresh inherited a cancelled context.
type ctxCheckingRefresher struct {
	mu        sync.Mutex
	sawCancel bool
	started   chan struct{}
	release   chan struct{}
}

func (r *ctxCheckingRefresher) RefreshToken(ctx context.Context, _ string) (string, time.Time, error) {
	select {
	case r.started <- struct{}{}:
	default:
	}
	<-r.release
	if ctx.Err() != nil {
		r.mu.Lock()
		r.sawCancel = true
		r.mu.Unlock()
		return "", time.Time{}, ctx.Err()
	}
	return "shared-token", time.Now().Add(time.Hour), nil
}

// TestTokenSourceSingleFlightLeaderCancel pins that cancelling the leader's
// context does not cancel the shared refresh and poison the other waiters.
func TestTokenSourceSingleFlightLeaderCancel(t *testing.T) {
	root := t.TempDir()
	seedHubV2(t, root, "dropbox", "conn-1")

	r := &ctxCheckingRefresher{started: make(chan struct{}, 1), release: make(chan struct{})}
	ts := newHubTokenSource(r, root)

	type result struct {
		tok string
		err error
	}
	leaderOut := make(chan result, 1)
	leaderCtx, cancel := context.WithCancel(context.Background())
	go func() {
		tok, _, err := ts.Token(leaderCtx, "dropbox")
		leaderOut <- result{tok, err}
	}()

	select {
	case <-r.started:
	case <-time.After(3 * time.Second):
		t.Fatal("RefreshToken was never called")
	}
	cancel() // leader goes away mid-refresh

	waiterOut := make(chan result, 1)
	go func() {
		tok, _, err := ts.Token(context.Background(), "dropbox")
		waiterOut <- result{tok, err}
	}()
	time.Sleep(50 * time.Millisecond)
	close(r.release)

	for name, ch := range map[string]chan result{"leader": leaderOut, "waiter": waiterOut} {
		got := <-ch
		if got.err != nil || got.tok != "shared-token" {
			t.Fatalf("%s: got tok=%q err=%v", name, got.tok, got.err)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sawCancel {
		t.Fatal("shared refresh inherited the leader's cancelled context")
	}
}
