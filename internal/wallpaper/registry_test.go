package wallpaper

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func isolateDataDir(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("LOCALAPPDATA", home)
}

func TestWallpaperPath_RejectsTraversalInUploadID(t *testing.T) {
	isolateDataDir(t)
	for _, id := range []string{
		"upload-../../etc/passwd",
		"upload-..",
		"upload-x/y",
		"upload-" + strings.Repeat("a", 31),
		"upload-" + strings.Repeat("G", 32),
	} {
		if _, err := WallpaperPath(id); err == nil {
			t.Errorf("WallpaperPath(%q) accepted a malformed upload id", id)
		}
	}
}

func TestWallpaperPath_AcceptsGeneratedUploadID(t *testing.T) {
	isolateDataDir(t)
	id := "upload-" + strings.Repeat("0f", 16)
	p, err := WallpaperPath(id)
	if err != nil {
		t.Fatalf("WallpaperPath(%q): %v", id, err)
	}
	dir, _ := UserUploadDir()
	if filepath.Dir(p) != dir {
		t.Fatalf("path %q escapes upload dir %q", p, dir)
	}
}

func TestRegisterUserUpload_RejectsActiveSVGContent(t *testing.T) {
	isolateDataDir(t)
	cases := map[string]string{
		"script":          `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
		"event handler":   `<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"></svg>`,
		"foreignObject":   `<svg xmlns="http://www.w3.org/2000/svg"><foreignObject><body xmlns="http://www.w3.org/1999/xhtml">x</body></foreignObject></svg>`,
		"javascript href": `<svg xmlns="http://www.w3.org/2000/svg"><a href="javascript:alert(1)"><text>x</text></a></svg>`,
		"mixed case":      `<svg xmlns="http://www.w3.org/2000/svg"><SCRIPT>alert(1)</SCRIPT></svg>`,
	}
	for name, svg := range cases {
		if _, err := RegisterUserUpload("x.svg", []byte(svg)); err == nil {
			t.Errorf("%s: upload accepted", name)
		}
	}
	dir, _ := UserUploadDir()
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("rejected uploads were written to disk: %d files", len(entries))
	}
}

func TestRegisterUserUpload_AcceptsPlainSVG(t *testing.T) {
	isolateDataDir(t)
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><rect width="10" height="10" fill="#333"/></svg>`
	meta, err := RegisterUserUpload("x.svg", []byte(svg))
	if err != nil {
		t.Fatalf("upload rejected: %v", err)
	}
	if !strings.HasPrefix(meta.ID, "upload-") {
		t.Fatalf("unexpected id %q", meta.ID)
	}
	if _, err := ReadWallpaperFile(meta.ID); err != nil {
		t.Fatalf("read back: %v", err)
	}
}

func TestDetectContentType_ShortInputDoesNotPanic(t *testing.T) {
	for _, in := range []string{"", "a", "<svg", "<?xm"} {
		if got := detectContentType([]byte(in)); got == "image/svg+xml" && in != "<svg" {
			t.Errorf("%q detected as svg", in)
		}
	}
}
