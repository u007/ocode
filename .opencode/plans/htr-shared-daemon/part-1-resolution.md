# Part 1 — Config fields and shared-daemon resolution (Tasks 1–2)

Self-contained. You do not need to read the other parts. Execute Task 1, then
Task 2, in order.

**Spec:** `.opencode/plans/2026-10-01-htr-shared-daemon-spec.md`, sections 1 and 6.
Ancestor spec for the existing subsystem: `docs/superpowers/specs/2026-09-09-embedded-htr-extension-design.md`.

## Constraints that apply to every task in this part

- htrcli's config is parsed as **JSON only**. Missing, unparseable or tokenless → AdoptOnly, never a silent default.
- Never write, rewrite or remove `com.htrcontrol.host`.
- Runtime state under `paths.GlobalDataDir()`; logs via the package logger or `paths.LogsDir()`.
- These are `ocodeconfig.json` keys — no `.env.example` entry.
- No `exec.Command`, no spawn, no port probe in this part. This part is pure resolution.

## Symbols

**Existing** (verified 2026-10-01): `config.BrowserConfig`, `config.DefaultBrowserConfig`, `config.validHTRNativeHostName` in `internal/config/ocodeconfig.go`; `resolveManagedHTROptions` in `internal/server/htr.go`; `cdp.HTROptions`, `cdp.NormalizeHTRPort`, `cdp.DefaultHTRPort` in `internal/browse/cdp/htr.go`.

**New in this part:** `cdp.SharedDaemon`, `cdp.ResolveSharedDaemon`, `cdp.htrcliConfig`, `cdp.LoadHTRcliConfig`, `config.BrowserConfig.HTRShared`, `config.BrowserConfig.HTRToken`.

---

### Task 1: Config fields and the shared-daemon resolver

**Files:**
- Modify: `internal/config/ocodeconfig.go` (`BrowserConfig`, `DefaultBrowserConfig`)
- Modify: `internal/config/ocodeconfig_htr_test.go`
- Create: `internal/browse/cdp/htr_shared.go`
- Create: `internal/browse/cdp/htr_shared_test.go`

**Interfaces:**
- Consumes: `config.BrowserConfig` (existing).
- Produces:
  ```go
  // SharedDaemon is the resolved description of the single htrcli daemon ocode
  // will ensure. Zero Mode means private (legacy) mode.
  type SharedDaemon struct {
      Mode        string // "shared" or "private"
      AdoptOnly   bool   // true when ocode may probe but must not spawn
      Port        int
      Socket      string
      Token       string
      Binary      string
      ConfigPath  string
      TokenSource string // "ocode-config", "htrcli-config", or "none"
      Notice      string // user-facing reason; empty when nothing is wrong
  }

  // HTRSharedInput carries the ocode-side inputs to resolution. Zero value is
  // valid: it reads the htrcli config from the user's home.
  type HTRSharedInput struct {
      Token         string // browser.htr_token override
      ConfigPath    string // "" = <home>/.htrcli/config.json
      Shared        bool   // browser.htr_shared
      LegacyPort    int    // browser.htr_port — private mode only
      LegacySocket  string // browser.htr_socket_path — private mode only
      Home          string // "" = os.UserHomeDir()
      Goos          string // "" = runtime.GOOS
  }

  func ResolveSharedDaemon(in HTRSharedInput) SharedDaemon
  ```

- [ ] **Step 1: Write the failing test**

Create `internal/browse/cdp/htr_shared_test.go`:

```go
package cdp

import (
	"os"
	"path/filepath"
	"runtime"
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
			name: "full config uses server port and token",
			write: `{"server":"http://127.0.0.1:3845","token":"htr_secret","htrcli_path":"/usr/local/bin/htrcli"}`,
			in:   HTRSharedInput{Shared: true, Home: home, Goos: "darwin"},
			wantMode: "shared", wantPort: 3845,
			wantSock: filepath.Join(home, ".htrcli", "daemon.sock"),
			wantTok:  "htr_secret", wantSrc: "htrcli-config",
		},
		{
			name: "missing config falls back to default port and adopt-only",
			in:   HTRSharedInput{Shared: true, Home: home, Goos: "darwin"},
			wantMode: "shared", wantAdopt: true, wantPort: 3845,
			wantSock: filepath.Join(home, ".htrcli", "daemon.sock"),
			wantTok:  "", wantSrc: "none",
		},
		{
			name: "non-JSON config is adopt-only, never a silent default",
			write: "server = \"http://127.0.0.1:3845\"\ntoken = \"x\"\n",
			in:   HTRSharedInput{Shared: true, Home: home, Goos: "darwin"},
			wantMode: "shared", wantAdopt: true, wantPort: 3845, wantSrc: "none",
		},
		{
			name: "tokenless config is adopt-only",
			write: `{"server":"http://127.0.0.1:3845"}`,
			in:   HTRSharedInput{Shared: true, Home: home, Goos: "darwin"},
			wantMode: "shared", wantAdopt: true, wantPort: 3845, wantSrc: "none",
		},
		{
			name: "ocode token override wins over the file",
			write: `{"server":"http://127.0.0.1:3845","token":"from_file"}`,
			in:   HTRSharedInput{Shared: true, Token: "from_ocode", Home: home, Goos: "darwin"},
			wantMode: "shared", wantPort: 3845, wantTok: "from_ocode", wantSrc: "ocode-config",
		},
		{
			name: "non-loopback server is adopt-only and does not adopt its port",
			write: `{"server":"http://10.0.0.5:9999","token":"t"}`,
			in:   HTRSharedInput{Shared: true, Home: home, Goos: "darwin"},
			wantMode: "shared", wantAdopt: true, wantPort: 3845, wantTok: "t", wantSrc: "htrcli-config",
		},
		{
			name: "shared mode ignores the legacy 3846 port",
			write: `{"server":"http://127.0.0.1:3845","token":"t"}`,
			in:   HTRSharedInput{Shared: true, LegacyPort: 3846, Home: home, Goos: "darwin"},
			wantMode: "shared", wantPort: 3845, wantTok: "t", wantSrc: "htrcli-config",
		},
		{
			name: "windows uses a loopback endpoint, not a unix socket",
			write: `{"server":"http://127.0.0.1:3845","token":"t"}`,
			in:   HTRSharedInput{Shared: true, Home: home, Goos: "windows"},
			wantMode: "shared", wantPort: 3845, wantSock: "127.0.0.1:3847",
			wantTok: "t", wantSrc: "htrcli-config",
		},
		{
			name: "private mode keeps the legacy port",
			in:   HTRSharedInput{Shared: false, LegacyPort: 3846, Home: home, Goos: "darwin"},
			wantMode: "private", wantPort: 3846,
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/browse/cdp/ -run TestResolveSharedDaemon -v`
Expected: FAIL to compile — `undefined: HTRSharedInput`, `undefined: ResolveSharedDaemon`.

- [ ] **Step 3: Add the config fields**

In `internal/config/ocodeconfig.go`, extend `BrowserConfig` with two fields and default `HTRShared` to true:

```go
type BrowserConfig struct {
	// existing fields unchanged
	HTRShared         bool   `json:"htr_shared"`
	HTRToken          string `json:"htr_token"`
}
```

`DefaultBrowserConfig` returns `defaultOcodeConfig().Browser`, so set `HTRShared: true` in the canonical default. `HTRToken` defaults to empty, meaning "read the htrcli config".

- [ ] **Step 4: Write the resolver**

Create `internal/browse/cdp/htr_shared.go`:

```go
package cdp

// Shared-daemon resolution. One htrcli serve serves both ocode's embedded
// browser and the user's own browser extension, so its coordinates come from
// htrcli's own config (~/.htrcli/config.json) rather than an ocode-invented
// identity. JSON only: a TOML/YAML config lands in AdoptOnly with a notice,
// because viper accepts formats this resolver deliberately does not.

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// DefaultHTRCLIPort is htrcli's own default, used when the config's server URL
// carries no usable loopback port.
const DefaultHTRCLIPort = 3845

// htrcliConfig is the subset of htrcli's config ocode needs. Unknown keys are
// ignored, which keeps this stable across htrcli releases.
type htrcliConfig struct {
	Server  string `json:"server"`
	Token   string `json:"token"`
	CliPath string `json:"htrcli_path"`
}

type SharedDaemon struct {
	Mode        string
	AdoptOnly   bool
	Port        int
	Socket      string
	Token       string
	Binary      string
	ConfigPath  string
	TokenSource string
	Notice      string
}

// LoadHTRcliConfig reads htrcli's config as JSON. A missing file is not an
// error: it yields the zero value so the caller can fall back to AdoptOnly.
func LoadHTRcliConfig(path string) (htrcliConfig, error) {
	var cfg htrcliConfig
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s as JSON: %w", path, err)
	}
	return cfg, nil
}

// ResolveSharedDaemon maps config to the one daemon ocode will ensure.
func ResolveSharedDaemon(in HTRSharedInput) SharedDaemon {
	goos := in.Goos
	if goos == "" {
		goos = runtime.GOOS
	}
	home := in.Home
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = h
		}
	}

	if !in.Shared {
		// Private (legacy) mode: today's managed daemon, unchanged.
		return SharedDaemon{
			Mode:        "private",
			Port:        in.LegacyPort,
			Socket:      in.LegacySocket,
			TokenSource: "generated",
		}
	}

	cfgPath := in.ConfigPath
	if cfgPath == "" {
		cfgPath = filepath.Join(home, ".htrcli", "config.json")
	}

	d := SharedDaemon{Mode: "shared", ConfigPath: cfgPath, Port: DefaultHTRCLIPort}
	d.Socket = sharedSocketPath(home, goos)

	cfg, err := LoadHTRcliConfig(cfgPath)
	switch {
	case err != nil:
		d.AdoptOnly = true
		d.Notice = fmt.Sprintf("Could not read %s (%v). Start `htrcli serve` yourself, or set browser.htr_token.", cfgPath, err)
	case in.Token != "":
		d.Token = in.Token
		d.TokenSource = "ocode-config"
	case strings.TrimSpace(cfg.Token) == "":
		d.AdoptOnly = true
		d.Notice = fmt.Sprintf("No token in %s. Start `htrcli serve` yourself, or set browser.htr_token.", cfgPath)
	default:
		d.Token = cfg.Token
		d.TokenSource = "htrcli-config"
	}

	if p, ok := loopbackPort(cfg.Server); ok {
		d.Port = p
	} else if strings.TrimSpace(cfg.Server) != "" {
		d.AdoptOnly = true
		if d.Notice == "" {
			d.Notice = fmt.Sprintf("htrcli server %q is not a loopback address; ocode will not start a daemon it cannot reach locally.", cfg.Server)
		}
	}

	d.Binary = strings.TrimSpace(cfg.CliPath)
	return d
}

func sharedSocketPath(home, goos string) string {
	if goos == "windows" {
		return "127.0.0.1:3847"
	}
	return filepath.Join(home, ".htrcli", "daemon.sock")
}

// loopbackPort extracts the port from a loopback server URL. A non-loopback or
// unparseable URL yields ok=false so the caller keeps the default port.
func loopbackPort(server string) (int, bool) {
	server = strings.TrimSpace(server)
	if server == "" {
		return 0, false
	}
	u, err := url.Parse(server)
	if err != nil || u.Host == "" {
		return 0, false
	}
	host := u.Hostname()
	if host == "localhost" {
		host = "127.0.0.1"
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return 0, false
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 && n <= 65535 {
			return n, true
		}
		return 0, false
	}
	if u.Scheme == "https" {
		return 443, true
	}
	return 80, true
}
```

`CliPath` is the only path key htrcli's viper config uses; there is deliberately
no nested options struct here, so a future htrcli key needs one flat field.

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/browse/cdp/ -run TestResolveSharedDaemon -v`
Expected: PASS, all nine subtests.

- [ ] **Step 6: Run the config test and the full package**

Run: `go test ./internal/config/ ./internal/browse/cdp/`
Expected: PASS. `TestValidHTRNativeHostName` and the native-host regression tests must be unaffected.

- [ ] **Step 7: Commit**

```bash
git add internal/config/ocodeconfig.go internal/config/ocodeconfig_htr_test.go \
        internal/browse/cdp/htr_shared.go internal/browse/cdp/htr_shared_test.go
git commit -m "feat(htr): resolve one shared daemon from htrcli's own config"
```

---

### Task 2: Thread the resolver through option building, with real notices

**Files:**
- Modify: `internal/server/htr.go` (`resolveManagedHTROptions`)
- Modify: `internal/server/handler_config.go` (`htrBrowserConfig`, `HandleGetBrowserConfig`)
- Test: `internal/server/handler_config_test.go`, `internal/server/htr_shared_options_test.go` (create)

**Interfaces:**
- Consumes: `cdp.ResolveSharedDaemon`, `cdp.HTRSharedInput`, `cdp.SharedDaemon` (Task 1); `cdp.HTROptions` (existing).
- Produces: `resolveManagedHTROptions` now fills `HTROptions.Shared`; `cdp.HTROptions` gains `Shared SharedDaemon`. `HandleGetBrowserConfig` response gains `htr_shared`, `htr_token_set`, `effective_port`, `effective_socket`, `token_source`, `adopt_only`, `config_path`.

- [ ] **Step 1: Write the failing test**

Create `internal/server/htr_shared_options_test.go`:

```go
package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

func TestSharedModeAdoptsHTRcliConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgDir := filepath.Join(home, ".htrcli")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"),
		[]byte(`{"server":"http://127.0.0.1:3845","token":"tok"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	opts, _ := resolveManagedHTROptions(config.BrowserConfig{
		HTREnabled: true,
		HTRShared:  true,
		HTRPort:    3846, // legacy value that shared mode must ignore
	})
	if !opts.Enabled {
		t.Skip("host browser gate disabled HTR on this machine")
	}
	if opts.Shared.Mode != "shared" {
		t.Fatalf("mode = %q, want shared", opts.Shared.Mode)
	}
	if opts.Port != 3845 {
		t.Errorf("port = %d, want 3845 (the htrcli config port, not the legacy 3846)", opts.Port)
	}
	if opts.Shared.AdoptOnly {
		t.Error("a readable config with a token must not be AdoptOnly")
	}
	if want := filepath.Join(home, ".htrcli", "daemon.sock"); opts.SocketPath != want {
		t.Errorf("socket = %q, want %q", opts.SocketPath, want)
	}
}

func TestSharedModeMissingConfigIsAdoptOnlyWithNamedPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	opts, _ := resolveManagedHTROptions(config.BrowserConfig{
		HTREnabled: true,
		HTRShared:  true,
	})
	if !opts.Enabled {
		t.Skip("host browser gate disabled HTR on this machine")
	}
	if !opts.Shared.AdoptOnly {
		t.Fatal("a missing htrcli config must produce AdoptOnly")
	}
	want := filepath.Join(home, ".htrcli", "config.json")
	if !strings.Contains(opts.Shared.Notice, want) {
		t.Errorf("notice %q must name the config path %q", opts.Shared.Notice, want)
	}
}
```

Both tests skip (never fail) when the machine's browser gate turns HTR off, so
the suite stays green on hosts without Chromium. Resolution itself is pinned
without any host dependency by `TestResolveSharedDaemon` in Task 1.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/server/ -run 'TestSharedMode' -v`
Expected: FAIL to compile — `opts.Shared undefined` (no `Shared` field on the
resolved options yet).

- [ ] **Step 3: Populate `HTROptions.Shared`**

In `internal/browse/cdp/htr.go`, add a `Shared SharedDaemon` field to `HTROptions`. In `internal/server/htr.go`, inside `resolveManagedHTROptions`, after the `!htr.Enabled` early return, resolve:

```go
	htr.Shared = cdp.ResolveSharedDaemon(cdp.HTRSharedInput{
		Token:        browser.HTRToken,
		Shared:       browser.HTRShared,
		LegacyPort:   browser.HTRPort,
		LegacySocket: browser.HTRSocketPath,
	})
	if htr.Shared.Mode == "shared" {
		htr.Port = htr.Shared.Port
		htr.SocketPath = htr.Shared.Socket
	}
```

When `htr.Shared.Mode == "private"`, keep the existing `3846`/managed-socket path untouched so the rollback really is byte-for-byte today's behaviour.

- [ ] **Step 4: Extend `HandleGetBrowserConfig`**

Add the provenance fields to the JSON body: `htr_shared`, `htr_token_set` (bool, never the token itself), `effective_port`, `effective_socket`, `token_source`, `adopt_only`, `config_path`. Never emit the token value over the wire beyond what `htr_token_set` implies.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/server/ -run 'Browser|HTR' -v`
Expected: PASS, including the pre-existing `TestHandleGetBrowserConfig`-style tests.

- [ ] **Step 6: Commit**

```bash
git add internal/browse/cdp/htr.go internal/server/htr.go internal/server/handler_config.go \
        internal/server/htr_shared_options_test.go
git commit -m "feat(htr): thread shared-daemon resolution into options and the settings API"
```
