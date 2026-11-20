package upstashbox

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"
)

// statusCreating is the status of a box that is still being provisioned.
// verified against @upstash/box 0.8.1 source on 2026-11-21
const statusCreating = "creating"

// Box is one Upstash Box as the API reports it.
type Box struct {
	ID        string
	Name      string
	Labels    []string
	Status    string
	Size      string
	Runtime   string
	StorageGB int
	Browser   bool
	Ephemeral bool
	// KeepAlive is false when the API omits keep_alive.
	KeepAlive bool
	// NetworkPolicy is nil when the API reports none.
	NetworkPolicy *NetworkPolicy
	// ExpiresAt is zero for a box that does not expire.
	ExpiresAt time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// wireBox mirrors one JSON box object. It follows the SDK's BoxData type
// (verified against @upstash/box 0.8.1 types.ts on 2026-11-21).
//
// Always present, so pointers: id, status, created_at, updated_at. A missing
// one is an error.
//
// Optional in the SDK type. An absent key takes the meaning the API gives it:
// name, labels, size, runtime and storage_gb are empty or zero, browser,
// ephemeral and keep_alive are false, and network_policy is nil. This is the
// API's own meaning of an absent key, not a silent fallback, because the SDK
// type says the key may be missing.
//
// ephemeral and expires_at are part of the SDK's EphemeralBoxData type, not
// BoxData. expires_at is required only for an ephemeral box that is not
// still creating.
type wireBox struct {
	ID            *string            `json:"id"`
	Name          string             `json:"name"`
	Labels        []string           `json:"labels"`
	Runtime       string             `json:"runtime"`
	StorageGB     int                `json:"storage_gb"`
	Status        *string            `json:"status"`
	Size          string             `json:"size"`
	Browser       bool               `json:"browser"`
	Ephemeral     bool               `json:"ephemeral"`
	KeepAlive     bool               `json:"keep_alive"`
	NetworkPolicy *wireNetworkPolicy `json:"network_policy"`
	ExpiresAt     *int64             `json:"expires_at"`
	CreatedAt     *int64             `json:"created_at"`
	UpdatedAt     *int64             `json:"updated_at"`
}

// toBox validates a response object and converts it.
func (w wireBox) toBox() (Box, error) {
	switch {
	case w.ID == nil || *w.ID == "":
		return Box{}, badBoxField("id")
	case w.Status == nil || *w.Status == "":
		return Box{}, badBoxField("status")
	case w.CreatedAt == nil || *w.CreatedAt <= 0:
		return Box{}, badBoxField("created_at")
	case w.UpdatedAt == nil || *w.UpdatedAt <= 0:
		return Box{}, badBoxField("updated_at")
	case w.ExpiresAt != nil && *w.ExpiresAt < 0:
		return Box{}, badBoxField("expires_at")
	case w.Ephemeral && *w.Status != statusCreating && (w.ExpiresAt == nil || *w.ExpiresAt <= 0):
		return Box{}, badBoxField("expires_at")
	case w.NetworkPolicy != nil && w.NetworkPolicy.Mode == "":
		return Box{}, badBoxField("network_policy.mode")
	}

	b := Box{
		ID:        *w.ID,
		Name:      w.Name,
		Labels:    w.Labels,
		Status:    *w.Status,
		Size:      w.Size,
		Runtime:   w.Runtime,
		StorageGB: w.StorageGB,
		Browser:   w.Browser,
		Ephemeral: w.Ephemeral,
		KeepAlive: w.KeepAlive,
		CreatedAt: time.Unix(*w.CreatedAt, 0).UTC(),
		UpdatedAt: time.Unix(*w.UpdatedAt, 0).UTC(),
	}
	if w.ExpiresAt != nil && *w.ExpiresAt > 0 {
		b.ExpiresAt = time.Unix(*w.ExpiresAt, 0).UTC()
	}
	if w.NetworkPolicy != nil {
		b.NetworkPolicy = &NetworkPolicy{
			Mode:           w.NetworkPolicy.Mode,
			AllowedDomains: w.NetworkPolicy.AllowedDomains,
			AllowedCIDRs:   w.NetworkPolicy.AllowedCIDRs,
			DeniedCIDRs:    w.NetworkPolicy.DeniedCIDRs,
		}
	}
	return b, nil
}

func badBoxField(field string) error {
	return fmt.Errorf("upstashbox: box response has no valid %q field", field)
}

// validBoxID rejects ids that would change the request path once escaped.
func validBoxID(id string) error {
	if id == "" {
		return errors.New("upstashbox: box id is required")
	}
	if strings.ContainsFunc(id, func(r rune) bool {
		return r == '/' || r == '?' || r == '#' || unicode.IsSpace(r) || unicode.IsControl(r)
	}) {
		return errors.New("upstashbox: box id contains a character that is not allowed in a path")
	}
	return nil
}

// compareNewestFirst orders by CreatedAt descending, then ID ascending.
func compareNewestFirst(a, b Box) int {
	return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), strings.Compare(a.ID, b.ID))
}

// List returns the boxes sorted by CreatedAt descending, then ID. A non-empty
// label keeps only boxes carrying that label. No paging parameters exist in the
// @upstash/box 0.8.1 SDK, so List returns every box the API returns.
func (c *Client) List(ctx context.Context, label string) ([]Box, error) {
	var query url.Values
	if label != "" {
		query = url.Values{queryLabel: {label}}
	}
	var wire []wireBox
	if err := c.do(ctx, http.MethodGet, pathBoxList, query, nil, &wire); err != nil {
		return nil, err
	}
	boxes := make([]Box, 0, len(wire))
	for i, w := range wire {
		b, err := w.toBox()
		if err != nil {
			return nil, fmt.Errorf("upstashbox: list boxes: item %d: %w", i, err)
		}
		if label != "" && !slices.Contains(b.Labels, label) {
			continue
		}
		boxes = append(boxes, b)
	}
	slices.SortFunc(boxes, compareNewestFirst)
	return boxes, nil
}

// Get returns one box by id.
func (c *Client) Get(ctx context.Context, id string) (*Box, error) {
	if err := validBoxID(id); err != nil {
		return nil, err
	}
	var wire wireBox
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf(pathBoxItem, url.PathEscape(id)), nil, nil, &wire); err != nil {
		return nil, err
	}
	b, err := wire.toBox()
	if err != nil {
		return nil, fmt.Errorf("upstashbox: get box: %w", err)
	}
	if b.ID != id {
		return nil, errors.New("upstashbox: get box: response describes a different box")
	}
	return &b, nil
}

// Delete removes a box.
func (c *Client) Delete(ctx context.Context, id string) error {
	if err := validBoxID(id); err != nil {
		return err
	}
	return c.do(ctx, http.MethodDelete, fmt.Sprintf(pathBoxItem, url.PathEscape(id)), nil, nil, nil)
}

// Pause pauses a box. Ephemeral boxes cannot be paused: the API answers 400,
// which surfaces as an *APIError.
func (c *Client) Pause(ctx context.Context, id string) error {
	if err := validBoxID(id); err != nil {
		return err
	}
	return c.do(ctx, http.MethodPost, fmt.Sprintf(pathBoxPause, url.PathEscape(id)), nil, nil, nil)
}

// Resume resumes a paused box.
func (c *Client) Resume(ctx context.Context, id string) error {
	if err := validBoxID(id); err != nil {
		return err
	}
	return c.do(ctx, http.MethodPost, fmt.Sprintf(pathBoxResume, url.PathEscape(id)), nil, nil, nil)
}
