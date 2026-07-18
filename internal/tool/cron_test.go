package tool

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/scheduler"
)

func newCronTestService(t *testing.T) *scheduler.Service {
	t.Helper()
	dir := t.TempDir()
	return scheduler.NewService(filepath.Join(dir, "jobs.json"))
}

func TestCronToolAddAndList(t *testing.T) {
	svc := newCronTestService(t)
	ct := &CronTool{Service: svc}
	if ct.Name() != "cron" {
		t.Fatalf("Name: %q", ct.Name())
	}
	// add
	out, err := ct.Execute([]byte(`{"action":"add","name":"ticker","message":"say hi","schedule":{"kind":"every","every_ms":60000}}`))
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if !strings.Contains(out, "scheduled job") {
		t.Fatalf("add returned: %q", out)
	}
	// list
	out, err = ct.Execute([]byte(`{"action":"list"}`))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, `"ticker"`) || !strings.Contains(out, `"jobs"`) {
		t.Fatalf("list returned: %q", out)
	}
}

func TestCronToolAddValidation(t *testing.T) {
	svc := newCronTestService(t)
	ct := &CronTool{Service: svc}
	if _, err := ct.Execute([]byte(`{"action":"add"}`)); err == nil {
		t.Fatal("expected error for empty message")
	}
	if _, err := ct.Execute([]byte(`{"action":"add","message":"x","schedule":{"kind":"at"}}`)); err == nil {
		t.Fatal("expected error for at schedule without at_ms")
	}
}

func TestCronToolRemoveAndDescribe(t *testing.T) {
	svc := newCronTestService(t)
	ct := &CronTool{Service: svc}
	addOut, err := ct.Execute([]byte(`{"action":"add","name":"d","message":"d","schedule":{"kind":"every","every_ms":60000}}`))
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	// Parse id from "scheduled job <id> (...)"
	id := strings.TrimSpace(strings.Split(strings.Fields(addOut)[2], "(")[0])
	if id == "" {
		t.Fatalf("could not extract id from %q", addOut)
	}
	if _, err := ct.Execute([]byte(`{"action":"describe","id":"` + id + `"}`)); err != nil {
		t.Fatalf("describe: %v", err)
	}
	if _, err := ct.Execute([]byte(`{"action":"remove","id":"` + id + `"}`)); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := ct.Execute([]byte(`{"action":"remove","id":"` + id + `"}`)); err == nil {
		t.Fatal("expected error removing same id twice")
	}
}

func TestCronToolWithoutService(t *testing.T) {
	ct := &CronTool{}
	if _, err := ct.Execute([]byte(`{"action":"list"}`)); err == nil {
		t.Fatal("expected error when no service attached")
	}
}

func TestCronToolPermModePassthrough(t *testing.T) {
	svc := newCronTestService(t)
	ct := &CronTool{Service: svc}
	out, err := ct.Execute([]byte(`{"action":"add","message":"yolo run","perm_mode":"yolo","schedule":{"kind":"every","every_ms":60000}}`))
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	// Find the new job and confirm perm_mode persisted.
	jobs := svc.ListJobs()
	if len(jobs) != 1 {
		t.Fatalf("want 1 job, got %d", len(jobs))
	}
	if !strings.Contains(out, "scheduled job") || jobs[0].Payload.PermMode != scheduler.PermYOLO {
		t.Fatalf("perm_mode not persisted: %+v", jobs[0].Payload)
	}
}
