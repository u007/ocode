package tui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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

// stubPartial swaps the live-partial decoder for one test.
func stubPartial(t *testing.T, fn func(context.Context, voiceRecorder, stt.Options) (stt.Result, error)) *int {
	t.Helper()
	calls := new(int)
	orig := voicePartial
	t.Cleanup(func() { voicePartial = orig })
	voicePartial = func(ctx context.Context, rec voiceRecorder, opts stt.Options) (stt.Result, error) {
		*calls++
		return fn(ctx, rec, opts)
	}
	return calls
}

// startRecording drives ctrl+x v twice-free: it opens the stubbed microphone
// and returns the model in the recording phase with gen set.
func startRecording(t *testing.T, s *voiceSeams) model {
	t.Helper()
	m := newVoiceTestModel()
	m, _ = press(t, m, voiceKey("ctrl+x"))
	m, cmd := press(t, m, voiceKey("v"))
	m = runMsg(t, m, cmd)
	if m.voice.phase != voiceRecording {
		t.Fatalf("phase = %v, want recording", m.voice.phase)
	}
	return m
}

func TestVoicePartialShownWhileRecordingAndClamped(t *testing.T) {
	stubVoice(t, availableStatus())
	m := startRecording(t, &voiceSeams{rec: &fakeVoiceRec{}})

	long := strings.Repeat("alpha beta gamma delta ", 10) + "THE END"
	m.onVoicePartial(voicePartialMsg{gen: m.voice.gen, text: long})

	status := stripANSI(m.voiceStatusText())
	if !strings.Contains(status, "REC - ctrl+x v to stop") || !strings.Contains(status, "heard: ") {
		t.Fatalf("status = %q", status)
	}
	if !strings.Contains(status, "THE END") {
		t.Fatalf("status should show the latest words, got %q", status)
	}
	if strings.ContainsAny(status, "\n\r") {
		t.Fatalf("status must be one line: %q", status)
	}
	segment := status[strings.Index(status, "heard: ")+len("heard: "):]
	if len(segment) > voicePartialShown {
		t.Fatalf("partial segment is %d cells, want <= %d: %q", len(segment), voicePartialShown, segment)
	}
}

func TestVoicePartialLineIsOneLine(t *testing.T) {
	got := voicePartialLine("  hello\n\n  wor\tld  café 你好 \x07")
	want := "hello wor ld café 你好"
	if got != want {
		t.Fatalf("voicePartialLine = %q, want %q", got, want)
	}
	if strings.ContainsAny(got, "\n\r\t\x07") {
		t.Fatalf("control character left in %q", got)
	}
}

// The tail keeps the last voicePartialKept runes, never a split UTF-8 sequence.
func TestVoicePartialLineTruncatesOnRuneBoundary(t *testing.T) {
	long := strings.Repeat("é", voicePartialKept+10)
	got := voicePartialLine(long)
	if !utf8.ValidString(got) || len([]rune(got)) != voicePartialKept {
		t.Fatalf("truncated to %d runes (valid utf8=%v)", len([]rune(got)), utf8.ValidString(got))
	}
}

func TestVoicePartialLateAfterStopIsDropped(t *testing.T) {
	s := stubVoice(t, availableStatus())
	stubPartial(t, func(context.Context, voiceRecorder, stt.Options) (stt.Result, error) {
		return stt.Result{Text: "late words"}, nil
	})
	m := startRecording(t, s)
	gen := m.voice.gen

	tickCmd := m.onVoicePartialTick(voicePartialTickMsg{gen: gen})
	if tickCmd == nil || !m.voice.partialBusy {
		t.Fatal("tick should start a partial decode")
	}

	// The user stops before the decode returns.
	m, _ = press(t, m, voiceKey("ctrl+x"))
	m, _ = press(t, m, voiceKey("v"))
	if m.voice.phase != voiceTranscribing {
		t.Fatalf("phase = %v, want transcribing", m.voice.phase)
	}

	if cmd := m.onVoicePartial(voicePartialMsg{gen: gen, text: "late words"}); cmd != nil {
		t.Fatal("late partial must not re-arm a tick")
	}
	if m.voice.partial != "" {
		t.Fatalf("late partial shown after stop: %q", m.voice.partial)
	}
	if strings.Contains(stripANSI(m.voiceStatusText()), "late words") {
		t.Fatal("status shows a partial after stop")
	}
}

func TestVoicePartialFromPreviousRecordingIsDropped(t *testing.T) {
	s := stubVoice(t, availableStatus())
	m := startRecording(t, s)
	oldGen := m.voice.gen

	// Stop, then start a fresh recording.
	m, _ = press(t, m, voiceKey("ctrl+x"))
	m, _ = press(t, m, voiceKey("v"))
	m.voice.phase = voiceIdle
	m, cmd := press(t, m, voiceKey("ctrl+x"))
	_ = cmd
	m, cmd = press(t, m, voiceKey("v"))
	m = runMsg(t, m, cmd)
	if m.voice.gen == oldGen {
		t.Fatal("a new recording must get a new generation")
	}

	m.onVoicePartial(voicePartialMsg{gen: oldGen, text: "stale"})
	if m.voice.partial != "" {
		t.Fatalf("partial from an earlier recording shown: %q", m.voice.partial)
	}
}

func TestVoicePartialErrorIsSilent(t *testing.T) {
	s := stubVoice(t, availableStatus())
	stubPartial(t, func(context.Context, voiceRecorder, stt.Options) (stt.Result, error) {
		return stt.Result{}, errors.New("decoder exploded")
	})
	m := startRecording(t, s)
	gen := m.voice.gen
	m.voice.partial = "previous words"

	if cmd := m.onVoicePartialTick(voicePartialTickMsg{gen: gen}); cmd == nil {
		t.Fatal("tick should start a decode")
	}
	cmd := m.onVoicePartial(voicePartialMsg{gen: gen, err: errors.New("decoder exploded")})
	if cmd == nil {
		t.Fatal("a failed partial should keep the tick chain alive")
	}
	if m.voice.note != "" {
		t.Fatalf("partial error set the note: %q", m.voice.note)
	}
	if m.voice.partial != "previous words" {
		t.Fatalf("partial error changed the shown text: %q", m.voice.partial)
	}
	if m.voice.phase != voiceRecording {
		t.Fatalf("partial error changed the phase to %v", m.voice.phase)
	}
}

func TestVoicePartialSingleFlight(t *testing.T) {
	s := stubVoice(t, availableStatus())
	calls := stubPartial(t, func(context.Context, voiceRecorder, stt.Options) (stt.Result, error) {
		return stt.Result{Text: "one two"}, nil
	})
	m := startRecording(t, s)
	gen := m.voice.gen

	first := m.onVoicePartialTick(voicePartialTickMsg{gen: gen})
	if second := m.onVoicePartialTick(voicePartialTickMsg{gen: gen}); second != nil {
		t.Fatal("a second tick while a partial is in flight must not start another decode")
	}
	msg := first().(voicePartialMsg)
	if *calls != 1 {
		t.Fatalf("decoder calls = %d, want 1", *calls)
	}
	if cmd := m.onVoicePartial(msg); cmd == nil {
		t.Fatal("result should re-arm the tick")
	}
	if m.voice.partial != "one two" || m.voice.partialBusy {
		t.Fatalf("partial=%q busy=%v", m.voice.partial, m.voice.partialBusy)
	}
}

func TestVoicePartialStaleTickIsIgnored(t *testing.T) {
	s := stubVoice(t, availableStatus())
	calls := stubPartial(t, func(context.Context, voiceRecorder, stt.Options) (stt.Result, error) {
		return stt.Result{Text: "x"}, nil
	})
	m := startRecording(t, s)
	if cmd := m.onVoicePartialTick(voicePartialTickMsg{gen: m.voice.gen + 7}); cmd != nil {
		t.Fatal("tick for another recording must do nothing")
	}
	if *calls != 0 {
		t.Fatalf("decoder ran %d times for a stale tick", *calls)
	}
}

func TestVoicePartialNeverSubmitsOrTouchesComposer(t *testing.T) {
	s := stubVoice(t, availableStatus())
	stubPartial(t, func(context.Context, voiceRecorder, stt.Options) (stt.Result, error) {
		return stt.Result{Text: "should not be sent"}, nil
	})
	m := startRecording(t, s)
	m.input.SetValue("my draft")
	gen := m.voice.gen

	msg := m.onVoicePartialTick(voicePartialTickMsg{gen: gen})().(voicePartialMsg)
	m.onVoicePartial(msg)

	if m.voice.partial != "should not be sent" {
		t.Fatalf("partial = %q", m.voice.partial)
	}
	if m.hasDelayedChatInput() {
		t.Fatal("a partial must never submit a message")
	}
	if got := m.input.Value(); got != "my draft" {
		t.Fatalf("composer changed to %q", got)
	}
	if m.voice.phase != voiceRecording {
		t.Fatalf("partial changed phase to %v", m.voice.phase)
	}
}

func TestVoicePartialClearedOnCancel(t *testing.T) {
	s := stubVoice(t, availableStatus())
	m := startRecording(t, s)
	m.voice.partial = "some words"
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.voice.partial != "" || m.voice.phase != voiceIdle {
		t.Fatalf("after cancel: partial=%q phase=%v", m.voice.partial, m.voice.phase)
	}
}

// A hosted engine never gets live previews: each would re-upload the clip.
func TestVoicePartialSkippedForHostedEngine(t *testing.T) {
	s := stubVoice(t, availableStatus())
	if err := config.SaveOcodeSTTConfig(config.STTConfig{Model: "whisper-1"}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	calls := stubPartial(t, func(context.Context, voiceRecorder, stt.Options) (stt.Result, error) {
		return stt.Result{Text: "should not run"}, nil
	})
	m := startRecording(t, s)
	cmd := m.onVoicePartialTick(voicePartialTickMsg{gen: m.voice.gen})
	if cmd == nil {
		t.Fatalf("tick returned no command")
	}
	msg, ok := cmd().(voicePartialMsg)
	if !ok || !msg.skipped {
		t.Fatalf("hosted engine partial = %#v, want skipped", msg)
	}
	if *calls != 0 {
		t.Fatalf("decoder ran %d times for a hosted engine", *calls)
	}
}
