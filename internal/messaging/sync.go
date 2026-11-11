package messaging

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ExportChanges returns messages with timestamp >= since (unix seconds).
func (s *Store) ExportChanges(since int64, limit int) ([]Message, int64, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.Query(`SELECT account_id,id,chat_id,sender_id,sender_name,direction,text,media_url,timestamp FROM messages WHERE timestamp >= ? ORDER BY timestamp ASC LIMIT ?`, since, limit)
	if err != nil {
		return nil, since, err
	}
	defer rows.Close()
	var out []Message
	var maxTS int64 = since
	for rows.Next() {
		var m Message
		var ts int64
		if err := rows.Scan(&m.AccountID, &m.ID, &m.ChatID, &m.SenderID, &m.SenderName, &m.Direction, &m.Text, &m.MediaURL, &ts); err != nil {
			return nil, since, err
		}
		m.Timestamp = time.Unix(ts, 0)
		if ts > maxTS {
			maxTS = ts
		}
		out = append(out, m)
	}
	return out, maxTS, rows.Err()
}

func (s *Store) GetCursor(scope string) int64 {
	var c int64
	_ = s.db.QueryRow(`SELECT cursor FROM sync_cursor WHERE scope=?`, scope).Scan(&c)
	return c
}

func (s *Store) SetCursor(scope string, cursor int64) error {
	_, err := s.db.Exec(`INSERT INTO sync_cursor(scope,cursor,updated_at) VALUES(?,?,?) ON CONFLICT(scope) DO UPDATE SET cursor=excluded.cursor,updated_at=excluded.updated_at`,
		scope, cursor, time.Now().Unix())
	return err
}

// ImportMessages upserts remote messages (idempotent by account_id+id).
func (s *Store) ImportMessages(msgs []Message) error {
	for _, m := range msgs {
		if err := s.AddMessage(m); err != nil {
			return err
		}
	}
	return nil
}

// PushToRemote exports changes since the stored cursor and POSTs them.
func (m *Manager) PushToRemote(ctx context.Context, url string) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	since := m.store.GetCursor("push")
	msgs, newCursor, err := m.store.ExportChanges(since, 500)
	if err != nil {
		return since, err
	}
	if len(msgs) == 0 {
		return since, nil
	}
	body, _ := json.Marshal(map[string]interface{}{"messages": msgs, "cursor": since, "dir": "push"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url+"/api/messaging/sync", bytes.NewReader(body))
	if err != nil {
		return since, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return since, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return since, fmt.Errorf("push failed: %s", resp.Status)
	}
	var r struct {
		Cursor int64 `json:"cursor"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&r)
	if r.Cursor > 0 {
		newCursor = r.Cursor
	}
	if err := m.store.SetCursor("push", newCursor); err != nil {
		return since, err
	}
	return newCursor, nil
}

// PullFromRemote fetches remote changes since cursor and merges.
func (m *Manager) PullFromRemote(ctx context.Context, url string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	since := m.store.GetCursor("pull")
	body, _ := json.Marshal(map[string]interface{}{"cursor": since, "dir": "pull"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url+"/api/messaging/sync", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("pull failed: %s", resp.Status)
	}
	var r struct {
		Messages []Message `json:"messages"`
		Cursor   int64     `json:"cursor"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return err
	}
	if err := m.store.ImportMessages(r.Messages); err != nil {
		return err
	}
	if r.Cursor > 0 {
		_ = m.store.SetCursor("pull", r.Cursor)
	}
	return nil
}
