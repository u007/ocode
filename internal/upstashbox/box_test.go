package upstashbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// observedBox is the create response seen on a real box (from the team's spike).
const observedBox = `{"id":"sunny-guppy-35079","name":"ocode-spike-1","labels":["ocode-spike"],"model":"","runtime":"node","browser":true,"status":"idle","size":"small","storage_gb":10,"ephemeral":true,"expires_at":1795194922,"created_at":1795193122,"updated_at":1795193122}`

// boxJSON builds a minimal valid box object.
func boxJSON(id string, created int64, labels ...string) string {
	raw, _ := json.Marshal(labels)
	return fmt.Sprintf(`{"id":%q,"labels":%s,"status":"idle","size":"small","browser":false,"ephemeral":false,"created_at":%d,"updated_at":%d}`,
		id, raw, created, created)
}

func TestPauseSendsPOSTAndSucceeds(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		expectRequest(t, r, http.MethodPost, "/v2/box/box-1/pause")
		w.WriteHeader(http.StatusOK)
	})
	if err := c.Pause(context.Background(), "box-1"); err != nil {
		t.Fatalf("Pause: %v", err)
	}
}

// TestPauseEphemeralBoxReturnsAPIError is the observed failure: pausing an
// ephemeral box answers 400 with the server's message.
func TestPauseEphemeralBoxReturnsAPIError(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		expectRequest(t, r, http.MethodPost, "/v2/box/sunny-guppy-35079/pause")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"Ephemeral boxes cannot be paused"}`)
	})
	err := c.Pause(context.Background(), "sunny-guppy-35079")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Pause: err = %v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want 400", apiErr.StatusCode)
	}
	if apiErr.Message != "Ephemeral boxes cannot be paused" {
		t.Errorf("Message = %q", apiErr.Message)
	}
	if apiErr.Method != http.MethodPost || apiErr.Path != "/v2/box/sunny-guppy-35079/pause" {
		t.Errorf("Method/Path = %s %s", apiErr.Method, apiErr.Path)
	}
}

func TestResumeSendsPOST(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		expectRequest(t, r, http.MethodPost, "/v2/box/box-1/resume")
	})
	if err := c.Resume(context.Background(), "box-1"); err != nil {
		t.Fatalf("Resume: %v", err)
	}
}

func TestDeleteSendsDELETE(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		expectRequest(t, r, http.MethodDelete, "/v2/box/box-1")
		w.WriteHeader(http.StatusNoContent)
	})
	if err := c.Delete(context.Background(), "box-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestGetDecodesObservedBox(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		expectRequest(t, r, http.MethodGet, "/v2/box/sunny-guppy-35079")
		_, _ = io.WriteString(w, observedBox)
	})
	b, err := c.Get(context.Background(), "sunny-guppy-35079")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if b.ID != "sunny-guppy-35079" || b.Name != "ocode-spike-1" || b.Runtime != "node" {
		t.Errorf("identity fields = %+v", b)
	}
	if len(b.Labels) != 1 || b.Labels[0] != "ocode-spike" {
		t.Errorf("Labels = %v", b.Labels)
	}
	if b.Status != "idle" || b.Size != "small" || b.StorageGB != 10 {
		t.Errorf("status/size/storage = %q %q %d", b.Status, b.Size, b.StorageGB)
	}
	if !b.Browser || !b.Ephemeral {
		t.Errorf("Browser=%v Ephemeral=%v, want both true", b.Browser, b.Ephemeral)
	}
	if got := b.ExpiresAt.Unix(); got != 1795194922 {
		t.Errorf("ExpiresAt = %d", got)
	}
	if got := b.CreatedAt.Unix(); got != 1795193122 {
		t.Errorf("CreatedAt = %d", got)
	}
	if got := b.UpdatedAt.Unix(); got != 1795193122 {
		t.Errorf("UpdatedAt = %d", got)
	}
}

func TestGetNonExpiringBoxHasZeroExpiry(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, boxJSON("box-1", 100))
	})
	b, err := c.Get(context.Background(), "box-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !b.ExpiresAt.IsZero() {
		t.Errorf("ExpiresAt = %v, want zero for a box that does not expire", b.ExpiresAt)
	}
	if !b.CreatedAt.Equal(time.Unix(100, 0)) {
		t.Errorf("CreatedAt = %v", b.CreatedAt)
	}
}

func TestGetDecodesNetworkPolicy(t *testing.T) {
	body := strings.Replace(observedBox, `"browser":true`,
		`"browser":true,"network_policy":{"mode":"custom","allowed_domains":["example.com"],"denied_cidrs":["10.0.0.0/8"]}`, 1)
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body)
	})
	b, err := c.Get(context.Background(), "sunny-guppy-35079")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if b.NetworkPolicy == nil || b.NetworkPolicy.Mode != NetworkCustom ||
		len(b.NetworkPolicy.AllowedDomains) != 1 || len(b.NetworkPolicy.DeniedCIDRs) != 1 {
		t.Errorf("NetworkPolicy = %+v", b.NetworkPolicy)
	}
}

func TestGetRejectsNetworkPolicyWithoutMode(t *testing.T) {
	body := strings.Replace(observedBox, `"browser":true`, `"browser":true,"network_policy":{}`, 1)
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body)
	})
	if _, err := c.Get(context.Background(), "sunny-guppy-35079"); err == nil {
		t.Fatal("Get with a network_policy that has no mode: want error")
	}
}

// TestGetRejectsMissingRequiredField was rewritten. The old version required
// size, browser and ephemeral too. The SDK's BoxData type says only id, status,
// created_at and updated_at are always present, so only those four are
// checked here. Each case removes one of them from a minimal valid box.
func TestGetRejectsMissingRequiredField(t *testing.T) {
	for field, body := range map[string]string{
		"id":         `{"status":"idle","created_at":1,"updated_at":1}`,
		"status":     `{"id":"box-1","created_at":1,"updated_at":1}`,
		"created_at": `{"id":"box-1","status":"idle","updated_at":1}`,
		"updated_at": `{"id":"box-1","status":"idle","created_at":1}`,
	} {
		t.Run(field, func(t *testing.T) {
			c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, body)
			})
			_, err := c.Get(context.Background(), "box-1")
			if err == nil || !strings.Contains(err.Error(), field) {
				t.Fatalf("Get without %s: err = %v, want a %s error", field, err, field)
			}
		})
	}
}

// normalBoxJSON is a box the API returns with only the always-present keys.
// That is the SDK's BoxData type, with no size, browser, ephemeral or expiry.
func normalBoxJSON(id string, created int64) string {
	return fmt.Sprintf(`{"id":%q,"status":"idle","created_at":%d,"updated_at":%d}`, id, created, created)
}

func TestGetNormalBoxWithOnlyRequiredKeysDecodes(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, normalBoxJSON("box-1", 100))
	})
	b, err := c.Get(context.Background(), "box-1")
	if err != nil {
		t.Fatalf("Get of a normal box: %v", err)
	}
	if b.Size != "" || b.Browser || b.Ephemeral || b.KeepAlive || !b.ExpiresAt.IsZero() {
		t.Errorf("absent keys should mean empty/false: Size=%q Browser=%v Ephemeral=%v KeepAlive=%v ExpiresAt=%v",
			b.Size, b.Browser, b.Ephemeral, b.KeepAlive, b.ExpiresAt)
	}
	if !b.CreatedAt.Equal(time.Unix(100, 0)) {
		t.Errorf("CreatedAt = %v", b.CreatedAt)
	}
}

func TestGetDecodesKeepAlive(t *testing.T) {
	body := strings.Replace(normalBoxJSON("box-1", 100), `"status":"idle"`, `"status":"idle","keep_alive":true`, 1)
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body)
	})
	b, err := c.Get(context.Background(), "box-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !b.KeepAlive {
		t.Error("KeepAlive = false, want true from keep_alive")
	}
}

func TestGetRejectsNegativeExpiry(t *testing.T) {
	body := strings.Replace(observedBox, `"expires_at":1795194922`, `"expires_at":-1`, 1)
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body)
	})
	if _, err := c.Get(context.Background(), "sunny-guppy-35079"); err == nil || !strings.Contains(err.Error(), "expires_at") {
		t.Fatalf("Get with negative expires_at: err = %v, want an expires_at error", err)
	}
}

// TestListMixesNormalAndEphemeralBoxes: one normal box with only the required
// keys and one ephemeral box must both decode.
func TestListMixesNormalAndEphemeralBoxes(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		// The normal box is created after the ephemeral one, so it sorts first.
		_, _ = io.WriteString(w, "["+normalBoxJSON("normal-1", 1795200000)+","+observedBox+"]")
	})
	boxes, err := c.List(context.Background(), "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(boxes) != 2 {
		t.Fatalf("len = %d, want 2", len(boxes))
	}
	if boxes[0].ID != "normal-1" || boxes[0].Ephemeral {
		t.Errorf("first box = %+v, want the newer normal box", boxes[0])
	}
	if boxes[1].ID != "sunny-guppy-35079" || !boxes[1].Ephemeral || boxes[1].ExpiresAt.IsZero() {
		t.Errorf("second box = %+v, want the ephemeral box", boxes[1])
	}
}

func TestGetRejectsEphemeralBoxWithoutExpiry(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"box-1","status":"idle","size":"small","browser":true,"ephemeral":true,"created_at":1,"updated_at":1}`)
	})
	if _, err := c.Get(context.Background(), "box-1"); err == nil || !strings.Contains(err.Error(), "expires_at") {
		t.Fatalf("Get of an ephemeral box without expires_at: err = %v", err)
	}
}

func TestGetRejectsDifferentBox(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, observedBox)
	})
	if _, err := c.Get(context.Background(), "some-other-box"); err == nil {
		t.Fatal("Get returning another box's id: want error")
	}
}

func TestBoxIDValidationSendsNothing(t *testing.T) {
	c := newServerClient(t, failIfCalled(t))
	ctx := context.Background()
	for _, id := range []string{"", "a/b", "a?x=1", "a#b", "a b"} {
		if _, err := c.Get(ctx, id); err == nil {
			t.Errorf("Get(%q): want error", id)
		}
		if err := c.Delete(ctx, id); err == nil {
			t.Errorf("Delete(%q): want error", id)
		}
		if err := c.Pause(ctx, id); err == nil {
			t.Errorf("Pause(%q): want error", id)
		}
		if _, err := c.CDPURL(ctx, id); err == nil {
			t.Errorf("CDPURL(%q): want error", id)
		}
	}
}

func TestListRequestsLabelAndSortsNewestFirst(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		expectRequest(t, r, http.MethodGet, "/v2/box")
		if got := r.URL.Query().Get("label"); got != "ocode-spike" {
			t.Errorf("label query = %q, want ocode-spike", got)
		}
		// The server is given no filtering duty in this test: List must filter
		// client-side too, so the "other" box is dropped here.
		_, _ = io.WriteString(w, "["+strings.Join([]string{
			boxJSON("b-old", 100, "ocode-spike"),
			boxJSON("a-new", 300, "ocode-spike"),
			boxJSON("c-other", 300, "other"),
			boxJSON("a-same", 300, "ocode-spike"),
		}, ",")+"]")
	})
	boxes, err := c.List(context.Background(), "ocode-spike")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var ids []string
	for _, b := range boxes {
		ids = append(ids, b.ID)
	}
	if got, want := strings.Join(ids, " "), "a-new a-same b-old"; got != want {
		t.Errorf("order = %q, want %q", got, want)
	}
}

func TestListWithoutLabelSendsNoQuery(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		expectRequest(t, r, http.MethodGet, "/v2/box")
		if r.URL.RawQuery != "" {
			t.Errorf("query = %q, want none", r.URL.RawQuery)
		}
		_, _ = io.WriteString(w, "[]")
	})
	boxes, err := c.List(context.Background(), "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(boxes) != 0 {
		t.Errorf("len = %d, want 0", len(boxes))
	}
}

func TestListRejectsUnexpectedShapes(t *testing.T) {
	for name, body := range map[string]string{
		"wrapped object": `{"boxes":[]}`,
		"incomplete box": `[{"id":"x"}]`,
		"null item":      `[null]`,
	} {
		t.Run(name, func(t *testing.T) {
			c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, body)
			})
			if _, err := c.List(context.Background(), ""); err == nil {
				t.Errorf("List with body %s: want error", body)
			}
		})
	}
}
