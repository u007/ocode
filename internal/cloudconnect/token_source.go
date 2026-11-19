package cloudconnect

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/u007/ocode/internal/cloudhub"
	"github.com/u007/ocode/internal/cloudkeystore"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/encryption"
)

// Credentials is one provider's local credential bundle inside the v2
// credentials envelope. RefreshToken is present only for Mode:"local" — hub
// mode connections never carry one.
type Credentials struct {
	AccessToken string `json:"access_token,omitempty"`
	// RefreshToken is present only for Mode:"local" — hub mode never has one.
	RefreshToken       string    `json:"refresh_token,omitempty"`
	ClientID           string    `json:"client_id,omitempty"`
	ClientSecret       string    `json:"client_secret,omitempty"`
	ServiceAccountJSON string    `json:"service_account_json,omitempty"`
	AccessKey          string    `json:"access_key,omitempty"`
	SecretKey          string    `json:"secret_key,omitempty"`
	Region             string    `json:"region,omitempty"`
	Endpoint           string    `json:"endpoint,omitempty"`
	ExpiresAt          time.Time `json:"expires_at,omitempty"`
}

// credentialsFromSecretBundle folds a legacy secret bundle into the v2 local
// credentials envelope without dropping S3/GCS static keys. AccessKey is kept
// in both fields: it was the OAuth access token for OAuth providers and the
// S3 access key for static-key providers.
func credentialsFromSecretBundle(sec SecretBundle) Credentials {
	return Credentials{
		AccessToken:        sec.AccessKey,
		RefreshToken:       sec.RefreshToken,
		ClientID:           sec.ClientID,
		ClientSecret:       sec.ClientSecret,
		ServiceAccountJSON: sec.ServiceAccountJSON,
		AccessKey:          sec.AccessKey,
		SecretKey:          sec.SecretKey,
		Region:             sec.Region,
		Endpoint:           sec.Endpoint,
	}
}

// hubRefresher is the narrow seam the hub token source needs from cloudhub.
// *cloudhub.Client satisfies it; tests inject a counting mock to prove
// single-flight without a real hub.
type hubRefresher interface {
	RefreshToken(ctx context.Context, connectionID string) (string, time.Time, error)
}

// refreshFlight is one in-progress hub refresh, shared by every concurrent
// Token() call for the same connection.
type refreshFlight struct {
	done      chan struct{}
	access    string
	expiresAt time.Time
	err       error
}

// HubTokenSource resolves tokens through kakiit. It reads provider ->
// connection_id from the local v2 file and single-flights refreshes per
// connection so concurrent browse/read calls share one hub round-trip.
type HubTokenSource struct {
	client      hubRefresher
	projectRoot string
	mu          sync.Mutex
	flights     map[string]*refreshFlight
}

// newHubTokenSource is the test seam; it accepts the narrow refresher so
// single-flight can be observed without a network hub.
func newHubTokenSource(client hubRefresher, projectRoot string) *HubTokenSource {
	return &HubTokenSource{client: client, projectRoot: projectRoot, flights: map[string]*refreshFlight{}}
}

// NewHubTokenSource builds a hub-backed TokenSource for a project root.
func NewHubTokenSource(client *cloudhub.Client, projectRoot string) *HubTokenSource {
	return newHubTokenSource(client, projectRoot)
}

// Token implements TokenSource. It looks up the central connection id for the
// provider in the local v2 file and refreshes through the hub, coalescing
// concurrent refreshes for the same connection.
func (s *HubTokenSource) Token(ctx context.Context, provider string) (string, time.Time, error) {
	f, err := config.LoadV2(s.projectRoot)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("cloudconnect: load v2 config: %w", err)
	}
	meta, ok := f.Central.Connections[provider]
	if !ok || meta.ConnectionID == "" {
		return "", time.Time{}, fmt.Errorf("cloudconnect: no central connection for provider %q", provider)
	}
	return s.refresh(ctx, meta.ConnectionID)
}

func (s *HubTokenSource) refresh(ctx context.Context, connectionID string) (string, time.Time, error) {
	s.mu.Lock()
	if flight, ok := s.flights[connectionID]; ok {
		s.mu.Unlock()
		select {
		case <-flight.done:
			return flight.access, flight.expiresAt, flight.err
		case <-ctx.Done():
			return "", time.Time{}, ctx.Err()
		}
	}
	flight := &refreshFlight{done: make(chan struct{})}
	s.flights[connectionID] = flight
	s.mu.Unlock()
	defer func() {
		close(flight.done)
		s.mu.Lock()
		delete(s.flights, connectionID)
		s.mu.Unlock()
	}()

	// The flight is shared: one caller's cancellation must not cancel the
	// refresh every other waiter depends on. WithoutCancel also means a
	// leader that goes away still completes the in-flight refresh.
	flight.access, flight.expiresAt, flight.err = s.client.RefreshToken(context.WithoutCancel(ctx), connectionID)
	return flight.access, flight.expiresAt, flight.err
}

// LegacyTokenSource reads the access token and expiry from the local v2
// credentials envelope. Local-mode provider refresh stays with the existing
// provider adapters; this source never contacts the hub.
type LegacyTokenSource struct {
	projectRoot string
	key         []byte
}

// NewLegacyTokenSource builds a local-mode TokenSource. If key is nil the
// install key is resolved through cloudkeystore.Resolve.
func NewLegacyTokenSource(projectRoot string, key []byte) *LegacyTokenSource {
	return &LegacyTokenSource{projectRoot: projectRoot, key: key}
}

// Token implements TokenSource for local-mode connections.
func (s *LegacyTokenSource) Token(_ context.Context, provider string) (string, time.Time, error) {
	key := s.key
	if len(key) == 0 {
		resolved, _, err := cloudkeystore.Resolve()
		if err != nil {
			return "", time.Time{}, fmt.Errorf("cloudconnect: resolve key: %w", err)
		}
		key = resolved
	}
	f, err := config.LoadV2(s.projectRoot)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("cloudconnect: load v2 config: %w", err)
	}
	plain, err := encryption.OpenWithKey(f.CredentialsEnvelope, key)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("cloudconnect: open credentials envelope: %w", err)
	}
	var creds map[string]Credentials
	if err := json.Unmarshal(plain, &creds); err != nil {
		return "", time.Time{}, fmt.Errorf("cloudconnect: decode credentials: %w", err)
	}
	c, ok := creds[provider]
	if !ok {
		return "", time.Time{}, fmt.Errorf("cloudconnect: no local credentials for provider %q", provider)
	}
	return c.AccessToken, c.ExpiresAt, nil
}
