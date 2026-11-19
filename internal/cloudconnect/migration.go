package cloudconnect

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/u007/ocode/internal/cloudkeystore"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/encryption"
	"github.com/u007/ocode/internal/keychain"
	"github.com/u007/ocode/internal/paths"
)

// The migration reads the legacy v1 secret sources once, then writes the v2
// file. These seams keep the one-time migration deterministic in tests without
// overriding HOME (ocode's path resolution is platform-split via XDG vars).
var (
	cloudDataDir         = paths.GlobalDataDir
	resolveCloudKey      = cloudkeystore.Resolve
	newLegacySecretStore = func(dataDir, password string) keychain.Store {
		return keychain.New(
			keychain.WithPath(filepath.Join(dataDir, "cloud-secrets.enc")),
			keychain.WithMasterPassword(password),
		)
	}
	migrateMasterPassword = func() string { return os.Getenv("OCODE_CLOUD_MASTER_PASSWORD") }
)

// legacyConfigPath mirrors config's v1 connectorsFilePath so the migration can
// refuse a project with no legacy file instead of writing an empty v2 file.
func legacyConfigPath(projectRoot string) (string, error) {
	if projectRoot != "" {
		return filepath.Join(projectRoot, ".ocode", "storage-connectors.json"), nil
	}
	dir, err := paths.GlobalDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "storage-connectors.json"), nil
}

// legacySecretsPath mirrors Manager.secretsPath for the legacy ENCv1 file.
func legacySecretsPath(projectRoot, dataDir string) string {
	base := dataDir
	if projectRoot != "" {
		base = filepath.Join(projectRoot, ".ocode")
	}
	return filepath.Join(base, "cloudconnect-secrets.enc")
}

// MigrateV1ToV2 folds a project's legacy v1 metadata and secrets into a v2
// local-mode file. It is additive and non-destructive: both the keychain "_all"
// blob and cloudconnect-secrets.enc are read only, never deleted or rewritten.
// The old refresh tokens are kept solely in the local credentials envelope;
// no old secret is transferred to kakiit.
func MigrateV1ToV2(projectRoot string) error {
	path, err := legacyConfigPath(projectRoot)
	if err != nil {
		return fmt.Errorf("cloudconnect: legacy config path: %w", err)
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("cloudconnect: no legacy v1 config to migrate: %w", err)
	}
	v1, err := config.LoadStorageConnectors(projectRoot)
	if err != nil {
		return fmt.Errorf("cloudconnect: load v1 config: %w", err)
	}
	// v1 and v2 share one file path, discriminated by "version". Once the file
	// is on v2 the migration is already done — re-running it would overwrite
	// live hub-mode state with an empty local-mode file.
	if v1.Version == "v2" {
		return nil
	}

	dataDir, err := cloudDataDir()
	if err != nil {
		return fmt.Errorf("cloudconnect: global data dir: %w", err)
	}
	password := migrateMasterPassword()
	secrets := map[string]SecretBundle{}

	// Fallback source first: the ENCv1 file. A missing file is benign, but a
	// corrupt file or wrong master password must abort — silently migrating an
	// empty envelope over v1 would lose the only copy of those secrets.
	raw, err := os.ReadFile(legacySecretsPath(projectRoot, dataDir))
	switch {
	case err == nil:
		plain, err := encryption.DecryptJSON(string(raw), password)
		if err != nil {
			return fmt.Errorf("cloudconnect: decrypt legacy secrets file: %w", err)
		}
		var fromFile map[string]SecretBundle
		if err := json.Unmarshal(plain, &fromFile); err != nil {
			return fmt.Errorf("cloudconnect: parse legacy secrets file: %w", err)
		}
		for id, sec := range fromFile {
			secrets[id] = sec
		}
	case errors.Is(err, os.ErrNotExist):
		// benign: this machine used only the keychain store
	default:
		return fmt.Errorf("cloudconnect: read legacy secrets file: %w", err)
	}

	// Primary source: the keychain "_all" blob overwrites any file-only entry.
	store := newLegacySecretStore(dataDir, password)
	if store != nil {
		raw, err := store.Get("ocode-cloud", "_all")
		switch {
		case err == nil:
			var fromKeychain map[string]SecretBundle
			if err := json.Unmarshal([]byte(raw), &fromKeychain); err != nil {
				return fmt.Errorf("cloudconnect: parse legacy keychain blob: %w", err)
			}
			for id, sec := range fromKeychain {
				secrets[id] = sec
			}
		case errors.Is(err, keychain.ErrNotFound):
			// benign: this machine used only the secrets file
		default:
			return fmt.Errorf("cloudconnect: read legacy keychain blob: %w", err)
		}
	}

	key, _, err := resolveCloudKey()
	if err != nil {
		return fmt.Errorf("cloudconnect: resolve install key: %w", err)
	}

	connections := map[string]config.ConnectionMeta{}
	credentials := map[string]Credentials{}
	for id, setting := range v1.Settings {
		provider := setting.Provider
		if provider == "" {
			provider = id
		}
		connections[provider] = config.ConnectionMeta{
			Provider:      provider,
			RootPath:      setting.RootPath,
			MaxCacheBytes: setting.MaxCacheBytes,
			Status:        "local",
		}
		credentials[provider] = credentialsFromSecretBundle(secrets[id])
		delete(secrets, id)
	}
	// Any secret without v1 metadata is still preserved, keyed by its old id.
	for id, sec := range secrets {
		credentials[id] = credentialsFromSecretBundle(sec)
	}

	plain, err := json.Marshal(credentials)
	if err != nil {
		return fmt.Errorf("cloudconnect: marshal credentials: %w", err)
	}
	envelope, err := encryption.SealWithKey(plain, key)
	if err != nil {
		return fmt.Errorf("cloudconnect: seal credentials: %w", err)
	}

	v2 := &config.StorageConnectorsFileV2{
		Version: "v2",
		Central: config.CentralConnector{
			ConnectorID: "kakiit",
			Mode:        "local",
			Connections: connections,
		},
		CredentialsEnvelope: envelope,
	}
	if err := config.SaveV2(projectRoot, v2); err != nil {
		return fmt.Errorf("cloudconnect: save v2 config: %w", err)
	}
	return nil
}
