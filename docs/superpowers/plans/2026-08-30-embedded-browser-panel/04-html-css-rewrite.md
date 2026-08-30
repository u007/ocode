# Part 04 — HTML/CSS Rewrite Engine

**Spec:** `docs/superpowers/specs/2026-08-30-embedded-browser-panel-design.md` (§ External mode → Rewrite surface).

**Goal:** Rewrite every URL-bearing construct in proxied HTML and CSS so it points back through the browse origin at `/b/{stateKey}/{scheme}/{host}/{path}`. This covers the *static* rewrite surface only. URLs constructed at runtime by JavaScript (`fetch("https://…")`, dynamic `import()`, `new WebSocket(…)`) are **not** reachable here and are handled client-side by the injected capture script in Part 05 — a limitation called out honestly in code comments and in the spec's "known to break" list.

**Dependency note:** `golang.org/x/net v0.56.0` is already in `go.mod` (indirect — line 88). Part 04's first import of `golang.org/x/net/html` makes it direct; run `go mod tidy` in Step 3 to move it out of the `// indirect` block. No version change, no new pin needed (it is already pinned to v0.56.0).

**Files:**
- Create: `internal/browse/rewrite.go`, `internal/browse/rewrite_test.go`

**Interfaces:**
- Consumes: `target` (Part 01).
- Produces: `mapURL(raw string, t target, base string) string`, `rewriteHTML(body []byte, t target, base string) ([]byte, error)`, `rewriteCSS(body []byte, t target) []byte` — all consumed by Part 03 (external fetch pipeline) and Part 05 (capture injection runs after rewrite).

**Rewrite surface (authoritative checklist for this part):**
- Attributes: `href`, `src`, `action`, `poster`, `data`, plus `formaction`.
- `srcset` (on `<img>`/`<source>`): comma-separated list of `url [descriptor]` — rewrite each URL, preserve descriptors.
- Inline `style="…url(…)…"` attributes.
- `<base href>`: its value becomes the resolution base for the rest of the document, then the element is dropped (its href would otherwise re-point the browser at the real origin).
- `<meta http-equiv="refresh" content="N;url=…">`: rewrite the URL portion.
- `<link rel="preload|prefetch|modulepreload|stylesheet|icon">` `href`.
- Strip `integrity` on any `<script>`/`<link>` whose URL we rewrote (rewritten bytes can never match the upstream SRI hash → browser refuses the resource).
- Untouched schemes/targets: `data:`, `blob:`, `javascript:`, `mailto:`, `tel:`, and pure `#fragment` anchors.

---

- [ ] **Step 1: Write the failing `mapURL` test**

Create `internal/browse/rewrite_test.go`:

```go
package browse

import (
	"strings"
	"testing"
)

func tgt() target {
	return target{StateKey: "tab:abc", Scheme: "https", Host: "example.com", Path: "/page/", Local: false}
}

func TestMapURL(t *testing.T) {
	base := "" // no <base>; resolution base is the document target itself
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"absolute https", "https://cdn.other.com/a.js", "/b/tab:abc/https/cdn.other.com/a.js"},
		{"absolute http", "http://img.x.com/p.png", "/b/tab:abc/http/img.x.com/p.png"},
		{"protocol-relative", "//cdn.other.com/a.js", "/b/tab:abc/https/cdn.other.com/a.js"},
		{"root-relative", "/assets/app.js", "/b/tab:abc/https/example.com/assets/app.js"},
		{"relative path", "sub/x.css", "/b/tab:abc/https/example.com/page/sub/x.css"},
		{"dotdot relative", "../y.css", "/b/tab:abc/https/example.com/y.css"},
		{"query preserved", "/api?q=1&x=2", "/b/tab:abc/https/example.com/api?q=1&x=2"},
		{"data untouched", "data:image/png;base64,AAAA", "data:image/png;base64,AAAA"},
		{"blob untouched", "blob:https://example.com/uuid", "blob:https://example.com/uuid"},
		{"javascript untouched", "javascript:void(0)", "javascript:void(0)"},
		{"mailto untouched", "mailto:a@b.com", "mailto:a@b.com"},
		{"anchor untouched", "#section", "#section"},
		{"empty untouched", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := mapURL(c.raw, tgt(), base)
			if got != c.want {
				t.Fatalf("mapURL(%q) = %q, want %q", c.raw, got, c.want)
			}
		})
	}
}

func TestMapURLWithBase(t *testing.T) {
	// <base href="https://example.com/app/"> shifts relative resolution.
	got := mapURL("x.js", tgt(), "https://example.com/app/")
	want := "/b/tab:abc/https/example.com/app/x.js"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	// Absolute base to another host.
	got2 := mapURL("y.js", tgt(), "https://cdn.z.com/lib/")
	if !strings.HasPrefix(got2, "/b/tab:abc/https/cdn.z.com/lib/y.js") {
		t.Fatalf("got %q", got2)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/browse/ -run TestMapURL`
Expected: FAIL — `undefined: mapURL`.

- [ ] **Step 3: Implement `mapURL` (and tidy the dependency)**

Create `internal/browse/rewrite.go`:

```go
package browse

import (
	"bytes"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// LIMITATION: this file rewrites only URLs that appear literally in the HTML/CSS
// byte stream. URLs constructed at runtime by page JavaScript — fetch(),
// XMLHttpRequest, dynamic import(), new WebSocket(), Worker() — are invisible
// here and are rerouted client-side by the injected capture script (Part 05).
// Some exotic loaders and cross-origin API calls from workers will still escape
// both layers; those land in the Network console as visible errors, and the
// site is flagged "degraded" in the status row. This is by design (spec:
// external mode is best-effort).

// untouchedScheme reports schemes/targets that must never be rewritten.
func untouchedScheme(raw string) bool {
	r := strings.TrimSpace(raw)
	if r == "" || strings.HasPrefix(r, "#") {
		return true
	}
	low := strings.ToLower(r)
	for _, p := range []string{"data:", "blob:", "javascript:", "mailto:", "tel:", "about:", "cid:"} {
		if strings.HasPrefix(low, p) {
			return true
		}
	}
	return false
}

// resolutionBase returns the absolute URL that relative references resolve
// against: the <base href> if present and absolute, else the document target.
func resolutionBase(t target, base string) *url.URL {
	docURL := &url.URL{Scheme: t.Scheme, Host: t.Host, Path: t.Path}
	if base == "" {
		return docURL
	}
	b, err := url.Parse(base)
	if err != nil || b.Host == "" {
		// Relative <base> resolves against the document URL.
		if err == nil {
			return docURL.ResolveReference(b)
		}
		return docURL
	}
	return b
}

// mapURL resolves raw against the document base and returns a browse-origin
// path /b/{stateKey}/{scheme}/{host}/{path}?{query}. Untouched schemes and
// anchors pass through verbatim.
func mapURL(raw string, t target, base string) string {
	if untouchedScheme(raw) {
		return raw
	}
	ref, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		// Unparseable URL: leave it alone rather than emit a broken /b/ path.
		return raw
	}
	abs := resolutionBase(t, base).ResolveReference(ref)
	if abs.Scheme != "http" && abs.Scheme != "https" {
		return raw
	}
	path := abs.EscapedPath()
	if path == "" {
		path = "/"
	}
	out := fmt.Sprintf("/b/%s/%s/%s%s", t.StateKey, abs.Scheme, abs.Host, path)
	if abs.RawQuery != "" {
		out += "?" + abs.RawQuery
	}
	if abs.Fragment != "" {
		out += "#" + abs.Fragment
	}
	return out
}
```

Run: `go mod tidy` (promotes `golang.org/x/net` to a direct dependency; still v0.56.0).

- [ ] **Step 4: Run to verify `mapURL` passes**

Run: `go test ./internal/browse/ -run TestMapURL`
Expected: PASS.

- [ ] **Step 5: Write the failing CSS rewrite test**

Append to `rewrite_test.go`:

```go
func TestRewriteCSS(t *testing.T) {
	in := []byte(`
body { background: url(/img/bg.png); }
@font-face { src: url("https://cdn.x.com/f.woff2"); }
@import '/theme.css';
.x { cursor: url(data:image/png;base64,AA), auto; }
`)
	out := string(rewriteCSS(in, tgt()))
	if !strings.Contains(out, "url(/b/tab:abc/https/example.com/img/bg.png)") {
		t.Fatalf("bg not rewritten: %s", out)
	}
	if !strings.Contains(out, `url("/b/tab:abc/https/cdn.x.com/f.woff2")`) {
		t.Fatalf("font not rewritten: %s", out)
	}
	if !strings.Contains(out, "@import '/b/tab:abc/https/example.com/theme.css'") {
		t.Fatalf("import not rewritten: %s", out)
	}
	if !strings.Contains(out, "url(data:image/png;base64,AA)") {
		t.Fatalf("data url must be untouched: %s", out)
	}
}
```

- [ ] **Step 6: Run to verify it fails**

Run: `go test ./internal/browse/ -run TestRewriteCSS`
Expected: FAIL — `undefined: rewriteCSS`.

- [ ] **Step 7: Implement `rewriteCSS`**

Append to `rewrite.go`:

```go
var (
	cssURLRe    = regexp.MustCompile(`url\(\s*(['"]?)([^'")]+)(['"]?)\s*\)`)
	cssImportRe = regexp.MustCompile(`@import\s+(['"])([^'"]+)(['"])`)
)

// rewriteCSS rewrites url(...) and @import "..." targets. Quoting is preserved.
func rewriteCSS(body []byte, t target) []byte {
	out := cssURLRe.ReplaceAllFunc(body, func(m []byte) []byte {
		sub := cssURLRe.FindSubmatch(m)
		q1, raw, q2 := string(sub[1]), string(sub[2]), string(sub[3])
		return []byte("url(" + q1 + mapURL(raw, t, "") + q2 + ")")
	})
	out = cssImportRe.ReplaceAllFunc(out, func(m []byte) []byte {
		sub := cssImportRe.FindSubmatch(m)
		q1, raw, q2 := string(sub[1]), string(sub[2]), string(sub[3])
		return []byte("@import " + q1 + mapURL(raw, t, "") + q2)
	})
	return out
}
```

- [ ] **Step 8: Run to verify pass**

Run: `go test ./internal/browse/ -run TestRewriteCSS`
Expected: PASS.

- [ ] **Step 9: Write the failing HTML rewrite test**

Append to `rewrite_test.go`:

```go
func TestRewriteHTML(t *testing.T) {
	in := []byte(`<!doctype html><html><head>
<base href="https://example.com/app/">
<link rel="stylesheet" href="theme.css" integrity="sha256-abc">
<link rel="preload" href="/p.js" as="script">
<meta http-equiv="refresh" content="5; url=/next">
<script src="https://cdn.x.com/lib.js" integrity="sha256-def"></script>
</head><body>
<a href="/about">about</a>
<a href="#top">top</a>
<img src="a.png" srcset="a.png 1x, https://cdn.x.com/a2.png 2x">
<form action="/submit"><input formaction="/f2"></form>
<div style="background:url(/bg.png)"></div>
<a href="mailto:x@y.com">mail</a>
</body></html>`)
	out, err := rewriteHTML(in, tgt(), "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	s := string(out)
	checks := []string{
		// relative resolved against <base>:
		`href="/b/tab:abc/https/example.com/app/theme.css"`,
		// preload with root-relative:
		`href="/b/tab:abc/https/example.com/p.js"`,
		// meta refresh:
		`content="5; url=/b/tab:abc/https/example.com/next"`,
		// cross-host script:
		`src="/b/tab:abc/https/cdn.x.com/lib.js"`,
		// anchor href (root-relative), resolved against base:
		`href="/b/tab:abc/https/example.com/about"`,
		// srcset both entries + descriptors:
		`a.png 1x`,
		`/b/tab:abc/https/example.com/app/a.png 1x`,
		`/b/tab:abc/https/cdn.x.com/a2.png 2x`,
		// formaction:
		`formaction="/b/tab:abc/https/example.com/f2"`,
		// inline style url:
		`url(/b/tab:abc/https/example.com/bg.png)`,
	}
	for _, c := range checks {
		if !strings.Contains(s, c) {
			t.Errorf("missing %q in:\n%s", c, s)
		}
	}
	// <base> element must be removed.
	if strings.Contains(s, "<base") {
		t.Errorf("<base> not removed:\n%s", s)
	}
	// integrity must be stripped on rewritten script/link.
	if strings.Contains(s, "integrity") {
		t.Errorf("integrity not stripped:\n%s", s)
	}
	// untouched targets survive.
	if !strings.Contains(s, `href="#top"`) {
		t.Errorf("anchor fragment altered:\n%s", s)
	}
	if !strings.Contains(s, `href="mailto:x@y.com"`) {
		t.Errorf("mailto altered:\n%s", s)
	}
}
```

- [ ] **Step 10: Run to verify it fails**

Run: `go test ./internal/browse/ -run TestRewriteHTML`
Expected: FAIL — `undefined: rewriteHTML`.

- [ ] **Step 11: Implement `rewriteHTML`**

Append to `rewrite.go`:

```go
// urlAttrs maps element atoms to the attributes whose values are URLs.
var urlAttrs = map[atom.Atom][]string{
	atom.A:      {"href"},
	atom.Link:   {"href"},
	atom.Script: {"src"},
	atom.Img:    {"src"},
	atom.Source: {"src"},
	atom.Video:  {"src", "poster"},
	atom.Audio:  {"src"},
	atom.Iframe: {"src"},
	atom.Form:   {"action"},
	atom.Input:  {"formaction"},
	atom.Button: {"formaction"},
	atom.Object: {"data"},
	atom.Track:  {"src"},
	atom.Embed:  {"src"},
}

var metaRefreshURLRe = regexp.MustCompile(`(?i)(url\s*=\s*)(\S+)`)

// rewriteHTML rewrites the static URL surface of an HTML document. base is an
// externally supplied resolution base (normally ""); a <base href> found in
// the document overrides it for subsequent elements and is then removed.
func rewriteHTML(body []byte, t target, base string) ([]byte, error) {
	node, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("browse: html parse: %w", err)
	}
	curBase := base

	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			// <base href> — capture, then mark for removal by blanking to a comment-like noop.
			if n.DataAtom == atom.Base {
				for _, a := range n.Attr {
					if strings.EqualFold(a.Key, "href") && a.Val != "" {
						curBase = resolveBase(t, curBase, a.Val)
					}
				}
				// Drop the element: convert to an empty text node so render omits it.
				n.Type = html.TextNode
				n.Data = ""
				n.Attr = nil
				n.DataAtom = 0
				return
			}
			rewriteElement(n, t, curBase)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(node)

	var buf bytes.Buffer
	if err := html.Render(&buf, node); err != nil {
		return nil, fmt.Errorf("browse: html render: %w", err)
	}
	return buf.Bytes(), nil
}

// resolveBase folds a possibly-relative <base href> onto the existing base.
func resolveBase(t target, curBase, href string) string {
	ref, err := url.Parse(href)
	if err != nil {
		return curBase
	}
	if ref.IsAbs() {
		return ref.String()
	}
	return resolutionBase(t, curBase).ResolveReference(ref).String()
}

func rewriteElement(n *html.Node, t target, base string) {
	rewroteURL := false
	setAttr := func(i int, v string) { n.Attr[i].Val = v }

	for i, a := range n.Attr {
		key := strings.ToLower(a.Key)
		// srcset (img/source).
		if key == "srcset" {
			n.Attr[i].Val = rewriteSrcset(a.Val, t, base)
			rewroteURL = true
			continue
		}
		// inline style url().
		if key == "style" && strings.Contains(a.Val, "url(") {
			setAttr(i, string(rewriteCSS([]byte(a.Val), t)))
			continue
		}
		// meta http-equiv=refresh content.
		if n.DataAtom == atom.Meta && key == "content" && metaIsRefresh(n) {
			setAttr(i, metaRefreshURLRe.ReplaceAllStringFunc(a.Val, func(m string) string {
				sub := metaRefreshURLRe.FindStringSubmatch(m)
				return sub[1] + mapURL(sub[2], t, base)
			}))
			continue
		}
		// generic URL attributes for this element.
		for _, ua := range urlAttrs[n.DataAtom] {
			if key == ua && !untouchedScheme(a.Val) {
				setAttr(i, mapURL(a.Val, t, base))
				rewroteURL = true
			}
		}
	}

	// Strip integrity on any script/link whose URL we rewrote (SRI would fail).
	if rewroteURL && (n.DataAtom == atom.Script || n.DataAtom == atom.Link) {
		filtered := n.Attr[:0]
		for _, a := range n.Attr {
			if strings.EqualFold(a.Key, "integrity") {
				continue
			}
			filtered = append(filtered, a)
		}
		n.Attr = filtered
	}
}

func metaIsRefresh(n *html.Node) bool {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, "http-equiv") && strings.EqualFold(a.Val, "refresh") {
			return true
		}
	}
	return false
}

// rewriteSrcset rewrites each "url [descriptor]" entry in a srcset list.
func rewriteSrcset(val string, t target, base string) string {
	parts := strings.Split(val, ",")
	for i, p := range parts {
		fields := strings.Fields(strings.TrimSpace(p))
		if len(fields) == 0 {
			continue
		}
		fields[0] = mapURL(fields[0], t, base)
		parts[i] = strings.Join(fields, " ")
	}
	return strings.Join(parts, ", ")
}
```

- [ ] **Step 12: Run to verify pass**

Run: `go test ./internal/browse/ -run TestRewriteHTML`
Expected: PASS. If the `srcset` first-entry assertion `a.png 1x` (unmapped literal) also matches the mapped entry substring, that is fine — both required substrings are present; the test asserts the mapped forms explicitly.

- [ ] **Step 13: Full package test + build**

Run: `go test ./internal/browse/ && go build ./...`
Expected: PASS, build OK.

- [ ] **Step 14: Commit**

```bash
git add internal/browse/rewrite.go internal/browse/rewrite_test.go go.mod go.sum
git commit -m "feat(browse): HTML/CSS static URL rewrite engine with SRI strip and base folding"
```
