package cdp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// unixSock is the socket a shared daemon uses on unix for the given home.
func unixSock(home string) string { return filepath.Join(home, ".htrcli", "daemon.sock") }

// noSock expects the empty socket (private mode carries only its configured one).
func noSock(string) string { return "" }

// litSock expects one fixed socket value regardless of the case's home.
func litSock(s string) func(string) string {
	return func(string) string { return s }
}

func TestResolveSharedDaemon(t *testing.T) {
	cases := []struct {
		name string
		// write is the htrcli config contents; "" means no config file exists.
		write     string
		in        HTRSharedInput // Home/ConfigPath are filled in per subtest.
		wantMode  string
		wantAdopt bool
		wantPort  int
		wantSock  func(home string) string
		wantTok   string
		wantSrc   string
		wantBin   string
		// wantNotice are substrings the notice must contain when wantAdopt is
		// true, so a generic notice cannot satisfy the case.
		wantNotice []string
		// wantNotNotice are substrings the notice must NOT contain: advice that
		// cannot work must not be offered.
		wantNotNotice []string
	}{
		{
			name:     "full config uses server port and token",
			write:    `{"server":"http://127.0.0.1:3845","token":"htr_secret","htrcli_path":"/usr/local/bin/htrcli"}`,
			in:       HTRSharedInput{Shared: true, Goos: "darwin"},
			wantMode: "shared", wantPort: 3845,
			wantSock: unixSock,
			wantTok:  "htr_secret", wantSrc: "htrcli-config",
			wantBin: "/usr/local/bin/htrcli",
		},
		{
			name:     "missing config falls back to default port and adopt-only",
			in:       HTRSharedInput{Shared: true, Goos: "darwin"},
			wantMode: "shared", wantAdopt: true, wantPort: 3845,
			wantSock: unixSock,
			wantTok:  "", wantSrc: "none",
			// A missing file must not be described as a file with no token.
			wantNotice: []string{"No htrcli config at"},
		},
		{
			name:     "missing config still honours the ocode token override",
			in:       HTRSharedInput{Shared: true, Token: "from_ocode", Goos: "darwin"},
			wantMode: "shared", wantPort: 3845,
			wantSock: unixSock,
			wantTok:  "from_ocode", wantSrc: "ocode-config",
		},
		{
			name:     "non-JSON config is adopt-only, never a silent default",
			write:    "server = \"http://127.0.0.1:3845\"\ntoken = \"x\"\n",
			in:       HTRSharedInput{Shared: true, Goos: "darwin"},
			wantMode: "shared", wantAdopt: true, wantPort: 3845,
			wantSock:   unixSock,
			wantSrc:    "none",
			wantNotice: []string{"Could not read", "as JSON"},
		},
		{
			name:     "unparseable config ignores the ocode token and does not offer it as a fix",
			write:    "server = \"http://127.0.0.1:3845\"\ntoken = \"x\"\n",
			in:       HTRSharedInput{Shared: true, Token: "from_ocode", Goos: "darwin"},
			wantMode: "shared", wantAdopt: true, wantPort: 3845,
			wantSock:   unixSock,
			wantTok:    "",
			wantSrc:    "none",
			wantNotice: []string{"Could not read"},
			// This branch runs before the token branch, so setting
			// browser.htr_token cannot help and the notice must not say so.
			wantNotNotice: []string{"htr_token"},
		},
		{
			name:     "tokenless config is adopt-only",
			write:    `{"server":"http://127.0.0.1:3845"}`,
			in:       HTRSharedInput{Shared: true, Goos: "darwin"},
			wantMode: "shared", wantAdopt: true, wantPort: 3845,
			wantSock:   unixSock,
			wantSrc:    "none",
			wantNotice: []string{"No token in"},
		},
		{
			name:     "ocode token override wins over the file",
			write:    `{"server":"http://127.0.0.1:3845","token":"from_file"}`,
			in:       HTRSharedInput{Shared: true, Token: "from_ocode", Goos: "darwin"},
			wantMode: "shared", wantPort: 3845,
			wantSock: unixSock,
			wantTok:  "from_ocode", wantSrc: "ocode-config",
		},
		{
			name:     "non-loopback server is adopt-only and does not adopt its port",
			write:    `{"server":"http://10.0.0.5:9999","token":"t"}`,
			in:       HTRSharedInput{Shared: true, Goos: "darwin"},
			wantMode: "shared", wantAdopt: true, wantPort: 3845,
			wantSock: unixSock,
			wantTok:  "t", wantSrc: "htrcli-config",
			wantNotice: []string{"not a loopback URL ocode can reach"},
		},
		{
			name: "prefix-lookalike host is not loopback",
			// Host matching is on the parsed hostname, never a "127." prefix:
			// 127.0.0.1.evil.com is a registrable name, not loopback.
			write:    `{"server":"http://127.0.0.1.evil.com:9999","token":"t"}`,
			in:       HTRSharedInput{Shared: true, Goos: "darwin"},
			wantMode: "shared", wantAdopt: true, wantPort: 3845,
			wantSock: unixSock,
			wantTok:  "t", wantSrc: "htrcli-config",
			wantNotice: []string{"not a loopback URL ocode can reach"},
		},
		{
			// url.Parse reads "127.0.0.1:3845" as a scheme with a colon in the
			// first path segment and rejects it, so this is an unreachable
			// server URL rather than a non-loopback host.
			name:     "schemeless host:port is adopt-only",
			write:    `{"server":"127.0.0.1:3845","token":"t"}`,
			in:       HTRSharedInput{Shared: true, Goos: "darwin"},
			wantMode: "shared", wantAdopt: true, wantPort: 3845,
			wantSock:   unixSock,
			wantTok:    "t",
			wantSrc:    "htrcli-config",
			wantNotice: []string{"not a loopback URL ocode can reach"},
		},
		{
			name:     "out-of-range port is adopt-only",
			write:    `{"server":"http://127.0.0.1:99999","token":"t"}`,
			in:       HTRSharedInput{Shared: true, Goos: "darwin"},
			wantMode: "shared", wantAdopt: true, wantPort: 3845,
			wantSock:   unixSock,
			wantTok:    "t",
			wantSrc:    "htrcli-config",
			wantNotice: []string{"not a loopback URL ocode can reach"},
		},
		{
			name:     "localhost adopts its explicit port",
			write:    `{"server":"http://localhost:3845","token":"t"}`,
			in:       HTRSharedInput{Shared: true, Goos: "darwin"},
			wantMode: "shared", wantPort: 3845,
			wantSock: unixSock,
			wantTok:  "t", wantSrc: "htrcli-config",
		},
		{
			name:     "ipv6 loopback adopts its explicit port",
			write:    `{"server":"http://[::1]:3845","token":"t"}`,
			in:       HTRSharedInput{Shared: true, Goos: "darwin"},
			wantMode: "shared", wantPort: 3845,
			wantSock: unixSock,
			wantTok:  "t", wantSrc: "htrcli-config",
		},
		{
			// htrcli has no 80/443 default, so a portless loopback URL keeps
			// DefaultHTRCLIPort instead of inventing a port nothing listens on.
			name:     "portless loopback server keeps the htrcli default port",
			write:    `{"server":"http://127.0.0.1","token":"t"}`,
			in:       HTRSharedInput{Shared: true, Goos: "darwin"},
			wantMode: "shared", wantPort: 3845,
			wantSock: unixSock,
			wantTok:  "t", wantSrc: "htrcli-config",
		},
		{
			name:     "shared mode ignores the legacy 3846 port",
			write:    `{"server":"http://127.0.0.1:3845","token":"t"}`,
			in:       HTRSharedInput{Shared: true, LegacyPort: 3846, Goos: "darwin"},
			wantMode: "shared", wantPort: 3845,
			wantSock: unixSock,
			wantTok:  "t", wantSrc: "htrcli-config",
		},
		{
			name:     "windows uses a loopback endpoint, not a unix socket",
			write:    `{"server":"http://127.0.0.1:3845","token":"t"}`,
			in:       HTRSharedInput{Shared: true, Goos: "windows"},
			wantMode: "shared", wantPort: 3845, wantSock: litSock(htrWindowsEndpoint),
			wantTok: "t", wantSrc: "htrcli-config",
		},
		{
			// Private mode must ignore htrcli's config entirely, so a config
			// IS written here: otherwise this case would also pass if private
			// mode read it.
			name:     "private mode keeps the legacy port and ignores the config file",
			write:    `{"server":"http://127.0.0.1:3845","token":"htr_secret","htrcli_path":"/usr/local/bin/htrcli"}`,
			in:       HTRSharedInput{Shared: false, LegacyPort: 3846, Goos: "darwin"},
			wantMode: "private", wantPort: 3846, wantSock: noSock, wantSrc: "generated",
			wantTok: "",
		},
	}

	notices := map[string]string{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Each case gets its own home and config path: sharing one path made
			// the cases order-coupled through an ignored os.Remove error.
			home := t.TempDir()
			cfgDir := filepath.Join(home, ".htrcli")
			if err := os.MkdirAll(cfgDir, 0o700); err != nil {
				t.Fatal(err)
			}
			cfgPath := filepath.Join(cfgDir, "config.json")
			if tc.write != "" {
				if err := os.WriteFile(cfgPath, []byte(tc.write), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			in := tc.in
			in.Home = home
			in.ConfigPath = cfgPath
			got := ResolveSharedDaemon(in)
			if got.Mode != tc.wantMode || got.AdoptOnly != tc.wantAdopt {
				t.Fatalf("mode=%q adoptOnly=%v, want %q/%v", got.Mode, got.AdoptOnly, tc.wantMode, tc.wantAdopt)
			}
			if got.Port != tc.wantPort {
				t.Errorf("port=%d, want %d", got.Port, tc.wantPort)
			}
			if wantSock := tc.wantSock(home); got.Socket != wantSock {
				t.Errorf("socket=%q, want %q", got.Socket, wantSock)
			}
			if got.Token != tc.wantTok {
				t.Errorf("token=%q, want %q", got.Token, tc.wantTok)
			}
			if got.TokenSource != tc.wantSrc {
				t.Errorf("tokenSource=%q, want %q", got.TokenSource, tc.wantSrc)
			}
			if got.Binary != tc.wantBin {
				t.Errorf("binary=%q, want %q", got.Binary, tc.wantBin)
			}
			if got.AdoptOnly {
				// Every adopt-only notice names the config file it is about, so a
				// generic string cannot pass as an explanation.
				if got.Notice == "" {
					t.Fatal("adopt-only must always carry a notice explaining why")
				}
				if !strings.Contains(got.Notice, cfgPath) {
					t.Errorf("notice %q does not name the config file %q", got.Notice, cfgPath)
				}
				for _, want := range tc.wantNotice {
					if !strings.Contains(got.Notice, want) {
						t.Errorf("notice %q does not contain %q", got.Notice, want)
					}
				}
				for _, unwanted := range tc.wantNotNotice {
					if strings.Contains(got.Notice, unwanted) {
						t.Errorf("notice %q contains %q, advice that cannot work here", got.Notice, unwanted)
					}
				}
			} else if tc.wantNotice != nil {
				t.Errorf("notice=%q, want it to contain %q", got.Notice, tc.wantNotice)
			}
			notices[tc.name] = got.Notice
		})
	}

	// The missing-file and tokenless-file notices must not be the same string:
	// telling a user with no config file that it holds no token sends them to a
	// file that does not exist.
	missing := notices["missing config falls back to default port and adopt-only"]
	tokenless := notices["tokenless config is adopt-only"]
	if missing == tokenless {
		t.Errorf("missing-file and tokenless-file notices are identical: %q", missing)
	}
}

// TestResolveSharedDaemonUnknownHome pins that an undeterminable home never
// degrades into a relative read: filepath.Join("", ".htrcli", "config.json")
// resolves against the process cwd, and a Dock-launched desktop app starts at
// "/", so it would read "/.htrcli/config.json".
func TestResolveSharedDaemonUnknownHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.UserHomeDir reads USERPROFILE on windows")
	}
	t.Setenv("HOME", "")

	got := ResolveSharedDaemon(HTRSharedInput{
		Shared:     true,
		Token:      "from_ocode",
		Goos:       "darwin",
		ConfigPath: filepath.Join(t.TempDir(), "htrcli", "config.json"),
	})
	if !got.AdoptOnly {
		t.Fatalf("adoptOnly=%v, want true when the home directory is unknown", got.AdoptOnly)
	}
	if got.ConfigPath != "" {
		t.Errorf("configPath=%q, want empty (no relative config path)", got.ConfigPath)
	}
	if got.Port != DefaultHTRCLIPort {
		t.Errorf("port=%d, want %d", got.Port, DefaultHTRCLIPort)
	}
	if got.Token != "" {
		t.Errorf("token=%q, want empty", got.Token)
	}
	if !strings.Contains(got.Notice, "home directory") {
		t.Errorf("notice=%q, want it to mention the home directory", got.Notice)
	}
}

// TestSharedDaemonTokenIsNeverSerialised pins the wire shape: the live bearer
// token must not survive a marshal, even when nothing else asks for one.
func TestSharedDaemonTokenIsNeverSerialised(t *testing.T) {
	raw := `{"server":"http://127.0.0.1:3845","token":"htr_secret"}`
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".htrcli"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".htrcli", "config.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	d := ResolveSharedDaemon(HTRSharedInput{Shared: true, Home: home, Goos: "darwin"})
	if d.Token != "htr_secret" {
		t.Fatalf("token=%q, want it resolved before marshalling", d.Token)
	}
	enc, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(enc), "htr_secret") {
		t.Errorf("marshalled daemon leaks the bearer token: %s", enc)
	}
	if !strings.Contains(string(enc), `"port":3845`) {
		t.Errorf("marshalled daemon=%s, want the snake_case port field", enc)
	}
}
