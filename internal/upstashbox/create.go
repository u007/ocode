package upstashbox

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// Size values for CreateOptions.Size.
// documented: the SDK provider options list small, medium and large (not checked against source)
const (
	SizeSmall  = "small"
	SizeMedium = "medium"
	SizeLarge  = "large"
)

// Runtime values for CreateOptions.Runtime. documented: the set the
// EphemeralBox.create docs list (not checked against source).
const (
	RuntimeNode   = "node"
	RuntimePython = "python"
	RuntimeGolang = "golang"
	RuntimeRuby   = "ruby"
	RuntimeRust   = "rust"
)

// Network policy modes for NetworkPolicy.Mode.
// verified against @upstash/box 0.8.1 types.ts on 2026-11-21
const (
	NetworkAllowAll = "allow-all"
	NetworkDenyAll  = "deny-all"
	NetworkCustom   = "custom"
)

// MaxTTLSeconds is the longest lifetime an ephemeral box accepts (3 days).
// documented: EphemeralBox.create docs (not checked against source)
const MaxTTLSeconds = 259200

// NetworkPolicy restricts a box's outbound traffic. The allow-all and deny-all
// modes take no lists. The custom mode needs at least one entry in a list.
type NetworkPolicy struct {
	Mode           string
	AllowedDomains []string
	AllowedCIDRs   []string
	DeniedCIDRs    []string
}

// CreateOptions describes a box to create. Options are validated before any
// request is sent.
type CreateOptions struct {
	Name string
	// Size is SizeSmall, SizeMedium or SizeLarge. Empty leaves the choice to the server.
	Size string
	// Runtime is one of the Runtime constants. Empty leaves the choice to the server.
	Runtime string
	Browser bool
	// Ephemeral boxes expire, cannot be paused, and are ready immediately.
	Ephemeral bool
	// TTLSeconds needs Ephemeral and is at most MaxTTLSeconds. Zero leaves the server default.
	TTLSeconds    int
	Labels        []string
	NetworkPolicy *NetworkPolicy
}

// createRequest is the JSON body of POST /v2/box. Keys are snake_case, as the
// @upstash/box 0.8.1 SDK sends them. Empty values are omitted.
// verified against @upstash/box 0.8.1 source on 2026-11-21
type createRequest struct {
	Name          string             `json:"name,omitempty"`
	Labels        []string           `json:"labels,omitempty"`
	Size          string             `json:"size,omitempty"`
	Runtime       string             `json:"runtime,omitempty"`
	Browser       bool               `json:"browser,omitempty"`
	Ephemeral     bool               `json:"ephemeral,omitempty"`
	TTL           int                `json:"ttl,omitempty"`
	NetworkPolicy *wireNetworkPolicy `json:"network_policy,omitempty"`
}

// wireNetworkPolicy is the network_policy object in both directions. A
// non-custom policy sends only mode; a custom one also sends its lists.
// verified against @upstash/box 0.8.1 source on 2026-11-21
type wireNetworkPolicy struct {
	Mode           string   `json:"mode"`
	AllowedDomains []string `json:"allowed_domains,omitempty"`
	AllowedCIDRs   []string `json:"allowed_cidrs,omitempty"`
	DeniedCIDRs    []string `json:"denied_cidrs,omitempty"`
}

// Create creates a box and returns it once it is no longer "creating". The
// API may answer "creating"; Create then polls GET /v2/box/{id} with an
// increasing delay, for at most createWaitBudget (300 s). A transient poll
// failure (a transport error or a 5xx) is retried on the same schedule. A 4xx
// poll answer ends the poll at once.
//
// Once the POST has succeeded, the box exists. When err != nil and the returned
// box is non-nil, the box exists and the caller must delete it. The box then
// carries its ID and the last status Create saw. Create does not delete the box
// itself. Only a failed POST, or a POST answer with no id, returns a nil box.
func (c *Client) Create(ctx context.Context, opts CreateOptions) (*Box, error) {
	body, err := opts.wire()
	if err != nil {
		return nil, err
	}
	var wire wireBox
	if err := c.do(ctx, http.MethodPost, pathBoxCreate, nil, body, &wire); err != nil {
		return nil, err
	}
	b, err := wire.toBox()
	if err != nil {
		err = fmt.Errorf("upstashbox: create box: %w", err)
		if wire.ID == nil || *wire.ID == "" {
			return nil, fmt.Errorf("%w: the response has no id, so the new box cannot be identified", err)
		}
		partial := &Box{ID: *wire.ID}
		if wire.Status != nil {
			partial.Status = *wire.Status
		}
		return partial, err
	}
	return c.awaitReady(ctx, &b)
}

// awaitReady polls a box that is still creating until it is no longer creating.
// The wait starts at pollInitial and doubles up to pollMax. The poll stops when
// the context ends or when createWait has elapsed. It always returns the last
// box it learned about, so a caller that gets an error still has the ID.
func (c *Client) awaitReady(ctx context.Context, b *Box) (*Box, error) {
	if b.Status != statusCreating {
		return b, nil
	}
	deadline := time.Now().Add(c.createWait)
	delay := c.pollInitial
	last := b
	var pollErr error
	for {
		if err := ctx.Err(); err != nil {
			return last, fmt.Errorf("upstashbox: create box %s: waiting for ready: %w", last.ID, err)
		}
		if !time.Now().Before(deadline) {
			if pollErr != nil {
				return last, fmt.Errorf("upstashbox: create box %s: still creating after %s, last poll error: %w", last.ID, c.createWait, pollErr)
			}
			return last, fmt.Errorf("upstashbox: create box %s: still creating after %s", last.ID, c.createWait)
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return last, fmt.Errorf("upstashbox: create box %s: waiting for ready: %w", last.ID, ctx.Err())
		case <-timer.C:
		}
		next, err := c.Get(ctx, last.ID)
		switch {
		case err == nil:
			if next.Status != statusCreating {
				return next, nil
			}
			last = next
			pollErr = nil
		case isTransientPollError(err):
			pollErr = err
		default:
			return last, fmt.Errorf("upstashbox: create box %s: waiting for ready: %w", last.ID, err)
		}
		delay = min(delay*2, c.pollMax)
	}
}

// isTransientPollError reports whether a poll failure may clear on its own: a
// round trip with no response, or a 5xx answer.
func isTransientPollError(err error) bool {
	var ne *netError
	if errors.As(err, &ne) {
		return true
	}
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode >= 500
}

// wire validates the options and builds the request body.
func (o CreateOptions) wire() (createRequest, error) {
	switch o.Size {
	case "", SizeSmall, SizeMedium, SizeLarge:
	default:
		return createRequest{}, fmt.Errorf("upstashbox: unknown size %q (want %q, %q or %q)", o.Size, SizeSmall, SizeMedium, SizeLarge)
	}
	switch o.Runtime {
	case "", RuntimeNode, RuntimePython, RuntimeGolang, RuntimeRuby, RuntimeRust:
	default:
		return createRequest{}, fmt.Errorf("upstashbox: unknown runtime %q", o.Runtime)
	}
	if o.TTLSeconds != 0 {
		if !o.Ephemeral {
			return createRequest{}, errors.New("upstashbox: TTLSeconds requires Ephemeral")
		}
		if o.TTLSeconds < 0 || o.TTLSeconds > MaxTTLSeconds {
			return createRequest{}, fmt.Errorf("upstashbox: TTLSeconds must be between 1 and %d", MaxTTLSeconds)
		}
	}
	for _, label := range o.Labels {
		if label == "" || label != strings.TrimSpace(label) {
			return createRequest{}, fmt.Errorf("upstashbox: label %q must be non-empty with no surrounding space", label)
		}
	}
	policy, err := o.NetworkPolicy.wire()
	if err != nil {
		return createRequest{}, err
	}
	return createRequest{
		Name:          o.Name,
		Labels:        o.Labels,
		Size:          o.Size,
		Runtime:       o.Runtime,
		Browser:       o.Browser,
		Ephemeral:     o.Ephemeral,
		TTL:           o.TTLSeconds,
		NetworkPolicy: policy,
	}, nil
}

// wire validates a policy and builds its request form. A nil policy yields nil.
func (p *NetworkPolicy) wire() (*wireNetworkPolicy, error) {
	if p == nil {
		return nil, nil
	}
	hasLists := len(p.AllowedDomains) > 0 || len(p.AllowedCIDRs) > 0 || len(p.DeniedCIDRs) > 0
	switch p.Mode {
	case NetworkAllowAll, NetworkDenyAll:
		if hasLists {
			return nil, fmt.Errorf("upstashbox: network mode %q takes no domains or CIDRs", p.Mode)
		}
		return &wireNetworkPolicy{Mode: p.Mode}, nil
	case NetworkCustom:
		if !hasLists {
			return nil, errors.New("upstashbox: custom network policy needs at least one domain or CIDR")
		}
	default:
		return nil, fmt.Errorf("upstashbox: unknown network mode %q (want %q, %q or %q)", p.Mode, NetworkAllowAll, NetworkDenyAll, NetworkCustom)
	}
	for _, d := range p.AllowedDomains {
		if d == "" || strings.ContainsAny(d, " \t/:") {
			return nil, fmt.Errorf("upstashbox: allowed domain %q must be a bare host name", d)
		}
	}
	for _, list := range [][]string{p.AllowedCIDRs, p.DeniedCIDRs} {
		for _, cidr := range list {
			if _, _, err := net.ParseCIDR(cidr); err != nil {
				return nil, fmt.Errorf("upstashbox: %q is not a valid CIDR", cidr)
			}
		}
	}
	return &wireNetworkPolicy{
		Mode:           p.Mode,
		AllowedDomains: p.AllowedDomains,
		AllowedCIDRs:   p.AllowedCIDRs,
		DeniedCIDRs:    p.DeniedCIDRs,
	}, nil
}
