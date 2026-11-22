package upstashbox

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// boxWithStatus returns observedBox with its status replaced.
func boxWithStatus(status string) string {
	return strings.Replace(observedBox, `"status":"idle"`, `"status":"`+status+`"`, 1)
}

func TestCreateSendsSnakeCaseBody(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		expectRequest(t, r, http.MethodPost, "/v2/box")
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		want := map[string]any{
			"name":      "ocode-spike-1",
			"size":      "medium",
			"runtime":   "node",
			"browser":   true,
			"ephemeral": true,
			"ttl":       float64(3600),
			"labels":    []any{"ocode-spike"},
			"network_policy": map[string]any{
				"mode": "allow-all",
			},
		}
		for k, v := range want {
			if !jsonEqual(body[k], v) {
				t.Errorf("body[%q] = %v, want %v", k, body[k], v)
			}
		}
		if _, camel := body["networkPolicy"]; camel {
			t.Error("body uses camelCase networkPolicy, want network_policy")
		}
		_, _ = io.WriteString(w, observedBox)
	})
	box, err := c.Create(context.Background(), CreateOptions{
		Name:          "ocode-spike-1",
		Size:          SizeMedium,
		Runtime:       RuntimeNode,
		Browser:       true,
		Ephemeral:     true,
		TTLSeconds:    3600,
		Labels:        []string{"ocode-spike"},
		NetworkPolicy: &NetworkPolicy{Mode: NetworkAllowAll},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if box.ID != "sunny-guppy-35079" || !box.Ephemeral || box.ExpiresAt.IsZero() {
		t.Errorf("returned box = %+v", box)
	}
}

// TestCreateNetworkPolicyBodyShapes checks that a non-custom policy sends only
// its mode, and a custom policy sends its snake_case lists.
func TestCreateNetworkPolicyBodyShapes(t *testing.T) {
	var got map[string]any
	c := newStubClient(t, func(r *http.Request) (*http.Response, error) {
		got = nil
		_ = json.NewDecoder(r.Body).Decode(&got)
		return jsonResponse(r, http.StatusOK, observedBox), nil
	})
	ctx := context.Background()

	if _, err := c.Create(ctx, CreateOptions{NetworkPolicy: &NetworkPolicy{Mode: NetworkDenyAll}}); err != nil {
		t.Fatalf("Create deny-all: %v", err)
	}
	np, _ := got["network_policy"].(map[string]any)
	if len(np) != 1 || np["mode"] != NetworkDenyAll {
		t.Errorf("deny-all network_policy = %v, want only mode", np)
	}

	_, err := c.Create(ctx, CreateOptions{NetworkPolicy: &NetworkPolicy{
		Mode:           NetworkCustom,
		AllowedDomains: []string{"example.com"},
		AllowedCIDRs:   []string{"192.168.0.0/16"},
		DeniedCIDRs:    []string{"10.0.0.0/8"},
	}})
	if err != nil {
		t.Fatalf("Create custom: %v", err)
	}
	np, _ = got["network_policy"].(map[string]any)
	for key, want := range map[string]any{
		"mode":            "custom",
		"allowed_domains": []any{"example.com"},
		"allowed_cidrs":   []any{"192.168.0.0/16"},
		"denied_cidrs":    []any{"10.0.0.0/8"},
	} {
		if !jsonEqual(np[key], want) {
			t.Errorf("custom network_policy[%q] = %v, want %v", key, np[key], want)
		}
	}
}

func TestCreateEmptyOptionsSendsEmptyObject(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if string(raw) != "{}" {
			t.Errorf("body = %s, want {}", raw)
		}
		_, _ = io.WriteString(w, observedBox)
	})
	if _, err := c.Create(context.Background(), CreateOptions{}); err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func TestCreateAcceptsBoundaryValues(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			TTL int `json:"ttl"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.TTL != MaxTTLSeconds {
			t.Errorf("ttl = %d, want %d", body.TTL, MaxTTLSeconds)
		}
		_, _ = io.WriteString(w, observedBox)
	})
	_, err := c.Create(context.Background(), CreateOptions{
		Ephemeral:  true,
		TTLSeconds: MaxTTLSeconds,
		Size:       SizeLarge,
		NetworkPolicy: &NetworkPolicy{
			Mode:           NetworkCustom,
			AllowedDomains: []string{"example.com"},
			DeniedCIDRs:    []string{"10.0.0.0/8"},
		},
	})
	if err != nil {
		t.Fatalf("Create at the boundaries: %v", err)
	}
}

// TestCreateOptionValidationSendsNothing covers each invalid combination. Each
// case must fail before any request leaves the client.
func TestCreateOptionValidationSendsNothing(t *testing.T) {
	c := newServerClient(t, failIfCalled(t))
	cases := map[string]CreateOptions{
		"ttl without ephemeral":        {TTLSeconds: 60},
		"negative ttl without eph":     {TTLSeconds: -1},
		"ttl above maximum":            {Ephemeral: true, TTLSeconds: MaxTTLSeconds + 1},
		"negative ttl with ephemeral":  {Ephemeral: true, TTLSeconds: -5},
		"unknown size":                 {Size: "huge"},
		"unknown runtime":              {Runtime: "perl"},
		"empty label":                  {Labels: []string{""}},
		"label with surrounding space": {Labels: []string{" x "}},
		"unknown network mode":         {NetworkPolicy: &NetworkPolicy{Mode: "block"}},
		"empty network mode":           {NetworkPolicy: &NetworkPolicy{}},
		"allow-all with domains":       {NetworkPolicy: &NetworkPolicy{Mode: NetworkAllowAll, AllowedDomains: []string{"a.com"}}},
		"deny-all with cidr":           {NetworkPolicy: &NetworkPolicy{Mode: NetworkDenyAll, DeniedCIDRs: []string{"10.0.0.0/8"}}},
		"custom with no entries":       {NetworkPolicy: &NetworkPolicy{Mode: NetworkCustom}},
		"invalid cidr":                 {NetworkPolicy: &NetworkPolicy{Mode: NetworkCustom, AllowedCIDRs: []string{"10.0.0.0/33"}}},
		"domain with scheme":           {NetworkPolicy: &NetworkPolicy{Mode: NetworkCustom, AllowedDomains: []string{"https://a.com"}}},
	}
	for name, opts := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := c.Create(context.Background(), opts); err == nil {
				t.Errorf("Create(%s): want validation error", name)
			}
		})
	}
}

func TestCreateRejectsEphemeralResponseWithoutExpiry(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, strings.Replace(observedBox, `"expires_at":1795194922,`, "", 1))
	})
	_, err := c.Create(context.Background(), CreateOptions{Ephemeral: true})
	if err == nil || !strings.Contains(err.Error(), "expires_at") {
		t.Fatalf("Create with no expires_at in response: err = %v", err)
	}
}

// TestCreatePollsUntilReady: the create answer is "creating", and the first
// poll answers "idle", so Create returns the idle box after one GET.
func TestCreatePollsUntilReady(t *testing.T) {
	var gets atomic.Int32
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			expectRequest(t, r, http.MethodPost, "/v2/box")
			_, _ = io.WriteString(w, boxWithStatus("creating"))
		case http.MethodGet:
			gets.Add(1)
			expectRequest(t, r, http.MethodGet, "/v2/box/sunny-guppy-35079")
			_, _ = io.WriteString(w, boxWithStatus("idle"))
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	})
	c.pollInitial = time.Millisecond
	box, err := c.Create(context.Background(), CreateOptions{Ephemeral: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if box.Status != "idle" {
		t.Errorf("Status = %q, want idle", box.Status)
	}
	if n := gets.Load(); n != 1 {
		t.Errorf("GET polls = %d, want 1", n)
	}
}

// TestCreatePollAcceptsCreatingEphemeralWithoutExpiry: while an ephemeral box is
// "creating", expires_at may be absent. The poll accepts that, and the idle
// answer with expires_at is returned.
func TestCreatePollAcceptsCreatingEphemeralWithoutExpiry(t *testing.T) {
	var gets atomic.Int32
	creating := strings.Replace(boxWithStatus("creating"), `"expires_at":1795194922,`, "", 1)
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			_, _ = io.WriteString(w, creating)
		case http.MethodGet:
			gets.Add(1)
			_, _ = io.WriteString(w, boxWithStatus("idle"))
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	})
	c.pollInitial = time.Millisecond
	box, err := c.Create(context.Background(), CreateOptions{Ephemeral: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if box.Status != "idle" || box.ExpiresAt.IsZero() {
		t.Errorf("returned box = %+v, want idle with ExpiresAt set", box)
	}
	if n := gets.Load(); n != 1 {
		t.Errorf("GET polls = %d, want 1", n)
	}
}

// TestCreateCancelledMidPoll cancels the caller's context during the first
// poll, which still answers "creating". Create must stop with context.Canceled.
func TestCreateCancelledMidPoll(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := newStubClient(t, func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			cancel()
		}
		return jsonResponse(r, http.StatusOK, boxWithStatus("creating")), nil
	})
	c.pollInitial = time.Millisecond
	box, err := c.Create(ctx, CreateOptions{Ephemeral: true})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Create cancelled mid-poll: err = %v, want context.Canceled", err)
	}
	if box == nil || box.ID != "sunny-guppy-35079" {
		t.Fatalf("Create cancelled mid-poll: box = %+v, want the created box with its ID", box)
	}
}

// createHandler answers POST with a creating box and runs pollGet for each GET.
func createHandler(t *testing.T, pollGet func(w http.ResponseWriter)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			_, _ = io.WriteString(w, boxWithStatus("creating"))
		case http.MethodGet:
			pollGet(w)
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}
}

// TestCreatePollRetriesServerError: one 500 on a poll is transient. The next
// poll answers idle, and Create succeeds.
func TestCreatePollRetriesServerError(t *testing.T) {
	var gets atomic.Int32
	c := newServerClient(t, createHandler(t, func(w http.ResponseWriter) {
		if gets.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":"try later"}`)
			return
		}
		_, _ = io.WriteString(w, boxWithStatus("idle"))
	}))
	c.pollInitial = time.Millisecond
	box, err := c.Create(context.Background(), CreateOptions{Ephemeral: true})
	if err != nil {
		t.Fatalf("Create after one transient 500: %v", err)
	}
	if box.Status != "idle" || gets.Load() != 2 {
		t.Errorf("box status = %q after %d GETs, want idle after 2", box.Status, gets.Load())
	}
}

// TestCreatePollRetriesTransportError: a round trip that fails once, with no
// response, is retried, and the next poll answers idle.
func TestCreatePollRetriesTransportError(t *testing.T) {
	gets := 0
	c := newStubClient(t, func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost {
			return jsonResponse(r, http.StatusOK, boxWithStatus("creating")), nil
		}
		gets++
		if gets == 1 {
			return nil, errors.New("connection reset by peer")
		}
		return jsonResponse(r, http.StatusOK, boxWithStatus("idle")), nil
	})
	c.pollInitial = time.Millisecond
	box, err := c.Create(context.Background(), CreateOptions{Ephemeral: true})
	if err != nil {
		t.Fatalf("Create after one transport failure: %v", err)
	}
	if box.Status != "idle" || gets != 2 {
		t.Errorf("box status = %q after %d GETs, want idle after 2", box.Status, gets)
	}
}

// TestCreatePollStopsAtClientError: a 4xx poll answer ends the poll at once.
// The box comes back with its ID and an APIError.
func TestCreatePollStopsAtClientError(t *testing.T) {
	var gets atomic.Int32
	c := newServerClient(t, createHandler(t, func(w http.ResponseWriter) {
		gets.Add(1)
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"box not found"}`)
	}))
	c.pollInitial = time.Millisecond
	box, err := c.Create(context.Background(), CreateOptions{Ephemeral: true})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
		t.Fatalf("Create with 404 poll: err = %v, want *APIError 404", err)
	}
	if gets.Load() != 1 {
		t.Errorf("GET polls = %d, want 1 (4xx is not retried)", gets.Load())
	}
	if box == nil || box.ID != "sunny-guppy-35079" {
		t.Errorf("box = %+v, want the created box with its ID", box)
	}
}

// TestCreatePersistentServerErrorReturnsBox: every poll answers 500 until the
// budget ends. Create returns the box with its ID and the last status, plus an error.
func TestCreatePersistentServerErrorReturnsBox(t *testing.T) {
	c := newServerClient(t, createHandler(t, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":"down"}`)
	}))
	c.pollInitial = time.Millisecond
	c.pollMax = time.Millisecond
	c.createWait = 10 * time.Millisecond
	box, err := c.Create(context.Background(), CreateOptions{Ephemeral: true})
	if err == nil {
		t.Fatal("Create with persistent 500 polls: want error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("err = %v, want the last poll's *APIError 500 in the chain", err)
	}
	if box == nil || box.ID != "sunny-guppy-35079" || box.Status != statusCreating {
		t.Errorf("box = %+v, want the created box with ID and creating status", box)
	}
}

// TestCreatePOSTFailureReturnsNilBox: when the POST itself fails, no box exists.
func TestCreatePOSTFailureReturnsNilBox(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":"no capacity"}`)
	})
	box, err := c.Create(context.Background(), CreateOptions{Ephemeral: true})
	if err == nil || box != nil {
		t.Fatalf("Create with failing POST: box = %+v, err = %v; want nil box and an error", box, err)
	}
}

// TestCreatePartialResponseKeepsID: the POST answer has an id but fails
// validation. The box exists, so Create returns it with the ID and an error.
func TestCreatePartialResponseKeepsID(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"partial-1","status":"creating"}`)
	})
	box, err := c.Create(context.Background(), CreateOptions{})
	if err == nil {
		t.Fatal("Create with an incomplete POST answer: want error")
	}
	if box == nil || box.ID != "partial-1" || box.Status != statusCreating {
		t.Errorf("box = %+v, want partial box with ID partial-1", box)
	}
}

// TestCreatePollBudgetExhausted: a box that never leaves "creating" makes
// Create give up once the poll budget is spent, rather than loop forever.
func TestCreatePollBudgetExhausted(t *testing.T) {
	c := newStubClient(t, func(r *http.Request) (*http.Response, error) {
		return jsonResponse(r, http.StatusOK, boxWithStatus("creating")), nil
	})
	c.pollInitial = time.Millisecond
	c.pollMax = time.Millisecond
	c.createWait = 5 * time.Millisecond
	_, err := c.Create(context.Background(), CreateOptions{Ephemeral: true})
	if err == nil || !strings.Contains(err.Error(), "still creating") {
		t.Fatalf("Create that never becomes ready: err = %v, want a still-creating error", err)
	}
}

// jsonEqual compares two values decoded from JSON.
func jsonEqual(got, want any) bool {
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(want)
	return string(a) == string(b)
}

// TestCreateOnCreatedRunsOnceBeforePolling: the id reaches OnCreated before any
// poll, once, and the request body carries nothing from OnCreated.
func TestCreateOnCreatedRunsOnceBeforePolling(t *testing.T) {
	var gets atomic.Int32
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
			}
			if len(body) != 1 || body["ephemeral"] != true {
				t.Errorf("body = %v, want only {ephemeral: true}; OnCreated must not be sent", body)
			}
			_, _ = io.WriteString(w, boxWithStatus("creating"))
		case http.MethodGet:
			gets.Add(1)
			_, _ = io.WriteString(w, boxWithStatus("idle"))
		}
	})
	c.pollInitial = time.Millisecond

	var calls int
	var getsAtCall int32 = -1
	var gotID string
	box, err := c.Create(context.Background(), CreateOptions{
		Ephemeral: true,
		OnCreated: func(id string) error {
			calls++
			gotID = id
			getsAtCall = gets.Load()
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if calls != 1 {
		t.Fatalf("OnCreated ran %d times, want 1", calls)
	}
	if gotID != "sunny-guppy-35079" || box.ID != gotID {
		t.Fatalf("OnCreated got %q, box is %q; want the created id", gotID, box.ID)
	}
	if getsAtCall != 0 {
		t.Fatalf("OnCreated ran after %d polls, want before any poll", getsAtCall)
	}
	if gets.Load() == 0 {
		t.Fatal("Create did not poll a creating box")
	}
}

// TestCreateOnCreatedErrorAbortsWithoutPolling: an OnCreated error stops Create
// before any poll, and the box with its id comes back with the error.
func TestCreateOnCreatedErrorAbortsWithoutPolling(t *testing.T) {
	var gets atomic.Int32
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			gets.Add(1)
		}
		_, _ = io.WriteString(w, boxWithStatus("creating"))
	})
	c.pollInitial = time.Millisecond
	c.pollMax = time.Millisecond
	c.createWait = 50 * time.Millisecond // a mutant that polls must fail fast, not wait the 300 s budget
	sentinel := errors.New("journal is read-only")

	box, err := c.Create(context.Background(), CreateOptions{
		Ephemeral: true,
		OnCreated: func(string) error { return sentinel },
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Create err = %v, want the OnCreated error in the chain", err)
	}
	if box == nil || box.ID != "sunny-guppy-35079" {
		t.Fatalf("box = %+v, want the created box with its id", box)
	}
	if n := gets.Load(); n != 0 {
		t.Fatalf("Create polled %d times after OnCreated failed, want none", n)
	}
}

// TestCreateOnCreatedSkippedWithoutID: a POST answer with no id has no box to
// report, so OnCreated does not run.
func TestCreateOnCreatedSkippedWithoutID(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"status":"creating"}`)
	})
	called := false
	_, err := c.Create(context.Background(), CreateOptions{
		Ephemeral: true,
		OnCreated: func(string) error { called = true; return nil },
	})
	if err == nil {
		t.Fatal("Create with no id in the POST answer: want error")
	}
	if called {
		t.Fatal("OnCreated ran without a box id")
	}
}

// TestCreatePersistentSendsNoEphemeralOrTTLKeys checks that a persistent box (not
// ephemeral, no ttl) sends neither key, not even as false or zero, and that a
// response with no expires_at is accepted as a box that does not expire.
func TestCreatePersistentSendsNoEphemeralOrTTLKeys(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		expectRequest(t, r, http.MethodPost, "/v2/box")
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		for _, key := range []string{"ephemeral", "ttl"} {
			if _, present := body[key]; present {
				t.Errorf("body[%q] is present = %v, want the key absent", key, body[key])
			}
		}
		if body["browser"] != true || body["name"] != "ocode-ls-work" {
			t.Errorf("body = %v, want browser true and the name", body)
		}
		_, _ = io.WriteString(w, `{"id":"box-persistent","name":"ocode-ls-work","status":"idle","browser":true,"ephemeral":false,"created_at":1700000000,"updated_at":1700000000}`)
	})
	box, err := c.Create(context.Background(), CreateOptions{
		Name:      "ocode-ls-work",
		Browser:   true,
		Ephemeral: false,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if box.ID != "box-persistent" || box.Ephemeral || !box.ExpiresAt.IsZero() || box.Status != "idle" {
		t.Errorf("returned box = %+v, want a persistent idle box with no expiry", box)
	}
}
