package upstashbox

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// CDPURL is the authenticated Chrome DevTools Protocol WebSocket URL of a box
// browser, shaped wss://<box-id>-9223.preview.box.upstash.com?token=<secret>.
// The token grants full control of the browser. String and GoString redact it,
// and the raw URL leaves only through RevealRaw. Pass the value itself: a
// CDPURL stored in an unexported struct field is printed by its fields.
type CDPURL struct {
	scheme string
	host   string
	raw    string
}

// String returns the URL with the token redacted.
func (u CDPURL) String() string {
	if u.raw == "" {
		return "upstashbox.CDPURL(empty)"
	}
	return fmt.Sprintf("%s://%s/?token=%s", u.scheme, u.host, redacted)
}

// GoString returns the same redacted form as String, so %#v is safe too.
func (u CDPURL) GoString() string {
	return "upstashbox.CDPURL(" + u.String() + ")"
}

// RevealRaw returns the full URL, token included. Use it only to hand the URL
// to a CDP client, and never log or display the result.
func (u CDPURL) RevealRaw() string {
	return u.raw
}

// CDPURL connects to a box's browser and returns its CDP URL. The call uses a
// 60 s timeout because Chromium starts on first use.
// verified against @upstash/box 0.8.1 source on 2026-11-21
func (c *Client) CDPURL(ctx context.Context, id string) (CDPURL, error) {
	if err := validBoxID(id); err != nil {
		return CDPURL{}, err
	}
	var wire struct {
		CDPURL *string `json:"cdp_url"`
	}
	path := fmt.Sprintf(pathBoxConnect, url.PathEscape(id))
	if err := c.doWithin(ctx, connectCallTimeout, http.MethodPost, path, nil, nil, &wire); err != nil {
		return CDPURL{}, err
	}
	if wire.CDPURL == nil || *wire.CDPURL == "" {
		return CDPURL{}, errors.New("upstashbox: browser connect response has no cdp_url")
	}
	return parseCDPURL(*wire.CDPURL)
}

// parseCDPURL validates a CDP URL. Errors never echo raw, because it carries
// the token and url.Parse's own error would repeat it.
func parseCDPURL(raw string) (CDPURL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return CDPURL{}, errors.New("upstashbox: CDP URL is not a valid URL")
	}
	if u.Scheme != "wss" {
		return CDPURL{}, fmt.Errorf("upstashbox: CDP URL scheme is %q, want \"wss\"", u.Scheme)
	}
	if u.Host == "" {
		return CDPURL{}, errors.New("upstashbox: CDP URL has no host")
	}
	if u.Query().Get("token") == "" {
		return CDPURL{}, errors.New("upstashbox: CDP URL carries no token")
	}
	return CDPURL{scheme: u.Scheme, host: u.Host, raw: raw}, nil
}
