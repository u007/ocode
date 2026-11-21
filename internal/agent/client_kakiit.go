package agent

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/u007/ocode/internal/config"
)

// kakiitProvider is the ocode provider id for the kakiit LLM gateway
// (https://hub.mercstudio.com/api/llm). Model ids are "kakiit/<model>". The
// gateway is OpenAI/Anthropic-compatible PER MODEL (x_kakiit_protocol), so
// the GenericClient serves it with per-model protocol dispatch — see
// usesAnthropicMessagesAPI / chatOpenAI in client.go and the live model list
// in models_registry.go.
const kakiitProvider = "kakiit"

// KakiitDefaultBaseURL is the production kakiit LLM gateway root.
const KakiitDefaultBaseURL = "https://hub.mercstudio.com/api/llm"

// kakiitRequestIDHeader is the per-turn request id the gateway persists on
// every ledger row it touches. It is SINGLE-USE at the gateway: replaying an id
// whose request is in flight or finished is refused with 409
// duplicate_request_id (a proxy cannot replay a response, so a replay would
// only double-bill the upstream). A turn therefore gets one attempt's worth of
// meaning out of its id; retries that reuse it fail closed rather than
// duplicating work.
const kakiitRequestIDHeader = "X-Request-Id"

// ctxKeyKakiitRequestID carries the per-turn kakiit request id through the
// transport layer (ChatWithContext → chatOpenAI/chatAnthropic) without
// mutating shared client state — the same pattern ctxKeyDeltaCallback uses.
type ctxKeyKakiitRequestID struct{}

// withKakiitRequestID parks the turn's request id on a context.
func withKakiitRequestID(ctx context.Context, id string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxKeyKakiitRequestID{}, id)
}

// kakiitRequestIDFromContext reads the turn's request id, "" when absent.
func kakiitRequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(ctxKeyKakiitRequestID{}).(string); ok {
		return v
	}
	return ""
}

// statusProviderLabel is the Provider field a typed status error carries.
//
// The protocol-specific transports label errors with the PROTOCOL ("anthropic",
// "openai responses"), but the kakiit classifier keys on the PROVIDER, so a
// gateway 402/409 on those routes went unrecognised and surfaced as raw JSON.
// Every kakiit transport reports "kakiit" instead; other providers keep their
// protocol label unchanged.
func (c *GenericClient) statusProviderLabel(fallback string) string {
	if c != nil && c.isKakiitProvider() {
		return kakiitProvider
	}
	return fallback
}

// isKakiitProvider reports whether this client talks to the kakiit LLM
// gateway.
func (c *GenericClient) isKakiitProvider() bool {
	return c != nil && c.Provider == kakiitProvider
}

// setKakiitHeaders stamps the kakiit per-turn X-Request-Id on an outbound
// gateway request. The sticky X-Opencode-Session rides the same request via
// setOpencodeSessionHeader (kakiit is admitted there by provider id), so this
// helper only adds the request id: minted once per ChatWithContext turn and
// parked on the context. Note the consequence for retries — the gateway refuses
// a reused id (409 duplicate_request_id) instead of re-executing it, so an
// ambiguous failure retried with the same id fails closed rather than billing
// the upstream twice.
func (c *GenericClient) setKakiitHeaders(req *http.Request) {
	if req == nil || c == nil || !c.isKakiitProvider() {
		return
	}
	if id := kakiitRequestIDFromContext(req.Context()); id != "" {
		req.Header.Set(kakiitRequestIDHeader, id)
	}
}

// KakiitTopUpMessage is the one-line user-visible notice for a gateway 402
// (llm_insufficient_credits). Verbatim per the spec — a user decides to top
// up; the turn has already failed.
const KakiitTopUpMessage = "Insufficient kakiit credits — top up at hub.mercstudio.com (or ask your site admin)"

// kakiitInsufficientCreditsCode is the gateway's error envelope code for an
// uncovered wallet hold (HTTP 402).
const kakiitInsufficientCreditsCode = "llm_insufficient_credits"

// kakiitDuplicateRequestIDCode is the gateway's envelope code for a reused
// X-Request-Id (HTTP 409). Since the gateway cannot replay a response, an id is
// SINGLE-USE: replaying one whose request finished, or one still in flight,
// is refused rather than re-executed (a replay would double-bill the upstream
// for one charge).
const kakiitDuplicateRequestIDCode = "duplicate_request_id"

// KakiitDuplicateRequestMessage is the one-line notice for that 409: the
// attempt already ran, so the user's "retry" must be a fresh turn (which mints
// a fresh id) rather than a resend of the same request.
const KakiitDuplicateRequestMessage = "kakiit: this request already ran (or is still running) — send it again to start a new attempt"

// kakiitUserFacingError maps a kakiit chat error to its one-line user-facing
// notice, "" when the error carries no kakiit-specific mapping (callers then
// render the generic error text).
//
// Classification follows the *providerStatusError conventions: HTTP status
// code FIRST, then the typed envelope code — never prose. A body that merely
// mentions credits on a non-402 status stays generic (pinned by test), and a
// 402 without the llm_insufficient_credits code stays generic too: the
// gateway may grow other payment errors whose remedy is different.
func kakiitUserFacingError(err error) string {
	var se *providerStatusError
	if !errors.As(err, &se) {
		return ""
	}
	if se.Provider != kakiitProvider {
		return ""
	}
	switch se.Code {
	case http.StatusPaymentRequired:
		if !strings.Contains(strings.ToLower(se.Body), kakiitInsufficientCreditsCode) {
			return ""
		}
		return KakiitTopUpMessage
	case http.StatusConflict:
		// A 409 is only the duplicate-id notice when the envelope says so; any
		// other conflict stays generic.
		if !strings.Contains(strings.ToLower(se.Body), kakiitDuplicateRequestIDCode) {
			return ""
		}
		return KakiitDuplicateRequestMessage
	default:
		return ""
	}
}

// UserFacingLLMError returns the one-line message a chat surface should show
// for an LLM error: the kakiit top-up notice when the classifier matches,
// otherwise the error's own text. The turn-failure surfaces (TUI chat line,
// web error banner via StepLimitHitDetail) wrap this with their own context.
func UserFacingLLMError(err error) string {
	if err == nil {
		return ""
	}
	if msg := kakiitUserFacingError(err); msg != "" {
		return msg
	}
	return err.Error()
}

// apiRoot returns the URL root the OpenAI/Anthropic-compatible transports
// append their endpoint suffixes to. kakiit's declared BaseURL is the gateway
// ROOT (…/api/llm) with every protocol endpoint under /v1 — unlike the
// api.openai.com/v1 convention where the providers-map baseURL already
// carries the version segment. One helper keeps all three transports
// (/chat/completions, /responses, /messages) consistent.
func (c *GenericClient) apiRoot() string {
	if c.isKakiitProvider() {
		return strings.TrimRight(c.BaseURL, "/") + "/v1"
	}
	return c.BaseURL
}

// kakiitTurnRequestID mints the id for one turn: a "req_"-prefixed UUIDv7
// (the gateway accepts any non-empty ≤64-char token; the prefix makes kakiit
// ids greppable in the admin usage log).
func kakiitTurnRequestID() string {
	return "req_" + newUUIDv7()
}

// newUUIDv7 returns an RFC 9562 UUIDv7 (48-bit unix-ms timestamp + 74 random
// bits). Implemented locally rather than promoting google/uuid from an
// indirect dependency: the format is a handful of lines and is pinned by
// tests (timestamp prefix, version/variant nibbles).
func newUUIDv7() string {
	var b [16]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		// crypto/rand failing is a broken runtime; keep a unique (if not
		// time-ordered) id rather than failing the turn over a trace header.
		t := time.Now().UnixNano()
		for i := 0; i < 16; i++ {
			b[i] = byte(t >> (8 * (i % 8)))
		}
	} else {
		ms := time.Now().UnixMilli()
		b[0] = byte(ms >> 40)
		b[1] = byte(ms >> 32)
		b[2] = byte(ms >> 24)
		b[3] = byte(ms >> 16)
		b[4] = byte(ms >> 8)
		b[5] = byte(ms)
		// version 7
		b[6] = (b[6] & 0x0f) | 0x70
		// variant 10xx
		b[8] = (b[8] & 0x3f) | 0x80
	}
	var buf [36]byte
	hex.Encode(buf[0:8], b[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], b[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], b[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], b[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], b[10:16])
	return string(buf[:])
}

// KakiitBaseURL returns the gateway base URL. Precedence: KAKIIT_BASE_URL env
// > ocode config `kakiit_base_url` > the per-credential override in auth.json
// (applied by NewClientWithProfile's standard override path) > the default.
// The env var mirrors LMSTUDIO_BASE_URL's role for local development.
func KakiitBaseURL() string {
	if v := strings.TrimSpace(os.Getenv("KAKIIT_BASE_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	if v := kakiitConfigBaseURL(); v != "" {
		return strings.TrimRight(v, "/")
	}
	return KakiitDefaultBaseURL
}

// kakiitConfigBaseURL reads the `kakiit_base_url` field of the ocode config.
// config.Load is the same read auth.ResolveKey performs per call; the kakiit
// URL is only resolved when a kakiit model is actually used, so this never
// runs per-request.
func kakiitConfigBaseURL() string {
	cfg, err := config.Load()
	if err != nil || cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Ocode.KakiitBaseURL)
}
