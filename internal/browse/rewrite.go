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
// byte stream, and is used in local mode only (Chrome mode renders via CDP
// and needs no rewriting). URLs constructed at runtime by page JavaScript —
// fetch(), XMLHttpRequest, dynamic import(), new WebSocket(), Worker() — are
// invisible here and are rerouted client-side by the injected capture script
// (local mode, Part 05). Some exotic loaders and cross-origin API calls from
// workers will still escape both layers; those land in the Network console as
// visible errors, and the site is flagged "degraded" in the status row. This
// is by design (spec: local mode is best-effort for subresources outside the
// wrapper's reach).

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
	// DNS is case-insensitive; lowercase the host so route parsing round-trips
	// consistently (the scheme is already lowercased by url.Parse).
	out := fmt.Sprintf("/b/%s/%s/%s%s", t.StateKey, abs.Scheme, strings.ToLower(abs.Host), path)
	if abs.RawQuery != "" {
		out += "?" + abs.RawQuery
	}
	if abs.Fragment != "" {
		out += "#" + abs.Fragment
	}
	return out
}

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
