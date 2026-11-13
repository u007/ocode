package cloudconnect

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"time"
)

// doWithRetry executes req with the shared client, retrying on 429/503 using
// Retry-After (capped at 5s) up to 2 times.
func doWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
	for range 3 {
		resp, err := driveHTTPClient.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != 429 && resp.StatusCode != 503 {
			return resp, nil
		}
		ra := resp.Header.Get("Retry-After")
		wait := time.Second
		if n, err := strconv.Atoi(ra); err == nil && n > 0 {
			wait = min(time.Duration(n)*time.Second, 5*time.Second)
		}
		resp.Body.Close()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
		// rebuild body if needed (req.Body already consumed? For GET no body)
	}
	return nil, context.DeadlineExceeded
}

// readAllBody reads and closes resp.Body.
func readAllBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
