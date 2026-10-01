package cdp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveSharedDaemon(t *testing.T) {
	home := t.TempDir()
	cfgDir := filepath.Join(home, ".htrcli")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, "config.json")

	cases := []struct {
		name      string
		write     string
		in        HTRSharedInput
		wantMode  string
		wantAdopt bool
		wantPort  int
		wantSock  string
		wantTok   string
		wantSrc   string
	}{
		{
			name:     "full config uses server port and token",
			write:    `{"server":"http://127.0.0.1:3845","token":"htr_secret","htrcli_path":"/usr/local/bin/htrcli"}`,
			in:       HTRSharedInput{Shared: true, Home: home, Goos: "darwin"},
			wantMode: "shared", wantPort: 3845,
			wantSock: filepath.Join(home, ".htrcli", "daemon.sock"),
			wantTok:  "htr_secret", wantSrc: "htrcli-config",
		},
		{
			name:     "missing config falls back to default port and adopt-only",
			in:       HTRSharedInput{Shared: true, Home: home, Goos: "darwin"},
			wantMode: "shared", wantAdopt: true, wantPort: 3845,
			wantSock: filepath.Join(home, ".htrcli", "daemon.sock"),
			wantTok:  "", wantSrc: "none",
		},
		{
			name:     "non-JSON config is adopt-only, never a silent default",
			write:    "server = \"http://127.0.0.1:3845\"\ntoken = \"x\"\n",
			in:       HTRSharedInput{Shared: true, Home: home, Goos: "darwin"},
			wantMode: "shared", wantAdopt: true, wantPort: 3845,
			wantSock: filepath.Join(home, ".htrcli", "daemon.sock"),
			wantSrc:  "none",
		},
		{
			name:     "tokenless config is adopt-only",
			write:    `{"server":"http://127.0.0.1:3845"}`,
			in:       HTRSharedInput{Shared: true, Home: home, Goos: "darwin"},
			wantMode: "shared", wantAdopt: true, wantPort: 3845,
			wantSock: filepath.Join(home, ".htrcli", "daemon.sock"),
			wantSrc:  "none",
		},
		{
			name:     "ocode token override wins over the file",
			write:    `{"server":"http://127.0.0.1:3845","token":"from_file"}`,
			in:       HTRSharedInput{Shared: true, Token: "from_ocode", Home: home, Goos: "darwin"},
			wantMode: "shared", wantPort: 3845,
			wantSock: filepath.Join(home, ".htrcli", "daemon.sock"),
			wantTok:  "from_ocode", wantSrc: "ocode-config",
		},
		{
			name:     "non-loopback server is adopt-only and does not adopt its port",
			write:    `{"server":"http://10.0.0.5:9999","token":"t"}`,
			in:       HTRSharedInput{Shared: true, Home: home, Goos: "darwin"},
			wantMode: "shared", wantAdopt: true, wantPort: 3845,
			wantSock: filepath.Join(home, ".htrcli", "daemon.sock"),
			wantTok:  "t", wantSrc: "htrcli-config",
		},
		{
			name:     "shared mode ignores the legacy 3846 port",
			write:    `{"server":"http://127.0.0.1:3845","token":"t"}`,
			in:       HTRSharedInput{Shared: true, LegacyPort: 3846, Home: home, Goos: "darwin"},
			wantMode: "shared", wantPort: 3845,
			wantSock: filepath.Join(home, ".htrcli", "daemon.sock"),
			wantTok:  "t", wantSrc: "htrcli-config",
		},
		{
			name:     "windows uses a loopback endpoint, not a unix socket",
			write:    `{"server":"http://127.0.0.1:3845","token":"t"}`,
			in:       HTRSharedInput{Shared: true, Home: home, Goos: "windows"},
			wantMode: "shared", wantPort: 3845, wantSock: "127.0.0.1:3847",
			wantTok: "t", wantSrc: "htrcli-config",
		},
		{
			name:     "private mode keeps the legacy port",
			in:       HTRSharedInput{Shared: false, LegacyPort: 3846, Home: home, Goos: "darwin"},
			wantMode: "private", wantPort: 3846, wantSrc: "generated",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.write != "" {
				if err := os.WriteFile(cfgPath, []byte(tc.write), 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				_ = os.Remove(cfgPath)
			}
			in := tc.in
			in.ConfigPath = cfgPath
			got := ResolveSharedDaemon(in)
			if got.Mode != tc.wantMode || got.AdoptOnly != tc.wantAdopt {
				t.Fatalf("mode=%q adoptOnly=%v, want %q/%v", got.Mode, got.AdoptOnly, tc.wantMode, tc.wantAdopt)
			}
			if got.Port != tc.wantPort {
				t.Errorf("port=%d, want %d", got.Port, tc.wantPort)
			}
			if got.Socket != tc.wantSock {
				t.Errorf("socket=%q, want %q", got.Socket, tc.wantSock)
			}
			if got.Token != tc.wantTok {
				t.Errorf("token=%q, want %q", got.Token, tc.wantTok)
			}
			if got.TokenSource != tc.wantSrc {
				t.Errorf("tokenSource=%q, want %q", got.TokenSource, tc.wantSrc)
			}
			if got.AdoptOnly && got.Notice == "" {
				t.Error("adopt-only must always carry a notice explaining why")
			}
		})
	}
}
