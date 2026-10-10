package server

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/u007/ocode/internal/auth"
	providerplugin "github.com/u007/ocode/internal/plugin/provider"
)

// Every start response reports the flow's `state`, so a client can branch on
// it without a second poll. Only the OpenAI starts reported it originally; the
// web panel had to wait for its first poll to learn the state for every other
// kind. Each case parks its flow (the completion blocks on release), so the
// state in the response is the state the flow is waiting in, not a race
// against a fast finish.
func TestConnectStartResponsesReportFlowState(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		method   string
		setup    func(t *testing.T, release <-chan struct{})
		want     string
	}{
		{
			name:     "anthropic paste-code",
			provider: "anthropic",
			method:   "oauth_max",
			want:     "waiting_input",
			setup: func(t *testing.T, _ <-chan struct{}) {
				stubConnectSeam(t, &anthropicAuthorizeFn, func(mode string) (auth.AnthropicFlow, error) {
					return auth.AnthropicFlow{URL: "https://claude.ai/oauth/authorize?x=1", State: "st", Verifier: "vf"}, nil
				})
			},
		},
		{
			name:     "google local-callback",
			provider: "google",
			method:   "oauth",
			want:     "waiting_browser",
			setup: func(t *testing.T, release <-chan struct{}) {
				stubConnectSeam(t, &googleStartFn, func() (string, func() (string, error), error) {
					return "https://accounts.google.com/o/oauth2/auth", func() (string, error) {
						<-release
						return "", errors.New("released")
					}, nil
				})
			},
		},
		{
			name:     "copilot device-code",
			provider: "copilot",
			method:   "oauth",
			want:     "waiting_browser",
			setup: func(t *testing.T, release <-chan struct{}) {
				stubConnectSeam(t, &copilotStartFn, func() (auth.CopilotDevice, error) {
					return auth.CopilotDevice{DeviceCode: "dc-1", UserCode: "ABCD-EFGH", VerificationURI: "https://github.com/login/device", Interval: 1}, nil
				})
				entered := make(chan struct{})
				stubConnectSeam(t, &copilotPollFn, func(ctx context.Context, dev auth.CopilotDevice) (auth.Credential, error) {
					close(entered)
					<-release
					return auth.Credential{}, errors.New("released")
				})
				// The poll goroutine reads copilotPollFn when it starts. Cleanups
				// run last-in first-out, so this wait runs before stubConnectSeam
				// restores the seam: the restore cannot race that read.
				t.Cleanup(func() { <-entered })
			},
		},
		{
			name:     "grok cookies",
			provider: "grok",
			method:   "grok_subscription",
			want:     "waiting_input",
			setup: func(t *testing.T, _ <-chan struct{}) {
				stubConnectPlugin(t, "grok", []providerplugin.AuthMethod{
					{Label: "Grok Subscription", Type: "oauth", Run: func(ctx context.Context) (providerplugin.AuthResult, error) {
						return providerplugin.AuthResult{}, errors.New("unused: the cookie exchange seam runs instead")
					}},
				})
			},
		},
		{
			name:     "plugin",
			provider: "opencode",
			method:   "plugin_Blocked Key",
			want:     "running",
			setup: func(t *testing.T, release <-chan struct{}) {
				stubConnectPlugin(t, "opencode", []providerplugin.AuthMethod{
					{Label: "Blocked Key", Type: "api", Run: func(ctx context.Context) (providerplugin.AuthResult, error) {
						<-release
						return providerplugin.AuthResult{}, errors.New("released")
					}},
				})
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHandler()
			preserveConnectCredential(t, tc.provider)
			release := make(chan struct{})
			t.Cleanup(func() { close(release) })
			tc.setup(t, release)

			resp, code := connectDo(t, h.handleConnectOAuthStart, "POST",
				"/api/auth/connect/"+tc.provider+"/oauth/start",
				map[string]string{"provider": tc.provider},
				map[string]string{"method": tc.method})
			if code != http.StatusOK {
				t.Fatalf("start %s: %d %v", tc.name, code, resp)
			}
			if got, _ := resp["state"].(string); got != tc.want {
				t.Fatalf("start response state = %q, want %q (full response: %v)", got, tc.want, resp)
			}

			flowID, _ := resp["flowId"].(string)
			if _, code := connectDo(t, h.handleConnectFlowCancel, "DELETE", "/flows/"+flowID,
				map[string]string{"flowId": flowID}, nil); code != http.StatusOK {
				t.Fatalf("cancel %s: %d", tc.name, code)
			}
		})
	}
}
