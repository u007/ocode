//go:build integration

package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
)

// Throwaway A/B experiment, not a regression test: compares the batched
// summary loop against a single in-conversation "summarise now" turn on a real
// exported session. Skipped unless OCODE_COMPACT_EXPERIMENT_MSGS names a JSONL
// file of persisted messages (one Message per line).
//
//	OCODE_COMPACT_EXPERIMENT_MSGS=/path/msgs.jsonl \
//	OCODE_COMPACT_EXPERIMENT_MAIN=opencode-go/mimo-v2.5 \
//	OCODE_COMPACT_EXPERIMENT_MODES=loop,inline,inline \
//	go test ./internal/agent -run TestCompactInlineExperiment -v -timeout 60m

type experimentUsage struct {
	mu                       sync.Mutex
	calls                    int
	prompt, completion, read int64
}

func (u *experimentUsage) add(m *Message) {
	if m == nil || m.Usage == nil {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.calls++
	pt, ct, cr := int64(0), int64(0), int64(0)
	if m.Usage.PromptTokens != nil {
		pt = *m.Usage.PromptTokens
	}
	if m.Usage.CompletionTokens != nil {
		ct = *m.Usage.CompletionTokens
	}
	if m.Usage.CacheReadTokens != nil {
		cr = *m.Usage.CacheReadTokens
	}
	if !m.Usage.PromptIncludesCacheRead {
		pt += cr
	}
	u.prompt += pt
	u.completion += ct
	u.read += cr
}

// cost prices the totals with the registry's per-million rates; prompt is
// inclusive of cache reads here.
func (u *experimentUsage) cost(t *testing.T, modelID string) float64 {
	m, ok := modelEntryFor(modelID)
	if !ok {
		t.Fatalf("model %s not in registry", modelID)
	}
	uncached := float64(u.prompt - u.read)
	return (uncached*m.Cost.Input + float64(u.read)*m.Cost.CacheRead + float64(u.completion)*m.Cost.Output) / 1e6
}

func TestCompactInlineExperiment(t *testing.T) {
	path := os.Getenv("OCODE_COMPACT_EXPERIMENT_MSGS")
	if path == "" {
		t.Skip("set OCODE_COMPACT_EXPERIMENT_MSGS to run")
	}
	mainModel := os.Getenv("OCODE_COMPACT_EXPERIMENT_MAIN")
	modes := strings.Split(os.Getenv("OCODE_COMPACT_EXPERIMENT_MODES"), ",")

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var messages []Message
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		var m Message
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		messages = append(messages, m)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := config.LoadOcodeConfig(cfg); err != nil {
		t.Fatal(err)
	}
	cfg.ThinkingBudget = 0
	var debugMu sync.Mutex
	DebugAppend = func(kind, msg string) {
		debugMu.Lock()
		defer debugMu.Unlock()
		t.Logf("[%s] %s", kind, msg)
	}
	defer func() { DebugAppend = nil }()

	client := NewClient(cfg, mainModel)
	if client == nil {
		t.Fatalf("no client for %s", mainModel)
	}
	t.Logf("messages=%d est_tokens=%d main=%s", len(messages), messagesTokens(messages, charsPerTokenFor(client.GetProvider(), client.GetModel())), mainModel)

	for i, mode := range modes {
		name := fmt.Sprintf("%d_%s", i, mode)
		var summary string
		usage := &experimentUsage{}
		var priceModel string
		started := time.Now()
		switch mode {
		case "loop", "compact":
			a := newTestAgent(client, nil, cfg, nil)
			rt := a.resolveCompactRuntime(true)
			priceModel = mainModel
			if mode == "loop" {
				// No headroom reading -> runCompact takes the batched loop.
				a.lastInputTokens.Store(int64(rt.WindowTokens))
				sc := a.compactSummaryClient()
				priceModel = sc.GetProvider() + "/" + sc.GetModel()
			}
			a.OnSideUsage = func(pt, ct, crt, cwt int64, spend *float64) {
				// RecordSideUsage passes provider-shaped counts; opencode-go is
				// OpenAI-style (prompt includes cache reads).
				usage.mu.Lock()
				defer usage.mu.Unlock()
				usage.calls++
				usage.prompt += pt
				usage.completion += ct
				usage.read += crt
			}
			res := a.runCompact(messages, rt, "", true)
			if res.Err != nil || !res.OK {
				t.Errorf("%s: failed ok=%v err=%v", name, res.OK, res.Err)
				continue
			}
			summary = res.Summary.Content
			cpt := charsPerTokenFor(client.GetProvider(), client.GetModel())
			spliced := append(append(append([]Message{}, messages[:res.ReplaceFrom]...), res.Summary), messages[res.ReplaceTo:]...)
			t.Logf("%s: transcript %d msgs/~%d est tokens -> %d msgs/~%d est tokens (summary ~%d, kept tail ~%d)", name,
				len(messages), messagesTokens(messages, cpt), len(spliced), messagesTokens(spliced, cpt),
				tokenEstimate(res.Summary, cpt), messagesTokens(messages[res.ReplaceTo:], cpt))
			t.Logf("%s: replaced [%d:%d] note=%q", name, res.ReplaceFrom, res.ReplaceTo, res.Note)
		case "inline":
			gc, ok := client.(*GenericClient)
			if !ok {
				t.Fatalf("main client is %T", client)
			}
			priceModel = mainModel
			instruction := compactionSystemPrompt + "\n\n" + summaryTemplate + "\n\n" +
				"Stop working on the task. Do not call tools. Summarise the ENTIRE conversation above. " +
				"Respond now with ONLY the summary, starting with the \"## Original Request\" header and containing every template section in order."
			req := append(append([]Message{}, messages...), Message{Role: "user", Content: instruction})
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			var firstToken time.Duration
			var once sync.Once
			ctx = withDeltaCallback(ctx, func(string, string) { once.Do(func() { firstToken = time.Since(started) }) })
			ctx = context.WithValue(ctx, ctxKeyMaxTokens, int(ModelMaxOutputTokens(mainModel)))
			resp, err := gc.ChatWithContext(ctx, req, nil)
			cancel()
			if err != nil {
				t.Errorf("%s: %v", name, err)
				continue
			}
			usage.add(resp)
			summary = resp.Content
			t.Logf("%s: first_token=%s tool_calls=%d", name, firstToken.Round(time.Millisecond), len(resp.ToolCalls))
		default:
			t.Fatalf("unknown mode %q", mode)
		}
		elapsed := time.Since(started)
		verr := validateSummary(summary)
		out := path + "." + name + ".md"
		if err := os.WriteFile(out, []byte(summary), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("RESULT %s: model=%s wall=%s calls=%d prompt=%d cached=%d completion=%d cost=$%.5f summary_chars=%d valid=%v -> %s",
			name, priceModel, elapsed.Round(time.Millisecond), usage.calls, usage.prompt, usage.read, usage.completion,
			usage.cost(t, priceModel), len(summary), verr == nil, out)
	}
}
