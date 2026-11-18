package umlinfo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProbeRepoIndex(t *testing.T) {
	if os.Getenv("UMLINFO_PROBE") == "" {
		t.Skip("set UMLINFO_PROBE=1 to probe the repo index")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	start := time.Now()
	fp, err := Fingerprint(ctx, root)
	t.Logf("fingerprint: %.2fs err=%v fp=%.12s", time.Since(start).Seconds(), err, fp)
	start = time.Now()
	idx, err := BuildIndex(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("build: %.2fs packages=%d symbols=%d edges=%d monorepo=%s truncated=%v",
		time.Since(start).Seconds(), len(idx.Packages), len(idx.Symbols), len(idx.Edges), idx.Monorepo, idx.Truncated)
	for _, p := range idx.Packages {
		fmt.Printf("%-40s files=%d symbols=%d\n", p.ID, p.FileCount, p.SymbolCount)
	}
}
