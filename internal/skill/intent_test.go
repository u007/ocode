package skill

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type intentEntry struct {
	name     string
	body     string
	typeflag byte // 0 means a regular file
	linkname string
}

func intentTarball(t *testing.T, entries []intentEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.body)), Typeflag: tar.TypeReg}
		if e.typeflag != 0 {
			hdr.Typeflag = e.typeflag
			hdr.Linkname = e.linkname
			hdr.Size = 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write header %s: %v", e.name, err)
		}
		if hdr.Size > 0 {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatalf("write body %s: %v", e.name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

func intentSRI(raw []byte) string {
	sum := sha512.Sum512(raw)
	return "sha512-" + base64.StdEncoding.EncodeToString(sum[:])
}

// intentRegistryServer serves a fake npm registry whose latest @tanstack/intent
// is tarball, with integrity as given, and points intentRegistry at it.
func intentRegistryServer(t *testing.T, tarball []byte, integrity string) {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// r.URL.Path is decoded, so the client's %2f arrives as "/".
		switch r.URL.Path {
		case "/@tanstack/intent/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"version": "9.9.9",
				"dist":    map[string]string{"tarball": srv.URL + "/tarball.tgz", "integrity": integrity},
			})
		case "/tarball.tgz":
			_, _ = w.Write(tarball)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	prev := intentRegistry
	intentRegistry = srv.URL
	t.Cleanup(func() { intentRegistry = prev })
}

const intentAlphaSkill = "---\nname: alpha\ndescription: >\n  Use when alpha\n  is needed.\n---\n\n# Alpha\n"

func intentStandardEntries() []intentEntry {
	return []intentEntry{
		{name: "package/package.json", body: "{}"},
		{name: "package/meta/alpha/SKILL.md", body: intentAlphaSkill},
		{name: "package/meta/alpha/references/one.md", body: "one\n"},
		{name: "package/meta/beta/SKILL.md", body: "---\nname: beta\ndescription: Beta skill.\n---\n"},
		{name: "package/meta/templates/workflows/check.yml", body: "on: push\n"},
	}
}

// TestFetchIntentPackageExtractsSkillDirs: each meta/<dir> with a SKILL.md is
// a skill with its whole directory; templates/ (no SKILL.md) is not offered.
func TestFetchIntentPackageExtractsSkillDirs(t *testing.T) {
	tgz := intentTarball(t, intentStandardEntries())
	intentRegistryServer(t, tgz, intentSRI(tgz))

	pkg, err := FetchIntentPackage(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if pkg.Version != "9.9.9" {
		t.Fatalf("version = %q, want 9.9.9", pkg.Version)
	}
	if len(pkg.Skills) != 2 || pkg.Skills[0].Name != "alpha" || pkg.Skills[1].Name != "beta" {
		t.Fatalf("skills = %+v, want [alpha beta]", pkg.Skills)
	}
	alpha := pkg.Skills[0]
	if alpha.Description != "Use when alpha is needed." {
		t.Fatalf("description = %q", alpha.Description)
	}
	if _, ok := alpha.Files["references/one.md"]; !ok {
		t.Fatalf("alpha files %v missing references/one.md", alpha.Files)
	}
}

// TestFetchIntentPackageRejectsIntegrityMismatch: a tarball that does not match
// the registry's sha512 is never extracted.
func TestFetchIntentPackageRejectsIntegrityMismatch(t *testing.T) {
	tgz := intentTarball(t, intentStandardEntries())
	other := intentTarball(t, []intentEntry{{name: "package/meta/x/SKILL.md", body: intentAlphaSkill}})
	intentRegistryServer(t, tgz, intentSRI(other))

	_, err := FetchIntentPackage(context.Background())
	if err == nil || !strings.Contains(err.Error(), "does not match registry integrity") {
		t.Fatalf("err = %v, want integrity mismatch", err)
	}
}

// TestExtractIntentSkillsRejectsTraversal: a ".." segment under meta/ fails the
// whole fetch instead of being skipped or written.
func TestExtractIntentSkillsRejectsTraversal(t *testing.T) {
	tgz := intentTarball(t, []intentEntry{
		{name: "package/meta/alpha/SKILL.md", body: intentAlphaSkill},
		{name: "package/meta/alpha/../../../escape.md", body: "x"},
	})
	_, err := extractIntentSkills(tgz)
	if err == nil || !strings.Contains(err.Error(), "unsafe entry path") {
		t.Fatalf("err = %v, want unsafe entry path", err)
	}
}

// TestExtractIntentSkillsRejectsLinks: a symlink under meta/ is an error, not a
// file to follow or skip.
func TestExtractIntentSkillsRejectsLinks(t *testing.T) {
	tgz := intentTarball(t, []intentEntry{
		{name: "package/meta/alpha/SKILL.md", body: intentAlphaSkill},
		{name: "package/meta/alpha/link.md", typeflag: tar.TypeSymlink, linkname: "/etc/passwd"},
	})
	_, err := extractIntentSkills(tgz)
	if err == nil || !strings.Contains(err.Error(), "unsupported entry") {
		t.Fatalf("err = %v, want unsupported entry", err)
	}
}

// TestInstallIntentSkillsToLifecycle: fresh install writes the directory; a
// second run is a no-op; a locally edited file is backed up before replacement.
func TestInstallIntentSkillsToLifecycle(t *testing.T) {
	pkg := IntentPackage{Version: "9.9.9", Skills: []IntentSkill{{
		Name:  "alpha",
		Files: map[string][]byte{"SKILL.md": []byte(intentAlphaSkill), "references/one.md": []byte("one\n")},
	}}}
	dir := filepath.Join(t.TempDir(), "skills")

	out, err := installIntentSkillsTo(pkg, []string{"alpha"}, dir)
	if err != nil || !strings.Contains(out, "installed alpha") {
		t.Fatalf("first install: out=%q err=%v", out, err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "alpha", "references", "one.md")); string(got) != "one\n" {
		t.Fatalf("reference not written: %q", got)
	}

	out, err = installIntentSkillsTo(pkg, []string{"alpha"}, dir)
	if err != nil || !strings.Contains(out, "already up to date") {
		t.Fatalf("second install: out=%q err=%v", out, err)
	}

	edited := filepath.Join(dir, "alpha", "references", "one.md")
	if err := os.WriteFile(edited, []byte("local edit\n"), 0o644); err != nil {
		t.Fatalf("edit: %v", err)
	}
	out, err = installIntentSkillsTo(pkg, []string{"alpha"}, dir)
	if err != nil || !strings.Contains(out, "updated alpha") {
		t.Fatalf("third install: out=%q err=%v", out, err)
	}
	if got, _ := os.ReadFile(edited); string(got) != "one\n" {
		t.Fatalf("file not restored to bundled body: %q", got)
	}
	backups, _ := filepath.Glob(edited + ".bak.*")
	if len(backups) != 1 {
		t.Fatalf("want one backup of the edited file, got %v", backups)
	}
	if got, _ := os.ReadFile(backups[0]); string(got) != "local edit\n" {
		t.Fatalf("backup body = %q, want the local edit", got)
	}
}

// TestInstallIntentSkillsToSymlinkAndUnknown: a symlinked target is refused
// inline, and an unknown name fails before anything is written.
func TestInstallIntentSkillsToSymlinkAndUnknown(t *testing.T) {
	pkg := IntentPackage{Version: "9.9.9", Skills: []IntentSkill{{
		Name:  "alpha",
		Files: map[string][]byte{"SKILL.md": []byte(intentAlphaSkill)},
	}}}
	root := t.TempDir()
	dir := filepath.Join(root, "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(root, filepath.Join(dir, "alpha")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	out, err := installIntentSkillsTo(pkg, []string{"alpha"}, dir)
	if err != nil || !strings.Contains(out, "! alpha: ") || !strings.Contains(out, "symlink") {
		t.Fatalf("symlink install: out=%q err=%v", out, err)
	}

	_, err = installIntentSkillsTo(pkg, []string{"nope"}, dir)
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("no skill %q", "nope")) {
		t.Fatalf("unknown name: err=%v", err)
	}
}

// TestFetchIntentPackageHonoursCancellation: a cancelled caller context aborts
// the registry request instead of waiting out the client timeout.
func TestFetchIntentPackageHonoursCancellation(t *testing.T) {
	tgz := intentTarball(t, intentStandardEntries())
	intentRegistryServer(t, tgz, intentSRI(tgz))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := FetchIntentPackage(ctx)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
