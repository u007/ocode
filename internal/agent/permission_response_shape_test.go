package agent

import (
	"reflect"
	"strings"
	"testing"
)

// TestPermissionResponseHasNoSecretField pins the security shape of the
// interactive permission answer. A human-typed elevation password must NEVER
// be a field of PermissionResponse (or any shared/decision struct): the answer
// is copied into tool results, SSE frames and persisted objects, and a future
// %+v debug line or marshal path would leak the secret. Hosts hand the secret
// to the child through the dedicated in-process path instead (see
// SetPendingElevationSecret), so the answer stays three plain decision fields.
func TestPermissionResponseHasNoSecretField(t *testing.T) {
	typ := reflect.TypeOf(PermissionResponse{})
	want := []string{"Level", "PersistRule", "PersistTool"}
	got := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		got = append(got, f.Name)
		lower := strings.ToLower(f.Name)
		for _, banned := range []string{"password", "secret", "token", "credential"} {
			if strings.Contains(lower, banned) {
				t.Errorf("PermissionResponse field %q is secret-bearing (%q); the elevation secret must use the in-process handoff, never the shared answer struct", f.Name, banned)
			}
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PermissionResponse fields = %v, want exactly %v (no extra secret channel)", got, want)
	}
}
