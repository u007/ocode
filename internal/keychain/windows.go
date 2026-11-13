//go:build windows

package keychain

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// execCommand is indirected so tests can stub the exec call.
var execCommand = exec.CommandContext

// windowsPowerShellBin is the name of the native tool; tests may override it.
var windowsPowerShellBin = "powershell"

func newPlatformStore(opts Options) (Store, bool) {
	if _, err := exec.LookPath(windowsPowerShellBin); err != nil {
		return nil, false
	}
	return &windowsStore{opts: opts}, true
}

type windowsStore struct {
	opts Options
}

func psScript(s string) []string {
	return []string{"-NoProfile", "-NonInteractive", "-Command", s}
}

func (w *windowsStore) Get(service, account string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	script := `$vault = New-Object Windows.Security.Credentials.PasswordVault; try { $c = $vault.Retrieve('` + service + `','` + account + `'); $c.RetrievePassword(); Write-Output $c.Password } catch { exit 1 }`
	cmd := execCommand(ctx, windowsPowerShellBin, psScript(script)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		low := strings.ToLower(string(out))
		if strings.Contains(low, "not found") || strings.Contains(low, "no match") || strings.Contains(low, "exception") {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("keychain: get: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSuffix(strings.TrimSuffix(string(out), "\n"), "\r"), nil
}

func (w *windowsStore) Set(service, account, secret string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	script := `$vault = New-Object Windows.Security.Credentials.PasswordVault; try { $vault.Remove($vault.Retrieve('` + service + `','` + account + `')) } catch {}; $vault.Add((New-Object Windows.Security.Credentials.PasswordCredential('` + service + `','` + account + `','` + secret + `')))`
	cmd := execCommand(ctx, windowsPowerShellBin, psScript(script)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("keychain: set: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (w *windowsStore) Delete(service, account string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	script := `$vault = New-Object Windows.Security.Credentials.PasswordVault; $vault.Remove($vault.Retrieve('` + service + `','` + account + `'))`
	cmd := execCommand(ctx, windowsPowerShellBin, psScript(script)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		low := strings.ToLower(string(out))
		if strings.Contains(low, "not found") || strings.Contains(low, "no match") || strings.Contains(low, "exception") {
			return ErrNotFound
		}
		return fmt.Errorf("keychain: delete: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
