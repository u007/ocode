package agent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/discovery"
)

// localSlotStaleAfter bounds how long a slot lock file may be held before a
// contending process treats it as abandoned (crashed holder) and steals it.
// Set comfortably above llmRequestTimeout so a legitimately slow generation
// is never stolen out from under it.
const localSlotStaleAfter = llmRequestTimeout + time.Minute

// localSlotPollInterval is how often a blocked acquireLocalModelSlot call
// re-checks for a free slot.
const localSlotPollInterval = 100 * time.Millisecond

// localConcurrencyTransport enforces LocalModelConfig.MaxParallel for
// requests to a locally-managed chat model server, as a cross-process
// counting semaphore (lock files under DiscoveryCacheDir()/locks/). This
// exists because some local server backends have no concurrency flag of
// their own — mlx_lm.server (used for Bonsai on Apple Silicon, see
// manifest.go) accepts no --parallel/--decode-concurrency equivalent, so
// nothing in the launched process enforces the configured limit. Since the
// deterministic port assignment (discovery.AssignChatPort) means every
// ocode process talks to the SAME physical server for a given model id, a
// per-process in-memory semaphore would undercount total concurrency once
// more than one ocode process is active — hence the file-lock-based cross-
// process gate here instead.
//
// Requests to any other host (remote providers, or a local port that isn't
// a registered ocode-managed model — e.g. user-run LM Studio) pass through
// unchanged.
type localConcurrencyTransport struct {
	next http.RoundTripper
}

func (t *localConcurrencyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	modelID, maxParallel, ok := resolveLocalModelForRequest(req)
	if !ok {
		return t.next.RoundTrip(req)
	}
	release, err := acquireLocalModelSlot(req.Context(), modelID, maxParallel, DiscoveryCacheDir())
	if err != nil {
		return nil, fmt.Errorf("local model %q: %w", modelID, err)
	}
	resp, err := t.next.RoundTrip(req)
	if err != nil {
		release()
		return nil, err
	}
	resp.Body = &slotReleasingBody{ReadCloser: resp.Body, release: release}
	return resp, nil
}

// slotReleasingBody releases its held concurrency slot exactly once, when
// the response body is closed — not when RoundTrip returns, since a
// streaming chat completion is still generating tokens (and so still
// occupying the server) for as long as the caller keeps reading the body.
type slotReleasingBody struct {
	io.ReadCloser
	release func()
	closed  bool
}

func (b *slotReleasingBody) Close() error {
	err := b.ReadCloser.Close()
	if !b.closed {
		b.closed = true
		b.release()
	}
	return err
}

// resolveLocalModelForRequest reports the registered local model id and
// configured max_parallel for req's target, if req targets one of THIS
// machine's ocode-managed local model ports. ok is false for any request
// that isn't (remote providers, or an unmanaged local server).
func resolveLocalModelForRequest(req *http.Request) (modelID string, maxParallel int, ok bool) {
	host := req.URL.Hostname()
	if host != "127.0.0.1" && host != "localhost" {
		return "", 0, false
	}
	port, err := strconv.Atoi(req.URL.Port())
	if err != nil {
		return "", 0, false
	}
	registeredIDs, err := config.RegisteredLocalModelIDs()
	if err != nil || len(registeredIDs) == 0 {
		return "", 0, false
	}
	for _, id := range registeredIDs {
		p, err := discovery.AssignChatPort(id, registeredIDs)
		if err != nil || p != port {
			continue
		}
		lm, lmOK, lmErr := config.LocalModelConfigFor(id)
		if lmErr != nil || !lmOK || lm.MaxParallel <= 0 {
			return "", 0, false
		}
		return id, lm.MaxParallel, true
	}
	return "", 0, false
}

// acquireLocalModelSlot blocks until one of modelID's maxParallel slots is
// free (or ctx is done), then returns a release func that must be called
// exactly once to free it. Slots are lock files (O_CREATE|O_EXCL — same
// primitive as config.lockOcodeConfig) named
// "<cacheDir>/locks/<sanitized modelID>.slotN.lock"; the OS releasing file
// descriptors on process death means a crashed holder cannot deadlock other
// processes forever, but since a plain create/remove lock (not flock) has no
// OS-enforced release, a holder that dies without releasing leaves its file
// behind — reaped via the mtime staleness check below.
func acquireLocalModelSlot(ctx context.Context, modelID string, maxParallel int, cacheDir string) (func(), error) {
	lockDir := filepath.Join(cacheDir, "locks")
	if err := os.MkdirAll(lockDir, 0755); err != nil {
		return nil, err
	}
	safeID := strings.ReplaceAll(modelID, "/", "_")

	ticker := time.NewTicker(localSlotPollInterval)
	defer ticker.Stop()
	for {
		for i := 0; i < maxParallel; i++ {
			slotPath := filepath.Join(lockDir, fmt.Sprintf("%s.slot%d.lock", safeID, i))
			f, err := os.OpenFile(slotPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
			if err == nil {
				f.Close()
				released := false
				return func() {
					if released {
						return
					}
					released = true
					os.Remove(slotPath)
				}, nil
			}
			if !os.IsExist(err) {
				return nil, fmt.Errorf("create slot lock %s: %w", slotPath, err)
			}
			if info, statErr := os.Stat(slotPath); statErr == nil && time.Since(info.ModTime()) > localSlotStaleAfter {
				os.Remove(slotPath)
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}
