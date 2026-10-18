package server

import (
	"encoding/json"
	"strings"
	"testing"
)

// session_new must report the session the server actually bound. The async
// /api/chat reply names it under "sessionId" (ChatResponse), so an unbound or
// mis-keyed reply has to fail instead of echoing the id the tool generated.
func TestPulseCreatedSessionIDReadsServerField(t *testing.T) {
	id, err := pulseCreatedSessionID(json.RawMessage(`{"sessionId":"ses_bound","model":"m"}`))
	if err != nil {
		t.Fatalf("pulseCreatedSessionID: %v", err)
	}
	if id != "ses_bound" {
		t.Fatalf("pulseCreatedSessionID = %q, want ses_bound", id)
	}
}

func TestPulseCreatedSessionIDRefusesUnboundReply(t *testing.T) {
	for _, body := range []string{`{}`, `{"session_id":"ses_wrong_key"}`} {
		id, err := pulseCreatedSessionID(json.RawMessage(body))
		if err == nil || !strings.Contains(err.Error(), "no session id") {
			t.Fatalf("pulseCreatedSessionID(%s) err = %v, want no session id", body, err)
		}
		if id != "" {
			t.Fatalf("pulseCreatedSessionID(%s) id = %q, want empty on error", body, id)
		}
	}
}
