//go:build !windows

package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// errTimeout and errShellDied are internal sentinels: waitMarker returns them
// so run can tell "no marker yet" from a real transport failure.
var (
	errTimeout   = errors.New("shell command timed out")
	errShellDied = errors.New("shell session exited")
)

// Session is one persistent, pty-backed interactive shell. Commands are framed
// with a per-session marker emitted by the shell's own prompt hook, so each
// Run reports the real exit status and cwd of the command it submitted, and
// shell state (cwd, exported vars, functions) carries across Runs.
//
// A Session is safe for concurrent use: Run serialises on runMu (one command at
// a time), while Close may be called from any goroutine and interrupts an
// in-flight command rather than waiting for it.
type Session struct {
	opts           SessionOptions
	nonce          string
	marker         string
	ready          string
	delim          string
	timeout        time.Duration
	grace          time.Duration
	startupTimeout time.Duration

	runMu sync.Mutex // serialises Run and all transport mutation

	transportMu sync.Mutex
	ptmx        *os.File
	cmd         *exec.Cmd

	outMu  sync.Mutex
	out    bytes.Buffer
	notify chan struct{}
	dead   chan struct{}
	gen    atomic.Int64

	aliveFlag atomic.Bool
	closed    atomic.Bool
	closeOnce sync.Once
	closeCh   chan struct{}
}

// NewSession spawns a persistent interactive shell and blocks until its
// prelude (echo/history/prompt suppression, hook reset, marker installer) has
// run and been drained. A non-nil error means no usable session exists — the
// caller should fall back to a one-shot shellpkg.Run.
func NewSession(opts SessionOptions) (*Session, error) {
	nonce, err := newNonce()
	if err != nil {
		return nil, err
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	grace := opts.Grace
	if grace <= 0 {
		grace = defaultGrace
	}
	// The startup handshake sources the user's rc, which can be slow cold;
	// never let a deliberately short per-command timeout abort startup.
	startup := 30 * time.Second
	if timeout > startup {
		startup = timeout
	}
	s := &Session{
		opts:           opts,
		nonce:          nonce,
		marker:         doneMarker(nonce),
		ready:          readyToken(nonce),
		delim:          commandDelimiter(nonce),
		timeout:        timeout,
		grace:          grace,
		startupTimeout: startup,
		closeCh:        make(chan struct{}),
	}
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if err := s.spawn(); err != nil {
		// Close releases anything spawn managed to create before it failed;
		// the factory error is what the caller needs, so log the teardown
		// failure rather than masking the original.
		if cerr := s.Close(); cerr != nil {
			log.Printf("shell: teardown after failed spawn: %v", cerr)
		}
		return nil, err
	}
	return s, nil
}

// Run executes command in the persistent shell and returns its combined output,
// exit code, and the shell's cwd afterwards. A command containing the
// session's heredoc delimiter is rejected before anything is written.
func (s *Session) Run(ctx context.Context, command string) (Result, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.closed.Load() {
		return Result{ExitCode: 1, Err: ErrClosed}, "", ErrClosed
	}
	if !s.aliveFlag.Load() {
		log.Printf("shell: session shell is not alive; respawning before running %q", command)
		if err := s.spawn(); err != nil {
			return Result{ExitCode: 1, Err: err}, "", err
		}
	}
	return s.run(ctx, command, s.timeout)
}

// Close tears the shell down. It is idempotent and never blocks on an
// in-flight Run: closing the pty master hangs up the foreground process group,
// which makes the blocked read (and therefore Run) return.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		s.aliveFlag.Store(false)
		close(s.closeCh)
		s.killGroup()
	})
	return nil
}

// Alive reports whether the shell process is still running and the session has
// not been closed.
func (s *Session) Alive() bool {
	return !s.closed.Load() && s.aliveFlag.Load()
}

// spawn starts a fresh shell on a new pty, installs the prelude, and drains the
// synchronizing sentinel. Callers must hold runMu.
func (s *Session) spawn() error {
	shellPath := Resolve(s.opts.Shell)
	cmd := exec.Command(shellPath, spawnArgs(shellPath)...)
	cmd.Dir = s.opts.Dir
	cmd.Env = s.sessionEnv()

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("start shell %s: %w", shellPath, err)
	}
	cols, rows := s.opts.Cols, s.opts.Rows
	if cols == 0 {
		cols = 400
	}
	if rows == 0 {
		rows = 50
	}
	if err := pty.Setsize(ptmx, &pty.Winsize{Cols: cols, Rows: rows}); err != nil {
		log.Printf("shell: set pty size %dx%d: %v", cols, rows, err)
	}

	// Fresh framing state. The generation counter retires the previous
	// readLoop: a stale loop can never append to or signal the new buffers.
	gen := s.gen.Add(1)
	notify := make(chan struct{}, 1)
	dead := make(chan struct{})
	s.notify = notify
	s.dead = dead

	s.transportMu.Lock()
	s.ptmx = ptmx
	s.cmd = cmd
	s.transportMu.Unlock()

	s.outMu.Lock()
	s.out.Reset()
	s.outMu.Unlock()

	s.aliveFlag.Store(true)
	go s.readLoop(ptmx, gen, notify, dead)
	go func() { _ = cmd.Wait() }()

	if _, err := io.WriteString(ptmx, s.prelude()); err != nil {
		s.teardown()
		return fmt.Errorf("write shell prelude: %w", err)
	}
	// Synchronizing sentinel. It prints a unique token, and the handshake waits
	// for the marker that follows it — NOT merely "the next marker". The prelude
	// ends by installing the prompt hook, so the shell has already emitted a
	// marker for the prompt after that install, and any of the prelude's own
	// prompts can race the sentinel's. Matching the first marker instead of the
	// one after the token desynchronises every later Run by one command.
	sentinel, err := frameCommand(startupCommand(s.nonce), s.delim)
	if err != nil {
		s.teardown()
		return fmt.Errorf("frame startup sentinel: %w", err)
	}
	if _, err := io.WriteString(ptmx, sentinel); err != nil {
		s.teardown()
		return fmt.Errorf("write startup sentinel: %w", err)
	}
	if err := s.waitStartup(context.Background(), s.startupTimeout); err != nil {
		s.teardown()
		return fmt.Errorf("shell prelude handshake: %w", err)
	}
	// Everything before the sentinel's marker (rc output, prelude echoes, the
	// prelude's own markers) is now consumed; start the first Run from a clean
	// buffer.
	s.outMu.Lock()
	s.out.Reset()
	s.outMu.Unlock()
	return nil
}

// waitStartup blocks until the startup sentinel's ready token has been followed
// by a prompt-hook marker, proving the prelude ran and the hook is installed.
// Callers must hold runMu.
func (s *Session) waitStartup(ctx context.Context, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		s.outMu.Lock()
		data := s.out.String()
		s.outMu.Unlock()
		if idx := strings.Index(data, s.ready); idx >= 0 {
			if _, _, _, ok := findMarker(data[idx+len(s.ready):], s.marker); ok {
				return nil
			}
		}
		select {
		case <-s.notify:
		case <-timer.C:
			return fmt.Errorf("startup sentinel not answered within %s", timeout)
		case <-ctx.Done():
			return ctx.Err()
		case <-s.dead:
			return errShellDied
		case <-s.closeCh:
			return ErrClosed
		}
	}
}

// run submits one command and waits for its marker. Callers must hold runMu.
func (s *Session) run(ctx context.Context, command string, timeout time.Duration) (Result, string, error) {
	framed, err := frameCommand(command, s.delim)
	if err != nil {
		return Result{ExitCode: 1, Err: err}, "", err
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	s.outMu.Lock()
	s.out.Reset()
	s.outMu.Unlock()

	ptmx, err := s.master()
	if err != nil {
		s.teardown()
		return Result{ExitCode: 1, Err: err}, "", err
	}
	if _, err := io.WriteString(ptmx, framed); err != nil {
		s.teardown()
		wrapped := fmt.Errorf("write shell command: %w", err)
		return Result{ExitCode: 1, Err: wrapped}, "", wrapped
	}

	out, status, cwd, err := s.waitMarker(ctx, timeout, framed)
	switch {
	case err == nil:
		return Result{Output: out, ExitCode: status}, cwd, nil
	case errors.Is(err, errShellDied), errors.Is(err, ErrClosed):
		return Result{ExitCode: 1, Err: err}, "", err
	default:
		return s.recoverTimeout(command, out, timeout, err)
	}
}

// recoverTimeout handles a command that produced no marker within timeout: it
// writes SIGINT, gives the shell a grace window to return to the prompt, and
// only hangs up (forcing a respawn on the next Run) when that fails.
func (s *Session) recoverTimeout(command string, partial string, timeout time.Duration, cause error) (Result, string, error) {
	// Capture only what follows the interrupt so the marker we are looking
	// for is not confused with the timed-out command's own output.
	s.outMu.Lock()
	s.out.Reset()
	s.outMu.Unlock()

	s.interrupt()

	_, _, cwd, err := s.waitMarker(context.Background(), s.grace, "")
	if err == nil {
		msg := fmt.Sprintf("command timed out after %s (interrupted; session shell preserved)", timeout)
		log.Printf("shell: %s while running %q", msg, command)
		res := Result{Output: partial, ExitCode: 1, Err: errors.New(msg)}
		return res, cwd, res.Err
	}
	log.Printf("shell: command timed out after %s and did not recover (%v); restarting session shell", timeout, err)
	s.teardown()
	res := Result{
		Output:   partial,
		ExitCode: 1,
		Err:      fmt.Errorf("command timed out after %s; session shell restarted", timeout),
	}
	return res, "", res.Err
}

// waitMarker blocks until the session's marker appears in the output, the
// deadline/caller context trips, or the shell dies/close is called. On success
// it returns the cleaned output, exit status and cwd. Callers must hold runMu.
func (s *Session) waitMarker(ctx context.Context, timeout time.Duration, framed string) (string, int, string, error) {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		s.outMu.Lock()
		data := s.out.String()
		s.outMu.Unlock()
		if start, status, cwd, ok := findMarker(data, s.marker); ok {
			return cleanOutput(data[:start], framed), status, cwd, nil
		}
		select {
		case <-s.notify:
		case <-timer.C:
			return cleanOutput(data, framed), 0, "", errTimeout
		case <-ctx.Done():
			return cleanOutput(data, framed), 0, "", ctx.Err()
		case <-s.dead:
			return cleanOutput(data, framed), 0, "", errShellDied
		case <-s.closeCh:
			return cleanOutput(data, framed), 0, "", ErrClosed
		}
	}
}

// readLoop drains the pty into the session buffer. It retires itself when its
// generation is superseded (a respawn) so it cannot corrupt the new session.
func (s *Session) readLoop(ptmx *os.File, gen int64, notify chan struct{}, dead chan struct{}) {
	buf := make([]byte, 4096)
	for {
		n, err := ptmx.Read(buf)
		if n > 0 && s.gen.Load() == gen {
			s.outMu.Lock()
			s.out.Write(buf[:n])
			s.outMu.Unlock()
			select {
			case notify <- struct{}{}:
			default:
			}
		}
		if err != nil {
			if s.gen.Load() == gen {
				if !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) {
					log.Printf("shell: session shell read ended: %v", err)
				}
				s.aliveFlag.Store(false)
				close(dead)
			}
			return
		}
	}
}

// interrupt sends SIGINT to the shell's foreground process group via the pty.
func (s *Session) interrupt() {
	ptmx, err := s.master()
	if err != nil {
		return
	}
	if _, err := ptmx.Write([]byte{0x03}); err != nil {
		log.Printf("shell: write SIGINT: %v", err)
	}
}

// teardown hangs up the current shell and retires its transport. The next Run
// respawns. Callers must hold runMu.
func (s *Session) teardown() {
	s.aliveFlag.Store(false)
	s.transportMu.Lock()
	ptmx, cmd := s.ptmx, s.cmd
	s.ptmx, s.cmd = nil, nil
	s.transportMu.Unlock()
	if ptmx != nil {
		if err := ptmx.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			log.Printf("shell: close pty: %v", err)
		}
	}
	killProcessGroup(cmd)
}

// killGroup hangs up the current shell's process group without touching state.
func (s *Session) killGroup() {
	s.transportMu.Lock()
	cmd := s.cmd
	s.transportMu.Unlock()
	killProcessGroup(cmd)
}

// master returns the live pty master, or an error when the session has none.
func (s *Session) master() (*os.File, error) {
	s.transportMu.Lock()
	defer s.transportMu.Unlock()
	if s.ptmx == nil {
		return nil, errors.New("shell session has no pty")
	}
	return s.ptmx, nil
}

// killProcessGroup sends SIGHUP then SIGKILL to cmd's process group. pty.Start
// makes the shell a session leader, so its pgid equals its pid.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	if err := syscall.Kill(-pid, syscall.SIGHUP); err != nil {
		log.Printf("shell: SIGHUP process group %d: %v", pid, err)
	}
	time.AfterFunc(2*time.Second, func() {
		if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			log.Printf("shell: SIGKILL process group %d: %v", pid, err)
		}
	})
}

// spawnArgs builds the shell invocation. `-il` makes it a login AND interactive
// shell (interactive is what sources ~/.zshrc / ~/.bashrc and enables the prompt
// hook). zsh additionally gets `-o nozle` so line editing/redraw cannot pollute
// the captured output.
func spawnArgs(shellPath string) []string {
	args := []string{"-il"}
	if isZsh(shellPath) {
		args = append(args, "-o", "nozle")
	}
	return args
}

// isZsh reports whether the resolved shell is a zsh binary.
func isZsh(shellPath string) bool {
	return strings.Contains(filepath.Base(shellPath), "zsh")
}

// sessionEnv is the process environment plus the overrides that keep the
// captured output clean and the user's history untouched. SessionOptions.Env is
// appended last so a caller can override any of these (the tests use it to
// point HOME/ZDOTDIR at a throwaway rc).
func (s *Session) sessionEnv() []string {
	env := os.Environ()
	env = append(env,
		// HISTFILE empty before start so zsh never opens/locks/writes history;
		// the prelude re-clears it after the rc may have set it.
		"HISTFILE=",
		"TERM=dumb",
		"NO_COLOR=1",
		"PAGER=cat",
		"GIT_PAGER=cat",
		"GIT_TERMINAL_PROMPT=0",
	)
	env = append(env, s.opts.Env...)
	return env
}

// prelude is sent once after spawn and discarded. It kills tty echo, blanks the
// prompts, suppresses history, clears rc-installed hooks (add-zsh-hook users
// such as starship/p10k/direnv would otherwise print into every result), and
// installs the marker hook.
func (s *Session) prelude() string {
	var b strings.Builder
	b.WriteString("stty -echo -onlcr 2>/dev/null\n")
	if isZsh(Resolve(s.opts.Shell)) {
		b.WriteString("PROMPT='' RPROMPT='' PROMPT2='' PROMPT_EOL_MARK=''\n")
		// PROMPT_SP makes zsh "clear" a partial line by writing spaces to the
		// end of the terminal width before every prompt; with a 400-column pty
		// that is a ~400-space blob appended to each command's output. It fires
		// even with an empty prompt, so it must be turned off explicitly.
		b.WriteString("unsetopt prompt_sp 2>/dev/null\n")
		b.WriteString("HISTFILE=; unset HISTFILE; unsetopt inc_append_history share_history 2>/dev/null\n")
		b.WriteString("unsetopt zle 2>/dev/null\n")
		// precmd_functions/preexec_functions are what `add-zsh-hook` appends to;
		// clearing them is the only way to remove a plugin's hook, since
		// defining precmd() alone leaves the array entries in place.
		b.WriteString("precmd_functions=() preexec_functions=()\n")
		b.WriteString("unset -f preexec 2>/dev/null\n")
		// Capture $? first — entering the function is itself a command.
		b.WriteString("precmd() { local st=$?; printf '\\n" + s.marker + " %s %s\\n' \"$st\" \"$PWD\" }\n")
	} else {
		b.WriteString("PS1='' PS2=''\n")
		b.WriteString("HISTFILE=; unset HISTFILE; set +o history 2>/dev/null\n")
		b.WriteString("trap - DEBUG\n")
		b.WriteString("PROMPT_COMMAND='printf \"\\n" + s.marker + " %s %s\\n\" \"$?\" \"$PWD\"'\n")
	}
	return b.String()
}
