package server

import (
	"testing"
	"time"
)

func TestArtifactLogRetention(t *testing.T) {
	cases := []struct {
		env  string
		want time.Duration
	}{
		{"", 30 * 24 * time.Hour},
		{"0", 0},
		{"7", 7 * 24 * time.Hour},
		{"-1", 30 * 24 * time.Hour},
		{"abc", 30 * 24 * time.Hour},
		{" 5 ", 5 * 24 * time.Hour},
	}
	for _, tc := range cases {
		t.Setenv("OCODE_ARTIFACT_LOG_RETENTION_DAYS", tc.env)
		if got := artifactLogRetention(); got != tc.want {
			t.Errorf("artifactLogRetention(%q) = %v, want %v", tc.env, got, tc.want)
		}
	}
}
