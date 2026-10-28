package cdp

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// TestEgressChromeBlocksMetadataTopLevel drives real Chrome through the egress
// proxy to the cloud metadata IP as the top-level page. The proxy must answer
// 403 with the X-Ocode-Blocked verdict, never relay a metadata response or fail
// with a plain dial error (502). Gated on OCODE_CHROME_PATH like the other
// real-Chrome tests.
func TestEgressChromeBlocksMetadataTopLevel(t *testing.T) {
	chromePath := os.Getenv("OCODE_CHROME_PATH")
	if chromePath == "" {
		t.Skip("OCODE_CHROME_PATH not set; skipping real-Chrome integration test")
	}

	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{GracePeriod: 2 * time.Second})
	m := NewManager(ManagerOptions{
		ChromePath: chromePath,
		Supervisor: sup,
		Dialer:     newBlockListDialer(),
		EmitNav:    func(NavEvent) {},
	})
	defer func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer ccancel()
		_ = m.Close(cctx)
	}()

	sink := &recordingSink{}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	target, err := m.Attach(ctx, "tab:metadata", sink)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	const metadataURL = "http://169.254.169.254/latest/meta-data/"
	if err := target.Navigate(ctx, metadataURL); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		_, _, networks := sink.snapshot()
		for _, n := range networks {
			if !strings.Contains(n.URL, "169.254.169.254") {
				continue
			}
			if n.Status == 200 {
				t.Fatalf("metadata top-level page was relayed: status 200 for %s", n.URL)
			}
			if n.Status == 403 {
				if _, ok := n.ResponseHeaders["X-Ocode-Blocked"]; !ok {
					t.Fatalf("403 without X-Ocode-Blocked verdict: headers=%v", n.ResponseHeaders)
				}
				return
			}
			if n.Status != 0 {
				t.Fatalf("metadata request answered %d, want 403 block (not a dial failure)", n.Status)
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("no 403 block observed for %s within 20s", metadataURL)
}
