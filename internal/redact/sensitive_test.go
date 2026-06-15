package redact

import "testing"

func TestIsSensitiveFile(t *testing.T) {
	cases := []struct {
		path  string
		want  bool
		label string
	}{
		// Sensitive files
		{".env", true, "plain .env"},
		{"/home/user/project/.env", true, ".env in directory"},
		{".env.local", true, ".env.local"},
		{".env.production", true, ".env.production"},
		{"keys/server.pem", true, "*.pem"},
		{"certs/private.key", true, "*.key"},
		{".ssh/id_rsa", true, "id_rsa"},
		{".ssh/id_rsa.pub", true, "id_rsa.pub"},
		{".ssh/id_dsa", true, "id_dsa"},
		{".ssh/id_ecdsa", true, "id_ecdsa"},
		{".ssh/id_ed25519", true, "id_ed25519"},
		{"certs/cert.pfx", true, "*.pfx"},
		{"certs/cert.p12", true, "*.p12"},
		{".npmrc", true, ".npmrc"},
		{".netrc", true, ".netrc"},
		{"config/aws-credentials.json", true, "*credentials*"},
		{"secrets.yaml", true, "secrets.*"},
		{"secrets.json", true, "secrets.* (json)"},
		{".pgpass", true, ".pgpass"},
		// Non-sensitive files
		{"main.go", false, "*.go"},
		{"app.ts", false, "*.ts"},
		{"component.tsx", false, "*.tsx"},
		{"README.md", false, "README.md"},
		{"package.json", false, "package.json"},
		{"go.mod", false, "go.mod"},
		{"Dockerfile", false, "Dockerfile"},
		{"", false, "empty string"},
	}
	for _, tc := range cases {
		got := IsSensitiveFile(tc.path)
		if got != tc.want {
			t.Errorf("IsSensitiveFile(%q) = %v, want %v (%s)", tc.path, got, tc.want, tc.label)
		}
	}
}
