package remote

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrNoStaleForward reports that the remote port is not held by a dead SSH
// reverse forward: nothing listens, the listener cannot be attributed to a
// logind session, or that session is provably in use. Callers treat it as
// "nothing to reap", never as a transport failure.
var ErrNoStaleForward = errors.New("remote port is not held by a stale reverse forward")

// StaleForward is a reverse-forward listener left on the remote by an SSH
// session whose client is gone. Terminating PIDs releases the port.
type StaleForward struct {
	Session string // logind session id, e.g. "63"
	Peer    string // the vanished client's address
	PIDs    []int  // the session's non-root sshd processes
}

// staleForwardPolls and staleForwardInterval bound how long ReapStaleReverseForward
// waits for the port to free after SIGTERM. The overall deadline is the caller's
// context.
const (
	staleForwardPolls    = 10
	staleForwardInterval = 300 * time.Millisecond
)

// FindStaleReverseForward reports the dead SSH session holding port on the
// remote, or ErrNoStaleForward when the port is not provably stale.
//
// An `ssh -R` listener is owned by sshd's privilege-separated monitor, which
// runs as root, so FindPortHolder cannot see it (ss and lsof both hide the
// pid). The listening socket still carries its logind cgroup, which is what
// makes attribution possible without root.
//
// Every step must be proven before a pid is reported:
//
//   - the listener's logind session is attributable from `ss -ltnpe`;
//   - that session runs NOTHING but sshd, so no shell is in use;
//   - the session's client is the peer of an established :22 connection that is
//     retransmitting unacked data, and that peer has no healthy connection.
//
// A healthy peer is refused even when its session is forward-only: a live
// `ssh -N` forward is a legitimate mapping, not a stale one.
func FindStaleReverseForward(ctx context.Context, tr BoundedTransport, port int) (*StaleForward, error) {
	res, err := tr.ExecContext(ctx, "ss -ltnpe 2>/dev/null || true")
	if err != nil {
		return nil, fmt.Errorf("list listeners on remote: %w", err)
	}
	session, err := parseListenerSession(res.Stdout, port)
	if err != nil {
		return nil, err
	}

	res, err = tr.ExecContext(ctx, sessionProcsCommand(session))
	if err != nil {
		return nil, fmt.Errorf("list processes of remote session %s: %w", session, err)
	}
	pids, err := sessionSSHPIDs(res.Stdout)
	if err != nil {
		return nil, fmt.Errorf("session %s: %w", session, err)
	}

	res, err = tr.ExecContext(ctx, "loginctl show-session "+session+" -p RemoteHost 2>/dev/null || true")
	if err != nil {
		return nil, fmt.Errorf("read peer of remote session %s: %w", session, err)
	}
	peer, err := parseSessionRemoteHost(res.Stdout)
	if err != nil {
		return nil, fmt.Errorf("session %s: %w", session, err)
	}

	res, err = tr.ExecContext(ctx, "ss -tnoe state established 2>/dev/null || true")
	if err != nil {
		return nil, fmt.Errorf("list ssh connections on remote: %w", err)
	}
	if err := checkPeerDead(res.Stdout, peer); err != nil {
		return nil, fmt.Errorf("session %s peer %s: %w", session, peer, err)
	}

	return &StaleForward{Session: session, Peer: peer, PIDs: pids}, nil
}

// ReapStaleReverseForward terminates the dead session holding port and waits
// for the port to free. It reports ErrNoStaleForward and signals nothing when
// FindStaleReverseForward refuses.
//
// SIGTERM only: the target is the user's own sshd session process, which exits
// immediately in practice; one that ignores it is reported, never -9'd.
func ReapStaleReverseForward(ctx context.Context, tr BoundedTransport, port int) (*StaleForward, error) {
	st, err := FindStaleReverseForward(ctx, tr, port)
	if err != nil {
		return nil, err
	}
	if _, err := tr.ExecContext(ctx, staleForwardKillCommand(st)+" 2>/dev/null || true"); err != nil {
		return nil, fmt.Errorf("terminate stale forward session %s: %w", st.Session, err)
	}
	for i := 0; i < staleForwardPolls; i++ {
		free, err := remotePortFree(ctx, tr, port)
		if err != nil {
			return nil, err
		}
		if free {
			return st, nil
		}
		if err := sleepCtx(ctx, staleForwardInterval); err != nil {
			return nil, fmt.Errorf("remote port %d not released by session %s: %w", port, st.Session, err)
		}
	}
	return nil, fmt.Errorf("remote port %d still bound after terminating session %s", port, st.Session)
}

// sessionProcsCommand lists every process in the logind session as
// "pid|uid|cmdline". unprivileged: /proc/<pid>/cgroup is world-readable.
func sessionProcsCommand(session string) string {
	return `for p in /proc/[0-9]*; do case "$(cat "$p/cgroup" 2>/dev/null)" in *session-` +
		session + `.scope*) echo "${p#/proc/}|$(awk '/^Uid:/{print $2}' "$p/status" 2>/dev/null)|$(tr '\0' ' ' < "$p/cmdline" 2>/dev/null)";; esac; done`
}

// staleForwardKillCommand builds ONE remote command that re-checks the session
// cgroup immediately before signalling. The check and the signal are therefore
// a single round trip: a pid recycled since enumeration belongs to another
// session, so it is skipped rather than killed. A missing or unreadable cgroup
// matches nothing and fails closed. There is no SIGKILL path, so the grace a
// TERM-then-KILL sequence would open does not exist here.
func staleForwardKillCommand(st *StaleForward) string {
	return `for p in ` + pidList(st.PIDs) + `; do case "$(cat /proc/$p/cgroup 2>/dev/null)" in *session-` +
		st.Session + `.scope*) kill -TERM "$p";; esac; done`
}

// parseListenerSession returns the logind session holding port from `ss -ltnpe`.
// Both address families report the same cgroup; a disagreement is refused.
func parseListenerSession(out string, port int) (string, error) {
	suffix := ":" + strconv.Itoa(port)
	session := ""
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || f[0] != "LISTEN" || !strings.HasSuffix(f[3], suffix) {
			continue
		}
		got := sessionFromCgroup(f)
		if got == "" {
			return "", fmt.Errorf("%w: port %d listener carries no session cgroup", ErrNoStaleForward, port)
		}
		if session != "" && got != session {
			return "", fmt.Errorf("%w: port %d listeners disagree on session (%s, %s)", ErrNoStaleForward, port, session, got)
		}
		session = got
	}
	if session == "" {
		return "", fmt.Errorf("%w: nothing listens on port %d", ErrNoStaleForward, port)
	}
	return session, nil
}

// sessionFromCgroup extracts N from a `cgroup:/…/session-N.scope` field. The id
// must be digits: it is interpolated into a shell command, and remote output is
// not a trusted source for shell syntax.
func sessionFromCgroup(fields []string) string {
	for _, f := range fields {
		cg, ok := strings.CutPrefix(f, "cgroup:")
		if !ok {
			continue
		}
		i := strings.LastIndex(cg, "/session-")
		if i < 0 {
			continue
		}
		rest := cg[i+len("/session-"):]
		if j := strings.Index(rest, ".scope"); j > 0 {
			if id := rest[:j]; allDigits(id) {
				return id
			}
		}
	}
	return ""
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// sessionSSHPIDs returns the session's non-root sshd processes. It refuses when
// anything other than sshd runs in the session: a shell there means the session
// is in use, and killing it would end someone's work.
func sessionSSHPIDs(out string) ([]int, error) {
	var pids []int
	seen := false
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		pidStr, rest, ok := strings.Cut(line, "|")
		if !ok {
			continue
		}
		uidStr, cmd, _ := strings.Cut(rest, "|")
		pid, err := strconv.Atoi(strings.TrimSpace(pidStr))
		if err != nil || pid <= 1 {
			continue
		}
		seen = true
		cmd = strings.TrimSpace(cmd)
		if !strings.HasPrefix(cmd, "sshd:") {
			return nil, fmt.Errorf("%w: session runs %q, so it is in use", ErrNoStaleForward, cmd)
		}
		uid, _ := strconv.Atoi(strings.TrimSpace(uidStr))
		if uid == 0 || strings.Contains(cmd, "[priv]") {
			continue
		}
		pids = append(pids, pid)
	}
	if !seen {
		return nil, fmt.Errorf("%w: session has no processes", ErrNoStaleForward)
	}
	if len(pids) == 0 {
		return nil, fmt.Errorf("%w: session has no user sshd process", ErrNoStaleForward)
	}
	return pids, nil
}

// parseSessionRemoteHost reads RemoteHost from `loginctl show-session … -p RemoteHost`.
func parseSessionRemoteHost(out string) (string, error) {
	for _, line := range strings.Split(out, "\n") {
		host, ok := strings.CutPrefix(strings.TrimSpace(line), "RemoteHost=")
		if !ok {
			continue
		}
		if host = strings.TrimSpace(host); host != "" {
			return host, nil
		}
	}
	return "", fmt.Errorf("%w: session has no remote host", ErrNoStaleForward)
}

// checkPeerDead proves the peer is gone: at least one established :22
// connection from it holds unacked data on a running retransmit timer, and no
// :22 connection from it is healthy. A single healthy connection means the
// client is alive (a reconnect after a stalled one), so nothing is reaped.
func checkPeerDead(ssOut, peer string) error {
	dead, healthy := 0, 0
	for _, line := range strings.Split(ssOut, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || !isNumeric(f[0]) {
			continue
		}
		if !strings.HasSuffix(f[2], ":22") {
			continue
		}
		if addrHost(f[3]) != peer {
			continue
		}
		sendQ, _ := strconv.Atoi(f[1])
		parts := strings.Join(f[4:], " ")
		if sendQ > 0 && strings.Contains(parts, "timer:(on,") {
			dead++
			continue
		}
		healthy++
	}
	if healthy > 0 {
		return fmt.Errorf("%w: peer has a healthy connection", ErrNoStaleForward)
	}
	if dead == 0 {
		return fmt.Errorf("%w: peer has no retransmitting connection", ErrNoStaleForward)
	}
	return nil
}

// remotePortFree reports whether nothing listens on port. An empty `ss` answer
// is an error, not "free": a host where the listing is unavailable must not
// read as a released port.
func remotePortFree(ctx context.Context, tr BoundedTransport, port int) (bool, error) {
	res, err := tr.ExecContext(ctx, "ss -ltn 2>/dev/null || true")
	if err != nil {
		return false, fmt.Errorf("list listeners on remote: %w", err)
	}
	if strings.TrimSpace(res.Stdout) == "" {
		return false, errors.New("remote listener listing is unavailable")
	}
	suffix := ":" + strconv.Itoa(port)
	for _, line := range strings.Split(res.Stdout, "\n") {
		f := strings.Fields(line)
		if len(f) >= 4 && f[0] == "LISTEN" && strings.HasSuffix(f[3], suffix) {
			return false, nil
		}
	}
	return true, nil
}

// addrHost strips the port from an `ss` address, handling bracketed IPv6.
func addrHost(addr string) string {
	if strings.HasPrefix(addr, "[") {
		if i := strings.LastIndex(addr, "]"); i > 0 {
			return addr[1:i]
		}
	}
	if i := strings.LastIndex(addr, ":"); i > 0 {
		return addr[:i]
	}
	return addr
}

func isNumeric(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}
