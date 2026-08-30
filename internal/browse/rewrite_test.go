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

// Edge cases for the path-structured /b/ route: encoded slashes must survive
// as escapes (never decoded into route separators), ports must round-trip,
// uppercase schemes must normalize, and a relative <base> must fold onto the
// document URL.
func TestMapURLEdgeCases(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"encoded slash preserved", "/a%2Fb/c", "/b/tab:abc/https/example.com/a%2Fb/c"},
		{"space escaped", "/a b/c.png", "/b/tab:abc/https/example.com/a%20b/c.png"},
		{"port preserved", "https://api.x.com:8443/v1", "/b/tab:abc/https/api.x.com:8443/v1"},
		{"uppercase scheme normalized", "HTTPS://X.COM/A", "/b/tab:abc/https/x.com/A"},
		{"fragment preserved", "/p#a", "/b/tab:abc/https/example.com/p#a"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mapURL(c.raw, tgt(), ""); got != c.want {
				t.Fatalf("mapURL(%q) = %q, want %q", c.raw, got, c.want)
			}
		})
	}
}

func TestRewriteHTMLRelativeBase(t *testing.T) {
	// <base href="../lib/"> relative to document /page/ resolves to /lib/.
	in := []byte(`<!doctype html><html><head><base href="../lib/"></head>
<body><img src="x.png"></body></html>`)
	out, err := rewriteHTML(in, tgt(), "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, `src="/b/tab:abc/https/example.com/lib/x.png"`) {
		t.Errorf("relative base not folded:\n%s", s)
	}
	if strings.Contains(s, "<base") {
		t.Errorf("<base> not removed:\n%s", s)
	}
}
