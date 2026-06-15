package tui

import "testing"

func TestBuildRCSessionURL(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		baseURL   string
		sessionID string
		token     string
		want      string
	}{
		{
			name:      "trims trailing slash",
			baseURL:   "https://alice.ts.net/",
			sessionID: "sess-123",
			token:     "tok-abc",
			want:      "https://alice.ts.net/session/sess-123?token=tok-abc",
		},
		{
			name:      "empty base url",
			baseURL:   "",
			sessionID: "sess-123",
			token:     "tok-abc",
			want:      "",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := buildRCSessionURL(tc.baseURL, tc.sessionID, tc.token); got != tc.want {
				t.Fatalf("buildRCSessionURL() = %q, want %q", got, tc.want)
			}
		})
	}
}
