package agent

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestSanitizeTitle(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Fix Bug In Agent", "Fix Bug In Agent"},
		{"\"Quoted Title\"", "Quoted Title"},
		{"  spaced  ", "spaced"},
		{"Title.\nGarbage second line", "Title"},
		{"With trailing period.", "With trailing period"},
		{"`backticks`", "backticks"},
		{"[bracketed]", "bracketed"},
	}
	for _, c := range cases {
		if got := sanitizeTitle(c.in); got != c.want {
			t.Errorf("sanitizeTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

type titleStubClient struct {
	reply string
}

func (c titleStubClient) Chat(messages []Message, tools []map[string]interface{}) (*Message, error) {
	return &Message{Role: "assistant", Content: c.reply}, nil
}
func (c titleStubClient) GetProvider() string { return "stub" }
func (c titleStubClient) GetModel() string    { return "stub" }
func (c titleStubClient) StreamChat(messages []Message, tools []map[string]interface{}, onChunk func(string)) (*Message, error) {
	return c.Chat(messages, tools)
}

func TestGenerateTitleAsync_DeliversSanitizedResult(t *testing.T) {
	a := &Agent{client: titleStubClient{reply: "  \"Hello World\"  "}}
	var (
		wg     sync.WaitGroup
		result string
	)
	wg.Add(1)
	a.GenerateTitleAsync("Help me fix a bug", "I'll look at it", func(t string) {
		result = t
		wg.Done()
	})
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("title callback never fired")
	}
	if result != "Hello World" {
		t.Fatalf("title = %q, want %q", result, "Hello World")
	}
}

func TestGenerateTitleAsync_EmptyUserSkipsCall(t *testing.T) {
	a := &Agent{client: titleStubClient{reply: "Should Not Be Used"}}
	got := make(chan string, 1)
	a.GenerateTitleAsync("   ", "", func(t string) { got <- t })
	select {
	case r := <-got:
		if r != "" {
			t.Fatalf("expected empty title for empty user msg, got %q", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("callback never fired")
	}
}

type titleErrClient struct{ calls *int }

func (c titleErrClient) Chat(messages []Message, tools []map[string]interface{}) (*Message, error) {
	*c.calls++
	return nil, errRateLimited
}
func (c titleErrClient) GetProvider() string { return "err" }
func (c titleErrClient) GetModel() string    { return "err-model" }
func (c titleErrClient) StreamChat(messages []Message, tools []map[string]interface{}, onChunk func(string)) (*Message, error) {
	return c.Chat(messages, tools)
}

var errRateLimited = fmt.Errorf("429 rate limited")

func TestGenerateTitleWithClients_FallsBackOnError(t *testing.T) {
	a := &Agent{}
	calls := 0
	clients := []LLMClient{
		titleErrClient{calls: &calls},
		titleStubClient{reply: "Fallback Title"},
	}
	got := a.generateTitleWithClients(clients, "sys", "prompt")
	if got != "Fallback Title" {
		t.Fatalf("expected fallback client result, got %q", got)
	}
	if calls != 1 {
		t.Fatalf("expected first client to be tried once, got %d", calls)
	}
}

func TestGenerateTitleWithClients_FallsBackOnEmptyReply(t *testing.T) {
	a := &Agent{}
	clients := []LLMClient{
		titleStubClient{reply: "   "},
		titleStubClient{reply: "Second Choice"},
	}
	if got := a.generateTitleWithClients(clients, "sys", "prompt"); got != "Second Choice" {
		t.Fatalf("expected empty reply to fall through, got %q", got)
	}
}

func TestGenerateTitleWithClients_AllFailReturnsEmpty(t *testing.T) {
	a := &Agent{}
	calls := 0
	clients := []LLMClient{titleErrClient{calls: &calls}, titleErrClient{calls: &calls}}
	if got := a.generateTitleWithClients(clients, "sys", "prompt"); got != "" {
		t.Fatalf("expected empty title when all clients fail, got %q", got)
	}
	if calls != 2 {
		t.Fatalf("expected both clients tried, got %d", calls)
	}
}
