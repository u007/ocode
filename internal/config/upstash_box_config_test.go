package config

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func validUpstashAccount(id, name string) UpstashBoxAccount {
	return UpstashBoxAccount{
		ID:             id,
		Name:           name,
		Size:           UpstashBoxSizeSmall,
		IdleMinutes:    UpstashBoxDefaultIdleMinutes,
		HardCapMinutes: UpstashBoxDefaultHardCapMinutes,
		RetainDays:     UpstashBoxDefaultRetainDays,
	}
}

func TestUpstashBoxAccountValidateBounds(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*UpstashBoxAccount)
		wantErr bool
	}{
		{"defaults are valid", func(a *UpstashBoxAccount) {}, false},
		{"hard cap at 3 days is valid", func(a *UpstashBoxAccount) { a.HardCapMinutes = 4320 }, false},
		{"hard cap above 3 days is rejected", func(a *UpstashBoxAccount) { a.HardCapMinutes = 4321 }, true},
		{"hard cap zero is rejected", func(a *UpstashBoxAccount) { a.HardCapMinutes = 0 }, true},
		{"idle zero is rejected", func(a *UpstashBoxAccount) { a.IdleMinutes = 0 }, true},
		{"idle one is valid", func(a *UpstashBoxAccount) { a.IdleMinutes = 1 }, false},
		{"retain zero is rejected", func(a *UpstashBoxAccount) { a.RetainDays = 0 }, true},
		{"unknown size is rejected", func(a *UpstashBoxAccount) { a.Size = "huge" }, true},
		{"empty size is rejected", func(a *UpstashBoxAccount) { a.Size = "" }, true},
		{"blank name is rejected", func(a *UpstashBoxAccount) { a.Name = "   " }, true},
		{"overlong name is rejected", func(a *UpstashBoxAccount) { a.Name = strings.Repeat("n", 65) }, true},
		{"idle 4320 is valid with cap 4320", func(a *UpstashBoxAccount) { a.IdleMinutes, a.HardCapMinutes = 4320, 4320 }, false},
		{"idle 4321 is rejected", func(a *UpstashBoxAccount) { a.IdleMinutes, a.HardCapMinutes = 4321, 4320 }, true},
		{"idle above hard cap is rejected", func(a *UpstashBoxAccount) { a.IdleMinutes = 121 }, true},
		{"idle equal to hard cap is valid", func(a *UpstashBoxAccount) { a.IdleMinutes = 120 }, false},
		{"retain 365 is valid", func(a *UpstashBoxAccount) { a.RetainDays = 365 }, false},
		{"retain 366 is rejected", func(a *UpstashBoxAccount) { a.RetainDays = 366 }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			acc := validUpstashAccount("ub_1", "box")
			tc.mutate(&acc)
			err := acc.Validate()
			if tc.wantErr {
				if !errors.Is(err, ErrUpstashBoxInvalid) {
					t.Fatalf("Validate() = %v, want ErrUpstashBoxInvalid", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestUpstashBoxFirstAccountBecomesDefaultAndListIsSorted(t *testing.T) {
	var c UpstashBoxConfig
	if err := c.AddAccount(validUpstashAccount("ub_b", "Beta")); err != nil {
		t.Fatal(err)
	}
	if def, ok := c.DefaultAccount(); !ok || def.ID != "ub_b" {
		t.Fatalf("first account should be default, got %+v ok=%v", def, ok)
	}
	if err := c.AddAccount(validUpstashAccount("ub_a", "alpha")); err != nil {
		t.Fatal(err)
	}
	if def, _ := c.DefaultAccount(); def.ID != "ub_b" {
		t.Fatalf("adding a second account must not move the default, got %s", def.ID)
	}
	// Case-insensitive name order: alpha sorts before Beta.
	if c.Accounts[0].Name != "alpha" || c.Accounts[1].Name != "Beta" {
		t.Fatalf("accounts not sorted by name: %+v", c.Accounts)
	}
}

func TestUpstashBoxDuplicateNameRejectedCaseInsensitive(t *testing.T) {
	var c UpstashBoxConfig
	if err := c.AddAccount(validUpstashAccount("ub_1", "Prod")); err != nil {
		t.Fatal(err)
	}
	err := c.AddAccount(validUpstashAccount("ub_2", "prod"))
	if !errors.Is(err, ErrUpstashBoxInvalid) {
		t.Fatalf("duplicate name: got %v, want ErrUpstashBoxInvalid", err)
	}
	if len(c.Accounts) != 1 {
		t.Fatalf("rejected add must not change the list, got %d accounts", len(c.Accounts))
	}
	// Renaming an account onto its own name is not a duplicate.
	self := validUpstashAccount("ub_1", "PROD")
	if err := c.UpdateAccount(self); err != nil {
		t.Fatalf("update to own name (case change) rejected: %v", err)
	}
}

func TestUpstashBoxDeleteDefaultReassignsThenLastAccountResetsBackend(t *testing.T) {
	var c UpstashBoxConfig
	_ = c.AddAccount(validUpstashAccount("ub_b", "beta"))
	_ = c.AddAccount(validUpstashAccount("ub_a", "alpha"))
	backend := UpstashBoxBackendUpstash
	if err := c.SetSettings(&backend, nil); err != nil {
		t.Fatal(err)
	}

	if err := c.RemoveAccount("ub_b"); err != nil {
		t.Fatal(err)
	}
	if def, ok := c.DefaultAccount(); !ok || def.ID != "ub_a" {
		t.Fatalf("deleting the default must reassign to the first remaining account, got %+v", def)
	}
	if c.Backend() != UpstashBoxBackendUpstash {
		t.Fatalf("backend changed while accounts remain: %q", c.Backend())
	}

	if err := c.RemoveAccount("ub_a"); err != nil {
		t.Fatal(err)
	}
	if c.Backend() != UpstashBoxBackendLocal {
		t.Fatalf("deleting the last account must reset backend to local, got %q", c.Backend())
	}
	if c.DefaultAccountID != "" {
		t.Fatalf("default account must be empty with no accounts, got %q", c.DefaultAccountID)
	}
	if err := c.RemoveAccount("ub_missing"); !errors.Is(err, ErrUpstashBoxNotFound) {
		t.Fatalf("unknown id: got %v, want ErrUpstashBoxNotFound", err)
	}
}

func TestUpstashBoxSetSettingsRefusals(t *testing.T) {
	var c UpstashBoxConfig
	upstash := UpstashBoxBackendUpstash
	if err := c.SetSettings(&upstash, nil); !errors.Is(err, ErrUpstashBoxInvalid) {
		t.Fatalf("upstash with no accounts: got %v, want ErrUpstashBoxInvalid", err)
	}
	_ = c.AddAccount(validUpstashAccount("ub_1", "box"))

	unknown := "ub_nope"
	if err := c.SetSettings(nil, &unknown); !errors.Is(err, ErrUpstashBoxInvalid) {
		t.Fatalf("unknown default account: got %v, want ErrUpstashBoxInvalid", err)
	}
	bogus := "gpu"
	if err := c.SetSettings(&bogus, nil); !errors.Is(err, ErrUpstashBoxInvalid) {
		t.Fatalf("unknown backend: got %v, want ErrUpstashBoxInvalid", err)
	}
	if err := c.SetSettings(&upstash, nil); err != nil {
		t.Fatalf("upstash with an account: %v", err)
	}
	if c.Backend() != UpstashBoxBackendUpstash {
		t.Fatalf("backend = %q, want upstash", c.Backend())
	}
}

// Round trip through ocodeconfig.json: the section persists, reloads, and the
// file holds no key material (the config layer never receives one).
func TestUpstashBoxMutatePersistsSectionRoundTrip(t *testing.T) {
	setHomeTree(t, t.TempDir())

	err := MutateUpstashBox(func(c *UpstashBoxConfig) error {
		return c.AddAccount(validUpstashAccount("ub_1", "box one"))
	})
	if err != nil {
		t.Fatalf("MutateUpstashBox: %v", err)
	}
	got, err := LoadUpstashBoxConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Accounts) != 1 || got.Accounts[0].Name != "box one" || got.DefaultAccountID != "ub_1" {
		t.Fatalf("reloaded section = %+v", got)
	}
	acc, ok, err := LoadDefaultUpstashBoxAccount()
	if err != nil || !ok || acc.ID != "ub_1" || acc.HardCapMinutes != UpstashBoxDefaultHardCapMinutes {
		t.Fatalf("LoadDefaultUpstashBoxAccount = %+v ok=%v err=%v", acc, ok, err)
	}
}

// A save of the upstash section must not erase an unrelated field another
// session wrote after this process loaded its config.
func TestUpstashBoxSaveKeepsConcurrentUnrelatedWrite(t *testing.T) {
	setHomeTree(t, t.TempDir())

	// This process loads its config snapshot first.
	if _, err := LoadOcodeConfigCopy(); err != nil {
		t.Fatal(err)
	}

	// Another session saves an unrelated field after that load.
	if err := SaveEditor("vim"); err != nil {
		t.Fatalf("SaveEditor: %v", err)
	}

	// This process then saves an Upstash account.
	if err := MutateUpstashBox(func(c *UpstashBoxConfig) error {
		return c.AddAccount(validUpstashAccount("ub_1", "box"))
	}); err != nil {
		t.Fatalf("MutateUpstashBox: %v", err)
	}

	reloaded, err := LoadOcodeConfigCopy()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Editor != "vim" {
		t.Fatalf("unrelated editor field was erased: got %q, want vim", reloaded.Editor)
	}
	if len(reloaded.UpstashBox.Accounts) != 1 {
		t.Fatalf("upstash account not persisted: %+v", reloaded.UpstashBox)
	}
	path, err := ActiveOcodeConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"editor"`) || !strings.Contains(string(raw), `"upstash_box"`) {
		t.Fatalf("ocodeconfig.json missing a section after the save:\n%s", raw)
	}
}

func TestValidateUpstashAPIKey(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{"typical key", "ubx-abc123_DEF-456", false},
		{"512 bytes", strings.Repeat("a", 512), false},
		{"513 bytes", strings.Repeat("a", 513), true},
		{"empty", "", true},
		{"inner space", "abc def", true},
		{"newline", "abc\ndef", true},
		{"carriage return", "abc\rdef", true},
		{"tab", "abc\tdef", true},
		{"nul byte", "abc\x00def", true},
		{"DEL control", "abc\x7fdef", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateUpstashAPIKey(tc.key)
			if tc.wantErr != (err != nil) {
				t.Fatalf("ValidateUpstashAPIKey(%q) = %v, wantErr %v", tc.key, err, tc.wantErr)
			}
			if err != nil && !errors.Is(err, ErrUpstashBoxInvalid) {
				t.Fatalf("error %v does not match ErrUpstashBoxInvalid", err)
			}
		})
	}
}

// The undo of a guarded change runs when the config save fails, and does not
// run when the change is saved.
func TestMutateUpstashBoxGuardedUndoOnSaveFailure(t *testing.T) {
	setHomeTree(t, t.TempDir())

	undone := 0
	restore := SetUpstashBoxSaveForTest(func(*OcodeConfig) error { return errors.New("disk full") })
	err := MutateUpstashBoxGuarded(func(c *UpstashBoxConfig) (func(), error) {
		if err := c.AddAccount(validUpstashAccount("ub_1", "box")); err != nil {
			return nil, err
		}
		return func() { undone++ }, nil
	})
	restore()
	if err == nil {
		t.Fatal("expected the injected save failure")
	}
	if undone != 1 {
		t.Fatalf("undo ran %d times on save failure, want 1", undone)
	}

	err = MutateUpstashBoxGuarded(func(c *UpstashBoxConfig) (func(), error) {
		if err := c.AddAccount(validUpstashAccount("ub_2", "box2")); err != nil {
			return nil, err
		}
		return func() { undone++ }, nil
	})
	if err != nil {
		t.Fatalf("saved change failed: %v", err)
	}
	if undone != 1 {
		t.Fatalf("undo ran on a successful save (count %d)", undone)
	}
}

// A refusal inside fn writes nothing and runs no undo.
func TestMutateUpstashBoxGuardedRefusalWritesNothing(t *testing.T) {
	setHomeTree(t, t.TempDir())
	if err := MutateUpstashBox(func(c *UpstashBoxConfig) error {
		return c.AddAccount(validUpstashAccount("ub_1", "taken"))
	}); err != nil {
		t.Fatal(err)
	}
	writes := 0
	restore := SetUpstashBoxSaveForTest(func(cfg *OcodeConfig) error {
		writes++
		return SaveOcodeConfig(cfg)
	})
	defer restore()

	err := MutateUpstashBoxGuarded(func(c *UpstashBoxConfig) (func(), error) {
		if err := c.AddAccount(validUpstashAccount("ub_2", "TAKEN")); err != nil {
			return nil, err
		}
		t.Fatal("fn must not run past a refused add")
		return nil, nil
	})
	if !errors.Is(err, ErrUpstashBoxInvalid) {
		t.Fatalf("duplicate: got %v, want ErrUpstashBoxInvalid", err)
	}
	if writes != 0 {
		t.Fatalf("refused change still saved the config %d times", writes)
	}
}
