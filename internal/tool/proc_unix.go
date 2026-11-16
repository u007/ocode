//go:build !windows

package tool

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
)

// setProcGroup places the command in its own session (and, as a consequence,
// its own process group headed by its own PID) so the whole group can be
// signalled together via -pid.
//
// Setsid rather than plain Setpgid: these children's stdio is always piped
// or redirected, never the real controlling terminal, but Setpgid alone
// leaves them in the SAME session as ocode — any child (or a grandchild,
// e.g. a spawned local-model server) can still open("/dev/tty") and get a
// live handle on the real terminal. A local-model server child was
// confirmed doing exactly that and issuing TIOCSPGRP against it, which
// knocked ocode's own process group out of the terminal's foreground and
// suspended/corrupted the whole shell session (SIGTTOU/SIGTTIN, "error on
// TTY read: Input/output error") — confirmed root cause via
// internal/tui/tty_foreground_unix.go's debugTTYState instrumentation:
// tty_fg_pgrp drifted away from ocode's own pgrp during local-model
// auto-start with plain Setpgid, and no longer drifts at all with Setsid.
// Setsid makes the child a new session leader with no controlling terminal
// at all, so open("/dev/tty") fails outright and this class of hijack is
// structurally impossible rather than something ocode has to detect and
// recover from.
func setProcGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// killProcessGroup force-kills the process group rooted at proc.
func killProcessGroup(proc *os.Process) error {
	return syscall.Kill(-proc.Pid, syscall.SIGKILL)
}

// freezeProcessGroup stops (SIGSTOP) every process in proc's group, so a runaway
// command makes no progress while liveGroupProcesses lists it. No SIGCONT is
// needed afterwards: SIGKILL ends stopped processes.
func freezeProcessGroup(proc *os.Process) error {
	return syscall.Kill(-proc.Pid, syscall.SIGSTOP)
}

// terminateProcess sends SIGTERM to the process, or to its whole group
// when it owns one.
func terminateProcess(proc *os.Process, ownsGroup bool) error {
	if ownsGroup {
		return syscall.Kill(-proc.Pid, syscall.SIGTERM)
	}
	return proc.Signal(syscall.SIGTERM)
}

// forceKillProcess sends SIGKILL to the process, or to its whole group
// when it owns one.
func forceKillProcess(proc *os.Process, ownsGroup bool) error {
	if ownsGroup {
		return syscall.Kill(-proc.Pid, syscall.SIGKILL)
	}
	return proc.Kill()
}

// liveGroupProcesses lists the processes still alive in process group pgid, one
// "- pid N, ET elapsed: command" line each. It runs on its own deadline because
// the caller's ctx is already done. A ps failure is logged (never printed: the
// TUI owns the terminal) and yields nil.
func liveGroupProcesses(pgid int) []string {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var out bytes.Buffer
	// -ww: without it ps clips the args column when stdout is not a terminal.
	// args= (not command=) is the column name procps, BSD/macOS and busybox ps
	// all accept.
	cmd := exec.CommandContext(ctx, "ps", "-axww", "-o", "pid=,ppid=,pgid=,etime=,args=")
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		log.Printf("bash: listing processes of stopped group %d (ps -ax) failed: %v", pgid, err)
		return nil
	}
	return parseLiveGroupProcesses(out.String(), pgid)
}

const (
	liveProcessListCap  = 8
	liveProcessCmdRunes = 200
	// parentMonitorShell is the argv prefix of every shell ParentMonitorWrap
	// starts ("( ppid=N; ..."): the wrapper's leader, its monitor, and any user
	// subshell inside the wrapper all carry it.
	parentMonitorShell = "( ppid="
)

// monitorPollSleeps are the monitor's poll sleeps, spelled exactly as
// ParentMonitorWrap spells them ("0.50", not "0.5", so a user's own sleep 0.5 is
// never mistaken for one). TestParentMonitorWrap_PollSleepSpellings keeps this
// list and the wrapper in step.
var monitorPollSleeps = []string{"sleep 0.50", "sleep 0.20"}

// parseLiveGroupProcesses formats the rows of `ps -axww -o pid=,ppid=,pgid=,etime=,args=`
// that belong to group pgid. Membership is decided by parentage; command text
// only confirms the wrapper shape and the poll-sleep spelling:
//   - the group leader (pid == pgid) is dropped;
//   - the monitor is the wrapper-shaped process whose parent is the leader.
//     ParentMonitorWrap runs the monitor as a subshell of the leader, and no user
//     process is a direct child of the leader, so this is the monitor;
//   - a poll sleep is a monitorPollSleeps command whose parent is the monitor.
//
// Every other row is listed, including a user subshell with the wrapper argv
// (its parent is the monitor, not the leader) and a user's own sleep. Sorted by
// pid, capped with an "and N more" line.
//
// Limit: the wrapper is identified by its leader-child position. A runner that
// interposes a process above the leader (a sandbox binary that forks rather than
// execs) moves the wrapped shell one level down, so the list then names that
// shell as the monitor and keeps the real monitor and its poll sleeps.
func parseLiveGroupProcesses(psOut string, pgid int) []string {
	type psRow struct {
		pid, ppid      int
		etime, command string
	}
	var rows []psRow
	for _, line := range strings.Split(psOut, "\n") {
		pid, ppid, gid, etime, command, ok := splitPSRow(line)
		if !ok || gid != pgid || pid == pgid {
			continue
		}
		rows = append(rows, psRow{pid: pid, ppid: ppid, etime: etime, command: command})
	}

	monitors := make(map[int]bool)
	for _, r := range rows {
		if r.ppid == pgid && strings.Contains(r.command, parentMonitorShell) {
			monitors[r.pid] = true
		}
	}

	var procs []psRow
	for _, r := range rows {
		if monitors[r.pid] || (monitors[r.ppid] && slices.Contains(monitorPollSleeps, r.command)) {
			continue
		}
		procs = append(procs, r)
	}
	sort.Slice(procs, func(i, j int) bool { return procs[i].pid < procs[j].pid })

	var lines []string
	for i, p := range procs {
		if i == liveProcessListCap {
			lines = append(lines, fmt.Sprintf("- and %d more", len(procs)-liveProcessListCap))
			break
		}
		lines = append(lines, fmt.Sprintf("- pid %d, %s elapsed: %s", p.pid, p.etime, clipRunes(p.command, liveProcessCmdRunes)))
	}
	return lines
}

// splitPSRow parses one "pid ppid pgid etime args" row; args is the rest of the
// line and may itself contain spaces. ok is false for blank or garbled rows.
func splitPSRow(line string) (pid, ppid, pgid int, etime, command string, ok bool) {
	rest := strings.TrimSpace(line)
	var head [4]string
	for i := range head {
		end := strings.IndexFunc(rest, unicode.IsSpace)
		if end < 0 {
			return 0, 0, 0, "", "", false
		}
		head[i] = rest[:end]
		rest = strings.TrimLeftFunc(rest[end:], unicode.IsSpace)
	}
	pid, errPid := strconv.Atoi(head[0])
	ppid, errPpid := strconv.Atoi(head[1])
	pgid, errPgid := strconv.Atoi(head[2])
	if errPid != nil || errPpid != nil || errPgid != nil || rest == "" {
		return 0, 0, 0, "", "", false
	}
	return pid, ppid, pgid, head[3], rest, true
}

// clipRunes keeps the first n runes of s and marks the cut with an ellipsis.
func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
