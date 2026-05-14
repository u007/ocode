package session

import (
	"os"
	"testing"
)

func TestProjectSlug(t *testing.T) {
	slug1 := getProjectSlug()
	if slug1 == "" {
		t.Error("expected non-empty slug")
	}

	origWd, _ := os.Getwd()
	os.Chdir("/")
	slug2 := getProjectSlug()
	os.Chdir(origWd)

	if slug1 == slug2 {
		t.Errorf("expected different slugs for different directories, got %s and %s", slug1, slug2)
	}
}
