package tui

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/paths"
	"github.com/u007/ocode/internal/stt"
)

// Voice input: ctrl+x v starts the microphone, ctrl+x v again stops it, the
// clip is transcribed on a tea.Cmd and the transcript is submitted as the next
// chat message through the same path as pressing Enter.
//
// The recorder and transcriber are package vars so tests never touch a real
// microphone or a real transcription backend.

// voiceRecorder is the part of *stt.Recording the TUI uses.
type voiceRecorder interface {
	Stop(timeout time.Duration) (string, error)
	Cancel()
}

var (
	voiceStart = func(dir string) (voiceRecorder, error) {
		rec, err := stt.StartRecording(dir)
		if err != nil {
			return nil, err
		}
		return rec, nil
	}
	voiceTranscribe = stt.Transcribe
	voiceStatusFor  = stt.StatusFor
	voiceMicCheck   = stt.RecordingAvailable
)

const (
	voiceStopTimeout       = 5 * time.Second
	voiceTranscribeTimeout = 3 * time.Minute
)

type voicePhase int

const (
	voiceIdle voicePhase = iota
	voiceStarting
	voiceRecording
	voiceTranscribing
)

// voiceState is the TUI's view of one dictation. note is the last one-line
// outcome (error, "nothing heard", ...) and is cleared by the next action.
type voiceState struct {
	phase voicePhase
	rec   voiceRecorder
	note  string
}

type voiceStartedMsg struct {
	rec voiceRecorder
	err error
}

type voiceTranscribedMsg struct {
	text string
	err  error
}

type voiceCancelledMsg struct{}

// voiceDir is where captured clips are written. Captured WAVs are removed
// after transcription, so the directory only holds a clip mid-flight.
func voiceDir() string {
	if base, err := paths.GlobalDataDir(); err == nil && base != "" {
		return filepath.Join(base, "stt")
	}
	return filepath.Join(os.TempDir(), "ocode-stt")
}

// voiceOptions reads the model selected in settings at call time, so a
// /voice model change applies to the next recording without a restart.
func voiceOptions() stt.Options {
	model := ""
	cfg, err := config.LoadOcodeConfigCopy()
	if err != nil {
		log.Printf("voice: load config: %v", err)
	} else if cfg != nil {
		model = cfg.STT.Model
	}
	return stt.Options{
		Model:     model,
		OpenAIKey: func() string { return auth.ResolveKey("openai") },
		TempDir:   voiceDir(),
	}
}

// voiceUnavailableReason explains why the selected model cannot run.
func voiceUnavailableReason(st stt.Status) string {
	for _, mi := range st.Models {
		if mi.ID != st.Selected {
			continue
		}
		if mi.Reason != "" {
			return fmt.Sprintf("voice model %s is unavailable: %s", mi.ID, mi.Reason)
		}
		return fmt.Sprintf("voice model %s is unavailable", mi.ID)
	}
	return fmt.Sprintf("voice model %s is unknown", st.Selected)
}

// chatComposerFree reports whether the chat composer owns the keyboard: the
// chat tab is up and no picker, dialog or detail view is open.
func (m model) chatComposerFree() bool {
	return m.activeTab == tabChat && !m.showPicker && !m.showConnect && !m.showFileSearch &&
		!m.showPermDialog && !m.showRetryDialog && !m.banClearConfirm && !m.showQuestionDialog &&
		!m.showBtwDialog && m.detail.empty()
}

// voiceKeyAllowed gates the ctrl+x v binding.
func (m model) voiceKeyAllowed() bool {
	return m.chatComposerFree() && !m.modalOpen()
}

// voiceCanSubmit gates auto-submission of a finished transcript.
func (m model) voiceCanSubmit() bool {
	return m.chatComposerFree() && !m.modalOpen() && !m.leaderActive
}

// toggleVoice is the ctrl+x v state machine. Only idle and recording have an
// action; the in-flight phases only report that they are busy.
func (m *model) toggleVoice() tea.Cmd {
	switch m.voice.phase {
	case voiceIdle:
		return m.startVoice()
	case voiceRecording:
		rec := m.voice.rec
		m.voice.rec = nil
		m.voice.phase = voiceTranscribing
		m.voice.note = ""
		return voiceFinishCmd(rec)
	default:
		m.voice.note = "still busy, try again in a moment"
		return nil
	}
}

// startVoice checks the model and opens the microphone on a tea.Cmd. The
// availability probe can run a Python interpreter, so it stays off the UI
// goroutine.
func (m *model) startVoice() tea.Cmd {
	m.voice.phase = voiceStarting
	m.voice.note = ""
	return func() tea.Msg {
		opts := voiceOptions()
		st := voiceStatusFor(opts)
		if !st.ModelAvailable(st.Selected) {
			return voiceStartedMsg{err: errors.New(voiceUnavailableReason(st))}
		}
		rec, err := voiceStart(voiceDir())
		if err == nil && rec == nil {
			err = errors.New("no microphone recorder")
		}
		return voiceStartedMsg{rec: rec, err: err}
	}
}

func (m *model) onVoiceStarted(msg voiceStartedMsg) tea.Cmd {
	if m.voice.phase != voiceStarting {
		// Unreachable today (nothing cancels a start in flight), but never
		// leave a live microphone behind.
		if msg.rec != nil {
			return voiceCancelCmd(msg.rec)
		}
		return nil
	}
	if msg.err != nil {
		m.voice.phase = voiceIdle
		m.voice.note = msg.err.Error()
		return nil
	}
	m.voice.phase = voiceRecording
	m.voice.rec = msg.rec
	m.voice.note = ""
	return nil
}

// voiceFinishCmd stops the capture and transcribes it. The clip is deleted
// once transcription returns, whatever the outcome.
func voiceFinishCmd(rec voiceRecorder) tea.Cmd {
	return func() tea.Msg {
		path, err := rec.Stop(voiceStopTimeout)
		if path != "" {
			defer func() { _ = os.Remove(path) }()
		}
		if err != nil {
			return voiceTranscribedMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), voiceTranscribeTimeout)
		defer cancel()
		res, err := voiceTranscribe(ctx, voiceOptions(), path)
		if err != nil {
			log.Printf("voice: transcribe: %v", err)
			return voiceTranscribedMsg{err: err}
		}
		return voiceTranscribedMsg{text: res.Text}
	}
}

// cancelVoice discards the active recording (esc). The capture process is
// stopped on a tea.Cmd because Stop waits for it to exit.
func (m *model) cancelVoice() tea.Cmd {
	rec := m.voice.rec
	m.voice.rec = nil
	m.voice.phase = voiceIdle
	m.voice.note = "recording cancelled"
	if rec == nil {
		return nil
	}
	return voiceCancelCmd(rec)
}

func voiceCancelCmd(rec voiceRecorder) tea.Cmd {
	return func() tea.Msg {
		rec.Cancel()
		return voiceCancelledMsg{}
	}
}

// abortVoice stops any capture on the way out of the TUI so the microphone is
// released. It runs on the quit path, after the program has stopped rendering.
func (m *model) abortVoice() {
	if m.voice.rec != nil {
		m.voice.rec.Cancel()
	}
	m.voice.rec = nil
	m.voice.phase = voiceIdle
}

// submitVoiceTranscript sends a finished transcript through the chat Enter
// path. The user's draft is set aside for the duration and restored after, so
// a dictation never eats text they were typing.
func (m model) submitVoiceTranscript(text string) (tea.Model, tea.Cmd) {
	m.voice.note = ""
	if !m.voiceCanSubmit() {
		if strings.TrimSpace(m.input.Value()) == "" {
			m.input.SetValue(text)
			m.voice.note = "a dialog is open; transcript is in the input, press enter to send"
		} else {
			m.voice.note = "a dialog is open and the input has a draft; transcript dropped"
		}
		return m, nil
	}
	draft := m.input.Value()
	m.input.SetValue(text)
	next, cmd := m.handleChatKeys(tea.KeyPressMsg{Code: tea.KeyEnter}, nil, nil)
	out := voiceModelOf(next)
	if draft != "" && out.input.Value() == "" {
		out.input.SetValue(draft)
	}
	return out, cmd
}

func voiceModelOf(v tea.Model) model {
	switch n := v.(type) {
	case model:
		return n
	case *model:
		return *n
	}
	return model{}
}

// voiceStatusText is the chat status-row segment for dictation. Empty when
// idle with no outcome to report.
func (m model) voiceStatusText() string {
	switch m.voice.phase {
	case voiceStarting:
		return "voice: starting mic..."
	case voiceRecording:
		return "REC - ctrl+x v to stop, esc to cancel"
	case voiceTranscribing:
		return "voice: transcribing..."
	}
	if m.voice.note != "" {
		return "voice: " + m.voice.note
	}
	return ""
}

func runVoiceCmd(m *model, args []string) tea.Cmd {
	m.handleVoiceCmd(args)
	return nil
}

// handleVoiceCmd implements /voice (status), /voice list and /voice model <id>.
// Everything here is synchronous and local, so the command is instant.
func (m *model) handleVoiceCmd(args []string) {
	reply := func(lines ...string) {
		m.messages = append(m.messages, message{role: roleAssistant, text: strings.Join(lines, "\n")})
	}
	sub := ""
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
	}
	switch sub {
	case "", "status":
		opts := voiceOptions()
		st := voiceStatusFor(opts)
		mic := "ready"
		if err := voiceMicCheck(); err != nil {
			mic = "unavailable: " + err.Error()
		}
		selected := "unknown"
		availability := "unavailable"
		for _, mi := range st.Models {
			if mi.ID != st.Selected {
				continue
			}
			selected = fmt.Sprintf("%s (%s)", mi.Label, mi.ID)
			if mi.Available {
				availability = "available"
			} else {
				availability = "unavailable: " + mi.Reason
			}
		}
		reply("Voice input: ctrl+x v starts and stops recording; the transcript is sent as your next message.",
			"Model: "+selected,
			"Model status: "+availability,
			"Microphone: "+mic,
			"Use /voice list to see models, /voice model <id> to change the model.")
	case "list":
		st := voiceStatusFor(voiceOptions())
		lines := []string{"Voice models:"}
		for _, mi := range st.Models {
			mark := " "
			if mi.ID == st.Selected {
				mark = "*"
			}
			state := "available"
			if !mi.Available {
				state = "unavailable: " + mi.Reason
			}
			lines = append(lines, fmt.Sprintf("%s %s  %s  [%s]", mark, mi.ID, mi.Label, state))
		}
		reply(lines...)
	case "model":
		if len(args) < 2 {
			reply("Usage: /voice model <id> (see /voice list)")
			return
		}
		id := args[1]
		if !stt.ValidModel(id) {
			reply(fmt.Sprintf("Unknown voice model %q. Run /voice list to see the choices.", id))
			return
		}
		if err := config.SaveOcodeSTTConfig(config.STTConfig{Model: id}); err != nil {
			reply("Could not save the voice model: " + err.Error())
			return
		}
		msg := "Voice model set to " + id + "."
		st := voiceStatusFor(voiceOptions())
		if !st.ModelAvailable(id) {
			msg += " Note: " + voiceUnavailableReason(st) + "."
		}
		reply(msg)
	default:
		reply("Usage: /voice [status|list|model <id>]")
	}
}
