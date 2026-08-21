package tool

import (
	"fmt"
	"os"
)

// ParentMonitorWrap returns cmdline wrapped in a shell monitor that hard-attaches
// the spawned server to parentPID. If parentPID exits for any reason — including
// SIGKILL / crash where ProcessSupervisor.Shutdown never runs — the monitor
// kills the child and exits. The monitor itself is the process tracked by the
// supervisor (bash -c "<wrapped>"), so a graceful Shutdown (SIGTERM to the
// process group) also terminates the child via both the group's signal and the
// monitor's trap.
//
// Polling via kill -0 is portable to darwin/linux and requires no prctl
// (PR_SET_PDEATHSIG is Linux-only and unavailable on macOS). The ppid is
// captured at spawn time (os.Getpid of the ocode process), so after reparent
// to init (PPID 1) the monitor still probes the original parent pid, not 1.
func ParentMonitorWrap(cmdline string, parentPID int) string {
	if parentPID <= 1 {
		parentPID = os.Getpid()
	}
	// The child command runs in a subshell "( cmdline )" so redirections and
	// env assignments in cmdline (e.g. HF_HUB_OFFLINE=1 python3 -m mlx_lm.server ...)
	// behave exactly as before. The monitor loop polls both parent and child:
	//  - if parent dies → kill child and exit
	//  - if child dies first → propagate its exit code
	// The trap forwards supervisor Shutdown's SIGTERM/SIGHUP to the child.
	return fmt.Sprintf("( ppid=%d; ( %s ) & child=$!; trap 'kill $child 2>/dev/null; sleep 0.2; kill -9 $child 2>/dev/null; wait $child 2>/dev/null; exit 0' TERM INT HUP; while kill -0 $ppid 2>/dev/null; do if ! kill -0 $child 2>/dev/null; then wait $child; exit $?; fi; sleep 0.5; done; kill $child 2>/dev/null; sleep 0.5; kill -9 $child 2>/dev/null; wait $child 2>/dev/null; exit 0 )", parentPID, cmdline)
}

// WrapWithParentMonitor is ParentMonitorWrap with parentPID = current process.
func WrapWithParentMonitor(cmdline string) string {
	return ParentMonitorWrap(cmdline, os.Getpid())
}
