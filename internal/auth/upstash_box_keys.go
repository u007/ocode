package auth

import "strings"

// upstashBoxCredentialPrefix namespaces Upstash Box API keys inside the
// credential store. The store is keyed by provider id, so each account's key
// sits under "upstash-box:<account id>" and is never a provider.
const upstashBoxCredentialPrefix = "upstash-box:"

// UpstashBoxCredentialID returns the auth.json key that holds an account's API key.
func UpstashBoxCredentialID(accountID string) string {
	return upstashBoxCredentialPrefix + accountID
}

// IsUpstashBoxCredentialID reports whether a credential-store key belongs to an
// Upstash Box account rather than to an LLM provider.
func IsUpstashBoxCredentialID(id string) bool {
	return strings.HasPrefix(id, upstashBoxCredentialPrefix)
}

// SetUpstashBoxAPIKey stores an account's API key in auth.json (owner-only).
func SetUpstashBoxAPIKey(accountID, key string) error {
	return Set(UpstashBoxCredentialID(accountID), Credential{Kind: KindAPIKey, Key: key})
}

// RemoveUpstashBoxAPIKey deletes an account's API key from auth.json.
func RemoveUpstashBoxAPIKey(accountID string) error {
	return Remove(UpstashBoxCredentialID(accountID))
}

// UpstashBoxAPIKey returns an account's stored API key. ok is false when the
// account has no key. The key is a secret: callers must not return it to a
// client or write it to a log.
func UpstashBoxAPIKey(accountID string) (key string, ok bool) {
	cred, found := Get(UpstashBoxCredentialID(accountID))
	if !found || cred.Key == "" {
		return "", false
	}
	return cred.Key, true
}
