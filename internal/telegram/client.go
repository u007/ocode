// Package telegram implements a minimal Telegram Bot API client (no external
// dependencies) plus the ocode bot logic that drives running /rc instances.
package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const apiBase = "https://api.telegram.org/bot%s"

// Client is a small Telegram Bot API client using long-polling (getUpdates),
// which works behind NAT without a public ingress.
type Client struct {
	token  string
	apiURL string
	http   *http.Client
}

// NewClient builds a Client for the given bot token.
func NewClient(token string) *Client {
	return &Client{
		token:  token,
		apiURL: fmt.Sprintf(apiBase, token),
		http:   &http.Client{Timeout: 120 * time.Second},
	}
}

// InlineButton is one button in an inline keyboard row.
type InlineButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

// ReplyMarkup wraps an inline keyboard. ForceReply, when set, prompts Telegram
// to pop up the reply box so the user can send a free-text answer (used for
// "Other"/custom question options that inline buttons cannot capture).
type ReplyMarkup struct {
	InlineKeyboard [][]InlineButton `json:"inline_keyboard"`
	ForceReply     bool             `json:"force_reply,omitempty"`
}

// Update is the minimal subset of a Telegram update we consume.
type Update struct {
	UpdateID int `json:"update_id"`
	Message  *struct {
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
		From *struct {
			ID       int64  `json:"id"`
			Username string `json:"username"`
		} `json:"from"`
		Text string `json:"text"`
	} `json:"message"`
	CallbackQuery *struct {
		ID   string `json:"id"`
		From struct {
			ID int64 `json:"id"`
		} `json:"from"`
		Data    string `json:"data"`
		Message struct {
			Chat struct {
				ID int64 `json:"id"`
			} `json:"chat"`
		} `json:"message"`
	} `json:"callback_query"`
}

// apiCall performs a Bot API method. When files is non-empty it sends
// multipart/form-data; otherwise JSON.
func (c *Client) apiCall(method string, params map[string]interface{}, files map[string][]byte) (map[string]interface{}, error) {
	u := c.apiURL + "/" + method
	var body io.Reader
	ct := "application/json"
	if len(files) > 0 {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		for k, v := range params {
			s, _ := v.(string)
			_ = mw.WriteField(k, s)
		}
		for name, data := range files {
			// Telegram's Bot API expects the document part to be named
			// "document"; use the caller-supplied filename as the part's
			// filename rather than the literal "file".
			part, err := mw.CreateFormFile("document", name)
			if err != nil {
				return nil, err
			}
			if _, err := part.Write(data); err != nil {
				return nil, err
			}
		}
		if err := mw.Close(); err != nil {
			return nil, err
		}
		body = &buf
		ct = mw.FormDataContentType()
	} else {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}

	req, err := http.NewRequest(http.MethodPost, u, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", ct)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		OK          bool                   `json:"ok"`
		Description string                 `json:"description"`
		Result      map[string]interface{} `json:"result"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode response: %w (body: %s)", err, string(raw))
	}
	if !out.OK {
		return nil, fmt.Errorf("telegram api error: %s", out.Description)
	}
	return out.Result, nil
}

// GetUpdatesRaw performs getUpdates and returns the raw update list. apiCall
// expects an object result, so we read the response directly here.
func (c *Client) GetUpdatesRaw(offset, timeout int) ([]Update, error) {
	u := c.apiURL + "/getUpdates"
	params := url.Values{}
	params.Set("offset", fmt.Sprintf("%d", offset))
	params.Set("timeout", fmt.Sprintf("%d", timeout))
	params.Set("allowed_updates", `["message","callback_query"]`)
	req, err := http.NewRequest(http.MethodGet, u+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		OK          bool     `json:"ok"`
		Description string   `json:"description"`
		Result      []Update `json:"result"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode updates: %w (body: %s)", err, string(raw))
	}
	if !out.OK {
		return nil, fmt.Errorf("telegram getUpdates returned ok=false: %s", out.Description)
	}
	return out.Result, nil
}

// SendMessage posts a text message and returns the new message id.
func (c *Client) SendMessage(chatID int64, text string, markup *ReplyMarkup) (int, error) {
	params := map[string]interface{}{
		"chat_id": chatID,
		"text":    text,
	}
	if markup != nil {
		params["reply_markup"] = markup
	}
	res, err := c.apiCall("sendMessage", params, nil)
	if err != nil {
		return 0, err
	}
	if id, ok := res["message_id"].(float64); ok {
		return int(id), nil
	}
	return 0, nil
}

// EditMessageText updates an existing message's text.
func (c *Client) EditMessageText(chatID int64, msgID int, text string) error {
	_, err := c.apiCall("editMessageText", map[string]interface{}{
		"chat_id":    chatID,
		"message_id": msgID,
		"text":       text,
	}, nil)
	return err
}

// EditMessageReplyMarkup updates (or clears, when markup is nil) an existing
// message's inline keyboard in place — cheaper than re-sending the text and
// ideal for reflecting selection state as the user toggles options.
func (c *Client) EditMessageReplyMarkup(chatID int64, msgID int, markup *ReplyMarkup) error {
	kb := [][]InlineButton{}
	if markup != nil {
		kb = markup.InlineKeyboard
	}
	_, err := c.apiCall("editMessageReplyMarkup", map[string]interface{}{
		"chat_id":      chatID,
		"message_id":   msgID,
		"reply_markup": ReplyMarkup{InlineKeyboard: kb},
	}, nil)
	return err
}

// AnswerCallbackQuery acknowledges an inline-button press.
func (c *Client) AnswerCallbackQuery(id string) error {
	_, err := c.apiCall("answerCallbackQuery", map[string]interface{}{
		"callback_query_id": id,
	}, nil)
	return err
}

// SendDocument uploads a file (e.g. a large tool output) as a document.
func (c *Client) SendDocument(chatID int64, filename string, content []byte, caption string) error {
	params := map[string]interface{}{
		"chat_id": chatID,
		"caption": caption,
	}
	files := map[string][]byte{filename: content}
	_, err := c.apiCall("sendDocument", params, files)
	return err
}

// CommandDesc describes one bot command for setMyCommands.
type CommandDesc struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// SetMyCommands registers the bot's slash commands in the Telegram UI.
func (c *Client) SetMyCommands(commands []CommandDesc) error {
	_, err := c.apiCall("setMyCommands", map[string]interface{}{
		"commands": commands,
	}, nil)
	return err
}

// EscapeMarkdownV2 escapes characters that are special in Telegram's
// MarkdownV2 parse mode.
func EscapeMarkdownV2(s string) string {
	special := []string{"_", "*", "[", "]", "(", ")", "~", "`", ">", "#", "+", "-", "=", "|", "{", "}", ".", "!"}
	for _, ch := range special {
		s = strings.ReplaceAll(s, ch, "\\"+ch)
	}
	return s
}
