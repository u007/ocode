package messaging

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Store persists accounts, chats and messages in SQLite.
type Store struct {
	db   *sql.DB
	path string
	key  []byte
}

func NewStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(path, 0600)
	s := &Store{db: db, path: path}
	if k, err := loadOrCreateMessagingKey(path); err == nil {
		s.key = k
	}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS accounts (
			id TEXT PRIMARY KEY,
			provider TEXT NOT NULL,
			label TEXT NOT NULL,
			credentials TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'disconnected',
			error TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS chats (
			account_id TEXT NOT NULL,
			id TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			type TEXT NOT NULL DEFAULT 'dm',
			last_message_at INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (account_id, id)
		);`,
		`CREATE TABLE IF NOT EXISTS messages (
			account_id TEXT NOT NULL,
			id TEXT NOT NULL,
			chat_id TEXT NOT NULL,
			sender_id TEXT NOT NULL DEFAULT '',
			sender_name TEXT NOT NULL DEFAULT '',
			direction TEXT NOT NULL DEFAULT 'in',
			text TEXT NOT NULL DEFAULT '',
			media_url TEXT NOT NULL DEFAULT '',
			timestamp INTEGER NOT NULL DEFAULT 0,
			raw TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (account_id, id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_messages_chat ON messages(account_id, chat_id, timestamp);`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(text, sender_name, content='messages', content_rowid='rowid');`,
		`CREATE TABLE IF NOT EXISTS sync_cursor (
			scope TEXT PRIMARY KEY,
			cursor INTEGER NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL DEFAULT 0
		);`,
	}
	for _, st := range stmts {
		if _, err := s.db.Exec(st); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	// triggers to keep fts in sync
	trigs := []string{
		`CREATE TRIGGER IF NOT EXISTS messages_ai AFTER INSERT ON messages BEGIN INSERT INTO messages_fts(rowid, text, sender_name) VALUES (new.rowid, new.text, new.sender_name); END;`,
		`CREATE TRIGGER IF NOT EXISTS messages_ad AFTER DELETE ON messages BEGIN INSERT INTO messages_fts(messages_fts, rowid, text, sender_name) VALUES('delete', old.rowid, old.text, old.sender_name); END;`,
		`CREATE TRIGGER IF NOT EXISTS messages_au AFTER UPDATE ON messages BEGIN INSERT INTO messages_fts(messages_fts, rowid, text, sender_name) VALUES('delete', old.rowid, old.text, old.sender_name); INSERT INTO messages_fts(rowid, text, sender_name) VALUES (new.rowid, new.text, new.sender_name); END;`,
	}
	for _, t := range trigs {
		if _, err := s.db.Exec(t); err != nil {
			return fmt.Errorf("migrate trig: %w", err)
		}
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

// --- Accounts ---

func (s *Store) UpsertAccount(a Account) error {
	now := time.Now().Unix()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	a.UpdatedAt = time.Now()
	creds := a.Credentials
	if s.key != nil {
		creds = encryptMessagingCreds(s.key, creds)
	}
	_, err := s.db.Exec(`INSERT INTO accounts(id,provider,label,credentials,status,error,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET provider=excluded.provider,label=excluded.label,credentials=excluded.credentials,status=excluded.status,error=excluded.error,updated_at=excluded.updated_at`,
		a.ID, string(a.Provider), a.Label, creds, a.Status, a.Error, a.CreatedAt.Unix(), a.UpdatedAt.Unix())
	if err != nil {
		return err
	}
	_ = now
	return nil
}

func (s *Store) GetAccount(id string) (Account, error) {
	row := s.db.QueryRow(`SELECT id,provider,label,credentials,status,error,created_at,updated_at FROM accounts WHERE id=?`, id)
	var a Account
	var provider string
	var created, updated int64
	if err := row.Scan(&a.ID, &provider, &a.Label, &a.Credentials, &a.Status, &a.Error, &created, &updated); err != nil {
		return a, err
	}
	a.Provider = Provider(provider)
	a.CreatedAt = time.Unix(created, 0)
	a.UpdatedAt = time.Unix(updated, 0)
	if s.key != nil {
		a.Credentials = decryptMessagingCreds(s.key, a.Credentials)
	}
	return a, nil
}

func (s *Store) ListAccounts() ([]Account, error) {
	rows, err := s.db.Query(`SELECT id,provider,label,credentials,status,error,created_at,updated_at FROM accounts ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		var a Account
		var provider string
		var created, updated int64
		if err := rows.Scan(&a.ID, &provider, &a.Label, &a.Credentials, &a.Status, &a.Error, &created, &updated); err != nil {
			return nil, err
		}
		a.Provider = Provider(provider)
		a.CreatedAt = time.Unix(created, 0)
		a.UpdatedAt = time.Unix(updated, 0)
		if s.key != nil {
			a.Credentials = decryptMessagingCreds(s.key, a.Credentials)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) DeleteAccount(id string) error {
	_, err := s.db.Exec(`DELETE FROM accounts WHERE id=?`, id)
	if err != nil {
		return err
	}
	_, _ = s.db.Exec(`DELETE FROM chats WHERE account_id=?`, id)
	_, _ = s.db.Exec(`DELETE FROM messages WHERE account_id=?`, id)
	return nil
}

// --- Chats ---

func (s *Store) UpsertChat(c Chat) error {
	ts := c.LastMessageAt.Unix()
	if c.LastMessageAt.IsZero() {
		ts = 0
	}
	_, err := s.db.Exec(`INSERT INTO chats(account_id,id,title,type,last_message_at) VALUES(?,?,?,?,?)
		ON CONFLICT(account_id,id) DO UPDATE SET title=excluded.title,type=excluded.type,last_message_at=excluded.last_message_at`,
		c.AccountID, c.ID, c.Title, c.Type, ts)
	return err
}

func (s *Store) ListChats(accountID string) ([]Chat, error) {
	q := `SELECT account_id,id,title,type,last_message_at FROM chats`
	args := []interface{}{}
	if accountID != "" {
		q += ` WHERE account_id=?`
		args = append(args, accountID)
	}
	q += ` ORDER BY last_message_at DESC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Chat
	for rows.Next() {
		var c Chat
		var ts int64
		if err := rows.Scan(&c.AccountID, &c.ID, &c.Title, &c.Type, &ts); err != nil {
			return nil, err
		}
		if ts > 0 {
			c.LastMessageAt = time.Unix(ts, 0)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// --- Messages ---

func (s *Store) AddMessage(m Message) error {
	ts := m.Timestamp.Unix()
	if m.Timestamp.IsZero() {
		ts = time.Now().Unix()
		m.Timestamp = time.Unix(ts, 0)
	}
	_, err := s.db.Exec(`INSERT OR REPLACE INTO messages(account_id,id,chat_id,sender_id,sender_name,direction,text,media_url,timestamp,raw) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		m.AccountID, m.ID, m.ChatID, m.SenderID, m.SenderName, m.Direction, m.Text, m.MediaURL, ts, m.Raw)
	if err != nil {
		return err
	}
	// update chat last_message_at
	_, _ = s.db.Exec(`INSERT INTO chats(account_id,id,title,type,last_message_at) VALUES(?,?,?,?,?) ON CONFLICT(account_id,id) DO UPDATE SET last_message_at=MAX(last_message_at, excluded.last_message_at)`,
		m.AccountID, m.ChatID, m.ChatID, "dm", ts)
	return nil
}

func (s *Store) ListMessages(accountID, chatID string, limit int) ([]Message, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT account_id,id,chat_id,sender_id,sender_name,direction,text,media_url,timestamp FROM messages WHERE account_id=? AND chat_id=? ORDER BY timestamp DESC LIMIT ?`, accountID, chatID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var ts int64
		if err := rows.Scan(&m.AccountID, &m.ID, &m.ChatID, &m.SenderID, &m.SenderName, &m.Direction, &m.Text, &m.MediaURL, &ts); err != nil {
			return nil, err
		}
		m.Timestamp = time.Unix(ts, 0)
		out = append(out, m)
	}
	// reverse to chronological
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}

func (s *Store) SearchMessages(query string, accountID string, limit int) ([]Message, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT m.account_id,m.id,m.chat_id,m.sender_id,m.sender_name,m.direction,m.text,m.media_url,m.timestamp
		FROM messages_fts f JOIN messages m ON m.rowid = f.rowid
		WHERE messages_fts MATCH ?`
	args := []interface{}{query}
	if accountID != "" {
		q += ` AND m.account_id=?`
		args = append(args, accountID)
	}
	q += ` ORDER BY m.timestamp DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var ts int64
		if err := rows.Scan(&m.AccountID, &m.ID, &m.ChatID, &m.SenderID, &m.SenderName, &m.Direction, &m.Text, &m.MediaURL, &ts); err != nil {
			return nil, err
		}
		m.Timestamp = time.Unix(ts, 0)
		out = append(out, m)
	}
	return out, rows.Err()
}

func loadOrCreateMessagingKey(path string) ([]byte, error) {
	kp := path + ".key"
	if data, err := os.ReadFile(kp); err == nil && len(data) == 32 {
		return data, nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.WriteFile(kp, key, 0600); err != nil {
		return nil, err
	}
	return key, nil
}

func encryptMessagingCreds(key []byte, plain string) string {
	if plain == "" {
		return ""
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return plain
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return plain
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return plain
	}
	ct := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return "enc:" + base64.StdEncoding.EncodeToString(ct)
}

func decryptMessagingCreds(key []byte, v string) string {
	if !strings.HasPrefix(v, "enc:") {
		return v
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(v, "enc:"))
	if err != nil {
		return v
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return v
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return v
	}
	if len(raw) < gcm.NonceSize() {
		return v
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return v
	}
	return string(pt)
}

var _ = json.Marshal
