// Package secretcli implements the `ocode secret` subcommand family:
// initializing a per-project age key and encrypting/decrypting individual
// files or whole directories with it from the command line, independent of
// the TUI/web editor integrations that use the same internal/secretfile
// and internal/secretjob packages.
package secretcli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/u007/ocode/internal/paths"
	"github.com/u007/ocode/internal/secretfile"
	"github.com/u007/ocode/internal/secretjob"
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

// readConfirm prompts on stderr and reads a y/N answer from stdin.
// Overridden in tests.
var readConfirm = func(prompt string) (bool, error) {
	fmt.Fprint(os.Stderr, prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes", nil
}

// SetIO temporarily overrides how Run reads passphrases and confirmations
// to use stdin/stderr explicitly, for callers (e.g. the TUI's suspended
// terminal) that have been handed a specific stdin/stderr by their host
// program rather than owning the process's os.Stdin/os.Stderr directly. It
// returns a restore function that must be called once Run has finished.
func SetIO(stdin *os.File, stderr io.Writer) (restore func()) {
	origPassphrase := readPassphrase
	origConfirm := readConfirm

	readPassphrase = func(prompt string) (string, error) {
		fmt.Fprint(stderr, prompt)
		b, err := term.ReadPassword(int(stdin.Fd()))
		fmt.Fprintln(stderr)
		if err != nil {
			return "", fmt.Errorf("read passphrase: %w", err)
		}
		return string(b), nil
	}
	readConfirm = func(prompt string) (bool, error) {
		fmt.Fprint(stderr, prompt)
		line, err := bufio.NewReader(stdin).ReadString('\n')
		if err != nil && err != io.EOF {
			return false, fmt.Errorf("read confirmation: %w", err)
		}
		line = strings.ToLower(strings.TrimSpace(line))
		return line == "y" || line == "yes", nil
	}

	return func() {
		readPassphrase = origPassphrase
		readConfirm = origConfirm
	}
}

const usage = "usage: ocode secret init [path] | ocode secret encrypt <file|dir> | ocode secret decrypt <file|dir> | ocode secret rekey [path]"

// Run dispatches a `ocode secret <subcommand> [args...]` invocation.
func Run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", usage)
	}
	switch args[0] {
	case "init":
		return runInit(args[1:])
	case "encrypt":
		return runTransform(args[1:], true)
	case "decrypt":
		return runTransform(args[1:], false)
	case "rekey":
		return runRekey(args[1:])
	default:
		return fmt.Errorf("unknown secret subcommand %q (want init, encrypt, decrypt, rekey)", args[0])
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

func runRekey(args []string) error {
	wd := "."
	if len(args) > 0 {
		wd = args[0]
	}
	root := paths.ProjectRoot(wd)
	keyPath := secretfile.ProjectKeyPath(root)
	if _, err := os.Stat(keyPath); err != nil {
		return fmt.Errorf("no project key at %s; run `ocode secret init` first", keyPath)
	}

	oldPass, err := readPassphrase("Current passphrase: ")
	if err != nil {
		return err
	}
	newPass, err := readPassphrase("New passphrase: ")
	if err != nil {
		return err
	}
	confirmPass, err := readPassphrase("Confirm new passphrase: ")
	if err != nil {
		return err
	}
	if newPass != confirmPass {
		return fmt.Errorf("passphrases did not match")
	}

	if err := secretfile.RekeyProjectKey(keyPath, oldPass, newPass); err != nil {
		return err
	}

	fmt.Printf("Passphrase updated for %s\n", keyPath)
	return nil
}

func runTransform(args []string, encrypt bool) error {
	if len(args) != 1 {
		return fmt.Errorf("%s", usage)
	}
	path := args[0]

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", path, err)
	}
	// Resolve symlinks so absPath agrees with paths.ProjectRoot (which does
	// the same) about the project key's path; otherwise a symlinked temp
	// dir (e.g. macOS /var -> /private/var) makes the string comparison in
	// secretjob.Walk that excludes the key file itself fail to match.
	if resolved, err := filepath.EvalSymlinks(absPath); err == nil {
		absPath = resolved
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}

	rootFor := absPath
	if !info.IsDir() {
		rootFor = filepath.Dir(absPath)
	}
	root := paths.ProjectRoot(rootFor)
	keyPath := secretfile.ProjectKeyPath(root)
	if _, err := os.Stat(keyPath); err != nil {
		return fmt.Errorf("no project key at %s; run `ocode secret init` first", keyPath)
	}

	var files []string
	if info.IsDir() {
		files, err = secretjob.Walk(absPath, keyPath, encrypt)
		if err != nil {
			return fmt.Errorf("scan %s: %w", path, err)
		}
		verb := "decrypt"
		if encrypt {
			verb = "encrypt"
		}
		if len(files) == 0 {
			fmt.Printf("No files to %s under %s\n", verb, path)
			return nil
		}
		ok, err := readConfirm(fmt.Sprintf("%s%s %d file(s) under %s? [y/N] ", strings.ToUpper(verb[:1]), verb[1:], len(files), path))
		if err != nil {
			return err
		}
		if !ok {
			fmt.Println("Aborted")
			return nil
		}
	} else {
		files = []string{absPath}
	}

	passphrase, err := readPassphrase("Passphrase: ")
	if err != nil {
		return err
	}
	if encrypt {
		confirmPass, err := readPassphrase("Confirm passphrase: ")
		if err != nil {
			return err
		}
		if passphrase != confirmPass {
			return fmt.Errorf("passphrases did not match")
		}
	}

	key, err := secretfile.UnlockProjectKey(keyPath, passphrase)
	if err != nil {
		return err
	}

	verbPast := "Decrypted"
	if encrypt {
		verbPast = "Encrypted"
	}
	multi := len(files) > 1
	for i, f := range files {
		if err := secretjob.TransformFile(key, f, encrypt); err != nil {
			return err
		}
		if multi {
			rel, err := filepath.Rel(rootFor, f)
			if err != nil {
				rel = f
			}
			fmt.Printf("[%d/%d] %s %s\n", i+1, len(files), verbPast, rel)
		}
	}

	if multi {
		fmt.Printf("%s %d file(s) under %s\n", verbPast, len(files), path)
	} else if encrypt {
		fmt.Printf("Encrypted %s\n", path)
	} else {
		fmt.Printf("Decrypted %s (plaintext now on disk)\n", path)
	}
	return nil
}
