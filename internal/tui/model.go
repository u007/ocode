package tui

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jamesmercstudio/ocode/internal/agent"
	"github.com/jamesmercstudio/ocode/internal/auth"
	"github.com/jamesmercstudio/ocode/internal/config"
	"github.com/jamesmercstudio/ocode/internal/plugins"
	"github.com/jamesmercstudio/ocode/internal/session"
	"github.com/jamesmercstudio/ocode/internal/skill"
	"github.com/jamesmercstudio/ocode/internal/snapshot"
	"github.com/jamesmercstudio/ocode/internal/tool"
	"github.com/jamesmercstudio/ocode/internal/usage"
	"github.com/jamesmercstudio/ocode/internal/version"

	"github.com/atotto/clipboard"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

//go:embed initialize_prompt.txt
var initializePromptTemplate string

type scrollbarDragTarget int

const (
	scrollbarDragNone scrollbarDragTarget = iota
	scrollbarDragTranscript
	scrollbarDragDetail
	scrollbarDragGitDiff
	scrollbarDragFilesPreview
	scrollbarDragLog
)

type role int

const (
	roleUser role = iota
	roleAssistant
	roleThinking
)

type message struct {
	role      role
	text      string
	raw       *agent.Message
	transient bool
}

// estimateTok approximates token count as len(s)/4.
func estimateTok(s string) int {
	return len(s) / 4
}

// formatTok formats an integer token count compactly, e.g. 1234 → "1.2k".
func formatTok(n int) string {
	switch {
	case n >= 1000000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return strconv.Itoa(n)
	}
}

// columnPad returns spaces to pad label to width w for alignment.
func columnPad(label string, w int) string {
	pad := w - len(label)
	if pad < 1 {
		pad = 1
	}
	return strings.Repeat(" ", pad)
}

// groupMCPToolDefs separates tool definitions into per-server MCP groups and builtin.
func groupMCPToolDefs(
	defs []map[string]interface{},
	mcpToolSet map[string]struct{},
	serverNames []string,
) (grouped map[string][]map[string]interface{}, builtin []map[string]interface{}) {
	grouped = make(map[string][]map[string]interface{})
	for _, def := range defs {
		name, _ := def["name"].(string)
		if _, isMCP := mcpToolSet[name]; !isMCP {
			builtin = append(builtin, def)
			continue
		}
		matched := false
		for _, srv := range serverNames {
			if strings.HasPrefix(name, srv+"_") {
				grouped[srv] = append(grouped[srv], def)
				matched = true
				break
			}
		}
		if !matched {
			builtin = append(builtin, def)
		}
	}
	return
}

func latestRequestUsage(messages []message) (input, output, total int64) {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].raw == nil || messages[i].raw.Usage == nil {
			continue
		}
		u := messages[i].raw.Usage
		if u.PromptTokens != nil {
			input = *u.PromptTokens
		}
		if u.CompletionTokens != nil {
			output = *u.CompletionTokens
		}
		if u.TotalTokens != nil {
			total = *u.TotalTokens
		} else {
			total = input + output
		}
		return
	}
	return 0, 0, 0
}

func (m *model) currentContextEstimate() (int64, string) {
	agentMsgs, _ := m.buildAgentMessagesSnapshot()
	if len(agentMsgs) == 0 {
		return 0, "empty"
	}
	tokens, source := agent.CurrentContextEstimate(agentMsgs)
	return int64(tokens), source
}

type editorFinishedMsg struct {
	content string
	err     error
}

type permissionAskMsg struct {
	toolName   string
	toolArgs   json.RawMessage
	toolCallID string
}

type authFinishedMsg struct {
	token string
	err   error
}

type shellFinishedMsg struct {
	command    string
	output     string
	toolCallID string
	err        error
}

type connectOAuthFinishedMsg struct {
	provider string
	cred     auth.Credential
	err      error
}

type connectTestFinishedMsg struct {
	provider string
	err      error
}

type fileSearchFinishedMsg struct {
	processedText string
	messages      []message
	images        []agent.Image
	err           error
}

type leaderTimeoutMsg struct {
	seq int
}

type statusMsg struct {
	text string
}

type usageSummaryMsg struct {
	text string
	err  error
}

type streamMsgEvent struct {
	msg    agent.Message
	ch     chan agent.Message
	errCh  chan error
	cancel chan struct{}
}

type ctrlCResetMsg struct{}
type cleanupRequestMsg struct{}
type dotTickMsg struct{}

// sidebarComputeCache memoises expensive sidebar values (context-token estimate
// and telemetry aggregation) that walk the full message slice. The cache is
// keyed on a coarse fingerprint of m.messages; when nothing has changed we
// skip the O(n) recompute on every View() call (every keystroke).
type sidebarComputeCache struct {
	key            sidebarCacheKey
	ctxTokens      int64
	ctxSource      string
	ctxComputed    bool
	telemetry      sidebarTelemetry
	telemetryReady bool
}

type sidebarCacheKey struct {
	msgCount int
	lastLen  int
	model    string
}
type registryReadyMsg struct{ failed bool }
type pickerFilterApplyMsg struct {
	seq    int
	filter string
}
type fileListCacheMsg struct{ items []slashSuggestion }
type streamStartedMsg struct{ cancel chan struct{} }

type streamDoneMsg struct {
	err error
}

type compactStartedMsg struct{}
type titleGeneratedMsg struct {
	title string
	gen   uint64
}

// titleResult is the envelope sent on titleCh so the receiver can detect
// stale results from goroutines started before /new or /title clear.
type titleResult struct {
	title string
	gen   uint64
}
type compactFinishedMsg struct {
	result agent.CompactResult
}

type activityUpdateMsg struct {
	tracker *agent.ActivityTracker
	snap    agent.ActivitySnapshot
}

type debugLogMsg struct{}

func waitForDebugLog() tea.Cmd {
	return func() tea.Msg {
		<-DebugLog.Notify()
		return debugLogMsg{}
	}
}

func waitForRegistry() tea.Cmd {
	return func() tea.Msg {
		// Poll for up to ~5 seconds (50 × 100ms). If the registry never becomes
		// ready (network down, no cache), give up and report failure so the UI
		// degrades gracefully instead of leaking this goroutine indefinitely.
		const maxPolls = 50
		for i := 0; i < maxPolls; i++ {
			if agent.RegistryReady() {
				return registryReadyMsg{}
			}
			time.Sleep(100 * time.Millisecond)
		}
		return registryReadyMsg{failed: true}
	}
}

func (m *model) ensureCleanupState() *modelCleanupState {
	if m.cleanupState == nil {
		m.cleanupState = newModelCleanupState()
	}
	if m.cleanupState.shutdown == nil {
		m.cleanupState.shutdown = make(map[*agent.Agent]struct{})
	}
	return m.cleanupState
}

func (m *model) cleanupAgent(target *agent.Agent) {
	state := m.ensureCleanupState()
	state.mu.Lock()
	if _, ok := state.shutdown[target]; ok {
		state.mu.Unlock()
		return
	}
	state.shutdown[target] = struct{}{}
	hook := state.onCleanup
	shutdown := state.shutdownAgent
	state.mu.Unlock()
	// saveSession and the onCleanup hook run inside the dedup guard so repeated
	// cleanupCurrentSession calls (e.g. signal handler + deferred cleanup) do not
	// write the session file or fire hooks more than once per agent.
	if hook != nil {
		hook()
	}
	m.saveSession()
	if target == nil {
		return
	}
	if shutdown != nil {
		shutdown(target)
		return
	}
	target.Shutdown()
}

func (m *model) cleanupCurrentSession() {
	if m.supervisor != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = m.supervisor.Shutdown(ctx)
	}
	m.cleanupAgent(m.agent)
}

func (m *model) replaceAgent(next *agent.Agent) tea.Cmd {
	prev := m.agent
	// Cancel sub-agents first so they can't start new processes while we
	// tear down the old agent.
	if prev != nil {
		prev.Cancel()
		if prev.Runs() != nil {
			prev.Runs().CancelAll()
		}
	}
	// Now kill every tracked process. Any process that was started by a
	// sub-agent before CancelAll ran is already registered with the
	// supervisor and will be killed here.
	if m.supervisor != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = m.supervisor.TerminateAll(ctx)
	}
	m.cleanupAgent(prev)
	return m.installAgent(next)
}

func (m *model) installAgent(next *agent.Agent) tea.Cmd {
	m.agent = next
	if m.agent != nil {
		m.agent.SetSupervisor(m.supervisor)
	}
	if m.agent == nil {
		return nil
	}
	m.wireCompactCallbacks()
	return listenJobs(m.agent)
}

type model struct {
	viewport            viewport.Model
	input               textarea.Model
	messages            []message
	agent               *agent.Agent
	config              *config.Config
	sessionID           string
	sessionTitle        string
	showThinking        bool
	showDetails         bool
	leaderActive        bool
	leaderSeq           int
	showPalette         bool
	showPicker          bool
	pickerKind          string
	pickerItems         []string
	pickerValues        []string
	pickerIsHeader      []bool
	pickerIndex         int
	pickerFilter        string
	pickerFilterPending string
	pickerFilterSeq     int

	// Pagination state for the session picker (infinite scroll)
	pickerSessionRefs        []session.Ref // all loaded session refs
	pickerSessionPage        int           // number of pages loaded so far
	pickerSessionTotal       int           // total count of all sessions
	pickerSessionMore        bool          // whether more pages are available
	showSlashPopup           bool
	slashPopupIndex          int
	slashPopupItems          []slashSuggestion
	fileListCache            []slashSuggestion
	fileShortcodePaths       map[string]string
	showConnect              bool
	connect                  *connectDialog
	showSidebar              bool
	sidebarScroll            int
	sessionTelemetry         sidebarTelemetry
	activeModel              string
	paletteInput             string
	width                    int
	height                   int
	ready                    bool
	activeTab                int
	chatUnread               bool
	files                    filesModel
	git                      gitModel
	logViewport              viewport.Model
	logEntries               []DebugEntry
	logSearch                string
	logKindFilter            map[DebugEntryKind]bool
	err                      error
	scrollSpeed              int
	restoredPendingScroll    bool
	scrollbarDrag            scrollbarDragTarget
	scrollbarDragOffset      int
	workDir                  string
	currentAgentIdx          int
	branchlessMode           bool
	showPermDialog           bool
	showQuestionDialog       bool
	questionToolCallID       string
	questionPrompts          []tool.QuestionPrompt
	questionTab              int
	questionCursor           []int
	questionSelected         []map[int]bool
	questionCustom           []string
	questionTextActive       bool
	questionInput            textarea.Model
	pendingToolName          string
	pendingToolArgs          json.RawMessage
	pendingToolCallID        string
	pendingPermission        agent.PermissionRequest
	styles                   Styles
	streaming                bool
	ctrlCPressed             bool
	exitPending              bool
	cancelStream             chan struct{}
	lastActivity             agent.ActivitySnapshot
	activityRowReserved      bool
	escPressed               bool
	escPressTime             time.Time
	lastRetryableLLMErr      string
	inputHistory             []string
	inputHistoryIndex        int
	unsavedInput             string
	inputAtFirstLineUpNotice bool
	queuedInputs             []string
	pendingJobMsgs           []message
	expandedToolOutputs      map[int]bool
	toolOutputRegions        []toolOutputRegion
	expandedThinking         map[int]bool
	thinkingRegions          []toolOutputRegion
	dotFrame                 int
	sel                      selectionState
	detail                   detailStack
	agentStripBlocks         []agentStripBlock
	agentStripRow0           int
	streamStartedAt          time.Time
	streamEndedAt            time.Time
	streamTokenEstimate      int       // live character count during streaming for token estimation
	streamThinkingChars      int       // live thinking/reasoning character count
	streamOutputChars        int       // live output (non-thinking) character count
	tokenBlinkUntil          time.Time // when the token-count blink effect expires (2s after last token)
	streamWasInterrupted     bool
	transcriptContent        string
	transcriptLines          []string
	rawTranscriptLines       []string
	filesSel                 selectionState
	inputSel                 selectionState
	gitSel                   selectionState
	sidebarSel               selectionState
	rawSidebarLines          []string
	hoverSidebarFile         string // file path hovered by mouse in sidebar, empty when no hover
	rawInputLines            []string
	rawInputLinesDirty       bool
	inputThemeApplied        bool
	inputThemeShellMode      bool
	sidebarCache             *sidebarComputeCache
	compactCh                chan agent.CompactResult
	compactStartCh           chan struct{}
	titleCh                  chan titleResult
	deltaCh                  chan deltaEvent
	deltaDrops               uint64 // bumped each time the deltaCh select-default path drops a streamed token; visual-only stat, full text still arrives via the final assistant Message
	usageCh                  chan usageEvent
	streamFinalOutputTokens  int64     // exact output tokens from streaming usage event (0 = not yet received)
	streamingThinkingIdx     int       // index into m.messages of the in-flight roleThinking message; -1 when none
	lastDeltaRender          time.Time // throttles renderTranscript to ≥50ms cadence during streams
	titleRequested           bool
	titleGen                 uint64 // monotonic counter; bumped on /new + /title clear so stale goroutine results land harmlessly
	compacting               bool
	lastCompactErr           error
	pendingCompactUIIdx      []int
	pendingCompactResume     bool
	skipCompactPreflight     bool
	thinkingLevelIdx         int  // index into thinkingBudgetLevels
	agentStripOffset         int  // first visible run index in the agent strip
	agentStripSelected       int  // selected run index in the agent strip
	agentStripFocused        bool // whether keyboard nav is routed to the agent strip
	subAgentPermCh           chan subAgentPermRequest
	subAgentPermMu           *sync.Mutex                   // serialises concurrent sub-agent permission asks
	pendingSubAgentResp      chan agent.PermissionResponse // non-nil while a sub-agent permission dialog is open
	lastClickTime            time.Time
	lastClickX               int
	lastClickY               int
	permButtonRegions        []permButtonRegion
	cleanupState             *modelCleanupState
	supervisor               *tool.ProcessSupervisor
}

type modelCleanupState struct {
	mu            sync.Mutex
	shutdown      map[*agent.Agent]struct{}
	onCleanup     func()
	shutdownAgent func(*agent.Agent)
}

func newModelCleanupState() *modelCleanupState {
	return &modelCleanupState{shutdown: make(map[*agent.Agent]struct{})}
}

// agentStripMaxRows caps how many strip rows are visible at once so a large
// number of running agents cannot push the input box off screen.
const agentStripMaxRows = 8

// subAgentPermRequest carries a sub-agent permission ask from the sub-agent's
// goroutine to the TUI Update loop, plus the channel the answer is sent back on.
type subAgentPermRequest struct {
	req    agent.PermissionRequest
	respCh chan agent.PermissionResponse
}

var thinkingBudgetLevels = []int{0, 1024, 8000, 16000}
var thinkingBudgetLabels = []string{"off", "low", "med", "high"}

func thinkingLevelIndexForBudget(budget int) int {
	for i, level := range thinkingBudgetLevels {
		if level == budget {
			return i
		}
	}
	return 0
}

type toolOutputRegion struct {
	messageIndex int
	startLine    int
	endLine      int
}

type permButtonRegion struct {
	choice         string
	x1, x2, y1, y2 int
}

type agentResponseMsg string
type errorMsg error

var (
	userStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#7AA2F7")).Bold(true)
	assistantStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#BB9AF7")).Bold(true)
	headerStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#7DCFFF")).Bold(true)
	borderStyle    = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#3B4261")).
			Padding(0, 1)
	cleanBoxStyle       = lipgloss.NewStyle()
	hintStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("#565F89")).Italic(true)
	selectedStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#1A1B26")).Background(lipgloss.Color("#7AA2F7"))
	statusStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("#787C99")).Background(lipgloss.Color("#1A1B26")).Padding(0, 1).Bold(true)
	successStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("#9ECE6A"))
	errorStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("#F7768E"))
	textStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("#C0CAF5"))
	thinkingStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	thinkingHeaderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Bold(true)
	dimStyle            = lipgloss.NewStyle().Foreground(lipgloss.Color("#3B4261"))
	toolBoxStyle        = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#3B4261")).Padding(0, 1)
	sidebarHeaderStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#7DCFFF")).Bold(true)
	sidebarSectionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#BB9AF7")).Bold(true)
	sidebarAccentStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#7AA2F7")).Bold(true)

	todoDoneStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#565F89")).Strikethrough(true)
	todoInProgressStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#E0AF68")).Bold(true)
	todoPendingStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#A9B1D6"))
	todoProgressStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#9ECE6A"))
)

func renderSidebarSectionTitle(title string) string {
	return sidebarSectionStyle.Render(title)
}

// styleTodoLine renders a markdown todo line with strikethrough/dim for done
// items and a warning color for in-progress (`- [~]` or `- [-]`). Also replaces
// status text like [in_progress], [pending], [completed] with emojis.
func styleTodoLine(line string) string {
	trimmed := strings.TrimLeft(line, " \t")
	indent := line[:len(line)-len(trimmed)]

	// Handle status text format: - [status_text] body
	if strings.HasPrefix(trimmed, "- [") {
		closeIdx := strings.Index(trimmed[3:], "]")
		if closeIdx != -1 {
			marker := trimmed[3 : 3+closeIdx]
			body := strings.TrimSpace(trimmed[3+closeIdx+1:])

			// Replace status text with emojis
			switch marker {
			case "completed", "x", "X", "✓":
				return indent + todoDoneStyle.Render("[✓] "+body)
			case "in_progress", "~", "-", "⟳":
				return indent + todoInProgressStyle.Render("[⟳] "+body)
			case "pending", " ", "○":
				return indent + todoPendingStyle.Render("[○] "+body)
			}
		}
	}

	// Standard checkbox format
	prefix, body, ok := splitTodoMarker(trimmed)
	if !ok {
		return line
	}
	switch prefix {
	case "x", "X":
		return indent + todoDoneStyle.Render("[✓] "+body)
	case "~", "-":
		return indent + todoInProgressStyle.Render("[⟳] "+body)
	case " ":
		return indent + todoPendingStyle.Render("[○] "+body)
	default:
		return line
	}
}

func splitTodoMarker(s string) (marker, body string, ok bool) {
	if len(s) < 6 || s[0] != '-' || s[1] != ' ' || s[2] != '[' || s[4] != ']' {
		return "", "", false
	}
	rest := s[5:]
	if len(rest) > 0 && rest[0] == ' ' {
		rest = rest[1:]
	}
	return string(s[3]), rest, true
}

func renderSidebarTodo(todo string, width int) []string {
	raw := strings.Split(todo, "\n")
	styled := make([]string, 0, len(raw)+3)
	done, active, pending := 0, 0, 0

	for _, line := range raw {
		trimmed := strings.TrimLeft(line, " \t")
		marker, _, ok := splitTodoMarker(trimmed)
		if ok {
			switch marker {
			case "x", "X":
				done++
			case "~", "-":
				active++
			case " ":
				pending++
			}
		}

		prefix := "  "
		if ok && (marker == "~" || marker == "-") {
			prefix = todoInProgressStyle.Render("› ")
		}
		styled = append(styled, prefix+styleTodoLine(line))
	}

	total := done + active + pending
	if total == 0 {
		return styled
	}

	summary := ""
	if done > 0 {
		summary += "✓ "
	}
	if active > 0 {
		summary += "⟳ "
	}
	if pending > 0 {
		summary += "○"
	}

	barWidth := width - lipgloss.Width(summary) - 3
	if barWidth > 14 {
		barWidth = 14
	}
	if barWidth < 6 {
		barWidth = 6
	}
	filled := (done*barWidth + total/2) / total
	if filled > barWidth {
		filled = barWidth
	}
	bar := todoProgressStyle.Render(strings.Repeat("━", filled)) + dimStyle.Render(strings.Repeat("━", barWidth-filled))

	lines := []string{bar + " " + hintStyle.Render(summary), ""}
	lines = append(lines, styled...)
	return lines
}

const (
	sidebarMinWidth    = 120
	sidebarColumnWidth = 38
)

func (m *model) applyTheme() {
	if m.config != nil && m.config.Ocode.TUI.Theme != "" {
		m.styles = ApplyThemeColors(m.config.Ocode.TUI.Theme)
	} else {
		m.styles = ApplyThemeColors("tokyonight")
	}
	m.inputThemeApplied = false
	m.applyInputTheme()
}

func (m *model) applyInputTheme() {
	shellMode := m.inputIsShellMode()
	if m.inputThemeApplied && m.inputThemeShellMode == shellMode {
		return
	}
	styles := m.input.Styles()
	promptStyle := m.styles.Header
	if shellMode {
		promptStyle = m.styles.Success
	}

	// Bubble's textarea renders all soft-wrapped segments of the logical cursor
	// line with CursorLine. Placeholder rendering also uses CursorLine for any
	// wrapped placeholder rows that contain text. If CursorLine falls back to the
	// library default, wrapped input/placeholder text can become bright white.
	styles.Focused.Base = m.styles.Text
	styles.Focused.Text = m.styles.Text
	styles.Focused.CursorLine = m.styles.Hint
	styles.Focused.Prompt = promptStyle
	styles.Focused.Placeholder = m.styles.Hint
	styles.Focused.EndOfBuffer = m.styles.Dim
	styles.Focused.LineNumber = m.styles.Dim
	styles.Focused.CursorLineNumber = m.styles.Dim

	styles.Blurred.Base = m.styles.Text
	styles.Blurred.Text = m.styles.Text
	styles.Blurred.CursorLine = m.styles.Hint
	styles.Blurred.Prompt = promptStyle
	styles.Blurred.Placeholder = m.styles.Hint
	styles.Blurred.EndOfBuffer = m.styles.Dim
	styles.Blurred.LineNumber = m.styles.Dim
	styles.Blurred.CursorLineNumber = m.styles.Dim

	m.input.SetStyles(styles)
	m.inputThemeApplied = true
	m.inputThemeShellMode = shellMode
}

func (m *model) toggleSidebar() {
	m.showSidebar = !m.showSidebar
	m.layout()
}

func (m *model) backgroundLatestForegroundBash() bool {
	if m.agent == nil || m.agent.Procs() == nil {
		return false
	}
	id, command, ok := m.agent.Procs().RequestBackgroundLatest()
	if !ok {
		return false
	}
	m.messages = append(m.messages, message{
		role:      roleAssistant,
		text:      hintStyle.Render(fmt.Sprintf("↩ moved bash to background: %s (%s)", id, truncateToWidth(command, 48))),
		transient: true,
	})
	m.rerenderTranscriptAndMaybeScroll()
	return true
}

func (m model) sidebarEnabled() bool {
	return m.showSidebar && m.width >= sidebarMinWidth
}

func (m model) panelWidth() int {
	if m.sidebarEnabled() {
		return m.width - sidebarColumnWidth
	}
	return m.width
}

func (m model) currentModelName() string {
	if m.activeModel != "" {
		return m.activeModel
	}
	if m.config != nil && m.config.Model != "" {
		return m.config.Model
	}

	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].raw != nil && m.messages[i].raw.Model != "" {
			return m.messages[i].raw.Model
		}
	}

	return "no model"
}

func (m *model) getInitialTools() []tool.Tool {
	return []tool.Tool{
		&tool.ReadTool{},
		&tool.WriteTool{Config: m.config},
		&tool.DeleteTool{},
		&tool.GlobTool{},
		&tool.GrepTool{},
		&tool.BashTool{},
		&tool.EditTool{Config: m.config},
		&tool.MultiEditTool{Config: m.config},
		&tool.MultiFileEditTool{Config: m.config},
		&tool.PatchTool{},
		&tool.TodoWriteTool{},
		&tool.SkillTool{},
		&tool.QuestionTool{},
		&tool.WebFetchTool{},
		&tool.WebSearchTool{},
		&tool.RepoCloneTool{},
		&tool.RepoOverviewTool{},
		&tool.ListTool{},
		&tool.LSPTool{},
		&tool.FormatTool{Config: m.config},
	}
}

func (m *model) switchAgent(name string) {
	spec := agent.FindAgentSpec(name)
	if spec == nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Unknown agent: %s", name)})
		return
	}
	for i, spec := range agent.PrimaryAgentSpecs() {
		if spec.Name == name {
			m.currentAgentIdx = i
			break
		}
	}
	if m.agent != nil {
		m.agent.SetSpec(spec)
	}
}

func newModel(sid string, cont bool, yolo bool) model {
	cfg, _ := config.Load()
	agent.ApplyAgentConfig(cfg)
	_ = auth.HydrateEnv()

	// shouldLoad tracks whether the session ID was explicitly provided
	// (via -session flag or -continue) vs auto-generated. We only attempt
	// to load an existing session file when explicitly requested.
	shouldLoad := sid != ""

	if cont {
		sessions, _ := session.List()
		if len(sessions) > 0 {
			sid = sessions[0].ID
			shouldLoad = true
		}
	}

	tmp := model{}
	tools := tmp.getInitialTools()

	var a *agent.Agent
	if cfg != nil && cfg.Model != "" {
		client := agent.NewClient(cfg, cfg.Model)
		a = agent.NewAgent(client, tools, cfg)
		if yolo && a.Permissions() != nil {
			a.Permissions().SetMode(agent.PermissionModeYOLO)
		}
		a.LoadExternalTools(cfg)
	}

	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{GracePeriod: 5 * time.Second})
	if a != nil {
		a.SetSupervisor(sup)
	}

	ta := textarea.New()
	ta.Placeholder = "Ask anything…  (prefix with ! to run a shell command, enter to send, shift+enter for newline, tab autocomplete, ctrl+c clears input, double-esc opens picker / exits shell mode)"
	ta.Focus()
	ta.Prompt = "▍ "
	ta.CharLimit = 8000
	ta.SetHeight(3)
	ta.MaxWidth = 80
	ta.ShowLineNumbers = false
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter"), key.WithHelp("shift+enter", "insert newline"))

	questionInput := textarea.New()
	questionInput.Placeholder = "Type your answer…"
	questionInput.Focus()
	questionInput.Prompt = "↳ "
	questionInput.CharLimit = 8000
	questionInput.SetHeight(2)
	questionInput.MaxWidth = 80
	questionInput.ShowLineNumbers = false
	questionInput.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter"), key.WithHelp("shift+enter", "insert newline"))

	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	vp.SetContent(hintStyle.Render("  ocode v" + version.Version + " — opencode clone · type a message to begin\n"))

	if sid == "" {
		sid = time.Now().Format("2006-01-02-150405")
	}
	tool.SetTodoSession(sid)
	snapshot.Reset()
	tool.ResetTodoState()

	m := model{
		viewport:     vp,
		input:        ta,
		messages:     []message{},
		config:       cfg,
		agent:        a,
		sessionID:    sid,
		showThinking: true,
		showSidebar:  true,
		activeModel: func() string {
			if cfg != nil {
				return cfg.Model
			}
			return ""
		}(),
		thinkingLevelIdx: func() int {
			if cfg != nil {
				return thinkingLevelIndexForBudget(cfg.ThinkingBudget)
			}
			return 0
		}(),
		scrollSpeed:       3,
		inputHistoryIndex: -1,
		workDir: func() string {
			d, _ := os.Getwd()
			return d
		}(),
		compactCh:            make(chan agent.CompactResult, 4),
		compactStartCh:       make(chan struct{}, 4),
		titleCh:              make(chan titleResult, 4),
		deltaCh:              make(chan deltaEvent, 256),
		usageCh:              make(chan usageEvent, 16),
		streamingThinkingIdx: -1,
		questionInput:        questionInput,
		subAgentPermCh:       make(chan subAgentPermRequest),
		subAgentPermMu:       &sync.Mutex{},
		cleanupState:         newModelCleanupState(),
		supervisor:           sup,
	}

	// Set workDir for path-scoped permission checks
	if m.agent != nil && m.agent.Permissions() != nil {
		m.agent.Permissions().SetWorkDir(m.workDir)
	}
	m.wireCompactCallbacks()

	if cfg != nil && cfg.Ocode.TUI.Scroll != 0 {
		m.scrollSpeed = int(cfg.Ocode.TUI.Scroll)
	}

	m.applyTheme()

	workDir := m.workDir
	m.files = newFilesModel(workDir)
	m.git = newGitModel(workDir)
	if cfg != nil {
		m.git.generateCommitMsg = m.makeCommitMsgGenerator(cfg)
		editor := config.ResolveEditor(&cfg.Ocode)
		editorMode := cfg.Ocode.EditorMode
		m.files.SetEditor(editor)
		m.files.SetEditorMode(editorMode)
		m.files.SetEditorOpener(createEditorOpener(
			editor,
			editorMode,
			func() int { return m.width },
			sup,
		))
		m.git.SetEditor(editor)
		m.git.SetEditorOpener(createEditorOpener(
			editor,
			editorMode,
			func() int { return m.width },
			sup,
		))
	}
	m.files.SetSaveEditor(config.SaveEditor)
	m.logViewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.sidebarCache = &sidebarComputeCache{}

	agent.DebugAppend = func(kind, msg string) {
		DebugLog.Append(DebugEntry{Kind: DebugEntryKind(kind), Message: msg})
	}

	if shouldLoad {
		sess, err := session.Load(sid)
		if err == nil {
			m.sessionTitle = sess.Title
			if sess.Title != "" {
				m.titleRequested = true
			}
			m.sessionTelemetry = telemetryFromSessionMetadata(sess.Metadata)
			restoreTodoState(sess.Metadata)
			for _, am := range sess.Messages {
				role := tuiRoleForAgentMessage(am)
				copyMsg := am
				m.messages = append(m.messages, message{role: role, text: displayTextForAgentMessage(am), raw: &copyMsg})
			}
			if len(m.messages) > 0 {
				m.restoredPendingScroll = true
			}
		} else {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Error loading session %s: %v", sid, err)})
		}
	}

	return m
}

func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{textarea.Blink, waitForDebugLog(), waitCompactEvent(m.compactStartCh, m.compactCh), waitTitleEvent(m.titleCh), waitDeltaEvent(m.deltaCh), listenSubAgentPerm(m.subAgentPermCh)}
	if m.agent != nil {
		cmds = append(cmds, listenJobs(m.agent))
	}
	if !agent.RegistryReady() {
		cmds = append(cmds, waitForRegistry())
	}
	return tea.Batch(cmds...)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		tiCmd    tea.Cmd
		vpCmd    tea.Cmd
		popupCmd tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
	case tea.PasteMsg:
		if m.activeTab == tabChat && !m.showPicker && !m.showConnect && !m.showPalette && !m.leaderActive {
			content := msg.Content
			if shortcode, ok := m.shortcodePastedFiles(content); ok {
				content = shortcode
			}
			m.input.InsertString(content)
			m.rawInputLinesDirty = true
			var cmd tea.Cmd
			m, cmd = m.updateSlashPopupState()
			return m, cmd
		}
	case tea.MouseClickMsg:
		if updated, cmd, ok := m.handleMouseAction(msg.Mouse(), true); ok {
			return updated, cmd
		}
	case tea.MouseReleaseMsg:
		if updated, cmd, ok := m.handleMouseAction(msg.Mouse(), false); ok {
			return updated, cmd
		}
	case tea.MouseMotionMsg:
		if updated, cmd, ok := m.handleMouseMotion(msg.Mouse()); ok {
			return updated, cmd
		}
	case tea.MouseWheelMsg:
		scrollSpeed := m.scrollSpeed
		if scrollSpeed < 1 {
			scrollSpeed = 1
		}
		if !m.detail.empty() {
			if !m.mouseOverDetailViewport(msg.Mouse()) {
				return m, nil
			}
			top := &m.detail[len(m.detail)-1]
			if msg.Button == tea.MouseWheelUp {
				top.vp.ScrollUp(scrollSpeed)
				return m, nil
			}
			if msg.Button == tea.MouseWheelDown {
				top.vp.ScrollDown(scrollSpeed)
				return m, nil
			}
		}
		if m.mouseOverSidebar(msg.Mouse()) {
			if msg.Button == tea.MouseWheelUp {
				m.sidebarScroll -= scrollSpeed
			}
			if msg.Button == tea.MouseWheelDown {
				m.sidebarScroll += scrollSpeed
			}
			m.clampSidebarScroll()
			return m, nil
		}
		if m.activeTab == tabFiles {
			if msg.Button == tea.MouseWheelUp {
				m.files.preview.ScrollUp(scrollSpeed)
				return m, nil
			}
			if msg.Button == tea.MouseWheelDown {
				m.files.preview.ScrollDown(scrollSpeed)
				return m, nil
			}
		}
		if m.activeTab == tabGit {
			if msg.Button == tea.MouseWheelUp {
				m.git.diff.ScrollUp(scrollSpeed)
				return m, nil
			}
			if msg.Button == tea.MouseWheelDown {
				m.git.diff.ScrollDown(scrollSpeed)
				return m, nil
			}
		}
		if m.activeTab == tabLog {
			if msg.Button == tea.MouseWheelUp {
				m.logViewport.ScrollUp(scrollSpeed)
				return m, nil
			}
			if msg.Button == tea.MouseWheelDown {
				m.logViewport.ScrollDown(scrollSpeed)
				return m, nil
			}
		}
		if !m.mouseOverTranscriptViewport(msg) {
			return m, nil
		}
		if msg.Button == tea.MouseWheelUp {
			m.viewport.ScrollUp(scrollSpeed)
			return m, nil
		}
		if msg.Button == tea.MouseWheelDown {
			m.viewport.ScrollDown(scrollSpeed)
			return m, nil
		}
	case tea.KeyPressMsg:
		if m.showSlashPopup {
			switch msg.String() {
			case "esc":
				m.closeSlashPopup()
				return m, nil
			case "up":
				if m.slashPopupIndex > 0 {
					m.slashPopupIndex--
					return m, nil
				}
				// fall through to textarea when already at top
			case "down", "tab":
				if len(m.slashPopupItems) == 0 {
					break // fall through to outer handler when nothing to navigate
				}
				if m.slashPopupIndex < len(m.slashPopupItems)-1 {
					m.slashPopupIndex++
				}
				return m, nil
			case "enter":
				if len(m.slashPopupItems) > 0 && m.slashPopupIndex < len(m.slashPopupItems) && !m.inputIsExactSlashCommand() {
					selected := m.slashPopupItems[m.slashPopupIndex]
					m.acceptPopupSuggestion(selected)
					return m, nil
				}
			}
		}
	}

	if m.activeTab == tabChat && !m.showQuestionDialog && m.detail.empty() {
		m.input, tiCmd = m.input.Update(msg)
		m.rawInputLinesDirty = true
		(&m).applyInputTheme()
	}
	if shouldForwardToTranscriptViewport(msg) {
		m.viewport, vpCmd = m.viewport.Update(msg)
	}
	m, popupCmd = m.updateSlashPopupState()

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layout()
		m.files.Resize(m.width, m.height)
		m.git.Resize(m.width, m.height)
		m.layoutLogViewport()
		m.ready = true
		if m.restoredPendingScroll {
			m.rerenderTranscriptAndMaybeScroll()
			m.restoredPendingScroll = false
		}
		m.updatePermButtonRegions()
	case tea.KeyPressMsg:
		// Reset double-esc state on any non-esc keypress
		if m.escPressed && msg.String() != "esc" {
			m.escPressed = false
		}

		// 1. True globals — tab switching, always active
		if handled, newM, cmd := m.handleGlobalTabKeys(msg); handled {
			return newM, cmd
		}

		// 2. Modal overlays — picker, connect, palette, leader
		if handled, newM, cmd := m.handleModalKeys(msg); handled {
			return newM, cmd
		}

		// 3. Per-tab dispatch
		switch m.activeTab {
		case tabChat:
			return m.handleChatKeys(msg, tiCmd, vpCmd)
		case tabFiles:
			if msg.String() == "ctrl+c" {
				return m.handleTabCtrlC()
			}
			if msg.String() == "esc" && !m.filesHasActiveFocus() {
				return m.handleEscKey()
			}
			if msg.String() == "a" && m.files.panel == filesPanelPreview && m.files.previewPath != "" {
				return m, m.filesAddToContext()
			}
			var cmd tea.Cmd
			m.files, cmd = m.files.Update(msg, m.width, m.height)
			return m, cmd
		case tabGit:
			if msg.String() == "ctrl+c" {
				return m.handleTabCtrlC()
			}
			if msg.String() == "esc" && !m.gitHasActiveFocus() {
				return m.handleEscKey()
			}
			var cmd tea.Cmd
			m.git, cmd = m.git.Update(msg, m.panelWidth(), m.height)
			return m, cmd
		case tabLog:
			return m.handleLogKeys(msg)
		}

		// Unreachable, but keep return for safety
		return m, nil

	case leaderTimeoutMsg:
		if m.leaderActive && msg.seq == m.leaderSeq {
			m.leaderActive = false
		}
		return m, nil
	case registryReadyMsg:
		// Registry loaded (or load timed out) — re-render so status bar and
		// sidebar reflect reasoning support. On failure the UI continues with
		// whatever defaults are available.
		if msg.failed {
			DebugLog.Append(DebugEntry{
				Kind:    DebugKindError,
				Message: "models registry failed to load within deadline; continuing with defaults",
			})
		}
		return m, nil
	case debugLogMsg:
		m.logEntries = DebugLog.Snapshot()
		if m.activeTab == tabLog {
			atBottom := m.logViewport.AtBottom() || m.logViewport.TotalLineCount() == 0
			m.refreshLogViewport()
			if atBottom {
				m.logViewport.GotoBottom()
			}
		}
		return m, waitForDebugLog()
	case filesPreviewMsg:
		var cmd tea.Cmd
		m.files, cmd = m.files.Update(msg, m.width, m.height)
		m.filesSel = selectionState{}
		return m, cmd
	case gitStatusMsg, gitRefreshMsg, loadMoreLogMsg:
		var cmd tea.Cmd
		m.git, cmd = m.git.Update(msg, m.panelWidth(), m.height)
		return m, cmd
	case filesAddToContextMsg:
		label := ""
		if msg.startLine >= 0 && msg.endLine > msg.startLine {
			label = fmt.Sprintf(" (lines %d-%d)", msg.startLine+1, msg.endLine)
		}
		fileCtx := fmt.Sprintf("\n--- File: %s%s ---\n%s\n", msg.path, label, msg.content)
		m.messages = append(m.messages, message{
			role: roleAssistant,
			text: fmt.Sprintf("\u2b06 Added context from %s%s", msg.path, label),
			raw: &agent.Message{
				Role:    "system",
				Content: fileCtx,
			},
		})
		m.rerenderTranscriptAndMaybeScroll()
		m.saveSession()
		return m, nil
	case fileListCacheMsg:
		m.fileListCache = msg.items
		m, _ = m.updateSlashPopupState()
		return m, nil
	case fileSearchFinishedMsg:
		if msg.err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Error processing files: %v", msg.err)})
		} else {
			m.messages = append(m.messages, msg.messages...)
			userMsg := message{role: roleUser, text: msg.processedText}
			if len(msg.images) > 0 {
				raw := agent.Message{Role: "user", Content: msg.processedText, Images: msg.images}
				userMsg.text = displayTextForAgentMessage(raw)
				userMsg.raw = &raw
			}
			m.messages = append(m.messages, userMsg)
			if m.agent != nil {
				m.agent.ResetSubagentDispatch()
			}
			agent.DebugAppendf("SESSION", "appended user msg to m.messages (total=%d, roleCounts: user=%d asst=%d tool=%d)", len(m.messages), countRole(m.messages, roleUser), countRole(m.messages, roleAssistant), countToolMsgs(m.messages))
		}
		m.rerenderTranscriptAndMaybeScroll()
		m.saveSession()
		if m.agent != nil {
			return m, m.askAgent()
		}
		m.messages = append(m.messages, message{role: roleAssistant, text: hintStyle.Render("(no llm configured, check opencode.json)")})
		m.rerenderTranscriptAndMaybeScroll()
	case authFinishedMsg:
		if msg.err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Login failed: %v", msg.err)})
		} else {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Google Login successful! Token received."})
			os.Setenv("GOOGLE_OAUTH_ACCESS_TOKEN", msg.token)
			if m.config != nil && m.config.Model != "" {
				client := agent.NewClient(m.config, m.config.Model)
				next := agent.NewAgent(client, m.getInitialTools(), m.config)
				return m, m.replaceAgent(next)
			}
		}
		m.renderTranscript()
	case statusMsg:
		m.messages = append(m.messages, message{role: roleAssistant, text: msg.text})
		m.rerenderTranscriptAndMaybeScroll()
	case usageSummaryMsg:
		if msg.err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Error querying usage: %v", msg.err)})
		} else {
			m.messages = append(m.messages, message{role: roleAssistant, text: msg.text})
		}
		m.rerenderTranscriptAndMaybeScroll()
	case connectOAuthFinishedMsg:
		if m.connect == nil {
			return m, nil
		}
		if msg.err != nil {
			m.connect.message = fmt.Sprintf("OAuth failed: %v", msg.err)
			m.connect.messageOK = false
			m.connect.stage = connectStageMessage
			m.viewport.GotoBottom()
			return m, nil
		}
		if err := auth.Set(msg.provider, msg.cred); err != nil {
			m.connect.message = fmt.Sprintf("OAuth succeeded but failed to save: %v", err)
			m.connect.messageOK = false
			m.connect.stage = connectStageMessage
			m.viewport.GotoBottom()
			return m, nil
		}
		m.rebuildAgentClient()
		label := msg.provider
		if msg.cred.Account != "" {
			label = msg.provider + " (" + msg.cred.Account + ")"
		}
		m.connect.message = fmt.Sprintf("%s OAuth complete. Testing connection…", label)
		m.connect.messageOK = true
		m.connect.stage = connectStageMessage
		m.viewport.GotoBottom()
		return m, m.testConnection(msg.provider)
	case connectTestFinishedMsg:
		if m.connect == nil {
			return m, nil
		}
		if msg.err != nil {
			m.connect.message = fmt.Sprintf("%s\n\n⚠ Connection test failed: %v", m.connect.message, msg.err)
			m.connect.messageOK = false
		} else {
			m.connect.message = fmt.Sprintf("%s\n\n✓ Connection verified.", m.connect.message)
		}
	case shellFinishedMsg:
		content := msg.output
		if msg.err != nil {
			if content == "" {
				content = fmt.Sprintf("Command failed: %v", msg.err)
			} else {
				content = fmt.Sprintf("Command failed (%v). Output:\n%s", msg.err, content)
			}
		} else if strings.TrimSpace(content) == "" {
			content = "Command executed successfully (no output)."
		}
		m.appendAgentMessage(agent.Message{
			Role:    "tool",
			ToolID:  msg.toolCallID,
			Content: content,
		})
		m.rerenderTranscriptAndMaybeScroll()
		m.saveSession()
	case []agent.Message:
		for _, am := range msg {
			m.appendAgentMessage(am)
		}
		m.rerenderTranscriptAndMaybeScroll()
		m.saveSession()
		if len(msg) > 0 && (msg[len(msg)-1].Role == "tool" || (msg[len(msg)-1].Role == "assistant" && len(msg[len(msg)-1].ToolCalls) > 0)) {
			stop := false
			for _, am := range msg {
				if am.Role == "tool" && strings.HasPrefix(am.Content, tool.SentinelPermissionAsk) {
					stop = true
					break
				}
			}
			if !stop {
				last := msg[len(msg)-1]
				if last.Role == "assistant" {
					for _, tc := range last.ToolCalls {
						if tc.Function.Name == "question" {
							stop = true
							break
						}
					}
				}
			}
			if !stop {
				return m, m.askAgent()
			}
		}
	case pickerFilterApplyMsg:
		if msg.seq == m.pickerFilterSeq {
			prevEmpty := m.pickerFilter == ""
			m.pickerFilter = msg.filter
			m.pickerIndex = 0
			// When filter is cleared for sessions, go back to paginated view
			if m.pickerKind == "session" && m.pickerSessionRefs != nil && !prevEmpty && msg.filter == "" {
				m.pickerSessionPage = 1
				m.pickerSessionMore = len(m.pickerSessionRefs) > sessionPickerPageSize
				m.rebuildSessionPickerItems()
			}
		}
	case ctrlCResetMsg:
		m.ctrlCPressed = false
	case cleanupRequestMsg:
		m.cleanupCurrentSession()
		return m, tea.Quit
	case dotTickMsg:
		if m.streaming || m.lastActivity.LLMRunning || m.compacting || len(m.lastActivity.ActiveTools) > 0 || !m.detail.empty() || time.Now().Before(m.tokenBlinkUntil) || m.agent != nil && (m.agent.Procs() != nil && m.agent.Procs().RunningCount() > 0 || m.agent.Runs() != nil && m.agent.Runs().RunningCount() > 0) {
			m.dotFrame = (m.dotFrame + 1) % 4
			// Refresh live detail view content.
			if !m.detail.empty() {
				m.refreshTopDetailView()
			}
			return m, tea.Tick(400*time.Millisecond, func(time.Time) tea.Msg { return dotTickMsg{} })
		}
	case streamStartedMsg:
		m.streaming = true
		m.cancelStream = msg.cancel
		m.lastActivity = agent.ActivitySnapshot{LLMRunning: true}
		m.streamStartedAt = time.Now()
		m.streamEndedAt = time.Time{}
		m.streamTokenEstimate = 0
		m.streamThinkingChars = 0
		m.streamOutputChars = 0
		m.tokenBlinkUntil = time.Time{}
		m.dotFrame = 0
		if !m.activityRowReserved {
			m.activityRowReserved = true
			m.layout()
		}
		cmd := tea.Tick(400*time.Millisecond, func(time.Time) tea.Msg { return dotTickMsg{} })
		if m.agent != nil {
			return m, tea.Batch(listenActivity(m.agent.Activity()), cmd)
		}
		return m, cmd
	case activityUpdateMsg:
		if m.agent == nil || msg.tracker != m.agent.Activity() {
			return m, nil
		}
		// If the stream was already cancelled/stopped, ignore stale
		// LLMRunning=true updates so the "⟳ LLM" indicator doesn't stay
		// visible indefinitely after escape. The step goroutine may not
		// have had a chance to call setLLMRunning(false) yet, and the
		// activity tracker's notify channel may still contain a snapshot
		// from before the cancellation.
		if !m.streaming && msg.snap.LLMRunning {
			if m.agent != nil {
				return m, listenActivity(m.agent.Activity())
			}
			return m, nil
		}
		m.lastActivity = msg.snap
		if !m.activityRowReserved {
			m.activityRowReserved = true
			m.layout()
		}
		if m.agent != nil {
			return m, listenActivity(m.agent.Activity())
		}
	case jobCompletedMsg:
		if msg.agent != m.agent {
			return m, nil
		}
		ev := msg.ev
		// For agent runs that were synchronous, the parent agent already
		// received the full result via the task tool's return value and the
		// LLM has already responded to it. Re-injecting the completion as a
		// fresh user/system message causes infinite re-dispatch loops with
		// small models. Just listen for the next job and bail.
		if ev.Kind == "agent" && !ev.Background {
			if m.agent != nil {
				return m, listenJobs(m.agent)
			}
			return m, nil
		}
		var header string
		if ev.Kind == "agent" {
			header = fmt.Sprintf("[Background agent %s (%s) %s]", ev.ID, ev.Name, ev.Status)
		} else {
			header = fmt.Sprintf("[Background process %s %s]", ev.ID, ev.Status)
		}
		body := header + "\n" + ev.Result
		// Use the system role so the model treats this as an out-of-band
		// notification, not a fresh user instruction. This makes it far less
		// likely to re-dispatch the same task in response to its own
		// completion notice.
		injected := message{
			role: roleUser,
			text: body,
			raw:  &agent.Message{Role: "system", Content: body},
		}
		if m.streaming {
			m.pendingJobMsgs = append(m.pendingJobMsgs, injected)
		} else {
			m.messages = append(m.messages, message{
				role:      roleAssistant,
				text:      hintStyle.Render("↩ " + header + " — resuming"),
				transient: true,
			})
			m.messages = append(m.messages, injected)
			m.rerenderTranscriptAndMaybeScroll()
			cmd := m.askAgent()
			if m.agent != nil {
				return m, tea.Batch(cmd, listenJobs(m.agent))
			}
			return m, cmd
		}
		if m.agent != nil {
			return m, listenJobs(m.agent)
		}
		return m, nil
	case streamMsgEvent:
		m.appendAgentMessage(msg.msg)
		if m.activeTab != tabChat {
			m.chatUnread = true
		}
		m.rerenderTranscriptAndMaybeScroll()
		return m, waitStreamEvent(msg.ch, msg.errCh, msg.cancel)
	case streamDoneMsg:
		if !m.streaming {
			return m, nil
		}
		m.streaming = false
		m.cancelStream = nil
		m.lastActivity = agent.ActivitySnapshot{}
		m.streamEndedAt = time.Now()
		m.streamWasInterrupted = msg.err != nil
		// Reset so the next turn's first reasoning delta starts a fresh
		// thinking block instead of appending into the prior turn's buffer.
		m.streamingThinkingIdx = -1
		if dropped := atomic.SwapUint64(&m.deltaDrops, 0); dropped > 0 {
			agent.DebugAppendf("stream", "dropped %d reasoning deltas under backpressure", dropped)
		}
		if msg.err != nil {
			agent.DebugAppendf("LLM", "stream done with error: %v", msg.err)
		} else {
			agent.DebugAppendf("LLM", "stream done OK (duration=%s)", time.Since(m.streamStartedAt).Round(time.Millisecond))
		}
		m.layout()
		m.saveSession()
		if msg.err == nil && m.agent != nil {
			agentMsgs, uiIdx := m.buildAgentMessagesSnapshot()
			// Only update the pending uiIdx mapping if the agent actually
			// started a compaction goroutine. Otherwise an earlier in-flight
			// compaction's eventual OnCompact would splice using *this*
			// turn's mapping — silently deleting the wrong messages.
			if m.agent.MaybeCompactAsync(agentMsgs) {
				m.pendingCompactUIIdx = uiIdx
			}
		}
		if msg.err != nil {
			if errors.Is(msg.err, context.Canceled) {
				m.lastRetryableLLMErr = ""
				return m, nil
			}
			errorText := fmt.Sprintf("Error: %v", msg.err)
			if isRetryableLLMError(msg.err) {
				m.lastRetryableLLMErr = errorText
			} else {
				m.lastRetryableLLMErr = ""
			}
			m.messages = append(m.messages, message{role: roleAssistant, text: errorText})
			m.rerenderTranscriptAndMaybeScroll()
		} else {
			m.lastRetryableLLMErr = ""
			if len(m.pendingJobMsgs) > 0 && m.agent != nil {
				m.messages = append(m.messages, message{
					role:      roleAssistant,
					text:      hintStyle.Render("↩ background job(s) completed — resuming"),
					transient: true,
				})
				m.messages = append(m.messages, m.pendingJobMsgs...)
				m.pendingJobMsgs = nil
				m.rerenderTranscriptAndMaybeScroll()
				return m, m.askAgent()
			}
			if len(m.queuedInputs) > 0 && m.agent != nil {
				// Concatenate all queued inputs into a single combined message.
				parts := make([]string, 0, len(m.queuedInputs))
				for _, q := range m.queuedInputs {
					parts = append(parts, strings.TrimSpace(q))
				}
				text := strings.Join(parts, "\n---\n")
				m.queuedInputs = nil
				m.layout()
				m.maybeScrollTranscriptToBottom()
				return m, m.processFileReferences(text)
			}
		}
	case compactStartedMsg:
		m.compacting = true
		m.lastCompactErr = nil
		m.layout()
		return m, tea.Batch(
			waitCompactEvent(m.compactStartCh, m.compactCh),
			tea.Tick(400*time.Millisecond, func(time.Time) tea.Msg { return dotTickMsg{} }),
		)
	case compactFinishedMsg:
		m.compacting = false
		resume := m.pendingCompactResume
		m.pendingCompactResume = false
		if msg.result.Err != nil {
			m.lastCompactErr = msg.result.Err
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("⚠ Compaction failed: %v (conversation continues uncompacted)", msg.result.Err)})
			m.renderTranscript()
		} else if msg.result.OK {
			if m.applyCompactionResult(msg.result, m.pendingCompactUIIdx) {
				m.pendingCompactUIIdx = nil
				m.rerenderTranscriptAndMaybeScroll()
				m.saveSession()
			}
		}
		m.layout()
		if resume && m.agent != nil {
			return m, m.askAgent()
		}
		return m, waitCompactEvent(m.compactStartCh, m.compactCh)
	case titleGeneratedMsg:
		// Drop stale results from goroutines started before /new or /title clear.
		if msg.gen == m.titleGen && msg.title != "" && m.sessionTitle == "" {
			m.sessionTitle = truncateTitle(msg.title, maxExplicitTitleLen)
			m.saveSession()
		}
		return m, waitTitleEvent(m.titleCh)
	case deltaMsg:
		m.applyThinkingDelta(msg.kind, msg.text)
		return m, waitDeltaEvent(m.deltaCh)
	case usageMsg:
		if msg.outputTokens > 0 {
			m.streamFinalOutputTokens = msg.outputTokens
		}
		// Note: sessionTelemetry is populated exclusively via addMessage
		// (called when the message is finalized in appendAgentMessage).
		// Do NOT set sessionTelemetry.inputTokens here — it would be
		// double-counted when addMessage adds the same Usage data later.
		return m, nil
	case subAgentPermAskMsg:
		// A sub-agent tool call needs a permission decision. Reuse the same
		// permission dialog the main agent uses. The sub-agent goroutine is
		// blocked on resp.respCh until handlePermissionChoice answers it.
		req := msg.req
		m.pendingSubAgentResp = msg.respCh
		m.showPermDialog = true
		m.activeTab = tabChat
		m.chatUnread = false
		m.pendingPermission = req
		m.pendingToolName = req.ToolName
		m.pendingToolArgs = req.Args
		m.pendingToolCallID = ""
		m.messages = append(m.messages, message{role: roleAssistant, text: "↳ sub-agent: " + permissionRequestSummary(req)})
		m.rerenderTranscriptAndMaybeScroll()
		return m, nil
	case editorFinishedMsg:
		m.layout()
		if msg.err != nil {
			if m.activeTab == tabFiles {
				m.files.statusMsg = fmt.Sprintf("Editor error: %v", msg.err)
				return m, nil
			}
			if m.activeTab == tabGit {
				m.git.statusMsg = fmt.Sprintf("Editor error: %v", msg.err)
				return m, nil
			}
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Editor error: %v", msg.err)})
		} else if msg.content != "" {
			m.input.SetValue(msg.content)
		}
		if m.activeTab == tabFiles {
			return m, m.files.refreshPreviewCmd()
		}
		if m.activeTab == tabGit {
			m.git.statusMsg = "editor closed"
			return m, m.git.cmdRefresh()
		}
	case errorMsg:
		if msg != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Error: %v", error(msg))})
			m.rerenderTranscriptAndMaybeScroll()
		}
	}

	return m, tea.Batch(tiCmd, vpCmd, popupCmd)
}

// handleGlobalTabKeys handles tab-switching keys (1-4, alt+[/], ctrl+shift+[/])
// regardless of the active tab. Returns (true, ...) when a key is consumed.
func (m model) handleGlobalTabKeys(msg tea.KeyPressMsg) (bool, tea.Model, tea.Cmd) {
	if m.showQuestionDialog {
		return false, m, nil
	}
	switch msg.String() {
	case "1":
		if m.activeTab == tabChat {
			return false, m, nil
		}
		m.activeTab = tabChat
		m.chatUnread = false
		return true, m, nil
	case "2":
		if m.activeTab == tabChat {
			return false, m, nil
		}
		m.activeTab = tabFiles
		return true, m, nil
	case "3":
		if m.activeTab == tabChat {
			return false, m, nil
		}
		m.activeTab = tabGit
		return true, m, nil
	case "4":
		if m.activeTab == tabChat {
			return false, m, nil
		}
		m.activeTab = tabLog
		m.refreshLogViewport()
		return true, m, nil
	case "alt+[", "ctrl+shift+[":
		m.activeTab = (m.activeTab - 1 + tabCount) % tabCount
		if m.activeTab == tabChat {
			m.chatUnread = false
		}
		if m.activeTab == tabLog {
			m.refreshLogViewport()
		}
		return true, m, nil
	case "alt+]", "ctrl+shift+]":
		m.activeTab = (m.activeTab + 1) % tabCount
		if m.activeTab == tabChat {
			m.chatUnread = false
		}
		if m.activeTab == tabLog {
			m.refreshLogViewport()
		}
		return true, m, nil
	}
	return false, m, nil
}

// handleModalKeys handles overlay dialogs (picker, connect, palette, leader)
// that take precedence over any active tab. Returns (true, ...) if consumed.
func (m model) handleModalKeys(msg tea.KeyPressMsg) (bool, tea.Model, tea.Cmd) {
	keyStr := msg.String()

	if m.showPicker {
		switch keyStr {
		case "esc":
			m.closePicker()
			return true, m, nil
		case "up":
			if m.pickerIndex > 0 {
				m.pickerIndex--
				for m.pickerIndex > 0 && m.pickerIndex < len(m.pickerIsHeader) && m.pickerIsHeader[m.pickerIndex] {
					m.pickerIndex--
				}
			}
			return true, m, nil
		case "down":
			items, _ := m.pickerVisibleItems()
			if m.pickerIndex < len(items)-1 {
				m.pickerIndex++
				for m.pickerIndex < len(items)-1 && m.pickerIndex < len(m.pickerIsHeader) && m.pickerIsHeader[m.pickerIndex] {
					m.pickerIndex++
				}
			}
			// Infinite scroll: trigger load more when within 5 items of bottom
			if m.pickerKind == "session" && m.pickerSessionMore {
				if m.pickerIndex >= len(m.pickerItems)-5 {
					m.loadMoreSessions()
				}
			}
			return true, m, nil
		case "enter":
			isFiltered := m.pickerKind == "model" && m.pickerFilter != ""
			if !isFiltered && m.pickerIndex < len(m.pickerIsHeader) && m.pickerIsHeader[m.pickerIndex] {
				return true, m, nil
			}
			newM, cmd := m.selectPickerIndex(m.pickerIndex)
			return true, newM, cmd
		case "backspace":
			if len(m.pickerFilterPending) > 0 {
				m.pickerFilterPending = m.pickerFilterPending[:len(m.pickerFilterPending)-1]
				m.pickerFilterSeq++
				seq := m.pickerFilterSeq
				pending := m.pickerFilterPending
				return true, m, tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
					return pickerFilterApplyMsg{seq: seq, filter: pending}
				})
			}
			// When filter is fully cleared for sessions, go back to paginated view
			if m.pickerKind == "session" && m.pickerSessionRefs != nil {
				m.pickerSessionPage = 1
				m.pickerSessionMore = len(m.pickerSessionRefs) > sessionPickerPageSize
				m.rebuildSessionPickerItems()
			}
			return true, m, nil
		}
		if keyStr == "f" && m.pickerFilter == "" && m.pickerKind == "model" {
			items, values := m.pickerVisibleItems()
			isSelectable := len(m.pickerIsHeader) == 0 || (m.pickerIndex < len(m.pickerIsHeader) && !m.pickerIsHeader[m.pickerIndex])
			if m.pickerIndex < len(items) && m.pickerIndex < len(values) && isSelectable {
				modelID := values[m.pickerIndex]
				if config.IsFavorite(modelID) {
					_ = config.RemoveFavoriteModel(modelID)
				} else {
					_ = config.SaveFavoriteModel(modelID)
				}
				m.openModelPicker()
				return true, m, nil
			}
			return true, m, nil
		}
		if len(msg.Text) > 0 {
			// When filtering sessions, load all sessions so the filter works globally
			if m.pickerKind == "session" && m.pickerSessionMore && m.pickerFilterPending == "" {
				m.loadAllSessions()
			}
			m.pickerFilterPending += msg.Text
			m.pickerFilterSeq++
			seq := m.pickerFilterSeq
			pending := m.pickerFilterPending
			return true, m, tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
				return pickerFilterApplyMsg{seq: seq, filter: pending}
			})
		}
		return true, m, nil
	}

	if m.showConnect {
		newM, cmd := m.updateConnectDialog(msg)
		return true, newM, cmd
	}

	if m.showPalette {
		if keyStr == "esc" || keyStr == "ctrl+p" {
			m.showPalette = false
			return true, m, nil
		}
		if keyStr == "enter" {
			m.showPalette = false
			newM, cmd := m.handleCommand(m.paletteInput)
			return true, newM, cmd
		}
		if keyStr == "backspace" {
			if len(m.paletteInput) > 0 {
				m.paletteInput = m.paletteInput[:len(m.paletteInput)-1]
			}
			return true, m, nil
		}
		if len(msg.Text) > 0 {
			m.paletteInput += msg.Text
		}
		return true, m, nil
	}

	if m.leaderActive {
		m.leaderActive = false

		key := keyStr
		if m.config != nil {
			if cmd, ok := m.config.Ocode.TUI.Keybinds[key]; ok {
				newM, c := m.handleCommand(cmd)
				return true, newM, c
			}
		}

		switch key {
		case "s":
			m.toggleSidebar()
			return true, m, nil
		case "u":
			newM, c := m.handleCommand("/undo")
			return true, newM, c
		case "r":
			newM, c := m.handleCommand("/redo")
			return true, newM, c
		case "n":
			newM, c := m.handleCommand("/new")
			return true, newM, c
		case "l":
			newM, c := m.handleCommand("/session")
			return true, newM, c
		case "c":
			newM, c := m.handleCommand("/compact")
			return true, newM, c
		case "y":
			if m.sessionID != "" {
				_ = clipboard.WriteAll(m.sessionID)
			}
			return true, m, nil
		case "q":
			m.cleanupCurrentSession()
			return true, m, tea.Quit
		}
		return true, m, nil
	}

	return false, m, nil
}

// handleChatKeys handles all chat-tab-specific key bindings. tiCmd and vpCmd
// are forwarded from the outer Update so chat's "enter" (empty input) batch
// can still flush textarea/viewport updates.
func (m model) handleChatKeys(msg tea.KeyPressMsg, tiCmd, vpCmd tea.Cmd) (tea.Model, tea.Cmd) {
	keyStr := msg.String()

	// Clear the first-line-up notice on any key that isn't "up".
	if keyStr != "up" {
		m.inputAtFirstLineUpNotice = false
	}

	if m.showPermDialog {
		switch keyStr {
		case "y", "n", "a", "t":
			m.showPermDialog = false
			cmd := m.handlePermissionChoice(keyStr)
			m.input.Reset()
			m.rerenderTranscriptAndMaybeScroll()
			m.saveSession()
			return m, cmd
		}
	}

	if m.showQuestionDialog {
		return m.handleQuestionKeys(msg, tiCmd, vpCmd)
	}

	// Route j/k/scroll inside a detail view before normal chat keys.
	if !m.detail.empty() {
		switch keyStr {
		case "j", "down":
			m.detail[len(m.detail)-1].vp.ScrollDown(m.scrollSpeed)
			return m, nil
		case "k", "up":
			m.detail[len(m.detail)-1].vp.ScrollUp(m.scrollSpeed)
			return m, nil
		case "ctrl+g":
			top := m.detail[len(m.detail)-1]
			if top.kind == detailAgentRun {
				m.openProcessListForRun(top.runPath)
				return m, nil
			}
		case "esc":
			// While a detail view is open: if the user has live agent work in
			// flight (streaming or running sub-agents), Esc cancels that first
			// so the gesture matches its meaning on the chat tab; otherwise
			// pop the detail card.
			if m.hasActiveAgentWork() {
				return m.handleEscKey()
			}
			m.detail.pop()
			return m, nil
		}
	}

	// ctrl+a toggles keyboard focus on the agent strip (only when runs exist).
	if keyStr == "ctrl+a" && m.detail.empty() {
		if m.agentStripRunCount() == 0 {
			return m, nil
		}
		m.agentStripFocused = !m.agentStripFocused
		if m.agentStripFocused {
			m.clampAgentStrip()
		}
		return m, nil
	}

	// When the agent strip has focus, route navigation keys to it.
	if m.agentStripFocused && m.detail.empty() && !m.showPermDialog {
		switch keyStr {
		case "j", "down", "k", "up":
			if keyStr == "j" || keyStr == "down" {
				m.agentStripSelected++
			} else {
				m.agentStripSelected--
			}
			m.clampAgentStrip()
			return m, nil
		case "enter":
			runs := m.agent.Runs().Snapshot()
			if m.agentStripSelected >= 0 && m.agentStripSelected < len(runs) {
				m.openAgentDetail(runs[m.agentStripSelected].ID)
			}
			m.agentStripFocused = false
			return m, nil
		case "esc":
			m.agentStripFocused = false
			return m, nil
		}
	}

	switch keyStr {
	case "ctrl+g":
		m.openProcessList()
		return m, nil
	case "ctrl+p":
		m.showPalette = !m.showPalette
		m.paletteInput = ""
		return m, nil
	case "up":
		// If already in history mode, continue navigating history directly.
		if m.inputHistoryIndex != -1 {
			if len(m.inputHistory) > 0 && m.inputHistoryIndex > 0 {
				m.inputHistoryIndex--
			}
			if len(m.inputHistory) > 0 {
				m.input.SetValue(m.inputHistory[m.inputHistoryIndex])
			}
			return m, nil
		}

		// Multi-line: navigate within input first.
		value := m.input.Value()
		if value != "" && m.input.LineCount() > 1 && m.input.Line() > 0 {
			m.input.CursorUp()
			m.inputAtFirstLineUpNotice = false
			return m, nil
		}

		// First press at first line: notice. Second press: enter history.
		if value != "" && !m.inputAtFirstLineUpNotice {
			m.inputAtFirstLineUpNotice = true
			return m, nil
		}
		if value != "" && m.inputAtFirstLineUpNotice {
			m.unsavedInput = value
			m.inputAtFirstLineUpNotice = false
		}

		if len(m.queuedInputs) > 0 && m.input.Value() == "" {
			last := m.queuedInputs[len(m.queuedInputs)-1]
			m.queuedInputs = m.queuedInputs[:len(m.queuedInputs)-1]
			m.input.SetValue(last)
			m.layout()
			return m, nil
		}
		if len(m.inputHistory) == 0 {
			break
		}
		if m.inputHistoryIndex == -1 {
			m.inputHistoryIndex = len(m.inputHistory) - 1
		} else if m.inputHistoryIndex > 0 {
			m.inputHistoryIndex--
		}
		m.input.SetValue(m.inputHistory[m.inputHistoryIndex])
		return m, nil
	case "down":
		if len(m.inputHistory) == 0 || m.inputHistoryIndex == -1 {
			break
		}
		if m.inputHistoryIndex < len(m.inputHistory)-1 {
			m.inputHistoryIndex++
			m.input.SetValue(m.inputHistory[m.inputHistoryIndex])
		} else {
			m.inputHistoryIndex = -1
			if m.unsavedInput != "" {
				m.input.SetValue(m.unsavedInput)
				m.unsavedInput = ""
			} else {
				m.input.SetValue("")
			}
		}
		return m, nil
	case "ctrl+t":
		if m.config != nil && agent.ModelSupportsThinking(m.config.Model) {
			m.thinkingLevelIdx = (m.thinkingLevelIdx + 1) % len(thinkingBudgetLevels)
			m.config.ThinkingBudget = thinkingBudgetLevels[m.thinkingLevelIdx]
			if err := config.SaveLastThinkingBudget(m.config.ThinkingBudget); err != nil {
				log.Printf("save last thinking budget: %v", err)
			}
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("thinking: %s", thinkingBudgetLabels[m.thinkingLevelIdx]), transient: true})
			m.rerenderTranscriptAndMaybeScroll()
		}
		return m, nil
	case "ctrl+b":
		if m.backgroundLatestForegroundBash() {
			return m, nil
		}
		m.messages = append(m.messages, message{role: roleAssistant, text: hintStyle.Render("↩ no running bash command to move to background"), transient: true})
		m.rerenderTranscriptAndMaybeScroll()
		return m, nil
	case "ctrl+o":
		return m.handleCommand("/yolo")
	case "ctrl+y":
		return m.retryLastLLMError()
	case "ctrl+x":
		m.leaderActive = true
		m.leaderSeq++
		timeout := 2000
		if m.config != nil && m.config.Ocode.TUI.LeaderTimeout != 0 {
			timeout = m.config.Ocode.TUI.LeaderTimeout
		}
		seq := m.leaderSeq
		return m, tea.Tick(time.Duration(timeout)*time.Millisecond, func(time.Time) tea.Msg {
			return leaderTimeoutMsg{seq: seq}
		})
	case "esc":
		return m.handleEscKey()
	case "ctrl+c":
		if strings.TrimSpace(m.input.Value()) != "" {
			m.input.Reset()
			m.inputHistoryIndex = -1
			m.unsavedInput = ""
			m.ctrlCPressed = false
			m.closeSlashPopup()
			return m, nil
		}
		if m.ctrlCPressed {
			m.cleanupCurrentSession()
			return m, tea.Quit
		}
		m.ctrlCPressed = true
		return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg { return ctrlCResetMsg{} })
	case "shift+tab":
		if m.agentStripRunCount() > 0 && m.detail.empty() {
			m.agentStripFocused = !m.agentStripFocused
			if m.agentStripFocused {
				m.clampAgentStrip()
			}
			return m, nil
		}
		m.cycleAgentMode()
		return m, nil
	case "tab":
		current := m.input.Value()
		if strings.HasPrefix(current, "/") {
			trimmed := strings.TrimSpace(current)
			if trimmed == "/models" || strings.HasPrefix(trimmed, "/models ") || trimmed == "/model" || strings.HasPrefix(trimmed, "/model ") {
				m.openModelPicker()
				return m, nil
			}
			if trimmed == "/themes" || strings.HasPrefix(trimmed, "/themes ") || trimmed == "/theme" || strings.HasPrefix(trimmed, "/theme ") {
				m.openThemePicker()
				return m, nil
			}

			suggestions := autocompleteSlashInput(&m, current)
			if len(suggestions) == 0 {
				return m, nil
			}

			if strings.HasSuffix(current, " ") {
				m.input.SetValue(strings.TrimSpace(current) + " " + suggestions[0])
				return m, nil
			}

			m.input.SetValue(suggestions[0])
			return m, nil
		}
		m.cycleAgentMode()
		return m, nil
	case "enter":
		text := strings.TrimSpace(m.input.Value())
		if text == "" {
			return m, tea.Batch(tiCmd, vpCmd)
		}

		if !strings.HasPrefix(text, "/") && !strings.HasPrefix(text, "!") {
			if len(m.inputHistory) == 0 || m.inputHistory[len(m.inputHistory)-1] != text {
				m.inputHistory = append(m.inputHistory, text)
			}
		}
		m.inputHistoryIndex = -1
		m.unsavedInput = ""

		if strings.HasPrefix(text, "/") {
			m.closeSlashPopup()
			return m.handleCommand(text)
		}

		if strings.HasPrefix(text, "!") {
			m.input.Reset()
			cmdText := strings.TrimPrefix(text, "!")
			toolCallID := fmt.Sprintf("shell-%d", time.Now().UnixNano())
			argsJSON, _ := json.Marshal(map[string]string{"command": cmdText})
			tc := agent.ToolCall{ID: toolCallID, Type: "function"}
			tc.Function.Name = "bash"
			tc.Function.Arguments = string(argsJSON)
			m.appendAgentMessage(agent.Message{
				Role:      "assistant",
				ToolCalls: []agent.ToolCall{tc},
			})
			m.rerenderTranscriptAndMaybeScroll()
			return m, m.runCapturedShell(cmdText, m.workDir, toolCallID)
		}

		if m.streaming {
			m.queuedInputs = append(m.queuedInputs, text)
			m.input.Reset()
			m.layout()
			m.maybeScrollTranscriptToBottom()
			return m, nil
		}

		var pendingToolCallID string
		if len(m.messages) > 0 {
			last := m.messages[len(m.messages)-1]
			if last.raw != nil && len(last.raw.ToolCalls) > 0 {
				for _, tc := range last.raw.ToolCalls {
					if tc.Function.Name == "question" {
						pendingToolCallID = tc.ID
						break
					}
				}
			}
		}

		if pendingToolCallID != "" {
			m.messages = append(m.messages, message{
				role: roleAssistant,
				text: fmt.Sprintf("✅ tool result: %s", text),
				raw: &agent.Message{
					Role:    "tool",
					Content: text,
					ToolID:  pendingToolCallID,
				},
			})
			m.input.Reset()
			m.rerenderTranscriptAndMaybeScroll()
			m.saveSession()
			return m, m.askAgent()
		}

		if m.showPermDialog {
			choice := strings.ToLower(strings.TrimSpace(text))
			m.showPermDialog = false
			cmd := m.handlePermissionChoice(choice)
			m.input.Reset()
			m.rerenderTranscriptAndMaybeScroll()
			m.saveSession()
			return m, cmd
		}

		m.input.Reset()
		return m, m.processFileReferences(text)
	}

	return m, tea.Batch(tiCmd, vpCmd)
}

// handleLogKeys handles key bindings for the log tab.
func (m model) handleLogKeys(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		m.logViewport.ScrollDown(1)
	case "k", "up":
		m.logViewport.ScrollUp(1)
	case "c":
		DebugLog.Clear()
		m.logEntries = nil
		m.logSearch = ""
		m.refreshLogViewport()
	case "esc":
		// If the LLM is streaming, cancel the stream even from the log tab.
		if m.streaming {
			return m.handleEscKey()
		}
		m.logSearch = ""
		m.refreshLogViewport()
	case "backspace":
		if len(m.logSearch) > 0 {
			runes := []rune(m.logSearch)
			m.logSearch = string(runes[:len(runes)-1])
			m.refreshLogViewport()
		}
	case "1":
		m.toggleLogKind(DebugKindLLM)
	case "2":
		m.toggleLogKind(DebugKindTool)
	case "3":
		m.toggleLogKind(DebugKindAgent)
	case "4":
		m.toggleLogKind(DebugKindError)
	default:
		if r := []rune(msg.String()); len(r) == 1 && r[0] >= 32 {
			m.logSearch += string(r)
			m.refreshLogViewport()
		}
	}
	return m, nil
}

func (m *model) toggleLogKind(kind DebugEntryKind) {
	if m.logKindFilter == nil {
		m.logKindFilter = map[DebugEntryKind]bool{
			DebugKindLLM:   true,
			DebugKindTool:  true,
			DebugKindAgent: true,
			DebugKindError: true,
		}
	}
	m.logKindFilter[kind] = !m.logKindFilter[kind]
	m.refreshLogViewport()
}

// hasActiveAgentWork reports whether the main agent or any sub-agent run is
// currently in flight. Used to gate the agent.Cancel() / Runs().CancelAll()
// calls in handleEscKey so that pressing Esc to dismiss a UI overlay (file
// picker, detail card, etc.) when nothing is streaming does NOT terminate
// background subagents.
func (m model) hasActiveAgentWork() bool {
	if m.streaming {
		return true
	}
	if m.agent != nil && m.agent.Runs() != nil && m.agent.Runs().RunningCount() > 0 {
		return true
	}
	return false
}

// handleEscKey is shared esc logic: cancel stream first, then double-esc either
// exits shell mode or opens the message picker.
func (m model) handleEscKey() (tea.Model, tea.Cmd) {
	hadActiveWork := m.hasActiveAgentWork()
	if m.streaming && m.cancelStream != nil {
		select {
		case <-m.cancelStream:
		default:
			close(m.cancelStream)
		}
	}
	// Cancel agent runs only when work is actually in flight. Calling
	// CancelAll() unconditionally would terminate background subagents on
	// every Esc keystroke (e.g. closing a detail view or clearing a file
	// selection), silently destroying in-flight results.
	if hadActiveWork && m.agent != nil {
		m.agent.Cancel()
		if m.agent.Runs() != nil {
			m.agent.Runs().CancelAll()
		}
	}
	if m.streaming {
		return m, func() tea.Msg { return streamDoneMsg{err: context.Canceled} }
	}
	if !m.detail.empty() {
		m.detail.pop()
		return m, nil
	}
	if m.activeTab == tabFiles {
		if m.filesSel.active || m.filesSel.dragging {
			m.filesSel = selectionState{}
			m.files.clearSelectionHighlight()
			return m, nil
		}
		if len(m.files.selectedFiles) > 0 {
			m.files.selectedFiles = nil
			return m, nil
		}
	}
	if m.activeTab == tabGit {
		if m.gitSel.active || m.gitSel.dragging {
			m.gitSel = selectionState{}
			m.git.clearDiffSelectionHighlight()
			return m, nil
		}
		if len(m.git.selectedFiles) > 0 {
			m.git.selectedFiles = nil
			return m, nil
		}
	}
	if m.sidebarSel.active || m.sidebarSel.dragging {
		m.sidebarSel = selectionState{}
		return m, nil
	}
	if !m.escPressed {
		m.escPressed = true
		m.escPressTime = time.Now()
		return m, nil
	}
	if time.Since(m.escPressTime) < 500*time.Millisecond {
		if m.activeTab == tabChat && m.inputIsShellMode() {
			m.escPressed = false
			m.disableShellMode()
			return m, nil
		}
		m.escPressed = false
		m.openMessagePicker()
		return m, nil
	}
	m.escPressed = false
	return m, nil
}

func (m model) handleTabCtrlC() (tea.Model, tea.Cmd) {
	if m.ctrlCPressed {
		m.cleanupCurrentSession()
		return m, tea.Quit
	}
	m.ctrlCPressed = true
	return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg { return ctrlCResetMsg{} })
}

// filesHasActiveFocus reports whether the files sub-model has an active mode that should consume esc.
func (m model) filesHasActiveFocus() bool {
	return m.files.fuzzy || m.files.mode != filesModeNormal || m.files.choosingEditor
}

// gitHasActiveFocus reports whether the git sub-model has an active mode that should consume esc.
func (m model) gitHasActiveFocus() bool {
	return m.git.committing || m.git.branchInputMode || m.git.stashInputMode || m.git.pendingAction != gitPendingNone
}

func shouldForwardToTranscriptViewport(msg tea.Msg) bool {
	switch msg.(type) {
	case tea.KeyPressMsg:
		return false
	default:
		return true
	}
}

func (m model) handleMouseAction(mouse tea.Mouse, pressed bool) (tea.Model, tea.Cmd, bool) {
	if pressed && mouse.Button == tea.MouseRight {
		if m.activeTab == tabGit {
			panelW := m.panelWidth()
			gitHeaderH := lipgloss.Height(m.styles.Header.Render("◆ ocode  Git"))
			gitBodyTop := gitHeaderH + 1
			sectW := panelW * 20 / 100
			filesW := panelW * 30 / 100
			sectRight := sectW
			filesRight := sectRight + filesW
			if mouse.X >= sectRight && mouse.X < filesRight && mouse.Y >= gitBodyTop {
				m.git.clearActiveFile()
				return m, nil, true
			}
		}
		if m.activeTab == tabFiles {
			filesHeaderH := lipgloss.Height(m.styles.Header.Render("◆ ocode  Files"))
			treeW := m.width * 35 / 100
			if mouse.X >= 0 && mouse.X < treeW && mouse.Y >= filesHeaderH+1 {
				m.files.clearActiveFile()
				return m, nil, true
			}
		}
		return m, nil, false
	}
	if pressed && mouse.Button != tea.MouseLeft {
		return m, nil, false
	}
	if !pressed && mouse.Button != tea.MouseLeft && mouse.Button != tea.MouseNone {
		return m, nil, false
	}

	if pressed {
		if thumbOffset, ok := m.detailScrollbarThumbOffset(mouse); ok {
			m.scrollbarDrag = scrollbarDragDetail
			m.scrollbarDragOffset = thumbOffset
			return m, nil, true
		}
		if m.detailScrollbarHit(mouse) {
			trackTop, trackHeight := m.detailScrollbarMetrics()
			scrollbarSetOffset(&m.detail[len(m.detail)-1].vp, mouse.Y, trackTop, trackHeight)
			return m, nil, true
		}
		if !m.detail.empty() {
			return m, nil, true
		}
		// Click on agent strip: open the clicked run's detail view.
		if strip, blocks := m.renderAgentStrip(); strip != "" {
			stripTop := m.agentStripTopY()
			stripH := lipgloss.Height(strip)
			relY := mouse.Y - stripTop
			if relY >= 0 && relY < stripH {
				for _, blk := range blocks {
					if relY >= blk.rowStart && relY < blk.rowEnd {
						m.openAgentDetail(blk.runID)
						return m, nil, true
					}
				}
				return m, nil, true
			}
		}
		if thumbOffset, ok := m.transcriptScrollbarThumbOffset(mouse); ok {
			m.scrollbarDrag = scrollbarDragTranscript
			m.scrollbarDragOffset = thumbOffset
			return m, nil, true
		}
		if m.transcriptScrollbarHit(mouse) {
			scrollbarSetOffset(&m.viewport, mouse.Y, m.viewportContentTopY(), m.viewport.Height())
			return m, nil, true
		}
	}
	if pressed && m.logScrollbarHit(mouse) {
		logTrackTop, logTrackHeight := m.logScrollbarMetrics()
		if thumbOffset, ok := m.logScrollbarThumbOffset(mouse); ok {
			m.scrollbarDrag = scrollbarDragLog
			m.scrollbarDragOffset = thumbOffset
		} else {
			scrollbarSetOffset(&m.logViewport, mouse.Y, logTrackTop, logTrackHeight)
		}
		return m, nil, true
	}
	if pressed && m.activeTab == tabGit {
		panelW := m.panelWidth()
		gitHeaderH := lipgloss.Height(m.styles.Header.Render("◆ ocode  Git"))
		gitBodyTop := gitHeaderH + 1 // +1 for top border of panes
		sectW := panelW * 20 / 100
		filesW := panelW * 30 / 100
		sectRight := sectW
		filesRight := sectRight + filesW
		diffRight := panelW - 1
		scrollX := diffRight - 1

		// scrollbar for diff panel
		gitTrackH := m.git.diff.Height()
		if mouse.X == scrollX && mouse.Y >= gitBodyTop && mouse.Y < gitBodyTop+gitTrackH {
			if thumbOffset, ok := scrollbarThumbOffset(mouse.Y, gitBodyTop, gitTrackH, m.git.diff.TotalLineCount(), m.git.diff.VisibleLineCount(), m.git.diff.YOffset()); ok {
				m.scrollbarDrag = scrollbarDragGitDiff
				m.scrollbarDragOffset = thumbOffset
			} else {
				scrollbarSetOffset(&m.git.diff, mouse.Y, gitBodyTop, gitTrackH)
			}
			return m, nil, true
		}

		// section panel click
		if mouse.X >= 0 && mouse.X < sectRight && mouse.Y >= gitBodyTop {
			row := mouse.Y - gitBodyTop - 1 // -1 for border
			if row >= 0 && row < 4 {
				m.git.section = gitSection(row)
				m.git.panel = gitPanelSections
				m.git.resetCursors()
				m.git.loadDiff()
			}
			return m, nil, true
		}

		// file list panel click
		if mouse.X >= sectRight && mouse.X < filesRight && mouse.Y >= gitBodyTop {
			row := mouse.Y - gitBodyTop - 1 // -1 for border
			if row >= 0 {
				isDoubleClick := time.Since(m.lastClickTime) < 400*time.Millisecond && mouse.X == m.lastClickX && mouse.Y == m.lastClickY
				m.lastClickTime = time.Now()
				m.lastClickX = mouse.X
				m.lastClickY = mouse.Y
				m.git.panel = gitPanelFiles
				switch m.git.section {
				case gitSectionChanges:
					files := m.git.currentFileList()
					if row < len(files) {
						m.git.filesCursor = row
						m.git.loadDiff()
						if isDoubleClick {
							path := filepath.Join(m.git.workDir, files[row].path)
							return m, m.git.openInEditor(path), true
						}
					}
				case gitSectionLog:
					if row < len(m.git.commits) {
						m.git.commitCursor = row
						m.git.loadDiff()
					}
				case gitSectionStash:
					if row < len(m.git.stashes) {
						m.git.stashCursor = row
						m.git.loadDiff()
					}
				case gitSectionBranches:
					if row < len(m.git.branches) {
						m.git.branchCursor = row
						m.git.loadDiff()
					}
				}
			}
			return m, nil, true
		}

		// diff panel text selection
		diffLeft := filesRight + 1 // after files pane border
		if mouse.X >= diffLeft && mouse.X < scrollX && mouse.Y >= gitBodyTop {
			contentLine := (mouse.Y - gitBodyTop - 1) + m.git.diff.YOffset()
			if contentLine >= 0 && contentLine < len(m.git.diffRawLines) {
				m.git.panel = gitPanelDiff
				m.gitSel = selectionState{
					dragging:  true,
					startLine: contentLine,
					startCol:  mouse.X - diffLeft,
					endLine:   contentLine,
					endCol:    mouse.X - diffLeft,
				}
				m.git.applyDiffSelectionHighlight(m.gitSel.startLine, m.gitSel.startCol, m.gitSel.endLine, m.gitSel.endCol)
				return m, nil, true
			}
		}
	}
	if pressed && m.activeTab == tabFiles {
		filesHeaderH := lipgloss.Height(m.styles.Header.Render("◆ ocode  Files"))
		// Handle tree panel click — select/open file or toggle directory
		if idx, ok := m.files.treeNodeForClick(mouse, filesHeaderH); ok {
			n := m.files.nodes[idx]
			m.files.cursor = idx
			isDoubleClick := time.Since(m.lastClickTime) < 400*time.Millisecond && mouse.X == m.lastClickX && mouse.Y == m.lastClickY
			m.lastClickTime = time.Now()
			m.lastClickX = mouse.X
			m.lastClickY = mouse.Y
			if n.isDir {
				m.files.toggleDir(idx)
			} else if isDoubleClick {
				m.files.openEditorPicker(n.path)
				return m, nil, true
			} else {
				return m, loadPreviewCmd(n), true
			}
			return m, nil, true
		}
		previewRight := m.width - 1
		scrollX := previewRight - 1
		filesTrackTop := filesHeaderH + 1
		filesTrackH := m.files.preview.Height()
		if mouse.X == scrollX && mouse.Y >= filesTrackTop && mouse.Y < filesTrackTop+filesTrackH {
			if thumbOffset, ok := scrollbarThumbOffset(mouse.Y, filesTrackTop, filesTrackH, m.files.preview.TotalLineCount(), m.files.preview.VisibleLineCount(), m.files.preview.YOffset()); ok {
				m.scrollbarDrag = scrollbarDragFilesPreview
				m.scrollbarDragOffset = thumbOffset
			} else {
				scrollbarSetOffset(&m.files.preview, mouse.Y, filesTrackTop, filesTrackH)
			}
			return m, nil, true
		}
		treeW := m.width * 35 / 100
		previewLeft := treeW + 2
		previewBodyTop := filesHeaderH + 1 + m.files.previewHeaderLines()
		if mouse.X >= previewLeft && mouse.X < scrollX && mouse.Y >= previewBodyTop && mouse.Y < previewBodyTop+m.files.preview.Height() {
			contentLine := (mouse.Y - previewBodyTop) + m.files.preview.YOffset()
			if contentLine >= 0 && contentLine < len(m.files.previewRawLines) {
				m.filesSel = selectionState{
					dragging:  true,
					startLine: contentLine,
					startCol:  mouse.X - previewLeft,
					endLine:   contentLine,
					endCol:    mouse.X - previewLeft,
				}
				m.files.applySelectionHighlight(m.filesSel.startLine, m.filesSel.startCol, m.filesSel.endLine, m.filesSel.endCol)
				return m, nil, true
			}
		}
	}
	if !pressed {
		m.scrollbarDrag = scrollbarDragNone
		m.scrollbarDragOffset = 0
		if m.sel.dragging {
			m.sel.dragging = false
			if m.sel.active {
				text := extractSelectionText(m.rawTranscriptLines, m.sel.startLine, m.sel.startCol, m.sel.endLine, m.sel.endCol)
				_ = clipboard.WriteAll(text)
				m.sel = selectionState{}
				m.applyOrClearSelectionHighlight()
				return m, nil, true
			}
			m.sel = selectionState{}
			m.applyOrClearSelectionHighlight()
		}
		if m.filesSel.dragging {
			m.filesSel.dragging = false
			if m.filesSel.active {
				text := m.files.extractSelectionText(m.filesSel.startLine, m.filesSel.startCol, m.filesSel.endLine, m.filesSel.endCol)
				_ = clipboard.WriteAll(text)
				// keep selection + highlight after release so it persists
				// until a new selection, file change, or add-to-context
				return m, nil, true
			}
			m.filesSel = selectionState{}
			m.files.clearSelectionHighlight()
		}
		if m.inputSel.dragging {
			m.inputSel.dragging = false
			if m.inputSel.active {
				(&m).ensureRawInputLines()
				text := extractSelectionText(m.rawInputLines, m.inputSel.startLine, m.inputSel.startCol, m.inputSel.endLine, m.inputSel.endCol)
				_ = clipboard.WriteAll(text)
				m.inputSel = selectionState{}
				return m, nil, true
			}
			m.inputSel = selectionState{}
		}
		if m.gitSel.dragging {
			m.gitSel.dragging = false
			if m.gitSel.active {
				text := extractSelectionText(m.git.diffRawLines, m.gitSel.startLine, m.gitSel.startCol, m.gitSel.endLine, m.gitSel.endCol)
				_ = clipboard.WriteAll(text)
				m.gitSel = selectionState{}
				m.git.clearDiffSelectionHighlight()
				return m, nil, true
			}
			m.gitSel = selectionState{}
			m.git.clearDiffSelectionHighlight()
		}
		if m.sidebarSel.dragging {
			m.sidebarSel.dragging = false
			if m.sidebarSel.active {
				text := extractSelectionText(m.rawSidebarLines, m.sidebarSel.startLine, m.sidebarSel.startCol, m.sidebarSel.endLine, m.sidebarSel.endCol)
				_ = clipboard.WriteAll(text)
				m.sidebarSel = selectionState{}
				return m, nil, true
			}
			// Simple click (no drag): try to open a file at the click position.
			if path, ok := m.sidebarFileForClick(mouse); ok {
				m.sidebarSel = selectionState{}
				return m, openSidebarFileInEditor(path), true
			}
			m.sidebarSel = selectionState{}
		}
	}

	if pressed && m.showPermDialog {
		for _, btn := range m.permButtonRegions {
			if mouse.Y >= btn.y1 && mouse.Y <= btn.y2 && mouse.X >= btn.x1 && mouse.X <= btn.x2 {
				m.showPermDialog = false
				cmd := m.handlePermissionChoice(btn.choice)
				m.input.Reset()
				m.rerenderTranscriptAndMaybeScroll()
				m.saveSession()
				return m, cmd, true
			}
		}
	}

	if pressed && m.exitButtonForClick(mouse) {
		if m.exitPending {
			m.cleanupCurrentSession()
			return m, tea.Quit, true
		}
		m.exitPending = true
		return m, nil, true
	}
	if pressed && !m.exitButtonForClick(mouse) {
		m.exitPending = false
	}

	if tab, ok := m.tabForClick(mouse); ok {
		m.activeTab = tab
		if tab == tabChat {
			m.chatUnread = false
		}
		if tab == tabLog {
			m.refreshLogViewport()
		}
		return m, nil, true
	}
	if pressed && m.isClickInInputArea(mouse) {
		topY := m.inputAreaTopY()
		relRow := mouse.Y - topY - 1 + m.input.ScrollYOffset() // -1 for top border
		if relRow < 0 {
			relRow = 0
		}
		m.inputSel = selectionState{
			dragging:  true,
			startLine: relRow,
			startCol:  mouse.X,
			endLine:   relRow,
			endCol:    mouse.X,
		}
		return m, nil, true
	}
	if pressed && m.activeTab == tabChat && mouse.X < m.mainScrollbarX() {
		contentLine := (mouse.Y - m.viewportContentTopY()) + m.viewport.YOffset()
		if contentLine >= 0 && contentLine < len(m.rawTranscriptLines) {
			m.sel = selectionState{
				dragging:  true,
				startLine: contentLine,
				startCol:  mouse.X,
				endLine:   contentLine,
				endCol:    mouse.X,
			}
			m.applyOrClearSelectionHighlight()
			return m, nil, true
		}
	}

	if !pressed && m.sel.active {
		m.sel = selectionState{}
		m.applyOrClearSelectionHighlight()
	}

	if !pressed && !m.sel.active {
		if updated, cmd, ok := m.handleDetailClick(mouse); ok {
			return updated, cmd, true
		}
		if !m.detail.empty() {
			return m, nil, true
		}
		if idx, ok := m.toolOutputForClick(mouse); ok {
			m.expandedToolOutputs[idx] = !m.expandedToolOutputs[idx]
			m.renderTranscript()
			return m, nil, true
		}
		if idx, ok := m.thinkingForClick(mouse); ok {
			m.expandedThinking[idx] = !m.expandedThinking[idx]
			m.renderTranscript()
			return m, nil, true
		}
	}
	if m.showPicker {
		if idx, ok := m.pickerRowForY(mouse.Y); ok {
			m.pickerIndex = idx
			updated, cmd := m.selectPickerIndex(idx)
			return updated, cmd, true
		}
		return m, nil, true
	}
	if m.showConnect {
		if idx, ok := m.connectRowForY(mouse.Y); ok {
			updated, cmd := m.selectConnectRow(idx)
			return updated, cmd, true
		}
		return m, nil, true
	}
	if m.showSlashPopup {
		if idx, ok := m.slashPopupRowForY(mouse.Y); ok {
			selected := m.slashPopupItems[idx]
			m.acceptPopupSuggestion(selected)
			return m, nil, true
		}
	}
	// Sidebar text selection — always start dragging on press so a subsequent
	// release can distinguish a simple click (no drag) from a selection drag.
	if pressed && m.mouseOverSidebar(mouse) {
		data := m.buildSidebarRenderData()
		headerHeight := 0
		title := m.sessionTitle
		if title == "" {
			if prompt := m.firstUserPromptText(); prompt != "" {
				title = truncateTitle(prompt, maxExplicitTitleLen)
			}
		}
		if title != "" {
			headerHeight = 1
		}
		boxTop := 1 + headerHeight + len(data.topLines)
		// Account for leading empty line when header is empty
		if headerHeight == 0 {
			boxTop++
		}
		visible := m.sidebarVisibleScrollLines(data, headerHeight)
		if mouse.Y >= boxTop && mouse.Y < boxTop+visible {
			scrollLine := m.sidebarScroll + (mouse.Y - boxTop)
			if scrollLine >= 0 && scrollLine < len(data.scrollLines) {
				m.sidebarSel = selectionState{
					dragging:  true,
					startLine: scrollLine,
					startCol:  mouse.X - m.panelWidth(),
					endLine:   scrollLine,
					endCol:    mouse.X - m.panelWidth(),
				}
				return m, nil, true
			}
		}
	}
	return m, nil, false
}

func (m model) handleMouseMotion(mouse tea.Mouse) (tea.Model, tea.Cmd, bool) {
	if mouse.Button != tea.MouseLeft {
		return m, nil, false
	}
	headerHeight := lipgloss.Height(m.styles.Header.Render("◆ ocode"))
	trackTop := headerHeight + 1

	switch m.scrollbarDrag {
	case scrollbarDragTranscript:
		scrollbarSetOffset(&m.viewport, mouse.Y-m.scrollbarDragOffset, trackTop, m.viewport.Height())
		return m, nil, true
	case scrollbarDragDetail:
		detailTrackTop, detailTrackHeight := m.detailScrollbarMetrics()
		scrollbarSetOffset(&m.detail[len(m.detail)-1].vp, mouse.Y-m.scrollbarDragOffset, detailTrackTop, detailTrackHeight)
		return m, nil, true
	case scrollbarDragLog:
		logTrackTop, logTrackHeight := m.logScrollbarMetrics()
		scrollbarSetOffset(&m.logViewport, mouse.Y-m.scrollbarDragOffset, logTrackTop, logTrackHeight)
		return m, nil, true
	case scrollbarDragGitDiff:
		gitHeaderH := lipgloss.Height(m.styles.Header.Render("◆ ocode  Git"))
		gitTrackTop := gitHeaderH + 1
		scrollbarSetOffset(&m.git.diff, mouse.Y-m.scrollbarDragOffset, gitTrackTop, m.git.diff.Height())
		return m, nil, true
	case scrollbarDragFilesPreview:
		filesHeaderH := lipgloss.Height(m.styles.Header.Render("◆ ocode  Files"))
		filesTrackTop := filesHeaderH + 1
		scrollbarSetOffset(&m.files.preview, mouse.Y-m.scrollbarDragOffset, filesTrackTop, m.files.preview.Height())
		return m, nil, true
	}

	if m.sel.dragging {
		contentLine := (mouse.Y - m.viewportContentTopY()) + m.viewport.YOffset()
		if contentLine < 0 {
			contentLine = 0
		}
		if contentLine >= len(m.rawTranscriptLines) && len(m.rawTranscriptLines) > 0 {
			contentLine = len(m.rawTranscriptLines) - 1
		}
		col := mouse.X
		if col < 0 {
			col = 0
		}
		m.sel.endLine = contentLine
		m.sel.endCol = col
		m.sel.active = m.sel.startLine != m.sel.endLine || m.sel.startCol != m.sel.endCol
		m.applyOrClearSelectionHighlight()
		return m, nil, true
	}

	if m.filesSel.dragging {
		treeW := m.width * 35 / 100
		previewLeft := treeW + 2
		filesHeaderH := lipgloss.Height(m.styles.Header.Render("◆ ocode  Files"))
		previewBodyTop := filesHeaderH + 1 + m.files.previewHeaderLines()
		contentLine := (mouse.Y - previewBodyTop) + m.files.preview.YOffset()
		if contentLine < 0 {
			contentLine = 0
		}
		if contentLine >= len(m.files.previewRawLines) && len(m.files.previewRawLines) > 0 {
			contentLine = len(m.files.previewRawLines) - 1
		}
		col := mouse.X - previewLeft
		if col < 0 {
			col = 0
		}
		m.filesSel.endLine = contentLine
		m.filesSel.endCol = col
		m.filesSel.active = m.filesSel.startLine != m.filesSel.endLine || m.filesSel.startCol != m.filesSel.endCol
		m.files.applySelectionHighlight(m.filesSel.startLine, m.filesSel.startCol, m.filesSel.endLine, m.filesSel.endCol)
		return m, nil, true
	}

	if m.inputSel.dragging {
		(&m).ensureRawInputLines()
		topY := m.inputAreaTopY()
		relRow := mouse.Y - topY - 1 + m.input.ScrollYOffset()
		if relRow < 0 {
			relRow = 0
		}
		if relRow >= len(m.rawInputLines) && len(m.rawInputLines) > 0 {
			relRow = len(m.rawInputLines) - 1
		}
		col := mouse.X
		if col < 0 {
			col = 0
		}
		m.inputSel.endLine = relRow
		m.inputSel.endCol = col
		m.inputSel.active = m.inputSel.startLine != m.inputSel.endLine || m.inputSel.startCol != m.inputSel.endCol
		return m, nil, true
	}

	if m.gitSel.dragging {
		panelW := m.panelWidth()
		gitHeaderH := lipgloss.Height(m.styles.Header.Render("◆ ocode  Git"))
		gitBodyTop := gitHeaderH + 1
		sectW := panelW * 20 / 100
		filesW := panelW * 30 / 100
		diffLeft := sectW + filesW + 1
		contentLine := (mouse.Y - gitBodyTop - 1) + m.git.diff.YOffset()
		if contentLine < 0 {
			contentLine = 0
		}
		if contentLine >= len(m.git.diffRawLines) && len(m.git.diffRawLines) > 0 {
			contentLine = len(m.git.diffRawLines) - 1
		}
		col := mouse.X - diffLeft
		if col < 0 {
			col = 0
		}
		m.gitSel.endLine = contentLine
		m.gitSel.endCol = col
		m.gitSel.active = m.gitSel.startLine != m.gitSel.endLine || m.gitSel.startCol != m.gitSel.endCol
		m.git.applyDiffSelectionHighlight(m.gitSel.startLine, m.gitSel.startCol, m.gitSel.endLine, m.gitSel.endCol)
		return m, nil, true
	}

	if m.sidebarSel.dragging {
		data := m.buildSidebarRenderData()
		headerHeight := 0
		title := m.sessionTitle
		if title == "" {
			if prompt := m.firstUserPromptText(); prompt != "" {
				title = truncateTitle(prompt, maxExplicitTitleLen)
			}
		}
		if title != "" {
			headerHeight = 1
		}
		boxTop := 1 + headerHeight + len(data.topLines)
		// Account for leading empty line when header is empty
		if headerHeight == 0 {
			boxTop++
		}
		visible := m.sidebarVisibleScrollLines(data, headerHeight)
		if mouse.Y >= boxTop && mouse.Y < boxTop+visible {
			scrollLine := m.sidebarScroll + (mouse.Y - boxTop)
			if scrollLine < 0 {
				scrollLine = 0
			}
			if len(data.scrollLines) > 0 && scrollLine >= len(data.scrollLines) {
				scrollLine = len(data.scrollLines) - 1
			}
			col := mouse.X - m.panelWidth()
			if col < 0 {
				col = 0
			}
			m.sidebarSel.endLine = scrollLine
			m.sidebarSel.endCol = col
			m.sidebarSel.active = m.sidebarSel.startLine != m.sidebarSel.endLine || m.sidebarSel.startCol != m.sidebarSel.endCol
			return m, nil, true
		}
	}

	// Sidebar hover detection — underline clickable file paths
	{
		prevHover := m.hoverSidebarFile
		if m.mouseOverSidebar(mouse) {
			data := m.buildSidebarRenderData()
			headerHeight := 0
			title := m.sessionTitle
			if title == "" {
				if prompt := m.firstUserPromptText(); prompt != "" {
					title = truncateTitle(prompt, maxExplicitTitleLen)
				}
			}
			if title != "" {
				headerHeight = 1
			}
			boxTop := 1 + headerHeight + len(data.topLines)
			if headerHeight == 0 {
				boxTop++
			}
			visible := m.sidebarVisibleScrollLines(data, headerHeight)
			if mouse.Y >= boxTop && mouse.Y < boxTop+visible {
				scrollLine := m.sidebarScroll + (mouse.Y - boxTop)
				if path, ok := data.fileScrollLinePaths[scrollLine]; ok {
					m.hoverSidebarFile = path
				} else {
					m.hoverSidebarFile = ""
				}
			} else {
				m.hoverSidebarFile = ""
			}
		} else {
			m.hoverSidebarFile = ""
		}
		if m.hoverSidebarFile != prevHover {
			return m, nil, true
		}
	}

	if tab, ok := m.tabForClick(mouse); ok {
		m.activeTab = tab
		if tab == tabChat {
			m.chatUnread = false
		}
		if tab == tabLog {
			m.refreshLogViewport()
		}
		return m, nil, true
	}
	if m.showPicker {
		if idx, ok := m.pickerRowForY(mouse.Y); ok {
			m.pickerIndex = idx
			updated, cmd := m.selectPickerIndex(idx)
			return updated, cmd, true
		}
		return m, nil, true
	}
	if m.showConnect {
		if idx, ok := m.connectRowForY(mouse.Y); ok {
			updated, cmd := m.selectConnectRow(idx)
			return updated, cmd, true
		}
		return m, nil, true
	}
	if m.showSlashPopup {
		if idx, ok := m.slashPopupRowForY(mouse.Y); ok {
			selected := m.slashPopupItems[idx]
			m.acceptPopupSuggestion(selected)
			return m, nil, true
		}
	}
	if path, ok := m.sidebarFileForClick(mouse); ok {
		return m, openSidebarFileInEditor(path), true
	}
	return m, nil, false
}

func (m model) handleDetailClick(mouse tea.Mouse) (tea.Model, tea.Cmd, bool) {
	if len(m.detail) == 0 {
		return m, nil, false
	}
	// Click anywhere in the header area (above the viewport) pops back to parent.
	topY := m.detailViewportContentTopY()
	if mouse.Y < topY && mouse.Y >= 0 {
		m.detail.pop()
		return m, nil, true
	}
	if !m.mouseOverDetailViewport(mouse) {
		return m, nil, false
	}
	top := m.detail[len(m.detail)-1]
	contentLine := (mouse.Y - m.detailViewportContentTopY()) + top.vp.YOffset()
	if contentLine < 0 {
		return m, nil, true
	}
	if top.kind == detailAgentRun {
		for _, r := range top.regions {
			if contentLine >= r.rowStart && contentLine < r.rowEnd {
				if m.detail[len(m.detail)-1].expanded == nil {
					m.detail[len(m.detail)-1].expanded = map[string]bool{}
				}
				m.detail[len(m.detail)-1].expanded[r.id] = !m.detail[len(m.detail)-1].expanded[r.id]
				m.refreshTopDetailView()
				return m, nil, true
			}
		}
		for _, b := range top.procs {
			if contentLine >= b.rowStart && contentLine < b.rowEnd {
				m.openProcessLogForRun(top.runPath, b.procID)
				return m, nil, true
			}
		}
		for _, b := range top.runs {
			if b.runPath != top.runPath && contentLine >= b.rowStart && contentLine < b.rowEnd {
				m.openAgentDetail(b.runPath)
				return m, nil, true
			}
		}
	}
	if top.kind == detailProcessList {
		row := contentLine - 2
		if row >= 0 {
			if reg := m.processRegistryForRun(top.runPath); reg != nil {
				procs := reg.Snapshot()
				if row < len(procs) {
					m.openProcessLogForRun(top.runPath, procs[row].ID)
					return m, nil, true
				}
			}
		}
	}
	return m, nil, true
}

func (m model) mouseOverTranscriptViewport(msg tea.MouseWheelMsg) bool {
	if m.activeTab != tabChat {
		return false
	}
	mouse := msg.Mouse()
	if mouse.X < 0 || mouse.X >= m.panelWidth() {
		return false
	}
	headerHeight := lipgloss.Height(m.styles.Header.Render("◆ ocode"))
	transcriptTop := headerHeight
	transcriptBottom := transcriptTop + m.viewport.Height() + 2
	return mouse.Y >= transcriptTop && mouse.Y < transcriptBottom
}

func (m *model) handleCommand(text string) (tea.Model, tea.Cmd) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return m, nil
	}
	cmd := parts[0]
	args := parts[1:]

	m.input.Reset()

	var cmdResult tea.Cmd
	spec := lookupCommand(cmd)
	if spec != nil {
		cmdResult = spec.handler(m, args)
	} else if customCmd, ok := customCommandLookup[cmd]; ok {
		prompt := customCmd.Prompt
		userArgs := strings.Join(args, " ")
		if userArgs != "" {
			prompt = strings.ReplaceAll(prompt, "{{args}}", userArgs)
			prompt = strings.ReplaceAll(prompt, "{args}", userArgs)
			if !strings.HasSuffix(prompt, "\n") && !strings.HasSuffix(prompt, " ") {
				prompt += " " + userArgs
			}
		}
		m.messages = append(m.messages, message{role: roleUser, text: cmd + " " + userArgs})
		if m.agent != nil {
			m.agent.ResetSubagentDispatch()
		}
		m.rerenderTranscriptAndMaybeScroll()
		return m, m.sendCustomCommandPrompt(prompt)
	} else {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Unknown command: %s", cmd)})
	}

	m.rerenderTranscriptAndMaybeScroll()
	return m, cmdResult
}

func (m *model) handleLoginCmd(args []string) tea.Cmd {
	return func() tea.Msg {
		token, err := auth.LoginWithGoogle()
		return authFinishedMsg{token: token, err: err}
	}
}

func (m *model) handleMCPAuth(serverName string) error {
	if m.config == nil {
		return fmt.Errorf("config not loaded")
	}
	mcpCfg, ok := m.config.MCP[serverName]
	if !ok {
		return fmt.Errorf("mcp server %q not found in config", serverName)
	}
	if mcpCfg.OAuth == nil {
		return fmt.Errorf("mcp server %q has no oauth configuration", serverName)
	}
	oauth := mcpCfg.OAuth
	if oauth.AuthorizationURL == "" || oauth.TokenURL == "" || oauth.ClientID == "" {
		return fmt.Errorf("mcp server %q oauth config is incomplete", serverName)
	}
	return auth.MCPAuthFlow(serverName, oauth.AuthorizationURL, oauth.TokenURL, oauth.ClientID, oauth.Scopes)
}

func (m *model) rebuildAgentWithExternalTools() tea.Cmd {
	if m.config == nil || m.config.Model == "" {
		return nil
	}
	client := agent.NewClient(m.config, m.config.Model)
	if client == nil {
		return nil
	}
	next := agent.NewAgent(client, m.getInitialTools(), m.config)
	if m.agent != nil {
		next.SetSpec(m.agent.Spec())
		if m.agent.Permissions() != nil {
			next.Permissions().LoadFromOcode(m.agent.Permissions().ExportConfig())
		}
	}
	next.LoadExternalTools(m.config)
	return m.replaceAgent(next)
}

func (m model) renderMCPList() string {
	if m.config == nil || len(m.config.MCP) == 0 {
		return "No MCP servers configured in opencode config."
	}
	names := make([]string, 0, len(m.config.MCP))
	for name := range m.config.MCP {
		names = append(names, name)
	}
	sort.Strings(names)

	loaded := map[string]int{}
	if m.agent != nil {
		for _, toolName := range m.agent.MCPToolNames() {
			if idx := strings.Index(toolName, "_"); idx > 0 {
				loaded[toolName[:idx]]++
			}
		}
	}

	var b strings.Builder
	b.WriteString("MCP servers:\n")
	for _, name := range names {
		cfg := m.config.MCP[name]
		state := "disabled"
		if cfg.Enabled {
			state = "enabled"
		}
		typ := cfg.Type
		if typ == "" {
			typ = "local"
		}
		b.WriteString(fmt.Sprintf("  %-18s %-8s %-8s %d tools\n", name, typ, state, loaded[name]))
	}
	if m.agent != nil {
		errs := m.agent.MCPErrors()
		if len(errs) > 0 {
			b.WriteString("\nErrors:\n")
			for _, errText := range errs {
				b.WriteString("  " + errText + "\n")
			}
		}
	}
	b.WriteString("\nUsage: /mcp enable <server>, /mcp disable <server>, /mcp-auth <server>")
	return strings.TrimRight(b.String(), "\n")
}

func (m *model) processFileReferences(text string) tea.Cmd {
	return func() tea.Msg {
		atRefRe := regexp.MustCompile(`@((?:\\.|[^\s])+)`)
		processedText := text
		var msgs []message
		var images []agent.Image
		seen := make(map[string]struct{})

		attachPath := func(path string) *fileSearchFinishedMsg {
			if path == "" {
				return nil
			}
			if _, ok := seen[path]; ok {
				return nil
			}
			seen[path] = struct{}{}

			foundPath := ""
			filepath.Walk(".", func(p string, info os.FileInfo, err error) error { //nolint:errcheck
				if err != nil {
					return nil
				}
				if foundPath != "" {
					// Already found — skip remaining directories to avoid full-tree scan.
					if info.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
				if info.IsDir() {
					return nil
				}
				if strings.Contains(strings.ToLower(p), strings.ToLower(path)) {
					foundPath = p
				}
				return nil
			})

			if foundPath != "" {
				path = foundPath
			}

			if agent.IsImageFile(path) {
				img, err := agent.NewImage(path)
				if err != nil {
					msg := fileSearchFinishedMsg{err: fmt.Errorf("attach image %s: %w", path, err)}
					return &msg
				}
				images = append(images, img)
				msgs = append(msgs, message{
					role: roleAssistant,
					text: fmt.Sprintf("📎 Attached image %s", path),
				})
				return nil
			}

			content, err := os.ReadFile(path)
			if err == nil {
				fileCtx := fmt.Sprintf("\n--- File: %s ---\n%s\n", path, string(content))
				msgs = append(msgs, message{
					role: roleAssistant,
					text: fmt.Sprintf("📎 Added context from %s", path),
					raw: &agent.Message{
						Role:    "system",
						Content: fileCtx,
					},
				})
			}
			return nil
		}

		for _, path := range m.compactFileReferencePaths(text) {
			if result := attachPath(path); result != nil {
				return *result
			}
		}

		for _, match := range atRefRe.FindAllStringSubmatch(text, -1) {
			path := unescapeAtPath(match[1])
			if result := attachPath(path); result != nil {
				return *result
			}
		}

		return fileSearchFinishedMsg{processedText: processedText, messages: msgs, images: images}
	}
}

func (m *model) compactFileReferencePaths(text string) []string {
	if len(m.fileShortcodePaths) == 0 || !strings.Contains(text, "[file:") {
		return nil
	}
	re := regexp.MustCompile(`\[file: [^\]]+\]`)
	matches := re.FindAllString(text, -1)
	paths := make([]string, 0, len(matches))
	for _, token := range matches {
		if path, ok := m.fileShortcodePaths[token]; ok {
			paths = append(paths, path)
		}
	}
	return paths
}

func (m *model) handleModelCmd(args []string) tea.Cmd {
	if len(args) == 0 {
		m.openModelPicker()
		return nil
	}
	if len(args) > 0 {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Switching to model %s", args[0])})
		var mcpNames []string
		if m.agent != nil {
			mcpNames = m.agent.MCPToolNames()
		}
		client := agent.NewClient(m.config, args[0])
		if client != nil {
			var tools []tool.Tool
			if m.agent != nil {
				tools = m.agent.GetTools()
			} else {
				tools = m.getInitialTools()
			}
			next := agent.NewAgent(client, tools, m.config)
			next.RestoreMCPToolNames(mcpNames)
			m.activeModel = args[0]
			if m.config != nil {
				m.config.Model = args[0]
			}
			// SaveLastModel persists any model name to ocodeconfig.json (project-level)
			if err := config.SaveLastModel(args[0]); err != nil {
				log.Printf("save last model: %v", err)
			}
			// SaveRecentModel requires "provider/model" format and goes to the global state file
			if strings.Contains(args[0], "/") {
				if err := config.SaveRecentModel(args[0]); err != nil {
					log.Printf("save recent model: %v", err)
				}
			}
			return m.replaceAgent(next)
		}
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Failed to switch to model %s: unknown provider or missing configuration", args[0])})
	}
	return nil
}

const (
	maxExplicitTitleLen = 80
)

func truncateTitle(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}

func (m *model) handleTitleCmd(args []string) tea.Cmd {
	if len(args) > 0 {
		title := strings.TrimSpace(strings.Join(args, " "))
		if title == "" {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /title [text]"})
			return nil
		}
		title = truncateTitle(title, maxExplicitTitleLen)
		m.sessionTitle = title
		m.saveSession()
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Session title set to %q.", title)})
		return nil
	}

	m.sessionTitle = ""
	m.titleRequested = false
	m.titleGen++
	m.saveSession()
	m.messages = append(m.messages, message{role: roleAssistant, text: "Title cleared — will auto-generate from next assistant response."})
	return nil
}

func (m *model) handleThinkingCmd(args []string) {
	m.showThinking = !m.showThinking
	status := "hidden"
	if m.showThinking {
		status = "visible"
	}
	m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Thinking blocks are now %s.", status)})
}

func maskKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + strings.Repeat("*", len(key)-8) + key[len(key)-4:]
}

func (m *model) handleConnectCmd(args []string) {
	if len(args) == 0 {
		m.openConnectDialog()
		return
	}
	if len(args) == 2 {
		provider := args[0]
		key := args[1]
		p := auth.FindProvider(provider)
		if p == nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Unknown provider: %s", provider)})
			return
		}
		if err := auth.Set(p.ID, auth.Credential{Kind: auth.KindAPIKey, Key: key}); err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Failed to save key: %v", err)})
			return
		}
		if p.EnvVar != "" {
			os.Setenv(p.EnvVar, key)
		}
		m.rebuildAgentClient()
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf(
			"API key for %s set (%s). Saved to ~/.config/ocode/auth.json.", p.Label, maskKey(key),
		)})
		return
	}
	m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /connect [provider apikey] — or run /connect with no args for the dialog."})
}

func (m *model) handleSessionCmd(args []string) {
	if len(args) == 0 {
		m.openSessionPicker()
	} else if args[0] == "list" {
		sessions, _ := session.ListAll()
		var b strings.Builder
		b.WriteString("Sessions:\n")
		for _, s := range sessions {
			title := s.Title
			if title == "" {
				title = "(no title)"
			}
			marker := "ocode"
			if s.Source == session.SourceClaude {
				marker = "claude"
			}
			b.WriteString(fmt.Sprintf("- [%s] %s: %s\n", marker, s.ID, title))
		}
		m.messages = append(m.messages, message{role: roleAssistant, text: b.String()})
	} else if args[0] == "load" && len(args) > 1 {
		sess, err := session.LoadAny(args[1])
		if err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Error loading session: %v", err)})
		} else {
			m.saveSession()
			m.sessionID = sess.ID
			m.sessionTitle = sess.Title
			m.titleRequested = sess.Title != ""
			tool.SetTodoSession(m.sessionID)
			snapshot.Reset()
			tool.ResetTodoState()
			m.sessionTelemetry = telemetryFromSessionMetadata(sess.Metadata)
			restoreTodoState(sess.Metadata)
			m.messages = []message{}
			m.streamingThinkingIdx = -1
			roleCounts := map[string]int{}
			for _, am := range sess.Messages {
				roleCounts[am.Role]++
				role := tuiRoleForAgentMessage(am)
				copyMsg := am
				m.messages = append(m.messages, message{role: role, text: displayTextForAgentMessage(am), raw: &copyMsg})
			}
			agent.DebugAppendf("SESSION", "loaded session %s: %d msgs (user=%d asst=%d tool=%d system=%d)", m.sessionID, len(sess.Messages), roleCounts["user"], roleCounts["assistant"], roleCounts["tool"], roleCounts["system"])
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Loaded session %s", m.sessionID)})
			m.input.Focus()
			m.layout()
			m.viewport.GotoBottom()
		}
	} else {
		m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /session [list|load <id>]"})
	}
}

func tuiRoleForAgentMessage(msg agent.Message) role {
	if msg.Role == "user" {
		return roleUser
	}
	return roleAssistant
}

func displayTextForAgentMessage(msg agent.Message) string {
	if msg.Role == "system" && strings.HasPrefix(msg.Content, "[Compacted summary of ") {
		return "📦 " + msg.Content
	}
	if msg.Role == "system" && strings.HasPrefix(msg.Content, "[ocode:compaction-summary]") {
		body := strings.TrimSpace(strings.TrimPrefix(msg.Content, "[ocode:compaction-summary]"))
		if body == "" {
			return "📦 Compacted summary"
		}
		return "📦 " + body
	}
	var text string
	if len(msg.Images) == 0 {
		text = msg.Content
	} else {
		var b strings.Builder
		b.WriteString(msg.Content)
		if msg.Content != "" {
			b.WriteString("\n")
		}
		for _, img := range msg.Images {
			label := img.Path
			if label == "" {
				label = img.MIMEType
			}
			b.WriteString(fmt.Sprintf("[image: %s]\n", label))
		}
		text = strings.TrimRight(b.String(), "\n")
	}
	if len(msg.ToolCalls) > 0 {
		var b strings.Builder
		if text != "" {
			b.WriteString(text)
			b.WriteString("\n\n")
		}
		for i, tc := range msg.ToolCalls {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(formatToolCallHint(tc))
		}
		return b.String()
	}
	return text
}

func (m *model) handleCompactCmd(args []string) {
	newMsgs := []message{}
	for _, msg := range m.messages {
		if msg.role == roleUser || (msg.role == roleAssistant && msg.raw == nil) {
			newMsgs = append(newMsgs, msg)
		}
	}
	m.messages = newMsgs
	m.messages = append(m.messages, message{role: roleAssistant, text: "Conversation compacted (removed tool history from view)."})
	m.sessionTelemetry = sidebarTelemetry{}
}

func (m *model) handleRedoCmd(args []string) {
	path, err := snapshot.Redo()
	if err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Error redoing: %v", err)})
	} else {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Successfully restored changes to %s", path)})
	}
}

func (m *model) handleExportCmd(args []string) {
	filename := fmt.Sprintf("ocode_export_%d.md", time.Now().Unix())
	var b strings.Builder
	for _, msg := range m.messages {
		role := "User"
		if msg.role == roleAssistant {
			role = "Assistant"
		}
		b.WriteString(fmt.Sprintf("## %s\n\n%s\n\n", role, msg.text))
	}
	err := os.WriteFile(filename, []byte(b.String()), 0644)
	if err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Error exporting: %v", err)})
	} else {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Exported conversation to %s", filename)})
	}
}

func (m *model) handleExportClaudeCmd(args []string) {
	msgs := m.persistedAgentMessages()
	if len(msgs) == 0 {
		m.messages = append(m.messages, message{role: roleAssistant, text: "No messages available to export."})
		return
	}
	path, err := session.AppendClaudeSession(m.sessionID, msgs)
	if err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Error exporting Claude Code session: %v", err)})
		return
	}
	m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Appended current session to Claude Code history: %s", path)})
}

func (m *model) handleNewCmd(args []string) tea.Cmd {
	cmd := m.resetSessionAgent()
	m.messages = []message{}
	m.streamingThinkingIdx = -1
	m.sessionID = time.Now().Format("2006-01-02-150405")
	m.sessionTitle = ""
	m.titleRequested = false
	m.titleGen++
	tool.SetTodoSession(m.sessionID)
	snapshot.Reset()
	tool.ResetTodoState()
	m.sessionTelemetry = sidebarTelemetry{}
	m.lastActivity = agent.ActivitySnapshot{}
	m.detail = nil
	m.agentStripOffset = 0
	m.agentStripSelected = 0
	m.agentStripFocused = false
	m.inputHistory = nil
	m.inputHistoryIndex = -1
	m.messages = append(m.messages, message{role: roleAssistant, text: "Started new session.", transient: true})
	return cmd
}

func (m *model) resetSessionAgent() tea.Cmd {
	prev := m.agent
	var next *agent.Agent
	if prev == nil {
		tools := m.getInitialTools()
		modelName := m.currentModelName()
		if modelName == "" && m.config != nil {
			modelName = m.config.Model
		}
		client := agent.NewClient(m.config, modelName)
		if client != nil {
			next = agent.NewAgent(client, tools, m.config)
			next.SetMode(agent.ModeBuild)
			if next.Permissions() != nil {
				next.Permissions().SetWorkDir(m.workDir)
			}
			next.LoadExternalTools(m.config)
		}
	} else {
		tools := prev.GetTools()
		if len(tools) == 0 {
			tools = m.getInitialTools()
		}
		mcpNames := prev.MCPToolNames()
		mode := prev.Mode()
		spec := prev.Spec()
		permCfg := prev.Permissions().ExportConfig()

		modelName := m.currentModelName()
		if modelName == "" && m.config != nil {
			modelName = m.config.Model
		}
		client := agent.NewClient(m.config, modelName)
		if client == nil {
			client = prev.Client()
		}

		next = agent.NewAgent(client, tools, m.config)
		next.SetMode(mode)
		next.SetSpec(spec)
		if next.Permissions() != nil {
			next.Permissions().LoadFromOcode(permCfg)
			next.Permissions().SetWorkDir(m.workDir)
		}
		if len(mcpNames) > 0 {
			next.RestoreMCPToolNames(mcpNames)
		}
	}

	if prev != nil {
		prev.Cancel()
		if prev.Runs() != nil {
			prev.Runs().CancelAll()
		}
	}
	if m.supervisor != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = m.supervisor.Shutdown(ctx)
	}
	if prev != nil {
		m.cleanupAgent(prev)
	}
	m.supervisor = tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{GracePeriod: 5 * time.Second})
	return m.installAgent(next)
}

func (m *model) handleEditorCmd(args []string) tea.Cmd {
	if len(args) == 0 {
		m.openEditorPicker()
		return nil
	}
	editor := strings.Join(args, " ")
	if err := validateEditorCmd(editor); err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Invalid editor: %v", err)})
		return nil
	}
	if err := config.SaveEditor(editor); err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Failed to save editor: %v", err)})
		return nil
	}
	if m.config != nil {
		m.config.Ocode.Editor = editor
	}
	m.refreshEditorOpener()
	m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Default editor set to: %s", editor)})
	return nil
}

func (m *model) handleEditorModeCmd(args []string) tea.Cmd {
	if len(args) == 0 {
		m.openEditorModePicker()
		return nil
	}
	mode := args[0]
	if mode != config.EditorModeExternal && mode != config.EditorModeTmuxSplit && mode != config.EditorModeTmuxWindow {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Invalid editor mode: %q. Valid modes: %s, %s, %s", mode, config.EditorModeExternal, config.EditorModeTmuxSplit, config.EditorModeTmuxWindow)})
		return nil
	}
	if err := validateTmuxEditorMode(mode); err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Cannot set tmux editor mode: %v", err)})
		return nil
	}
	if err := config.SaveEditorMode(mode); err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Failed to save editor mode: %v", err)})
		return nil
	}
	if m.config != nil {
		m.config.Ocode.EditorMode = mode
	}
	m.refreshEditorOpener()
	m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Editor mode set to: %s", mode)})
	return nil
}

func (m *model) handleUsageCmd(args []string) tea.Cmd {
	// Determine date range
	var from, to time.Time

	if len(args) == 0 {
		// Show a help message with options
		var b strings.Builder
		b.WriteString("📊 Usage Summary\n\n")
		b.WriteString("Available date ranges:\n")
		for _, dr := range usage.DateRanges {
			fmt.Fprintf(&b, "  /usage %s\n", dr.Label)
		}
		b.WriteString("\nExamples:\n")
		b.WriteString("  /usage hour         - Last 1 hour\n")
		b.WriteString("  /usage day          - Today\n")
		b.WriteString("  /usage week         - Last 7 days\n")
		b.WriteString("  /usage month        - Last 30 days\n")
		b.WriteString("  /usage last-month   - Previous calendar month\n")
		b.WriteString("  /usage last-3-month - Previous 3 calendar months\n")
		b.WriteString("  /usage all          - All time\n")
		m.messages = append(m.messages, message{role: roleAssistant, text: b.String()})
		return nil
	}

	arg := strings.ToLower(strings.TrimSpace(args[0]))
	now := time.Now()

	switch arg {
	case "hour", "1h":
		from = now.Add(-1 * time.Hour)
		to = now
	case "day", "today":
		from = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		to = now
	case "week", "7d":
		from = now.Add(-7 * 24 * time.Hour)
		to = now
	case "month", "30d":
		from = now.Add(-30 * 24 * time.Hour)
		to = now
	case "last-month", "lastmonth":
		firstOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		from = firstOfMonth.AddDate(0, -1, 0)
		to = firstOfMonth.Add(-time.Nanosecond)
	case "last-3-month", "last-3-months", "last3month", "last3months", "quarter":
		firstOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		from = firstOfMonth.AddDate(0, -3, 0)
		to = firstOfMonth.Add(-time.Nanosecond)
	case "all", "all-time", "alltime":
		from = time.Time{}
		to = now
	default:
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Unknown date range: %q. Use /usage to see available options.", arg)})
		return nil
	}

	return func() tea.Msg {
		records, err := usage.Query(from, to)
		if err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Error querying usage: %v", err)})
			return nil
		}
		summary := usage.Summarize(records)
		output := usage.FormatSummary(summary)
		m.messages = append(m.messages, message{role: roleAssistant, text: output})
		return nil
	}
}

func (m *model) refreshEditorOpener() {
	if m.config == nil {
		return
	}
	editor := config.ResolveEditor(&m.config.Ocode)
	mode := m.config.Ocode.EditorMode
	m.files.SetEditor(editor)
	m.files.SetEditorMode(mode)
	m.files.SetEditorOpener(createEditorOpener(editor, mode, func() int { return m.width }, m.supervisor))
	m.git.SetEditor(editor)
	m.git.SetEditorOpener(createEditorOpener(editor, mode, func() int { return m.width }, m.supervisor))
}

// runCapturedShell runs `command` non-interactively, capturing combined
// stdout/stderr, and emits a shellFinishedMsg with the output.
func (m *model) runCapturedShell(command string, dir string, toolCallID string) tea.Cmd {
	supervisor := m.supervisor
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 600*time.Second)
		defer cancel()
		var c *exec.Cmd
		if runtime.GOOS == "windows" {
			c = exec.CommandContext(ctx, "cmd", "/C", command)
		} else {
			c = exec.CommandContext(ctx, "bash", "-c", command)
		}
		if dir != "" {
			c.Dir = dir
		}
		if runtime.GOOS != "windows" {
			c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		}
		var buf bytes.Buffer
		c.Stdout = &buf
		c.Stderr = &buf

		id := fmt.Sprintf("shell-cmd-%d-%d", os.Getpid(), time.Now().UnixNano())
		if supervisor != nil {
			_, _ = supervisor.Register(tool.ProcessRegistration{
				ID:               id,
				Command:          command,
				Kind:             tool.ProcessKindBackgroundBash,
				Cmd:              c,
				OwnsProcessGroup: runtime.GOOS != "windows",
				StartedAt:        time.Now(),
			})
		}

		err := c.Run()
		out := buf.String()
		if ctx.Err() == context.DeadlineExceeded {
			err = fmt.Errorf("timed out after 600s")
		}
		if supervisor != nil {
			if err == nil {
				supervisor.MarkExited(id, 0)
			} else {
				code := 1
				if exitErr, ok := err.(*exec.ExitError); ok {
					code = exitErr.ExitCode()
				}
				supervisor.MarkKilled(id, code)
			}
		}
		return shellFinishedMsg{command: command, output: out, toolCallID: toolCallID, err: err}
	}
}

func shellExecCommand(command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/C", command)
	}
	return exec.Command("bash", "-c", command)
}

var openSidebarFileInEditor = openPathInEditor

func openPathInEditor(path string) tea.Cmd {
	editor, ok := resolveEditor(os.Getenv("EDITOR"))
	if !ok {
		return func() tea.Msg { return errorMsg(fmt.Errorf("EDITOR not set and no common editor found")) }
	}

	cmdParts := strings.Fields(editor)
	cmdParts = append(cmdParts, path)
	c := exec.Command(cmdParts[0], cmdParts[1:]...)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		if err != nil {
			return errorMsg(err)
		}
		return nil
	})
}

func resolveEditor(editor string) (string, bool) {
	if editor != "" {
		return editor, true
	}
	for _, candidate := range []string{"vim", "nano", "notepad"} {
		if _, err := exec.LookPath(candidate); err == nil {
			return candidate, true
		}
	}
	return "", false
}

func (m *model) handleShareCmd(args []string) {
	filename := fmt.Sprintf("ocode_share_%s.md", m.sessionID)
	var b strings.Builder
	b.WriteString("# Shared OpenCode Session\n\n")
	for _, msg := range m.messages {
		role := "User"
		if msg.role == roleAssistant {
			role = "Assistant"
		}
		b.WriteString(fmt.Sprintf("### %s\n\n%s\n\n", role, msg.text))
	}
	os.WriteFile(filename, []byte(b.String()), 0644)
	m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Session shared to local file: %s", filename)})
}

func (m *model) handleThemesCmd(args []string) {
	if len(args) == 0 {
		m.openThemePicker()
		return
	}

	name := args[0]
	if _, ok := GetTheme(name); !ok {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Unknown theme: %s", name)})
		return
	}

	m.config.Ocode.TUI.Theme = name
	m.applyTheme()
	if err := config.SaveTUITheme(name); err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Theme switched to %s (save failed: %v)", name, err)})
	} else {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Theme switched to %s", name)})
	}
}

// handleModelsCmd is an alias for handleModelCmd; see commandSpecs for the /model ↔ /models aliasing.
func (m *model) handleModelsCmd(args []string) tea.Cmd {
	return m.handleModelCmd(args)
}

func (m *model) handleDetailsCmd(args []string) {
	m.showDetails = !m.showDetails
	status := "hidden"
	if m.showDetails {
		status = "visible"
	}
	m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Tool execution details are now %s.", status)})
}

func (m *model) handleInitCmd(args []string) tea.Cmd {
	prompt := strings.ReplaceAll(initializePromptTemplate, "$ARGUMENTS", strings.Join(args, " "))
	m.messages = append(m.messages, message{role: roleUser, text: "/init " + strings.Join(args, " ")})
	if m.agent != nil {
		m.agent.ResetSubagentDispatch()
	}
	m.rerenderTranscriptAndMaybeScroll()
	return m.sendCustomCommandPrompt(prompt)
}

func (m *model) handleHelpCmd(args []string) {
	m.messages = append(m.messages, message{role: roleAssistant, text: commandHelpText()})
}

func (m *model) handleSkillsCmd(args []string) {
	skills := skill.LoadSkills()
	if len(skills) == 0 {
		m.messages = append(m.messages, message{role: roleAssistant, text: "No skills found."})
		return
	}
	var b strings.Builder
	b.WriteString("Available skills:\n")
	for _, s := range skills {
		b.WriteString(fmt.Sprintf("- %s: %s\n", s.Name, s.Description))
	}
	m.messages = append(m.messages, message{role: roleAssistant, text: b.String()})
}

func (m *model) handleContextCmd(args []string) {
	if m.agent == nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: "No agent configured."})
		return
	}

	var b strings.Builder
	b.WriteString("Context Budget\n")
	b.WriteString(strings.Repeat("═", 38) + "\n")

	// ── Base Prompt ──────────────────────────────
	b.WriteString("\nBase Prompt\n")
	baseTotal := 0
	for _, msg := range m.agent.BasePromptMessages("") {
		if !strings.Contains(msg.Content, "[ocode:environment]") {
			continue
		}
		tok := estimateTok(msg.Content)
		baseTotal += tok
		fmt.Fprintf(&b, "  %-28s ~%s tok\n", "Environment", formatTok(tok))
		break
	}

	modePrompt := m.agent.Mode().SystemPrompt()
	modeTok := estimateTok(modePrompt)
	baseTotal += modeTok
	modeLabel := fmt.Sprintf("Mode (%s)", m.agent.Mode().String())
	fmt.Fprintf(&b, "  %s%s~%s tok\n", modeLabel, columnPad(modeLabel, 28), formatTok(modeTok))

	ambientFiles := []string{"AGENTS.md", "CLAUDE.md", ".cursorrules"}
	rulesDir := filepath.Join(".opencode", "rules")
	if entries, err := os.ReadDir(rulesDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && filepath.Ext(e.Name()) == ".md" {
				ambientFiles = append(ambientFiles, filepath.Join(rulesDir, e.Name()))
			}
		}
	}
	anyAmbient := false
	for _, f := range ambientFiles {
		content, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		anyAmbient = true
		tok := estimateTok(string(content))
		baseTotal += tok
		label := filepath.Base(f)
		fmt.Fprintf(&b, "  %-28s ~%s tok\n", label, formatTok(tok))
	}
	if !anyAmbient {
		b.WriteString("  (no ambient files found)\n")
	}

	plugs := plugins.LoadPlugins()
	for _, p := range plugs {
		if p.Instructions == "" {
			continue
		}
		tok := estimateTok(p.Instructions)
		baseTotal += tok
		fmt.Fprintf(&b, "  Plugin: %-20s ~%s tok\n", p.Name, formatTok(tok))
	}
	fmt.Fprintf(&b, "  %-28s ~%s tok\n", "Subtotal", formatTok(baseTotal))

	// ── Tools ────────────────────────────────────
	b.WriteString("\nTools (injected every request)\n")
	toolsTotal := 0
	allDefs := m.agent.GetToolDefinitions()
	mcpSet := make(map[string]struct{})
	for _, name := range m.agent.MCPToolNames() {
		mcpSet[name] = struct{}{}
	}
	var serverNames []string
	if m.config != nil {
		serverNames = make([]string, 0, len(m.config.MCP))
		for name := range m.config.MCP {
			serverNames = append(serverNames, name)
		}
	}
	sort.Strings(serverNames)

	grouped, builtinDefs := groupMCPToolDefs(allDefs, mcpSet, serverNames)
	builtinTok := 0
	for _, def := range builtinDefs {
		raw, _ := json.Marshal(def)
		builtinTok += estimateTok(string(raw))
	}
	toolsTotal += builtinTok
	builtinLabel := fmt.Sprintf("Built-in (%d tools)", len(builtinDefs))
	fmt.Fprintf(&b, "  %s%s~%s tok\n", builtinLabel, columnPad(builtinLabel, 28), formatTok(builtinTok))

	if len(serverNames) == 0 {
		fmt.Fprintf(&b, "  %-28s ~%s tok\n", "MCP: (none)", "0")
	}
	for _, srv := range serverNames {
		defs, ok := grouped[srv]
		if !ok {
			continue
		}
		srvTok := 0
		for _, def := range defs {
			raw, _ := json.Marshal(def)
			srvTok += estimateTok(string(raw))
		}
		toolsTotal += srvTok
		label := fmt.Sprintf("MCP: %s  %d tools", srv, len(defs))
		fmt.Fprintf(&b, "  %-28s ~%s tok\n", label, formatTok(srvTok))
	}
	fmt.Fprintf(&b, "  %-28s ~%s tok\n", "Subtotal", formatTok(toolsTotal))

	injectedTotal := baseTotal + toolsTotal
	b.WriteString("\nSkill catalog (pre-injected)\n")
	skillCatalog := skill.BuildCatalog()
	if skillCatalog == "" {
		b.WriteString("  (none found)\n")
	} else {
		catalogTok := estimateTok(skillCatalog)
		injectedTotal += catalogTok
		fmt.Fprintf(&b, "  %-28s ~%s tok\n", "Compact catalog", formatTok(catalogTok))
	}
	fmt.Fprintf(&b, "\n  %-28s ~%s tok\n", "Injected per request", formatTok(injectedTotal))

	// ── Skills ───────────────────────────────────
	b.WriteString("\nSkills (full contents available on demand, not pre-injected)\n")
	skills := skill.LoadSkills()
	if len(skills) == 0 {
		b.WriteString("  (none found)\n")
	} else {
		shown := skills
		extra := 0
		if len(skills) > 5 {
			shown = skills[:5]
			extra = len(skills) - 5
		}
		skillTotal := 0
		for _, s := range skills {
			skillTotal += estimateTok(s.Content)
		}
		for _, s := range shown {
			tok := estimateTok(s.Content)
			fmt.Fprintf(&b, "  %-28s ~%s tok\n", s.Name, formatTok(tok))
		}
		if extra > 0 {
			moreLabel := fmt.Sprintf("... +%d more (%d total)", extra, len(skills))
			fmt.Fprintf(&b, "  %s%s~%s tok available\n", moreLabel, columnPad(moreLabel, 24), formatTok(skillTotal))
		}
	}

	// ── Session Messages ─────────────────────────
	b.WriteString("\nSession Messages\n")
	modelName := m.currentModelName()

	ctxTokens, ctxSource := m.currentContextEstimate()
	if ctxTokens > 0 {
		if window, ok := modelContextWindow(modelName); ok {
			pct := formatPercent(ctxTokens, window)
			fmt.Fprintf(&b, "  Context    %s / %s (%s)  %s\n", strconv.FormatInt(ctxTokens, 10), strconv.FormatInt(window, 10), pct, ctxSource)
		} else {
			fmt.Fprintf(&b, "  Context    %s tok  %s\n", strconv.FormatInt(ctxTokens, 10), ctxSource)
		}
	} else {
		b.WriteString("  Context    n/a\n")
	}

	lastIn, lastOut, lastTotal := latestRequestUsage(m.messages)
	if lastTotal > 0 {
		fmt.Fprintf(&b, "  Last req   In %s  Out %s  Total %s\n", strconv.FormatInt(lastIn, 10), strconv.FormatInt(lastOut, 10), strconv.FormatInt(lastTotal, 10))
	}

	telemetry := m.sessionTelemetry
	if !telemetry.hasData() {
		telemetry = aggregateSidebarTelemetry(m.messages)
	}
	if telemetry.hasData() {
		fmt.Fprintf(&b, "  Usage      In %s  Cache %s  Out %s\n", strconv.FormatInt(telemetry.inputTokens, 10), strconv.FormatInt(telemetry.cachedTokens, 10), strconv.FormatInt(telemetry.outputTokens, 10))
		if telemetry.spend != nil {
			fmt.Fprintf(&b, "             $%.4f\n", *telemetry.spend)
		}
	} else {
		b.WriteString("  Usage      n/a\n")
	}

	m.messages = append(m.messages, message{role: roleAssistant, text: b.String()})
}

func (m model) buildSelectionContext() string {
	var b strings.Builder
	writeHeader := func() {
		if b.Len() == 0 {
			b.WriteString("[Selected context]\n")
		}
	}

	if len(m.files.selectedFiles) > 0 {
		writeHeader()
		b.WriteString("\n## Files\n")
		indices := make([]int, 0, len(m.files.selectedFiles))
		for idx := range m.files.selectedFiles {
			indices = append(indices, idx)
		}
		sort.Ints(indices)
		for _, idx := range indices {
			if idx < 0 || idx >= len(m.files.nodes) {
				continue
			}
			n := m.files.nodes[idx]
			path := n.path
			if rel, err := filepath.Rel(m.workDir, path); err == nil && !strings.HasPrefix(rel, "..") {
				path = rel
			}
			if path == "" {
				path = n.name
			}
			b.WriteString("- ")
			b.WriteString(path)
			b.WriteString("\n")
		}
	}

	if m.filesSel.active && m.files.previewPath != "" && len(m.files.previewRawLines) > 0 {
		writeHeader()
		path := m.files.previewPath
		if rel, err := filepath.Rel(m.workDir, path); err == nil && !strings.HasPrefix(rel, "..") {
			path = rel
		}
		startLine, _, endLine, _ := normaliseSelection(m.filesSel.startLine, m.filesSel.startCol, m.filesSel.endLine, m.filesSel.endCol)
		b.WriteString("\n## File selection: ")
		b.WriteString(path)
		b.WriteString("\n")
		for i := startLine; i <= endLine && i < len(m.files.previewRawLines); i++ {
			if i < 0 {
				continue
			}
			fmt.Fprintf(&b, "%d: %s\n", i+1, m.files.previewRawLines[i])
		}
	}

	if len(m.git.selectedFiles) > 0 {
		allFiles := m.git.currentFileList()
		var files []gitFile
		for idx := range m.git.selectedFiles {
			if idx >= 0 && idx < len(allFiles) {
				files = append(files, allFiles[idx])
			}
		}
		if len(files) > 0 {
			writeHeader()
			b.WriteString("\n## Git diff\n")
			for _, f := range files {
				b.WriteString("- ")
				b.WriteString(f.path)
				b.WriteString("\n")
			}
		}
	}

	return b.String()
}

func (m model) buildSelectionSidebarBody(width int) []string {
	body, _ := m.buildSelectionSidebarData(width)
	return body
}

func (m model) buildSelectionSidebarData(width int) ([]string, []string) {
	if width < 8 {
		width = 8
	}
	var body []string
	var filePaths []string
	if len(m.files.selectedFiles) > 0 {
		indices := make([]int, 0, len(m.files.selectedFiles))
		for idx := range m.files.selectedFiles {
			indices = append(indices, idx)
		}
		sort.Ints(indices)
		for _, idx := range indices {
			if idx < 0 || idx >= len(m.files.nodes) {
				continue
			}
			n := m.files.nodes[idx]
			body = append(body, "• "+formatSidebarFilePath(n.path, m.workDir, maxInt(1, width-2)))
			filePaths = append(filePaths, n.path)
		}
	}
	if m.filesSel.active && m.files.previewPath != "" && len(m.files.previewRawLines) > 0 {
		startLine, _, endLine, _ := normaliseSelection(m.filesSel.startLine, m.filesSel.startCol, m.filesSel.endLine, m.filesSel.endCol)
		lineLabel := fmt.Sprintf("↳ %s:%d-%d", formatSidebarFilePath(m.files.previewPath, m.workDir, maxInt(1, width-7)), startLine+1, endLine+1)
		body = append(body, lineLabel)
		filePaths = append(filePaths, m.files.previewPath)
	}
	return body, filePaths
}

func (m model) prepareAgentMessages(msgs []agent.Message) []agent.Message {
	if m.agent == nil {
		return msgs
	}
	return m.agent.PrepareMessages(msgs, m.buildSelectionContext())
}

func (m *model) sendCustomCommandPrompt(prompt string) tea.Cmd {
	return func() tea.Msg {
		if m.agent == nil {
			return errorMsg(fmt.Errorf("no agent configured"))
		}
		agentMsgs := []agent.Message{{Role: "user", Content: prompt}}
		agentMsgs = m.prepareAgentMessages(agentMsgs)
		resp, err := m.agent.Step(agentMsgs)
		if err != nil {
			return errorMsg(err)
		}
		return resp
	}
}

func (m *model) handleUndoCmd(args []string) {
	path, err := snapshot.Undo()
	if err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Error undoing: %v", err)})
	} else {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Successfully reverted changes to %s", path)})
	}
}

func (m *model) handleGitHubPR(owner, repo string, prNumber int) (string, error) {
	pr, err := ghGetPR(owner, repo, prNumber)
	if err != nil {
		return "", err
	}
	diff, err := ghGetPRDiff(owner, repo, prNumber)
	if err != nil {
		diff = "(diff unavailable)"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("PR #%d: %s\n", pr.Number, pr.Title))
	b.WriteString(fmt.Sprintf("State: %s | Author: %s\n\n", pr.State, pr.User.Login))
	if pr.Body != "" {
		b.WriteString(pr.Body + "\n\n")
	}
	b.WriteString("--- DIFF ---\n")
	b.WriteString(diff)
	return b.String(), nil
}

func (m *model) handleGitHubIssueList(owner, repo, state string) (string, error) {
	issues, err := ghListIssues(owner, repo, state)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, issue := range issues {
		labels := strings.Join(issue.Labels, ", ")
		b.WriteString(fmt.Sprintf("#%d: %s [%s] by %s", issue.Number, issue.Title, issue.State, issue.Author))
		if labels != "" {
			b.WriteString(fmt.Sprintf(" | labels: %s", labels))
		}
		b.WriteString("\n")
	}
	if len(issues) == 0 {
		return "No issues found.", nil
	}
	return b.String(), nil
}

func (m *model) handleGitHubIssueGet(owner, repo string, number int) (string, error) {
	issue, err := ghGetIssue(owner, repo, number)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("#%d: %s\n", issue.Number, issue.Title))
	b.WriteString(fmt.Sprintf("State: %s | Author: %s\n", issue.State, issue.Author))
	if len(issue.Labels) > 0 {
		b.WriteString(fmt.Sprintf("Labels: %s\n", strings.Join(issue.Labels, ", ")))
	}
	b.WriteString("\n")
	b.WriteString(issue.Body)
	return b.String(), nil
}

func (m *model) handleGitHubWorkflow(name string) string {
	return ghGenerateWorkflow(name, nil)
}

func (m *model) saveSession() {
	agentMsgs := m.persistedAgentMessages()
	if len(agentMsgs) == 0 {
		return
	}
	roleCounts := map[string]int{}
	for _, m := range agentMsgs {
		roleCounts[m.Role]++
	}
	session.Save(m.sessionID, m.sessionTitle, agentMsgs, m.sessionSidebarMetadata())
	agent.DebugAppendf("SESSION", "saved session %s (%d msgs: user=%d asst=%d tool=%d system=%d)", m.sessionID, len(agentMsgs), roleCounts["user"], roleCounts["assistant"], roleCounts["tool"], roleCounts["system"])
}

func (m *model) persistedAgentMessages() []agent.Message {
	if len(m.messages) == 0 {
		return nil
	}
	var agentMsgs []agent.Message
	for _, msg := range m.messages {
		if msg.transient || msg.role == roleThinking {
			continue
		}
		if msg.raw != nil {
			agentMsgs = append(agentMsgs, *msg.raw)
		} else {
			role := "user"
			if msg.role == roleAssistant {
				role = "assistant"
			}
			agentMsgs = append(agentMsgs, agent.Message{Role: role, Content: msg.text})
		}
	}
	return agentMsgs
}

func countUserMessages(msgs []message) int {
	n := 0
	for _, msg := range msgs {
		if msg.role == roleUser && !msg.transient {
			n++
		}
	}
	return n
}

var ocodeTitleRe = regexp.MustCompile(`(?s)<ocode-title>(.*?)</ocode-title>`)

func extractSessionTitle(content string) (title, rest string) {
	m := ocodeTitleRe.FindStringSubmatchIndex(content)
	if m == nil {
		return "", content
	}
	title = strings.TrimSpace(content[m[2]:m[3]])
	// Remove the title tag from content, preserving surrounding text
	rest = content[:m[0]] + content[m[1]:]
	rest = strings.TrimSpace(rest)
	return title, rest
}

// maybeGenerateTitle asynchronously generates a session title once, after the
// first assistant response with non-empty content has landed. Subsequent
// responses are ignored unless /title clears the session title (which also
// resets titleRequested via handleTitleCmd).
func (m *model) maybeGenerateTitle(assistantContent string) {
	if m.agent == nil || m.titleRequested || m.sessionTitle != "" {
		return
	}
	if strings.TrimSpace(assistantContent) == "" {
		return
	}
	userMsg := m.lastUserMessageText()
	if strings.TrimSpace(userMsg) == "" {
		return
	}
	m.titleRequested = true
	ch := m.titleCh
	gen := m.titleGen
	m.agent.GenerateTitleAsync(userMsg, assistantContent, func(t string) {
		select {
		case ch <- titleResult{title: t, gen: gen}:
		default:
		}
	})
}

func (m *model) lastUserMessageText() string {
	for i := len(m.messages) - 1; i >= 0; i-- {
		msg := m.messages[i]
		if msg.role == roleUser && !msg.transient {
			return msg.text
		}
	}
	return ""
}

// firstUserPromptText returns the text of the first non-transient user message.
// This serves as a fallback session title when no LLM-generated title is available.
func (m *model) firstUserPromptText() string {
	for _, msg := range m.messages {
		if msg.role == roleUser && !msg.transient {
			text := strings.TrimSpace(msg.text)
			if text != "" {
				return text
			}
		}
	}
	return ""
}

func (m *model) appendAgentMessage(am agent.Message) {
	copyMsg := am
	if am.Role == "assistant" {
		if am.ReasoningContent != "" && m.showThinking {
			if m.streamingThinkingIdx >= 0 && m.streamingThinkingIdx < len(m.messages) && m.messages[m.streamingThinkingIdx].role == roleThinking {
				// Streamed live — replace partial buffer with the canonical text
				// and collapse to last-3-lines view now that streaming is done.
				m.messages[m.streamingThinkingIdx].text = am.ReasoningContent
				delete(m.expandedThinking, m.streamingThinkingIdx)
			} else {
				m.messages = append(m.messages, message{
					role: roleThinking,
					text: am.ReasoningContent,
				})
			}
		}
		m.streamingThinkingIdx = -1
		content := am.Content
		if m.sessionTitle == "" && content != "" {
			if title, rest := extractSessionTitle(content); title != "" {
				m.sessionTitle = title
				content = rest
				copyMsg.Content = content
			}
		}
		if len(am.ToolCalls) > 0 {
			var b strings.Builder
			if content != "" {
				b.WriteString(content)
				b.WriteString("\n\n")
			}
			for i, tc := range am.ToolCalls {
				if i > 0 {
					b.WriteString("\n")
				}
				b.WriteString(formatToolCallHint(tc))
			}
			m.messages = append(m.messages, message{role: roleAssistant, text: b.String(), raw: &copyMsg})
		} else if content != "" {
			m.messages = append(m.messages, message{role: roleAssistant, text: content, raw: &copyMsg})
		}
		m.maybeGenerateTitle(content)
	} else if am.Role == "tool" {
		if strings.HasPrefix(am.Content, tool.SentinelPermissionAsk) {
			if req, ok := parsePermissionRequest(am.Content); ok {
				m.showPermDialog = true
				m.activeTab = tabChat
				m.chatUnread = false
				m.pendingPermission = req
				m.pendingToolName = req.ToolName
				m.pendingToolArgs = req.Args
				m.pendingToolCallID = am.ToolID
				m.messages = append(m.messages, message{role: roleAssistant, text: renderPermissionPrompt(req), raw: &copyMsg})
			}
		} else if prompts, ok := parseQuestionPrompt(am.Content); ok {
			m.startQuestionPrompt(am.ToolID, prompts)
			m.messages = append(m.messages, message{role: roleAssistant, text: renderQuestionTranscriptNotice(prompts), raw: &copyMsg})
		} else {
			toolName := m.lookupToolName(am.ToolID)
			m.messages = append(m.messages, message{
				role: roleAssistant,
				text: renderToolResult(toolName, am.Content, m.styles),
				raw:  &copyMsg,
			})
		}
	}
	if am.Usage != nil || am.Spend != nil {
		m.sessionTelemetry.addMessage(am)
		// Record usage to persistent storage
		m.recordUsage(am)
	}
}

// recordUsage persists a usage record for a message that has token usage data.
func (m *model) recordUsage(am agent.Message) {
	if am.Usage == nil {
		return
	}
	u := am.Usage
	promptTokens := int64(0)
	completionTokens := int64(0)
	cacheReadTokens := int64(0)
	totalTokens := int64(0)
	spend := 0.0

	if u.PromptTokens != nil {
		promptTokens = *u.PromptTokens
	}
	if u.CompletionTokens != nil {
		completionTokens = *u.CompletionTokens
	}
	if u.CacheReadTokens != nil {
		cacheReadTokens = *u.CacheReadTokens
	}
	if u.TotalTokens != nil {
		totalTokens = *u.TotalTokens
	} else {
		totalTokens = promptTokens + completionTokens
	}
	if am.Spend != nil {
		spend = *am.Spend
	}

	model := am.Model
	if model == "" {
		model = m.activeModel
	}

	// Write asynchronously to avoid blocking the chat
	go func() {
		if err := usage.RecordUsage(time.Now(), model, "", promptTokens, completionTokens, cacheReadTokens, totalTokens, spend); err != nil {
			log.Printf("usage: record: %v", err)
		}
	}()
}

func parsePermissionRequest(content string) (agent.PermissionRequest, bool) {
	var req agent.PermissionRequest
	payload := strings.TrimPrefix(content, tool.SentinelPermissionAsk)
	if payload == content || payload == "" {
		return req, false
	}
	if err := json.Unmarshal([]byte(payload), &req); err != nil {
		return req, false
	}
	if req.ToolName == "" {
		return req, false
	}
	return req, true
}

func permissionRequestSummary(req agent.PermissionRequest) string {
	if req.Command != "" {
		return formatToolCallHint(makeToolCall(req.ToolName, string(req.Args)))
	}
	if len(req.Args) > 0 {
		return formatToolCallHint(makeToolCall(req.ToolName, string(req.Args)))
	}
	if req.ToolName != "" {
		return "🔧 " + req.ToolName
	}
	return "🔧 tool action"
}

func renderPermissionRequestBody(req agent.PermissionRequest) string {
	var lines []string
	lines = append(lines, permissionRequestSummary(req))
	if req.Scope == agent.PermissionScopeBashPrefix && req.Prefix != "" {
		lines = append(lines, fmt.Sprintf("Always-rule scope: bash prefix %q (all `%s ...` commands)", req.Prefix, req.Prefix))
	}
	if root := outOfScopePathRoot(req); root != "" {
		lines = append(lines, "Path scope: target is outside the workspace")
		lines = append(lines, fmt.Sprintf("Path root: %s", root))
		lines = append(lines, "[y] once = temporary path access for this one call")
		lines = append(lines, "[a] always this rule = also persists this path root")
		lines = append(lines, "[t] always this tool = remembers tool permission; path root is not persisted")
	}
	return strings.Join(lines, "\n")
}

func renderPermissionPrompt(req agent.PermissionRequest) string {
	var b strings.Builder
	b.WriteString("Allow this action?\n\n")
	b.WriteString(renderPermissionRequestBody(req))
	b.WriteString("\n\n[y] once  [n] deny  [a] always this rule  [t] always this tool")
	return b.String()
}

func (m *model) handlePermissionChoice(choice string) tea.Cmd {
	if m.agent == nil {
		return func() tea.Msg { return errorMsg(fmt.Errorf("no agent configured")) }
	}
	req := m.pendingPermission
	toolName := m.pendingToolName
	args := m.pendingToolArgs
	if len(args) == 0 {
		args = req.Args
	}

	// Sub-agent permission ask: the sub-agent goroutine is blocked on respCh.
	// Send the granted level back instead of re-asking the main agent. "a"/"t"
	// also persist a rule on the main agent's PermissionManager so future asks
	// (main or sub-agent) are auto-allowed.
	if m.pendingSubAgentResp != nil {
		respCh := m.pendingSubAgentResp
		m.pendingSubAgentResp = nil
		resp := agent.PermissionResponse{Level: agent.PermissionDeny}
		switch choice {
		case "y", "yes", "allow", "once":
			resp.Level = agent.PermissionAllow
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Allowed sub-agent %q once.", toolName), transient: true})
		case "a", "always", "always allow":
			resp = agent.PermissionResponse{Level: agent.PermissionAllow, PersistRule: true}
			if toolName == "webfetch" && strings.HasPrefix(req.Rule, "webfetch.domain.") {
				domain := strings.TrimPrefix(req.Rule, "webfetch.domain.")
				if m.agent.Permissions() != nil {
					m.agent.Permissions().SetWebfetchDomain(domain, agent.PermissionAllow)
				}
			} else {
				m.setPermissionRule(req, agent.PermissionAllow)
			}
			m.persistPermissions()
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Always allowing %s (sub-agent).", permissionRuleLabel(req)), transient: true})
		case "t":
			resp = agent.PermissionResponse{Level: agent.PermissionAllow, PersistTool: true}
			m.setToolPermission(toolName, agent.PermissionAllow)
			m.persistPermissions()
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Always allowing tool %q (sub-agent).", toolName), transient: true})
		case "n", "no", "deny":
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Denied sub-agent %q.", toolName), transient: true})
		default:
			m.showPermDialog = true
			m.pendingSubAgentResp = respCh
			m.updatePermButtonRegions()
			m.messages = append(m.messages, message{role: roleAssistant, text: "Invalid permission choice. Use y, n, a, or t.", transient: true})
			return nil
		}
		respCh <- resp
		// Re-arm the listener so subsequent sub-agent asks are still received.
		return listenSubAgentPerm(m.subAgentPermCh)
	}

	switch choice {
	case "y", "yes", "allow", "once":
		pathRoot := outOfScopePathRoot(req)
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Allowed %q once.", toolName), transient: true})
		return m.executeApprovedTool(toolName, args, pathRoot)
	case "a", "always", "always allow":
		m.allowOutOfScopePath(req, true)
		// Special handling for webfetch domains
		if toolName == "webfetch" && strings.HasPrefix(req.Rule, "webfetch.domain.") {
			domain := strings.TrimPrefix(req.Rule, "webfetch.domain.")
			if m.agent != nil && m.agent.Permissions() != nil {
				m.agent.Permissions().SetWebfetchDomain(domain, agent.PermissionAllow)
			}
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Always allowing webfetch for domain %q.", domain), transient: true})
		} else {
			m.setPermissionRule(req, agent.PermissionAllow)
			m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Always allowing %s.", permissionRuleLabel(req)), transient: true})
		}
		m.persistPermissions()
		return m.executeToolWithRules(toolName, args, "")
	case "t":
		pathRoot := outOfScopePathRoot(req)
		m.setToolPermission(toolName, agent.PermissionAllow)
		m.persistPermissions()
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Always allowing tool %q.", toolName), transient: true})
		return m.executeToolWithRules(toolName, args, pathRoot)
	case "n", "no", "deny":
		return m.permissionDeniedToolResult(toolName)
	default:
		m.showPermDialog = true
		m.updatePermButtonRegions()
		m.messages = append(m.messages, message{role: roleAssistant, text: "Invalid permission choice. Use y, n, a, or t.", transient: true})
		return nil
	}
}

func permissionRuleLabel(req agent.PermissionRequest) string {
	if req.Scope == agent.PermissionScopeBashPrefix && req.Prefix != "" {
		return fmt.Sprintf("bash prefix %q", req.Prefix)
	}
	return fmt.Sprintf("tool %q", req.ToolName)
}

func (m *model) setPermissionRule(req agent.PermissionRequest, level agent.PermissionLevel) {
	if req.Scope == agent.PermissionScopeBashPrefix && req.Prefix != "" {
		if m.agent != nil && m.agent.Permissions() != nil {
			m.agent.Permissions().SetBashPrefixRule(req.Prefix, level)
		}
		return
	}
	m.setToolPermission(req.ToolName, level)
}

func (m *model) setToolPermission(toolName string, level agent.PermissionLevel) {
	if m.agent != nil && m.agent.Permissions() != nil {
		m.agent.Permissions().SetRule(toolName, level)
	}
}

func (m *model) persistPermissions() {
	if m.agent == nil || m.agent.Permissions() == nil {
		return
	}
	permissions := m.agent.Permissions().ExportConfig()
	if m.config != nil {
		m.config.Ocode.Permissions = permissions
	}
	if err := config.SaveOcodePermissions(permissions); err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Failed to save permissions: %v", err)})
	}
}

func (m *model) allowOutOfScopePath(req agent.PermissionRequest, persist bool) {
	if !persist {
		return
	}
	path := outOfScopePathRoot(req)
	if path == "" {
		return
	}
	if !tool.AddExtraAllowedPath(path) {
		return
	}
	if m.config == nil {
		return
	}
	for _, existing := range m.config.Ocode.ExtraAllowedPaths {
		if existing == path {
			return
		}
	}
	m.config.Ocode.ExtraAllowedPaths = append(m.config.Ocode.ExtraAllowedPaths, path)
	if err := config.SaveOcodeConfig(&m.config.Ocode); err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Failed to save extra_allowed_paths: %v", err)})
	}
}

func outOfScopePathRoot(req agent.PermissionRequest) string {
	if !strings.HasSuffix(req.Rule, ".out_of_scope") {
		return ""
	}
	return pathRootFromPermissionArgs(req.Args)
}

func pathRootFromPermissionArgs(args json.RawMessage) string {
	var params struct {
		Path     string `json:"path"`
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return ""
	}
	target := strings.TrimSpace(params.Path)
	if target == "" {
		target = strings.TrimSpace(params.FilePath)
	}
	if target == "" || !filepath.IsAbs(target) {
		return ""
	}
	if info, err := os.Stat(target); err == nil {
		if info.IsDir() {
			return target
		}
		return filepath.Dir(target)
	}
	return filepath.Dir(target)
}

func (m model) executeApprovedTool(toolName string, args json.RawMessage, pathRoot string) tea.Cmd {
	return func() tea.Msg {
		releaseAfter := false
		if pathRoot != "" {
			releaseAfter = tool.AcquireTemporaryAllowedPath(pathRoot)
		}
		if releaseAfter {
			defer tool.ReleaseTemporaryAllowedPath(pathRoot)
		}
		result, err := m.agent.HandleApprovedToolCall(toolName, args)
		if err != nil {
			result = fmt.Sprintf("Error: %v", err)
		}
		result = agent.TruncateToolResult(m.pendingToolCallID, result)
		return []agent.Message{{Role: "tool", ToolID: m.pendingToolCallID, Content: result}}
	}
}

func (m model) executeToolWithRules(toolName string, args json.RawMessage, pathRoot string) tea.Cmd {
	return func() tea.Msg {
		releaseAfter := false
		if pathRoot != "" {
			releaseAfter = tool.AcquireTemporaryAllowedPath(pathRoot)
		}
		if releaseAfter {
			defer tool.ReleaseTemporaryAllowedPath(pathRoot)
		}
		result, err := m.agent.HandleToolCall(toolName, args)
		if err != nil {
			result = fmt.Sprintf("Error: %v", err)
		}
		result = agent.TruncateToolResult(m.pendingToolCallID, result)
		return []agent.Message{{Role: "tool", ToolID: m.pendingToolCallID, Content: result}}
	}
}

func (m model) permissionDeniedToolResult(toolName string) tea.Cmd {
	return func() tea.Msg {
		return []agent.Message{{Role: "tool", ToolID: m.pendingToolCallID, Content: fmt.Sprintf("denied: tool %q denied by user", toolName)}}
	}
}

func (m *model) lookupToolName(toolID string) string {
	if toolID == "" {
		return ""
	}
	for i := len(m.messages) - 1; i >= 0; i-- {
		raw := m.messages[i].raw
		if raw == nil {
			continue
		}
		for _, tc := range raw.ToolCalls {
			if tc.ID == toolID {
				return tc.Function.Name
			}
		}
	}
	return ""
}

func (m *model) askAgent() tea.Cmd {
	// Reset any prior cancellation so a new request isn't immediately
	// short-circuited by a stopCh that was closed by a previous Escape/Cancel.
	if m.agent != nil {
		m.agent.ResetCancellation()
	}
	// Load context once — both buildAgentMessagesSnapshot and Step call
	// BasePromptMessages, which would otherwise re-read context files twice.
	if m.agent != nil {
		m.agent.SetPreloadedContext(agent.LoadContext())
	}
	agentMsgs, uiIdx := m.buildAgentMessagesSnapshot()

	// Log agent message summary for debugging
	if m.agent != nil {
		roleCounts := map[string]int{}
		for _, m := range agentMsgs {
			roleCounts[m.Role]++
			if m.Role == "tool" {
				roleCounts["tool:"+m.ToolID]++
			}
		}
		tokens, source := agent.CurrentContextEstimate(agentMsgs)
		modelName := m.agent.GetProvider() + "/" + m.agent.Client().GetModel()
		agent.DebugAppendf("LLM", "askAgent: %d msgs → %s (est=%d tok, src=%s)", len(agentMsgs), modelName, tokens, source)
	}

	if m.skipCompactPreflight {
		m.skipCompactPreflight = false
	} else if m.agent != nil {
		if m.agent.MaybeCompactAsync(agentMsgs) {
			m.agent.SetPreloadedContext("")
			m.pendingCompactUIIdx = uiIdx
			m.pendingCompactResume = true
			m.skipCompactPreflight = true
			agent.DebugAppendf("COMPACT", "preflight compaction started, deferring LLM call")
			return nil
		}
	}

	cancel := make(chan struct{})
	ch := make(chan agent.Message, 16)
	errCh := make(chan error, 1)
	a := m.agent
	go func() {
		// Use a non-blocking send so the goroutine cannot hang forever
		// when the channel is drained by waitStreamEvent after cancel
		// closes. Without this, OnMessage would block on a full ch after
		// the TUI stops reading, leaking the goroutine and keeping the
		// activity tracker stuck in LLMRunning=true.
		a.OnMessage = func(am agent.Message) {
			select {
			case ch <- am:
			case <-cancel:
				// Stream cancelled — drop to avoid blocking.
			}
		}
		_, err := a.Step(agentMsgs)
		a.SetPreloadedContext("")
		a.OnMessage = nil
		close(ch)
		errCh <- err
	}()
	return tea.Batch(
		func() tea.Msg { return streamStartedMsg{cancel: cancel} },
		waitStreamEvent(ch, errCh, cancel),
	)
}

func (m model) retryLastLLMError() (tea.Model, tea.Cmd) {
	if m.streaming {
		return m, nil
	}
	if m.agent == nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: "No LLM configured to retry."})
		m.rerenderTranscriptAndMaybeScroll()
		return m, nil
	}
	if m.lastRetryableLLMErr == "" {
		m.messages = append(m.messages, message{role: roleAssistant, text: "No retryable LLM timeout or I/O error."})
		m.rerenderTranscriptAndMaybeScroll()
		return m, nil
	}
	if len(m.messages) > 0 {
		last := m.messages[len(m.messages)-1]
		if last.role == roleAssistant && last.text == m.lastRetryableLLMErr {
			m.messages = m.messages[:len(m.messages)-1]
		}
	}
	m.lastRetryableLLMErr = ""
	m.rerenderTranscriptAndMaybeScroll()
	return m, m.askAgent()
}

func isRetryableLLMError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "timeout") || strings.Contains(lower, "timed out") || strings.Contains(lower, "connection reset") || strings.Contains(lower, "connection refused") || strings.Contains(lower, "eof")
}

func waitStreamEvent(ch chan agent.Message, errCh chan error, cancel chan struct{}) tea.Cmd {
	return func() tea.Msg {
		select {
		case <-cancel:
			return streamDoneMsg{err: nil}
		case am, ok := <-ch:
			if !ok {
				return streamDoneMsg{err: <-errCh}
			}
			return streamMsgEvent{msg: am, ch: ch, errCh: errCh, cancel: cancel}
		}
	}
}

func (m model) reExecutePendingTool(toolName string) tea.Cmd {
	return func() tea.Msg {
		if m.agent == nil {
			return errorMsg(fmt.Errorf("no agent configured"))
		}
		var agentMsgs []agent.Message
		for _, msg := range m.messages {
			if msg.transient {
				continue
			}
			if msg.raw != nil {
				agentMsgs = append(agentMsgs, *msg.raw)
				continue
			}
			role := "user"
			if msg.role == roleAssistant {
				role = "assistant"
			}
			agentMsgs = append(agentMsgs, agent.Message{
				Role:    role,
				Content: msg.text,
			})
		}
		agentMsgs = m.prepareAgentMessages(agentMsgs)
		resp, err := m.agent.Step(agentMsgs)
		if err != nil {
			return errorMsg(err)
		}
		return resp
	}
}

func (m *model) layout() {
	if m.width <= 0 || m.height <= 0 {
		return
	}

	panelWidth := m.panelWidth()
	innerWidth := panelWidth - 7
	if innerWidth < 1 {
		innerWidth = 1
	}
	m.input.SetWidth(innerWidth)
	m.input.MaxWidth = innerWidth
	m.viewport.SetWidth(innerWidth)
	newHeight := m.height - m.bottomChromeHeight(panelWidth)
	if newHeight < 1 {
		newHeight = 1
	}
	m.viewport.SetHeight(newHeight)
	m.renderTranscript()
	if !m.detail.empty() {
		m.refreshTopDetailView()
	}
	m.layoutLogViewport()
}

func (m *model) layoutLogViewport() {
	if m.width <= 0 || m.height <= 0 {
		return
	}

	statusH := lipgloss.Height(m.renderStatus())
	logHeight := m.height - (1 + 1 + 1 + 2 + statusH)
	if logHeight < 1 {
		logHeight = 1
	}

	m.logViewport.SetWidth(max(1, m.width-5))
	m.logViewport.SetHeight(logHeight)
}

func (m model) bottomChromeHeight(panelWidth int) int {
	m.applyInputTheme()
	header := m.styles.Header.Render("◆ ocode") + hintStyle.Render("  ·  opencode clone v"+version.Version)
	var inputArea string
	if m.showPermDialog {
		inputArea = borderStyle.Width(panelWidth - 2).Render(m.renderPermissionDialog(panelWidth - 2))
	} else if m.showQuestionDialog {
		inputArea = borderStyle.Width(panelWidth - 2).Render(m.renderQuestionDialog(panelWidth - 2))
	} else {
		inputArea = borderStyle.Width(panelWidth - 2).Render(m.input.View())
	}
	status := m.renderStatus()

	height := lipgloss.Height(header)
	height += 2 // transcript border
	height += lipgloss.Height(inputArea)
	if m.showSlashPopup && !m.showPermDialog && !m.showQuestionDialog {
		height += lipgloss.Height(m.renderSlashPopup())
	}
	if row := m.renderQueueRow(); row != "" {
		height += lipgloss.Height(row)
	}
	if row := m.renderStoppedIndicator(); row != "" {
		height += lipgloss.Height(row)
	}
	if strip, _ := m.renderAgentStrip(); strip != "" {
		height += lipgloss.Height(strip)
	}
	if row := m.renderActivityRow(); row != "" {
		height += lipgloss.Height(row)
	}
	height += lipgloss.Height(status)
	return height
}

func (m *model) renderPalette() string {
	header := m.styles.Header.Render(" > ") + m.paletteInput
	commands := commandNames()
	var results []string
	for _, c := range commands {
		if strings.Contains(c, m.paletteInput) {
			results = append(results, c)
		}
	}

	body := strings.Join(results, "\n")
	return borderStyle.Width(m.width - 2).Render(header + "\n\n" + body)
}

var permBtnStyle = lipgloss.NewStyle().Bold(true).Padding(0, 1).Border(lipgloss.RoundedBorder())

type permBtnDef struct {
	label  string
	choice string
	desc   string
}

var permBtnDefs = []permBtnDef{
	{"Y", "y", "allow once"},
	{"N", "n", "deny"},
	{"A", "a", "always allow rule"},
	{"T", "t", "always allow tool"},
}

func (m *model) renderPermissionDialog(width int) string {
	req := m.pendingPermission

	contentWidth := max(0, width-2)

	body := renderPermissionRequestBody(req)

	header := m.styles.Header.Render("⚠ Permission required")

	var btnParts []string
	for _, b := range permBtnDefs {
		btnParts = append(btnParts, permBtnStyle.Render(b.label+" "+b.desc))
	}
	buttonRow := lipgloss.NewStyle().Width(contentWidth).MaxWidth(contentWidth).Render(
		lipgloss.JoinHorizontal(lipgloss.Top, btnParts...),
	)

	return lipgloss.NewStyle().Width(contentWidth).MaxWidth(contentWidth).Render(
		header + "\n\n" + body + "\n\n" + buttonRow,
	)
}

// updatePermButtonRegions computes absolute screen positions for the permission
// dialog buttons and stores them on the model. Call from Update() after layout changes.
func (m *model) updatePermButtonRegions() {
	if !m.showPermDialog {
		m.permButtonRegions = nil
		return
	}
	req := m.pendingPermission

	// Count lines above the button row inside the dialog content (excluding border).
	// header(1) + blank(1) + body + blank(1)
	linesAbove := 3 + strings.Count(renderPermissionRequestBody(req), "\n") + 1

	// +1 for the dialog box top border
	buttonTopY := m.inputAreaTopY() + 1 + linesAbove

	m.permButtonRegions = nil
	x := 1 // after left border
	for _, b := range permBtnDefs {
		rendered := permBtnStyle.Render(b.label + " " + b.desc)
		w := lipgloss.Width(rendered)
		h := lipgloss.Height(rendered)
		m.permButtonRegions = append(m.permButtonRegions, permButtonRegion{
			choice: b.choice,
			x1:     x,
			x2:     x + w - 1,
			y1:     buttonTopY,
			y2:     buttonTopY + h - 1,
		})
		x += w
	}
}

func (m model) toolOutputForClick(mouse tea.Mouse) (int, bool) {
	if len(m.toolOutputRegions) == 0 {
		return 0, false
	}
	if m.sidebarEnabled() && mouse.X >= m.panelWidth() {
		return 0, false
	}
	clickY := mouse.Y - lipgloss.Height(m.styles.Header.Render("◆ ocode")) - 1
	if clickY < 0 || clickY >= m.viewport.Height() {
		return 0, false
	}
	clickY += m.viewport.YOffset()

	for _, region := range m.toolOutputRegions {
		if clickY >= region.startLine && clickY <= region.endLine {
			return region.messageIndex, true
		}
	}
	return 0, false
}

func (m model) thinkingForClick(mouse tea.Mouse) (int, bool) {
	if m.sidebarEnabled() && mouse.X >= m.panelWidth() {
		return 0, false
	}
	clickY := mouse.Y - lipgloss.Height(m.styles.Header.Render("◆ ocode")) - 1
	if clickY < 0 || clickY >= m.viewport.Height() {
		return 0, false
	}
	clickY += m.viewport.YOffset()
	for _, region := range m.thinkingRegions {
		if clickY >= region.startLine && clickY <= region.endLine {
			return region.messageIndex, true
		}
	}
	return 0, false
}

func (m *model) shouldAutoScrollTranscript() bool {
	if m.restoredPendingScroll {
		return true
	}
	if m.viewport.TotalLineCount() == 0 {
		return true
	}
	return m.viewport.AtBottom() || m.viewport.ScrollPercent() >= 0.9
}

func (m *model) rerenderTranscriptAndMaybeScroll() {
	shouldScroll := m.shouldAutoScrollTranscript()
	m.renderTranscript()
	if shouldScroll {
		m.viewport.GotoBottom()
	}
}

func (m *model) maybeScrollTranscriptToBottom() {
	if m.shouldAutoScrollTranscript() {
		m.viewport.GotoBottom()
	}
}

func (m *model) renderTranscript() {
	if len(m.messages) == 0 {
		return
	}
	var b strings.Builder
	m.toolOutputRegions = nil
	m.thinkingRegions = nil
	if m.expandedToolOutputs == nil {
		m.expandedToolOutputs = make(map[int]bool)
	}
	if m.expandedThinking == nil {
		m.expandedThinking = make(map[int]bool)
	}

	for i, msg := range m.messages {
		if i > 0 {
			b.WriteString("\n\n")
		}
		switch msg.role {
		case roleUser:
			b.WriteString(m.renderUserText(strings.TrimRight(msg.text, "\n")))
		case roleThinking:
			text := strings.TrimSpace(msg.text)
			if text == "" {
				continue
			}
			content := renderThinkingContent(text, m.styles)
			expanded := m.expandedThinking[i]
			width := m.viewport.Width()
			wrapped := wrapView(content, width)
			lines := strings.Split(wrapped, "\n")
			totalLines := len(lines)
			collapsed := !expanded && totalLines > 8
			header := "⟁ thinking"
			if collapsed {
				header = fmt.Sprintf("⟁ thinking · %d lines [▸ click to expand]", totalLines)
			} else if totalLines > 8 {
				header = fmt.Sprintf("⟁ thinking · %d lines [▾ click to collapse]", totalLines)
			}
			startLine := lipgloss.Height(b.String())
			b.WriteString(m.styles.ThinkingHeader.Render(header))
			b.WriteString("\n")
			body := content
			if collapsed {
				body = strings.Join(lines[totalLines-8:], "\n")
			}
			b.WriteString(m.styles.Thinking.Render(body))
			endLine := lipgloss.Height(b.String()) - 1
			m.thinkingRegions = append(m.thinkingRegions, toolOutputRegion{
				messageIndex: i,
				startLine:    startLine,
				endLine:      endLine,
			})
		case roleAssistant:
			if msg.raw != nil && msg.raw.Role == "tool" && msg.raw.ToolID != "" {
				if _, ok := parseQuestionPrompt(msg.raw.Content); ok {
					b.WriteString(m.renderAssistantText(strings.TrimRight(msg.text, "\n")))
					break
				}
				if strings.HasPrefix(msg.raw.Content, tool.SentinelPermissionAsk) {
					b.WriteString(m.renderAssistantText(strings.TrimRight(msg.text, "\n")))
					break
				}
				toolName := m.lookupToolName(msg.raw.ToolID)
				if toolName == "" {
					toolName = "tool"
				}
				startLine := lipgloss.Height(b.String())
				var boxContent string
				if strings.HasPrefix(msg.raw.Content, "ORPHAN_TOOL_ERROR:") {
					boxContent = m.renderOrphanWarningBox(msg.raw.Content, m.expandedToolOutputs[i])
				} else {
					boxContent = m.renderToolOutputBox(toolName, msg.raw.Content, m.expandedToolOutputs[i])
				}
				b.WriteString(boxContent)
				endLine := lipgloss.Height(b.String()) - 1
				m.toolOutputRegions = append(m.toolOutputRegions, toolOutputRegion{
					messageIndex: i,
					startLine:    startLine,
					endLine:      endLine,
				})
			} else {
				b.WriteString(m.renderAssistantText(strings.TrimRight(msg.text, "\n")))
			}
		}
	}
	m.transcriptContent = wrapView(b.String(), m.viewport.Width())
	m.transcriptLines = strings.Split(m.transcriptContent, "\n")
	m.rawTranscriptLines = strings.Split(stripANSI(m.transcriptContent), "\n")
	m.viewport.SetContent(m.transcriptContent)
	m.sel = selectionState{}
	m.updatePermButtonRegions()
}

func (m *model) renderUserText(text string) string {
	content := renderMarkdownBold(text, m.styles.Text)
	bubbleWidth := m.viewport.Width() - 6
	if bubbleWidth < 12 {
		bubbleWidth = 12
	}
	body := m.styles.UserMessageBox.Width(bubbleWidth).Render(content)
	return body
}

func (m *model) renderToolOutputBox(toolName, content string, expanded bool) string {
	content = stripTruncationFooter(content)
	content = strings.TrimRight(content, "\n")
	lines := strings.Split(content, "\n")
	boxContent := content
	footer := m.styles.Hint.Render("  ▲ click to collapse")

	if !expanded {
		footer = ""
		if len(lines) > toolOutputPreviewLines {
			boxContent = strings.Join(lines[len(lines)-toolOutputPreviewLines:], "\n")
			footer = m.styles.Hint.Render(fmt.Sprintf("  … %d earlier lines · click to expand", len(lines)-toolOutputPreviewLines))
		}
	}

	width := m.viewport.Width() - 4
	if width < 1 {
		width = 1
	}
	box := m.styles.ToolBox.Width(width).Render(renderToolResult(toolName, boxContent, m.styles))
	header := m.styles.Hint.Render("  " + toolName + " output")
	if footer != "" {
		return header + "\n" + box + "\n" + footer
	}
	return header + "\n" + box
}

// renderOrphanWarningBox renders a warning box for tool calls that failed even
// after the recovery retry. Format: "ORPHAN_TOOL_ERROR:<name>:<err>\n<detail>"
func (m *model) renderOrphanWarningBox(content string, expanded bool) string {
	const maxLines = 10
	warnColor := lipgloss.Color("#E5A50A")
	warnStyle := lipgloss.NewStyle().Foreground(warnColor).Bold(true)
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(warnColor).
		Padding(0, 1)

	// Parse "ORPHAN_TOOL_ERROR:<name>:<err>\n<detail>"
	body := strings.TrimPrefix(content, "ORPHAN_TOOL_ERROR:")
	toolName := "unknown"
	errMsg := ""
	detail := ""
	if idx := strings.IndexByte(body, ':'); idx >= 0 {
		toolName = body[:idx]
		rest := body[idx+1:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			errMsg = rest[:nl]
			detail = strings.TrimSpace(rest[nl+1:])
		} else {
			errMsg = rest
		}
	}

	header := warnStyle.Render("⚠ tool call failed after retry: " + toolName)
	if errMsg != "" {
		header += "\n" + m.styles.Error.Render("  error: "+errMsg)
	}

	boxLines := []string{}
	if detail != "" {
		boxLines = strings.Split(detail, "\n")
	}

	footer := ""
	boxContent := strings.Join(boxLines, "\n")
	if !expanded && len(boxLines) > maxLines {
		boxContent = strings.Join(boxLines[:maxLines], "\n")
		footer = m.styles.Hint.Render(fmt.Sprintf("  … %d more lines · click to expand", len(boxLines)-maxLines))
	} else if expanded && len(boxLines) > maxLines {
		footer = m.styles.Hint.Render("  ▲ click to collapse")
	}

	width := m.viewport.Width() - 4
	if width < 1 {
		width = 1
	}

	var b strings.Builder
	b.WriteString(header)
	if boxContent != "" {
		b.WriteString("\n")
		b.WriteString(boxStyle.Width(width).Render(boxContent))
	}
	if footer != "" {
		b.WriteString("\n")
		b.WriteString(footer)
	}
	return b.String()
}

func padViewHeight(view string, height int) string {
	if height <= 0 {
		return view
	}
	for lipgloss.Height(view) < height {
		view += "\n"
	}
	return view
}

func constrainView(view string, width int, height int) string {
	if width > 0 {
		view = wrapView(view, width)
	}
	if height > 0 {
		lines := strings.Split(view, "\n")
		if len(lines) > height {
			lines = lines[:height]
		}
		view = strings.Join(lines, "\n")
	}
	return padViewHeight(view, height)
}

func constrainViewPreservingBottom(view string, width int, height int, bottomLinesCount int) string {
	if width > 0 {
		view = wrapView(view, width)
	}
	if height > 0 {
		lines := strings.Split(view, "\n")
		if len(lines) > height {
			// Preserve the last bottomLinesCount lines, truncate from the middle
			if bottomLinesCount >= len(lines) {
				// All lines are bottom lines — keep the last `height` lines
				if len(lines) > height {
					lines = lines[len(lines)-height:]
				}
			} else if bottomLinesCount > 0 {
				// Keep top part and bottom part
				keepTop := height - bottomLinesCount
				if keepTop < 0 {
					keepTop = 0
				}
				if keepTop > 0 {
					lines = append(lines[:keepTop], lines[len(lines)-bottomLinesCount:]...)
				} else {
					lines = lines[len(lines)-bottomLinesCount:]
				}
			} else {
				lines = lines[:height]
			}
		}
		view = strings.Join(lines, "\n")
	}
	return padViewHeight(view, height)
}

func wrapView(view string, width int) string {
	if width <= 0 {
		return view
	}
	lines := strings.Split(view, "\n")
	wrapped := make([]string, 0, len(lines))
	for _, line := range lines {
		wrapped = append(wrapped, strings.Split(ansi.Hardwrap(line, width, false), "\n")...)
	}
	return strings.Join(wrapped, "\n")
}

// wireCompactCallbacks attaches OnCompactStart and OnCompact to the active
// agent so async compaction results flow back through the TUI's event loop.
// Must be re-invoked whenever m.agent is replaced.
func (m *model) wireCompactCallbacks() {
	if m.agent == nil {
		return
	}
	startCh := m.compactStartCh
	doneCh := m.compactCh
	m.agent.OnCompactStart = func() {
		select {
		case startCh <- struct{}{}:
		default:
		}
	}
	m.agent.OnCompact = func(r agent.CompactResult) {
		select {
		case doneCh <- r:
		default:
		}
	}
	deltaCh := m.deltaCh
	m.agent.OnDelta = func(kind, text string) {
		if deltaCh == nil {
			return
		}
		select {
		case deltaCh <- deltaEvent{kind: kind, text: text}:
		default:
			// drop on backpressure — visual stream may skip a token but state
			// stays consistent because the full ReasoningContent arrives in the
			// final assistant Message. Counter is incremented atomically because
			// this callback fires from the LLM streaming goroutine while the TUI
			// Update loop may read deltaDrops.
			atomic.AddUint64(&m.deltaDrops, 1)
		}
	}
	usageCh := m.usageCh
	m.agent.OnUsage = func(inputTokens, outputTokens int64) {
		if usageCh == nil {
			return
		}
		select {
		case usageCh <- usageEvent{inputTokens: inputTokens, outputTokens: outputTokens}:
		default:
		}
	}
	// Sub-agent permission asks: the callback runs inside a sub-agent goroutine.
	// It hands the request to the TUI Update loop and blocks for the answer. The
	// mutex serialises concurrent asks (multiple sub-agents may ask at once) so
	// only one permission dialog is live and pendingPermission isn't stomped.
	// subAgentPermCh / subAgentPermMu are created once in newModel and must not
	// be recreated here: the listener armed in Init holds the original channel.
	if m.subAgentPermMu == nil {
		m.subAgentPermMu = &sync.Mutex{}
	}
	permCh := m.subAgentPermCh
	permMu := m.subAgentPermMu
	done := m.agent.Done()
	m.agent.SetSubAgentPermAsker(func(req agent.PermissionRequest) agent.PermissionResponse {
		permMu.Lock()
		defer permMu.Unlock()
		respCh := make(chan agent.PermissionResponse, 1)
		// Select on the agent's Done channel so a Shutdown while this sub-agent
		// is waiting unblocks cleanly (deny) instead of leaking the goroutine.
		select {
		case permCh <- subAgentPermRequest{req: req, respCh: respCh}:
		case <-done:
			return agent.PermissionResponse{Level: agent.PermissionDeny}
		}
		select {
		case resp := <-respCh:
			return resp
		case <-done:
			return agent.PermissionResponse{Level: agent.PermissionDeny}
		}
	})
}

// listenSubAgentPerm blocks on the sub-agent permission channel and re-arms the
// command after each request, so the TUI keeps receiving sub-agent asks.
func listenSubAgentPerm(ch chan subAgentPermRequest) tea.Cmd {
	return func() tea.Msg {
		return subAgentPermAskMsg(<-ch)
	}
}

type subAgentPermAskMsg subAgentPermRequest

func waitCompactEvent(startCh chan struct{}, doneCh chan agent.CompactResult) tea.Cmd {
	return func() tea.Msg {
		select {
		case <-startCh:
			return compactStartedMsg{}
		case r := <-doneCh:
			return compactFinishedMsg{result: r}
		}
	}
}

// deltaEvent carries one streamed token (kind ∈ {"reasoning","text"}) from
// the LLM HTTP goroutine to the TUI's event loop via deltaCh.
type deltaEvent struct {
	kind string
	text string
}

type deltaMsg deltaEvent

type usageEvent struct {
	inputTokens  int64
	outputTokens int64
}

type usageMsg usageEvent

// applyThinkingDelta appends a streamed reasoning token to the in-flight
// roleThinking message (creating one if none exists). Text deltas are
// ignored — the final assistant Message replaces them on arrival, and
// streaming the assistant text would duplicate it. Auto-expands the
// streaming message so users see it grow live.
func (m *model) applyThinkingDelta(kind, text string) {
	// Count all streamed characters for live token estimation.
	if text != "" {
		m.streamTokenEstimate += len(text)
		if kind == "reasoning" {
			m.streamThinkingChars += len(text)
		} else {
			m.streamOutputChars += len(text)
		}
		m.tokenBlinkUntil = time.Now().Add(2 * time.Second)
	}
	if kind != "reasoning" || text == "" || !m.showThinking {
		return
	}
	// If streamingThinkingIdx was reset (by appendAgentMessage) and we already
	// have a final assistant message, late thinking deltas are stale — drop them.
	if m.streamingThinkingIdx < 0 {
		for i := len(m.messages) - 1; i >= 0; i-- {
			if m.messages[i].role == roleAssistant {
				return
			}
		}
	}
	if m.streamingThinkingIdx < 0 || m.streamingThinkingIdx >= len(m.messages) || m.messages[m.streamingThinkingIdx].role != roleThinking {
		m.messages = append(m.messages, message{role: roleThinking, text: text})
		m.streamingThinkingIdx = len(m.messages) - 1
		if m.expandedThinking == nil {
			m.expandedThinking = make(map[int]bool)
		}
		m.rerenderTranscriptAndMaybeScroll()
		m.lastDeltaRender = time.Now()
		return
	}
	m.messages[m.streamingThinkingIdx].text += text
	// Throttle re-renders to ≥50ms during a stream — a long reasoning turn
	// can emit thousands of tokens and renderTranscript walks the full
	// message list + re-wraps. Final state always lands via appendAgentMessage.
	if time.Since(m.lastDeltaRender) < 50*time.Millisecond {
		return
	}
	m.rerenderTranscriptAndMaybeScroll()
	m.lastDeltaRender = time.Now()
}

func waitDeltaEvent(ch chan deltaEvent) tea.Cmd {
	return func() tea.Msg {
		return deltaMsg(<-ch)
	}
}

func waitTitleEvent(ch chan titleResult) tea.Cmd {
	return func() tea.Msg {
		r := <-ch
		return titleGeneratedMsg{title: r.title, gen: r.gen}
	}
}

// buildAgentMessagesSnapshot reconstructs the full agent.Message list
// equivalent to what askAgent would send. Used at compaction trigger time so
// the agent can splice against the canonical history.
func (m *model) buildAgentMessagesSnapshot() ([]agent.Message, []int) {
	var agentMsgs []agent.Message
	var uiIdx []int
	if m.agent != nil {
		base := m.agent.BasePromptMessages(m.buildSelectionContext())
		agentMsgs = append(agentMsgs, base...)
		for range base {
			uiIdx = append(uiIdx, -1) // sentinel: synthetic message, not present in m.messages
		}
	}
	for i, msg := range m.messages {
		if msg.transient || msg.role == roleThinking {
			continue
		}
		if msg.raw != nil {
			if strings.HasPrefix(msg.raw.Content, tool.SentinelPermissionAsk) {
				continue
			}
			if strings.Contains(msg.raw.Content, tool.SentinelWaitingForUser) {
				continue
			}
			agentMsgs = append(agentMsgs, *msg.raw)
			uiIdx = append(uiIdx, i)
			continue
		}
		role := "user"
		if msg.role == roleAssistant {
			role = "assistant"
		}
		agentMsgs = append(agentMsgs, agent.Message{Role: role, Content: msg.text})
		uiIdx = append(uiIdx, i)
	}

	// Strip shell-* tool_calls (from !command synthesis) that have already been
	// responded to. DeepSeek is strict: assistant messages with tool_calls must
	// be immediately followed by tool messages. Both the tool_call entries AND
	// their corresponding tool result messages are stripped so orphaned tool
	// results don't sit between remaining real tool_calls and their results.
	strippedIDs := make(map[string]bool)
	respondedToolIDs := make(map[string]bool)
	for _, msg := range agentMsgs {
		if msg.Role == "tool" && msg.ToolID != "" {
			if strings.HasPrefix(msg.ToolID, "shell-") {
				strippedIDs[msg.ToolID] = true
			}
			respondedToolIDs[msg.ToolID] = true
		}
	}
	for i := range agentMsgs {
		if agentMsgs[i].Role == "assistant" && len(agentMsgs[i].ToolCalls) > 0 {
			var filtered []agent.ToolCall
			for _, tc := range agentMsgs[i].ToolCalls {
				isShell := strings.HasPrefix(tc.ID, "shell-")
				if !isShell || !respondedToolIDs[tc.ID] {
					filtered = append(filtered, tc)
				}
			}
			agentMsgs[i].ToolCalls = filtered
		}
	}
	// Strip orphaned shell-* tool result messages whose matching tool_calls
	// were removed above.
	if len(strippedIDs) > 0 {
		filtered := agentMsgs[:0]
		uiFiltered := uiIdx[:0]
		for i, msg := range agentMsgs {
			if msg.Role == "tool" && msg.ToolID != "" && strippedIDs[msg.ToolID] {
				continue
			}
			filtered = append(filtered, msg)
			if i < len(uiIdx) {
				uiFiltered = append(uiFiltered, uiIdx[i])
			}
		}
		agentMsgs = filtered
		uiIdx = uiFiltered
	}

	// Merge consecutive same-role user messages. Session resume can produce
	// back-to-back user messages (e.g. a saved unanswered query followed by
	// the user retyping the same input), confusing the LLM into spurious
	// "done" responses. Merging prevents this without losing information.
	mergedLen := 0
	mergedUserCount := 0
	for i, msg := range agentMsgs {
		if mergedLen > 0 && agentMsgs[mergedLen-1].Role == msg.Role && msg.Role == "user" {
			sep := "\n"
			if agentMsgs[mergedLen-1].Content == "" {
				sep = ""
			}
			agentMsgs[mergedLen-1].Content += sep + msg.Content
			mergedUserCount++
			continue
		}
		agentMsgs[mergedLen] = agentMsgs[i]
		if i < len(uiIdx) {
			uiIdx[mergedLen] = uiIdx[i]
		}
		mergedLen++
	}
	agentMsgs = agentMsgs[:mergedLen]
	uiIdx = uiIdx[:mergedLen]
	if mergedUserCount > 0 {
		agent.DebugAppendf("SESSION", "merged %d consecutive user messages in buildAgentMessagesSnapshot (final agent msgs=%d)", mergedUserCount, mergedLen)
	}

	return agentMsgs, uiIdx
}

// applyCompactionResult splices m.messages by replacing the UI rows that
// correspond to the agent-message range [r.ReplaceFrom, r.ReplaceTo) with a
// single banner message wrapping r.Summary. The uiIdx slice is the mapping
// produced by buildAgentMessagesSnapshot at the time MaybeCompactAsync was
// called. We re-check the snapshot against current m.messages to guard
// against drift (messages added/removed while compaction was running).
func (m *model) applyCompactionResult(r agent.CompactResult, uiIdx []int) bool {
	if !r.OK {
		return false
	}
	if r.ReplaceFrom >= r.ReplaceTo {
		return false
	}
	if r.ReplaceTo > len(uiIdx) {
		return false
	}
	// Collect the UI indices that correspond to the agent range. -1 sentinels
	// (synthetic context system msg) are skipped — those don't live in
	// m.messages and don't need replacing.
	var realUIIndices []int
	for i := r.ReplaceFrom; i < r.ReplaceTo; i++ {
		if uiIdx[i] >= 0 {
			realUIIndices = append(realUIIndices, uiIdx[i])
		}
	}
	if len(realUIIndices) == 0 {
		return false
	}
	uiFrom := realUIIndices[0]
	uiTo := realUIIndices[len(realUIIndices)-1] + 1
	if uiFrom < 0 || uiTo > len(m.messages) || uiFrom >= uiTo {
		return false
	}
	rawCopy := r.Summary
	replacedCount := r.ReplaceTo - r.ReplaceFrom
	banner := message{
		role: roleAssistant,
		text: fmt.Sprintf("📦 Compacted %d earlier messages", replacedCount),
		raw:  &rawCopy,
	}
	newMsgs := make([]message, 0, len(m.messages)-(uiTo-uiFrom)+1)
	newMsgs = append(newMsgs, m.messages[:uiFrom]...)
	newMsgs = append(newMsgs, banner)
	newMsgs = append(newMsgs, m.messages[uiTo:]...)
	m.messages = newMsgs
	return true
}

type jobCompletedMsg struct {
	agent *agent.Agent
	ev    agent.JobEvent
}

// listenJobs blocks on the agent's job-events channel and re-arms itself.
func listenJobs(a *agent.Agent) tea.Cmd {
	return func() tea.Msg {
		ev := <-a.JobEvents()
		return jobCompletedMsg{agent: a, ev: ev}
	}
}

func listenActivity(tracker *agent.ActivityTracker) tea.Cmd {
	return func() tea.Msg {
		snap := <-tracker.Notify()
		return activityUpdateMsg{tracker: tracker, snap: snap}
	}
}

func (m model) renderActivityRow() string {
	if !m.activityRowReserved {
		return ""
	}
	snap := m.lastActivity
	if !snap.LLMRunning && len(snap.ActiveTools) == 0 && len(snap.ActiveAgents) == 0 {
		return m.styles.Status.Width(m.statusContentWidth()).Render("")
	}
	var parts []string
	if snap.LLMRunning {
		parts = append(parts, "⟳ LLM")
	}
	if len(snap.ActiveTools) > 0 {
		toolParts := make([]string, len(snap.ActiveTools))
		for i, ta := range snap.ActiveTools {
			elapsed := time.Since(ta.StartedAt).Round(time.Second)
			toolParts[i] = fmt.Sprintf("%s [%s · %s]", ta.Name, ta.StartedAt.Format("15:04:05"), formatDuration(elapsed))
		}
		parts = append(parts, "⚙ "+strings.Join(toolParts, ", "))
	}
	if len(snap.ActiveAgents) > 0 {
		parts = append(parts, "🤖 "+strings.Join(snap.ActiveAgents, ", "))
	}
	return m.styles.Status.Width(m.statusContentWidth()).Render(" " + strings.Join(parts, "  │  "))
}

// jobCounts returns the number of running background processes and agent runs.
func (m model) jobCounts() (procs, agents int) {
	if m.agent == nil {
		return 0, 0
	}
	if m.agent.Procs() != nil {
		procs = m.agent.Procs().RunningCount()
	}
	if m.agent.Runs() != nil {
		agents = m.agent.Runs().RunningCount()
	}
	return procs, agents
}

// renderJobCounts renders the "▣ N bg · M agents" segment, or "" when idle.
func (m model) renderJobCounts() string {
	procs, agents := m.jobCounts()
	if procs == 0 && agents == 0 {
		return ""
	}
	return fmt.Sprintf("▣ %d bg · %d agents", procs, agents)
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func truncateToWidth(s string, w int) string {
	if w < 1 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}

// constrainToWidth ensures every line in a multi-line string does not exceed
// the given visual width. Lines are silently truncated; no ellipsis is added.
// This is useful for constraining free-form rendered blocks that are joined
// into the panel alongside bordered elements.
func constrainToWidth(s string, w int) string {
	if w < 1 {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, w, "")
	}
	return strings.Join(lines, "\n")
}

// renderAgentStrip renders top-level agent runs as collapsed activity cards: a
// header line plus the latest meaningful transcript events. The strip is capped
// at agentStripMaxRows visible
// rows; when more runs exist than fit, the slice starting at agentStripOffset is
// shown with "↑ more"/"↓ more" indicator rows. The selected run (when the strip
// has focus) is highlighted. Returns the rendered string and the per-block row
// ranges (relative to the strip's first row, including any indicator rows).
func (m model) renderAgentStrip() (string, []agentStripBlock) {
	if m.agent == nil || m.agent.Runs() == nil {
		return "", nil
	}
	runs := m.agent.Runs().Snapshot()
	if len(runs) == 0 {
		return "", nil
	}
	width := m.panelWidth() - 2
	frame := spinnerFrames[m.dotFrame%len(spinnerFrames)]

	offset := m.agentStripOffset
	if offset < 0 {
		offset = 0
	}
	if offset > len(runs)-1 {
		offset = len(runs) - 1
	}

	var b strings.Builder
	var blocks []agentStripBlock
	row := 0

	// When focused, show a one-line hint above the strip describing the keys.
	if m.agentStripFocused {
		hint := fmt.Sprintf("  agents %d/%d · j/k: move · enter: open · esc: exit", m.agentStripSelected+1, len(runs))
		b.WriteString(hintStyle.Render(truncateToWidth(hint, width)) + "\n")
		row++
	}

	// agentStripMaxRows is the hard cap on rows occupied by run blocks plus the
	// "↑ more"/"↓ more" indicators (the focus hint above is separate chrome).
	// runRows counts only run-block rows; the budget is the cap minus the rows
	// reserved for whichever indicators are shown.
	showUp := offset > 0
	if showUp {
		b.WriteString(truncateToWidth(hintStyle.Render("  ↑ more"), width) + "\n")
		row++
	}

	runRows := 0
	rendered := 0
	for i := offset; i < len(runs); i++ {
		ri := runs[i]
		// A block is 1 header + up to agentRunPreviewLineCount event lines.
		lines := agentRunEvents(ri, agentRunPreviewLineCount)
		blockRows := 1 + len(lines)
		// Reserve indicator rows: 1 for "↑ more" if scrolled, 1 for "↓ more" if
		// more runs follow this one.
		reserve := 0
		if showUp {
			reserve++
		}
		if i < len(runs)-1 {
			reserve++
		}
		// Always render at least one block even if it exceeds the budget,
		// otherwise a tall first block could render nothing.
		if rendered > 0 && runRows+blockRows+reserve > agentStripMaxRows {
			break
		}

		start := row
		status := string(ri.Status)
		icon := statusIcon(ri.Status, frame)
		head := fmt.Sprintf("▸ %-10s %s %s · %s", ri.Name, icon, status, formatRunElapsed(ri))
		if summary := formatChildSummary(agentRunChildren(ri)); summary != "" {
			head += " · " + summary
		}
		selected := m.agentStripFocused && i == m.agentStripSelected
		if selected {
			b.WriteString(selectedStyle.Render(truncateToWidth(head, width)) + "\n")
		} else {
			b.WriteString(hintStyle.Render(truncateToWidth(head, width)) + "\n")
		}
		row++
		for _, ln := range lines {
			b.WriteString(hintStyle.Render("  │ "+truncateToWidth(stripANSI(ln), width-4)) + "\n")
			row++
		}
		blocks = append(blocks, agentStripBlock{runID: ri.ID, rowStart: start, rowEnd: row})
		runRows += blockRows
		rendered++
	}

	if offset+rendered < len(runs) {
		more := fmt.Sprintf("  ↓ more (%d)", len(runs)-offset-rendered)
		b.WriteString(truncateToWidth(hintStyle.Render(more), width) + "\n")
		row++
	}

	return strings.TrimRight(b.String(), "\n"), blocks
}

// agentStripRunCount returns the number of agent runs, used for clamping the
// strip's selection and scroll offset.
func (m model) agentStripRunCount() int {
	if m.agent == nil || m.agent.Runs() == nil {
		return 0
	}
	return len(m.agent.Runs().Snapshot())
}

// agentStripVisibleCount returns how many runs render starting at the given
// offset, given the agentStripMaxRows cap. It mirrors renderAgentStrip's row
// accounting so callers can keep the selection inside the visible window.
func (m model) agentStripVisibleCount(offset int) int {
	runs := m.agent.Runs().Snapshot()
	if offset < 0 || offset >= len(runs) {
		return 0
	}
	showUp := offset > 0
	runRows := 0
	rendered := 0
	for i := offset; i < len(runs); i++ {
		blockRows := 1 + len(agentRunEvents(runs[i], agentRunPreviewLineCount))
		reserve := 0
		if showUp {
			reserve++
		}
		if i < len(runs)-1 {
			reserve++
		}
		if rendered > 0 && runRows+blockRows+reserve > agentStripMaxRows {
			break
		}
		runRows += blockRows
		rendered++
	}
	return rendered
}

// clampAgentStrip keeps the selected index and scroll offset within bounds and
// scrolls the offset so the selected run stays visible.
func (m *model) clampAgentStrip() {
	n := m.agentStripRunCount()
	if n == 0 {
		m.agentStripSelected = 0
		m.agentStripOffset = 0
		m.agentStripFocused = false
		return
	}
	if m.agentStripSelected < 0 {
		m.agentStripSelected = 0
	}
	if m.agentStripSelected > n-1 {
		m.agentStripSelected = n - 1
	}
	if m.agentStripOffset < 0 {
		m.agentStripOffset = 0
	}
	if m.agentStripOffset > n-1 {
		m.agentStripOffset = n - 1
	}
	// Scroll up if the selection is above the window.
	if m.agentStripSelected < m.agentStripOffset {
		m.agentStripOffset = m.agentStripSelected
	}
	// Scroll down until the selection falls inside the visible window.
	for m.agentStripOffset < m.agentStripSelected {
		count := m.agentStripVisibleCount(m.agentStripOffset)
		if count == 0 || m.agentStripSelected < m.agentStripOffset+count {
			break
		}
		m.agentStripOffset++
	}
}

// openAgentDetail pushes a drill-in view for the given run id.
func (m *model) openAgentDetail(runID string) {
	if m.modalOpen() || m.agent == nil || m.agent.Runs() == nil {
		return
	}
	run, ok := m.findAgentRun(runID)
	if !ok {
		return
	}
	vp := viewport.New(viewport.WithWidth(m.detailViewportWidth()), viewport.WithHeight(m.detailViewportHeight()))
	expanded := map[string]bool{}
	content, runs, procs, regions := renderRunTranscriptDetail(run, runID, vp.Width(), expanded)
	vp.SetContent(content)
	vp.GotoBottom()
	m.detail.push(detailView{kind: detailAgentRun, runID: run.ID, runPath: runID, vp: vp, runs: runs, procs: procs, expanded: expanded, regions: regions})
}

// openProcessList pushes a process list drill-in view.
func (m *model) openProcessList() {
	m.openProcessListForRun("")
}

func (m *model) openProcessListForRun(runID string) {
	reg := m.processRegistryForRun(runID)
	if m.modalOpen() || reg == nil {
		return
	}
	vp := viewport.New(viewport.WithWidth(m.detailViewportWidth()), viewport.WithHeight(m.detailViewportHeight()))
	vp.SetContent(renderProcessList(reg))
	vp.GotoBottom()
	dv := detailView{kind: detailProcessList, runPath: runID, vp: vp}
	if run, ok := m.findAgentRun(runID); ok {
		dv.runID = run.ID
	}
	m.detail.push(dv)
}

// openProcessLog pushes a process log drill-in view.
func (m *model) openProcessLog(procID string) {
	m.openProcessLogForRun("", procID)
}

func (m *model) openProcessLogForRun(runID, procID string) {
	reg := m.processRegistryForRun(runID)
	if m.modalOpen() || reg == nil {
		return
	}
	vp := viewport.New(viewport.WithWidth(m.detailViewportWidth()), viewport.WithHeight(m.detailViewportHeight()))
	vp.SetContent(renderProcessLog(reg, procID))
	vp.GotoBottom()
	dv := detailView{kind: detailProcessLog, runPath: runID, procID: procID, vp: vp}
	if run, ok := m.findAgentRun(runID); ok {
		dv.runID = run.ID
	}
	m.detail.push(dv)
}

func (m *model) refreshTopDetailView() {
	if len(m.detail) == 0 {
		return
	}
	top := &m.detail[len(m.detail)-1]
	top.vp.SetWidth(m.detailViewportWidth())
	top.vp.SetHeight(m.detailViewportHeight())
	switch top.kind {
	case detailAgentRun:
		run, ok := m.findAgentRun(top.runPath)
		if !ok {
			return
		}
		content, runs, procs, regions := renderRunTranscriptDetail(run, top.runPath, top.vp.Width(), top.expanded)
		top.vp.SetContent(content)
		top.runID = run.ID
		top.runs = runs
		top.procs = procs
		top.regions = regions
	case detailProcessList:
		if reg := m.processRegistryForRun(top.runPath); reg != nil {
			top.vp.SetContent(renderProcessList(reg))
		}
	case detailProcessLog:
		if reg := m.processRegistryForRun(top.runPath); reg != nil {
			top.vp.SetContent(renderProcessLog(reg, top.procID))
		}
	}
}

func (m model) detailViewportWidth() int {
	return max(1, m.panelWidth()-7)
}

func (m model) detailViewportHeight() int {
	return max(1, m.height-6)
}

func (m model) processRegistryForRun(runID string) *tool.ProcessRegistry {
	if m.agent == nil {
		return nil
	}
	if runID == "" {
		return m.agent.Procs()
	}
	run, ok := m.findAgentRun(runID)
	if !ok {
		return nil
	}
	return run.Procs
}

func (m model) findAgentRun(runID string) (*agent.AgentRun, bool) {
	if m.agent == nil || m.agent.Runs() == nil || runID == "" {
		return nil, false
	}
	return findAgentRunByPath(m.agent.Runs(), runID)
}

func findAgentRunByPath(reg *agent.AgentRunRegistry, runPath string) (*agent.AgentRun, bool) {
	if reg == nil || runPath == "" {
		return nil, false
	}
	parts := strings.Split(runPath, "/")
	cur := reg
	var run *agent.AgentRun
	for _, part := range parts {
		if part == "" || cur == nil {
			return nil, false
		}
		var ok bool
		run, ok = cur.Get(part)
		if !ok {
			return nil, false
		}
		if run.Sub == nil {
			cur = nil
		} else {
			cur = run.Sub.Runs()
		}
	}
	return run, true
}

// modalOpen reports whether any modal overlay is currently shown.
func (m model) modalOpen() bool {
	return m.showPicker || m.showConnect || m.showPalette || m.showPermDialog || m.showQuestionDialog
}

// renderDetailView renders the top-of-stack detail view.
func (m model) renderDetailView(d detailView) string {
	var title string
	switch d.kind {
	case detailAgentRun:
		title = "Agent " + d.runID
	case detailProcessList:
		title = "Background processes"
	case detailProcessLog:
		title = "Process " + d.procID
	}
	hints := "esc: back · j/k: scroll · mouse: scroll"
	if d.kind == detailAgentRun {
		hints += " · click: sub-agent/process · ctrl+g: processes"
	} else if d.kind == detailProcessList {
		hints += " · click: open process"
	}
	header := wrapView(hintStyle.Render("◆ "+title)+hintStyle.Render("  "+hints), m.panelWidth())
	scrollbar := renderScrollbar(d.vp.Height(), d.vp.TotalLineCount(), d.vp.VisibleLineCount(), d.vp.YOffset())
	bodyContent := lipgloss.JoinHorizontal(lipgloss.Top,
		constrainView(d.vp.View(), d.vp.Width(), d.vp.Height()),
		scrollbar,
	)
	body := borderStyle.Width(m.panelWidth() - 2).Render(bodyContent)
	statusBar := m.renderDetailStatusBar(d)
	if statusBar == "" {
		return lipgloss.JoinVertical(lipgloss.Left, header, body)
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, body, statusBar)
}

// renderDetailStatusBar shows live status + token usage for an agent-run detail
// view. Returns "" for non-agent views.
func (m model) renderDetailStatusBar(d detailView) string {
	if d.kind != detailAgentRun {
		return ""
	}
	run, ok := m.findAgentRun(d.runPath)
	if !ok || run == nil {
		return ""
	}
	in, out := run.Usage()
	state := string(run.Status)
	icon := statusIcon(run.Status, "●")
	parts := []string{
		fmt.Sprintf("%s %s", icon, state),
		formatRunElapsed(run),
		fmt.Sprintf("in %s · out %s", formatTokenCount(in), formatTokenCount(out)),
	}
	line := strings.Join(parts, "  ·  ")
	return hintStyle.Render(line)
}

func formatTokenCount(n int64) string {
	if n <= 0 {
		return "0"
	}
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1_000_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
}

func (m model) renderAssistantText(text string) string {
	var b strings.Builder
	for {
		start, tagLen := findThinkingStart(text)
		if start < 0 {
			b.WriteString(renderMarkdownBold(text, m.styles.Text))
			break
		}
		if start > 0 {
			b.WriteString(renderMarkdownBold(text[:start], m.styles.Text))
		}
		remaining := text[start+tagLen:]
		end, endLen := findThinkingEnd(remaining)
		if end < 0 {
			if m.showThinking {
				b.WriteString(m.styles.Thinking.Render(remaining))
			}
			break
		}
		if m.showThinking {
			b.WriteString(m.styles.Thinking.Render(remaining[:end]))
		}
		text = remaining[end+endLen:]
	}
	return b.String()
}

func renderMarkdownBold(text string, normalStyle lipgloss.Style) string {
	boldStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#da702c")).Bold(true)
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#3aa99f")).Bold(true)
	var b strings.Builder
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "# ") {
			b.WriteString(titleStyle.Render(strings.TrimPrefix(line, "# ")))
		} else if strings.HasPrefix(line, "## ") {
			b.WriteString(titleStyle.Render(strings.TrimPrefix(line, "## ")))
		} else if strings.HasPrefix(line, "### ") {
			b.WriteString(titleStyle.Render(strings.TrimPrefix(line, "### ")))
		} else {
			rendered := renderBoldInLine(line, normalStyle, boldStyle)
			b.WriteString(rendered)
		}
		if i < len(lines)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func renderBoldInLine(line string, normalStyle, boldStyle lipgloss.Style) string {
	var b strings.Builder
	for {
		start := strings.Index(line, "**")
		if start < 0 {
			b.WriteString(normalStyle.Render(line))
			break
		}
		if start > 0 {
			b.WriteString(normalStyle.Render(line[:start]))
		}
		line = line[start+2:]
		end := strings.Index(line, "**")
		if end < 0 {
			b.WriteString(normalStyle.Render("**" + line))
			break
		}
		b.WriteString(boldStyle.Render(line[:end]))
		line = line[end+2:]
	}
	return b.String()
}

func findThinkingStart(text string) (int, int) {
	think := strings.Index(text, "<think>")
	thinking := strings.Index(text, "<thinking>")
	if think < 0 {
		if thinking < 0 {
			return -1, 0
		}
		return thinking, len("<thinking>")
	}
	if thinking < 0 || think < thinking {
		return think, len("<think>")
	}
	return thinking, len("<thinking>")
}

func findThinkingEnd(text string) (int, int) {
	think := strings.Index(text, "</think>")
	thinking := strings.Index(text, "</thinking>")
	if think < 0 {
		if thinking < 0 {
			return -1, 0
		}
		return thinking, len("</thinking>")
	}
	if thinking < 0 || think < thinking {
		return think, len("</think>")
	}
	return thinking, len("</thinking>")
}

func (m model) View() tea.View {
	v := tea.NewView(m.renderContent())
	v.AltScreen = true
	if m.mouseEnabled() {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

func (m model) mouseEnabled() bool {
	return m.config == nil || m.config.Ocode.TUI.Mouse == nil || *m.config.Ocode.TUI.Mouse
}

func (m model) renderContent() string {
	if !m.ready {
		return "initializing…"
	}

	if m.showPicker {
		return m.renderPicker()
	}

	if m.showConnect {
		return m.renderConnect()
	}

	if m.showPalette {
		return m.renderPalette()
	}

	// Drill-in detail view takes precedence over tab content (but not modals).
	if top, ok := m.detail.top(); ok {
		return m.renderDetailView(top)
	}

	// Route non-modal views by active tab
	switch m.activeTab {
	case tabFiles:
		return m.files.View(m.width, m.height, m.styles, m.chatUnread, m.exitPending)
	case tabGit:
		return m.git.View(m.width, m.height, m.styles, m.chatUnread, m.exitPending)
	case tabLog:
		return m.renderLogTab()
	}
	// tabChat falls through to existing rendering below

	title := m.sessionTitle
	if title == "" {
		if prompt := m.firstUserPromptText(); prompt != "" {
			title = truncateTitle(prompt, maxExplicitTitleLen)
		}
	}
	var headerLeft string
	if title != "" {
		headerLeft = m.styles.Header.Render("\u25c6 ocode "+title) + hintStyle.Render("  \u00b7  opencode clone v"+version.Version)
	} else {
		headerLeft = m.styles.Header.Render("\u25c6 ocode") + hintStyle.Render("  \u00b7  opencode clone v"+version.Version)
	}

	// Build tab bar + exit button for the header (full width, like other tabs).
	tabBar := renderTabBar(m.activeTab, m.chatUnread)
	var exitBtn string
	if m.exitPending {
		exitBtn = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1")).Padding(0, 1).Render("\u2715 exit?")
	} else {
		exitBtn = hintStyle.Padding(0, 1).Render("\u2715 exit")
	}
	headerPad := m.width - lipgloss.Width(headerLeft) - lipgloss.Width(tabBar) - lipgloss.Width(exitBtn)
	if headerPad < 0 {
		headerPad = 0
	}
	header := headerLeft + strings.Repeat(" ", headerPad) + tabBar + exitBtn

	status := m.renderStatus()
	panelWidth := m.panelWidth()

	transcriptSB := renderScrollbar(m.viewport.Height(), m.viewport.TotalLineCount(), m.viewport.VisibleLineCount(), m.viewport.YOffset())
	transcriptContent := lipgloss.JoinHorizontal(lipgloss.Top,
		constrainView(m.viewport.View(), m.viewport.Width(), m.viewport.Height()),
		transcriptSB,
	)
	transcript := borderStyle.Width(panelWidth - 2).Render(transcriptContent)
	var inputArea string
	if m.showPermDialog {
		inputArea = borderStyle.Width(panelWidth - 2).Render(m.renderPermissionDialog(panelWidth - 2))
	} else if m.showQuestionDialog {
		inputArea = borderStyle.Width(panelWidth - 2).Render(m.renderQuestionDialog(panelWidth - 2))
	} else {
		inputArea = borderStyle.Width(panelWidth - 2).Render(m.inputViewWithSelection())
	}
	leftParts := []string{transcript}
	if m.showSlashPopup && !m.showPermDialog && !m.showQuestionDialog {
		leftParts = append(leftParts, m.renderSlashPopup())
	}
	if row := m.renderQueueRow(); row != "" {
		leftParts = append(leftParts, row)
	}
	if row := m.renderStoppedIndicator(); row != "" {
		leftParts = append(leftParts, row)
	}
	if strip, _ := m.renderAgentStrip(); strip != "" {
		// Constrain the agent strip so it never pushes the sidebar.
		leftParts = append(leftParts, constrainToWidth(strip, panelWidth-2))
	}
	leftParts = append(leftParts, inputArea)
	if row := m.renderActivityRow(); row != "" {
		leftParts = append(leftParts, row)
	}
	leftParts = append(leftParts, status)
	left := lipgloss.JoinVertical(lipgloss.Left, leftParts...)

	if m.sidebarEnabled() {
		return lipgloss.JoinVertical(lipgloss.Left,
			header,
			lipgloss.JoinHorizontal(lipgloss.Top, left, m.renderSidebar()),
		)
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, left)
}

func (m *model) renderStatus() string {
	agentName := "build"
	agentColor := ""
	specs := agent.PrimaryAgentSpecs()
	if m.currentAgentIdx >= 0 && m.currentAgentIdx < len(specs) {
		agentName = specs[m.currentAgentIdx].Name
		agentColor = specs[m.currentAgentIdx].Color
	}
	displayAgentName := agentName
	if agentColor != "" {
		displayAgentName = lipgloss.NewStyle().Foreground(lipgloss.Color(agentColor)).Bold(true).Render(agentName)
	}

	var suffix string
	supportsReasoning := m.config != nil && agent.ModelSupportsThinking(m.config.Model)
	switch m.activeTab {
	case tabFiles:
		suffix = " · i: edit · ^S: save · n/N: new · r: rename · D: delete · y: copy path · R: reload · alt+[/]: tab"
	case tabGit:
		suffix = " · tab: cycle panel · s: stage · u: unstage · c: commit · alt+[/]/ctrl+shift+[/]: switch tab"
	case tabLog:
		suffix = " · j/k: scroll · c: clear · alt+[/]/ctrl+shift+[/]: switch tab"
	default:
		if supportsReasoning {
			suffix = " · tab: agent · ctrl+p: palette · ctrl+x: leader [y:copy-id] · ctrl+o: yolo · ctrl+y: retry · ctrl+t: reasoning"
		} else {
			suffix = " · tab: agent · ctrl+p: palette · ctrl+x: leader [y:copy-id] · ctrl+o: yolo · ctrl+y: retry"
		}
		if m.ctrlCPressed {
			suffix = " · ctrl+c again to quit"
		} else if m.streaming {
			suffix = " · esc: stop"
		}
	}
	llmState := "○ idle"
	if m.streaming || m.lastActivity.LLMRunning {
		dots := [4]string{"●○○", "●●○", "●●●", "○●●"}
		llmState = dots[m.dotFrame]
		if !m.streamStartedAt.IsZero() {
			elapsed := time.Since(m.streamStartedAt).Round(time.Second)
			tokStr := ""
			if m.streamTokenEstimate > 0 {
				ratio := modelCharPerToken(m.currentModelName())
				// Use exact output tokens from the API when available (more accurate
				// than the character-based heuristic). The usage event carrying these
				// arrives at different times per provider — Anthropic sends it early
				// (message_start), OpenAI at the end (final chunk).
				hasExact := m.streamFinalOutputTokens > 0
				var totalTok int
				if hasExact {
					totalTok = int(m.streamFinalOutputTokens)
				} else {
					totalTok = int(float64(m.streamTokenEstimate) / ratio)
				}
				prefix := "~"
				if hasExact {
					prefix = "" // exact count from API — no tilde
				}
				if time.Now().Before(m.tokenBlinkUntil) {
					colors := []string{"#7dcfff", "#9ece6a"}
					c := colors[m.dotFrame%len(colors)]
					tokStr = lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Bold(true).Render(fmt.Sprintf(" · %s%s tok", prefix, formatTok(totalTok)))
				} else {
					tokStr = fmt.Sprintf(" · %s%s tok", prefix, formatTok(totalTok))
				}
			}
			llmState = fmt.Sprintf("%s · %s%s", llmState, formatDuration(elapsed), tokStr)
		}
	}
	permissionMode := ""
	if m.agent != nil && m.agent.Permissions() != nil && m.agent.Permissions().Mode() == agent.PermissionModeYOLO {
		permissionMode = " | YOLO permissions"
	}
	compactState := ""
	if m.compacting {
		dots := []string{".  ", ".. ", "...", " ..", "  ."}
		compactState = fmt.Sprintf(" | 📦 compacting%s", dots[m.dotFrame%len(dots)])
	}
	jobState := ""
	if jc := m.renderJobCounts(); jc != "" {
		jobState = " | " + jc
	}
	reasoningState := ""
	if supportsReasoning {
		reasoningState = fmt.Sprintf(" | Reasoning: %s", thinkingBudgetLabels[m.thinkingLevelIdx])
	}
	width := m.statusContentWidth()

	// First line: status info on left
	leftStatus := fmt.Sprintf(" LLM: %s · Agent: %s · Model: %s%s%s%s%s", llmState, displayAgentName, m.currentModelName(), reasoningState, permissionMode, compactState, jobState)

	// Second line: session ID and hints
	rightContent := fmt.Sprintf("Session: %s%s", m.sessionID, suffix)

	line1 := m.styles.Status.Width(width).Render(ansi.Truncate(leftStatus, width, "..."))
	line2 := m.styles.Hint.Render(ansi.Truncate(rightContent, width, "..."))

	return line1 + "\n" + line2 + "\n"
}

func (m model) renderStoppedIndicator() string {
	if m.streaming || m.streamEndedAt.IsZero() || m.streamStartedAt.IsZero() {
		return ""
	}
	elapsed := m.streamEndedAt.Sub(m.streamStartedAt).Round(time.Second)
	at := m.streamEndedAt.Format("3:04:05 PM")
	var label string
	if m.streamWasInterrupted {
		label = fmt.Sprintf(" ⚡ interrupted at %s · took %s", at, elapsed)
	} else {
		label = fmt.Sprintf(" ✓ done at %s · took %s", at, elapsed)
	}
	return m.styles.Status.Width(m.statusContentWidth()).Render(label)
}

func (m model) renderQueueRow() string {
	if len(m.queuedInputs) == 0 {
		return ""
	}
	items := make([]string, 0, len(m.queuedInputs))
	for i, input := range m.queuedInputs {
		label := fmt.Sprintf("%d. %s", i+1, strings.TrimSpace(input))
		items = append(items, ansi.Truncate(label, 48, "..."))
	}
	text := fmt.Sprintf(" Queued (%d): %s", len(m.queuedInputs), strings.Join(items, " | "))
	return m.styles.Status.Width(m.statusContentWidth()).Render(text)
}

func (m model) statusContentWidth() int {
	width := m.panelWidth() - 2
	if width < 1 {
		return 1
	}
	return width
}

type sidebarTelemetry struct {
	inputTokens  int64
	outputTokens int64
	totalTokens  int64
	cachedTokens int64
	spend        *float64
}

type sidebarRenderData struct {
	topLines            []string
	scrollLines         []string
	bottomLines         []string
	fileScrollLinePaths map[int]string
}

func (t sidebarTelemetry) usedTokens() int64 {
	if t.totalTokens > 0 {
		return t.totalTokens
	}
	return t.inputTokens + t.outputTokens
}

func (t sidebarTelemetry) hasData() bool {
	return t.inputTokens > 0 || t.outputTokens > 0 || t.totalTokens > 0 || t.cachedTokens > 0 || t.spend != nil
}

func (t *sidebarTelemetry) addMessage(msg agent.Message) {
	messageTotal := int64(0)
	if msg.Usage != nil {
		if msg.Usage.PromptTokens != nil {
			t.inputTokens += *msg.Usage.PromptTokens
			messageTotal += *msg.Usage.PromptTokens
		}
		if msg.Usage.CompletionTokens != nil {
			t.outputTokens += *msg.Usage.CompletionTokens
			messageTotal += *msg.Usage.CompletionTokens
		}
		if msg.Usage.CacheReadTokens != nil {
			t.cachedTokens += *msg.Usage.CacheReadTokens
		}
		if msg.Usage.TotalTokens != nil {
			messageTotal = *msg.Usage.TotalTokens
		}
		t.totalTokens += messageTotal
	}
	if msg.Spend != nil {
		if t.spend == nil {
			t.spend = new(float64)
		}
		*t.spend += *msg.Spend
	}
}

func (t sidebarTelemetry) metadata() map[string]any {
	if !t.hasData() {
		return nil
	}
	meta := map[string]any{
		"input_tokens":  t.inputTokens,
		"output_tokens": t.outputTokens,
		"billed_tokens": t.totalTokens,
		"cached_tokens": t.cachedTokens,
	}
	if t.spend != nil {
		meta["spend"] = *t.spend
	}
	return meta
}

func (m model) sessionSidebarMetadata() map[string]any {
	meta := m.sessionTelemetry.metadata()
	todo := tool.TodoState()
	if todo != "" {
		if meta == nil {
			meta = make(map[string]any)
		}
		meta["todo_text"] = todo
	}
	return meta
}

func restoreTodoState(meta map[string]any) {
	if len(meta) == 0 {
		return
	}
	if v, ok := meta["todo_text"]; ok {
		if s, ok := v.(string); ok {
			tool.SetTodoState(s)
		}
	}
}

func telemetryFromSessionMetadata(meta map[string]any) sidebarTelemetry {
	if len(meta) == 0 {
		return sidebarTelemetry{}
	}

	var telemetry sidebarTelemetry
	// New keys
	if v, ok := meta["input_tokens"]; ok {
		telemetry.inputTokens = int64FromAny(v)
	}
	if v, ok := meta["output_tokens"]; ok {
		telemetry.outputTokens = int64FromAny(v)
	}
	if v, ok := meta["billed_tokens"]; ok {
		telemetry.totalTokens = int64FromAny(v)
	}
	if v, ok := meta["cached_tokens"]; ok {
		telemetry.cachedTokens = int64FromAny(v)
	}
	// Legacy keys for backward compatibility with older sessions
	if v, ok := meta["prompt_tokens"]; ok {
		telemetry.inputTokens = int64FromAny(v)
	}
	if v, ok := meta["completion_tokens"]; ok {
		telemetry.outputTokens = int64FromAny(v)
	}
	if v, ok := meta["total_tokens"]; ok {
		telemetry.totalTokens = int64FromAny(v)
	}
	if v, ok := meta["spend"]; ok {
		if f, ok := float64FromAny(v); ok {
			telemetry.spend = &f
		}
	}
	return telemetry
}

func int64FromAny(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int8:
		return int64(n)
	case int16:
		return int64(n)
	case int32:
		return int64(n)
	case int64:
		return n
	case float32:
		return int64(n)
	case float64:
		return int64(n)
	default:
		return 0
	}
}

func float64FromAny(v any) (float64, bool) {
	switch n := v.(type) {
	case float32:
		return float64(n), true
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

func aggregateSidebarTelemetry(messages []message) sidebarTelemetry {
	var telemetry sidebarTelemetry
	for _, msg := range messages {
		if msg.raw == nil {
			continue
		}
		telemetry.addMessage(*msg.raw)
	}
	return telemetry
}

func modelContextWindow(modelName string) (int64, bool) {
	// Check models.dev registry first
	if mw := agent.ModelWindow(modelName); mw > 0 {
		return mw, true
	}

	// Fallback to hardcoded values for common models not in registry
	switch modelName {
	case "gpt-4o", "gpt-4o-mini", "o1-preview":
		return 128000, true
	case "claude-3-5-sonnet-20241022", "claude-3-opus-20240229", "claude-3-haiku-20240307":
		return 200000, true
	case "gemini-1.5-pro":
		return 1048576, true
	case "gemini-1.5-flash":
		return 1000000, true
	default:
		return 0, false
	}
}

// modelCharPerToken returns estimated characters per token for a given model name.
// Used in the streaming status bar to give a more accurate live token estimate.
// Values are heuristics based on typical character-to-token ratios for each family.
func modelCharPerToken(modelName string) float64 {
	if strings.Contains(modelName, "deepseek") || strings.Contains(modelName, "deep-seek") {
		return 3.5
	}
	if strings.Contains(modelName, "claude") {
		return 3.8
	}
	if strings.Contains(modelName, "gemini") {
		return 3.9
	}
	if strings.Contains(modelName, "gpt-4") || strings.Contains(modelName, "gpt-3.5") {
		return 4.1
	}
	if strings.Contains(modelName, "o1") || strings.Contains(modelName, "o3") || strings.Contains(modelName, "o4") || strings.Contains(modelName, "o5") {
		return 3.6
	}
	return 4.0 // default fallback
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%ds", m, s)
}

func formatCompactInt(n int64) string {
	if n >= 1_000_000 {
		if n%1_000_000 == 0 {
			return fmt.Sprintf("%dM", n/1_000_000)
		}
		return fmt.Sprintf("%.2fM", float64(n)/1_000_000)
	}
	if n >= 1000 {
		if n%1000 == 0 {
			return fmt.Sprintf("%dk", n/1000)
		}
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return strconv.FormatInt(n, 10)
}

func formatPercent(used, total int64) string {
	if total <= 0 {
		return "0%"
	}
	percent := float64(used) / float64(total) * 100
	return fmt.Sprintf("%.1f%%", percent)
}

func sidebarUsageLines(telemetry sidebarTelemetry) []string {
	if !telemetry.hasData() {
		return []string{"n/a"}
	}
	tokenLine := fmt.Sprintf(
		"In %s  Cache %s  Out %s",
		formatCompactInt(telemetry.inputTokens),
		formatCompactInt(telemetry.cachedTokens),
		formatCompactInt(telemetry.outputTokens),
	)
	if telemetry.spend == nil {
		return []string{tokenLine}
	}
	spend := fmt.Sprintf("$%.4f", *telemetry.spend)
	return []string{tokenLine + dimStyle.Render(" · ") + sidebarAccentStyle.Render(spend)}
}

func (m model) buildSidebarRenderData() sidebarRenderData {
	data := sidebarRenderData{fileScrollLinePaths: map[int]string{}}
	// User requested no border/padding on scroll sections (2026-05-25)
	outerBodyWidth := sidebarColumnWidth - 4
	boxBodyWidth := sidebarColumnWidth - 4
	if boxBodyWidth < 8 {
		boxBodyWidth = 8
	}
	appendWrapped := func(dst *[]string, line string, width int) []int {
		start := len(*dst)
		wrapped := strings.Split(ansi.Hardwrap(line, width, false), "\n")
		*dst = append(*dst, wrapped...)
		idxs := make([]int, 0, len(wrapped))
		for i := range wrapped {
			idxs = append(idxs, start+i)
		}
		return idxs
	}
	appendScrollSection := func(title string, body []string, filePaths []string) {
		if len(data.scrollLines) > 0 {
			data.scrollLines = append(data.scrollLines, "")
		}
		data.scrollLines = append(data.scrollLines, renderSidebarSectionTitle(title))
		for i, line := range body {
			idxs := appendWrapped(&data.scrollLines, line, boxBodyWidth)
			if i < len(filePaths) {
				for _, idx := range idxs {
					data.fileScrollLinePaths[idx] = filePaths[i]
				}
			}
		}
	}

	modelName := m.currentModelName()

	// Cache the two O(messages) computations so typing in the input box doesn't
	// re-walk the entire transcript on every keystroke. Keyed on a coarse
	// fingerprint of m.messages plus the active model name (which affects token
	// counting heuristics). A second-by-second drift is acceptable here — the
	// numbers refresh as soon as a message is appended or the user stops typing
	// for one tick.
	cacheKey := sidebarCacheKey{msgCount: len(m.messages), model: modelName}
	if n := len(m.messages); n > 0 {
		cacheKey.lastLen = len(m.messages[n-1].text)
	}
	cache := m.sidebarCache
	if cache == nil {
		cache = &sidebarComputeCache{}
	}
	if cache.key != cacheKey {
		cache.key = cacheKey
		cache.ctxComputed = false
		cache.telemetryReady = false
	}

	telemetry := m.sessionTelemetry
	if !telemetry.hasData() {
		if !cache.telemetryReady {
			cache.telemetry = aggregateSidebarTelemetry(m.messages)
			cache.telemetryReady = true
		}
		telemetry = cache.telemetry
	}

	var ctxTokens int64
	if cache.ctxComputed {
		ctxTokens = cache.ctxTokens
	} else {
		tokens, source := m.currentContextEstimate()
		cache.ctxTokens = tokens
		cache.ctxSource = source
		cache.ctxComputed = true
		ctxTokens = tokens
	}
	contextLine := "n/a"
	if ctxTokens > 0 {
		if window, ok := modelContextWindow(modelName); ok {
			contextLine = fmt.Sprintf("%s / %s (%s)", formatCompactInt(ctxTokens), formatCompactInt(window), formatPercent(ctxTokens, window))
		} else {
			contextLine = fmt.Sprintf("%s tok", formatCompactInt(ctxTokens))
		}
	}

	usageLines := sidebarUsageLines(telemetry)

	// ── Line 1: mode + model name ──
	var statusParts []string
	if m.agent != nil {
		modeStr := strings.ToUpper(string(m.agent.Mode()))
		statusParts = append(statusParts, sidebarAccentStyle.Render("["+modeStr+"]"))
	}
	if modelName != "" {
		statusParts = append(statusParts, sidebarHeaderStyle.Render(modelName))
	}
	if len(statusParts) > 0 {
		statusLine := strings.Join(statusParts, "  ")
		data.topLines = append(data.topLines, statusLine)
	}
	// ── Line 2: token / context window ──
	if ctxTokens > 0 {
		data.topLines = append(data.topLines, dimStyle.Render(contextLine))
	}
	cwdLabel := dimStyle.Render("cwd: ")
	cwdMax := sidebarColumnWidth - 4 - lipgloss.Width(cwdLabel)
	data.topLines = append(data.topLines, cwdLabel+sidebarAccentStyle.Render(compactWorkingDir(m.workDir, cwdMax)))
	data.topLines = append(data.topLines, "")

	// ── Git status section (scrollable) ──
	gitBranch := m.git.currentBranch
	if gitBranch == "" {
		gitBranch = "(no git repo)"
	}
	gitBody := []string{dimStyle.Render("Branch: ") + gitBranch}
	if m.git.aheadBehind != "" {
		gitBody[0] += "  " + dimStyle.Render(m.git.aheadBehind)
	}
	stagedCount := len(m.git.stagedFiles)
	unstagedCount := len(m.git.unstagedFiles)
	untrackedCount := len(m.git.untrackedFiles)
	if stagedCount+unstagedCount+untrackedCount > 0 {
		var parts []string
		if stagedCount > 0 {
			parts = append(parts, successStyle.Render(fmt.Sprintf("+%d staged", stagedCount)))
		}
		if unstagedCount > 0 {
			parts = append(parts, errorStyle.Render(fmt.Sprintf("~%d modified", unstagedCount)))
		}
		if untrackedCount > 0 {
			parts = append(parts, dimStyle.Render(fmt.Sprintf("?%d untracked", untrackedCount)))
		}
		gitBody = append(gitBody, strings.Join(parts, "  "))
	}
	appendScrollSection("Git", gitBody, nil)

	// ── Selection section (scrollable) ──
	if selectionBody, selectionPaths := m.buildSelectionSidebarData(boxBodyWidth); len(selectionBody) > 0 {
		appendScrollSection("Selection", selectionBody, selectionPaths)
	}

	// ── Files section (scrollable) ──
	changed := snapshot.ChangedFiles()
	if len(changed) == 0 {
		appendScrollSection("Files", []string{"No files changed this session."}, nil)
	} else {
		body := make([]string, 0, len(changed))
		for _, path := range changed {
			// Look up git status for this file
			status := gitFileStatus(m.git, path)
			prefix := "- "
			if status != "" {
				prefix = status + " "
			}
			body = append(body, prefix+formatSidebarFilePath(path, m.workDir, sidebarColumnWidth-lipgloss.Width(prefix)-4))
		}
		appendScrollSection("Files", body, changed)
	}

	// ── TODO section (scrollable) ──
	todo := tool.TodoState()
	if todo == "" {
		appendScrollSection("TODO", []string{"No todo list for this session yet."}, nil)
	} else {
		appendScrollSection("TODO", renderSidebarTodo(todo, boxBodyWidth), nil)
	}

	// ── MCP + LSP on one line ──
	mcpLine := "MCP: " + m.renderMCPStatus()
	lspLine := "LSP: " + m.renderLSPStatus()
	appendScrollSection("Tools", []string{mcpLine + "  |  " + lspLine}, nil)

	// ── Bottom: usage + quick actions ──
	data.bottomLines = append(data.bottomLines, "")
	for _, usageLine := range usageLines {
		appendWrapped(&data.bottomLines, usageLine, outerBodyWidth)
	}
	appendWrapped(&data.bottomLines, dimStyle.Render("Ctrl+B bg bash  r run  l lint  b build"), outerBodyWidth)
	return data
}

// gitFileStatus returns a short git porcelain status string for path.
func gitFileStatus(g gitModel, path string) string {
	for _, f := range g.stagedFiles {
		if f.path == path || strings.HasSuffix(path, "/"+f.path) {
			return successStyle.Render("+" + f.status)
		}
	}
	for _, f := range g.unstagedFiles {
		if f.path == path || strings.HasSuffix(path, "/"+f.path) {
			return errorStyle.Render("~" + f.status)
		}
	}
	for _, f := range g.untrackedFiles {
		if f.path == path || strings.HasSuffix(path, "/"+f.path) {
			return dimStyle.Render("?" + f.status)
		}
	}
	return ""
}

func (m model) renderSidebar() string {
	data := m.buildSidebarRenderData()

	title := m.sessionTitle
	if title == "" {
		if prompt := m.firstUserPromptText(); prompt != "" {
			title = truncateTitle(prompt, maxExplicitTitleLen)
		}
	}
	var header string
	if title != "" {
		header = sidebarHeaderStyle.Render("◆ ") + m.styles.Header.Render(title)
	}

	headerHeight := lipgloss.Height(header)
	effectiveHeaderHeight := maxInt(1, headerHeight)
	contentHeight := m.height - 2 - effectiveHeaderHeight
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Reserve space for topLines and bottomLines, rest goes to scrollBox
	minScrollHeight := 3
	spaceForScroll := maxInt(minScrollHeight, contentHeight-len(data.topLines)-len(data.bottomLines))

	scrollBoxHeight := m.sidebarScrollBoxHeight(data, headerHeight)
	// User requested: no border — scrollBoxHeight IS the visible height
	visibleScrollLines := minInt(scrollBoxHeight, spaceForScroll)
	if visibleScrollLines < 1 {
		visibleScrollLines = 1
	}
	scrollOffset := clampInt(m.sidebarScroll, 0, maxInt(0, len(data.scrollLines)-visibleScrollLines))
	visible := sliceLines(data.scrollLines, scrollOffset, visibleScrollLines)

	// Store raw (ANSI-stripped) scroll lines for text selection
	m.rawSidebarLines = make([]string, len(data.scrollLines))
	for i, line := range data.scrollLines {
		m.rawSidebarLines[i] = stripANSI(line)
	}

	// Apply selection highlight if selection is active and overlaps visible area
	if m.sidebarSel.active && len(visible) > 0 {
		sl, sc, el, ec := normaliseSelection(m.sidebarSel.startLine, m.sidebarSel.startCol, m.sidebarSel.endLine, m.sidebarSel.endCol)
		visibleStart := scrollOffset
		visibleEnd := scrollOffset + visibleScrollLines - 1
		if sl <= visibleEnd && el >= visibleStart {
			adjSl := sl - visibleStart
			adjEl := el - visibleStart
			if adjSl < 0 {
				adjSl = 0
				sc = 0
			}
			if adjEl >= len(visible) {
				adjEl = len(visible) - 1
				if el < len(m.rawSidebarLines) {
					ec = len(m.rawSidebarLines[el])
				}
			}
			rawVisible := m.rawSidebarLines[visibleStart : visibleStart+len(visible)]
			visible = applySelectionHighlight(visible, rawVisible, adjSl, sc, adjEl, ec)
		}
	}

	// Apply hover underline for clickable file paths
	if m.hoverSidebarFile != "" && len(visible) > 0 {
		hoverStyle := lipgloss.NewStyle().Underline(true)
		for i, line := range visible {
			actualLineIdx := scrollOffset + i
			if path, ok := data.fileScrollLinePaths[actualLineIdx]; ok && path == m.hoverSidebarFile {
				visible[i] = hoverStyle.Render(stripANSI(line))
			}
		}
	}

	if len(data.scrollLines) > visibleScrollLines {
		marker := fmt.Sprintf(" %d/%d", scrollOffset+1, len(data.scrollLines))
		if len(visible) > 0 {
			visible[0] = ansi.Truncate(visible[0], maxInt(1, sidebarColumnWidth-4-lipgloss.Width(marker)), "") + hintStyle.Render(marker)
		}
	}
	// User requested no border/padding on Git/Files/TODO/Tools (2026-05-25)
	scrollContent := strings.Join(visible, "\n")
	scrollBox := lipgloss.NewStyle().
		Width(sidebarColumnWidth - 4).
		Render(constrainView(scrollContent, sidebarColumnWidth-4, visibleScrollLines))

	// Build sections preserving top and bottom lines
	allLines := append([]string{}, data.topLines...)
	allLines = append(allLines, strings.Split(scrollBox, "\n")...)
	allLines = append(allLines, data.bottomLines...)
	sections := strings.Join(allLines, "\n")

	// Only constrain if needed and prefer to keep bottom lines
	if len(allLines) > contentHeight {
		sections = constrainViewPreservingBottom(sections, sidebarColumnWidth-4, contentHeight, len(data.bottomLines))
	}
	return borderStyle.
		BorderForeground(lipgloss.Color("#7AA2F7")).
		Width(sidebarColumnWidth).
		Render(header + "\n" + sections)
}

func (m model) sidebarScrollBoxHeight(data sidebarRenderData, headerHeight int) int {
	effectiveHeaderHeight := maxInt(1, headerHeight)
	available := m.height - 2 - effectiveHeaderHeight - len(data.topLines) - len(data.bottomLines)
	if available < 3 {
		return 3
	}

	contentHeight := m.height - 2 - effectiveHeaderHeight
	maxScrollBoxHeight := contentHeight * 40 / 100
	if maxScrollBoxHeight < 3 {
		maxScrollBoxHeight = 3
	}
	if available > maxScrollBoxHeight {
		return maxScrollBoxHeight
	}
	return available
}

// sidebarVisibleScrollLines returns the number of scroll lines actually rendered
// in the sidebar. This matches the logic in renderSidebar for consistent hit-testing.
func (m model) sidebarVisibleScrollLines(data sidebarRenderData, headerHeight int) int {
	effectiveHeaderHeight := maxInt(1, headerHeight)
	contentHeight := m.height - 2 - effectiveHeaderHeight
	spaceForScroll := maxInt(3, contentHeight-len(data.topLines)-len(data.bottomLines)-2)
	scrollBoxHeight := m.sidebarScrollBoxHeight(data, headerHeight)
	return minInt(scrollBoxHeight, spaceForScroll)
}

func (m model) renderSidebarWithTabBar() string {
	tabBar := renderTabBar(m.activeTab, m.chatUnread)
	var exitBtn string
	if m.exitPending {
		exitBtn = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1")).Padding(0, 1).Render("✕ exit?")
	} else {
		exitBtn = hintStyle.Padding(0, 1).Render("✕ exit")
	}
	tabBar = sidebarAccentStyle.Render("▌ ") + tabBar
	// Clamp tab bar to ensure it fits within sidebar column width.
	maxTabBar := sidebarColumnWidth - lipgloss.Width(exitBtn)
	if maxTabBar < 10 {
		maxTabBar = 10
	}
	if lipgloss.Width(tabBar) > maxTabBar {
		tabBar = ansi.Truncate(tabBar, maxTabBar, "…")
	}
	pad := sidebarColumnWidth - lipgloss.Width(tabBar) - lipgloss.Width(exitBtn)
	if pad < 0 {
		pad = 0
	}
	topRow := tabBar + strings.Repeat(" ", pad) + exitBtn
	return lipgloss.JoinVertical(lipgloss.Left, topRow, m.renderSidebar())
}

func (m *model) clampSidebarScroll() {
	data := m.buildSidebarRenderData()
	headerHeight := lipgloss.Height(m.styles.Header.Render("◆ "))
	visible := m.sidebarScrollBoxHeight(data, headerHeight)
	if visible < 1 {
		visible = 1
	}
	m.sidebarScroll = clampInt(m.sidebarScroll, 0, maxInt(0, len(data.scrollLines)-visible))
}

func (m model) mouseOverSidebar(mouse tea.Mouse) bool {
	return m.sidebarEnabled() && mouse.X >= m.panelWidth()
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func sliceLines(lines []string, start, count int) []string {
	if count <= 0 || start >= len(lines) {
		return []string{}
	}
	if start < 0 {
		start = 0
	}
	end := start + count
	if end > len(lines) {
		end = len(lines)
	}
	return append([]string{}, lines[start:end]...)
}

func (m model) sidebarFileForClick(mouse tea.Mouse) (string, bool) {
	if !m.mouseOverSidebar(mouse) {
		return "", false
	}

	data := m.buildSidebarRenderData()
	headerHeight := 0
	title := m.sessionTitle
	if title == "" {
		if prompt := m.firstUserPromptText(); prompt != "" {
			title = truncateTitle(prompt, maxExplicitTitleLen)
		}
	}
	if title != "" {
		headerHeight = 1
	}
	// User requested: no border/padding on scroll sections (2026-05-25)
	boxTop := 1 + headerHeight + len(data.topLines)
	// Account for leading empty line when header is empty
	if headerHeight == 0 {
		boxTop++
	}
	contentTop := boxTop
	visible := m.sidebarVisibleScrollLines(data, headerHeight)
	if mouse.Y < contentTop || mouse.Y >= contentTop+visible {
		return "", false
	}
	scrollLine := m.sidebarScroll + (mouse.Y - contentTop)
	if path, ok := data.fileScrollLinePaths[scrollLine]; ok {
		return path, true
	}
	return "", false
}

func (m model) tabForClick(mouse tea.Mouse) (int, bool) {
	if mouse.Y != 0 {
		return 0, false
	}
	startX := m.tabBarStartX()
	if mouse.X < startX {
		return 0, false
	}
	if tab, ok := tabAtX(mouse.X, startX, m.activeTab, m.chatUnread); ok {
		return tab, true
	}
	return 0, false
}

func (m model) tabBarStartX() int {
	starts := m.tabBarStartXs(lipgloss.Width(renderTabBar(m.activeTab, m.chatUnread)))
	return starts[0]
}

func (m model) tabBarStartXs(tabBarWidth int) []int {
	var exitBtnWidth int
	if m.exitPending {
		exitBtnWidth = lipgloss.Width(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1")).Padding(0, 1).Render("\u2715 exit?"))
	} else {
		exitBtnWidth = lipgloss.Width(hintStyle.Padding(0, 1).Render("\u2715 exit"))
	}
	startX := m.width - tabBarWidth - exitBtnWidth
	if startX < 0 {
		startX = 0
	}
	return []int{startX}
}

func (m model) exitButtonForClick(mouse tea.Mouse) bool {
	if mouse.Y != 0 {
		return false
	}
	var exitBtn string
	if m.exitPending {
		exitBtn = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1")).Padding(0, 1).Render("\u2715 exit?")
	} else {
		exitBtn = hintStyle.Padding(0, 1).Render("\u2715 exit")
	}
	exitStartX := m.width - lipgloss.Width(exitBtn)
	return mouse.X >= exitStartX
}

func tabAtX(mouseX int, barStartX int, activeTab int, unread bool) (int, bool) {
	labels := []string{"1:chat", "2:files", "3:git", "4:log"}
	if unread && activeTab != 0 {
		labels[0] = "1:chat●"
	}
	x := barStartX
	for i, label := range labels {
		var w int
		if i == activeTab {
			w = lipgloss.Width(lipgloss.NewStyle().Bold(true).Reverse(true).Padding(0, 1).Render(label))
		} else {
			w = lipgloss.Width(hintStyle.Padding(0, 1).Render(label))
		}
		if mouseX < x+w {
			return i, true
		}
		x += w
	}
	return 0, false
}

func (m *model) refreshLogViewport() {
	kindColor := map[DebugEntryKind]lipgloss.Style{
		DebugKindLLM:   userStyle,
		DebugKindTool:  headerStyle,
		DebugKindAgent: successStyle,
		DebugKindError: errorStyle,
	}
	var lines []string
	for _, e := range m.logEntries {
		if m.logKindFilter != nil {
			if enabled, ok := m.logKindFilter[e.Kind]; ok && !enabled {
				continue
			}
		}
		if m.logSearch != "" && !logFuzzyMatch(m.logSearch, string(e.Kind)+" "+e.Message) {
			continue
		}
		style, ok := kindColor[e.Kind]
		if !ok {
			style = hintStyle
		}
		tag := style.Bold(true).Render(fmt.Sprintf("%-5s", string(e.Kind)))
		lines = append(lines, tag+" "+e.Message)
	}
	if len(lines) == 0 {
		m.logViewport.SetContent(hintStyle.Render("  no entries match"))
	} else {
		m.logViewport.SetContent(strings.Join(lines, "\n"))
	}
}

// logFuzzyMatch returns true if all runes in query appear in target in order (case-insensitive).
func logFuzzyMatch(query, target string) bool {
	query = strings.ToLower(query)
	target = strings.ToLower(target)
	qi := 0
	qr := []rune(query)
	for _, c := range target {
		if qi < len(qr) && c == qr[qi] {
			qi++
		}
	}
	return qi == len(qr)
}

func (m model) renderLogTab() string {
	tabBar := renderTabBar(tabLog, m.chatUnread)
	var exitBtn string
	if m.exitPending {
		exitBtn = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1")).Padding(0, 1).Render("✕ exit?")
	} else {
		exitBtn = m.styles.Hint.Padding(0, 1).Render("✕ exit")
	}
	headerLeft := m.styles.Header.Render("◆ ocode") + m.styles.Hint.Render("  ·  debug log")
	headerPad := m.width - lipgloss.Width(headerLeft) - lipgloss.Width(tabBar) - lipgloss.Width(exitBtn)
	if headerPad < 0 {
		headerPad = 0
	}
	header := headerLeft + strings.Repeat(" ", headerPad) + tabBar + exitBtn

	// search bar
	searchPrefix := hintStyle.Render("/ ")
	searchText := m.logSearch
	if searchText == "" {
		searchText = hintStyle.Render("search…")
	}
	searchBar := searchPrefix + searchText

	// kind filter toggles
	kinds := []struct {
		kind  DebugEntryKind
		label string
		key   string
	}{
		{DebugKindLLM, "LLM", "1"},
		{DebugKindTool, "TOOL", "2"},
		{DebugKindAgent, "AGENT", "3"},
		{DebugKindError, "ERROR", "4"},
	}
	kindBar := hintStyle.Render("filter: ")
	for _, k := range kinds {
		enabled := m.logKindFilter == nil || m.logKindFilter[k.kind]
		label := fmt.Sprintf("[%s]%s ", k.key, k.label)
		if enabled {
			kindBar += selectedStyle.Render(label)
		} else {
			kindBar += hintStyle.Render(label)
		}
	}

	// scrollbar + viewport
	sb := renderScrollbar(m.logViewport.Height(), m.logViewport.TotalLineCount(), m.logViewport.VisibleLineCount(), m.logViewport.YOffset())
	viewportContent := lipgloss.JoinHorizontal(lipgloss.Top,
		constrainView(m.logViewport.View(), m.logViewport.Width(), m.logViewport.Height()),
		sb,
	)
	contentWidth := max(1, m.width-4)
	content := m.styles.Border.Width(contentWidth).Height(m.logViewport.Height() + 2).Render(viewportContent)
	status := m.renderStatus()

	return lipgloss.JoinVertical(lipgloss.Left, header, searchBar, kindBar, content, status)
}

func shortenWorkingDir(dir string) string {
	if dir == "" {
		return "(no project)"
	}
	home, err := os.UserHomeDir()
	if err == nil && strings.HasPrefix(dir, home) {
		rel := "~" + dir[len(home):]
		if rel == "~" {
			return "~/"
		}
		return rel
	}
	return dir
}

// compactWorkingDir produces a sidebar-friendly form: replaces $HOME with ~,
// then truncates with a middle ellipsis (e.g. "/a/b/.../xyz") when the result
// exceeds max characters. The base directory name is always preserved.
func compactWorkingDir(dir string, max int) string {
	short := shortenWorkingDir(dir)
	if max <= 0 || len(short) <= max {
		return short
	}
	if max <= 3 {
		return short[:max]
	}
	base := filepath.Base(short)
	// Pick a leading anchor: "~" if home-relative, otherwise the root "/".
	var head string
	switch {
	case strings.HasPrefix(short, "~"):
		head = "~"
	case strings.HasPrefix(short, "/"):
		head = ""
	default:
		head = ""
	}
	ellipsis := "/.../"
	// Reserve room for head + ellipsis + base; if base alone is too long, tail-truncate it.
	if len(head)+len(ellipsis)+len(base) > max {
		keep := max - len(head) - len(ellipsis)
		if keep < 1 {
			return short[:max]
		}
		return head + ellipsis + base[len(base)-keep:]
	}
	return head + ellipsis + base
}

func shortenSidebarPath(path string, max int) string {
	if len(path) <= max {
		return path
	}
	if max <= 3 {
		return path[:max]
	}
	return path[:max-3] + "..."
}

func formatSidebarFilePath(path string, workDir string, max int) string {
	path = filepath.Clean(path)
	if rel, err := filepath.Rel(workDir, path); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		path = rel
	}
	path = strings.TrimPrefix(filepath.ToSlash(path), "./")
	if len(path) <= max {
		return path
	}
	if max <= 3 {
		return path[:max]
	}

	file := filepath.ToSlash(filepath.Base(path))
	if len(file) >= max-3 {
		return "..." + file[len(file)-(max-3):]
	}

	prefixMax := max - len(file) - 4
	if prefixMax < 0 {
		prefixMax = 0
	}
	prefix := path
	if len(prefix) > prefixMax {
		prefix = prefix[:prefixMax]
	}
	return prefix + ".../" + file
}

func (m model) renderMCPStatus() string {
	enabled := 0
	if m.config != nil {
		for _, cfg := range m.config.MCP {
			if cfg.Enabled {
				enabled++
			}
		}
	}

	if enabled == 0 {
		return "disabled"
	}

	loaded := 0
	if m.agent != nil {
		loaded = m.agent.MCPToolCount()
	}

	if m.agent != nil {
		errs := m.agent.MCPErrors()
		if len(errs) > 0 {
			return fmt.Sprintf("%d configured, %d loaded, %d errors", enabled, loaded, len(errs))
		}
	}

	if loaded > 0 {
		return fmt.Sprintf("%d configured, %d loaded", enabled, loaded)
	}
	return fmt.Sprintf("%d configured", enabled)
}

func (m model) mainScrollbarX() int {
	return m.panelWidth() - 5
}

func (m model) viewportContentTopY() int {
	return lipgloss.Height(m.styles.Header.Render("◆ ocode")) + 1
}

// agentStripTopY returns the first row of the agent strip in screen coordinates.
func (m model) agentStripTopY() int {
	headerH := lipgloss.Height(m.styles.Header.Render("◆ ocode"))
	y := headerH + m.viewport.Height() + 2 // +2 for transcript border
	if m.showSlashPopup && !m.showPermDialog && !m.showQuestionDialog {
		y += lipgloss.Height(m.renderSlashPopup())
	}
	if row := m.renderQueueRow(); row != "" {
		y += lipgloss.Height(row)
	}
	if row := m.renderStoppedIndicator(); row != "" {
		y += lipgloss.Height(row)
	}
	return y
}

func (m *model) applyOrClearSelectionHighlight() {
	if !m.sel.active {
		m.viewport.SetContent(m.transcriptContent)
		return
	}
	sl, sc, el, ec := normaliseSelection(m.sel.startLine, m.sel.startCol, m.sel.endLine, m.sel.endCol)
	highlighted := applySelectionHighlight(m.transcriptLines, m.rawTranscriptLines, sl, sc, el, ec)
	m.viewport.SetContent(strings.Join(highlighted, "\n"))
}

func (m *model) syncRawInputLines() {
	// Guard against an uninitialised textarea (tests construct zero models
	// and dispatch key events before Init); calling View on zero state
	// panics inside the bubbles textarea memoizer.
	if m.input.Width() <= 0 {
		return
	}
	rendered := stripANSI(m.input.View())
	m.rawInputLines = strings.Split(rendered, "\n")
	m.rawInputLinesDirty = false
}

// ensureRawInputLines synchronises rawInputLines lazily. Callers that need an
// up-to-date copy (mouse handlers, selection highlight) call this; the typing
// hot path only sets the dirty bit to avoid an extra textarea View() per key.
func (m *model) ensureRawInputLines() {
	if !m.rawInputLinesDirty {
		return
	}
	m.syncRawInputLines()
}

func (m model) inputViewWithSelection() string {
	m.applyInputTheme()
	view := m.input.View()
	if !m.inputSel.active {
		return view
	}
	m.ensureRawInputLines()
	renderedLines := strings.Split(view, "\n")
	sl, sc, el, ec := normaliseSelection(m.inputSel.startLine, m.inputSel.startCol, m.inputSel.endLine, m.inputSel.endCol)
	highlighted := applySelectionHighlight(renderedLines, m.rawInputLines, sl, sc, el, ec)
	return strings.Join(highlighted, "\n")
}

func (m model) inputAreaHeight() int {
	panelWidth := m.panelWidth()
	m.applyInputTheme()
	var rendered string
	if m.showPermDialog {
		rendered = borderStyle.Width(panelWidth - 2).Render(m.renderPermissionDialog(panelWidth - 2))
	} else if m.showQuestionDialog {
		rendered = borderStyle.Width(panelWidth - 2).Render(m.renderQuestionDialog(panelWidth - 2))
	} else {
		rendered = borderStyle.Width(panelWidth - 2).Render(m.input.View())
	}
	return lipgloss.Height(rendered)
}

func (m model) inputIsShellMode() bool {
	value := m.input.Value()
	trimmedPrefix := len(value) - len(strings.TrimLeft(value, " \t"))
	return strings.HasPrefix(value[trimmedPrefix:], "!")
}

func (m *model) disableShellMode() {
	value := m.input.Value()
	trimmedPrefix := len(value) - len(strings.TrimLeft(value, " \t"))
	if !strings.HasPrefix(value[trimmedPrefix:], "!") {
		return
	}
	m.input.SetValue(value[:trimmedPrefix] + value[trimmedPrefix+1:])
	m.rawInputLinesDirty = true
	m.applyInputTheme()
}

func (m model) inputAreaTopY() int {
	statusH := lipgloss.Height(m.renderStatus())
	activityH := 0
	if row := m.renderActivityRow(); row != "" {
		activityH = lipgloss.Height(row)
	}
	return m.height - statusH - activityH - m.inputAreaHeight()
}

func (m model) isClickInInputArea(mouse tea.Mouse) bool {
	if m.activeTab != tabChat || m.showPermDialog || m.showQuestionDialog {
		return false
	}
	if mouse.X >= m.panelWidth() {
		return false
	}
	topY := m.inputAreaTopY()
	h := m.inputAreaHeight()
	return mouse.Y >= topY && mouse.Y < topY+h
}

func (m model) detailHeaderHeight() int {
	if len(m.detail) == 0 {
		return 0
	}
	top := m.detail[len(m.detail)-1]
	title := ""
	switch top.kind {
	case detailAgentRun:
		title = "Agent " + top.runID
	case detailProcessList:
		title = "Background processes"
	case detailProcessLog:
		title = "Process " + top.procID
	}
	header := hintStyle.Render("◆ " + title)
	return lipgloss.Height(header)
}

func (m model) detailViewportContentTopY() int {
	return m.detailHeaderHeight() + 1
}

func (m model) detailScrollbarMetrics() (top, height int) {
	if len(m.detail) == 0 {
		return 0, 0
	}
	top = m.detailViewportContentTopY()
	height = m.detail[len(m.detail)-1].vp.Height()
	if height < 1 {
		height = 1
	}
	return top, height
}

func (m model) detailScrollbarX() int {
	return m.panelWidth() - 3
}

func (m model) mouseOverDetailViewport(mouse tea.Mouse) bool {
	if len(m.detail) == 0 {
		return false
	}
	topY, height := m.detailScrollbarMetrics()
	return mouse.X >= 0 && mouse.X <= m.detailScrollbarX() && mouse.Y >= topY && mouse.Y < topY+height
}

func (m model) detailScrollbarHit(mouse tea.Mouse) bool {
	if len(m.detail) == 0 || mouse.X != m.detailScrollbarX() {
		return false
	}
	topY, height := m.detailScrollbarMetrics()
	return mouse.Y >= topY && mouse.Y < topY+height
}

func (m model) detailScrollbarThumbOffset(mouse tea.Mouse) (int, bool) {
	if !m.detailScrollbarHit(mouse) {
		return 0, false
	}
	topY, height := m.detailScrollbarMetrics()
	vp := m.detail[len(m.detail)-1].vp
	return scrollbarThumbOffset(mouse.Y, topY, height, vp.TotalLineCount(), vp.VisibleLineCount(), vp.YOffset())
}

func (m model) transcriptScrollbarHit(mouse tea.Mouse) bool {
	if m.activeTab != tabChat {
		return false
	}
	if mouse.X != m.mainScrollbarX() {
		return false
	}
	top := m.viewportContentTopY()
	return mouse.Y >= top && mouse.Y < top+m.viewport.Height()
}

func (m model) transcriptScrollbarThumbOffset(mouse tea.Mouse) (int, bool) {
	if !m.transcriptScrollbarHit(mouse) {
		return 0, false
	}
	return scrollbarThumbOffset(mouse.Y, m.viewportContentTopY(), m.viewport.Height(), m.viewport.TotalLineCount(), m.viewport.VisibleLineCount(), m.viewport.YOffset())
}

func (m model) logScrollbarMetrics() (top, height int) {
	top = 3
	height = m.logViewport.Height()
	if height < 1 {
		height = 1
	}
	return top, height
}

func (m model) logScrollbarHit(mouse tea.Mouse) bool {
	if m.activeTab != tabLog {
		return false
	}
	trackTop, trackHeight := m.logScrollbarMetrics()
	trackX := m.width - 3
	return mouse.X == trackX && mouse.Y >= trackTop && mouse.Y < trackTop+trackHeight
}

func (m model) logScrollbarThumbOffset(mouse tea.Mouse) (int, bool) {
	if !m.logScrollbarHit(mouse) {
		return 0, false
	}
	trackTop, trackHeight := m.logScrollbarMetrics()
	return scrollbarThumbOffset(mouse.Y, trackTop, trackHeight, m.logViewport.TotalLineCount(), m.logViewport.VisibleLineCount(), m.logViewport.YOffset())
}

func scrollbarSetOffset(vp *viewport.Model, mouseY, trackTop, trackHeight int) {
	clickRow := mouseY - trackTop
	if clickRow < 0 {
		clickRow = 0
	}
	if clickRow >= trackHeight {
		clickRow = trackHeight - 1
	}
	total := vp.TotalLineCount()
	visible := vp.VisibleLineCount()
	maxOffset := total - visible
	if maxOffset <= 0 {
		return
	}
	_, thumbSize, ok := scrollbarThumbMetrics(trackHeight, total, visible, vp.YOffset())
	if !ok {
		return
	}
	maxThumbTop := trackHeight - thumbSize
	if maxThumbTop <= 0 {
		vp.SetYOffset(0)
		return
	}
	if clickRow > maxThumbTop {
		clickRow = maxThumbTop
	}
	offset := int(float64(clickRow) / float64(maxThumbTop) * float64(maxOffset))
	vp.SetYOffset(offset)
}

func (m model) renderLSPStatus() string {
	if m.agent == nil {
		return "unavailable"
	}

	for _, tool := range m.agent.GetTools() {
		if tool.Name() == "lsp" {
			return "available"
		}
	}

	return "unavailable"
}

func (m *model) filesAddToContext() tea.Cmd {
	return func() tea.Msg {
		rel, err := filepath.Rel(m.workDir, m.files.previewPath)
		if err != nil {
			rel = m.files.previewPath
		}
		var content string
		var startLine, endLine int
		if m.filesSel.active {
			sl, sc, el, ec := normaliseSelection(m.filesSel.startLine, m.filesSel.startCol, m.filesSel.endLine, m.filesSel.endCol)
			content = m.files.extractSelectionText(sl, sc, el, ec)
			startLine = sl + 1
			endLine = el + 1
			m.filesSel = selectionState{}
			m.files.clearSelectionHighlight()
		} else {
			content = m.files.previewRaw
			startLine = 0
			endLine = len(m.files.previewRawLines)
		}
		label := ""
		if startLine > 0 && endLine > startLine {
			label = fmt.Sprintf(" (lines %d-%d)", startLine, endLine)
		}
		fileCtx := fmt.Sprintf("\n--- File: %s%s ---\n%s\n", rel, label, content)
		return filesAddToContextMsg{
			path:      rel,
			content:   fileCtx,
			startLine: startLine - 1,
			endLine:   endLine,
		}
	}
}

func (m *model) makeCommitMsgGenerator(cfg *config.Config) func(diff string) tea.Cmd {
	return func(diff string) tea.Cmd {
		return func() tea.Msg {
			model := cfg.Ocode.CommitMsgModel
			if model == "" {
				model = "openai/gpt-5.4-mini"
			}
			client := agent.NewClient(cfg, model)
			if client == nil {
				return gitCommitMsgMsg{err: fmt.Errorf("no LLM configured")}
			}
			if len(diff) > 8000 {
				diff = diff[:8000]
			}
			prompt := cfg.Ocode.CommitMsgPrompt
			if prompt == "" {
				prompt = "Write a concise git commit message (subject line only, max 72 chars) for these changes. Output only the commit message text, nothing else:"
			}
			msg, err := client.Chat([]agent.Message{{Role: "user", Content: prompt + "\n\n" + diff}}, nil)
			if err != nil {
				return gitCommitMsgMsg{err: err}
			}
			return gitCommitMsgMsg{text: strings.TrimSpace(msg.Content)}
		}
	}
}

// countRole returns the number of TUI messages with the given role.
func countRole(msgs []message, r role) int {
	n := 0
	for _, m := range msgs {
		if m.role == r {
			n++
		}
	}
	return n
}

// countToolMsgs returns the number of TUI messages that are tool results.
func countToolMsgs(msgs []message) int {
	n := 0
	for _, m := range msgs {
		if m.raw != nil && m.raw.Role == "tool" {
			n++
		}
	}
	return n
}
