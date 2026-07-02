package web

import (
	"testing"
)

func TestFSContainsIndexHTML(t *testing.T) {
	f := FS()
	if f == nil {
		t.Fatal("FS() returned nil; web/dist must be built and embedded")
	}
	if _, err := f.Open("index.html"); err != nil {
		t.Fatalf("index.html not found in embedded SPA: %v", err)
	}
}
