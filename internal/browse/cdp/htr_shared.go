package cdp

// Shared-daemon resolution. One htrcli serve serves both ocode's embedded
// browser and the user's own browser extension, so its coordinates come from
// htrcli's own config (~/.htrcli/config.json) rather than an ocode-invented
// identity. JSON only: a TOML/YAML config lands in AdoptOnly with a notice,
// because viper accepts formats this resolver deliberately does not.

import (
	"encoding/json"
	"errors"
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

// errHTRCLiConfigMissing distinguishes "there is no config file" from "the
// config file is there but unreadable or unparseable". Without it both land in
// the same branch and the notice points the user at a file that does not exist.
var errHTRCLiConfigMissing = errors.New("no htrcli config")

// htrcliConfig is the subset of htrcli's config ocode needs. Unknown keys are
// ignored, which keeps this stable across htrcli releases.
type htrcliConfig struct {
	Server  string `json:"server"`
	Token   string `json:"token"`
	CliPath string `json:"htrcli_path"`
}

// SharedDaemon is the resolved description of the single htrcli daemon ocode
// will ensure. Zero Mode means private (legacy) mode.
type SharedDaemon struct {
	Mode       string `json:"mode"`
	AdoptOnly  bool   `json:"adopt_only"`
	Port       int    `json:"port"`
	Socket     string `json:"socket"`
	Token      string `json:"-"` // live bearer credential: never serialised
	Binary     string `json:"binary"`
	ConfigPath string `json:"config_path"`
	// TokenSource is "ocode-config" (browser.htr_token), "htrcli-config"
	// (htrcli's own token), "none" (no token available — AdoptOnly), or
	// "generated" (private mode, where ocode mints a per-launch identity).
	TokenSource string `json:"token_source"`
	Notice      string `json:"notice"`
}

// loadHTRcliConfig reads htrcli's config as JSON. A missing file is not a
// failure: it yields the zero value plus errHTRCLiConfigMissing, so the caller
// can name the real reason it fell back to AdoptOnly.
func loadHTRcliConfig(path string) (htrcliConfig, error) {
	var cfg htrcliConfig
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, fmt.Errorf("%w at %s", errHTRCLiConfigMissing, path)
		}
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s as JSON: %w", path, err)
	}
	return cfg, nil
}

// HTRSharedInput carries the ocode-side inputs to resolution. Zero value is
// valid: it reads the htrcli config from the user's home.
type HTRSharedInput struct {
	Token        string // browser.htr_token override
	ConfigPath   string // "" = <home>/.htrcli/config.json
	Shared       bool   // browser.htr_shared
	LegacyPort   int    // browser.htr_port — private mode only
	LegacySocket string // browser.htr_socket_path — private mode only
	Home         string // "" = os.UserHomeDir()
	Goos         string // "" = runtime.GOOS
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
		// Private (legacy) mode: today's managed daemon, unchanged. htrcli's
		// config is deliberately never consulted — a leftover config must not
		// move the private port or supply a token.
		return SharedDaemon{
			Mode:        "private",
			Port:        in.LegacyPort,
			Socket:      in.LegacySocket,
			TokenSource: "generated",
		}
	}

	if home == "" {
		// os.UserHomeDir failed. Never fall through: filepath.Join("", ".htrcli",
		// "config.json") is the RELATIVE path ".htrcli/config.json", which a
		// Finder/Dock-launched desktop app (cwd "/") would read as
		// "/.htrcli/config.json". An explicit ConfigPath does not bypass this:
		// shared mode also derives its unix socket path from home.
		//
		// This notice deliberately names "~/.htrcli/config.json" instead of
		// cfgPath — there is no config path here, and printing the relative one
		// is the bug this guard exists to prevent. Do not fold it into a later
		// "every notice names cfgPath" sweep.
		return SharedDaemon{
			Mode:        "shared",
			AdoptOnly:   true,
			Port:        DefaultHTRCLIPort,
			TokenSource: "none",
			Notice: "Could not determine the home directory, so htrcli's config " +
				"(~/.htrcli/config.json) cannot be read. Start `htrcli serve` yourself. " +
				"Setting browser.htr_token does not help here: a daemon ocode spawned " +
				"would resolve no token at all, because ocode never passes one.",
		}
	}

	cfgPath := in.ConfigPath
	if cfgPath == "" {
		cfgPath = filepath.Join(home, ".htrcli", "config.json")
	}

	// TokenSource starts at "none": shared mode only earns a real source from a
	// token it actually found, so an absent token stays visibly tokenless rather
	// than silently implying ocode may start a daemon.
	d := SharedDaemon{Mode: "shared", ConfigPath: cfgPath, Port: DefaultHTRCLIPort, TokenSource: "none"}
	d.Socket = sharedSocketPath(home, goos)

	cfg, err := loadHTRcliConfig(cfgPath)
	missing := errors.Is(err, errHTRCLiConfigMissing)
	switch {
	case err != nil:
		// A config that cannot be read — missing, unreadable, or not JSON —
		// forces AdoptOnly even when browser.htr_token is set, so this branch
		// is consulted FIRST. ocode deliberately never passes HTR_BEARER_TOKEN
		// to a daemon it spawns: htrcli resolves its own token from its own
		// config, so with no readable config a spawned daemon comes up with no
		// token at all and every authenticated probe 401s. ocode would believe
		// it may spawn, spawn, and then never see the daemon become healthy.
		//
		// browser.htr_token overrides the token VALUE, not the presence of the
		// config, so neither notice here may offer it as the fix — the file is
		// what htrcli itself needs.
		d.AdoptOnly = true
		if missing {
			d.Notice = fmt.Sprintf("No htrcli config at %s. Start `htrcli serve` yourself; ocode stays adopt-only until that file exists.", cfgPath)
		} else {
			d.Notice = fmt.Sprintf("Could not read %s (%v). Start `htrcli serve` yourself; ocode stays adopt-only until that file is readable JSON.", cfgPath, err)
		}
	case in.Token != "":
		// browser.htr_token WINS over a token the config also carries. This is
		// the specified precedence (spec: "token | browser.htr_token if set,
		// else token in htrcli's config"), not a fallback for the tokenless
		// case, and it is deliberate: ocode spawns a shared daemon with
		// HTR_MANAGED_ID set to this very token (EnsureHTRServe), so the value
		// that must match the daemon ocode starts is this one.
		//
		// The consequence to be aware of: with a stale browser.htr_token set,
		// ocode probes an ADOPTED user-run daemon with a bearer it does not
		// hold, every authenticated probe 401s, and a later ensure can find the
		// port busy and unable to authenticate. Clearing browser.htr_token is
		// the fix; do not "fix" it by reversing this precedence.
		d.Token = in.Token
		d.TokenSource = "ocode-config"
	case strings.TrimSpace(cfg.Token) == "":
		d.AdoptOnly = true
		d.Notice = fmt.Sprintf("No token in %s. Start `htrcli serve` yourself, or set browser.htr_token.", cfgPath)
	default:
		d.Token = cfg.Token
		d.TokenSource = "htrcli-config"
	}

	if p, isLoopback := loopbackEndpoint(cfg.Server); isLoopback {
		if p > 0 {
			d.Port = p
		}
		// A loopback URL with no explicit port keeps DefaultHTRCLIPort.
	} else if strings.TrimSpace(cfg.Server) != "" {
		// loopbackEndpoint also answers false for a URL ocode cannot parse into
		// a host+port ("127.0.0.1:3845" has no scheme, "localhost:3845" parses as
		// a scheme, an out-of-range port is rejected), so the notice speaks about
		// reachability rather than claiming the address is not loopback.
		//
		// It is only set when no notice already fired. The only reachable co-fire
		// is a readable but tokenless config whose server is also non-loopback:
		// a config that could not be read at all leaves cfg.Server empty, so it
		// never reaches here. Notices are never accumulated into one string and
		// never overwritten — the first reason wins, and the token is the first
		// thing to fix.
		d.AdoptOnly = true
		if d.Notice == "" {
			d.Notice = fmt.Sprintf("htrcli server %q in %s is not a loopback URL ocode can reach locally (expected something like http://127.0.0.1:3845).", cfg.Server, cfgPath)
		}
	}

	d.Binary = strings.TrimSpace(cfg.CliPath)
	return d
}

func sharedSocketPath(home, goos string) string {
	if goos == "windows" {
		return htrWindowsEndpoint
	}
	return filepath.Join(home, ".htrcli", "daemon.sock")
}

// loopbackEndpoint reports whether server is a loopback URL and, when it is,
// the port it names explicitly. A loopback URL with no explicit port returns
// port 0: htrcli has no 80/443 default, so inventing one would point ocode at
// a port nothing listens on — the caller keeps DefaultHTRCLIPort instead.
//
// Host matching is on the parsed hostname, never a prefix: "127.0.0.1.evil.com"
// parses to that registrable name and is NOT loopback.
func loopbackEndpoint(server string) (port int, isLoopback bool) {
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
	p := u.Port()
	if p == "" {
		return 0, true
	}
	n, err := strconv.Atoi(p)
	if err != nil || n <= 0 || n > 65535 {
		return 0, false
	}
	return n, true
}
