package auth

import (
	"sync"
	"sync/atomic"
	"testing"
)

// Two goroutines that reach the legacy import at once must import exactly once.
// The loser either waits on the file lock and then sees the sentinel, or sees it
// before taking the lock; in both cases it reports that it wrote nothing.
func TestConcurrentLegacyImportImportsOnce(t *testing.T) {
	resetCrossProcessStores(t)
	useLegacyFile(t, `{"openai":{"type":"api","key":"sk-legacy"}}`)
	path := mustAuthPath(t)

	const n = 8
	var wrote atomic.Int32
	errs := make(chan error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			w, err := importLegacyOnce(path)
			if err != nil {
				errs <- err
			}
			if w {
				wrote.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("importLegacyOnce: %v", err)
	}
	if got := wrote.Load(); got != 1 {
		t.Fatalf("importLegacyOnce wrote %d times across %d callers, want exactly 1", got, n)
	}
	if !sentinelExists(t) {
		t.Error("the sentinel is missing after the import")
	}
	if _, ok := onDiskAuth(t)["openai"]; !ok {
		t.Error("the legacy provider was not imported")
	}
}
