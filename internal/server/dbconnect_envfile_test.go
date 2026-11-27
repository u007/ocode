package server

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
)

func TestDBEnvLookupNamedFileWinsOverProcessEnv(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env.staging"), []byte("DATABASE_URL=postgres://u:p@staging:5432/db\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/local")

	lookup, envErr := dbEnvLookup(dir, ".staging")
	if err := envErr(); err != nil {
		t.Fatalf("envErr: %v", err)
	}
	got, ok := lookup("DATABASE_URL")
	if !ok || got != "postgres://u:p@staging:5432/db" {
		t.Fatalf("lookup = %q, %v; want the file value", got, ok)
	}
}

func TestDBEnvLookupMissingKeyInNamedFileStaysUnset(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env.staging"), []byte("OTHER=yes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/local")

	lookup, envErr := dbEnvLookup(dir, ".env.staging")
	if err := envErr(); err != nil {
		t.Fatalf("envErr: %v", err)
	}
	if got, ok := lookup("DATABASE_URL"); ok {
		t.Fatalf("lookup = %q, true; want unset (process env must not fill a named-file gap)", got)
	}
}

func TestDBEnvLookupDefaultKeepsProcessEnvPrecedence(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("DATABASE_URL=from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATABASE_URL", "from-process")
	lookup, _ := dbEnvLookup(dir, "")
	if got, ok := lookup("DATABASE_URL"); !ok || got != "from-process" {
		t.Fatalf("lookup = %q, %v; want from-process", got, ok)
	}
}

func TestDBResolveTunnelExpandsFieldsAndPorts(t *testing.T) {
	tun := &config.DBTunnel{
		Host:          "${SSH_HOST}",
		PortEnv:       "${SSH_PORT}",
		RemoteHost:    "${DB_HOST}",
		RemotePortEnv: "${DB_PORT}",
		IdentityFile:  "${SSH_KEY}",
		AuthMethod:    "password",
		Password:      "sealed-envelope-$ignored",
	}
	lookup := func(name string) (string, bool) {
		vals := map[string]string{
			"SSH_HOST": "bastion.example", "SSH_PORT": "2222",
			"DB_HOST": "db.internal", "DB_PORT": "5432",
			"SSH_KEY": "/keys/id_ed25519",
		}
		v, ok := vals[name]
		return v, ok
	}
	got, err := dbResolveTunnel(tun, lookup, func() error { return nil })
	if err != nil {
		t.Fatalf("dbResolveTunnel: %v", err)
	}
	if got.Host != "bastion.example" || got.Port != 2222 || got.RemoteHost != "db.internal" || got.RemotePort != 5432 {
		t.Fatalf("resolved tunnel = %+v", got)
	}
	if got.IdentityFile != "/keys/id_ed25519" || got.AuthMethod != "password" {
		t.Fatalf("resolved tunnel = %+v", got)
	}
	if got.PortEnv != "" || got.RemotePortEnv != "" {
		t.Fatalf("template fields survived resolution: %+v", got)
	}
	if got.Password != tun.Password {
		t.Fatalf("password was expanded: %q", got.Password)
	}
}

func TestDBResolveTunnelRejectsUnsetVariableWithoutItsValue(t *testing.T) {
	tun := &config.DBTunnel{Host: "${SSH_HOST_IS_UNSET}", RemoteHost: "db", RemotePort: 5432}
	_, err := dbResolveTunnel(tun, func(string) (string, bool) { return "", false }, func() error { return nil })
	if err == nil || !strings.Contains(err.Error(), "SSH_HOST_IS_UNSET") {
		t.Fatalf("err = %v, want the missing variable name", err)
	}
}

func TestDBResolveTunnelRejectsInvalidResolvedPort(t *testing.T) {
	tun := &config.DBTunnel{Host: "bastion", RemoteHost: "db", RemotePortEnv: "${DB_PORT}"}
	lookup := func(string) (string, bool) { return "70000", true }
	_, err := dbResolveTunnel(tun, lookup, func() error { return nil })
	if err == nil || !strings.Contains(err.Error(), "remote_port") {
		t.Fatalf("err = %v, want a remote_port error", err)
	}
}

func TestDBResolveTunnelValidatesAfterExpansion(t *testing.T) {
	tun := &config.DBTunnel{Host: "bastion", RemoteHost: "db", RemotePort: 5432, IdentityFile: "${SSH_KEY}"}
	lookup := func(string) (string, bool) { return "relative/key", true }
	if _, err := dbResolveTunnel(tun, lookup, func() error { return nil }); err == nil {
		t.Fatal("a relative identity file from the env file was accepted")
	}
}

func TestDBConnectUnlockResolvesTunnelFromNamedEnvFile(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h, root := newFilesHandler(t)
	body := "DATABASE_URL=postgres://u:p@db.internal:5432/db\n" +
		"SSH_HOST=bastion.example\nDB_HOST=db.internal\nSSH_PORT=2222\nDB_PORT=5432\n"
	if err := os.WriteFile(filepath.Join(root, ".env.staging"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	stubDBEnsureTunnel(t, func(context.Context, string, *config.DBTunnel, string) (int, error) {
		return 15432, nil
	})

	add := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/add", map[string]any{
		"name": "staging", "url": "${DATABASE_URL}", "env_file": ".staging", "project_path": root,
		"tunnel": map[string]any{
			"host": "${SSH_HOST}", "remote_host": "${DB_HOST}",
			"remote_port_env": "${DB_PORT}",
		},
	})
	if add.Code != http.StatusCreated {
		t.Fatalf("add = %d body=%s", add.Code, add.Body.String())
	}

	unlock := dbPostJSON(t, h.HandleDBConnectUnlock, "/api/dbconnect/unlock", map[string]any{
		"surface": "s-env", "project_path": root,
	})
	if unlock.Code != http.StatusOK {
		t.Fatalf("unlock = %d body=%s", unlock.Code, unlock.Body.String())
	}

	h.dbMu.Lock()
	entry := h.dbGrants["s-env"]["staging"]
	h.dbMu.Unlock()
	if !entry.hasTunnel || entry.tunnel.Host != "bastion.example" || entry.tunnel.RemoteHost != "db.internal" || entry.tunnel.RemotePort != 5432 {
		t.Fatalf("grant tunnel = %+v (hasTunnel=%v)", entry.tunnel, entry.hasTunnel)
	}
	if entry.tunnel.RemotePortEnv != "" {
		t.Fatalf("template field survived into the grant: %+v", entry.tunnel)
	}
}

// A selected file that does not exist must fail its connection, even when the
// URL itself is sealed and needs no template expansion.
func TestDBConnectUnlockFailsWhenSelectedEnvFileIsMissing(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h, root := newFilesHandler(t)
	if add := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/add", map[string]any{
		"name": "sealed", "url": dbTestURL, "password": "master-pw",
	}); add.Code != http.StatusCreated {
		t.Fatalf("add = %d body=%s", add.Code, add.Body.String())
	}
	// The add API refuses env_file on a literal URL, so the stale selection is
	// written into the saved config directly. Unlock must still refuse it.
	cfg, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatal(err)
	}
	for i := range cfg.DB.Connections {
		if cfg.DB.Connections[i].Name == "sealed" {
			cfg.DB.Connections[i].EnvFile = ".env.missing"
		}
	}
	if err := config.SaveOcodeConfig(cfg); err != nil {
		t.Fatal(err)
	}
	rec := dbPostJSON(t, h.HandleDBConnectUnlock, "/api/dbconnect/unlock", map[string]any{
		"surface": "s-missing", "project_path": root, "password": "master-pw",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unlock = %d body=%s; want 400 when nothing can be granted", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), ".env.missing") {
		t.Fatalf("unlock body = %s, want the missing file named", rec.Body.String())
	}
	h.dbMu.Lock()
	_, granted := h.dbGrants["s-missing"]["sealed"]
	h.dbMu.Unlock()
	if granted {
		t.Fatal("a connection with a missing selected env file was granted")
	}
}

// A resolved tunnel must actually be the one used to open the connection:
// the grant dials the local port that the tunnel manager reports.
func TestDBConnectTunnelFromNamedEnvFileDialsTunnelPort(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h, root := newFilesHandler(t)
	body := "DATABASE_URL=postgres://u:p@db.internal:5432/db\nSSH_HOST=bastion.example\nDB_PORT=5432\n"
	if err := os.WriteFile(filepath.Join(root, ".env.staging"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan struct{}, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		accepted <- struct{}{}
		conn.Close()
	}()
	stubDBEnsureTunnel(t, func(context.Context, string, *config.DBTunnel, string) (int, error) {
		return ln.Addr().(*net.TCPAddr).Port, nil
	})

	add := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/add", map[string]any{
		"name": "staging", "url": "${DATABASE_URL}", "env_file": ".staging", "project_path": root,
		"tunnel": map[string]any{
			"host": "${SSH_HOST}", "remote_host": "db.internal", "remote_port_env": "${DB_PORT}",
		},
	})
	if add.Code != http.StatusCreated {
		t.Fatalf("add = %d body=%s", add.Code, add.Body.String())
	}
	if rec := dbPostJSON(t, h.HandleDBConnectUnlock, "/api/dbconnect/unlock", map[string]any{
		"surface": "s-tunnel", "project_path": root,
	}); rec.Code != http.StatusOK {
		t.Fatalf("unlock = %d body=%s", rec.Code, rec.Body.String())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, _, err := h.dbGrantDB(ctx, "s-tunnel", "staging"); err == nil {
		t.Fatal("open against a listener that hangs up succeeded")
	}
	select {
	case <-accepted:
	case <-time.After(2 * time.Second):
		t.Fatal("the connection did not dial the tunnel port")
	}
}

// A literal "$NAME" is not a template: it must reach the tunnel verbatim, and
// the lookup must never be consulted for it.
func TestDBResolveTunnelKeepsLiteralDollarFieldsVerbatim(t *testing.T) {
	tun := &config.DBTunnel{
		Host:         "bastion.example",
		RemoteHost:   "db.internal",
		RemotePort:   5432,
		IdentityFile: "/Users/me/.ssh/id_$work",
	}
	lookup := func(name string) (string, bool) {
		t.Fatalf("lookup consulted for %q; a literal field must not be expanded", name)
		return "", false
	}
	got, err := dbResolveTunnel(tun, lookup, func() error { return nil })
	if err != nil {
		t.Fatalf("dbResolveTunnel: %v", err)
	}
	if got.IdentityFile != "/Users/me/.ssh/id_$work" {
		t.Fatalf("identity_file = %q, want the literal path", got.IdentityFile)
	}
	if dbTunnelHasEnv(tun) {
		t.Fatal("a literal bare $NAME counted as an env reference")
	}
}

// A literal companion port is a number, not a template: it resolves without a
// lookup and without requiring an env file.
func TestDBResolveTunnelLiteralPortCompanionIsNotATemplate(t *testing.T) {
	tun := &config.DBTunnel{Host: "bastion.example", RemoteHost: "db.internal", RemotePortEnv: "5432"}
	if dbTunnelHasEnv(tun) {
		t.Fatal("a literal remote_port_env counted as an env reference")
	}
	got, err := dbResolveTunnel(tun, func(string) (string, bool) { return "", false }, func() error { return nil })
	if err != nil {
		t.Fatalf("dbResolveTunnel: %v", err)
	}
	if got.RemotePort != 5432 {
		t.Fatalf("remote port = %d, want 5432", got.RemotePort)
	}
}

// A ${VAR} field still expands, and an unset ${VAR} still errors naming the
// variable, even when a sibling field is a literal "$NAME".
func TestDBResolveTunnelTemplateStillExpandsAndErrorsWithLiteralSibling(t *testing.T) {
	tun := &config.DBTunnel{
		Host:         "bastion.example",
		RemoteHost:   "db.internal",
		RemotePort:   5432,
		IdentityFile: "/keys/${KEY_NAME}_$work",
	}
	ok := func(name string) (string, bool) {
		if name == "KEY_NAME" {
			return "id_ed25519", true
		}
		return "", false
	}
	got, err := dbResolveTunnel(tun, ok, func() error { return nil })
	if err != nil {
		t.Fatalf("dbResolveTunnel: %v", err)
	}
	if got.IdentityFile != "/keys/id_ed25519_$work" {
		t.Fatalf("identity_file = %q; the braced reference must expand and the bare one must not have been added to the lookup", got.IdentityFile)
	}

	unset := &config.DBTunnel{Host: "${SSH_HOST_IS_UNSET}", RemoteHost: "db", RemotePort: 5432}
	_, err = dbResolveTunnel(unset, func(string) (string, bool) { return "", false }, func() error { return nil })
	if err == nil || !strings.Contains(err.Error(), "SSH_HOST_IS_UNSET") {
		t.Fatalf("err = %v, want the unset variable named", err)
	}
}

// The port-range message names the real range for each field: local_port
// accepts 0 (auto-assign), the others start at 1.
func TestDBResolveTunnelPortRangeMessageMatchesAllowZero(t *testing.T) {
	noErr := func() error { return nil }
	lookup := func(string) (string, bool) { return "70000", true }

	local := &config.DBTunnel{Host: "bastion", RemoteHost: "db", RemotePort: 5432, LocalPortEnv: "${LP}"}
	_, err := dbResolveTunnel(local, lookup, noErr)
	if err == nil || !strings.Contains(err.Error(), "local_port must resolve to a port between 0 and 65535") {
		t.Fatalf("local_port err = %v, want the 0-to-65535 range", err)
	}

	remote := &config.DBTunnel{Host: "bastion", RemoteHost: "db", RemotePortEnv: "${RP}"}
	_, err = dbResolveTunnel(remote, lookup, noErr)
	if err == nil || !strings.Contains(err.Error(), "remote_port must resolve to a port between 1 and 65535") {
		t.Fatalf("remote_port err = %v, want the 1-to-65535 range", err)
	}

	zero := &config.DBTunnel{Host: "bastion", RemoteHost: "db", RemotePort: 5432, LocalPortEnv: "${LP}"}
	got, err := dbResolveTunnel(zero, func(string) (string, bool) { return "0", true }, noErr)
	if err != nil {
		t.Fatalf("local_port 0 rejected: %v", err)
	}
	if got.LocalPort != 0 {
		t.Fatalf("local port = %d, want 0", got.LocalPort)
	}
}

// A literal URL with a ${VAR} tunnel field has no env file to read from, so the
// add is refused up front and nothing is saved.
func TestDBConnectAddRejectsLiteralURLWithEnvTunnel(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h, _ := newFilesHandler(t)
	rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/add", map[string]any{
		"name": "mixed", "url": dbTestURL, "password": "master-pw",
		"tunnel": map[string]any{"host": "${SSH_HOST}", "remote_host": "db", "remote_port": 5432},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("add = %d body=%s, want 400", rec.Code, rec.Body.String())
	}
	want := "SSH tunnel fields that use ${VAR} need a template URL (with an env file); use literal tunnel values with a literal URL"
	if !strings.Contains(rec.Body.String(), want) {
		t.Fatalf("body = %s, want the template-URL message", rec.Body.String())
	}
}

// env_file on a connection that reads no env reference is refused: it would be
// silently ignored otherwise.
func TestDBConnectAddRejectsEnvFileForLiteralConnection(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h, _ := newFilesHandler(t)
	rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/add", map[string]any{
		"name": "literal", "url": dbTestURL, "password": "master-pw", "env_file": ".staging",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("add = %d body=%s, want 400", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "env_file only applies to a template URL") {
		t.Fatalf("body = %s, want the env_file explanation", rec.Body.String())
	}
}

// A literal connection with a literal tunnel may still be added: the literal
// companion port is validated through the resolver, not refused.
func TestDBConnectAddAcceptsLiteralURLWithLiteralTunnel(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h, _ := newFilesHandler(t)
	rec := dbPostJSON(t, h.HandleDBConnectAdd, "/api/dbconnect/add", map[string]any{
		"name": "literal-tunnel", "url": dbTestURL, "password": "master-pw",
		"tunnel": map[string]any{"host": "bastion", "remote_host": "db", "remote_port_env": "5432"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add = %d body=%s, want 201", rec.Code, rec.Body.String())
	}
}
