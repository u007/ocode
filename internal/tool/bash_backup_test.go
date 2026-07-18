package tool

import (
	"reflect"
	"testing"
)

func TestDestructiveBashBackupPaths(t *testing.T) {
	cases := []struct {
		name    string
		command string
		want    []string
	}{
		{"rm single", "rm foo.txt", []string{"foo.txt"}},
		{"rm flags", "rm -rf foo.txt bar.txt", []string{"foo.txt", "bar.txt"}},
		{"mv", "mv a.txt b.txt", []string{"a.txt", "b.txt"}},
		{"cp", "cp a.txt b.txt", []string{"b.txt"}},
		{"truncate size", "truncate -s 0 foo.txt", []string{"foo.txt"}},
		{"sed in place gnu", "sed -i 's/x/y/' foo.txt", []string{"foo.txt"}},
		{"sed in place suffix", "sed -i.bak 's/x/y/' foo.txt", []string{"foo.txt"}},
		{"sed in place bsd", "sed -i '' 's/x/y/' foo.txt", []string{"foo.txt"}},
		{"sed no -i is read only", "sed 's/x/y/' foo.txt", nil},
		{"read only command", "cat foo.txt", nil},
		{"unsupported command", "ls -la", nil},
		{"compound command skipped", "rm foo.txt && rm bar.txt", nil},
		{"pipe skipped", "cat foo.txt | rm bar.txt", nil},
		{"redirection skipped", "rm foo.txt > out.log", nil},
		{"substitution skipped", "rm $(echo foo.txt)", nil},
		{"cp with extra flag unsupported form", "cp -r dir1 dir2 dir3", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := destructiveBashBackupPaths(tc.command)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("destructiveBashBackupPaths(%q) = %v, want %v", tc.command, got, tc.want)
			}
		})
	}
}
