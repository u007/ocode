// Package cloudconnect provides read-only connectors to cloud storage providers
// (Google Drive, OneDrive, GCS, S3). Phase 1 ships the connector framework plus
// a Google Drive implementation; later phases add providers and write support.
//
// Secrets (OAuth refresh tokens, service-account keys) are stored encrypted on
// disk via internal/encryption, keyed by a user master password. Metadata (provider
// id, root folder, cache limits) lives in internal/config storage_connectors.json.
package cloudconnect

import (
	"context"
	"io"
	"time"
)

// Entry is a single file or folder in a cloud tree.
type Entry struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Path     string    `json:"path"` // virtual path within the connector ("" = root)
	IsDir    bool      `json:"is_dir"`
	Size     int64     `json:"size"`
	MimeType string    `json:"mime_type"`
	Modified time.Time `json:"modified"`
}

// Connector is the read-only interface every cloud provider implements.
type Connector interface {
	// Provider returns the provider key (e.g. "google-drive").
	Provider() string
	// ID returns the connector instance id (key in settings).
	ID() string
	// List returns the children of the given virtual path ("" = root).
	List(ctx context.Context, path string) ([]Entry, error)
	// Read downloads the content of a file at the given virtual path.
	Read(ctx context.Context, path string) (io.ReadCloser, error)
	// Metadata returns metadata for a single entry.
	Metadata(ctx context.Context, path string) (*Entry, error)
	// Write uploads content to the given virtual path, creating or overwriting.
	Write(ctx context.Context, path string, r io.Reader, size int64) error
	// Delete removes the entry at the given virtual path.
	Delete(ctx context.Context, path string) error
}

// Config is the non-secret metadata for a connector instance, mirroring
// config.ConnectorSettings. Secrets are resolved separately by the manager.
type Config struct {
	ID            string `json:"id"`
	Provider      string `json:"provider"`
	RootPath      string `json:"root_path"`
	ProjectSlug   string `json:"project_slug"`
	MaxCacheBytes int64  `json:"max_cache_bytes"`
}

// SecretBundle holds the encrypted-at-rest secret material for a connector.
// It is stored separately from Config and never serialized to settings JSON.
type SecretBundle struct {
	// OAuth refresh-token flow (Google Drive, OneDrive).
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	// Service-account / static key flow (GCS, S3).
	ServiceAccountJSON string `json:"service_account_json,omitempty"`
	AccessKey          string `json:"access_key,omitempty"`
	SecretKey          string `json:"secret_key,omitempty"`
	Region             string `json:"region,omitempty"`
	Endpoint           string `json:"endpoint,omitempty"`
}
