package messaging

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMessengerVerifyHandshake(t *testing.T) {
	prev := DefaultManager
	DefaultManager = nil
	defer func() { DefaultManager = prev }()

	p := NewMessengerProvider(Account{ID: "fb-1", Provider: ProviderFacebook, Credentials: `{"page_token":"tok","verify_token":"vt"}`})
	// bypass Start (no network) by setting fields directly
	p.pageToken = "tok"
	p.verifyToken = "vt"
	p.pageID = "page1"
	p.status = "connected"
	registerMessenger(p)
	defer unregisterMessenger(p)

	// correct token
	req := httptest.NewRequest(http.MethodGet, "/api/messaging/messenger/webhook?hub.mode=subscribe&hub.verify_token=vt&hub.challenge=abc123", nil)
	rr := httptest.NewRecorder()
	HandleMessengerWebhook(rr, req)
	if rr.Code != 200 || strings.TrimSpace(rr.Body.String()) != "abc123" {
		t.Fatalf("good verify: %d %s", rr.Code, rr.Body.String())
	}

	// wrong token
	req2 := httptest.NewRequest(http.MethodGet, "/api/messaging/messenger/webhook?hub.mode=subscribe&hub.verify_token=wrong&hub.challenge=abc123", nil)
	rr2 := httptest.NewRecorder()
	HandleMessengerWebhook(rr2, req2)
	if rr2.Code != 403 {
		t.Fatalf("bad verify: %d", rr2.Code)
	}
}

func signBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestMessengerPostSignature(t *testing.T) {
	prev := DefaultManager
	DefaultManager = nil
	defer func() { DefaultManager = prev }()

	p := NewMessengerProvider(Account{ID: "fb-2", Provider: ProviderFacebook})
	p.pageToken = "tok"
	p.verifyToken = "vt"
	p.appSecret = "secret"
	p.pageID = "page2"
	p.status = "connected"
	registerMessenger(p)
	defer unregisterMessenger(p)

	body := `{"object":"page","entry":[{"id":"page2","messaging":[{"sender":{"id":"user1"},"recipient":{"id":"page2"},"timestamp":1234,"message":{"mid":"m1","text":"hi"}}]}]}`
	secret := "secret"

	// missing signature -> 403
	req := httptest.NewRequest(http.MethodPost, "/api/messaging/messenger/webhook", strings.NewReader(body))
	rr := httptest.NewRecorder()
	HandleMessengerWebhook(rr, req)
	if rr.Code != 403 {
		t.Fatalf("missing sig: %d", rr.Code)
	}

	// bad signature -> 403
	req2 := httptest.NewRequest(http.MethodPost, "/api/messaging/messenger/webhook", strings.NewReader(body))
	req2.Header.Set("X-Hub-Signature-256", "sha256=deadbeef")
	rr2 := httptest.NewRecorder()
	HandleMessengerWebhook(rr2, req2)
	if rr2.Code != 403 {
		t.Fatalf("bad sig: %d", rr2.Code)
	}

	// good signature -> 200
	req3 := httptest.NewRequest(http.MethodPost, "/api/messaging/messenger/webhook", strings.NewReader(body))
	req3.Header.Set("X-Hub-Signature-256", signBody(secret, []byte(body)))
	rr3 := httptest.NewRecorder()
	HandleMessengerWebhook(rr3, req3)
	if rr3.Code != 200 {
		t.Fatalf("good sig: %d %s", rr3.Code, rr3.Body.String())
	}
}

func TestMessengerSend(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/me/messages") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"message_id": "mid-123"})
	}))
	defer srv.Close()

	prevBase := graphAPIBase
	graphAPIBase = srv.URL
	defer func() { graphAPIBase = prevBase }()

	p := NewMessengerProvider(Account{ID: "fb-3", Provider: ProviderFacebook})
	p.pageToken = "tok"
	p.pageID = "page3"
	p.status = "connected"
	mid, err := p.Send(t.Context(), "user1", "hello")
	if err != nil {
		t.Fatalf("send err: %v", err)
	}
	if mid != "mid-123" {
		t.Fatalf("mid: %s", mid)
	}
}

func TestMessengerRegistryRace(t *testing.T) {
	var ps []*MessengerProvider
	for i := 0; i < 10; i++ {
		pp := NewMessengerProvider(Account{ID: fmt.Sprintf("r-%d", i), Provider: ProviderFacebook})
		pp.pageID = fmt.Sprintf("page-%d", i)
		pp.verifyToken = fmt.Sprintf("vt-%d", i)
		ps = append(ps, pp)
	}
	done := make(chan struct{})
	go func() {
		for _, pp := range ps {
			registerMessenger(pp)
		}
		close(done)
	}()
	for i := 0; i < 100; i++ {
		_ = MessengerProviderByPage("page-5")
		_ = MessengerProviderByVerify("vt-5")
	}
	<-done
	for _, pp := range ps {
		unregisterMessenger(pp)
	}
}

func TestMessengerPostMalformed(t *testing.T) {
	p := NewMessengerProvider(Account{ID: "fb-m", Provider: ProviderFacebook})
	p.pageToken = "tok"
	p.appSecret = "secret"
	p.pageID = "pagem"
	p.verifyToken = "vtm"
	p.status = "connected"
	registerMessenger(p)
	defer unregisterMessenger(p)

	req := httptest.NewRequest(http.MethodPost, "/api/messaging/messenger/webhook", strings.NewReader("{bad json"))
	req.Header.Set("X-Hub-Signature-256", signBody("secret", []byte("{bad json")))
	rr := httptest.NewRecorder()
	HandleMessengerWebhook(rr, req)
	if rr.Code != 400 {
		t.Fatalf("malformed: %d", rr.Code)
	}
}

func TestMessengerPostOversized(t *testing.T) {
	p := NewMessengerProvider(Account{ID: "fb-o", Provider: ProviderFacebook})
	p.pageToken = "tok"
	p.appSecret = "secret"
	p.pageID = "pageo"
	p.verifyToken = "vto"
	p.status = "connected"
	registerMessenger(p)
	defer unregisterMessenger(p)

	big := strings.Repeat("a", (1<<20)+100)
	req := httptest.NewRequest(http.MethodPost, "/api/messaging/messenger/webhook", strings.NewReader(big))
	req.Header.Set("X-Hub-Signature-256", signBody("secret", []byte(big)))
	rr := httptest.NewRecorder()
	HandleMessengerWebhook(rr, req)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized: %d", rr.Code)
	}
}

func TestMessengerPostUnknownPage(t *testing.T) {
	body := `{"object":"page","entry":[{"id":"unknownpage","messaging":[{"sender":{"id":"u1"},"recipient":{"id":"unknownpage"},"timestamp":1,"message":{"mid":"x","text":"hi"}}]}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/messaging/messenger/webhook", strings.NewReader(body))
	rr := httptest.NewRecorder()
	HandleMessengerWebhook(rr, req)
	if rr.Code != 200 {
		t.Fatalf("unknown page: %d", rr.Code)
	}
}

func TestMessengerEchoSkipped(t *testing.T) {
	// just ensure no panic; inbound not asserted here
	p := NewMessengerProvider(Account{ID: "fb-e", Provider: ProviderFacebook})
	p.pageToken = "tok"
	p.appSecret = "secret"
	p.pageID = "pagee"
	p.verifyToken = "vte"
	p.status = "connected"
	registerMessenger(p)
	defer unregisterMessenger(p)

	body := `{"object":"page","entry":[{"id":"pagee","messaging":[{"sender":{"id":"pagee"},"recipient":{"id":"pagee"},"timestamp":1,"message":{"mid":"echo1","text":"echo","is_echo":true}}]}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/messaging/messenger/webhook", strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", signBody("secret", []byte(body)))
	rr := httptest.NewRecorder()
	HandleMessengerWebhook(rr, req)
	if rr.Code != 200 {
		t.Fatalf("echo: %d", rr.Code)
	}
}

func TestMessengerStopUnregisters(t *testing.T) {
	p := NewMessengerProvider(Account{ID: "fb-s", Provider: ProviderFacebook})
	p.pageToken = "tok"
	p.appSecret = "secret"
	p.pageID = "pages"
	p.verifyToken = "vts"
	p.status = "connected"
	if err := registerMessenger(p); err != nil {
		t.Fatal(err)
	}
	p.Stop()
	if MessengerProviderByPage("pages") != nil {
		t.Fatal("not unregistered by page")
	}
	if MessengerProviderByVerify("vts") != nil {
		t.Fatal("not unregistered by verify")
	}
}

func TestMessengerDuplicateMid(t *testing.T) {
	p := NewMessengerProvider(Account{ID: "fb-d", Provider: ProviderFacebook})
	p.pageToken = "tok"
	p.appSecret = "secret"
	p.pageID = "paged"
	p.verifyToken = "vtd"
	p.status = "connected"
	registerMessenger(p)
	defer unregisterMessenger(p)

	body := `{"object":"page","entry":[{"id":"paged","messaging":[{"sender":{"id":"u1"},"recipient":{"id":"paged"},"timestamp":1,"message":{"mid":"dup1","text":"hi"}}]}]}`
	sig := signBody("secret", []byte(body))
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/messaging/messenger/webhook", strings.NewReader(body))
		req.Header.Set("X-Hub-Signature-256", sig)
		rr := httptest.NewRecorder()
		HandleMessengerWebhook(rr, req)
		if rr.Code != 200 {
			t.Fatalf("iter %d: %d", i, rr.Code)
		}
	}
}

func TestMessengerTwoPages(t *testing.T) {
	p1 := NewMessengerProvider(Account{ID: "fb-a", Provider: ProviderFacebook})
	p1.pageToken = "tok1"
	p1.appSecret = "secret1"
	p1.pageID = "pageA"
	p1.verifyToken = "vtA"
	p1.status = "connected"
	if err := registerMessenger(p1); err != nil {
		t.Fatal(err)
	}
	defer unregisterMessenger(p1)

	p2 := NewMessengerProvider(Account{ID: "fb-b", Provider: ProviderFacebook})
	p2.pageToken = "tok2"
	p2.appSecret = "secret2"
	p2.pageID = "pageB"
	p2.verifyToken = "vtB"
	p2.status = "connected"
	if err := registerMessenger(p2); err != nil {
		t.Fatal(err)
	}
	defer unregisterMessenger(p2)

	if MessengerProviderByPage("pageA") != p1 {
		t.Fatal("pageA wrong")
	}
	if MessengerProviderByPage("pageB") != p2 {
		t.Fatal("pageB wrong")
	}
}

func TestMessengerStartRequiresSecret(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "pageX", "name": "Page X"})
	}))
	defer srv.Close()
	prevBase := graphAPIBase
	graphAPIBase = srv.URL
	defer func() { graphAPIBase = prevBase }()

	st, _ := NewStore(t.TempDir() + "/m.db")
	defer st.Close()
	mgr := NewManager(st, t.TempDir())
	acc := Account{ID: "fb-sec", Provider: ProviderFacebook, Credentials: `{"page_token":"tok"}`}
	if err := st.UpsertAccount(acc); err != nil {
		t.Fatal(err)
	}
	// no app secret -> Start should fail
	if err := mgr.StartAccount(t.Context(), acc); err == nil {
		t.Fatal("expected error for missing app secret")
	}
}

func TestMessengerStartStopStart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "pageSS", "name": "Page SS"})
	}))
	defer srv.Close()
	prevBase := graphAPIBase
	graphAPIBase = srv.URL
	defer func() { graphAPIBase = prevBase }()

	st, _ := NewStore(t.TempDir() + "/m.db")
	defer st.Close()
	mgr := NewManager(st, t.TempDir())
	acc := Account{ID: "fb-ss", Provider: ProviderFacebook, Credentials: `{"page_token":"tok","app_secret":"secret","verify_token":"vtss"}`}
	if err := st.UpsertAccount(acc); err != nil {
		t.Fatal(err)
	}
	if err := mgr.StartAccount(t.Context(), acc); err != nil {
		t.Fatalf("start1: %v", err)
	}
	mgr.StopAccount(acc.ID)
	if err := mgr.StartAccount(t.Context(), acc); err != nil {
		t.Fatalf("start2: %v", err)
	}
	mgr.StopAccount(acc.ID)
}
