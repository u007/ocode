package sync

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// Client talks to the kakiit /api/ocode/* endpoints.
type Client struct {
	BaseURL      string
	HTTPClient   *http.Client
	pollInterval time.Duration // exposed for tests; zero-value = production default
}

// NewClient creates a sync client pointing at the given kakiit server.
func NewClient(baseURL string) *Client {
	return &Client{BaseURL: baseURL, HTTPClient: http.DefaultClient, pollInterval: 2 * time.Second}
}

func (c *Client) interval() time.Duration {
	if c.pollInterval == 0 {
		return 2 * time.Second
	}
	return c.pollInterval
}

func (c *Client) jsonBody(v interface{}) (io.Reader, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(b), nil
}

func (c *Client) decode(resp *http.Response, v interface{}) error {
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(v)
}
