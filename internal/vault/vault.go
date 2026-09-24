package vault

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

// Item is a decrypted vault entry. The secret fields (Password, Notes) only
// ever exist in memory while the vault is unlocked.
type Item struct {
	ID       string `json:"id"`
	Site     string `json:"site"`
	URL      string `json:"url"`
	Title    string `json:"title"`
	Username string `json:"username"`
	Password string `json:"password"`
	Notes    string `json:"notes"`
	Created  string `json:"created"`
	Updated  string `json:"updated"`
}

// ItemMeta is the non-secret projection of an Item used by list responses and
// URL matching.
type ItemMeta struct {
	ID       string `json:"id"`
	Site     string `json:"site"`
	URL      string `json:"url"`
	Title    string `json:"title"`
	Username string `json:"username"`
}

// List bounds. They are applied explicitly (never as an optional-chaining
// fallback): a caller that omits limit gets listDefaultLimit, and one that
// asks for more than listMaxLimit gets listMaxLimit.
const (
	listDefaultLimit = 200
	listMaxLimit     = 1000
)

// sortKeySite is the only supported List sort key (case-insensitive site, then
// username).
const sortKeySite = "site"

// Vault owns the encrypted file at path and the in-memory decrypted state
// while it is unlocked. All exported methods are safe for concurrent use.
type Vault struct {
	path string

	mu      sync.Mutex
	dk      []byte          // nil while locked
	items   map[string]Item // id -> decrypted item; nil while locked
	deleted map[string]bool // ids deleted in this process since the last persist
}

// New returns a locked Vault backed by path. It performs no I/O.
func New(path string) *Vault {
	return &Vault{path: path}
}

// Path reports the backing file path.
func (v *Vault) Path() string { return v.path }

// Exists reports whether the vault file is present.
func (v *Vault) Exists() bool {
	_, err := os.Stat(v.path)
	return err == nil
}

// Unlocked reports whether a data key is currently held in memory.
func (v *Vault) Unlocked() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.dk != nil
}

// Init creates a new vault protected by master: it generates a random data key
// and salt, wraps the key under an Argon2id-derived KEK, writes the envelope
// atomically, and leaves the vault unlocked. It refuses to overwrite an
// existing vault (ErrExists).
func (v *Vault) Init(master string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.Exists() {
		return ErrExists
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return fmt.Errorf("vault: generate salt: %w", err)
	}
	dk := make([]byte, keyLen)
	if _, err := rand.Read(dk); err != nil {
		return fmt.Errorf("vault: generate data key: %w", err)
	}
	wrapped, err := wrapKey(deriveKEK(master, salt), dk)
	if err != nil {
		return fmt.Errorf("vault: wrap data key: %w", err)
	}
	vf := &vaultFile{
		Version: 1,
		KDF: kdfParams{
			Algo:      "argon2id",
			Salt:      base64.StdEncoding.EncodeToString(salt),
			Time:      kdfTime,
			MemoryKiB: kdfMemory,
			Threads:   kdfThreads,
		},
		WrappedKey: wrapped,
		Items:      []diskItem{},
	}
	if err := writeVaultFile(v.path, vf); err != nil {
		return err
	}
	v.dk = dk
	v.items = map[string]Item{}
	v.deleted = map[string]bool{}
	return nil
}

// Unlock derives the KEK from master and unwraps the data key. A wrong master
// (bad salt decode, failed unwrap) is reported as ErrWrongMaster and leaves the
// vault locked; a missing file is ErrNoVault; an item that fails to decrypt is
// ErrCorrupt.
func (v *Vault) Unlock(master string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	vf, err := readVaultFile(v.path)
	if err != nil {
		if errors.Is(err, ErrNoVault) {
			return ErrNoVault
		}
		log.Printf("vault: unlock: read vault file: %v", err)
		return err
	}
	salt, err := base64.StdEncoding.DecodeString(vf.KDF.Salt)
	if err != nil {
		log.Printf("vault: unlock: decode salt: %v", err)
		return ErrWrongMaster
	}
	dk, err := unwrapKey(deriveKEK(master, salt), vf.WrappedKey)
	if err != nil {
		return ErrWrongMaster
	}
	items, err := decryptItems(dk, vf.Items)
	if err != nil {
		log.Printf("vault: unlock: decrypt items: %v", err)
		return err
	}
	v.dk = dk
	v.items = items
	v.deleted = map[string]bool{}
	return nil
}

// Lock zeroes the data key and drops all decrypted state.
func (v *Vault) Lock() {
	v.mu.Lock()
	defer v.mu.Unlock()
	for i := range v.dk {
		v.dk[i] = 0
	}
	v.dk = nil
	v.items = nil
	v.deleted = nil
}

// Count returns the number of items, or 0 while locked.
func (v *Vault) Count() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.dk == nil {
		return 0
	}
	return len(v.items)
}

// List returns item metadata sorted case-insensitively by site then username.
// Bounds are applied explicitly: offset<0 ⇒ 0; limit<=0 ⇒ listDefaultLimit;
// limit>listMaxLimit ⇒ listMaxLimit; offset past the end ⇒ empty slice.
func (v *Vault) List(sortKey string, limit, offset int) ([]ItemMeta, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.dk == nil {
		return nil, ErrLocked
	}
	if sortKey != "" && sortKey != sortKeySite {
		return nil, fmt.Errorf("vault: unknown sort key %q", sortKey)
	}
	metas := make([]ItemMeta, 0, len(v.items))
	for _, it := range v.items {
		metas = append(metas, ItemMeta{
			ID:       it.ID,
			Site:     it.Site,
			URL:      it.URL,
			Title:    it.Title,
			Username: it.Username,
		})
	}
	slices.SortStableFunc(metas, func(a, b ItemMeta) int {
		if c := strings.Compare(strings.ToLower(a.Site), strings.ToLower(b.Site)); c != 0 {
			return c
		}
		return strings.Compare(strings.ToLower(a.Username), strings.ToLower(b.Username))
	})

	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = listDefaultLimit
	}
	if limit > listMaxLimit {
		limit = listMaxLimit
	}
	if offset >= len(metas) {
		return []ItemMeta{}, nil
	}
	return metas[offset:min(offset+limit, len(metas))], nil
}

// Reveal returns the full item, including its password, by id.
func (v *Vault) Reveal(id string) (Item, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.dk == nil {
		return Item{}, ErrLocked
	}
	it, ok := v.items[id]
	if !ok {
		return Item{}, ErrNotFound
	}
	return it, nil
}

// Create adds a new item with a freshly generated id, persists it, and returns
// the stored item (with Created/Updated timestamps filled in).
func (v *Vault) Create(it Item) (Item, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.dk == nil {
		return Item{}, ErrLocked
	}
	id, err := newItemID()
	if err != nil {
		return Item{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	it.ID = id
	it.Created = now
	it.Updated = now
	if v.items == nil {
		v.items = map[string]Item{}
	}
	v.items[id] = it
	if err := v.persistLocked(); err != nil {
		return Item{}, err
	}
	return it, nil
}

// Update replaces the item with the given id, preserving its Created
// timestamp, persisting the change, and returning the stored item.
func (v *Vault) Update(id string, it Item) (Item, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.dk == nil {
		return Item{}, ErrLocked
	}
	existing, ok := v.items[id]
	if !ok {
		return Item{}, ErrNotFound
	}
	it.ID = id
	it.Created = existing.Created
	it.Updated = time.Now().UTC().Format(time.RFC3339)
	v.items[id] = it
	if err := v.persistLocked(); err != nil {
		return Item{}, err
	}
	return it, nil
}

// Delete removes the item with the given id and persists the tombstone.
func (v *Vault) Delete(id string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.dk == nil {
		return ErrLocked
	}
	if _, ok := v.items[id]; !ok {
		return ErrNotFound
	}
	delete(v.items, id)
	if v.deleted == nil {
		v.deleted = map[string]bool{}
	}
	v.deleted[id] = true
	return v.persistLocked()
}

// ChangeMaster verifies old against the file's wrapped key, then re-wraps the
// same data key under a new Argon2id KEK derived from new. Item blobs are left
// byte-identical: only the kdf block and wrapped_key change.
func (v *Vault) ChangeMaster(old, new string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.dk == nil {
		return ErrLocked
	}

	lock, err := acquireFileLock(v.path)
	if err != nil {
		log.Printf("vault: change master: acquire file lock: %v", err)
		return err
	}
	defer lock.release()

	vf, err := readVaultFile(v.path)
	if err != nil {
		log.Printf("vault: change master: read vault file: %v", err)
		return err
	}
	salt, err := base64.StdEncoding.DecodeString(vf.KDF.Salt)
	if err != nil {
		log.Printf("vault: change master: decode salt: %v", err)
		return ErrWrongMaster
	}
	if _, err := unwrapKey(deriveKEK(old, salt), vf.WrappedKey); err != nil {
		return ErrWrongMaster
	}

	newSalt := make([]byte, saltLen)
	if _, err := rand.Read(newSalt); err != nil {
		return fmt.Errorf("vault: generate salt: %w", err)
	}
	wrapped, err := wrapKey(deriveKEK(new, newSalt), v.dk)
	if err != nil {
		return fmt.Errorf("vault: wrap data key: %w", err)
	}
	vf.KDF = kdfParams{
		Algo:      "argon2id",
		Salt:      base64.StdEncoding.EncodeToString(newSalt),
		Time:      kdfTime,
		MemoryKiB: kdfMemory,
		Threads:   kdfThreads,
	}
	vf.WrappedKey = wrapped
	if err := writeVaultFile(v.path, vf); err != nil {
		log.Printf("vault: change master: write vault file: %v", err)
		return err
	}
	return nil
}

// persistLocked is the concurrency core of every mutation. It must be called
// with v.mu held and the vault unlocked.
//
// It takes the cross-process file lock, re-reads the file, decrypts whatever is
// on disk (another process may have added items since this process last read),
// drops ids this process deleted, overlays this process's items, re-seals the
// whole set under the in-memory data key (each item with its id as AAD), and
// writes atomically. Saving a stale in-memory snapshot instead would silently
// erase a concurrent process's item.
func (v *Vault) persistLocked() error {
	lock, err := acquireFileLock(v.path)
	if err != nil {
		log.Printf("vault: persist vault: acquire file lock: %v", err)
		return err
	}
	defer lock.release()

	vf, err := readVaultFile(v.path)
	if err != nil {
		log.Printf("vault: persist vault: read current file: %v", err)
		return err
	}
	foreign, err := decryptItems(v.dk, vf.Items)
	if err != nil {
		log.Printf("vault: persist vault: decrypt current items: %v", err)
		return err
	}

	merged := make(map[string]Item, len(foreign)+len(v.items))
	for id, it := range foreign {
		merged[id] = it
	}
	for id := range v.deleted {
		delete(merged, id)
	}
	for id, it := range v.items {
		merged[id] = it
	}

	ids := make([]string, 0, len(merged))
	for id := range merged {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	disks := make([]diskItem, 0, len(ids))
	for _, id := range ids {
		blob, err := sealItem(v.dk, id, merged[id])
		if err != nil {
			log.Printf("vault: persist vault: seal item %s: %v", id, err)
			return err
		}
		disks = append(disks, diskItem{ID: id, Blob: blob})
	}

	out := &vaultFile{Version: vf.Version, KDF: vf.KDF, WrappedKey: vf.WrappedKey, Items: disks}
	if out.Version == 0 {
		out.Version = 1
	}
	if err := writeVaultFile(v.path, out); err != nil {
		log.Printf("vault: persist vault: write vault file: %v", err)
		return err
	}
	v.items = merged
	v.deleted = map[string]bool{}
	return nil
}

// sealItem serialises it and seals it whole under dk with its id as AAD.
func sealItem(dk []byte, id string, it Item) (string, error) {
	plain, err := json.Marshal(it)
	if err != nil {
		return "", fmt.Errorf("vault: marshal item %s: %w", id, err)
	}
	blob, err := seal(dk, []byte(id), plain)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(blob), nil
}

// decryptItems opens every disk blob under dk using its id as AAD. Any failure
// is wrapped ErrCorrupt naming the item, and is logged.
func decryptItems(dk []byte, disks []diskItem) (map[string]Item, error) {
	items := make(map[string]Item, len(disks))
	for _, d := range disks {
		raw, err := base64.StdEncoding.DecodeString(d.Blob)
		if err != nil {
			log.Printf("vault: decrypt item %s: base64: %v", d.ID, err)
			return nil, fmt.Errorf("%w: item %s", ErrCorrupt, d.ID)
		}
		plain, err := open(dk, []byte(d.ID), raw)
		if err != nil {
			log.Printf("vault: decrypt item %s: %v", d.ID, err)
			return nil, fmt.Errorf("%w: item %s", ErrCorrupt, d.ID)
		}
		var it Item
		if err := json.Unmarshal(plain, &it); err != nil {
			log.Printf("vault: decrypt item %s: unmarshal: %v", d.ID, err)
			return nil, fmt.Errorf("%w: item %s", ErrCorrupt, d.ID)
		}
		it.ID = d.ID
		items[d.ID] = it
	}
	return items, nil
}

// newItemID returns a random 128-bit hex item id.
func newItemID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("vault: generate item id: %w", err)
	}
	return hex.EncodeToString(b), nil
}
