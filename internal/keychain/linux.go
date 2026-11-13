//go:build linux

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

// linuxSecretToolBin is the name of the native tool; tests may override it.
var linuxSecretToolBin = "secret-tool"

func newPlatformStore(opts Options) (Store, bool) {
	if _, err := exec.LookPath(linuxSecretToolBin); err != nil {
		return nil, false
	}
	return &linuxStore{opts: opts}, true
}

type linuxStore struct {
	opts Options
}

func (l *linuxStore) Get(service, account string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := execCommand(ctx, linuxSecretToolBin, "lookup", "service", service, "account", account)
	out, err := cmd.CombinedOutput()
	if err != nil {
		low := strings.ToLower(string(out))
		if strings.Contains(low, "not found") || strings.Contains(low, "no such") {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("keychain: get: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSuffix(strings.TrimSuffix(string(out), "\n"), "\r"), nil
}

func (l *linuxStore) Set(service, account, secret string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := execCommand(ctx, linuxSecretToolBin, "store", "--label=ocode", "service", service, "account", account)
	cmd.Stdin = strings.NewReader(secret)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("keychain: set: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (l *linuxStore) Delete(service, account string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := execCommand(ctx, linuxSecretToolBin, "clear", "service", service, "account", account)
	out, err := cmd.CombinedOutput()
	if err != nil {
		low := strings.ToLower(string(out))
		if strings.Contains(low, "not found") || strings.Contains(low, "no such") {
			return ErrNotFound
		}
		return fmt.Errorf("keychain: delete: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
