package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/session"
)

// pulseChatsTestCall runs one handler method against a request and returns the
// status and body. Each test drives the handlers directly, as the existing
// assistant tests do.
func pulseChatsTestCall(t *testing.T, handler func(http.ResponseWriter, *http.Request), method, target, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec.Code, rec.Body.String()
}

type pulseChatsListBody struct {
	Chats []struct {
		SessionID string `json:"session_id"`
	} `json:"chats"`
	Total   int    `json:"total"`
	Current string `json:"current"`
}

func listPulseChats(t *testing.T, h *Handler, query string) pulseChatsListBody {
	t.Helper()
	code, raw := pulseChatsTestCall(t, h.HandleListPulseChats, http.MethodGet, "/api/pulse/assistant/chats"+query, "")
	if code != http.StatusOK {
		t.Fatalf("list %q: status %d, body %s", query, code, raw)
	}
	var out pulseChatsListBody
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode list %q: %v", raw, err)
	}
	return out
}

func TestPulseChatNewRotatesAndKeepsOldTranscript(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)
	h := newPulseAssistantHandler(t, home)
	_, first, _ := getPulseAssistant(t, h)
	firstID := first["session_id"]

	code, raw := pulseChatsTestCall(t, h.HandleNewPulseChat, http.MethodPost, "/api/pulse/assistant/new", "")
	if code != http.StatusOK {
		t.Fatalf("new chat: status %d, body %s", code, raw)
	}
	var created map[string]string
	if err := json.Unmarshal([]byte(raw), &created); err != nil {
		t.Fatal(err)
	}
	newID := created["session_id"]
	if newID == "" || newID == firstID || !isPulseSession(newID) {
		t.Fatalf("new chat id = %q, want a fresh pulse_ id distinct from %q", newID, firstID)
	}

	root, err := pulseRootPath()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), newID) {
		t.Fatalf("state.json = %s, want current id %q", data, newID)
	}
	if exists, err := session.ExistsForDir(root, firstID); err != nil || !exists {
		t.Fatalf("previous transcript %q exists=%v err=%v; new chat must not delete it", firstID, exists, err)
	}

	list := listPulseChats(t, h, "")
	if list.Total != 2 || list.Current != newID {
		t.Fatalf("list total=%d current=%q, want total 2 current %q", list.Total, list.Current, newID)
	}
}

func TestPulseChatSelectSwitchesBackToEarlierChat(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)
	h := newPulseAssistantHandler(t, home)
	_, first, _ := getPulseAssistant(t, h)
	firstID := first["session_id"]
	pulseChatsTestCall(t, h.HandleNewPulseChat, http.MethodPost, "/api/pulse/assistant/new", "")

	code, raw := pulseChatsTestCall(t, h.HandleSelectPulseChat, http.MethodPut, "/api/pulse/assistant",
		`{"session_id":"`+firstID+`"}`)
	if code != http.StatusOK {
		t.Fatalf("select: status %d, body %s", code, raw)
	}
	if list := listPulseChats(t, h, ""); list.Current != firstID {
		t.Fatalf("current after select = %q, want %q", list.Current, firstID)
	}
}

func TestPulseChatSelectRejectsBadAndMissingIDs(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)
	h := newPulseAssistantHandler(t, home)
	getPulseAssistant(t, h)

	if code, _ := pulseChatsTestCall(t, h.HandleSelectPulseChat, http.MethodPut, "/api/pulse/assistant",
		`{"session_id":"ses_not_a_pulse_chat"}`); code != http.StatusBadRequest {
		t.Fatalf("non-pulse id: status %d, want 400", code)
	}
	if code, _ := pulseChatsTestCall(t, h.HandleSelectPulseChat, http.MethodPut, "/api/pulse/assistant",
		`{"session_id":"pulse_2000-01-01-000000-deadbeef"}`); code != http.StatusNotFound {
		t.Fatalf("missing chat: status %d, want 404", code)
	}
	// The id becomes a path segment under the pulse root, so a traversal id is
	// refused before any file is probed.
	if code, _ := pulseChatsTestCall(t, h.HandleSelectPulseChat, http.MethodPut, "/api/pulse/assistant",
		`{"session_id":"pulse_/../../x"}`); code != http.StatusBadRequest {
		t.Fatalf("traversal id: status %d, want 400", code)
	}
}

func TestPulsePageClampsAndCopies(t *testing.T) {
	all := []int{1, 2, 3, 4, 5}
	page, total := pulsePage(all, 2, 4)
	if total != 5 || len(page) != 1 || page[0] != 5 {
		t.Fatalf("last partial page: %v of %d, want [5] of 5", page, total)
	}
	past, total := pulsePage(all, 2, 9)
	if total != 5 || past == nil || len(past) != 0 {
		t.Fatalf("offset past the end: %v of %d, want an empty non-nil page of 5", past, total)
	}
}

func TestPulseChatNewAndSelectRefuseWhileCurrentChatIsBusy(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)
	h := newPulseAssistantHandler(t, home)
	_, first, _ := getPulseAssistant(t, h)
	firstID := first["session_id"]
	pulseChatsTestCall(t, h.HandleNewPulseChat, http.MethodPost, "/api/pulse/assistant/new", "")
	list := listPulseChats(t, h, "")
	current := list.Current

	// A running turn on the current chat must block a switch away from it: the
	// drawer would stop showing a turn that is still producing output.
	h.cancelMu.Lock()
	h.turnInFlight[current]++
	h.cancelMu.Unlock()
	defer func() {
		h.cancelMu.Lock()
		h.turnInFlight[current]--
		h.cancelMu.Unlock()
	}()

	if code, _ := pulseChatsTestCall(t, h.HandleNewPulseChat, http.MethodPost, "/api/pulse/assistant/new", ""); code != http.StatusConflict {
		t.Fatalf("new while busy: status %d, want 409", code)
	}
	if code, _ := pulseChatsTestCall(t, h.HandleSelectPulseChat, http.MethodPut, "/api/pulse/assistant",
		`{"session_id":"`+firstID+`"}`); code != http.StatusConflict {
		t.Fatalf("select while busy: status %d, want 409", code)
	}
	if got := listPulseChats(t, h, "").Current; got != current {
		t.Fatalf("current changed to %q while busy; want %q", got, current)
	}
}

func TestPulseChatListPaginatesNewestFirst(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)
	h := newPulseAssistantHandler(t, home)
	getPulseAssistant(t, h)
	for i := 0; i < 2; i++ {
		pulseChatsTestCall(t, h.HandleNewPulseChat, http.MethodPost, "/api/pulse/assistant/new", "")
	}

	if page := listPulseChats(t, h, "?limit=2"); len(page.Chats) != 2 || page.Total != 3 {
		t.Fatalf("limit=2: got %d rows total %d, want 2 rows total 3", len(page.Chats), page.Total)
	}
	if page := listPulseChats(t, h, "?limit=2&offset=2"); len(page.Chats) != 1 || page.Total != 3 {
		t.Fatalf("offset=2: got %d rows total %d, want 1 row total 3", len(page.Chats), page.Total)
	}
	if code, _ := pulseChatsTestCall(t, h.HandleListPulseChats, http.MethodGet, "/api/pulse/assistant/chats?limit=0", ""); code != http.StatusBadRequest {
		t.Fatalf("limit=0: status %d, want 400", code)
	}
}
