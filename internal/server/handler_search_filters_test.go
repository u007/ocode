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

func setupFilterTestDir(t *testing.T) (*Handler, string) {
	t.Helper()
	h, tmp := newFilesHandler(t)
	// a.go has varied cases and word boundaries
	mustWrite(t, filepath.Join(tmp, "a.go"), "hello world\nHello World\nhello_world\nworld hello\nHELLO\n")
	mustWrite(t, filepath.Join(tmp, "b.txt"), "hello world\n")
	mustWrite(t, filepath.Join(tmp, "c.log"), "hello log\n")
	mustWrite(t, filepath.Join(tmp, "d.min.js"), "hello min\n")
	mustWrite(t, filepath.Join(tmp, ".hidden"), "hello hidden\n")
	if err := os.Mkdir(filepath.Join(tmp, "dist"), 0755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(tmp, "dist", "ignore.go"), "hello dist\n")
	if err := os.Mkdir(filepath.Join(tmp, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(tmp, "sub", "deep.log"), "hello deep\n")
	return h, tmp
}

func mustWrite(t *testing.T, p, c string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(c), 0644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

func searchWith(t *testing.T, h *Handler, q string) (int, []FileSearchResult, int) {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/files/search?"+q, nil)
	h.HandleFileSearch(w, r)
	if w.Code != 200 {
		t.Fatalf("search %q failed %d: %s", q, w.Code, w.Body.String())
	}
	var resp FileSearchResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return w.Code, resp.Results, resp.Total
}

func TestSearchFilters_CaseSensitive(t *testing.T) {
	h, _ := setupFilterTestDir(t)
	// default case-insensitive: matches Hello and HELLO
	_, res, _ := searchWith(t, h, "q="+url.QueryEscape("hello"))
	if len(res) < 5 {
		t.Fatalf("default should be case-insensitive, got %d results", len(res))
	}
	// caseSensitive true: should not match "Hello World" nor "HELLO"
	_, res2, _ := searchWith(t, h, "q="+url.QueryEscape("hello")+"&caseSensitive=1")
	// Count lines with exact "hello" lowercase: a.go:1, a.go:3, b.txt:1, c.log, d.min.js, dist/ignore.go, sub/deep.log = 7 but dist and hidden excluded by default? Check: hidden excluded, dist included. So expect fewer with caseSensitive.
	hasHelloWorld := false
	for _, r := range res2 {
		if strings.Contains(r.Text, "Hello") || strings.Contains(r.Text, "HELLO") {
			hasHelloWorld = true
		}
	}
	if hasHelloWorld {
		t.Fatalf("caseSensitive should not match Hello/HELLO, got %+v", res2)
	}
	// alias param "case=1" should also work
	_, res3, _ := searchWith(t, h, "q="+url.QueryEscape("hello")+"&case=1")
	if len(res2) != len(res3) {
		t.Fatalf("case alias mismatch %d vs %d", len(res2), len(res3))
	}
}

func TestSearchFilters_WholeWord(t *testing.T) {
	h, _ := setupFilterTestDir(t)
	_, resDefault, _ := searchWith(t, h, "q="+url.QueryEscape("hello"))
	_, resWhole, _ := searchWith(t, h, "q="+url.QueryEscape("hello")+"&wholeWord=1")
	if len(resWhole) >= len(resDefault) {
		t.Fatalf("wholeWord should filter more: default %d whole %d", len(resDefault), len(resWhole))
	}
	for _, r := range resWhole {
		if r.Text == "hello_world" {
			t.Fatalf("wholeWord should not match hello_world, got %+v", r)
		}
	}
}

func TestSearchFilters_Regex(t *testing.T) {
	h, _ := setupFilterTestDir(t)
	// regex h.*o should match hello lines
	_, res, _ := searchWith(t, h, "q="+url.QueryEscape("h.*o")+"&regex=1")
	if len(res) == 0 {
		t.Fatalf("regex should match")
	}
	// literal .* should not match (treated as literal)
	_, resLit, _ := searchWith(t, h, "q="+url.QueryEscape("h.*o"))
	if len(resLit) != 0 {
		t.Fatalf("literal h.*o should not match, got %d", len(resLit))
	}
	// match=regex param alias
	_, resAlias, _ := searchWith(t, h, "q="+url.QueryEscape("h.*o")+"&match=regex")
	if len(resAlias) == 0 {
		t.Fatalf("match=regex alias should work")
	}
}

func TestSearchFilters_InvalidRegex(t *testing.T) {
	h, _ := setupFilterTestDir(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/files/search?q="+url.QueryEscape("[")+"&regex=1", nil)
	h.HandleFileSearch(w, r)
	if w.Code != 400 {
		t.Fatalf("invalid regex should 400, got %d %s", w.Code, w.Body.String())
	}
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("GET", "/api/files/search/stream?q="+url.QueryEscape("[")+"&regex=1", nil)
	h.HandleFileSearchStream(w2, r2)
	if w2.Code != 400 {
		t.Fatalf("invalid regex stream should 400, got %d %s", w2.Code, w2.Body.String())
	}
}

func TestSearchFilters_Exts(t *testing.T) {
	h, _ := setupFilterTestDir(t)
	_, resGo, _ := searchWith(t, h, "q="+url.QueryEscape("hello")+"&exts=go")
	for _, r := range resGo {
		if filepath.Ext(r.Path) != ".go" {
			t.Fatalf("exts=go should only return .go, got %s", r.Path)
		}
	}
	_, resMulti, _ := searchWith(t, h, "q="+url.QueryEscape("hello")+"&exts=*.go,*.txt")
	for _, r := range resMulti {
		ext := strings.ToLower(filepath.Ext(r.Path))
		if ext != ".go" && ext != ".txt" {
			t.Fatalf("exts *.go,*.txt got %s", r.Path)
		}
	}
}

func TestSearchFilters_Ignore(t *testing.T) {
	h, _ := setupFilterTestDir(t)
	_, resDefault, _ := searchWith(t, h, "q="+url.QueryEscape("hello"))
	_, resIgnoreLog, _ := searchWith(t, h, "q="+url.QueryEscape("hello")+"&ignore=*.log")
	if len(resIgnoreLog) >= len(resDefault) {
		t.Fatalf("ignore *.log should reduce results %d vs %d", len(resIgnoreLog), len(resDefault))
	}
	for _, r := range resIgnoreLog {
		if strings.HasSuffix(r.Path, ".log") {
			t.Fatalf("ignore *.log should exclude %s", r.Path)
		}
	}
	// dist/** should exclude dist/ignore.go
	_, resIgnoreDist, _ := searchWith(t, h, "q="+url.QueryEscape("hello")+"&ignore=dist/**")
	for _, r := range resIgnoreDist {
		if strings.HasPrefix(r.Path, "dist/") {
			t.Fatalf("ignore dist/** should exclude %s", r.Path)
		}
	}
	// comma multiple
	_, resMulti, _ := searchWith(t, h, "q="+url.QueryEscape("hello")+"&ignore=*.log,*.txt")
	for _, r := range resMulti {
		if strings.HasSuffix(r.Path, ".log") || strings.HasSuffix(r.Path, ".txt") {
			t.Fatalf("ignore multiple should exclude %s", r.Path)
		}
	}
	// patterns without slash match basename at any depth
	_, resBase, _ := searchWith(t, h, "q="+url.QueryEscape("hello")+"&ignore=deep.log")
	for _, r := range resBase {
		if filepath.Base(r.Path) == "deep.log" {
			t.Fatalf("ignore deep.log basename should exclude %s", r.Path)
		}
	}
	// exclude alias
	_, resExclude, _ := searchWith(t, h, "q="+url.QueryEscape("hello")+"&exclude=*.log")
	if len(resExclude) != len(resIgnoreLog) {
		t.Fatalf("exclude alias should match ignore")
	}
}

func TestSearchFilters_IncludeIgnored(t *testing.T) {
	h, _ := setupFilterTestDir(t)
	_, resDefault, _ := searchWith(t, h, "q="+url.QueryEscape("hello"))
	hasHiddenDefault := false
	for _, r := range resDefault {
		if r.Path == ".hidden" {
			hasHiddenDefault = true
		}
	}
	if hasHiddenDefault {
		t.Fatalf("default should not include .hidden")
	}
	_, resInc, _ := searchWith(t, h, "q="+url.QueryEscape("hello")+"&includeIgnored=1")
	hasHidden := false
	for _, r := range resInc {
		if r.Path == ".hidden" {
			hasHidden = true
		}
	}
	if !hasHidden {
		t.Fatalf("includeIgnored should include .hidden, got %+v", resInc)
	}
}

func TestSearchFilters_StreamParity(t *testing.T) {
	h, _ := setupFilterTestDir(t)
	cases := []string{
		"q=" + url.QueryEscape("hello"),
		"q=" + url.QueryEscape("hello") + "&caseSensitive=1",
		"q=" + url.QueryEscape("hello") + "&wholeWord=1",
		"q=" + url.QueryEscape("h.*o") + "&regex=1",
		"q=" + url.QueryEscape("hello") + "&exts=go",
		"q=" + url.QueryEscape("hello") + "&ignore=*.log",
		"q=" + url.QueryEscape("hello") + "&includeIgnored=1",
		"q=" + url.QueryEscape("hello") + "&wholeWord=1&caseSensitive=1",
	}
	for _, q := range cases {
		t.Run(q, func(t *testing.T) {
			// paged
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/api/files/search?"+q, nil)
			h.HandleFileSearch(w, r)
			if w.Code != 200 {
				t.Fatalf("paged %q failed %d: %s", q, w.Code, w.Body.String())
			}
			var presp FileSearchResponse
			if err := json.Unmarshal(w.Body.Bytes(), &presp); err != nil {
				t.Fatalf("paged decode: %v", err)
			}
			// stream
			w2 := httptest.NewRecorder()
			r2 := httptest.NewRequest("GET", "/api/files/search/stream?"+q, nil)
			h.HandleFileSearchStream(w2, r2)
			if w2.Code != 200 {
				t.Fatalf("stream %q failed %d: %s", q, w2.Code, w2.Body.String())
			}
			frames := parseSSEFrames(t, w2.Body.String())
			streamResults, done := flattenStreamResults(t, frames)
			if len(streamResults) != len(presp.Results) {
				t.Fatalf("stream/paged count mismatch for %q: stream %d paged %d", q, len(streamResults), len(presp.Results))
			}
			if done.Total != presp.Total {
				t.Fatalf("done.Total %d != paged Total %d for %q", done.Total, presp.Total, q)
			}
			if done.Capped != presp.Capped || done.Truncated != presp.Truncated {
				t.Fatalf("cap mismatch stream capped=%v truncated=%v paged capped=%v truncated=%v for %q", done.Capped, done.Truncated, presp.Capped, presp.Truncated, q)
			}
			// Exact parity of paths/lines/text in order (both walk filesystem order)
			for i := range presp.Results {
				a, b := presp.Results[i], streamResults[i]
				if a.Path != b.Path || a.Line != b.Line || a.Text != b.Text {
					t.Fatalf("result %d mismatch paged %+v vs stream %+v for %q", i, a, b, q)
				}
			}
		})
	}
}
