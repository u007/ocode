package agent

import (
	"encoding/json"
	"fmt"
	"testing"
)

// TestSandboxRepoMetadataReadsAllow pins the fix for "git ls-files and
// ls .git/ should be auto allowed": reading or listing a repository-metadata
// directory (.git/, .github/workflows/) is ordinary repo inspection, so it must
// not Ask in sandbox mode. Before this, sandboxSensitivePath blanket-reused
// isSensitivePath (whose comment scoped the reuse to ".env files"), which also
// matched the repo-metadata dirs and made `ls .git/` — but not the identical
// `ls .git` — Ask, contradicting normal mode (which allows it) and the
// documented sandbox sensitive set (auth.json, config-dir writes, ~/.ssh, .env).
func TestSandboxRepoMetadataReadsAllow(t *testing.T) {
	pm := sandboxDecideTestPM(t)

	for _, cmd := range []string{
		// The user-reported command, plus the spellings that resolve to the
		// same <workdir>/.git target.
		"ls .git/",
		"ls -la .git/",
		"cat .git/config",
		"cat .git/HEAD",
		"du -sh .git/",
		"find .git -maxdepth 1",
		"grep -r pattern .git/config",
		"ls .github/workflows/",
		"cat .github/workflows/ci.yml",
		"head -5 .github/workflows/ci.yml",
		// Compound line: the benign half must stay allowed.
		"ls .git/ && echo hi > notes.txt",
		// Absolute spelling of the same target.
		"cat " + pm.workDir + "/.git/config",
	} {
		if dec := decideBash(t, pm, cmd); dec.Level != PermissionAllow {
			t.Errorf("sandbox %q = %s, want Allow (repo-metadata reads are not sensitive)", cmd, dec.Level)
		}
	}
}

// TestSandboxRepoMetadataWritesStillAsk is the other half of the contract:
// splitting read from write must NOT open a write path. A planted
// .git/hooks/* or .github/workflows/* executes arbitrary code on the next git
// command / CI run, and stays inside the workdir so the OS write-wall cannot
// constrain it — every write/delete form must still Ask.
func TestSandboxRepoMetadataWritesStillAsk(t *testing.T) {
	pm := sandboxDecideTestPM(t)

	for _, cmd := range []string{
		// Redirection / heredoc
		"echo x > .git/config",
		"cat > .git/hooks/pre-commit",
		// Writer commands taking the target as a positional
		"tee .git/hooks/pre-commit",
		"touch .git/hooks/x",
		"rm .git/config",
		"mv /tmp/x .git/config",
		// mv deletes its source: moving repo metadata OUT is a delete.
		"mv .git/hooks/pre-commit /tmp/x",
		"mv .github/workflows/ci.yml /tmp",
		"cp /tmp/x .git/config",
		"sed -i s/a/b/ .git/config",
		"chmod +x .git/hooks/x",
		// Unrecognized write commands must fail closed.
		"truncate -s 0 .git/config",
		// Workflow writes too.
		"tee .github/workflows/ci.yml",
		"echo x > .github/workflows/ci.yml",
	} {
		if dec := decideBash(t, pm, cmd); dec.Level != PermissionAsk {
			t.Errorf("sandbox %q = %s, want Ask (repo-metadata writes stay gated)", cmd, dec.Level)
		}
	}
}

// TestSandboxSecretMaterialStillAsksOnRead guards the other direction of the
// split: secret material stays sensitive for READS, because a read can
// exfiltrate a credential and the OS write-wall does not prevent that.
func TestSandboxSecretMaterialStillAsksOnRead(t *testing.T) {
	work := t.TempDir()
	pm := NewPermissionManager()
	pm.SetWorkDir(work)
	pm.SetMode(PermissionModeSandbox)

	for _, cmd := range []string{
		"cat " + work + "/.env",
		"cat " + work + "/.env.local",
		"cat " + work + "/.netrc",
		"cat " + work + "/.npmrc",
		"cat " + work + "/id_rsa",
		"cat " + work + "/foo.pem",
		"cat " + work + "/foo.key",
		"cat " + work + "/.aws/credentials",
		"ls " + work + "/.aws/",
	} {
		dec := pm.Decide("bash", json.RawMessage(fmt.Sprintf(`{"command":%q}`, cmd)))
		if dec.Level != PermissionAsk {
			t.Errorf("sandbox %q = %s, want Ask (secret material read stays gated)", cmd, dec.Level)
		}
	}
}

// TestSandboxTargetsWriteClassification pins sandboxSensitiveTargets'
// per-target write map, which is what lets the carve-out split repo-metadata
// reads from writes. The map must be conservative: an unrecognized command
// marks its path args as writes (fail closed).
func TestSandboxTargetsWriteClassification(t *testing.T) {
	work := t.TempDir()
	gitConfig := resolvePath(".git/config", work)

	cases := []struct {
		cmd        string
		wantTarget string
		wantWrite  bool
	}{
		{"cat .git/config", resolvePath(".git/config", work), false},
		{"ls .git/", resolvePath(".git", work), false},
		{"echo x > .git/config", gitConfig, true},
		{"tee .git/config", gitConfig, true},
		{"truncate -s 0 .git/config", gitConfig, true},
		{"cp /tmp/x .git/config", gitConfig, true},
		{"sed -n 1p .git/config", resolvePath(".git/config", work), false},
		{"sed -i s/a/b/ .git/config", gitConfig, true},
	}
	for _, tc := range cases {
		t.Run(tc.cmd, func(t *testing.T) {
			targets, writeTargets := sandboxSensitiveTargets(tc.cmd, work)
			found := false
			for _, tgt := range targets {
				if tgt == tc.wantTarget {
					found = true
					if got := writeTargets[tgt]; got != tc.wantWrite {
						t.Errorf("writeTargets[%q] = %v, want %v", tgt, got, tc.wantWrite)
					}
				}
			}
			if !found {
				t.Errorf("target %q missing from %v", tc.wantTarget, targets)
			}
		})
	}
}
