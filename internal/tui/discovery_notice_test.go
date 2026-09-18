package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/server"
)

// The discovery notices were TUI-only: the browser mirror never saw them
// because the delta branch appended a transcript notice but did not call
// broadcastRC. This pins the mirror for both kinds the TUI handles.
func TestDiscoveryDeltaBroadcastsToRCWeb(t *testing.T) {
	cases := []struct {
		kind  string
		text  string
		event string
	}{
		{"discovery", "Notion/search", "discovery"},
		{"md-indexing", "docs/README.md", "md_indexing"},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			bridge := &server.RCBridge{SessionID: "ses_rc"}
			ch := bridge.Subscribe()
			defer bridge.Unsubscribe(ch)

			m := replacementTestModel()
			m.rcBridge = bridge

			updated, _ := m.Update(deltaMsg{delta: deltaEvent{kind: tc.kind, text: tc.text}})
			got := updated.(model)

			select {
			case ev := <-ch:
				if ev.Event != tc.event {
					t.Fatalf("event = %q, want %q", ev.Event, tc.event)
				}
				data, ok := ev.Data.(map[string]string)
				if !ok {
					t.Fatalf("data type = %T, want map[string]string", ev.Data)
				}
				if data["delta"] != tc.text {
					t.Fatalf("delta = %q, want %q", data["delta"], tc.text)
				}
			case <-time.After(time.Second):
				t.Fatalf("no %q frame broadcast to the web mirror", tc.event)
			}

			// The TUI transcript notice is still appended alongside the mirror.
			if !hasNoticeContaining(got.messages, tc.text) {
				t.Fatalf("transcript notice for %q missing", tc.text)
			}
		})
	}
}

func hasNoticeContaining(msgs []message, want string) bool {
	for _, msg := range msgs {
		if msg.transient && msg.skipLLM && strings.Contains(msg.text, want) {
			return true
		}
	}
	return false
}
