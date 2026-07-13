package telegram

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/rc"
)

// maxMsgRunes is the Telegram per-message length cap (we trim to stay safe).
const maxMsgRunes = 4000

// Bot drives running ocode /rc instances from Telegram.
type Bot struct {
	client  *Client
	allowed map[int64]bool // empty => allow everyone
	rcDir   string         // override registry dir; "" => default

	mu       sync.Mutex
	selected map[int64]string // chatID -> selected instanceID
	active   map[int64]bool   // chatID -> relay in progress

	// pending approvals/questions awaiting a Telegram inline-button press.
	pendingApprovals map[string]approvalPending
	pendingQuestions map[string]questionPending

	// awaitingCustom records that the next plain-text message from a chat is a
	// free-form "Other" answer for a specific pending question (Telegram inline
	// buttons cannot capture text, so we collect it from the following message).
	awaitingCustom map[int64]customAwait
}

// customAwait identifies which pending question is awaiting a custom-text reply.
type customAwait struct {
	key string
	qi  int
}

// approvalPending records a permission ask awaiting a Telegram decision.
type approvalPending struct {
	instanceID string
	requestID  string
	tool       string
	msgID      int // Telegram message id of the prompt, for in-place update
}

// questionPending records a question prompt awaiting Telegram answers.
type questionPending struct {
	instanceID string
	requestID  string
	questions  []questionPromptLite
	// selected maps a question index (qi) to the list of selected option
	// indices (oi). Multiple entries when the question allows Multiple.
	selected map[int][]int
	// custom maps a question index (qi) to a free-form "Other" answer typed by
	// the user (mutually exclusive with selected[qi], like the TUI).
	custom map[int]string
	msgID  int
}

// questionPromptLite is the bot-side view of a question prompt.
type questionPromptLite struct {
	Header   string
	Question string
	Options  []string // option labels
	Multiple bool
}

// permissionEvent / questionEvent are the SSE payloads emitted by the TUI.
type permissionEvent struct {
	RequestID string `json:"request_id"`
	Tool      string `json:"tool"`
	Command   string `json:"command"`
	Rule      string `json:"rule"`
	Summary   string `json:"summary"`
}

type questionEvent struct {
	RequestID string `json:"request_id"`
	Questions []struct {
		Header   string `json:"header"`
		Question string `json:"question"`
		Options  []struct {
			Label       string `json:"label"`
			Description string `json:"description"`
		} `json:"options"`
		Multiple bool `json:"multiple"`
	} `json:"questions"`
}

// NewBot constructs a Bot. allowedUsers, when non-empty, restricts access to
// those Telegram user ids. rcDir overrides the registry location (test/embed).
func NewBot(token string, allowedUsers []int64, rcDir string) *Bot {
	allowed := make(map[int64]bool, len(allowedUsers))
	for _, u := range allowedUsers {
		allowed[u] = true
	}
	return &Bot{
		client:           NewClient(token),
		allowed:          allowed,
		rcDir:            rcDir,
		selected:         make(map[int64]string),
		active:           make(map[int64]bool),
		pendingApprovals: make(map[string]approvalPending),
		pendingQuestions: make(map[string]questionPending),
		awaitingCustom:   make(map[int64]customAwait),
	}
}

// Run polls Telegram until ctx is cancelled.
func (b *Bot) Run(ctx context.Context) error {
	b.registerCommands()
	offset := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		updates, err := b.client.GetUpdatesRaw(offset, 30)
		if err != nil {
			// Transient network error: back off briefly and retry.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
			}
			continue
		}
		for _, u := range updates {
			offset = u.UpdateID + 1
			b.dispatch(ctx, u)
		}
	}
}

func (b *Bot) registerCommands() {
	cmds := []CommandDesc{
		{Command: "sessions", Description: "List running ocode instances"},
		{Command: "session", Description: "/session <id> select an instance"},
		{Command: "current", Description: "Show the selected instance"},
		{Command: "yolo", Description: "/yolo on|off toggle autonomous mode"},
		{Command: "help", Description: "Show help"},
	}
	_ = b.client.SetMyCommands(cmds)
}

// allowedUser reports whether the given Telegram user may use the bot.
func (b *Bot) allowedUser(userID int64) bool {
	if len(b.allowed) == 0 {
		return true
	}
	return b.allowed[userID]
}

func (b *Bot) dispatch(ctx context.Context, u Update) {
	if u.Message != nil && u.Message.From != nil {
		if !b.allowedUser(u.Message.From.ID) {
			_, _ = b.client.SendMessage(u.Message.Chat.ID, "🚫 Unauthorized.", nil)
			return
		}
		b.handleMessage(ctx, u.Message.Chat.ID, u.Message.Text)
		return
	}
	if u.CallbackQuery != nil {
		if !b.allowedUser(u.CallbackQuery.From.ID) {
			_ = b.client.AnswerCallbackQuery(u.CallbackQuery.ID)
			return
		}
		b.handleCallback(ctx, u.CallbackQuery)
	}
}

// parseCommand splits "/cmd arg1 arg2" into (cmd, args).
func parseCommand(text string) (string, []string) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", nil
	}
	parts := strings.Fields(text)
	cmd := strings.ToLower(strings.TrimPrefix(parts[0], "/"))
	// Strip a possible @botusername suffix.
	if i := strings.Index(cmd, "@"); i >= 0 {
		cmd = cmd[:i]
	}
	if len(parts) > 1 {
		return cmd, parts[1:]
	}
	return cmd, nil
}

func (b *Bot) handleMessage(ctx context.Context, chatID int64, text string) {
	// If we asked for a free-form "Other" answer, the next (non-command) message
	// is that answer — consume it instead of relaying it as a new chat message.
	b.mu.Lock()
	_, awaiting := b.awaitingCustom[chatID]
	b.mu.Unlock()
	if awaiting {
		if strings.HasPrefix(strings.TrimSpace(text), "/") {
			// A command cancels the pending custom-text capture.
			b.mu.Lock()
			delete(b.awaitingCustom, chatID)
			b.mu.Unlock()
		} else {
			b.handleCustomText(chatID, text)
			return
		}
	}
	cmd, args := parseCommand(text)
	switch cmd {
	case "", "start", "help":
		b.sendHelp(chatID)
	case "sessions":
		b.sendSessions(chatID)
	case "session":
		if len(args) == 0 {
			b.sendSessions(chatID)
			return
		}
		b.selectSession(chatID, args[0])
	case "current":
		b.sendCurrent(chatID)
	case "yolo":
		mode := "on"
		if len(args) > 0 {
			mode = strings.ToLower(args[0])
		}
		b.setYolo(chatID, mode == "on" || mode == "true" || mode == "1")
	default:
		// Plain text => relay to the selected instance.
		b.relayOrPrompt(ctx, chatID, text)
	}
}

func (b *Bot) sendHelp(chatID int64) {
	help := "ocode Telegram bot\n\n" +
		"Each running ocode instance with /rc enabled appears as a session.\n" +
		"/sessions - list running instances\n" +
		"/session <id> - select an instance to talk to\n" +
		"/current - show the selected instance\n" +
		"/yolo on|off - toggle autonomous mode (no approval prompts)\n" +
		"/help - this message\n\n" +
		"Any other message is sent to the selected instance and streamed back.\n" +
		"In non-yolo mode, permission prompts still appear in the ocode terminal."
	_, _ = b.client.SendMessage(chatID, help, nil)
}

func (b *Bot) sendSessions(chatID int64) {
	entries, err := b.listInstances()
	if err != nil {
		_, _ = b.client.SendMessage(chatID, "❌ Could not list instances: "+err.Error(), nil)
		return
	}
	if len(entries) == 0 {
		_, _ = b.client.SendMessage(chatID, "No running ocode instances found. Start one and run /rc in ocode.", nil)
		return
	}
	var rows [][]InlineButton
	for i, e := range entries {
		label := fmt.Sprintf("#%d %s · %s", i+1, short(e.SessionID, 6), short(e.Model, 10))
		rows = append(rows, []InlineButton{{Text: label, CallbackData: "sel:" + e.InstanceID}})
	}
	markup := &ReplyMarkup{InlineKeyboard: rows}
	_, _ = b.client.SendMessage(chatID, "Select an ocode instance:", markup)
}

func (b *Bot) selectSession(chatID int64, id string) {
	e, ok := b.findInstance(id)
	if !ok {
		_, _ = b.client.SendMessage(chatID, "❌ No instance matches "+id+". Use /sessions.", nil)
		return
	}
	b.mu.Lock()
	b.selected[chatID] = e.InstanceID
	b.mu.Unlock()
	b.sendCurrent(chatID)
}

func (b *Bot) sendCurrent(chatID int64) {
	e, ok := b.current(chatID)
	if !ok {
		_, _ = b.client.SendMessage(chatID, "No instance selected. Use /sessions.", nil)
		return
	}
	msg := fmt.Sprintf("Selected instance:\nID: %s\nSession: %s\nModel: %s\nCWD: %s\nAddr: %s",
		e.InstanceID, e.SessionID, e.Model, e.CWD, e.Addr)
	_, _ = b.client.SendMessage(chatID, msg, nil)
}

// findInstance resolves an instance id, honoring a non-default rcDir when the
// bot is configured with OCODE_TG_RC_DIR so every lookup stays consistent with
// listInstances (which also scopes to that directory).
func (b *Bot) findInstance(id string) (rc.Entry, bool) {
	if b.rcDir != "" {
		return rc.FindIn(b.rcDir, id)
	}
	return rc.Find(id)
}

func (b *Bot) current(chatID int64) (rc.Entry, bool) {
	b.mu.Lock()
	id := b.selected[chatID]
	b.mu.Unlock()
	if id == "" {
		return rc.Entry{}, false
	}
	return b.findInstance(id)
}

func (b *Bot) relayOrPrompt(ctx context.Context, chatID int64, text string) {
	e, ok := b.current(chatID)
	if !ok {
		b.sendSessions(chatID)
		return
	}
	b.mu.Lock()
	if b.active[chatID] {
		b.mu.Unlock()
		_, _ = b.client.SendMessage(chatID, "⏳ Still working on your previous message. Please wait.", nil)
		return
	}
	b.active[chatID] = true
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		delete(b.active, chatID)
		b.mu.Unlock()
	}()

	b.relay(ctx, chatID, e, text)
}

// relay streams a message to an ocode instance and mirrors it into Telegram,
// batching edits to respect Telegram's ~1/sec edit rate limit.
func (b *Bot) relay(ctx context.Context, chatID int64, e rc.Entry, text string) {
	msgID, err := b.client.SendMessage(chatID, "⏳ thinking…", nil)
	if err != nil {
		return
	}

	// The auth token is sent via the Authorization: Bearer header (see
	// StreamEvents), never in the URL query string, so it cannot leak into
	// proxy/access logs.
	streamURL := fmt.Sprintf("http://%s/api/chat/stream?session=%s&message=%s&remote=telegram",
		e.Addr,
		url.QueryEscape(e.SessionID),
		url.QueryEscape(text),
	)

	var (
		buf          strings.Builder
		mu           sync.Mutex
		done         = make(chan struct{})
		ctx2, cancel = context.WithCancel(ctx)
	)
	defer cancel()

	var (
		lastEvent   = time.Now()
		stalledNote bool
	)
	flush := func(final bool) {
		mu.Lock()
		content := buf.String()
		mu.Unlock()
		if content == "" && !final {
			return
		}
		if content == "" {
			content = "✅ done"
		}
		_ = b.client.EditMessageText(chatID, msgID, trimRunes(content, maxMsgRunes))
	}
	go func() {
		t := time.NewTicker(1200 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-ctx2.Done():
				return
			case <-done:
				return
			case <-t.C:
				mu.Lock()
				stalled := time.Since(lastEvent) > 90*time.Second
				mu.Unlock()
				if stalled && !stalledNote {
					mu.Lock()
					buf.WriteString("\n⏸️ Paused — approval may be required at the ocode terminal.")
					stalledNote = true
					mu.Unlock()
				}
				flush(false)
			}
		}
	}()

	var toolOut strings.Builder
	streamErr := StreamEvents(ctx2, streamURL, e.Token, func(ev SSEEvent) {
		mu.Lock()
		lastEvent = time.Now()
		stalledNote = false
		mu.Unlock()
		switch ev.Event {
		case "text":
			var d struct {
				Delta string `json:"delta"`
			}
			if decodeData(ev.Data, &d) == nil {
				mu.Lock()
				buf.WriteString(d.Delta)
				mu.Unlock()
			}
		case "tool_start":
			var d struct {
				Tool    string `json:"tool"`
				Command string `json:"command"`
			}
			if decodeData(ev.Data, &d) == nil {
				mu.Lock()
				buf.WriteString(fmt.Sprintf("\n🔧 %s %s\n", d.Tool, d.Command))
				mu.Unlock()
			}
		case "tool_result":
			var d struct {
				Tool   string `json:"tool"`
				Output string `json:"output"`
			}
			if decodeData(ev.Data, &d) == nil && d.Output != "" {
				mu.Lock()
				buf.WriteString(fmt.Sprintf("\n✅ %s:\n%s\n", d.Tool, truncate(d.Output, 400)))
				mu.Unlock()
				toolOut.WriteString(fmt.Sprintf("\n# %s\n%s\n", d.Tool, d.Output))
			}
		case "permission":
			var d permissionEvent
			if decodeData(ev.Data, &d) == nil {
				b.sendPermissionPrompt(chatID, e, d)
			}
		case "question":
			var d questionEvent
			if decodeData(ev.Data, &d) == nil {
				b.sendQuestionPrompt(chatID, e, d)
			}
		case "error":
			mu.Lock()
			buf.WriteString("\n❌ " + ev.Data)
			mu.Unlock()
			flush(true)
			cancel()
		case "done":
			flush(true)
			close(done)
			cancel()
		}
	})

	// Final safety flush once the stream ends.
	flush(true)
	if streamErr != nil {
		_, _ = b.client.SendMessage(chatID, "❌ stream error: "+trimRunes(streamErr.Error(), 200), nil)
	}
	if toolOut.Len() > 0 {
		_ = b.client.SendDocument(chatID, "tool-output.txt", []byte(toolOut.String()), "Full tool output")
	}
}

func (b *Bot) handleCallback(ctx context.Context, cb *struct {
	ID   string `json:"id"`
	From struct {
		ID int64 `json:"id"`
	} `json:"from"`
	Data    string `json:"data"`
	Message struct {
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	} `json:"message"`
}) {
	_ = b.client.AnswerCallbackQuery(cb.ID)
	data := cb.Data
	chatID := cb.Message.Chat.ID
	switch {
	case strings.HasPrefix(data, "sel:"):
		b.selectSession(chatID, strings.TrimPrefix(data, "sel:"))
	case data == "yolo:on":
		b.setYolo(chatID, true)
	case data == "yolo:off":
		b.setYolo(chatID, false)
	case strings.HasPrefix(data, "perm:"):
		// perm:<key>:<allow|always|deny>
		parts := strings.Split(data, ":")
		if len(parts) == 3 {
			key := parts[1]
			decision := parts[2]
			b.mu.Lock()
			pa, ok := b.pendingApprovals[key]
			delete(b.pendingApprovals, key)
			b.mu.Unlock()
			if ok {
				b.resolvePermission(chatID, pa, decision)
			}
		}
	case strings.HasPrefix(data, "q:"):
		// q:<key>:submit  OR  q:<key>:<qi>:<oi>
		parts := strings.Split(data, ":")
		if len(parts) >= 2 {
			key := parts[1]
			if len(parts) == 3 && parts[2] == "submit" {
				b.mu.Lock()
				pq, ok := b.pendingQuestions[key]
				b.mu.Unlock()
				if !ok {
					break
				}
				if !questionComplete(pq) {
					_, _ = b.client.SendMessage(chatID, "⚠️ Select at least one option for every question, then tap ✅ Submit.", nil)
					break
				}
				b.mu.Lock()
				delete(b.pendingQuestions, key)
				b.mu.Unlock()
				b.submitQuestion(chatID, pq)
				break
			}
			if len(parts) == 4 {
				qi, _ := strconv.Atoi(parts[2])
				if parts[3] == "other" {
					b.awaitCustomText(chatID, key, qi)
				} else {
					oi, _ := strconv.Atoi(parts[3])
					b.toggleQuestionOption(chatID, key, qi, oi)
				}
			}
		}
	}
}

// randKey returns a short random key used to correlate inline-button presses
// with pending approvals/questions (keeps callback_data well under 64 bytes).
func randKey() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// sendPermissionPrompt posts an inline-keyboard approval request in Telegram.
func (b *Bot) sendPermissionPrompt(chatID int64, e rc.Entry, ev permissionEvent) {
	key := randKey()
	b.mu.Lock()
	b.pendingApprovals[key] = approvalPending{instanceID: e.InstanceID, requestID: ev.RequestID, tool: ev.Tool}
	b.mu.Unlock()

	text := fmt.Sprintf("🔐 *Permission required*\nTool: `%s`", ev.Tool)
	if ev.Command != "" {
		text += fmt.Sprintf("\n```\n%s\n```", truncate(ev.Command, 500))
	}
	if ev.Summary != "" {
		text += "\n" + ev.Summary
	}
	markup := &ReplyMarkup{InlineKeyboard: [][]InlineButton{
		{{Text: "✅ Approve", CallbackData: "perm:" + key + ":allow"}},
		{{Text: "✅ Approve always", CallbackData: "perm:" + key + ":always"}},
		{{Text: "❌ Deny", CallbackData: "perm:" + key + ":deny"}},
	}}
	if msgID, err := b.client.SendMessage(chatID, text, markup); err == nil {
		b.mu.Lock()
		if pa, ok := b.pendingApprovals[key]; ok {
			pa.msgID = msgID
			b.pendingApprovals[key] = pa
		}
		b.mu.Unlock()
	}
}

// sendQuestionPrompt posts an inline-keyboard question prompt. Each question's
// options are buttons; the user toggles selections (multiple when allowed) and
// taps ✅ Submit to send all answers at once — so multi-question and multi-select
// prompts are answered correctly instead of resolving on the first tap.
func (b *Bot) sendQuestionPrompt(chatID int64, e rc.Entry, ev questionEvent) {
	key := randKey()
	lite := make([]questionPromptLite, 0, len(ev.Questions))
	for _, q := range ev.Questions {
		opts := make([]string, 0, len(q.Options))
		for _, o := range q.Options {
			opts = append(opts, o.Label)
		}
		lite = append(lite, questionPromptLite{Header: q.Header, Question: q.Question, Options: opts, Multiple: q.Multiple})
	}
	pq := questionPending{instanceID: e.InstanceID, requestID: ev.RequestID, questions: lite, selected: map[int][]int{}, custom: map[int]string{}}
	b.mu.Lock()
	b.pendingQuestions[key] = pq
	b.mu.Unlock()

	text := "❓ Question — tap options (multiple if allowed), then ✅ Submit:"
	markup := &ReplyMarkup{InlineKeyboard: buildQuestionKeyboard(key, pq)}
	if msgID, err := b.client.SendMessage(chatID, text, markup); err == nil {
		b.mu.Lock()
		if p, ok := b.pendingQuestions[key]; ok {
			p.msgID = msgID
			b.pendingQuestions[key] = p
		}
		b.mu.Unlock()
	}
}

// buildQuestionKeyboard renders the inline keyboard for a question prompt,
// prefixing selected options with ✓, appending an "Other" (custom-text) button
// per question (matching the TUI's affordance), and a final Submit button.
func buildQuestionKeyboard(key string, pq questionPending) [][]InlineButton {
	var rows [][]InlineButton
	for qi, q := range pq.questions {
		header := q.Header
		if header == "" {
			header = q.Question
		}
		rows = append(rows, []InlineButton{{Text: "❓ " + short(header, 40), CallbackData: "qhdr:" + key + ":" + strconv.Itoa(qi)}})
		for oi, opt := range q.Options {
			// An "Other"-type option captures free text, so it routes to the
			// custom-answer flow rather than a normal selection toggle.
			if isOtherLabel(opt) {
				rows = append(rows, []InlineButton{{Text: "✏️ " + short(opt, 46), CallbackData: "q:" + key + ":" + strconv.Itoa(qi) + ":other"}})
				continue
			}
			prefix := ""
			if isSelected(pq.selected, qi, oi) {
				prefix = "✓ "
			}
			rows = append(rows, []InlineButton{{Text: prefix + short(opt, 48), CallbackData: "q:" + key + ":" + strconv.Itoa(qi) + ":" + strconv.Itoa(oi)}})
			if len(rows) >= 95 {
				break
			}
		}
		// Synthesize an "Other" button when the prompt didn't include one.
		if !otherOptionExists(q) {
			otherLabel := "✏️ Something else"
			if c, ok := pq.custom[qi]; ok && c != "" {
				otherLabel = "✏️ " + short(c, 40)
			}
			rows = append(rows, []InlineButton{{Text: otherLabel, CallbackData: "q:" + key + ":" + strconv.Itoa(qi) + ":other"}})
		}
		if len(rows) >= 95 {
			break
		}
	}
	rows = append(rows, []InlineButton{{Text: "✅ Submit answers", CallbackData: "q:" + key + ":submit"}})
	return rows
}

// isSelected reports whether option oi of question qi is currently selected.
func isSelected(sel map[int][]int, qi, oi int) bool {
	for _, x := range sel[qi] {
		if x == oi {
			return true
		}
	}
	return false
}

// isOtherLabel reports whether an option label denotes a free-text "Other"
// choice (mirrors the TUI's question_prompt.go detection).
func isOtherLabel(label string) bool {
	lower := strings.ToLower(strings.TrimSpace(label))
	return strings.Contains(lower, "something else") || strings.Contains(lower, "other") ||
		strings.Contains(lower, "own answer") || strings.Contains(lower, "custom")
}

// otherOptionExists reports whether a question already lists an "Other" option.
func otherOptionExists(q questionPromptLite) bool {
	for _, o := range q.Options {
		if isOtherLabel(o) {
			return true
		}
	}
	return false
}

// resolvePermission posts a decision to the instance and updates the prompt.
func (b *Bot) resolvePermission(chatID int64, pa approvalPending, decision string) {
	e, ok := b.findInstance(pa.instanceID)
	if !ok {
		_, _ = b.client.SendMessage(chatID, "❌ Instance no longer available.", nil)
		return
	}
	// Auth via the Authorization: Bearer header, not the URL query string, to
	// avoid leaking the token into logs/proxies.
	u := fmt.Sprintf("http://%s/api/rc/permission/resolve", e.Addr)
	body := strings.NewReader(fmt.Sprintf(`{"request_id":%q,"decision":%q}`, pa.requestID, decision))
	req, err := http.NewRequest(http.MethodPost, u, body)
	if err != nil {
		log.Printf("telegram: resolvePermission: build request for %s: %v", pa.instanceID, err)
		_ = b.client.EditMessageText(chatID, pa.msgID, "❌ Failed to build request: "+err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.Token)
	resp, err := http.DefaultClient.Do(req)
	outcome := "✅ Approved"
	switch decision {
	case "deny":
		outcome = "❌ Denied"
	case "always":
		outcome = "✅ Approved (always)"
	}
	if err != nil {
		outcome = "❌ Could not reach instance: " + err.Error()
	} else {
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			outcome = fmt.Sprintf("❌ resolve failed (%d): %s", resp.StatusCode, string(raw))
		}
	}
	_ = b.client.EditMessageText(chatID, pa.msgID, outcome+" · "+pa.tool)
}

// toggleQuestionOption flips the selection state of one option on the server
// side (so we stay under Telegram's 64-byte callback_data limit) and re-renders
// the keyboard in place. Single-select questions replace; multi-select toggle.
func (b *Bot) toggleQuestionOption(chatID int64, key string, qi, oi int) {
	b.mu.Lock()
	pq, ok := b.pendingQuestions[key]
	if !ok {
		b.mu.Unlock()
		return
	}
	if qi < 0 || qi >= len(pq.questions) || oi < 0 || oi >= len(pq.questions[qi].Options) {
		b.mu.Unlock()
		return
	}
	if pq.selected == nil {
		pq.selected = map[int][]int{}
	}
	if pq.questions[qi].Multiple {
		list := pq.selected[qi]
		found := -1
		for idx, x := range list {
			if x == oi {
				found = idx
				break
			}
		}
		if found >= 0 {
			list = append(list[:found], list[found+1:]...)
		} else {
			list = append(list, oi)
		}
		pq.selected[qi] = list
	} else {
		pq.selected[qi] = []int{oi}
	}
	msgID := pq.msgID
	b.pendingQuestions[key] = pq
	b.mu.Unlock()

	_ = b.client.EditMessageReplyMarkup(chatID, msgID, &ReplyMarkup{InlineKeyboard: buildQuestionKeyboard(key, pq)})
}

// awaitCustomText puts the chat into "collect the next message as a free-text
// answer" mode for question qi, and prompts the user to reply (ForceReply pops
// up the input box in Telegram). Inline buttons cannot carry text, so the
// answer arrives as the following message.
func (b *Bot) awaitCustomText(chatID int64, key string, qi int) {
	b.mu.Lock()
	pq, ok := b.pendingQuestions[key]
	b.mu.Unlock()
	if !ok || qi < 0 || qi >= len(pq.questions) {
		return
	}
	prompt := pq.questions[qi].Question
	if prompt == "" {
		prompt = pq.questions[qi].Header
	}
	b.mu.Lock()
	b.awaitingCustom[chatID] = customAwait{key: key, qi: qi}
	b.mu.Unlock()
	_, _ = b.client.SendMessage(chatID, "✏️ Reply with your answer for: "+short(prompt, 80), &ReplyMarkup{ForceReply: true})
}

// handleCustomText consumes the user's free-text "Other" reply and either
// resolves the prompt immediately (single-question prompts are one step) or
// re-renders the keyboard so the user can finish and tap Submit.
func (b *Bot) handleCustomText(chatID int64, text string) {
	b.mu.Lock()
	aw, ok := b.awaitingCustom[chatID]
	pq, pqOK := b.pendingQuestions[aw.key]
	if ok {
		delete(b.awaitingCustom, chatID)
	}
	if ok && pqOK {
		if pq.custom == nil {
			pq.custom = map[int]string{}
		}
		pq.custom[aw.qi] = text
		pq.selected[aw.qi] = nil // custom answer replaces normal selections
		b.pendingQuestions[aw.key] = pq
	}
	b.mu.Unlock()
	if !ok || !pqOK {
		return
	}
	if len(pq.questions) == 1 {
		// One-step resolution for a single-question prompt.
		b.mu.Lock()
		delete(b.pendingQuestions, aw.key)
		b.mu.Unlock()
		b.submitQuestion(chatID, pq)
		return
	}
	b.mu.Lock()
	msgID := pq.msgID
	b.mu.Unlock()
	_ = b.client.EditMessageReplyMarkup(chatID, msgID, &ReplyMarkup{InlineKeyboard: buildQuestionKeyboard(aw.key, pq)})
}

// questionComplete reports whether every question has at least one answer
// (either a selected option or a free-form custom answer).
func questionComplete(pq questionPending) bool {
	for i := range pq.questions {
		if len(pq.selected[i]) == 0 {
			if c, ok := pq.custom[i]; !ok || c == "" {
				return false
			}
		}
	}
	return true
}

// submitQuestion builds the full per-question answer sets and posts them to the
// instance, then clears the prompt's inline keyboard.
func (b *Bot) submitQuestion(chatID int64, pq questionPending) {
	e, ok := b.findInstance(pq.instanceID)
	if !ok {
		_, _ = b.client.SendMessage(chatID, "❌ Instance no longer available.", nil)
		return
	}
	sets := make([]map[string]interface{}, 0, len(pq.questions))
	for qi, q := range pq.questions {
		ans := make([]map[string]interface{}, 0, len(pq.selected[qi]))
		if custom, ok := pq.custom[qi]; ok && custom != "" {
			// A free-form "Other" answer replaces any normal selections.
			ans = append(ans, map[string]interface{}{"label": custom, "text": custom, "custom": true})
		} else {
			for _, oi := range pq.selected[qi] {
				label := q.Options[oi]
				ans = append(ans, map[string]interface{}{"label": label, "text": label, "custom": false})
			}
		}
		sets = append(sets, map[string]interface{}{"header": q.Header, "question": q.Question, "answers": ans})
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"request_id": pq.requestID,
		"answers":    sets,
	})
	// Auth via the Authorization: Bearer header, not the URL query string.
	u := fmt.Sprintf("http://%s/api/rc/question/answer", e.Addr)
	req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(payload))
	if err != nil {
		log.Printf("telegram: submitQuestion: build request for %s: %v", pq.instanceID, err)
		_ = b.client.EditMessageText(chatID, pq.msgID, "❌ Failed to build request: "+err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.Token)
	resp, err := http.DefaultClient.Do(req)
	outcome := "✅ Answers submitted"
	if err != nil {
		outcome = "❌ Could not reach instance: " + err.Error()
	} else {
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			outcome = fmt.Sprintf("❌ submit failed (%d): %s", resp.StatusCode, string(raw))
		}
	}
	_ = b.client.EditMessageText(chatID, pq.msgID, outcome)
	// Clear the inline keyboard so the resolved prompt is no longer tappable.
	_ = b.client.EditMessageReplyMarkup(chatID, pq.msgID, nil)
}

func (b *Bot) setYolo(chatID int64, on bool) {
	e, ok := b.current(chatID)
	if !ok {
		b.sendSessions(chatID)
		return
	}
	// Auth via the Authorization: Bearer header, not the URL query string.
	u := fmt.Sprintf("http://%s/api/permissions/yolo", e.Addr)
	method := http.MethodPut
	body := strings.NewReader(fmt.Sprintf(`{"enabled":%t}`, on))
	req, err := http.NewRequest(method, u, body)
	if err != nil {
		_, _ = b.client.SendMessage(chatID, "❌ "+err.Error(), nil)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		_, _ = b.client.SendMessage(chatID, "❌ Could not reach instance: "+err.Error(), nil)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		_, _ = b.client.SendMessage(chatID, fmt.Sprintf("❌ yolo toggle failed (%d): %s", resp.StatusCode, string(raw)), nil)
		return
	}
	state := "off"
	if on {
		state = "on"
	}
	_, _ = b.client.SendMessage(chatID, fmt.Sprintf("🔧 yolo %s for %s", state, short(e.SessionID, 6)), nil)
}

// listInstances returns live registry entries.
func (b *Bot) listInstances() ([]rc.Entry, error) {
	if b.rcDir != "" {
		return rc.ListIn(b.rcDir, rc.DefaultTTL)
	}
	return rc.List(rc.DefaultTTL)
}

func short(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func trimRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}
