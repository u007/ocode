package auth

import "testing"

func TestCloudflareWorkersBaseURL(t *testing.T) {
	got := CloudflareWorkersBaseURL("abc123")
	want := "https://api.cloudflare.com/client/v4/accounts/abc123/ai/v1"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
