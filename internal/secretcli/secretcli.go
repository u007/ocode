// Package secretcli implements the `ocode secret` subcommand family:
// initializing a per-project age key and encrypting/decrypting individual
// files with it from the command line, independent of the TUI/web editor
// integrations that use the same internal/secretfile package.
package secretcli

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/term"

	"github.com/u007/ocode/internal/paths"
	"github.com/u007/ocode/internal/secretfile"
)

// readPassphrase prompts on stderr and reads a passphrase from the
// controlling terminal without echoing it. Overridden in tests.
var readPassphrase = func(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read passphrase: %w", err)
	}
	return string(b), nil
}

// Run dispatches a `ocode secret <subcommand> [args...]` invocation.
func Run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ocode secret init [path] | ocode secret encrypt <file> | ocode secret decrypt <file>")
	}
	switch args[0] {
	case "init":
		return runInit(args[1:])
	case "encrypt":
		return runEncrypt(args[1:])
	case "decrypt":
		return runDecrypt(args[1:])
	default:
		return fmt.Errorf("unknown secret subcommand %q (want init, encrypt, decrypt)", args[0])
	}
}

func runInit(args []string) error {
	wd := "."
	if len(args) > 0 {
		wd = args[0]
	}
	root := paths.ProjectRoot(wd)
	keyPath := secretfile.ProjectKeyPath(root)

	passphrase, err := readPassphrase("Set a passphrase for this project: ")
	if err != nil {
		return err
	}
	confirm, err := readPassphrase("Confirm passphrase: ")
	if err != nil {
		return err
	}
	if passphrase != confirm {
		return fmt.Errorf("passphrases did not match")
	}

	if _, err := secretfile.GenerateProjectKey(keyPath, passphrase); err != nil {
		return err
	}

	fmt.Printf("Project key created at %s\n", keyPath)
	return nil
}

func runEncrypt(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: ocode secret encrypt <file>")
	}
	return transform(args[0], true)
}

func runDecrypt(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: ocode secret decrypt <file>")
	}
	return transform(args[0], false)
}

func transform(file string, encrypt bool) error {
	absFile, err := filepath.Abs(file)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", file, err)
	}

	info, err := os.Stat(absFile)
	if err != nil {
		return fmt.Errorf("stat %s: %w", file, err)
	}

	root := paths.ProjectRoot(filepath.Dir(absFile))
	keyPath := secretfile.ProjectKeyPath(root)
	if _, err := os.Stat(keyPath); err != nil {
		return fmt.Errorf("no project key at %s; run `ocode secret init` first", keyPath)
	}

	passphrase, err := readPassphrase("Passphrase: ")
	if err != nil {
		return err
	}
	key, err := secretfile.UnlockProjectKey(keyPath, passphrase)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(absFile)
	if err != nil {
		return fmt.Errorf("read %s: %w", file, err)
	}

	var out []byte
	if encrypt {
		if secretfile.IsEncrypted(data) {
			return fmt.Errorf("%s is already encrypted", file)
		}
		if out, err = key.Encrypt(data); err != nil {
			return err
		}
	} else {
		if !secretfile.IsEncrypted(data) {
			return fmt.Errorf("%s is not encrypted", file)
		}
		if out, err = key.Decrypt(data); err != nil {
			return err
		}
	}

	if err := secretfile.WriteFileAtomic(absFile, out, info.Mode().Perm()); err != nil {
		return err
	}

	if encrypt {
		fmt.Printf("Encrypted %s\n", file)
	} else {
		fmt.Printf("Decrypted %s (plaintext now on disk)\n", file)
	}
	return nil
}
