package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectDBConfig_LoadSaveMerge(t *testing.T) {
	tmp := t.TempDir()
	conns := []DBConnection{
		{Name: "proj", Driver: DBDriverPostgres, EnvTemplate: "${DATABASE_URL}"},
	}
	if err := SaveProjectDBConnections(tmp, conns); err != nil {
		t.Fatal(err)
	}
	got, err := LoadProjectDBConnections(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "proj" {
		t.Fatalf("load mismatch: %+v", got)
	}
	// missing file -> nil
	empty := t.TempDir()
	got2, err := LoadProjectDBConnections(empty)
	if err != nil {
		t.Fatal(err)
	}
	if got2 != nil {
		t.Fatalf("expected nil for missing file, got %+v", got2)
	}
	// merge: project wins on name collision
	global := []DBConnection{{Name: "x", Driver: DBDriverPostgres, EnvTemplate: "global"}}
	proj := []DBConnection{{Name: "x", Driver: DBDriverPostgres, EnvTemplate: "project"}}
	merged := MergeDBConnections(global, proj)
	if len(merged) != 1 || merged[0].EnvTemplate != "project" {
		t.Fatalf("merge wrong: %+v", merged)
	}
	// missing file path exists
	if _, err := os.Stat(ProjectDBConfigPath(tmp)); err != nil {
		t.Fatal(err)
	}
	_ = filepath.Join
}

func TestProjectDBConfig_FileMode(t *testing.T) {
	tmp := t.TempDir()
	conns := []DBConnection{{Name: "x", Driver: DBDriverPostgres, EnvTemplate: "${X}"}}
	if err := SaveProjectDBConnections(tmp, conns); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(ProjectDBConfigPath(tmp))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %o, want 0600", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(filepath.Join(tmp, ".ocode"))
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %o, want 0700", dirInfo.Mode().Perm())
	}
}
