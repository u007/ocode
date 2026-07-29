package sync

import (
	"context"
	"log"
	"time"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
)

const defaultDebounce = 5 * time.Second

type watcherOptions struct {
	debounce time.Duration
}

// WatcherOption configures the background sync watcher.
type WatcherOption func(*watcherOptions)

// WithDebounce overrides the default 5s idle debounce; exposed for tests.
func WithDebounce(d time.Duration) WatcherOption {
	return func(o *watcherOptions) { o.debounce = d }
}

// StartWatcher hooks config.OnConfigSaved / auth.OnCredentialsSaved and
// pushes each blob to the server after a debounce period of inactivity.
// Returns a stop func that unhooks and stops the timers.
//
// Known limitation: auth.json can be shared with a separate opencode CLI
// installation (see internal/auth's opencodeLegacyAuthPath). Because these
// hooks only fire on ocode's own Set/Remove calls, a credential rotated by
// opencode alone (with no accompanying ocode-side save) sits on disk
// un-pushed until some later ocode-triggered save or the next process
// startup's Pull picks it up as part of the local blob. This is a
// staleness gap, not a corruption risk (Push/Pull always re-read the
// current on-disk content fresh, and writes are atomic — see
// writeLocalConfigFileAtomic) — closing it fully would need to watch
// auth.json for external changes (e.g. fsnotify), which is out of scope
// for now.
func StartWatcher(c *Client, opts ...WatcherOption) (stop func()) {
	o := watcherOptions{debounce: defaultDebounce}
	for _, apply := range opts {
		apply(&o)
	}

	configCh := make(chan struct{}, 1)
	authCh := make(chan struct{}, 1)

	removeConfig := config.OnConfigSaved.Add(func() { nonBlockingSignal(configCh) })
	removeAuth := auth.OnCredentialsSaved.Add(func() { nonBlockingSignal(authCh) })

	done := make(chan struct{})
	go debounceLoop(configCh, done, o.debounce, func() { pushBlobFromDisk(c, BlobTypeConfig) })
	go debounceLoop(authCh, done, o.debounce, func() { pushBlobFromDisk(c, BlobTypeAuth) })

	return func() {
		removeConfig()
		removeAuth()
		close(done)
	}
}

func nonBlockingSignal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func debounceLoop(signal <-chan struct{}, done <-chan struct{}, debounce time.Duration, fire func()) {
	var timer *time.Timer
	var timerCh <-chan time.Time
	for {
		select {
		case <-done:
			if timer != nil {
				timer.Stop()
			}
			return
		case <-signal:
			if timer != nil {
				timer.Stop()
			}
			timer = time.NewTimer(debounce)
			timerCh = timer.C
		case <-timerCh:
			fire()
			timerCh = nil
		}
	}
}

func pushBlobFromDisk(c *Client, blob BlobType) {
	path, err := localConfigPathFor(blob)
	if err != nil {
		log.Printf("sync: resolve path for %s: %v", blob, err)
		return
	}
	data, err := readLocalConfigFile(path)
	if err != nil {
		log.Printf("sync: reading %s for background push failed: %v", blob, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := c.Push(ctx, blob, data); err != nil {
		log.Printf("sync: background push of %s failed: %v", blob, err)
	}
}
