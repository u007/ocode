package session

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestIndexConcurrentUpsertsDoNotBusy reproduces the shared-index contention
// seen when several sessions of one project stream concurrently: each live
// write opens the project's index.sqlite (running CREATE TABLE IF NOT EXISTS
// DDL) and upserts its row. The DDL runs as an implicit transaction, which on
// a deferred lock upgrade returns SQLITE_BUSY without consulting
// busy_timeout, so concurrent writers must not fail.
func TestIndexConcurrentUpsertsDoNotBusy(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 6; j++ {
				id := fmt.Sprintf("ses_idx-%d", i)
				if err := upsertIndexRow(dir, ocodeMeta{
					ID:        id,
					Title:     fmt.Sprintf("t-%d-%d", i, j),
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				}); err != nil {
					errs[i] = err
					return
				}
			}
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent upsert %d failed: %v", i, err)
		}
	}
}
