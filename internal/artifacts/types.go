package artifacts

import "time"

// Artifact is a published project snapshot container.
type Artifact struct {
	ID          string    `json:"id"`
	ProjectHost string    `json:"project_host"`
	ProjectPath string    `json:"project_path"`
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"created_at"`
	CreatedBy   string    `json:"created_by"`
	Status      string    `json:"status"`
	HostingMode string    `json:"hosting_mode"`
}

// Version is an immutable published version of an artifact.
type Version struct {
	ID               string    `json:"id"`
	ArtifactID       string    `json:"artifact_id"`
	Version          string    `json:"version"`
	Entrypoint       string    `json:"entrypoint"`
	BlobManifestHash string    `json:"blob_manifest_hash"`
	SizeBytes        int64     `json:"size_bytes"`
	CreatedAt        time.Time `json:"created_at"`
	BuildCmd         string    `json:"build_cmd"`
	BuildLogPath     string    `json:"build_log_path"`
	SchemaVersion    int       `json:"schema_version"`
}

// File is one file entry within a version, pointing at a content-addressed blob.
type File struct {
	VersionID string `json:"version_id"`
	Path      string `json:"path"`
	BlobHash  string `json:"blob_hash"`
	Size      int64  `json:"size"`
	Mode      string `json:"mode"`
	Mime      string `json:"mime"`
}

// AccessToken is a durable capability token bound to one artifact.
type AccessToken struct {
	ID           string    `json:"id"`
	ArtifactID   string    `json:"artifact_id"`
	TokenHash    string    `json:"token_hash"`
	Kind         string    `json:"kind"`
	ExpiresAt    time.Time `json:"expires_at"`
	CreatedAt    time.Time `json:"created_at"`
	RevokedAt    time.Time `json:"revoked_at"`
	PasswordHash string    `json:"password_hash"`
	Label        string    `json:"label"`
}

// AllowEntry is an allow-listed email for an artifact.
type AllowEntry struct {
	ArtifactID      string    `json:"artifact_id"`
	Email           string    `json:"email"`
	InvitedAt       time.Time `json:"invited_at"`
	AcceptedAt      time.Time `json:"accepted_at"`
	InviteTokenHash string    `json:"invite_token_hash"`
}

// AccessLogEntry records one visitor access.
type AccessLogEntry struct {
	ArtifactID string    `json:"artifact_id"`
	TokenID    string    `json:"token_id"`
	IP         string    `json:"ip"`
	UA         string    `json:"ua"`
	Path       string    `json:"path"`
	Status     int       `json:"status"`
	TS         time.Time `json:"ts"`
}
