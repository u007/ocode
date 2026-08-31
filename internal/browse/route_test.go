package browse

import "testing"

func TestHostIsLiteralPrivateLocalhostSubdomain(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"app.localhost", true},
		{"a.b.localhost:3000", true},
		{"localhost", true},
		{"localhost.evil.com", false},
		{"example.com", false},
		{"127.0.0.1", true},
		{"192.168.1.1", true},
		{"a.localhost.", false},
		{"LOCALHOST", true},
		{"APP.LOCALHOST", true},
	}
	for _, c := range cases {
		if got := hostIsLiteralPrivate(c.host); got != c.want {
			t.Errorf("hostIsLiteralPrivate(%q)=%v want %v", c.host, got, c.want)
		}
	}
}

func TestParseTarget(t *testing.T) {
	cases := []struct {
		name    string
		path    string
		query   string
		want    target
		wantErr bool
	}{
		{
			name:  "external https",
			path:  "/b/tab:abc/https/example.com/foo/bar",
			query: "q=1",
			want:  target{StateKey: "tab:abc", Scheme: "https", Host: "example.com", Path: "/foo/bar", RawQuery: "q=1", Local: false},
		},
		{
			name:  "root path",
			path:  "/b/side:chat:s1/https/example.com",
			query: "",
			want:  target{StateKey: "side:chat:s1", Scheme: "https", Host: "example.com", Path: "/", RawQuery: "", Local: false},
		},
		{
			name:  "loopback host is local",
			path:  "/b/tab:x/http/127.0.0.1:5173/",
			query: "",
			want:  target{StateKey: "tab:x", Scheme: "http", Host: "127.0.0.1:5173", Path: "/", RawQuery: "", Local: true},
		},
		{name: "missing scheme", path: "/b/tab:x", wantErr: true},
		{name: "bad prefix", path: "/x/tab:x/https/example.com", wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseTarget(c.path, c.query)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Fatalf("got %+v want %+v", got, c.want)
			}
		})
	}
}
