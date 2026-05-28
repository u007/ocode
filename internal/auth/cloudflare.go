package auth

import "fmt"

// CloudflareWorkersBaseURL builds the Workers AI endpoint URL for a given account ID.
// Store this in Credential.BaseURL at save time so NewClient picks it up via
// auth.GetBaseURL without needing to parse it back.
func CloudflareWorkersBaseURL(accountID string) string {
	return fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai/v1", accountID)
}
