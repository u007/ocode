package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/u007/ocode/internal/paths"
)

// crashLogName is the file under <GlobalDataDir>/logs that receives the
// process's stderr for the lifetime of a TUI run. Go runtime panics, fatal
// errors, crashguard traces, and the exit reason all land there, so a TUI
// that dies with the terminal still in raw/mouse mode leaves evidence on
// disk instead of scrolling off an alt-screen that no longer exists.
const crashLogName = "tui-crash.log"

// installCrashLog redirects fd 2 into the crash log and returns a logger
// that appends one timestamped line per call, plus the log's path ("" when
// stderr stayed on the terminal). When the log cannot be opened, stderr is
// left on the terminal and the returned logger writes there, so
// instrumentation never blocks a run.
func installCrashLog() (func(format string, args ...interface{}), string) {
	dataDir, err := paths.GlobalDataDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ocode: crash log disabled (data dir: %v)\n", err)
		return stderrLogf, ""
	}
	f, err := openCrashLog(filepath.Join(dataDir, "logs"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "ocode: crash log disabled: %v\n", err)
		return stderrLogf, ""
	}
	if err := redirectStderr(f); err != nil {
		fmt.Fprintf(os.Stderr, "ocode: crash log disabled (redirect stderr: %v)\n", err)
		_ = f.Close()
		return stderrLogf, ""
	}
	return func(format string, args ...interface{}) {
		fmt.Fprintf(f, time.Now().Format(time.RFC3339)+" "+format+"\n", args...)
	}, f.Name()
}

// openCrashLog creates dir if needed and opens the crash log for append.
func openCrashLog(dir string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(dir, crashLogName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
}

func stderrLogf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, time.Now().Format(time.RFC3339)+" "+format+"\n", args...)
}

// ttyStateLine renders the job-control facts that distinguish "ocode died"
// from "ocode lost the terminal": its own process group versus the group the
// controlling terminal currently treats as foreground.
func ttyStateLine() string {
	self, fg, err := ttyForegroundPgrp()
	if err != nil {
		return fmt.Sprintf("pgrp=%d tty_fg=? (%v)", self, err)
	}
	return fmt.Sprintf("pgrp=%d tty_fg=%d", self, fg)
}
