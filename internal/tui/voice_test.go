package tui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/stt"
)

// fakeVoiceRec stands in for *stt.Recording so no microphone is opened.
type fakeVoiceRec struct {
	cancels  int
	stopPath string
	stopErr  error
}

func (f *fakeVoiceRec) Stop(time.Duration) (string, error) { return f.stopPath, f.stopErr }
func (f *fakeVoiceRec) Cancel()                            { f.cancels++ }

type voiceSeams struct {
	starts     int
	rec        *fakeVoiceRec
	transcribe func(context.Context, stt.Options, string) (stt.Result, error)
}

// stubVoice swaps the recorder, transcriber and model-status seams for the
// duration of one test. HOME is isolated first so voiceOptions() reads and
// SaveOcodeSTTConfig writes only under the test's temp tree.
func stubVoice(t *testing.T, status stt.Status) *voiceSeams {
	t.Helper()
	setVoiceHomeTree(t, t.TempDir())
	s := &voiceSeams{rec: &fakeVoiceRec{stopPath: ""}}
	s.transcribe = func(context.Context, stt.Options, string) (stt.Result, error) {
		return stt.Result{Text: "hello"}, nil
	}
	origStart, origTr, origStatus := voiceStart, voiceTranscribe, voiceStatusFor
	t.Cleanup(func() {
		voiceStart, voiceTranscribe, voiceStatusFor = origStart, origTr, origStatus
	})
	voiceStart = func(string) (voiceRecorder, error) {
		s.starts++
		return s.rec, nil
	}
	voiceTranscribe = func(ctx context.Context, opts stt.Options, p string) (stt.Result, error) {
		return s.transcribe(ctx, opts, p)
	}
	voiceStatusFor = func(stt.Options) stt.Status { return status }
	return s
}

// setVoiceHomeTree mirrors internal/config's setHomeTree (the tui package has
// none): HOME plus the XDG variables pointed at the matching default
// subdirectories, so Linux and darwin resolve config to the same place.
func setVoiceHomeTree(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
}

func availableStatus() stt.Status {
	return stt.Status{Selected: "whisper-1", Models: []stt.ModelInfo{{ID: "whisper-1", Label: "Whisper", Available: true}}}
}

func newVoiceTestModel() model {
	return model{
		activeTab: tabChat,
		input:     textarea.New(),
		mcpReady:  true,
		width:     100,
		height:    40,
	}
}

func voiceKey(s string) tea.KeyPressMsg {
	if len(s) == 1 {
		return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
	}
	return tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl}
}

// press sends one key through Update and returns the new model and its cmd.
func press(t *testing.T, m model, key tea.KeyPressMsg) (model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(key)
	return next.(model), cmd
}

// runMsg executes a cmd and feeds its message back through Update, the way
// the Bubble Tea runtime would.
func runMsg(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a cmd")
	}
	msg := cmd()
	next, _ := m.Update(msg)
	return next.(model)
}

func TestVoiceCtrlXVStartsThenStopsAndSubmits(t *testing.T) {
	s := stubVoice(t, availableStatus())
	m := newVoiceTestModel()

	m, cmd := press(t, m, voiceKey("ctrl+x"))
	if !m.leaderActive {
		t.Fatal("ctrl+x should arm the leader")
	}
	m, cmd = press(t, m, voiceKey("v"))
	if cmd == nil || m.voice.phase != voiceStarting {
		t.Fatalf("first ctrl+x v: phase=%v, want starting", m.voice.phase)
	}
	m = runMsg(t, m, cmd)
	if m.voice.phase != voiceRecording || s.starts != 1 {
		t.Fatalf("after start: phase=%v starts=%d", m.voice.phase, s.starts)
	}
	if !strings.Contains(stripANSI(m.voiceStatusText()), "REC") {
		t.Fatalf("status text while recording = %q", m.voiceStatusText())
	}

	m, _ = press(t, m, voiceKey("ctrl+x"))
	m, cmd = press(t, m, voiceKey("v"))
	if m.voice.phase != voiceTranscribing || cmd == nil {
		t.Fatalf("second ctrl+x v: phase=%v, want transcribing", m.voice.phase)
	}
	m = runMsg(t, m, cmd)

	if !m.hasDelayedChatInput() {
		t.Fatal("transcript was not submitted through the chat Enter path")
	}
	if got := m.takeDelayedChatInput(); got != "hello" {
		t.Fatalf("submitted %q, want %q", got, "hello")
	}
	if m.voice.phase != voiceIdle {
		t.Fatalf("phase after submit = %v, want idle", m.voice.phase)
	}
}

func TestVoiceRefusesUnavailableModelWithoutOpeningMic(t *testing.T) {
	s := stubVoice(t, stt.Status{Selected: "whisper-1", Models: []stt.ModelInfo{
		{ID: "whisper-1", Label: "Whisper", Available: false, Reason: "needs an OpenAI API key"},
	}})
	m := newVoiceTestModel()

	m, cmd := press(t, m, voiceKey("ctrl+x"))
	m, cmd = press(t, m, voiceKey("v"))
	m = runMsg(t, m, cmd)

	if s.starts != 0 {
		t.Fatalf("microphone opened %d times for an unavailable model", s.starts)
	}
	if m.voice.phase != voiceIdle {
		t.Fatalf("phase = %v, want idle", m.voice.phase)
	}
	if !strings.Contains(m.voice.note, "needs an OpenAI API key") {
		t.Fatalf("note = %q, want the availability reason", m.voice.note)
	}
}

func TestVoiceEmptyTranscriptDoesNotSubmit(t *testing.T) {
	stubVoice(t, availableStatus())
	m := newVoiceTestModel()
	m, _ = press(t, m, voiceKey("ctrl+x"))
	m, _ = press(t, m, voiceKey("v"))
	m.voice.phase = voiceTranscribing

	m = runMsg(t, m, func() tea.Msg { return voiceTranscribedMsg{text: "   "} })
	if m.hasDelayedChatInput() {
		t.Fatal("empty transcript must not submit a message")
	}
	if m.voice.note != "nothing heard" {
		t.Fatalf("note = %q, want %q", m.voice.note, "nothing heard")
	}
}

func TestVoiceTranscriptKeepsDraft(t *testing.T) {
	stubVoice(t, availableStatus())
	m := newVoiceTestModel()
	m.input.SetValue("half typed")
	m.voice.phase = voiceTranscribing

	m = runMsg(t, m, func() tea.Msg { return voiceTranscribedMsg{text: "hello"} })
	if got := m.takeDelayedChatInput(); got != "hello" {
		t.Fatalf("submitted %q, want %q", got, "hello")
	}
	if got := m.input.Value(); got != "half typed" {
		t.Fatalf("draft after submit = %q, want it restored", got)
	}
}

func TestVoiceEscCancelsActiveRecording(t *testing.T) {
	s := stubVoice(t, availableStatus())
	m := newVoiceTestModel()
	m, _ = press(t, m, voiceKey("ctrl+x"))
	m, cmd := press(t, m, voiceKey("v"))
	m = runMsg(t, m, cmd)

	m, cmd = press(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.voice.phase != voiceIdle {
		t.Fatalf("phase after esc = %v, want idle", m.voice.phase)
	}
	if cmd == nil {
		t.Fatal("esc should return the cancel cmd")
	}
	cmd()
	if s.rec.cancels != 1 {
		t.Fatalf("recorder Cancel calls = %d, want 1", s.rec.cancels)
	}
}

func TestVoiceTranscribeFailureShowsErrorAndDoesNotSubmit(t *testing.T) {
	s := stubVoice(t, availableStatus())
	s.transcribe = func(context.Context, stt.Options, string) (stt.Result, error) {
		return stt.Result{}, errors.New("upstream refused")
	}
	m := newVoiceTestModel()
	m.voice.phase = voiceRecording
	m.voice.rec = s.rec

	m, cmd := press(t, m, voiceKey("ctrl+x"))
	m, cmd = press(t, m, voiceKey("v"))
	m = runMsg(t, m, cmd)
	if m.hasDelayedChatInput() {
		t.Fatal("failed transcription must not submit")
	}
	if !strings.Contains(m.voice.note, "upstream refused") {
		t.Fatalf("note = %q, want the transcription error", m.voice.note)
	}
}

func TestVoiceKeyIgnoredWhileDialogOpen(t *testing.T) {
	s := stubVoice(t, availableStatus())
	m := newVoiceTestModel()
	m.showPermDialog = true
	m.leaderActive = true

	m, cmd := press(t, m, voiceKey("v"))
	if s.starts != 0 || m.voice.phase != voiceIdle {
		t.Fatalf("ctrl+x v with a dialog open started recording (phase=%v starts=%d)", m.voice.phase, s.starts)
	}
	_ = cmd
}

func TestVoiceSlashModelRejectsUnknownID(t *testing.T) {
	stubVoice(t, availableStatus())
	m := newVoiceTestModel()
	m.handleVoiceCmd([]string{"model", "not-a-model"})

	if len(m.messages) == 0 || !strings.Contains(m.messages[len(m.messages)-1].text, "Unknown voice model") {
		t.Fatalf("messages = %+v, want an unknown-model reply", m.messages)
	}
	cfg, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.STT.Model == "not-a-model" {
		t.Fatal("invalid model was persisted")
	}
}

func TestVoiceSlashModelPersistsValidID(t *testing.T) {
	stubVoice(t, availableStatus())
	m := newVoiceTestModel()
	m.handleVoiceCmd([]string{"model", "whisper-1"})

	if got := m.messages[len(m.messages)-1].text; !strings.Contains(got, "set to whisper-1") {
		t.Fatalf("reply = %q", got)
	}
	cfg, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.STT.Model != "whisper-1" {
		t.Fatalf("persisted STT model = %q, want whisper-1", cfg.STT.Model)
	}
}

func TestVoiceStatusAndListRender(t *testing.T) {
	stubVoice(t, availableStatus())
	m := newVoiceTestModel()
	m.handleVoiceCmd(nil)
	m.handleVoiceCmd([]string{"list"})

	out := stripANSI(m.messages[len(m.messages)-2].text + "\n" + m.messages[len(m.messages)-1].text)
	for _, want := range []string{"Model: Whisper (whisper-1)", "Microphone:", "whisper-1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestVoiceAbortOnQuitCancelsRecording(t *testing.T) {
	s := &fakeVoiceRec{}
	m := newVoiceTestModel()
	m.voice = voiceState{phase: voiceRecording, rec: s}
	m.abortVoice()
	if s.cancels != 1 || m.voice.phase != voiceIdle || m.voice.rec != nil {
		t.Fatalf("abort: cancels=%d phase=%v rec=%v", s.cancels, m.voice.phase, m.voice.rec)
	}
}
