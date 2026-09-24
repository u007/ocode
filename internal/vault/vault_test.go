package vault

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func newTestVault(t *testing.T) (*Vault, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vault.json")
	return New(path), path
}

func mustInit(t *testing.T, v *Vault, master string) {
	t.Helper()
	if err := v.Init(master); err != nil {
		t.Fatalf("Init: %v", err)
	}
}

func mustCreate(t *testing.T, v *Vault, it Item) Item {
	t.Helper()
	out, err := v.Create(it)
	if err != nil {
		t.Fatalf("Create(%+v): %v", it, err)
	}
	return out
}

func TestInitUnlockRoundTrip(t *testing.T) {
	v, path := newTestVault(t)
	mustInit(t, v, "correct horse")
	if !v.Unlocked() {
		t.Fatal("vault not unlocked after Init")
	}
	created := mustCreate(t, v, Item{Site: "Example", Username: "alice", Password: "s3cret"})

	v.Lock()
	if v.Unlocked() {
		t.Fatal("vault still unlocked after Lock")
	}

	fresh := New(path)
	if err := fresh.Unlock("correct horse"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	got, err := fresh.Reveal(created.ID)
	if err != nil {
		t.Fatalf("Reveal: %v", err)
	}
	if got.Password != "s3cret" || got.Username != "alice" {
		t.Fatalf("Reveal = %+v, want the created item", got)
	}
}

func TestUnlockWrongMaster(t *testing.T) {
	v, path := newTestVault(t)
	mustInit(t, v, "right-password")
	mustCreate(t, v, Item{Site: "Example", Password: "p"})

	v.Lock()
	wrong := New(path)
	if err := wrong.Unlock("wrong-password"); !errors.Is(err, ErrWrongMaster) {
		t.Fatalf("Unlock(wrong) = %v, want ErrWrongMaster", err)
	}
	if wrong.Unlocked() {
		t.Fatal("vault unlocked after a failed Unlock")
	}
}

func TestLockedOperationsBlocked(t *testing.T) {
	v, _ := newTestVault(t)

	if _, err := v.List("site", 10, 0); !errors.Is(err, ErrLocked) {
		t.Errorf("List on locked vault = %v, want ErrLocked", err)
	}
	if _, err := v.Reveal("anything"); !errors.Is(err, ErrLocked) {
		t.Errorf("Reveal on locked vault = %v, want ErrLocked", err)
	}
	if _, err := v.Create(Item{Site: "S"}); !errors.Is(err, ErrLocked) {
		t.Errorf("Create on locked vault = %v, want ErrLocked", err)
	}
	if n := v.Count(); n != 0 {
		t.Errorf("Count on locked vault = %d, want 0", n)
	}
}

// TestAADBindingRejectsMovedBlob pins the item-id-as-AAD invariant: swapping
// two sealed blobs on disk must make a later Unlock fail rather than open the
// wrong credential.
func TestAADBindingRejectsMovedBlob(t *testing.T) {
	v, path := newTestVault(t)
	mustInit(t, v, "master")
	mustCreate(t, v, Item{Site: "One", Username: "u1", Password: "p1"})
	mustCreate(t, v, Item{Site: "Two", Username: "u2", Password: "p2"})

	vf, err := readVaultFile(path)
	if err != nil {
		t.Fatalf("readVaultFile: %v", err)
	}
	if len(vf.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(vf.Items))
	}
	vf.Items[0].Blob, vf.Items[1].Blob = vf.Items[1].Blob, vf.Items[0].Blob
	if err := writeVaultFile(path, vf); err != nil {
		t.Fatalf("writeVaultFile: %v", err)
	}

	fresh := New(path)
	if err := fresh.Unlock("master"); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Unlock after moving a blob = %v, want ErrCorrupt", err)
	}
}

func TestListSortAndPagination(t *testing.T) {
	v, _ := newTestVault(t)
	mustInit(t, v, "master")
	mustCreate(t, v, Item{Site: "Beta", Username: "zoe"})
	mustCreate(t, v, Item{Site: "alpha", Username: "bob"})
	mustCreate(t, v, Item{Site: "Beta", Username: "amy"})
	mustCreate(t, v, Item{Site: "gamma", Username: "ivy"})
	mustCreate(t, v, Item{Site: "ALPHA", Username: "ann"})

	all, err := v.List("site", 0, 0) // limit<=0 ⇒ default 200
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 5 {
		t.Fatalf("List = %d items, want 5", len(all))
	}
	// Case-insensitive site order, then username.
	want := []string{"ann", "bob", "amy", "zoe", "ivy"}
	for i, w := range want {
		if all[i].Username != w {
			t.Fatalf("order[%d] = %q, want %q (all=%+v)", i, all[i].Username, w, all)
		}
	}

	page, err := v.List("site", 2, 1)
	if err != nil {
		t.Fatalf("List page: %v", err)
	}
	if len(page) != 2 || page[0].Username != "bob" || page[1].Username != "amy" {
		t.Fatalf("page = %+v, want [bob amy]", page)
	}

	past, err := v.List("site", 2, 99)
	if err != nil {
		t.Fatalf("List past end: %v", err)
	}
	if past == nil || len(past) != 0 {
		t.Fatalf("List past end = %#v, want empty non-nil slice", past)
	}

	huge, err := v.List("site", 100000, 0) // limit>1000 ⇒ 1000
	if err != nil {
		t.Fatalf("List huge limit: %v", err)
	}
	if len(huge) != 5 {
		t.Fatalf("List huge limit = %d, want 5", len(huge))
	}
}

func TestChangeMasterRewrapsOnly(t *testing.T) {
	v, path := newTestVault(t)
	mustInit(t, v, "old-master")
	mustCreate(t, v, Item{Site: "Example", Username: "alice", Password: "p"})

	before, err := readVaultFile(path)
	if err != nil {
		t.Fatalf("readVaultFile: %v", err)
	}

	if err := v.ChangeMaster("old-master", "new-master"); err != nil {
		t.Fatalf("ChangeMaster: %v", err)
	}

	after, err := readVaultFile(path)
	if err != nil {
		t.Fatalf("readVaultFile after: %v", err)
	}
	if after.WrappedKey == before.WrappedKey {
		t.Fatal("wrapped_key did not change")
	}
	if len(after.Items) != len(before.Items) {
		t.Fatalf("items len = %d, want %d", len(after.Items), len(before.Items))
	}
	for i := range before.Items {
		if before.Items[i].Blob != after.Items[i].Blob {
			t.Fatalf("item %s blob changed", before.Items[i].ID)
		}
	}

	if err := New(path).Unlock("old-master"); !errors.Is(err, ErrWrongMaster) {
		t.Fatalf("Unlock(old) = %v, want ErrWrongMaster", err)
	}
	if err := New(path).Unlock("new-master"); err != nil {
		t.Fatalf("Unlock(new) = %v, want success", err)
	}
}

func TestPersistMergeKeepsForeignItems(t *testing.T) {
	v1, path := newTestVault(t)
	mustInit(t, v1, "master")

	// A second process opens (and unlocks) the same file while it is still
	// empty, then both create an item. A naive save would lose one.
	v2 := New(path)
	if err := v2.Unlock("master"); err != nil {
		t.Fatalf("v2 Unlock: %v", err)
	}
	mustCreate(t, v1, Item{Site: "One", Username: "u1"})
	mustCreate(t, v2, Item{Site: "Two", Username: "u2"})

	fresh := New(path)
	if err := fresh.Unlock("master"); err != nil {
		t.Fatalf("fresh Unlock: %v", err)
	}
	metas, err := fresh.List("site", 0, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 2 {
		t.Fatalf("fresh reader sees %d items, want 2 (lost update)", len(metas))
	}
}

func TestChangeMasterRequiresUnlocked(t *testing.T) {
	v, _ := newTestVault(t)
	if err := v.ChangeMaster("a", "b"); !errors.Is(err, ErrLocked) {
		t.Fatalf("ChangeMaster on locked vault = %v, want ErrLocked", err)
	}
}

func TestFileNeverContainsPlaintextPassword(t *testing.T) {
	v, path := newTestVault(t)
	mustInit(t, v, "master")
	mustCreate(t, v, Item{
		Site:     "supersecret-site",
		Username: "plaintext-user",
		Password: "plaintext-password",
	})

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read raw file: %v", err)
	}
	for _, needle := range []string{"plaintext-password", "plaintext-user", "supersecret-site", "master"} {
		if bytes.Contains(raw, []byte(needle)) {
			t.Fatalf("raw vault file contains plaintext %q", needle)
		}
	}
}
