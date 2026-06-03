package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRequestDeviceCode_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/accounts/deviceauth/usercode" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(deviceCodeResponse{
			DeviceAuthID: "dev123",
			UserCode:     "ABCD-1234",
			Interval:     "5",
		})
	}))
	defer srv.Close()

	oldAuth := deviceAuthURL
	oldToken := deviceTokenURL
	deviceAuthURL = srv.URL + "/api/accounts/deviceauth/usercode"
	deviceTokenURL = srv.URL + "/api/accounts/deviceauth/token"
	defer func() {
		deviceAuthURL = oldAuth
		deviceTokenURL = oldToken
	}()

	resp, err := requestDeviceCode(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.DeviceAuthID != "dev123" {
		t.Errorf("expected DeviceAuthID=dev123, got %s", resp.DeviceAuthID)
	}
	if resp.UserCode != "ABCD-1234" {
		t.Errorf("expected UserCode=ABCD-1234, got %s", resp.UserCode)
	}
}

func TestRequestDeviceCode_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer srv.Close()

	oldAuth := deviceAuthURL
	deviceAuthURL = srv.URL + "/api/accounts/deviceauth/usercode"
	defer func() { deviceAuthURL = oldAuth }()

	_, err := requestDeviceCode(context.Background())
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestPollDeviceToken_PendingThenSuccess(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount < 3 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		json.NewEncoder(w).Encode(deviceTokenResponse{
			AuthorizationCode: "auth_code_123",
			CodeVerifier:      "verifier_123",
		})
	}))
	defer srv.Close()

	oldToken := deviceTokenURL
	deviceTokenURL = srv.URL + "/api/accounts/deviceauth/token"
	defer func() { deviceTokenURL = oldToken }()

	code, err := pollDeviceToken(context.Background(), "dev123", "ABCD-1234", 10*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != "auth_code_123" {
		t.Errorf("expected auth_code_123, got %s", code)
	}
	if callCount != 3 {
		t.Errorf("expected 3 calls, got %d", callCount)
	}
}

func TestPollDeviceToken_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	oldToken := deviceTokenURL
	deviceTokenURL = srv.URL + "/api/accounts/deviceauth/token"
	defer func() { deviceTokenURL = oldToken }()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := pollDeviceToken(ctx, "dev123", "ABCD-1234", 10*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestPollDeviceToken_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	oldToken := deviceTokenURL
	deviceTokenURL = srv.URL + "/api/accounts/deviceauth/token"
	defer func() { deviceTokenURL = oldToken }()

	_, err := pollDeviceToken(context.Background(), "dev123", "ABCD-1234", 10*time.Millisecond)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}
