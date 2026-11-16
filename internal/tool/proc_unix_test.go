//go:build !windows

package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestParseLiveGroupProcesses covers the ps-row parser that backs
// liveGroupProcesses. Membership is by parentage: the leader and the monitor (the
// wrapper-shaped child of the leader) are dropped, and only the monitor's own
// poll sleeps are dropped, never a sleep elsewhere in the group. Also: the group
// filter, pid sort, the 8-entry cap, command clipping, and blank or garbled rows.
func TestParseLiveGroupProcesses(t *testing.T) {
	long := strings.Repeat("x", 250)
	var many []string
	for pid := 1999; pid >= 1990; pid-- { // ten rows, emitted in reverse pid order
		many = append(many, fmt.Sprintf("%d 1 500 00:01 worker %d", pid, pid))
	}
	// The argv a shell carries when it runs the wrapped line: the leader, the
	// monitor and any user subshell inside the wrapper all share this prefix.
	shellArgv := "bash -c " + ParentMonitorWrap("(sleep 30) & wait", 4242)
	wantSubshell := "- pid 503, 00:01 elapsed: " + clipRunes(shellArgv, liveProcessCmdRunes)

	tests := []struct {
		name string
		ps   string
		pgid int
		want []string
	}{
		{
			name: "filters by pgid and drops the leader and the monitor",
			ps: "500 1 500 01:00 bash -c ( ppid=1; ( sleep 30 ) & ...\n" +
				"501 500 500 00:59 bash -c ( ppid=1; ( sleep 30 ) & ...\n" +
				"502 500 500 00:59 sleep 30\n" +
				"503 1 999 00:59 sleep 99\n",
			pgid: 500,
			want: []string{"- pid 502, 00:59 elapsed: sleep 30"},
		},
		{
			name: "drops the monitor's poll sleeps, not other processes",
			ps: "500 1 500 00:01 " + shellArgv + "\n" +
				"501 500 500 00:01 " + shellArgv + "\n" +
				"502 501 500 00:01 sleep 0.50\n" +
				"503 501 500 00:01 sleep 0.20\n" +
				"504 501 500 00:01 lsof -ti:8099\n",
			pgid: 500,
			want: []string{"- pid 504, 00:01 elapsed: lsof -ti:8099"},
		},
		{
			name: "keeps a user subshell with the wrapper argv, clipped to 200 runes",
			ps: "500 1 500 00:01 " + shellArgv + "\n" +
				"501 500 500 00:01 " + shellArgv + "\n" +
				"503 501 500 00:01 " + shellArgv + "\n",
			pgid: 500,
			want: []string{wantSubshell},
		},
		{
			name: "keeps user sleeps; drops only a poll sleep whose parent is the monitor",
			ps: "500 1 500 00:01 " + shellArgv + "\n" +
				"501 500 500 00:01 " + shellArgv + "\n" +
				"502 501 500 00:01 sleep 0.5\n" + // user sleep 0.5 directly under the monitor
				"503 501 500 00:01 " + shellArgv + "\n" + // user subshell under the monitor
				"504 503 500 00:01 sleep 0.5\n" + // user sleep 0.5 under the user subshell
				"505 501 500 00:01 sleep 0.50\n" + // the monitor's poll sleep
				"506 503 500 00:01 sleep 0.50\n", // same spelling, but not the monitor's child
			pgid: 500,
			want: []string{
				"- pid 502, 00:01 elapsed: sleep 0.5",
				wantSubshell,
				"- pid 504, 00:01 elapsed: sleep 0.5",
				"- pid 506, 00:01 elapsed: sleep 0.50",
			},
		},
		{
			name: "sorts by pid",
			ps:   "9 1 1 00:01 c\n3 1 1 00:01 a\n7 1 1 00:01 b\n",
			pgid: 1,
			want: []string{"- pid 3, 00:01 elapsed: a", "- pid 7, 00:01 elapsed: b", "- pid 9, 00:01 elapsed: c"},
		},
		{
			name: "caps at eight and counts the rest",
			ps:   strings.Join(many, "\n"),
			pgid: 500,
			want: []string{
				"- pid 1990, 00:01 elapsed: worker 1990",
				"- pid 1991, 00:01 elapsed: worker 1991",
				"- pid 1992, 00:01 elapsed: worker 1992",
				"- pid 1993, 00:01 elapsed: worker 1993",
				"- pid 1994, 00:01 elapsed: worker 1994",
				"- pid 1995, 00:01 elapsed: worker 1995",
				"- pid 1996, 00:01 elapsed: worker 1996",
				"- pid 1997, 00:01 elapsed: worker 1997",
				"- and 2 more",
			},
		},
		{
			name: "truncates long commands to 200 runes",
			ps:   "42 1 7 00:01 " + long + "\n",
			pgid: 7,
			want: []string{"- pid 42, 00:01 elapsed: " + strings.Repeat("x", 200) + "…"},
		},
		{
			name: "skips blank and garbled rows",
			ps:   "\n   \nnot a row\n12 1 abc 00:01 cmd\n13 1 7\n14 1 7 00:01\n15 1 7 00:01 ok\n",
			pgid: 7,
			want: []string{"- pid 15, 00:01 elapsed: ok"},
		},
		{
			name: "no rows yields nil",
			ps:   "",
			pgid: 7,
			want: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseLiveGroupProcesses(tc.ps, tc.pgid)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseLiveGroupProcesses:\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}

// TestParentMonitorWrap_PollSleepSpellings keeps the wrapper and the lister in
// step: every "sleep N" the wrapper runs must be a monitorPollSleeps entry, and
// every entry must be run by the wrapper. Otherwise a poll sleep leaks into the
// list, or a user sleep is dropped.
func TestParentMonitorWrap_PollSleepSpellings(t *testing.T) {
	wrapped := ParentMonitorWrap("true", 4242)
	seen := map[string]bool{}
	for _, s := range regexp.MustCompile(`sleep [0-9.]+`).FindAllString(wrapped, -1) {
		seen[s] = true
		if !slices.Contains(monitorPollSleeps, s) {
			t.Errorf("wrapper runs %q but monitorPollSleeps does not list it", s)
		}
	}
	for _, s := range monitorPollSleeps {
		if !seen[s] {
			t.Errorf("monitorPollSleeps lists %q but the wrapper never runs it", s)
		}
	}
}

// runCancelledBash runs command through the foreground bash tool and cancels it
// after 300 ms, so the stop path (freeze, list, kill) is the one that returns.
func runCancelledBash(t *testing.T, command string) string {
	t.Helper()
	reg := NewProcessRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(300*time.Millisecond, cancel)
	args, _ := json.Marshal(map[string]interface{}{"command": command})
	out, err := (BashTool{Procs: reg}).ExecuteStreamCtx(ctx, args, nil)
	if err != nil {
		t.Fatalf("ExecuteStreamCtx returned error: %v", err)
	}
	return out
}

// stillRunningLines returns the entries after the "Still running when stopped:"
// header of a stop result, or nil when there is no such section.
func stillRunningLines(out string) []string {
	const header = "Still running when stopped:\n"
	i := strings.Index(out, header)
	if i < 0 {
		return nil
	}
	return strings.Split(strings.TrimSpace(out[i+len(header):]), "\n")
}

// TestBashTool_ForegroundCancelListsUserSubshellOnly runs a user subshell in a
// real wrapped command. The list must hold exactly the user's subshell and its
// sleep: the monitor and its poll sleeps are the wrapper's own processes.
func TestBashTool_ForegroundCancelListsUserSubshellOnly(t *testing.T) {
	out := runCancelledBash(t, "(sleep 30) & wait")
	if !strings.Contains(out, "was stopped after") {
		t.Fatalf("cancel result missing stop text: %q", out)
	}
	list := stillRunningLines(out)
	if len(list) != 2 {
		t.Fatalf("want exactly the user subshell and sleep 30 listed, got %d: %q", len(list), list)
	}
	var sawSleep, sawSubshell bool
	for _, l := range list {
		switch {
		case strings.HasSuffix(l, ": sleep 30"):
			sawSleep = true
		case strings.Contains(l, ": bash -c ( ppid="):
			sawSubshell = true
		default:
			t.Fatalf("unexpected listed process (monitor or poll sleep leaked?): %q", l)
		}
	}
	if !sawSleep || !sawSubshell {
		t.Fatalf("want both the subshell and sleep 30 listed, got %q", list)
	}
}

// TestBashTool_ForegroundCancelLeavesNoProcessGroup checks the freeze does not
// leave stopped processes behind. The command prints its process group id ($$
// is the top-level shell, which Setsid makes the group leader), so the test can
// probe the group after the call returns.
func TestBashTool_ForegroundCancelLeavesNoProcessGroup(t *testing.T) {
	out := runCancelledBash(t, "echo PGID=$$; sleep 30")
	m := regexp.MustCompile(`PGID=(\d+)`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("cancel result has no PGID marker: %q", out)
	}
	pgid, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("bad PGID %q: %v", m[1], err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err := syscall.Kill(-pgid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("process group %d still has live (possibly stopped) members 2s after the cancelled call returned", pgid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
