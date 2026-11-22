package config

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Upstash Box account settings. The API key of each account is a secret and
// lives in auth.json (internal/auth, upstash_box_keys.go), never here. Only the
// non-secret fields below are persisted under the "upstash_box" key of
// ocodeconfig.json.

const (
	UpstashBoxBackendLocal   = "local"
	UpstashBoxBackendUpstash = "upstash"

	UpstashBoxSizeSmall  = "small"
	UpstashBoxSizeMedium = "medium"
	UpstashBoxSizeLarge  = "large"

	UpstashBoxDefaultSize           = UpstashBoxSizeSmall
	UpstashBoxDefaultIdleMinutes    = 15
	UpstashBoxDefaultHardCapMinutes = 120
	UpstashBoxDefaultRetainDays     = 14

	UpstashBoxMinIdleMinutes    = 1
	UpstashBoxMinHardCapMinutes = 1
	UpstashBoxMaxHardCapMinutes = 4320 // 3 days
	UpstashBoxMaxIdleMinutes    = 4320 // idle may not exceed the 3-day cap
	UpstashBoxMinRetainDays     = 1
	UpstashBoxMaxRetainDays     = 365

	// UpstashBoxMaxAPIKeyBytes bounds a stored key. Keys are opaque tokens, so
	// anything longer is a paste error, not a real key.
	UpstashBoxMaxAPIKeyBytes = 512

	upstashBoxMaxNameRunes = 64
)

var (
	// ErrUpstashBoxInvalid marks a rejected request: bad field, duplicate
	// name, unknown default, or a backend with no accounts. Callers map it to 400.
	ErrUpstashBoxInvalid = errors.New("invalid upstash box settings")
	// ErrUpstashBoxNotFound marks an account id that does not exist. Callers map it to 404.
	ErrUpstashBoxNotFound = errors.New("upstash box account not found")
)

// upstashInvalidError is a validation failure whose message is the detail
// alone. errors.Is reports it as ErrUpstashBoxInvalid, so callers map it to 400.
type upstashInvalidError struct{ msg string }

func (e upstashInvalidError) Error() string { return e.msg }

func (e upstashInvalidError) Is(target error) bool { return target == ErrUpstashBoxInvalid }

func invalidf(format string, args ...any) error {
	return upstashInvalidError{msg: fmt.Sprintf(format, args...)}
}

// UpstashBoxAccount is one stored Upstash Box account without its API key.
type UpstashBoxAccount struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Size           string `json:"size"`
	IdleMinutes    int    `json:"idle_minutes"`
	HardCapMinutes int    `json:"hard_cap_minutes"`
	RetainDays     int    `json:"retain_days"`
}

// UpstashBoxConfig is the persisted "upstash_box" section of ocodeconfig.json.
// Accounts are kept sorted by name (case-insensitive), then id. LoginStates maps
// a project root to the name of the login state assigned to it. A login state
// is a saved set of cookies and site storage kept on this machine; its data is
// never in this file.
type UpstashBoxConfig struct {
	DefaultBackend   string              `json:"default_backend,omitempty"`
	DefaultAccountID string              `json:"default_account_id,omitempty"`
	Accounts         []UpstashBoxAccount `json:"accounts,omitempty"`
	LoginStates      map[string]string   `json:"login_states,omitempty"`
}

// loginStateNamePattern is the one login-state name rule. It is defined here,
// not in internal/boxsession, because boxsession imports the browser stack and
// config must stay below it. boxsession.ValidLoginStateName delegates here.
var loginStateNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)

// LoginStateNone is the reserved login-state name that means "no login state".
// It is never a saved state, and assigning it clears the assignment.
const LoginStateNone = "none"

// ValidLoginStateName checks a login-state name: 1 to 40 characters from a-z,
// 0-9, '_' and '-', starting with a letter or digit. "none" is reserved.
func ValidLoginStateName(name string) error {
	if name == LoginStateNone {
		return invalidf("login state name %q is reserved", LoginStateNone)
	}
	if !loginStateNamePattern.MatchString(name) {
		return invalidf("login state name must be 1 to 40 characters of a-z, 0-9, _ or -, starting with a letter or digit")
	}
	return nil
}

// LoginStateFor returns the login state assigned to a project root, or "" when
// none is assigned.
func (c UpstashBoxConfig) LoginStateFor(projectRoot string) string {
	if projectRoot == "" {
		return ""
	}
	return c.LoginStates[projectRoot]
}

// SetLoginState assigns a login state to a project root. An empty name or
// "none" removes the assignment. Any other name must pass ValidLoginStateName.
// The name need not exist as a saved state yet: it is created on first save.
func (c *UpstashBoxConfig) SetLoginState(projectRoot, name string) error {
	if projectRoot == "" {
		return invalidf("project is required")
	}
	if name == "" || name == LoginStateNone {
		delete(c.LoginStates, projectRoot)
		c.normalize()
		return nil
	}
	if err := ValidLoginStateName(name); err != nil {
		return err
	}
	if c.LoginStates == nil {
		c.LoginStates = map[string]string{}
	}
	c.LoginStates[projectRoot] = name
	c.normalize()
	return nil
}

// ClearLoginStateAssignments removes every project's assignment of the named
// login state. It reports whether any assignment was removed.
func (c *UpstashBoxConfig) ClearLoginStateAssignments(name string) bool {
	changed := false
	for root, assigned := range c.LoginStates {
		if assigned == name {
			delete(c.LoginStates, root)
			changed = true
		}
	}
	c.normalize()
	return changed
}

// Validate checks one account's non-secret fields. It does not check name
// uniqueness, which needs the whole section (see AddAccount / UpdateAccount).
func (a UpstashBoxAccount) Validate() error {
	name := strings.TrimSpace(a.Name)
	if name == "" {
		return invalidf("name is required")
	}
	if utf8.RuneCountInString(name) > upstashBoxMaxNameRunes {
		return invalidf("name must be at most %d characters", upstashBoxMaxNameRunes)
	}
	switch a.Size {
	case UpstashBoxSizeSmall, UpstashBoxSizeMedium, UpstashBoxSizeLarge:
	default:
		return invalidf("size must be small, medium or large")
	}
	if a.IdleMinutes < UpstashBoxMinIdleMinutes || a.IdleMinutes > UpstashBoxMaxIdleMinutes {
		return invalidf("idle_minutes must be between %d and %d", UpstashBoxMinIdleMinutes, UpstashBoxMaxIdleMinutes)
	}
	if a.HardCapMinutes < UpstashBoxMinHardCapMinutes || a.HardCapMinutes > UpstashBoxMaxHardCapMinutes {
		return invalidf("hard_cap_minutes must be between %d and %d", UpstashBoxMinHardCapMinutes, UpstashBoxMaxHardCapMinutes)
	}
	if a.IdleMinutes > a.HardCapMinutes {
		return invalidf("idle_minutes (%d) must be less than or equal to hard_cap_minutes (%d)",
			a.IdleMinutes, a.HardCapMinutes)
	}
	if a.RetainDays < UpstashBoxMinRetainDays || a.RetainDays > UpstashBoxMaxRetainDays {
		return invalidf("retain_days must be between %d and %d", UpstashBoxMinRetainDays, UpstashBoxMaxRetainDays)
	}
	return nil
}

// ValidateUpstashAPIKey checks a trimmed API key before it is stored. It must be
// non-empty, at most UpstashBoxMaxAPIKeyBytes bytes, and free of whitespace and
// control characters (a pasted key with a line break or an inner space is a
// broken key, and must not be written).
func ValidateUpstashAPIKey(key string) error {
	if key == "" {
		return invalidf("api_key is required")
	}
	if len(key) > UpstashBoxMaxAPIKeyBytes {
		return invalidf("api_key must be at most %d bytes", UpstashBoxMaxAPIKeyBytes)
	}
	for _, r := range key {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return invalidf("api_key must not contain whitespace or control characters")
		}
	}
	return nil
}

// Backend returns the effective default backend. An unset value means local.
func (c UpstashBoxConfig) Backend() string {
	if c.DefaultBackend == "" {
		return UpstashBoxBackendLocal
	}
	return c.DefaultBackend
}

// DefaultAccount returns the default account, if any.
func (c UpstashBoxConfig) DefaultAccount() (UpstashBoxAccount, bool) {
	i := c.indexOf(c.DefaultAccountID)
	if i < 0 {
		return UpstashBoxAccount{}, false
	}
	return c.Accounts[i], true
}

// FindAccount returns the account with the given id, if any.
func (c UpstashBoxConfig) FindAccount(id string) (UpstashBoxAccount, bool) {
	i := c.indexOf(id)
	if i < 0 {
		return UpstashBoxAccount{}, false
	}
	return c.Accounts[i], true
}

// AddAccount appends a validated account. The first account added becomes the
// default. A name that matches an existing one (case-insensitive) is rejected.
func (c *UpstashBoxConfig) AddAccount(acc UpstashBoxAccount) error {
	acc.Name = strings.TrimSpace(acc.Name)
	if err := acc.Validate(); err != nil {
		return err
	}
	if acc.ID == "" {
		return invalidf("account id is required")
	}
	if c.indexOf(acc.ID) >= 0 {
		return invalidf("account id already exists")
	}
	if err := c.checkNameFree(acc.Name, ""); err != nil {
		return err
	}
	c.Accounts = append(c.Accounts, acc)
	c.normalize()
	return nil
}

// UpdateAccount replaces the non-secret fields of an existing account, keeping
// its id. A rename onto another account's name is rejected.
func (c *UpstashBoxConfig) UpdateAccount(acc UpstashBoxAccount) error {
	i := c.indexOf(acc.ID)
	if i < 0 {
		return ErrUpstashBoxNotFound
	}
	acc.Name = strings.TrimSpace(acc.Name)
	if err := acc.Validate(); err != nil {
		return err
	}
	if err := c.checkNameFree(acc.Name, acc.ID); err != nil {
		return err
	}
	c.Accounts[i] = acc
	c.normalize()
	return nil
}

// RemoveAccount deletes an account. If it was the default, the first remaining
// account (by sort order) becomes the default. Removing the last account sets
// the backend back to local.
func (c *UpstashBoxConfig) RemoveAccount(id string) error {
	i := c.indexOf(id)
	if i < 0 {
		return ErrUpstashBoxNotFound
	}
	c.Accounts = append(c.Accounts[:i], c.Accounts[i+1:]...)
	c.normalize()
	return nil
}

// SetSettings changes the default backend and/or default account. A nil
// pointer leaves that field alone. "upstash" is refused while there are no
// accounts, and an unknown default account is refused.
func (c *UpstashBoxConfig) SetSettings(backend, accountID *string) error {
	if backend != nil {
		switch *backend {
		case UpstashBoxBackendLocal, UpstashBoxBackendUpstash:
		default:
			return invalidf("default_backend must be local or upstash")
		}
		if *backend == UpstashBoxBackendUpstash && len(c.Accounts) == 0 {
			return invalidf("add an Upstash Box account before selecting upstash")
		}
	}
	if accountID != nil {
		// With accounts present the default must name one of them (an empty
		// value is therefore refused). With none, the only valid value is "".
		if len(c.Accounts) == 0 && *accountID != "" || len(c.Accounts) > 0 && c.indexOf(*accountID) < 0 {
			return invalidf("default_account_id does not match an account")
		}
		c.DefaultAccountID = *accountID
	}
	if backend != nil {
		c.DefaultBackend = *backend
	}
	c.normalize()
	return nil
}

// normalize restores the invariants after any mutation: accounts sorted, the
// default account is one that exists (first account when unset or stale), and
// no accounts means the local backend with no default account.
func (c *UpstashBoxConfig) normalize() {
	// Dropping an empty or "none" assignment is order-independent, so the
	// result does not depend on map iteration order.
	for root, name := range c.LoginStates {
		if name == "" || name == LoginStateNone {
			delete(c.LoginStates, root)
		}
	}
	if len(c.LoginStates) == 0 {
		c.LoginStates = nil
	}
	sort.SliceStable(c.Accounts, func(i, j int) bool {
		a, b := c.Accounts[i], c.Accounts[j]
		la, lb := strings.ToLower(a.Name), strings.ToLower(b.Name)
		if la != lb {
			return la < lb
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.ID < b.ID
	})
	if len(c.Accounts) == 0 {
		c.DefaultAccountID = ""
		c.DefaultBackend = UpstashBoxBackendLocal
		return
	}
	if c.indexOf(c.DefaultAccountID) < 0 {
		c.DefaultAccountID = c.Accounts[0].ID
	}
	if c.DefaultBackend == "" {
		c.DefaultBackend = UpstashBoxBackendLocal
	}
}

func (c UpstashBoxConfig) indexOf(id string) int {
	if id == "" {
		return -1
	}
	for i := range c.Accounts {
		if c.Accounts[i].ID == id {
			return i
		}
	}
	return -1
}

func (c UpstashBoxConfig) checkNameFree(name, exceptID string) error {
	for _, a := range c.Accounts {
		if a.ID == exceptID {
			continue
		}
		if strings.EqualFold(a.Name, name) {
			return invalidf("an account named %q already exists", name)
		}
	}
	return nil
}

// LoadUpstashBoxConfig returns the upstash_box section read fresh from disk.
func LoadUpstashBoxConfig() (UpstashBoxConfig, error) {
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return UpstashBoxConfig{}, err
	}
	out := cfg.UpstashBox
	out.Accounts = append([]UpstashBoxAccount(nil), out.Accounts...)
	out.normalize()
	return out, nil
}

// LoadDefaultUpstashBoxAccount returns the default account's non-secret
// settings. ok is false when there is no default account. The API key is not
// included: read it with auth.UpstashBoxAPIKey.
func LoadDefaultUpstashBoxAccount() (acc UpstashBoxAccount, ok bool, err error) {
	c, err := LoadUpstashBoxConfig()
	if err != nil {
		return UpstashBoxAccount{}, false, err
	}
	acc, ok = c.DefaultAccount()
	return acc, ok, nil
}

// upstashBoxSave persists the ocodeconfig file. It is a variable only so a test
// can force the write to fail (SetUpstashBoxSaveForTest).
var upstashBoxSave = func(cfg *OcodeConfig) error { return SaveOcodeConfig(cfg) }

// SetUpstashBoxSaveForTest replaces the config write used by MutateUpstashBox
// and MutateUpstashBoxGuarded, and returns a function that restores the real
// write. It exists so a test can inject a config-write failure without relying
// on file permissions.
func SetUpstashBoxSaveForTest(fn func(cfg *OcodeConfig) error) (restore func()) {
	prev := upstashBoxSave
	upstashBoxSave = fn
	return func() { upstashBoxSave = prev }
}

// MutateUpstashBox runs fn against a freshly loaded upstash_box section while
// holding the ocodeconfig lock, then saves the file. Only this section is
// changed: every other field is read from disk and written back as it is, so a
// concurrent session's settings survive. fn must not retain the pointer.
func MutateUpstashBox(fn func(c *UpstashBoxConfig) error) error {
	return MutateUpstashBoxGuarded(func(c *UpstashBoxConfig) (func(), error) {
		return nil, fn(c)
	})
}

// MutateUpstashBoxGuarded is MutateUpstashBox for a change that has a side
// effect outside ocodeconfig.json (the API key in auth.json). fn runs under the
// config lock. It must finish every check that can refuse the change before it
// performs the side effect, and returns an undo for that effect. If fn returns
// an error, nothing is saved and fn must have undone its own partial work. If
// the save then fails, undo runs before the lock is released, so a concurrent
// mutation of the same account cannot interleave with the rollback.
func MutateUpstashBoxGuarded(fn func(c *UpstashBoxConfig) (undo func(), err error)) error {
	unlock, err := lockOcodeConfig()
	if err != nil {
		return err
	}
	defer unlock()
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return fmt.Errorf("load ocode config: %w", err)
	}
	cfg.UpstashBox.Accounts = append([]UpstashBoxAccount(nil), cfg.UpstashBox.Accounts...)
	cfg.UpstashBox.normalize()
	undo, err := fn(&cfg.UpstashBox)
	if err != nil {
		return err
	}
	if err := upstashBoxSave(cfg); err != nil {
		if undo != nil {
			undo()
		}
		return err
	}
	OnConfigSaved.Fire()
	return nil
}
