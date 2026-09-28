package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/u007/ocode/internal/filelock"
)

// cacheFormatVersion is bumped whenever the embedding pipeline changes in a way
// that makes previously-cached vectors incompatible for the SAME model id + dim.
// A load whose stored version differs is discarded (re-embed). v2: the local
// LFM2.5 backend moved from MLX (causal + mean pooling) to llama.cpp (bidir +
// CLS pooling) under the same "local/lfm2.5-embedding" id and dim 1024, and the
// bge-m3 llama.cpp build was bumped b9747→b9777 — both produce different vectors
// that the id+dim check alone would not catch.
const cacheFormatVersion = 2

// Cache is a per-model on-disk store of passage vectors keyed by DocHash.
type Cache struct {
	path    string
	Version int                  `json:"version"`
	Model   string               `json:"model"`
	Dim     int                  `json:"dim"`
	Items   map[string][]float32 `json:"items"`
}

func sanitizeModelID(id string) string {
	return strings.NewReplacer("/", "_", ":", "_", " ", "_").Replace(id)
}

// LoadCache reads the cache file for modelID. A missing file, or a file whose
// format version / model / dim don't match, yields an empty (fresh) cache —
// that is the invalidation path.
func LoadCache(dir, modelID string, dim int) (*Cache, error) {
	path := filepath.Join(dir, "corpus-"+sanitizeModelID(modelID)+".json")
	fresh := &Cache{path: path, Version: cacheFormatVersion, Model: modelID, Dim: dim, Items: map[string][]float32{}}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fresh, nil
		}
		return nil, fmt.Errorf("read discovery cache %s: %w", path, err)
	}
	var loaded Cache
	if err := json.Unmarshal(data, &loaded); err != nil {
		// Corrupt cache is non-fatal: log and start fresh (the only recovery
		// path, explicitly logged per the no-silent-recovery rule).
		emitDiscoveryDebug("WARN", fmt.Sprintf("discovery cache %s unreadable, rebuilding: %v", path, err))
		return fresh, nil
	}
	if loaded.Version != cacheFormatVersion || loaded.Model != modelID || loaded.Dim != dim || loaded.Items == nil {
		return fresh, nil
	}
	loaded.path = path
	return &loaded, nil
}

func (c *Cache) Get(hash string) ([]float32, bool) { v, ok := c.Items[hash]; return v, ok }
func (c *Cache) Put(hash string, vec []float32)    { c.Items[hash] = vec }

// corpusLockTimeout bounds how long a build waits for the corpus lock before
// giving up and skipping. Missing the lock is the NORMAL contended case, not a
// failure: another ocode instance is embedding this very corpus, so there is
// nothing to recover and nothing to panic about — the caller already retries a
// failed Warm in the background, which picks the results up.
//
// Atomic rather than a plain var: tests shorten the wait while a peer goroutine
// may still be inside the lock reading it, and a plain read/write of a shared
// duration is a genuine data race. Production never writes it.
var corpusLockTimeout atomic.Int64

func init() { corpusLockTimeout.Store(int64(5 * time.Second)) }

// corpusLockWait is the current acquire budget.
func corpusLockWait() time.Duration { return time.Duration(corpusLockTimeout.Load()) }

// corpusLockWaitFor clamps the budget to the caller's own deadline. The per-turn
// Warm runs with a 500ms context, and filelock does not observe a context, so
// without this clamp a contended turn would stall for the full budget rather
// than for the slice of it the caller is willing to spend.
//
// The result is always strictly positive: filelock treats a non-positive timeout
// as "use the 10s package default", which would turn an exhausted budget into a
// ten-second stall — the exact opposite of the clamp.
func corpusLockWaitFor(ctx context.Context) time.Duration {
	wait := corpusLockWait()
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < wait {
			wait = remaining
		}
	}
	if wait <= 0 {
		wait = time.Millisecond
	}
	return wait
}

// ErrCorpusLocked reports that another ocode instance holds the corpus lock, so
// this build was skipped rather than run. Callers treat any Warm error as
// "retry in the background", which is the right response: the lock holder is
// embedding the same corpus and will persist the vectors either way.
var ErrCorpusLocked = errors.New("discovery corpus is locked by another instance")

// isCorpusLocked reports whether err is (or wraps) ErrCorpusLocked.
func isCorpusLocked(err error) bool { return errors.Is(err, ErrCorpusLocked) }

// withCorpusLock runs fn holding an exclusive advisory lock on the corpus
// cache's lock file, so re-check → embed → write is atomic against other
// instances. Holding it across the embed call is safe because a flock is owned
// by the kernel, not by a pid we track: a holder that dies releases it
// automatically, so there is no stale lock to detect, time out, or steal.
//
// wait bounds the acquire; the caller derives it from its own deadline.
func withCorpusLock(ctx context.Context, cachePath string, wait time.Duration, fn func() error) error {
	// The lock file lives beside the cache, and on a first run neither exists
	// yet. filelock opens with O_CREATE, which creates the lock FILE but not its
	// parent directory — so the directory has to be made first, or every lock
	// attempt fails and each instance silently re-embeds the whole corpus.
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return fmt.Errorf("mkdir discovery cache dir: %w", err)
	}
	return filelock.WithFileLockTimeout(cachePath+".lock", wait, fn)
}

// Save writes the cache atomically (temp + rename) so concurrent sessions can't
// observe a half-written file.
func (c *Cache) Save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0755); err != nil {
		return fmt.Errorf("mkdir discovery cache dir: %w", err)
	}
	data, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal discovery cache: %w", err)
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write discovery cache tmp: %w", err)
	}
	if err := os.Rename(tmp, c.path); err != nil {
		return fmt.Errorf("rename discovery cache: %w", err)
	}
	return nil
}

// BuildCorpusCached builds a corpus, embedding only docs missing from the cache.
// Returns the corpus and the count of docs actually embedded (cache misses).
//
// The cache is shared by every ocode instance on the machine, so this is
// probe → lock → re-check → embed → write. The probe reads the passed-in cache
// without a lock: a fully-cached corpus has nothing to embed and nothing to
// write, so it must not contend for the lock at all (that is the steady state,
// and contending unconditionally would stall a second instance every turn).
//
// Once there ARE misses, re-check and write happen inside ONE critical section.
// That pairing is load-bearing: a separate re-read and a separate write leave a
// window in which two instances both see the docs as missing and both embed the
// entire corpus, which is the duplication this lock exists to prevent. The
// re-read matters as much as the lock — a peer that embedded these docs while we
// were waiting must not be re-embedded.
//
// A lock we cannot take is reported as ErrCorpusLocked rather than worked around.
func BuildCorpusCached(ctx context.Context, e Embedder, docs []Doc, c *Cache) (*Corpus, int, error) {
	if !corpusHasMisses(docs, c) {
		corpus, err := corpusFromCache(docs, c)
		if err != nil {
			return nil, 0, err
		}
		return corpus, 0, nil
	}

	var corpus *Corpus
	var misses int
	err := withCorpusLock(ctx, c.path, corpusLockWaitFor(ctx), func() error {
		// Re-read under the lock: the probe was only a hint, and a peer may have
		// persisted these very vectors while we waited for the lock.
		fresh, err := LoadCache(filepath.Dir(c.path), c.Model, c.Dim)
		if err != nil {
			return fmt.Errorf("reload discovery cache: %w", err)
		}
		corpus, misses, err = embedCorpusMisses(ctx, e, docs, fresh)
		return err
	})
	if err != nil {
		if isLockTimeout(err) {
			return nil, 0, fmt.Errorf("%w: %v", ErrCorpusLocked, err)
		}
		return nil, 0, err
	}
	return corpus, misses, nil
}

// corpusHasMisses reports whether any doc lacks a cached vector. It reads no
// shared mutable state, so it is safe to call outside the lock.
func corpusHasMisses(docs []Doc, c *Cache) bool {
	for _, d := range docs {
		if _, ok := c.Get(DocHash(d)); !ok {
			return true
		}
	}
	return false
}

// corpusFromCache assembles a corpus purely from already-cached vectors. It
// touches neither the lock nor the disk, so it is the no-miss fast path.
func corpusFromCache(docs []Doc, c *Cache) (*Corpus, error) {
	vecs := make([][]float32, len(docs))
	for i, d := range docs {
		v, ok := c.Get(DocHash(d))
		if !ok {
			return nil, fmt.Errorf("doc %q missing from a cache reported as complete", d.ID)
		}
		vecs[i] = v
	}
	return &Corpus{Docs: docs, Vecs: vecs}, nil
}

// embedCorpusMisses embeds the docs absent from c and persists the result. The
// caller must hold the corpus lock (or have established there are no misses).
func embedCorpusMisses(ctx context.Context, e Embedder, docs []Doc, c *Cache) (*Corpus, int, error) {
	vecs := make([][]float32, len(docs))
	var missIdx []int
	var missTexts []string
	for i, d := range docs {
		if v, ok := c.Get(DocHash(d)); ok {
			vecs[i] = v
			continue
		}
		missIdx = append(missIdx, i)
		missTexts = append(missTexts, d.Text)
	}
	if len(missTexts) == 0 {
		return &Corpus{Docs: docs, Vecs: vecs}, 0, nil
	}
	embedded, err := e.Embed(ctx, missTexts, Passage)
	if err != nil {
		return nil, 0, fmt.Errorf("embed %d corpus misses: %w", len(missTexts), err)
	}
	if len(embedded) != len(missTexts) {
		return nil, 0, fmt.Errorf("embedder returned %d vectors for %d misses", len(embedded), len(missTexts))
	}
	for k, idx := range missIdx {
		vecs[idx] = embedded[k]
		c.Put(DocHash(docs[idx]), embedded[k])
	}
	if err := c.Save(); err != nil {
		// Non-fatal: corpus is usable in-memory this session; log it.
		emitDiscoveryDebug("WARN", fmt.Sprintf("persist discovery cache failed: %v", err))
	}
	return &Corpus{Docs: docs, Vecs: vecs}, len(missTexts), nil
}

// isLockTimeout reports whether err is the shared filelock helper's
// "could not acquire in time" error. That condition specifically means a peer
// holds the lock, which is a skip; any other lock error (e.g. a mkdir failure)
// is a real problem and must not be reported as mere contention.
func isLockTimeout(err error) bool {
	return err != nil && strings.Contains(err.Error(), "timeout waiting for lock")
}
