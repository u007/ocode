package server

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/browse/cdp"
	"github.com/u007/ocode/internal/browsertabs"
)

// TestRedactBrowserURL pins what a URL keeps: scheme, host and path. A query or
// fragment becomes a marker, userinfo is dropped, and a URL with no host keeps
// only its scheme.
func TestRedactBrowserURL(t *testing.T) {
	cases := map[string]string{
		"https://api.example.com/v1/items?token=abc123&page=2#section": "https://api.example.com/v1/items?…#…",
		"https://user:pw@files.example.com/a/b":                        "https://files.example.com/a/b",
		"https://example.com/":                                         "https://example.com/",
		"http://localhost:3000/app?x=1":                                "http://localhost:3000/app?…",
		"https://example.com/p#frag":                                   "https://example.com/p#…",
		"data:text/plain,secret":                                       "data:…",
		"https://[::1":                                                 "[unparseable URL redacted]",
	}
	for in, want := range cases {
		if got := redactBrowserURL(in); got != want {
			t.Errorf("redactBrowserURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestRedactBrowserURLsInText pins that the text redactor finds the URLs in a
// page's words, redacts each, and leaves the words and sentence punctuation.
func TestRedactBrowserURLsInText(t *testing.T) {
	in := "Failed to load https://cdn.example.com/a.js?sig=zzz, see (https://x.example/y#k)."
	want := "Failed to load https://cdn.example.com/a.js?…, see (https://x.example/y#…)."
	if got := redactBrowserURLsInText(in); got != want {
		t.Fatalf("redactBrowserURLsInText = %q, want %q", got, want)
	}
	plain := "no url here: just 'quoted' text."
	if got := redactBrowserURLsInText(plain); got != plain {
		t.Fatalf("text without a URL changed: %q", got)
	}
}

// redactionTool returns a browser tool whose strip tab b1 is on tabURL.
func redactionTool(t *testing.T, surf *fakeSurface, tabURL string) *browserTool {
	t.Helper()
	store, err := browsertabs.NewStoreAt(filepath.Join(t.TempDir(), "browser-tabs.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if err := store.ApplyBulk(map[string]browsertabs.ProjectBrowsers{
		testProjectKey: {
			Tabs:     []browsertabs.Tab{{ID: "b1", Title: "Callback"}},
			ActiveID: "b1",
			Surfaces: map[string]browsertabs.Surface{
				"b1": {URL: tabURL, History: []string{tabURL}},
			},
		},
	}); err != nil {
		t.Fatalf("seed ApplyBulk: %v", err)
	}
	return newBrowserTool(testProjectKey, 1568, store, &fakeSurfaces{surf: surf})
}

// TestBrowserTool_NetworkAndTabsElideQueryTokens pins that a token in a request
// URL or in a tab's URL never reaches the model, and that the elision shows.
func TestBrowserTool_NetworkAndTabsElideQueryTokens(t *testing.T) {
	surf := &fakeSurface{network: []cdp.NetworkEntry{{
		Method: "GET", URL: "https://api.example.com/v1/me?access_token=SECRET#frag",
		ResourceType: "XHR", Status: 200,
	}}}
	b := redactionTool(t, surf, "https://one.example/cb?code=SECRET")

	tabs, err := browserCall(t, b, `{"action":"tabs"}`)
	if err != nil {
		t.Fatalf("tabs: %v", err)
	}
	if strings.Contains(tabs, "SECRET") || !strings.Contains(tabs, "https://one.example/cb?…") {
		t.Fatalf("tabs output did not elide the tab URL's query:\n%s", tabs)
	}

	network, err := browserCall(t, b, `{"action":"network"}`)
	if err != nil {
		t.Fatalf("network: %v", err)
	}
	if strings.Contains(network, "SECRET") || strings.Contains(network, "access_token") {
		t.Fatalf("network output kept the query token:\n%s", network)
	}
	if !strings.Contains(network, `"https://api.example.com/v1/me?…#…"`) {
		t.Fatalf("network output lacks the redacted URL with both markers:\n%s", network)
	}
}

// TestBrowserTool_ConsoleRedactsURLsInTextAndLocation pins that URLs inside console
// and exception text, and in an exception's location, are redacted, and that the
// rest of the entry stays readable.
func TestBrowserTool_ConsoleRedactsURLsInTextAndLocation(t *testing.T) {
	surf := &fakeSurface{console: []cdp.ConsoleEntry{{
		Level: "error", Kind: "exception",
		Text:     "Boom fetching https://api.example.com/cb?token=SECRET",
		Location: "https://app.example.com/main.js?v=SECRET:1:2",
	}}}
	b := redactionTool(t, surf, "https://one.example/")
	out, err := browserCall(t, b, `{"action":"console"}`)
	if err != nil {
		t.Fatalf("console: %v", err)
	}
	if strings.Contains(out, "SECRET") {
		t.Fatalf("console output kept a query token:\n%s", out)
	}
	for _, want := range []string{`"Boom fetching https://api.example.com/cb?…"`, "at https://app.example.com/main.js?…"} {
		if !strings.Contains(out, want) {
			t.Errorf("console output lacks %q:\n%s", want, out)
		}
	}
}
