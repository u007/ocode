package agent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// toolDigestStubClient is a minimal LLMClient for the contract tests. It never
// answers Chat: the payload, guard and key code only reads provider and model.
type toolDigestStubClient struct {
	provider string
	model    string
}

func (c *toolDigestStubClient) Chat([]Message, []map[string]interface{}) (*Message, error) {
	return nil, errors.New("toolDigestStubClient: Chat is not expected in contract tests")
}
func (c *toolDigestStubClient) GetProvider() string { return c.provider }
func (c *toolDigestStubClient) GetModel() string    { return c.model }

const digestTestMaxPage = 8192

var digestTestSecretRE = regexp.MustCompile(`sk-live-[A-Za-z0-9]+`)

// digestTestMask stands in for the real masker so the pager tests do not depend
// on the secret detector. It rewrites every sk-live- token to one placeholder.
func digestTestMask(s string) string {
	return digestTestSecretRE.ReplaceAllString(s, "sk-live-[MASKED]")
}

func digestTestIdentity(s string) string { return s }

func TestToolDigestPayloadPage_ExactWindowsLargePayloads(t *testing.T) {
	big := strings.Repeat("abcdefghij", 8*1024) // 80 KiB of ASCII
	args, err := json.Marshal(map[string]string{"blob": big})
	if err != nil {
		t.Fatal(err)
	}
	p, err := buildToolDigestPayload(ToolCallDigestInput{Tool: "read", Command: "cat big", Args: args}, digestTestIdentity)
	if err != nil {
		t.Fatalf("buildToolDigestPayload: %v", err)
	}
	if len(p.canonical) < 80*1024 {
		t.Fatalf("canonical payload is %d bytes, want at least 80 KiB", len(p.canonical))
	}
	if p.totalBytes != len(p.canonical) {
		t.Fatalf("totalBytes = %d, want len(canonical) = %d", p.totalBytes, len(p.canonical))
	}

	running := sha256.New()
	var got []byte
	pages := 0
	for offset := 0; offset < p.totalBytes; {
		pg, err := p.page(offset, digestTestMaxPage)
		if err != nil {
			t.Fatalf("page(%d): %v", offset, err)
		}
		pages++
		if pg.offset != offset {
			t.Fatalf("page.offset = %d, want %d", pg.offset, offset)
		}
		if len(pg.text) == 0 {
			t.Fatalf("empty page before the end at offset %d: no progress", offset)
		}
		if pg.nextOffset != offset+len(pg.text) {
			t.Fatalf("nextOffset = %d, want %d", pg.nextOffset, offset+len(pg.text))
		}
		if pg.nextOffset < p.totalBytes && len(pg.text) != digestTestMaxPage {
			t.Fatalf("non-final ASCII page is %d bytes, want exactly %d", len(pg.text), digestTestMaxPage)
		}
		if pg.totalBytes != p.totalBytes {
			t.Fatalf("page.totalBytes = %d, want %d", pg.totalBytes, p.totalBytes)
		}
		if pg.sha256 != p.sha256 {
			t.Fatalf("page.sha256 = %q, want %q", pg.sha256, p.sha256)
		}
		if pg.remainingBytes != p.totalBytes-pg.nextOffset {
			t.Fatalf("remainingBytes = %d, want %d", pg.remainingBytes, p.totalBytes-pg.nextOffset)
		}
		running.Write([]byte(pg.text))
		got = append(got, pg.text...)
		offset = pg.nextOffset
	}
	if pages < 10 {
		t.Fatalf("80 KiB in 8 KiB windows produced %d pages, want at least 10", pages)
	}
	if !bytes.Equal(got, p.canonical) {
		t.Fatal("concatenated pages do not reproduce the canonical payload exactly")
	}
	if hex.EncodeToString(running.Sum(nil)) != p.sha256 {
		t.Fatal("running SHA-256 over the pages does not equal the payload sha256")
	}

	end, err := p.page(p.totalBytes, digestTestMaxPage)
	if err != nil {
		t.Fatalf("page at the exact end: %v", err)
	}
	if end.text != "" || end.nextOffset != p.totalBytes || end.remainingBytes != 0 {
		t.Fatalf("end page = %+v, want empty page at total", end)
	}
}

func TestToolDigestPayloadPage_UTF8Boundaries(t *testing.T) {
	// 2-, 3- and 4-byte runes, with a 7-byte window that never lines up with a
	// rune boundary, so every page must shorten to one.
	cmd := strings.Repeat("é世🙂", 3000)
	p, err := buildToolDigestPayload(ToolCallDigestInput{Tool: "bash", Command: cmd}, digestTestIdentity)
	if err != nil {
		t.Fatalf("buildToolDigestPayload: %v", err)
	}

	var got []byte
	shortened := false
	for offset := 0; offset < p.totalBytes; {
		pg, err := p.page(offset, 7)
		if err != nil {
			t.Fatalf("page(%d, 7): %v", offset, err)
		}
		if !utf8.ValidString(pg.text) {
			t.Fatalf("page at offset %d splits a rune: %q", offset, pg.text)
		}
		if len(pg.text) < 7 && pg.nextOffset < p.totalBytes {
			shortened = true
		}
		got = append(got, pg.text...)
		offset = pg.nextOffset
	}
	if !shortened {
		t.Fatal("no page was shortened to a rune boundary; the window never hit a multibyte rune")
	}
	if !bytes.Equal(got, p.canonical) {
		t.Fatal("UTF-8-aligned pages do not reproduce the canonical payload")
	}

	// A window too small to hold the rune at the offset cannot make progress,
	// so it must fail instead of returning an empty page.
	lead := bytes.Index(p.canonical, []byte("世"))
	if lead < 0 {
		t.Fatal("fixture does not contain the 3-byte rune")
	}
	if _, err := p.page(lead, 1); err == nil {
		t.Fatal("page(offset on a 3-byte rune, 1) succeeded; want a zero-progress error")
	}
	// An offset in the middle of a rune is a caller bug, not a window to clip.
	if _, err := p.page(lead+1, digestTestMaxPage); err == nil {
		t.Fatal("page starting inside a multibyte rune succeeded; want an error")
	}
}

func TestToolDigestPayloadPage_InvalidOffsetAndWindow(t *testing.T) {
	p, err := buildToolDigestPayload(ToolCallDigestInput{Tool: "bash", Command: "git status"}, digestTestIdentity)
	if err != nil {
		t.Fatalf("buildToolDigestPayload: %v", err)
	}
	bad := []struct {
		name            string
		offset, maxSize int
	}{
		{"negative offset", -1, digestTestMaxPage},
		{"offset past end", p.totalBytes + 1, digestTestMaxPage},
		{"zero window", 0, 0},
		{"negative window", 0, -5},
		{"window over 8 KiB", 0, digestTestMaxPage + 1},
	}
	for _, c := range bad {
		if _, err := p.page(c.offset, c.maxSize); err == nil {
			t.Errorf("%s: page(%d, %d) succeeded, want error", c.name, c.offset, c.maxSize)
		}
	}
	if _, err := p.page(0, digestTestMaxPage); err != nil {
		t.Errorf("page(0, 8192) on a small payload: %v", err)
	}
	if pg, err := p.page(0, 1); err != nil || len(pg.text) != 1 {
		t.Errorf("page(0, 1) = %q, %v; want one byte", pg.text, err)
	}
	if pg, err := p.page(p.totalBytes, digestTestMaxPage); err != nil || pg.text != "" {
		t.Errorf("page at total = %q, %v; want an empty page", pg.text, err)
	}
}

func TestBuildToolDigestPayload_MalformedAndDuplicateArgs(t *testing.T) {
	rejected := []string{
		`{"a":1`,
		`{"a":1,"a":2}`,
		`{"outer":{"a":1,"a":2}}`,
		`[{"k":1,"k":2}]`,
		`{} {}`,
		`{}x`,
		`[1] 2`,
		`{"a":}`,
	}
	for _, raw := range rejected {
		in := ToolCallDigestInput{Tool: "bash", Command: "x", Args: json.RawMessage(raw)}
		if _, err := buildToolDigestPayload(in, digestTestIdentity); err == nil {
			t.Errorf("Args %s: accepted, want rejection", raw)
		}
	}

	accepted := []string{
		`[1,2]`,
		`"str"`,
		`42`,
		`null`,
		`true`,
		`  {"a":1}  `,
		`{"a":1,"b":{"a":2}}`,
	}
	for _, raw := range accepted {
		in := ToolCallDigestInput{Tool: "bash", Command: "x", Args: json.RawMessage(raw)}
		if _, err := buildToolDigestPayload(in, digestTestIdentity); err != nil {
			t.Errorf("Args %s: rejected: %v", raw, err)
		}
	}

	// An absent Args is JSON null, so it appears as "args":null.
	absent, err := buildToolDigestPayload(ToolCallDigestInput{Tool: "bash", Command: "x"}, digestTestIdentity)
	if err != nil {
		t.Fatalf("absent Args: %v", err)
	}
	if !bytes.Contains(absent.canonical, []byte(`"args":null`)) {
		t.Fatalf("absent Args canonical = %s, want \"args\":null", absent.canonical)
	}
}

func TestBuildToolDigestPayload_MasksNestedStringsAndKeysBeforeHash(t *testing.T) {
	args := `{"sk-live-KEYAAA1":"ok","zeta":{"token":"sk-live-SECRETAAA1"},"alpha":["sk-live-LISTBBB2",{"b":1,"a":2}]}`
	in := ToolCallDigestInput{Tool: "bash", Command: "curl sk-live-CMDSECRET1", Args: json.RawMessage(args), Rule: "sk-live-RULE0001", Scope: "sk-live-SCOPE001"}
	p, err := buildToolDigestPayload(in, digestTestMask)
	if err != nil {
		t.Fatalf("buildToolDigestPayload: %v", err)
	}
	for _, secret := range []string{"KEYAAA1", "SECRETAAA1", "LISTBBB2", "CMDSECRET1", "RULE0001", "SCOPE001"} {
		if bytes.Contains(p.canonical, []byte(secret)) {
			t.Errorf("canonical payload still contains %q: %s", secret, p.canonical)
		}
	}
	if !bytes.Contains(p.canonical, []byte("sk-live-[MASKED]")) {
		t.Fatalf("canonical payload has no masked placeholder: %s", p.canonical)
	}
	if !bytes.Contains(p.canonical, []byte(`{"a":2,"b":1}`)) {
		t.Fatalf("nested object keys are not sorted: %s", p.canonical)
	}
	alpha := bytes.Index(p.canonical, []byte(`"alpha"`))
	masked := bytes.Index(p.canonical, []byte(`"sk-live-[MASKED]"`))
	zeta := bytes.Index(p.canonical, []byte(`"zeta"`))
	if !(alpha >= 0 && alpha < masked && masked < zeta) {
		t.Fatalf("top-level argument keys are not in stable sorted order: %s", p.canonical)
	}

	// The hash covers the MASKED bytes: different raw secrets that mask to the
	// same text must produce the same canonical payload and the same digest.
	other := `{"sk-live-KEYEEE5":"ok","zeta":{"token":"sk-live-SECRETCCC3"},"alpha":["sk-live-LISTDDD4",{"b":1,"a":2}]}`
	in2 := ToolCallDigestInput{Tool: "bash", Command: "curl sk-live-CMDOTHER2", Args: json.RawMessage(other), Rule: "sk-live-RULE0002", Scope: "sk-live-SCOPE002"}
	p2, err := buildToolDigestPayload(in2, digestTestMask)
	if err != nil {
		t.Fatalf("second buildToolDigestPayload: %v", err)
	}
	if !bytes.Equal(p.canonical, p2.canonical) || p.sha256 != p2.sha256 {
		t.Fatalf("masked-equal inputs hashed differently:\n%s\n%s", p.canonical, p2.canonical)
	}

	// Two different raw keys that mask to one placeholder collide, and the
	// payload is refused rather than silently merged.
	collide := `{"sk-live-AAA1":1,"sk-live-BBB2":2}`
	if _, err := buildToolDigestPayload(ToolCallDigestInput{Tool: "bash", Command: "x", Args: json.RawMessage(collide)}, digestTestMask); err == nil {
		t.Fatal("masked key collision was accepted; want an error")
	}
}

func TestBuildToolDigestPayload_BashEnvRawArgsPreserved(t *testing.T) {
	in := ToolCallDigestInput{
		Tool:    "bash",
		Command: "git commit",
		Args:    json.RawMessage(`{"command":"EDITOR=vim git commit"}`),
	}
	p, err := buildToolDigestPayload(in, digestTestIdentity)
	if err != nil {
		t.Fatalf("buildToolDigestPayload: %v", err)
	}
	want := `{"tool":"bash","command":"git commit","args":{"command":"EDITOR=vim git commit"},"rule":"","scope":""}`
	if string(p.canonical) != want {
		t.Fatalf("canonical = %s\nwant      %s", p.canonical, want)
	}
	if got, wantLen := p.fieldBytes["args"], len(`{"command":"EDITOR=vim git commit"}`); got != wantLen {
		t.Fatalf("fieldBytes[args] = %d, want %d", got, wantLen)
	}
	if len(p.fieldBytes) != 5 {
		t.Fatalf("fieldBytes has %d entries, want 5: %v", len(p.fieldBytes), p.fieldBytes)
	}
}

func TestToolDigestContextGuard_KnownUnknownAndRefusal(t *testing.T) {
	// A synthetic registry snapshot gives openai/gpt-4o a known window and
	// output limit. The unknown model must resolve to no entry at all.
	home := t.TempDir()
	setHomeTree(t, home)
	t.Setenv("XDG_CACHE_HOME", home)
	t.Setenv("APPDATA", home)
	t.Setenv(envModelsPath, "")
	wrapped := registryFile{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Providers: map[string]providerEntry{
			"openai": {ID: "openai", Models: map[string]modelEntry{
				"gpt-4o": {ID: "gpt-4o", Limit: modelLimit{Context: 128000, Output: 16384}},
			}},
		},
	}
	snap, err := json.Marshal(wrapped)
	if err != nil {
		t.Fatal(err)
	}
	withSnapshotBytes(t, snap)
	if got := ModelWindow("openai/gpt-4o"); got != 128000 {
		t.Fatalf("ModelWindow(openai/gpt-4o) = %d, want 128000 from the fixture", got)
	}
	if got := ModelWindow("fakeprov/no-such-model-xyz"); got != 0 {
		t.Fatalf("ModelWindow(unknown) = %d, want 0", got)
	}

	known := newToolDigestContextGuard(&toolDigestStubClient{provider: "openai", model: "gpt-4o"})
	if known.Model != "openai/gpt-4o" {
		t.Errorf("known.Model = %q, want openai/gpt-4o", known.Model)
	}
	if known.ContextWindow != 128000 || known.ResponseReserve != 16384 {
		t.Errorf("known window/reserve = %d/%d, want 128000/16384", known.ContextWindow, known.ResponseReserve)
	}
	if known.CharsPerToken != charsPerTokenFor("openai", "gpt-4o") {
		t.Errorf("known.CharsPerToken = %v, want charsPerTokenFor(openai, gpt-4o)", known.CharsPerToken)
	}

	unknown := newToolDigestContextGuard(&toolDigestStubClient{provider: "fakeprov", model: "no-such-model-xyz"})
	if unknown.ContextWindow != 100000 || unknown.ResponseReserve != 8192 {
		t.Errorf("unknown window/reserve = %d/%d, want 100000/8192", unknown.ContextWindow, unknown.ResponseReserve)
	}

	small := []Message{{Role: "user", Content: "hello"}}
	if err := known.admit(small); err != nil {
		t.Errorf("known guard refused a small candidate: %v", err)
	}
	if err := unknown.admit(small); err != nil {
		t.Errorf("unknown guard refused a small candidate: %v", err)
	}

	// About 1.5M characters is well past a 128k window; the guard refuses and
	// never truncates the candidate.
	huge := []Message{{Role: "user", Content: strings.Repeat("word ", 300000)}}
	err = known.admit(huge)
	if err == nil {
		t.Fatal("known guard admitted an over-budget candidate")
	}
	var ctxErr *toolDigestContextError
	if !errors.As(err, &ctxErr) {
		t.Fatalf("refusal error type = %T, want *toolDigestContextError", err)
	}
	if ctxErr.RemainingTokens >= 0 {
		t.Errorf("RemainingTokens = %d, want negative for a refusal", ctxErr.RemainingTokens)
	}
	if !strings.Contains(err.Error(), "remaining_context_tokens") {
		t.Errorf("refusal message %q does not name remaining_context_tokens", err.Error())
	}
}

// TestToolDigestCacheKey_IDAndPayload: the cache key needs both the request id
// and the masked payload, so a reused id with another call misses, and a new id
// with the same call starts a fresh run.
func TestToolDigestCacheKey_IDAndPayload(t *testing.T) {
	if toolDigestCacheKey("owner", "call_0", "hashA") == toolDigestCacheKey("owner", "call_0", "hashB") {
		t.Fatal("a reused request id with a different payload shared a cache key")
	}
	if toolDigestCacheKey("owner", "call_0", "hashA") == toolDigestCacheKey("owner", "call_1", "hashA") {
		t.Fatal("a new request id with the same payload shared a cache key")
	}
	if toolDigestCacheKey("owner", "call_0", "hashA") != toolDigestCacheKey("owner", "call_0", "hashA") {
		t.Fatal("identical id and payload must share a cache key")
	}
}

func TestToolDigestKey_RequestIDAndMaskedPayloadHash(t *testing.T) {
	// With a request id the key ignores the payload hash.
	if toolDigestKey("owner", "req-1", "hashA") != toolDigestKey("owner", "req-1", "hashB") {
		t.Fatal("key changed with payload hash although a request id was given")
	}
	// Without one the key follows the masked payload hash.
	if toolDigestKey("owner", "", "hashA") == toolDigestKey("owner", "", "hashB") {
		t.Fatal("empty-id key did not change with the masked payload hash")
	}
	// The owner always separates keys.
	if toolDigestKey("owner", "req-1", "") == toolDigestKey("other", "req-1", "") {
		t.Fatal("owner did not separate keys")
	}
	want := sha256.Sum256([]byte("owner\x00req-1"))
	if got := toolDigestKey("owner", "req-1", "ignored"); got != hex.EncodeToString(want[:]) {
		t.Fatalf("request-id key = %q, want lowercase sha256(owner NUL id)", got)
	}
}
