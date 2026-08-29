package server

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandleFileSearchPaginationAndExactLine(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	// Create files with known content
	// a.txt has 5 matching lines
	if err := os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("hello world\nfoo bar\nhello again\nHELLO case\nhello world again\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("nothing\nhello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Test basic search
	w := httptest.NewRecorder()
	q := url.QueryEscape("hello")
	r := httptest.NewRequest("GET", "/api/files/search?q="+q, nil)
	h.HandleFileSearch(w, r)
	if w.Code != 200 {
		t.Fatalf("search failed %d: %s", w.Code, w.Body.String())
	}
	var resp FileSearchResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Results) == 0 {
		t.Fatal("expected results")
	}
	// Should be case-insensitive: 4 from a.txt +1 from b.txt =5
	if len(resp.Results) != 5 {
		t.Fatalf("expected 5 results, got %d: %+v", len(resp.Results), resp.Results)
	}
	// Exact line preserved
	if resp.Results[0].Text != "hello world" {
		t.Fatalf("exact line mismatch got %q", resp.Results[0].Text)
	}
	if resp.Total != 5 {
		t.Fatalf("total should be 5, got %d", resp.Total)
	}
	if resp.HasMore {
		t.Fatal("hasMore should be false for 5 results with default limit 50")
	}
	// Pagination: limit 2 offset 0
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("GET", "/api/files/search?q="+q+"&limit=2&offset=0", nil)
	h.HandleFileSearch(w2, r2)
	var resp2 FileSearchResponse
	if err := json.Unmarshal(w2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("decode2: %v", err)
	}
	if len(resp2.Results) != 2 {
		t.Fatalf("expected 2 results page1, got %d", len(resp2.Results))
	}
	if !resp2.HasMore {
		t.Fatal("hasMore should be true when 5 >2")
	}
	if resp2.Total != 5 {
		t.Fatalf("total should still be 5, got %d", resp2.Total)
	}
	// Page 2 offset 2 limit 2
	w3 := httptest.NewRecorder()
	r3 := httptest.NewRequest("GET", "/api/files/search?q="+q+"&limit=2&offset=2", nil)
	h.HandleFileSearch(w3, r3)
	var resp3 FileSearchResponse
	if err := json.Unmarshal(w3.Body.Bytes(), &resp3); err != nil {
		t.Fatalf("decode3: %v", err)
	}
	if len(resp3.Results) != 2 {
		t.Fatalf("expected 2 results page2, got %d", len(resp3.Results))
	}
	// Last page offset 4 limit 2 should have 1
	w4 := httptest.NewRecorder()
	r4 := httptest.NewRequest("GET", "/api/files/search?q="+q+"&limit=2&offset=4", nil)
	h.HandleFileSearch(w4, r4)
	var resp4 FileSearchResponse
	if err := json.Unmarshal(w4.Body.Bytes(), &resp4); err != nil {
		t.Fatalf("decode4: %v", err)
	}
	if len(resp4.Results) != 1 {
		t.Fatalf("expected 1 result last page, got %d", len(resp4.Results))
	}
	if resp4.HasMore {
		t.Fatal("hasMore should be false on last page")
	}
	// Offset beyond total
	w5 := httptest.NewRecorder()
	r5 := httptest.NewRequest("GET", "/api/files/search?q="+q+"&limit=2&offset=10", nil)
	h.HandleFileSearch(w5, r5)
	var resp5 FileSearchResponse
	if err := json.Unmarshal(w5.Body.Bytes(), &resp5); err != nil {
		t.Fatalf("decode5: %v", err)
	}
	if len(resp5.Results) != 0 {
		t.Fatalf("expected 0 results beyond total, got %d", len(resp5.Results))
	}
	if resp5.HasMore {
		t.Fatal("hasMore should be false beyond total")
	}
}

func TestHandleFileSearchExactLineUnicodeAndLongLine(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	// Unicode line: should not be split by byte truncation
	unicodeLine := "hello " + strings.Repeat("é", 10) + " world" // é is 2 bytes, 10 => 20 bytes, total runes ~ 22
	if err := os.WriteFile(filepath.Join(tmpDir, "uni.txt"), []byte(unicodeLine+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Very long line >2000 runes should be rune-truncated, not byte-split
	longLine := "hello " + strings.Repeat("a", 3000)
	if err := os.WriteFile(filepath.Join(tmpDir, "long.txt"), []byte(longLine+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	q := url.QueryEscape("hello")
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/files/search?q="+q, nil)
	h.HandleFileSearch(w, r)
	var resp FileSearchResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Find uni.txt result
	foundUni := false
	for _, res := range resp.Results {
		if res.Path == "uni.txt" {
			foundUni = true
			if res.Text != unicodeLine {
				t.Fatalf("unicode exact line not preserved, got %q want %q", res.Text, unicodeLine)
			}
			// Ensure valid UTF-8
			if !strings.Contains(res.Text, "é") {
				t.Fatal("unicode char lost")
			}
		}
		if res.Path == "long.txt" {
			if len([]rune(res.Text)) != 2000 {
				t.Fatalf("long line should be truncated to 2000 runes, got %d", len([]rune(res.Text)))
			}
			// Ensure valid UTF-8 and starts with hello
			if !strings.HasPrefix(res.Text, "hello") {
				t.Fatalf("long line prefix lost %q", res.Text[:10])
			}
		}
	}
	if !foundUni {
		t.Fatal("uni.txt not found in results")
	}
	// Check scanner handles long line up to 1MiB (previous limit was 16KiB)
	// Create a file with a single 100KiB line containing hello
	hugeLine := "hello " + strings.Repeat("x", 100*1024)
	tmpFile := filepath.Join(tmpDir, "huge.txt")
	if err := os.WriteFile(tmpFile, []byte(hugeLine+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("GET", "/api/files/search?q="+q, nil)
	h.HandleFileSearch(w2, r2)
	var resp2 FileSearchResponse
	if err := json.Unmarshal(w2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("decode huge: %v", err)
	}
	foundHuge := false
	for _, res := range resp2.Results {
		if res.Path == "huge.txt" {
			foundHuge = true
			if !strings.HasPrefix(res.Text, "hello") {
				t.Fatalf("huge line not found correctly")
			}
		}
	}
	if !foundHuge {
		t.Fatal("huge.txt with 100KiB line should be found, scanner limit was increased to 1MiB")
	}
}

func TestHandleFileSearchRootCollision(t *testing.T) {
	// Two projects with same relative path should not collide when using project param
	h1, tmp1 := newFilesHandler(t)
	h2, tmp2 := newFilesHandler(t)
	// Both have same file name but different content
	if err := os.WriteFile(filepath.Join(tmp1, "same.txt"), []byte("hello from project1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp2, "same.txt"), []byte("hello from project2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	q := url.QueryEscape("hello")
	// Search in project1 should only return its file
	w1 := httptest.NewRecorder()
	r1 := httptest.NewRequest("GET", "/api/files/search?q="+q+"&path="+url.QueryEscape(tmp1), nil)
	// Need to allow project path: register as allowed root
	h1.SetWorkDir(tmp1)
	// For h2 similarly
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("GET", "/api/files/search?q="+q+"&path="+url.QueryEscape(tmp2), nil)
	h2.SetWorkDir(tmp2)
	h1.HandleFileSearch(w1, r1)
	h2.HandleFileSearch(w2, r2)
	var resp1, resp2 FileSearchResponse
	if err := json.Unmarshal(w1.Body.Bytes(), &resp1); err != nil {
		t.Fatalf("decode1: %v", err)
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("decode2: %v", err)
	}
	if len(resp1.Results) != 1 || resp1.Results[0].Path != "same.txt" {
		t.Fatalf("project1 search wrong %+v", resp1.Results)
	}
	if len(resp2.Results) != 1 || resp2.Results[0].Path != "same.txt" {
		t.Fatalf("project2 search wrong %+v", resp2.Results)
	}
	// Ensure they don't leak: h1 searching with project2 path should fail with 400
	w3 := httptest.NewRecorder()
	r3 := httptest.NewRequest("GET", "/api/files/search?q="+q+"&path="+url.QueryEscape(tmp2), nil)
	// h1's workDir is tmp1, so tmp2 is outside
	h1.HandleFileSearch(w3, r3)
	if w3.Code != 400 {
		t.Fatalf("expected 400 for outside path, got %d", w3.Code)
	}
}
