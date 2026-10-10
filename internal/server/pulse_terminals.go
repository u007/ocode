package server

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

// pulseBoardMaxTerminals caps the running-terminal lines in the per-turn board.
const pulseBoardMaxTerminals = 20

// pulseTerminalRow is one live terminal on the Pulse dashboard and on the
// assistant's board: the terminal's open tab (for its title) joined with the
// live shell process the registry tracks for it.
type pulseTerminalRow struct {
	ID      string `json:"id"`
	Project string `json:"project"`
	Title   string `json:"title"`
	PID     int32  `json:"pid"`
	// Command is the program running in the terminal, or the bare shell name
	// when the terminal is idle.
	Command string `json:"command"`
	// Running is true when a program other than the shell is in the foreground.
	Running bool `json:"running"`
}

// pulseRowsMemo is the last walk behind pulseTerminalRows. The walk reads the
// whole process table. The dashboard poll (one per open viewer, every
// terminalProcsPollInterval), every assistant turn and terminal_tabs all call
// it, so callers inside one interval share a single walk. A terminal opened or
// closed bumps the registry generation, so the memo never serves a stale set
// of terminals.
type pulseRowsMemo struct {
	mu    sync.Mutex
	valid bool
	gen   uint64
	at    time.Time
	rows  []pulseTerminalRow
}

// pulseTerminalRows lists every live terminal, running programs first, then by
// project and id. The result is shared between callers, so it is read-only.
// It is memoized for terminalProcsPollInterval (see pulseRowsMemo), so a
// command that just started can show as idle for up to that long.
func (h *Handler) pulseTerminalRows() []pulseTerminalRow {
	// The command line of a terminal can carry secrets (a --token flag, a
	// connection string), so it is listed only when the same access gate that
	// guards the terminal endpoints passes. The board sends these rows to the
	// model provider, so the gate must hold here, not only at the HTTP layer.
	// It runs before the memo, so a denied caller never sees cached rows.
	if !h.terminalAccessAllowed() {
		return nil
	}
	m := &h.terminalProcs.pulseRows
	m.mu.Lock()
	defer m.mu.Unlock()
	// Read the generation before the walk: a terminal registered mid-walk then
	// leaves the memo tagged with an older generation, and the next call walks again.
	gen := h.terminalProcs.generation()
	if m.valid && m.gen == gen && time.Since(m.at) < terminalProcsPollInterval {
		return m.rows
	}
	at := time.Now()
	rows := h.walkPulseTerminalRows()
	m.valid, m.gen, m.at, m.rows = true, gen, at, rows
	return rows
}

// walkPulseTerminalRows does the process-table walk behind pulseTerminalRows.
// It takes no CPU sample (that needs the emitter's long-lived cache).
func (h *Handler) walkPulseTerminalRows() []pulseTerminalRow {
	entries := h.terminalProcs.snapshot()
	titles := make(map[string]string)
	if h.termTabsStore != nil {
		for _, pt := range h.termTabsStore.All() {
			for _, t := range pt.Terminals {
				titles[t.ID] = t.Title
			}
		}
	}
	childPids := processChildrenSnapshot()
	cache := make(map[int32]*process.Process)
	rows := make([]pulseTerminalRow, 0, len(entries))
	for id, e := range entries {
		cmd, running := terminalForeground(e.PID, cache, childPids)
		rows = append(rows, pulseTerminalRow{
			ID:      id,
			Project: e.Project,
			Title:   titles[id],
			PID:     e.PID,
			Command: cmd,
			Running: running,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Running != rows[j].Running {
			return rows[i].Running
		}
		if rows[i].Project != rows[j].Project {
			return rows[i].Project < rows[j].Project
		}
		return rows[i].ID < rows[j].ID
	})
	return rows
}

// renderPulseTerminals is the board block for running terminals. Idle terminals
// are left out: the assistant needs to know what is running, not every shell.
func renderPulseTerminals(rows []pulseTerminalRow) string {
	running := make([]pulseTerminalRow, 0, len(rows))
	for _, r := range rows {
		if r.Running {
			running = append(running, r)
		}
	}
	if len(running) == 0 {
		return "No programs running in terminals."
	}
	shown := running
	if len(shown) > pulseBoardMaxTerminals {
		shown = shown[:pulseBoardMaxTerminals]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Running terminals (%d of %d shown): terminal_id | project | title | command", len(shown), len(running))
	for _, r := range shown {
		b.WriteByte('\n')
		fmt.Fprintf(&b, "%s | %s | %s | %s",
			r.ID,
			filepath.Base(r.Project),
			pulseOneLine(r.Title, pulseBoardTitleRunes),
			pulseOneLine(r.Command, pulseBoardTaskRunes))
	}
	return b.String()
}
