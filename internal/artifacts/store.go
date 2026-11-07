package artifacts

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	sqlite "modernc.org/sqlite"
)

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("artifacts: not found")

type Store struct {
	mu    sync.Mutex
	dir   string
	db    *sql.DB
	blobs *BlobStore
}

func openDB(path string) (*sql.DB, error) {
	escaped := strings.ReplaceAll(path, "%", "%25")
	escaped = strings.ReplaceAll(escaped, "?", "%3F")
	escaped = strings.ReplaceAll(escaped, "#", "%23")
	dsn := fmt.Sprintf("file:%s?_txlock=immediate&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", escaped)
	return sql.Open("sqlite", dsn)
}

// NewStore opens (or creates) an artifact store rooted at dir.
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("artifacts: create dir: %w", err)
	}
	db, err := openDB(filepath.Join(dir, "artifacts.db"))
	if err != nil {
		return nil, fmt.Errorf("artifacts: open db: %w", err)
	}
	bs, err := NewBlobStore(dir)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Store{dir: dir, db: db, blobs: bs}, nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// Migrate creates the schema if needed.
func (s *Store) Migrate() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("artifacts: begin migrate: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS artifacts (
			id TEXT PRIMARY KEY,
			project_host TEXT NOT NULL DEFAULT '',
			project_path TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			created_by TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '',
			hosting_mode TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS artifact_versions (
			id TEXT PRIMARY KEY,
			artifact_id TEXT NOT NULL,
			version TEXT NOT NULL DEFAULT '',
			entrypoint TEXT NOT NULL DEFAULT '',
			blob_manifest_hash TEXT NOT NULL DEFAULT '',
			size_bytes INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			build_cmd TEXT NOT NULL DEFAULT '',
			build_log_path TEXT NOT NULL DEFAULT '',
			schema_version INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY (artifact_id) REFERENCES artifacts(id)
		)`,
		`CREATE TABLE IF NOT EXISTS artifact_files (
			version_id TEXT NOT NULL,
			path TEXT NOT NULL,
			blob_hash TEXT NOT NULL,
			size INTEGER NOT NULL DEFAULT 0,
			mode TEXT NOT NULL DEFAULT '',
			mime TEXT NOT NULL DEFAULT '',
			FOREIGN KEY (version_id) REFERENCES artifact_versions(id),
			PRIMARY KEY (version_id, path)
		)`,
		`CREATE TABLE IF NOT EXISTS access_tokens (
			id TEXT PRIMARY KEY,
			artifact_id TEXT NOT NULL,
			token_hash TEXT NOT NULL,
			kind TEXT NOT NULL DEFAULT '',
			expires_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			revoked_at TEXT NOT NULL DEFAULT '',
			password_hash TEXT NOT NULL DEFAULT '',
			label TEXT NOT NULL DEFAULT '',
			FOREIGN KEY (artifact_id) REFERENCES artifacts(id)
		)`,
		`CREATE TABLE IF NOT EXISTS allowlist (
			artifact_id TEXT NOT NULL,
			email TEXT NOT NULL,
			invited_at TEXT NOT NULL,
			accepted_at TEXT NOT NULL DEFAULT '',
			invite_token_hash TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (artifact_id, email),
			FOREIGN KEY (artifact_id) REFERENCES artifacts(id)
		)`,
		`CREATE TABLE IF NOT EXISTS access_log (
			artifact_id TEXT NOT NULL,
			token_id TEXT NOT NULL DEFAULT '',
			ip TEXT NOT NULL DEFAULT '',
			ua TEXT NOT NULL DEFAULT '',
			path TEXT NOT NULL DEFAULT '',
			status INTEGER NOT NULL DEFAULT 0,
			ts TEXT NOT NULL
		)`,
	}
	for _, st := range stmts {
		if _, err := tx.Exec(st); err != nil {
			return fmt.Errorf("artifacts: migrate exec: %w", err)
		}
	}
	return tx.Commit()
}

func newID(prefix string) string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		// try RFC3339
		t2, err2 := time.Parse(time.RFC3339, s)
		if err2 != nil {
			return time.Time{}
		}
		return t2
	}
	return t
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// CreateArtifact inserts a new artifact. ID generated if empty.
func (s *Store) CreateArtifact(a Artifact) (Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a.ID == "" {
		a.ID = newID("art_")
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.Exec(
		`INSERT INTO artifacts (id, project_host, project_path, name, created_at, created_by, status, hosting_mode) VALUES (?,?,?,?,?,?,?,?)`,
		a.ID, a.ProjectHost, a.ProjectPath, a.Name, fmtTime(a.CreatedAt), a.CreatedBy, a.Status, a.HostingMode,
	)
	if err != nil {
		if isConstraintErr(err) {
			return Artifact{}, fmt.Errorf("artifacts: create artifact duplicate id: %w", err)
		}
		return Artifact{}, fmt.Errorf("artifacts: create artifact: %w", err)
	}
	return a, nil
}

func isConstraintErr(err error) bool {
	var e *sqlite.Error
	if !errors.As(err, &e) {
		return false
	}
	return e.Code()&0xff == 19
}

// GetArtifact fetches an artifact by id.
func (s *Store) GetArtifact(id string) (Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.db.QueryRow(`SELECT id, project_host, project_path, name, created_at, created_by, status, hosting_mode FROM artifacts WHERE id = ?`, id)
	var a Artifact
	var created string
	err := row.Scan(&a.ID, &a.ProjectHost, &a.ProjectPath, &a.Name, &created, &a.CreatedBy, &a.Status, &a.HostingMode)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Artifact{}, ErrNotFound
		}
		return Artifact{}, fmt.Errorf("artifacts: get artifact: %w", err)
	}
	a.CreatedAt = parseTime(created)
	return a, nil
}

// ListArtifacts returns artifacts paginated and sorted (newest first).
func (s *Store) ListArtifacts(limit, offset int) ([]Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.Query(`SELECT id, project_host, project_path, name, created_at, created_by, status, hosting_mode FROM artifacts ORDER BY created_at DESC, id ASC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("artifacts: list artifacts: %w", err)
	}
	defer rows.Close()
	var out []Artifact
	for rows.Next() {
		var a Artifact
		var created string
		if err := rows.Scan(&a.ID, &a.ProjectHost, &a.ProjectPath, &a.Name, &created, &a.CreatedBy, &a.Status, &a.HostingMode); err != nil {
			return nil, err
		}
		a.CreatedAt = parseTime(created)
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []Artifact{}
	}
	return out, nil
}

// PublishVersion inserts a version and its files in one transaction.
func (s *Store) PublishVersion(v Version, files []File) (Version, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v.ID == "" {
		v.ID = newID("ver_")
	}
	if v.CreatedAt.IsZero() {
		v.CreatedAt = time.Now().UTC()
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Version{}, fmt.Errorf("artifacts: begin publish: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	// verify artifact exists
	var exists int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM artifacts WHERE id = ?`, v.ArtifactID).Scan(&exists); err != nil {
		return Version{}, err
	}
	if exists == 0 {
		return Version{}, ErrNotFound
	}
	_, err = tx.Exec(
		`INSERT INTO artifact_versions (id, artifact_id, version, entrypoint, blob_manifest_hash, size_bytes, created_at, build_cmd, build_log_path, schema_version) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		v.ID, v.ArtifactID, v.Version, v.Entrypoint, v.BlobManifestHash, v.SizeBytes, fmtTime(v.CreatedAt), v.BuildCmd, v.BuildLogPath, v.SchemaVersion,
	)
	if err != nil {
		return Version{}, fmt.Errorf("artifacts: insert version: %w", err)
	}
	for i := range files {
		files[i].VersionID = v.ID
		_, err = tx.Exec(
			`INSERT INTO artifact_files (version_id, path, blob_hash, size, mode, mime) VALUES (?,?,?,?,?,?)`,
			files[i].VersionID, files[i].Path, files[i].BlobHash, files[i].Size, files[i].Mode, files[i].Mime,
		)
		if err != nil {
			return Version{}, fmt.Errorf("artifacts: insert file: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Version{}, err
	}
	return v, nil
}

// GetVersion fetches a version by id.
func (s *Store) GetVersion(id string) (Version, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.db.QueryRow(`SELECT id, artifact_id, version, entrypoint, blob_manifest_hash, size_bytes, created_at, build_cmd, build_log_path, schema_version FROM artifact_versions WHERE id = ?`, id)
	var v Version
	var created string
	err := row.Scan(&v.ID, &v.ArtifactID, &v.Version, &v.Entrypoint, &v.BlobManifestHash, &v.SizeBytes, &created, &v.BuildCmd, &v.BuildLogPath, &v.SchemaVersion)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Version{}, ErrNotFound
		}
		return Version{}, fmt.Errorf("artifacts: get version: %w", err)
	}
	v.CreatedAt = parseTime(created)
	return v, nil
}

// ListVersions returns versions for an artifact sorted oldest first.
func (s *Store) ListVersions(artifactID string) ([]Version, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id, artifact_id, version, entrypoint, blob_manifest_hash, size_bytes, created_at, build_cmd, build_log_path, schema_version FROM artifact_versions WHERE artifact_id = ? ORDER BY created_at ASC, id ASC`, artifactID)
	if err != nil {
		return nil, fmt.Errorf("artifacts: list versions: %w", err)
	}
	defer rows.Close()
	var out []Version
	for rows.Next() {
		var v Version
		var created string
		if err := rows.Scan(&v.ID, &v.ArtifactID, &v.Version, &v.Entrypoint, &v.BlobManifestHash, &v.SizeBytes, &created, &v.BuildCmd, &v.BuildLogPath, &v.SchemaVersion); err != nil {
			return nil, err
		}
		v.CreatedAt = parseTime(created)
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []Version{}
	}
	return out, nil
}

// MintToken inserts a new access token.
func (s *Store) MintToken(t AccessToken) (AccessToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t.ID == "" {
		t.ID = newID("tok_")
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.Exec(
		`INSERT INTO access_tokens (id, artifact_id, token_hash, kind, expires_at, created_at, revoked_at, password_hash, label) VALUES (?,?,?,?,?,?,?,?,?)`,
		t.ID, t.ArtifactID, t.TokenHash, t.Kind, fmtTime(t.ExpiresAt), fmtTime(t.CreatedAt), fmtTime(t.RevokedAt), t.PasswordHash, t.Label,
	)
	if err != nil {
		return AccessToken{}, fmt.Errorf("artifacts: mint token: %w", err)
	}
	return t, nil
}

// GetTokenByHash fetches a token by its hash.
func (s *Store) GetTokenByHash(hash string) (AccessToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.db.QueryRow(`SELECT id, artifact_id, token_hash, kind, expires_at, created_at, revoked_at, password_hash, label FROM access_tokens WHERE token_hash = ?`, hash)
	var t AccessToken
	var expires, created, revoked string
	err := row.Scan(&t.ID, &t.ArtifactID, &t.TokenHash, &t.Kind, &expires, &created, &revoked, &t.PasswordHash, &t.Label)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AccessToken{}, ErrNotFound
		}
		return AccessToken{}, fmt.Errorf("artifacts: get token: %w", err)
	}
	t.ExpiresAt = parseTime(expires)
	t.CreatedAt = parseTime(created)
	t.RevokedAt = parseTime(revoked)
	return t, nil
}

// RevokeToken sets revoked_at for a token.
func (s *Store) RevokeToken(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(`UPDATE access_tokens SET revoked_at = ? WHERE id = ? AND (revoked_at = '' OR revoked_at IS NULL)`, now, id)
	if err != nil {
		return fmt.Errorf("artifacts: revoke token: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var c int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM access_tokens WHERE id = ?`, id).Scan(&c); err != nil {
			return fmt.Errorf("artifacts: revoke token lookup: %w", err)
		}
		if c == 0 {
			return ErrNotFound
		}
	}
	return nil
}

// AddAllowEntry inserts an allow-list entry.
func (s *Store) AddAllowEntry(e AllowEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.InvitedAt.IsZero() {
		e.InvitedAt = time.Now().UTC()
	}
	_, err := s.db.Exec(
		`INSERT INTO allowlist (artifact_id, email, invited_at, accepted_at, invite_token_hash) VALUES (?,?,?,?,?)`,
		e.ArtifactID, e.Email, fmtTime(e.InvitedAt), fmtTime(e.AcceptedAt), e.InviteTokenHash,
	)
	if err != nil {
		return fmt.Errorf("artifacts: add allow entry: %w", err)
	}
	return nil
}

// GetAllowEntry fetches an allow-list entry.
func (s *Store) GetAllowEntry(artifactID, email string) (AllowEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.db.QueryRow(`SELECT artifact_id, email, invited_at, accepted_at, invite_token_hash FROM allowlist WHERE artifact_id = ? AND email = ?`, artifactID, email)
	var e AllowEntry
	var invited, accepted string
	err := row.Scan(&e.ArtifactID, &e.Email, &invited, &accepted, &e.InviteTokenHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AllowEntry{}, ErrNotFound
		}
		return AllowEntry{}, fmt.Errorf("artifacts: get allow entry: %w", err)
	}
	e.InvitedAt = parseTime(invited)
	e.AcceptedAt = parseTime(accepted)
	return e, nil
}

// AcceptInvite accepts an invitation by invite token hash.
func (s *Store) AcceptInvite(inviteTokenHash string) (AllowEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// find entry
	row := s.db.QueryRow(`SELECT artifact_id, email, invited_at, accepted_at, invite_token_hash FROM allowlist WHERE invite_token_hash = ?`, inviteTokenHash)
	var e AllowEntry
	var invited, accepted string
	err := row.Scan(&e.ArtifactID, &e.Email, &invited, &accepted, &e.InviteTokenHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AllowEntry{}, ErrNotFound
		}
		return AllowEntry{}, fmt.Errorf("artifacts: accept invite lookup: %w", err)
	}
	if !parseTime(accepted).IsZero() {
		return AllowEntry{}, errors.New("artifacts: invite already accepted")
	}
	e.InvitedAt = parseTime(invited)
	now := time.Now().UTC()
	_, err = s.db.Exec(`UPDATE allowlist SET accepted_at = ?, invite_token_hash = '' WHERE invite_token_hash = ?`, fmtTime(now), inviteTokenHash)
	if err != nil {
		return AllowEntry{}, fmt.Errorf("artifacts: accept invite update: %w", err)
	}
	e.AcceptedAt = now
	e.InviteTokenHash = ""
	return e, nil
}

// LogAccess inserts an access log entry.
func (s *Store) LogAccess(e AccessLogEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.TS.IsZero() {
		e.TS = time.Now().UTC()
	}
	_, err := s.db.Exec(
		`INSERT INTO access_log (artifact_id, token_id, ip, ua, path, status, ts) VALUES (?,?,?,?,?,?,?)`,
		e.ArtifactID, e.TokenID, e.IP, e.UA, e.Path, e.Status, fmtTime(e.TS),
	)
	if err != nil {
		return fmt.Errorf("artifacts: log access: %w", err)
	}
	return nil
}

// GC removes unreferenced blobs.
func (s *Store) GC() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT blob_hash FROM artifact_files`)
	if err != nil {
		return fmt.Errorf("artifacts: gc query: %w", err)
	}
	defer rows.Close()
	ref := map[string]bool{}
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return err
		}
		ref[h] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return s.blobs.GCUnreferenced(ref)
}
