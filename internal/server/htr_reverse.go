package server

import (
	"fmt"
	"log"
	"sync/atomic"

	"github.com/u007/ocode/internal/browse/cdp"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/projects"
	"github.com/u007/ocode/internal/remote"
)

// localHTRPort reports the port of the local htrcli daemon when HTR is enabled
// and that daemon is answering. ok is false when HTR is off or nothing is
// listening; that is the normal "nothing to expose" case, not an error.
func localHTRPort(browser config.BrowserConfig) (port int, ok bool) {
	if !browser.HTREnabled {
		return 0, false
	}
	shared := cdp.ResolveSharedDaemon(cdp.HTRSharedInput{
		Token:        browser.HTRToken,
		Shared:       browser.HTRShared,
		LegacyPort:   browser.HTRPort,
		LegacySocket: browser.HTRSocketPath,
	})
	socket := browser.HTRSocketPath
	port = browser.HTRPort
	token := browser.HTRToken
	if shared.Mode == "shared" {
		socket = shared.Socket
		port = shared.Port
		// The resolved token may come from htrcli's own config rather than
		// browser.htr_token; probing with the empty config value makes a healthy
		// shared daemon read as down.
		token = shared.Token
	}
	info := cdp.HTRDaemonStatus(port, socket, token)
	return info.Port, info.Running
}

// htrReverseForwardFn is the shape of the reverse-forward opener.
type htrReverseForwardFn func(fm *remote.ForwardManager, persisted []projects.PortMap) error

// startHTRReverseForward is the seam the tests replace, so a test can observe
// whether the reverse forward was even ASKED for — which the real
// implementation cannot show when the local htrcli daemon is not running (it
// is then a no-op for reasons unrelated to the caller).
//
// It is an atomic pointer, not a plain var: the opener runs on a BACKGROUND
// goroutine (the auto-start), and a test that installs and restores the seam
// would otherwise race with that goroutine's read.
var startHTRReverseForward atomic.Pointer[htrReverseForwardFn]

func init() {
	fn := htrReverseForwardFn(StartHTRReverseForward)
	startHTRReverseForward.Store(&fn)
}

// htrReverseForwardOpener returns the currently installed opener.
func htrReverseForwardOpener() htrReverseForwardFn {
	if fn := startHTRReverseForward.Load(); fn != nil {
		return *fn
	}
	return StartHTRReverseForward
}

// StartHTRReverseForward opens `ssh -R <port>:127.0.0.1:<port>` so htrcli on an
// SSH remote reaches the local machine's htrcli daemon (and therefore the
// local browser). It is a no-op when HTR is disabled, the daemon is not
// running, or the user already has a persisted forward on that remote port
// (their entry wins). The forward is ephemeral: it is re-evaluated on every
// remote autostart and is not written to projects.json.
func StartHTRReverseForward(fm *remote.ForwardManager, persisted []projects.PortMap) error {
	cfg, err := config.LoadOcodeConfigCopy()
	if err != nil {
		return fmt.Errorf("load config for htr reverse forward: %w", err)
	}
	port, ok := localHTRPort(cfg.Browser)
	if !ok {
		return nil
	}
	for _, pm := range persisted {
		if pm.RemotePort == port {
			return nil
		}
	}
	if err := fm.Start(remote.ProjectPortMap{RemotePort: port, LocalPort: port, Enabled: true, Reverse: true}); err != nil {
		return fmt.Errorf("htr reverse forward on remote port %d: %w", port, err)
	}
	log.Printf("htr: remote %d -> local %d reverse forward open", port, port)
	return nil
}
