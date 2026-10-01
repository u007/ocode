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

// SharedDaemon is the resolved description of the single htrcli daemon ocode
// will ensure. Zero Mode means private (legacy) mode.
type SharedDaemon struct {
	Mode       string
	AdoptOnly  bool
	Port       int
	Socket     string
	Token      string
	Binary     string
	ConfigPath string
	// TokenSource is "ocode-config" (browser.htr_token), "htrcli-config"
	// (htrcli's own token), "none" (no token available — AdoptOnly), or
	// "generated" (private mode, where ocode mints a per-launch identity).
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

	// TokenSource starts at "none": shared mode only earns a real source from a
	// token it actually found, so an absent token stays visibly tokenless rather
	// than silently implying ocode may start a daemon.
	d := SharedDaemon{Mode: "shared", ConfigPath: cfgPath, Port: DefaultHTRCLIPort, TokenSource: "none"}
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
