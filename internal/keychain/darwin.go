//go:build darwin

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

// darwinSecurityBin is the name of the native tool; tests may override it.
var darwinSecurityBin = "security"

func newPlatformStore(opts Options) (Store, bool) {
	if _, err := exec.LookPath(darwinSecurityBin); err != nil {
		return nil, false
	}
	return &darwinStore{opts: opts}, true
}

type darwinStore struct {
	opts Options
}

func isNotFoundError(out string, err error) bool {
	l := strings.ToLower(out)
	if strings.Contains(l, "could not be found") ||
		strings.Contains(l, "not found") {
		return true
	}
	return false
}

func (d *darwinStore) Get(service, account string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := execCommand(ctx, darwinSecurityBin, "find-generic-password", "-s", service, "-a", account, "-w")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if isNotFoundError(string(out), err) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("keychain: get: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSuffix(strings.TrimSuffix(string(out), "\n"), "\r"), nil
}

func (d *darwinStore) Set(service, account, secret string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := execCommand(ctx, darwinSecurityBin, "add-generic-password", "-U", "-s", service, "-a", account, "-w", secret)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("keychain: set: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (d *darwinStore) Delete(service, account string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := execCommand(ctx, darwinSecurityBin, "delete-generic-password", "-s", service, "-a", account)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if isNotFoundError(string(out), err) {
			return ErrNotFound
		}
		return fmt.Errorf("keychain: delete: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
