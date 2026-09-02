package remote

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// StageStatus is the state of one connect stage.
type StageStatus int

const (
	StagePending StageStatus = iota
	StageRunning
	StageDone
	StageFailed
	// StageWarned is StageDone with a non-fatal warning attached (e.g. no
	// multiplexer found — connect continues, resume just isn't available).
	StageWarned
)

// Stage is one row of the connect progress display.
type Stage struct {
	Name    string // stable id, e.g. "reachable"
	Label   string // human label, e.g. "ssh reachable"
	Status  StageStatus
	Detail  string // e.g. "linux/arm64", "14.2 MB"
	Warning string // set only when Status == StageWarned
	Err     error  // set only when Status == StageFailed; underlying stderr lives in Err's text
	Hint    string // one actionable hint, shown alongside Err
}

// Progress renders staged connect progress to an io.Writer: TTY gets
// spinner + checkmarks updated in place, non-TTY gets one plain line per
// stage — per the "Progress & error reporting" contract in
// 01-architecture.md. Safe for sequential use from one goroutine (the
// connect flow is inherently sequential, one stage at a time).
type Progress struct {
	mu     sync.Mutex
	out    io.Writer
	tty    bool
	stages []*Stage
	spin   *spinner
}

func NewProgress(out io.Writer, header string) *Progress {
	tty := isTerminalWriter(out)
	p := &Progress{out: out, tty: tty}
	if header != "" {
		fmt.Fprintln(out, header)
	}
	return p
}

var spinnerFrames = []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}

type spinner struct {
	frame int
}

func (s *spinner) next() rune {
	r := spinnerFrames[s.frame%len(spinnerFrames)]
	s.frame++
	return r
}

// Start begins a new stage, rendering it as running.
func (p *Progress) Start(name, label string) *Stage {
	p.mu.Lock()
	defer p.mu.Unlock()
	st := &Stage{Name: name, Label: label, Status: StageRunning}
	p.stages = append(p.stages, st)
	p.renderLocked(st)
	return st
}

// Done marks the current (last-started) stage as successfully completed,
// with an optional detail string (e.g. "linux/arm64").
func (p *Progress) Done(detail string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	st := p.lastLocked()
	if st == nil {
		return
	}
	st.Status = StageDone
	st.Detail = detail
	p.renderLocked(st)
}

// Warn marks the current stage as done-with-warning: connect continues, but
// the warning is shown and never silently dropped.
func (p *Progress) Warn(warning string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	st := p.lastLocked()
	if st == nil {
		return
	}
	st.Status = StageWarned
	st.Warning = warning
	p.renderLocked(st)
}

// Fail marks the current stage as failed. err's text (expected to already
// contain the verbatim underlying ssh/scp/go-build stderr) and hint are
// rendered together; no stage output is ever swallowed.
func (p *Progress) Fail(err error, hint string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	st := p.lastLocked()
	if st == nil {
		return
	}
	st.Status = StageFailed
	st.Err = err
	st.Hint = hint
	p.renderLocked(st)
}

func (p *Progress) lastLocked() *Stage {
	if len(p.stages) == 0 {
		return nil
	}
	return p.stages[len(p.stages)-1]
}

func (p *Progress) renderLocked(st *Stage) {
	if p.tty {
		p.renderTTYLocked(st)
		return
	}
	p.renderPlainLocked(st)
}

func (p *Progress) renderPlainLocked(st *Stage) {
	switch st.Status {
	case StageRunning:
		fmt.Fprintf(p.out, "  %s…\n", st.Label)
	case StageDone:
		if st.Detail != "" {
			fmt.Fprintf(p.out, "  OK %s (%s)\n", st.Label, st.Detail)
		} else {
			fmt.Fprintf(p.out, "  OK %s\n", st.Label)
		}
	case StageWarned:
		fmt.Fprintf(p.out, "  WARN %s: %s\n", st.Label, st.Warning)
	case StageFailed:
		fmt.Fprintf(p.out, "  FAIL %s: %v\n", st.Label, st.Err)
		if st.Hint != "" {
			fmt.Fprintf(p.out, "       hint: %s\n", st.Hint)
		}
	}
}

// renderTTYLocked redraws the just-updated stage in place. It intentionally
// does not attempt full-screen spinner animation (no timer goroutine): each
// call comes from a real state transition (Start/Done/Warn/Fail), which is
// exactly when the line needs to change. A stage in StageRunning prints a
// spinner glyph the first time and does not re-render on a timer — a
// long-running stage (e.g. cross-compiling) is expected to call Start once
// and Done/Fail once, not stream intermediate frames.
func (p *Progress) renderTTYLocked(st *Stage) {
	glyph := "?"
	switch st.Status {
	case StageRunning:
		if p.spin == nil {
			p.spin = &spinner{}
		}
		glyph = string(p.spin.next())
	case StageDone:
		glyph = "✓"
	case StageWarned:
		glyph = "!"
	case StageFailed:
		glyph = "✗"
	}

	line := fmt.Sprintf("  %s %s", glyph, st.Label)
	switch st.Status {
	case StageDone:
		if st.Detail != "" {
			line += fmt.Sprintf(" (%s)", st.Detail)
		}
	case StageWarned:
		line += ": " + st.Warning
	case StageFailed:
		line += fmt.Sprintf(": %v", st.Err)
	}
	// \r + clear-to-EOL, then the line; a newline is added once the stage
	// leaves the running state, so a later Start begins its own row rather
	// than overwriting a finished one.
	fmt.Fprint(p.out, "\r\x1b[2K"+line)
	if st.Status != StageRunning {
		fmt.Fprint(p.out, "\n")
		if st.Status == StageFailed && st.Hint != "" {
			fmt.Fprintf(p.out, "     hint: %s\n", st.Hint)
		}
	}
}

func isTerminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// namedError formats a connect-stage failure the way every stage in the
// spec's error contract is required to: the failing stage's own words plus
// the underlying stderr verbatim, never paraphrased.
func namedError(stage, verb string, stderr string, cause error) error {
	msg := fmt.Sprintf("%s: %s failed", stage, verb)
	if cause != nil {
		msg += ": " + cause.Error()
	}
	if s := strings.TrimSpace(stderr); s != "" {
		msg += "\n" + s
	}
	return fmt.Errorf("%s", msg)
}
