package agent

import (
	"fmt"
	"reflect"
	"testing"
)

func TestBashEgressTargetsLoopVarLoopback(t *testing.T) {
	cases := []struct {
		name      string
		command   string
		wantCount int // targets expected; 0 means "any count, all must match want"
		want      bool
	}{
		{
			name:      "agentkit oauth probe loops over literal paths",
			command:   `cd /Users/james/www/agentkit && for u in /.well-known/oauth-authorization-server /.well-known/oauth-protected-resource /api/mcp; do printf '%-45s ' "$u"; curl -s -o /dev/null -w '%{http_code}\n' "http://localhost:4000$u"; done; echo; for u in /.well-known/oauth-authorization-server /api/mcp; do printf '%-45s ' "3000$u"; curl -s -o /dev/null -w '%{http_code}\n' "http://localhost:3000$u"; done`,
			wantCount: 2,
			want:      true,
		},
		{
			name:    "nested loops both substituted",
			command: `for p in 4000 3000; do for u in /a /b; do curl "http://localhost:$p$u"; done; done`,
			want:    true,
		},
		{
			name:    "while loop around a for loop",
			command: `while true; do for u in /a; do curl "http://localhost:4000$u"; done; done`,
			want:    true,
		},
		{
			name:    "userinfo value escapes localhost",
			command: `for u in @evil.com; do curl "http://localhost:4000$u"; done`,
			want:    false,
		},
		{
			name:    "one escaping value among literal values",
			command: `for u in /a @evil.com; do curl "http://localhost:4000$u"; done`,
			want:    false,
		},
		{
			name:    "reference before the loop",
			command: `curl "http://localhost:4000$u"; for u in /a; do :; done`,
			want:    false,
		},
		{
			name:    "reference after the loop",
			command: `for u in /a; do :; done; curl "http://localhost:4000$u"`,
			want:    false,
		},
		{
			name:    "do word as an argument does not open a frame",
			command: `for u in /a; do echo do; done; curl "http://localhost:4000$u"`,
			want:    false,
		},
		{
			name:    "reassignment inside the body",
			command: `for u in /a; do u=@evil.com; curl "http://localhost:4000$u"; done`,
			want:    false,
		},
		{
			name:    "read rebinds the loop variable",
			command: `for u in /a; do read u; curl "http://localhost:4000$u"; done`,
			want:    false,
		},
		{
			name:    "command substitution list is not literal",
			command: `for u in $(cat f); do curl "http://localhost:4000$u"; done`,
			want:    false,
		},
		{
			name:    "unknown variable in the list",
			command: `for u in /a $X; do curl "http://localhost:4000$u"; done`,
			want:    false,
		},
		{
			name:    "default-value expansion is opaque",
			command: `for u in /a; do curl "http://localhost:4000${u:-@evil.com}"; done`,
			want:    false,
		},
		{
			name:    "quoted do inside the list ends the header early",
			command: `for u in /a "do" @evil.com; do curl "http://localhost:4000$u"; "done"; done`,
			want:    false,
		},
		{
			name:    "escaped do keyword",
			command: `for u in /a \do @evil.com; do curl "http://localhost:4000$u"; done`,
			want:    false,
		},
		{
			name:    "assignment glued to an operator",
			command: `for u in /a; do :;u=@evil.com; curl "http://localhost:4000$u"; done`,
			want:    false,
		},
		{
			name:    "assignment glued to a paren",
			command: `for u in /a; do (u=@evil.com; curl "http://localhost:4000$u"); done`,
			want:    false,
		},
		{
			name:    "IFS splits the substituted value into a second word",
			command: `IFS=x; for u in /axevil.com; do curl http://localhost:4000$u; done`,
			want:    false,
		},
		{
			name:    "command-prefixed read rebinds the loop variable",
			command: `for u in /a; do command read u; curl "http://localhost:4000$u"; done`,
			want:    false,
		},
		{
			name:    "command-prefixed source can rebind the loop variable",
			command: `for u in /a; do command source ./x.sh; curl "http://localhost:4000$u"; done`,
			want:    false,
		},
		{
			name:    "proxy flag still defeats the proof",
			command: `for u in /a; do curl -x http://evil.com "http://localhost:4000$u"; done`,
			want:    false,
		},
		{
			name:    "legacy arithmetic expansion rebinds the loop variable",
			command: `for u in 127; do : $[u++]; curl http://$u.0.0.1; done`,
			want:    false,
		},
		{
			name:    "[[ ]] operand is evaluated arithmetically and rebinds the loop variable",
			command: `for u in 127; do [[ u++ -eq 1 ]]; curl http://$u.0.0.1; done`,
			want:    false,
		},
		{
			name:    "(( )) arithmetic command rebinds the loop variable",
			command: `for u in 127; do ((u++)); curl http://$u.0.0.1; done`,
			want:    false,
		},
		{
			name:    "arithmetic assignment rebinds the loop variable",
			command: `for u in 127; do x=$((u++)); curl http://$u.0.0.1; done`,
			want:    false,
		},
		{
			name:    "literal loop value builds a loopback host",
			command: `for u in 127; do curl http://$u.0.0.1; done`,
			want:    true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			targets := bashEgressTargets(tc.command)
			if len(targets) == 0 {
				t.Fatalf("no egress targets found for %q", tc.command)
			}
			if tc.wantCount != 0 && len(targets) != tc.wantCount {
				t.Fatalf("got %d targets, want %d", len(targets), tc.wantCount)
			}
			for i, target := range targets {
				if target.Loopback != tc.want {
					t.Errorf("target %d (%q): Loopback = %v, want %v", i, target.Value, target.Loopback, tc.want)
				}
			}
		})
	}
}

func TestLiteralLoopVarValues(t *testing.T) {
	cases := []struct {
		name    string
		command string
		want    map[string][]string // nil means literalLoopVarValues must return nil
	}{
		{
			name:    "literal list in a loop body",
			command: `for u in /a /b; do curl "http://x$u"; done`,
			want:    map[string][]string{"u": {"/a", "/b"}},
		},
		{
			name:    "two loops over one name merge their values",
			command: `for u in /a; do :; done; for u in /b; do curl "http://x$u"; done`,
			want:    map[string][]string{"u": {"/a", "/b"}},
		},
		{
			name:    "read poisons the name",
			command: `for u in /a; do read u; curl "http://x$u"; done`,
			want:    nil,
		},
		{
			name:    "command substitution poisons the name",
			command: `for u in $(cat f); do curl "http://x$u"; done`,
			want:    nil,
		},
		{
			name:    "reference outside every loop poisons the name",
			command: `for u in /a; do :; done; echo "$u"`,
			want:    nil,
		},
		{
			name:    "for without in is refused",
			command: `for u; do curl "http://x$u"; done`,
			want:    nil,
		},
		{
			name:    "select is refused",
			command: `select u in /a /b; do curl "http://x$u"; done`,
			want:    nil,
		},
		{
			name:    "source is refused",
			command: `for u in /a; do source ./x.sh; done`,
			want:    nil,
		},
		{
			name:    "opaque expansion poisons the name",
			command: `for u in /a; do echo "${u#/}"; done`,
			want:    nil,
		},
		{
			name:    "unterminated quote returns nil",
			command: `for u in "/a; do :; done`,
			want:    nil,
		},
		{
			name:    "unclosed loop returns nil",
			command: `for u in /a; do curl "http://x$u"`,
			want:    nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := literalLoopVarValues(tc.command)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("literalLoopVarValues(%q) = %v, want %v", tc.command, got, tc.want)
			}
		})
	}
}

func TestExpandLoopVars(t *testing.T) {
	vars := map[string][]string{"p": {"4000", "3000"}, "u": {"/a", "/b"}}
	cases := []struct {
		name  string
		value string
		vars  map[string][]string
		want  []string
	}{
		{
			name:  "two names, sorted odometer order",
			value: "http://localhost:$p$u",
			vars:  vars,
			want: []string{
				"http://localhost:4000/a",
				"http://localhost:4000/b",
				"http://localhost:3000/a",
				"http://localhost:3000/b",
			},
		},
		{
			name:  "braced reference",
			value: "http://x${u}y",
			vars:  map[string][]string{"u": {"/a"}},
			want:  []string{"http://x/ay"},
		},
		{
			name:  "no referenced name in vars",
			value: "http://x$other",
			vars:  vars,
			want:  nil,
		},
		{
			name:  "no reference at all",
			value: "http://localhost:4000/a",
			vars:  vars,
			want:  nil,
		},
		{
			name:  "longer identifier is a different name",
			value: "http://x$ux",
			vars:  map[string][]string{"u": {"/a"}},
			want:  nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := expandLoopVars(tc.value, tc.vars)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("expandLoopVars(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestExpandLoopVarsVariantCap(t *testing.T) {
	makeVals := func(n int) []string {
		vals := make([]string, n)
		for i := range vals {
			vals[i] = fmt.Sprintf("/v%d", i)
		}
		return vals
	}
	atCap := map[string][]string{"a": makeVals(8), "b": makeVals(8)}
	if got := expandLoopVars("http://x/$a/$b", atCap); len(got) != 64 {
		t.Fatalf("8x8 = %d variants, want exactly 64", len(got))
	}
	overCap := map[string][]string{"a": makeVals(9), "b": makeVals(8)}
	if got := expandLoopVars("http://x/$a/$b", overCap); got != nil {
		t.Fatalf("9x8 = %d variants, want nil above the cap", len(got))
	}
}
