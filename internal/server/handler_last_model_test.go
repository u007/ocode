package server

import (
	"net/http"
	"testing"
)

func TestRewindAcceptedResponseUsesDispatchedModel(t *testing.T) {
	f := newRewindFixture(t, rewindMessages())
	client := newBlockingClient()
	resident := f.resident(client, rewindMessages())
	resident.model = "stale-resident-model"
	f.handler.cfg.Model = "dispatched-model"

	token := f.arm(2, "replace me", 2)
	rec := f.request(http.MethodPost, "/api/sessions/"+f.id+"/message", map[string]any{
		"content":     "edited replacement",
		"async":       true,
		"rewindToken": token,
	}, true)
	close(client.release)
	f.handler.turnJobsWG.Wait()

	if rec.Code != http.StatusAccepted {
		t.Fatalf("tokenized send status = %d, want 202: %s", rec.Code, rec.Body.String())
	}
	var response ChatResponse
	decodeRewindResponse(t, rec, &response)
	if response.Model != "dispatched-model" {
		t.Fatalf("rewind response model = %q, want dispatched-model", response.Model)
	}
}

func TestRCBridgeModelForDispatchPrefersLiveStatus(t *testing.T) {
	bridge := &RCBridge{Model: "registration-model"}
	bridge.StatusStore().Set(TUIStatus{MainModel: "live-model"}, bridge)

	if got := bridge.ModelForDispatch(); got != "live-model" {
		t.Fatalf("ModelForDispatch() = %q, want live-model", got)
	}

	bridge.StatusStore().Set(TUIStatus{}, bridge)
	if got := bridge.ModelForDispatch(); got != "registration-model" {
		t.Fatalf("ModelForDispatch() fallback = %q, want registration-model", got)
	}
}
