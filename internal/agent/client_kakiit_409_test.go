package agent

import (
	"net/http"
	"testing"
)

// kakiit409Body is the gateway's OpenAI-shaped 409 duplicate-request-id
// envelope. The gateway treats X-Request-Id as SINGLE-USE (it cannot replay a
// response, and re-executing would bill the upstream twice), so a reused id is
// refused rather than re-run.
const kakiit409Body = `{"error":{"message":"this request id has already been used; use a fresh id per request","type":"duplicate_request_id","code":"duplicate_request_id"},"request_id":"req-stub-409"}`

// TestKakiit409DuplicateRequestIDMapsToClearMessage pins the user-visible
// mapping: a reused id is not a generic provider failure, it means the attempt
// already ran — the user's remedy is to send the message again (a new turn mints
// a fresh id), not to retry the same request.
func TestKakiit409DuplicateRequestIDMapsToClearMessage(t *testing.T) {
	seedKakiitProtocols(t)

	err := chatErrorFromStub(t, http.StatusConflict, kakiit409Body)
	if err == nil {
		t.Fatal("expected the 409 to fail the turn")
	}
	if msg := kakiitUserFacingError(err); msg != KakiitDuplicateRequestMessage {
		t.Fatalf("409 message = %q, want %q", msg, KakiitDuplicateRequestMessage)
	}
}

// TestKakiit409OtherConflictsStayGeneric pins that classification is by the
// envelope CODE, not the status alone: an unrelated 409 must not tell the user
// their request "already ran".
func TestKakiit409OtherConflictsStayGeneric(t *testing.T) {
	seedKakiitProtocols(t)

	const otherBody = `{"error":{"message":"conflict","type":"conflict","code":"something_else"}}`
	err := chatErrorFromStub(t, http.StatusConflict, otherBody)
	if err == nil {
		t.Fatal("expected the 409 to fail the turn")
	}
	if msg := kakiitUserFacingError(err); msg != "" {
		t.Fatalf("an unrelated 409 must stay generic, got %q", msg)
	}
}
