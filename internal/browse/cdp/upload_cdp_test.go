package cdp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// uploadPage is a fake page with one file input at #cv and one text input at #nm.
func uploadPage() *fakePage {
	return &fakePage{
		els: map[string]*fakeEl{
			pathFile: {backendID: 9, count: 1, probe: elementProbe{Tag: "input", Type: "file"}},
			pathNm:   {backendID: 3, count: 1, probe: elementProbe{Tag: "input", Type: "text"}},
		},
		findAll: map[string][]string{"#cv": {pathFile}, "#nm": {pathNm}},
		live:    map[int64]string{9: pathFile, 3: pathNm},
	}
}

// writeFile creates a regular file under a test temp dir and returns its path
// with symlinks resolved (on macOS the temp dir is under /var, a link to /private/var),
// which is the path the upload policy and the browser both see.
func writeFile(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// TestUploadPolicySeesResolvedPath: a symlink inside the allowed root that points
// outside it is judged by where it points, which is where Chrome would read.
func TestUploadPolicySeesResolvedPath(t *testing.T) {
	base := t.TempDir()
	allowedDir := filepath.Join(base, "work")
	secretDir := filepath.Join(base, "secret")
	for _, d := range []string{allowedDir, secretDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(secretDir, "id_rsa"), []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secretDir, filepath.Join(allowedDir, "link")); err != nil {
		t.Fatal(err)
	}
	resolvedAllowed, err := filepath.EvalSymlinks(allowedDir)
	if err != nil {
		t.Fatal(err)
	}
	// The lexical path is under the allowed root; the file it names is not.
	policy := func(p string) error {
		if strings.HasPrefix(p, resolvedAllowed+string(filepath.Separator)) {
			return nil
		}
		return fmt.Errorf("outside the allowed root")
	}
	tgt, s := newPageTarget(t, uploadPage().handle)
	_, err = tgt.UploadFiles(context.Background(), "", "#cv", []string{filepath.Join(allowedDir, "link", "id_rsa")}, nil, policy)
	if err == nil || !strings.Contains(err.Error(), "upload refused") {
		t.Fatalf("UploadFiles(symlink out of root) err = %v, want the policy refusal", err)
	}
	if len(s.calls) != 0 {
		t.Errorf("a symlink out of the root made %d CDP calls", len(s.calls))
	}
}

// TestUploadRejectedPathMakesNoCDPCall: the caller's policy refuses a path, so
// the upload aborts before a single CDP call.
func TestUploadRejectedPathMakesNoCDPCall(t *testing.T) {
	tgt, s := newPageTarget(t, uploadPage().handle)
	file := writeFile(t, "cv.pdf")
	var seen []string
	deny := func(p string) error {
		seen = append(seen, p)
		return fmt.Errorf("outside the allowed roots")
	}
	_, err := tgt.UploadFiles(context.Background(), "", "#cv", []string{file}, nil, deny)
	if err == nil || !strings.Contains(err.Error(), "upload refused") {
		t.Fatalf("UploadFiles err = %v, want the policy refusal", err)
	}
	if len(seen) != 1 || seen[0] != file {
		t.Errorf("policy saw %v, want %s", seen, file)
	}
	if len(s.calls) != 0 {
		t.Errorf("a rejected upload made %d CDP calls: %v", len(s.calls), s.calls)
	}
}

// TestUploadPolicyAbortsOnAnyRejectedPath: one refused path among several aborts
// the whole upload, so none of the files reaches the browser.
func TestUploadPolicyAbortsOnAnyRejectedPath(t *testing.T) {
	tgt, s := newPageTarget(t, uploadPage().handle)
	good := writeFile(t, "a.txt")
	bad := writeFile(t, "b.txt")
	policy := func(p string) error {
		if p == bad {
			return fmt.Errorf("denied")
		}
		return nil
	}
	if _, err := tgt.UploadFiles(context.Background(), "", "#cv", []string{good, bad}, nil, policy); err == nil {
		t.Fatalf("UploadFiles accepted a batch with a refused path")
	}
	if len(s.calls) != 0 {
		t.Errorf("a partly refused batch made %d CDP calls", len(s.calls))
	}
}

func TestUploadPathChecksRunBeforeCDP(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"relative":  "cv.pdf",
		"missing":   filepath.Join(dir, "nope.pdf"),
		"directory": dir,
	}
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			tgt, s := newPageTarget(t, uploadPage().handle)
			if _, err := tgt.UploadFiles(context.Background(), "", "#cv", []string{path}, nil, allowAll); err == nil {
				t.Fatalf("UploadFiles(%s) accepted %q", name, path)
			}
			if len(s.calls) != 0 {
				t.Errorf("%s path made %d CDP calls", name, len(s.calls))
			}
		})
	}
}

func TestUploadNilPolicyAndFileCapRefused(t *testing.T) {
	tgt, s := newPageTarget(t, uploadPage().handle)
	file := writeFile(t, "cv.pdf")
	if _, err := tgt.UploadFiles(context.Background(), "", "#cv", []string{file}, nil, nil); err == nil || !strings.Contains(err.Error(), "nil allowed") {
		t.Errorf("nil policy err = %v", err)
	}
	many := make([]string, uploadMaxFiles+1)
	for i := range many {
		many[i] = file
	}
	if _, err := tgt.UploadFiles(context.Background(), "", "#cv", many, nil, allowAll); err == nil || !strings.Contains(err.Error(), "exceeds the cap") {
		t.Errorf("file cap err = %v", err)
	}
	if len(s.calls) != 0 {
		t.Errorf("refused uploads made %d CDP calls", len(s.calls))
	}
}

func TestUploadSetsFilesByBackendNodeAndReadsBack(t *testing.T) {
	page := uploadPage()
	tgt, s := newPageTarget(t, page.handle)
	file := writeFile(t, "cv.pdf")
	got, err := tgt.UploadFiles(context.Background(), "", "#cv", []string{file}, nil, allowAll)
	if err != nil {
		t.Fatalf("UploadFiles: %v", err)
	}
	if !strings.HasPrefix(got, "uploaded 1 file(s) to #cv: cv.pdf") {
		t.Errorf("result = %q", got)
	}
	if n := s.callCount("DOM.setFileInputFiles"); n != 1 {
		t.Fatalf("DOM.setFileInputFiles calls = %d, want 1", n)
	}
	if got := page.setFiles[0]["backendNodeId"]; got != int64(9) {
		t.Errorf("setFileInputFiles backendNodeId = %v, want 9", got)
	}
	if files, _ := page.setFiles[0]["files"].([]string); len(files) != 1 || files[0] != file {
		t.Errorf("setFileInputFiles files = %v, want [%s]", files, file)
	}
}

func TestUploadRefusesNonFileInputAndMultipleOnSingle(t *testing.T) {
	page := uploadPage()
	tgt, s := newPageTarget(t, page.handle)
	a, b := writeFile(t, "a.txt"), writeFile(t, "b.txt")
	if _, err := tgt.UploadFiles(context.Background(), "", "#nm", []string{a}, nil, allowAll); err == nil || !strings.Contains(err.Error(), "not an <input type=file>") {
		t.Errorf("upload to a text input err = %v", err)
	}
	if _, err := tgt.UploadFiles(context.Background(), "", "#cv", []string{a, b}, nil, allowAll); err == nil || !strings.Contains(err.Error(), "takes one file") {
		t.Errorf("two files to a single input err = %v", err)
	}
	if n := s.callCount("DOM.setFileInputFiles"); n != 0 {
		t.Errorf("refused uploads reached DOM.setFileInputFiles %d times", n)
	}
}
